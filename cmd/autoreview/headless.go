package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	bundle "github.com/venabots/autoreview"
	"github.com/venabots/autoreview/internal/cli"
	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/findings"
	"github.com/venabots/autoreview/internal/gh"
	"github.com/venabots/autoreview/internal/ledger"
	"github.com/venabots/autoreview/internal/marks"
	"github.com/venabots/autoreview/internal/plain"
	"github.com/venabots/autoreview/internal/proc"
	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/rebase"
	"github.com/venabots/autoreview/internal/repo"
	"github.com/venabots/autoreview/internal/review"
	"github.com/venabots/autoreview/internal/rundir"
	"github.com/venabots/autoreview/internal/sanitize"
	"github.com/venabots/autoreview/internal/session"
	"github.com/venabots/autoreview/internal/skills"
)

// envDashp names a dash-p binary to use in place of the one on PATH.
const envDashp = "DASHP_BIN"

// runHeadless makes one pass off a terminal: it reviews every PR that is
// ready, prints one line for each step, and exits with a status a script
// can trust.
func runHeadless(cfg cli.Config, rc repo.Context, stdout, stderr io.Writer) int {
	run, err := start(cfg, rc, false, stdout, stderr)
	if err != nil {
		say(stderr, "error: %s\n", sanitize.Line(err.Error()))
		return engine.ExitFailed
	}
	defer run.close()
	result := run.engine.Run()
	run.printer.Close()
	run.printer.Finish(result, cfg.NoPost)
	return result.Code
}

// started is a run that is ready to go: its engine, and the printer that
// writes what it does.
type started struct {
	engine  *engine.Engine
	printer *plain.Printer
	log     *os.File
}

func (s started) close() {
	_ = s.log.Close()
}

// start checks what a review needs, makes the run directory, and builds
// the engine. Its first lines are printed here, on the normal screen: the
// skills that run, and any note a person should read before the view
// covers it.
//
// A held run is the view's: it stays open after its pass, and its lines go
// to the run log only, because the view owns the terminal.
func start(cfg cli.Config, rc repo.Context, held bool, stdout, stderr io.Writer) (started, error) {
	prepared, err := prepare(cfg, rc)
	if err != nil {
		return started{}, err
	}
	logFile, err := os.OpenFile(prepared.run.Log(), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return started{}, err
	}
	out, errs := stdout, stderr
	if held {
		out, errs = io.Discard, io.Discard
	}
	// Which skills run is for the log and a plain run. A note is for a
	// person, so it is printed before the view covers the screen.
	say(io.MultiWriter(out, logFile), "skills: %s\n", prepared.skills)
	if prepared.fallbackLine != "" {
		say(io.MultiWriter(out, logFile), "fallback: %s\n", prepared.fallbackLine)
	}
	for _, note := range prepared.notes {
		say(io.MultiWriter(stderr, logFile), "note: %s\n", sanitize.Line(note))
	}
	printer := plain.NewPrinter(out, errs, logFile)

	deps := deps(cfg, rc)
	deps.Procs = proc.OS{}
	deps.Run = prepared.run
	deps.SkillsDir = prepared.skillsDir
	deps.Journal = printer.Emit
	deps.Sessions, deps.Transcript = sessions()
	deps.Verdict = func(ctx context.Context, pr int, since time.Time) review.Readback {
		return review.Verdict(ctx, gh.Exec, rc.Slug(), pr, rc.Me, since)
	}
	deps.Record = recorder(rc)
	// Under --no-post the run still decides, and says what it would post.
	deps.Rebase = func(ctx context.Context, pr prs.PR) (rebase.State, error) {
		return rebase.Check(ctx, gh.Exec, rc.Slug(), rc.Me, pr, !cfg.NoPost)
	}
	if !cfg.NoPost {
		// --no-post promises to leave the PR alone, the reaction too.
		deps.Marks = marks.New(gh.Exec, rc.Owner, rc.Name)
	}
	if held {
		// Tasks start only from keys, so only the view has them.
		deps = withTasks(deps, cfg, rc, prepared.run)
	}

	config := engineConfig(cfg, rc, held)
	config.Review = true
	config.HoldOpen = held
	config.Jobs = cfg.Jobs
	config.Continue = cfg.Continue
	config.NoPost = cfg.NoPost
	config.Focus = cfg.Focus
	config.Timeout = cfg.Timeout
	config.Budget = cfg.Budget
	config.Orchestrator = cfg.Orchestrator
	config.Fallback = prepared.fallback
	config.Override = cfg.Override
	config.Dashp = prepared.dashp

	eng := engine.New(config, deps)
	stopOnSignal(eng)
	return started{engine: eng, printer: printer, log: logFile}, nil
}

// prepared is what a run needs in place before its first review.
type prepared struct {
	run       rundir.Run
	dashp     string
	skillsDir string
	// skills says which copy of the skills runs.
	skills string
	// fallback retries a review whose orchestrator failed, or is nil.
	// fallbackLine says which, or why there is none.
	fallback     *review.Orchestrator
	fallbackLine string
	notes        []string
}

