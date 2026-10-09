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
	"github.com/venabots/autoreview/internal/plain"
	"github.com/venabots/autoreview/internal/proc"
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
	prepared, err := prepare(cfg, rc)
	if err != nil {
		say(stderr, "error: %s\n", sanitize.Line(err.Error()))
		return engine.ExitFailed
	}
	logFile, err := os.OpenFile(prepared.run.Log(), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		say(stderr, "error: %v\n", err)
		return engine.ExitFailed
	}
	defer func() { _ = logFile.Close() }()

	// Written before the printer starts, so it is always the first line.
	say(io.MultiWriter(stdout, logFile), "skills: %s\n", prepared.skills)
	printer := plain.NewPrinter(stdout, stderr, logFile)
	for _, line := range prepared.notes {
		printer.Emit(engine.Noted{Text: line})
	}

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

	config := engineConfig(cfg, rc, false)
	config.Review = true
	config.Jobs = cfg.Jobs
	config.Continue = cfg.Continue
	config.NoPost = cfg.NoPost
	config.Focus = cfg.Focus
	config.Timeout = cfg.Timeout
	config.Budget = cfg.Budget
	config.Orchestrator = cfg.Orchestrator
	config.Override = cfg.Override
	config.Dashp = prepared.dashp

	eng := engine.New(config, deps)
	stopOnSignal(eng)
	result := eng.Run()
	printer.Close()
	printer.Finish(result, cfg.NoPost)
	return result.Code
}

// prepared is what a run needs in place before its first review.
type prepared struct {
	run       rundir.Run
	dashp     string
	skillsDir string
	// skills says which copy of the skills runs.
	skills string
	notes  []string
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
		if _, err := exec.LookPath(cfg.Orchestrator.CLI()); err != nil {
			return prepared{}, fmt.Errorf("%s is not installed, and it runs the reviews (--orchestrator chooses another)", cfg.Orchestrator.CLI())
		}
	}
	source, err := skills.Parse(cfg.Skills)
	if err != nil {
		return prepared{}, err
	}
	if p.run, err = rundir.New(cfg.LogDir, os.TempDir()); err != nil {
		return prepared{}, err
	}
	if !builtIn {
		return p, nil
	}
	p.skills = source.Describe(version)
	if !cfg.Orchestrator.DiscoversStagedSkills() {
		return p, missingSkills(cfg.Orchestrator)
	}
	staged, err := skills.Stage(source, bundle.Skills(), p.run.Agent())
	if err != nil {
		return prepared{}, err
	}
	if staged {
		p.skillsDir = p.run.Agent()
	}
	installed := []string{
		filepath.Join(session.ConfigDir(os.Getenv), "skills"),
		filepath.Join(rc.Root, ".claude", "skills"),
	}
	if note := skills.ShadowNote(installed, source.Names(bundle.Skills())); note != "" {
		p.notes = append(p.notes, note)
	}
	return p, nil
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