// prepare checks what a review needs and makes the run directory. A
// missing program is found here, once, with a message that says what to
// install. Found later, it would fail every PR one at a time.
func prepare(cfg cli.Config, rc repo.Context) (prepared, error) {
	p := prepared{skills: "not used by $" + cli.EnvOverride}
	builtIn := cfg.Override == ""
	if builtIn {
		dashp, err := findDashp()
		if err != nil {
			return prepared{}, err
		}
		p.dashp = dashp
		if !installed(cfg.Orchestrator.CLI()) {
			return prepared{}, fmt.Errorf("%s is not installed, and it runs the reviews (--orchestrator chooses another)", cfg.Orchestrator.CLI())
		}
		if !cfg.Orchestrator.DiscoversStagedSkills() {
			if err := missingSkills(cfg.Orchestrator); err != nil {
				return prepared{}, err
			}
		}
		fallback, line, err := resolveFallback(cfg)
		if err != nil {
			return prepared{}, err
		}
		p.fallback, p.fallbackLine = fallback, line
		p.notes = review.BudgetNotes(cfg.Budget, cfg.Orchestrator, fallback)
		if len(cfg.Panelists) == 1 {
			// auto-review approves only when several models reviewed.
			p.notes = append(p.notes, "the settings seat one panelist, so no review approves a PR: auto-review asks for several")
		}
	}
	source, err := skills.Parse(cfg.Skills)
	if err != nil {
		return prepared{}, err
	}
	if builtIn {
		if p, err = withVerdictCopies(p, cfg, source, rc.Root); err != nil {
			return prepared{}, err
		}
	}
	if p.run, err = rundir.New(cfg.LogDir, os.TempDir()); err != nil {
		return prepared{}, err
	}
	if !builtIn {
		return p, nil
	}
	p.skills = source.Describe(version)
	staged, err := skills.Stage(source, bundle.Skills(), p.run.Agent())
	if err != nil {
		return prepared{}, err
	}
	if staged {
		p.skillsDir = p.run.Agent()
	}
	dirs := []string{
		filepath.Join(session.ConfigDir(os.Getenv), "skills"),
		filepath.Join(rc.Root, ".claude", "skills"),
	}
	if note := skills.ShadowNote(dirs, source.Names(bundle.Skills())); note != "" {
		p.notes = append(p.notes, note)
	}
	return p, nil
}

func installed(cli string) bool {
	_, err := exec.LookPath(cli)
	return err == nil
}

// resolveFallback decides at the start which orchestrator retries a review
// whose orchestrator failed, and says so. Found out at the first failure,
// a fallback that cannot run would turn one outage into two.
//
// A fallback named by the person must be installed. An automatic one is
// the other agent if it is installed, and is dropped with a reason when it
// lacks the review skills.
func resolveFallback(cfg cli.Config) (*review.Orchestrator, string, error) {
	resolved, ok := cfg.Fallback.Resolve(cfg.Orchestrator, installed)
	if !ok {
		return nil, cfg.Fallback.Describe(cfg.Orchestrator, nil), nil
	}
	if !installed(resolved.CLI()) {
		return nil, "", fmt.Errorf("--fallback %s is not installed", resolved.CLI())
	}
	if !resolved.DiscoversStagedSkills() {
		if err := missingSkills(resolved); err != nil {
			if cfg.Fallback.Kind == review.FallbackSpec {
				return nil, "", fmt.Errorf("--fallback %s: %w", resolved.Label(), err)
			}
			return nil, "none (" + err.Error() + ")", nil
		}
	}
	return &resolved, resolved.Label(), nil
}

func findDashp() (string, error) {
	if path := os.Getenv(envDashp); path != "" {
		return path, nil
	}
	path, err := exec.LookPath("dash-p")
	if err != nil {
		return "", fmt.Errorf("dash-p is not installed (brew install venabots/tap/dash-p), and $%s names no other", envDashp)
	}
	return path, nil
}

// missingSkills checks an agent that finds skills in its own directories
// only. It is not handed the staged copies, so a skill it lacks would fail
// every review, each after the agent had started.
func missingSkills(o review.Orchestrator) error {
	roots := review.SkillRoots(o.Backend, os.Getenv("HOME"), os.Getenv("CODEX_HOME"))
	missing := review.MissingSkills(roots, review.EntrySkills)
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("%s has no %s skill; install the review skills under %s", o.CLI(), strings.Join(missing, ", "), o.SkillsHome())
}

// sessions answers what the engine asks about review sessions, from Claude
// Code's own store and the process table.
func sessions() (session.Probe, func(string) (string, bool)) {
	store := session.StoreIn(session.ConfigDir(os.Getenv))
	probe := session.Probe{
		Exists: store.Exists,
		InUse:  func(id string) bool { return session.InUse(context.Background(), id) },
	}
	return probe, store.Transcript
}

// recorder keeps each finished review in the ledger, or is nil when the
// ledger is off.
func recorder(rc repo.Context) func(engine.Finished) error {
	path, ok := ledger.Path(os.Getenv)
	if !ok {
		return nil
	}
	return func(f engine.Finished) error {
		if f.Job.Trailer == nil {
			return errors.New("the review reported no trailer")
		}
		return ledger.Append(path, ledger.ForReview(ledger.Review{
			Repo:       rc.Slug(),
			PR:         f.Job.PR,
			Session:    f.Job.Session,
			StartedAt:  f.Job.StartedAt,
			FinishedAt: f.FinishedAt,
			Verdict:    f.Job.Verdict,
			Trailer:    *f.Job.Trailer,
			Cost:       spent(f.Job.Cost),
			Elapsed:    f.Job.Elapsed,
			Model:      f.Job.Model,
			Findings:   findings.Parse(f.Text),
			// A sign-off is text, and it is not a review. Only a synthesis
			// counts as read, or the keep rate would score a review that
			// was never there to score.
			Reviewed: f.HasText && findings.IsSynthesis(f.Text),
		}))
	}
}

// spent is a cost for the ledger. A harness that does not count writes a
// zero, and v1 records no cost of zero: it would read as a free review.
func spent(cost *float64) *float64 {
	if cost == nil || *cost <= 0 {
		return nil
	}
	return cost
}
