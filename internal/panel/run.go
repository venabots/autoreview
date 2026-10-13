// Package panel is `autoreview panel`: one change, reviewed by several
// models at once, then synthesized.
//
// It is the fan-out a review pass does with a different unit of work: N
// models on one diff, where a pass runs N PRs through one model. The shape
// is flat on purpose. Starting panelists, waiting for them, retrying a
// transient failure and collecting the results are mechanical, so a program
// does them. Deciding which findings are real needs a model with the code in
// front of it, so exactly one model call does that, at the end.
package panel

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/venabots/autoreview/internal/ledger"
	"github.com/venabots/autoreview/internal/repo"
	"github.com/venabots/autoreview/internal/review"
	"github.com/venabots/autoreview/internal/sanitize"
	"github.com/venabots/autoreview/internal/words"
)

// The exit statuses of the subcommand.
const (
	ExitOK          = 0
	ExitFailed      = 1
	ExitInterrupted = 130
)

// Run is `autoreview panel ...`. args are the arguments after the word
// "panel". It returns the exit status; everything it has to say is written
// to d.Stdout or d.Stderr by then. A ctx that ends is a ctrl-C: the run
// stops its panelists, removes its worktrees and exits 130.
func Run(ctx context.Context, args []string, d Deps) int {
	parsed, err := Parse(args, d.Env)
	if err != nil {
		_, _ = fmt.Fprintf(d.Stderr, "error: %v\n", err)
		if WantsHelp(err) {
			_, _ = fmt.Fprint(d.Stderr, Help)
		}
		return ExitFailed
	}
	switch {
	case parsed.Help:
		_, _ = fmt.Fprint(d.Stdout, Help)
		return ExitOK
	case parsed.Version:
		_, _ = fmt.Fprintf(d.Stdout, "autoreview panel %s\n", d.Version)
		return ExitOK
	}
	code, err := execute(ctx, parsed.Config, d)
	if err != nil && ctx.Err() != nil {
		// A git that a ctrl-C stopped says "signal: killed", which names
		// the wrong cause. The person pressed ctrl-C, and 130 says so.
		_, _ = fmt.Fprintln(d.Stderr, "panel: interrupted")
		return ExitInterrupted
	}
	if err != nil {
		// Printed here, after the spinner is gone. The message can carry
		// what git said, which is process output like any other.
		_, _ = fmt.Fprintf(d.Stderr, "error: %s\n", sanitize.Line(err.Error()))
		return ExitFailed
	}
	return code
}

// execute is one run after the flags are read. An error is a failure the
// caller prints; a status with no error has already said what it had to.
func execute(ctx context.Context, cfg Config, d Deps) (int, error) {
	if !d.require("git", d.dashp()) {
		return ExitFailed, nil
	}
	specs, ok := d.panelSpecs(cfg)
	if !ok {
		return ExitFailed, nil
	}
	started := time.Now()
	// Started after every check that prints its own error, so none of them
	// arrives wearing a spinner.
	status := NewStatus(d.Stderr, d.Live)
	defer status.Clear()

	status.Step("reading the repo")
	root, err := d.repoRoot(ctx)
	if err != nil {
		return ExitFailed, err
	}
	status.Step(resolvingStep(cfg.Target))
	resolved, err := ResolveTarget(ctx, d.Git, cfg.Target, root, d.Cwd)
	if err != nil {
		return ExitFailed, err
	}
	dir, err := makeOutDir(cfg.LogDir, d.TempDir)
	if err != nil {
		return ExitFailed, err
	}
	s := prepared{cfg: cfg, d: d, status: status, root: root, dir: dir, resolved: resolved, panel: ResolvePanel(specs), started: started}
	return s.run(ctx)
}

// repoRoot is the top of the checkout the person stands in.
func (d Deps) repoRoot(ctx context.Context) (string, error) {
	out, err := d.Git(ctx, d.Cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("not inside a git checkout: %w", err)
	}
	return repo.RootFrom(out)
}

// makeOutDir makes the directory this run keeps its prompts, outputs and
// synthesis in. One for each run, made and not named: with a fixed
// --log-dir, two runs at once would otherwise each read the other's results.
//
// The directory is absolute. Git makes each worktree relative to the repo
// root, and a panelist starts relative to where the person stands, so a
// relative --log-dir would name two places.
func makeOutDir(logDir, tempDir string) (string, error) {
	base, err := filepath.Abs(orDefault(logDir, tempDir))
	if err != nil {
		return "", fmt.Errorf("creating %s: %w", orDefault(logDir, tempDir), err)
	}
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", fmt.Errorf("creating %s: %w", base, err)
	}
	dir, err := os.MkdirTemp(base, "panel.")
	if err != nil {
		return "", fmt.Errorf("creating the run directory under %s: %w", base, err)
	}
	return dir, nil
}

// prepared is one run with its target resolved and its directory made.
type prepared struct {
	cfg      Config
	d        Deps
	status   *Status
	root     string
	dir      string
	resolved Resolved
	panel    []Panelist
	started  time.Time
}

func (s prepared) run(ctx context.Context) (int, error) {
	promptPath := filepath.Join(s.dir, "review.prompt")
	prompt := BuildPrompt(s.resolved.Label, s.resolved.Isolated, s.cfg.Focus, s.resolved.Subject, s.resolved.Untracked)
	if err := os.WriteFile(promptPath, []byte(prompt), 0o600); err != nil {
		return ExitFailed, fmt.Errorf("writing the panelist prompt: %w", err)
	}
	cwds, synthCwd, wts, err := s.checkouts(ctx)
	// Removed on every way out, a failed panelist and a ctrl-C too: they
	// are registered in the person's real repository.
	defer wts.Remove(s.d.Git)
	if err != nil {
		if ctx.Err() != nil {
			s.status.Say("panel: interrupted while creating worktrees")
			return ExitInterrupted, nil
		}
		return ExitFailed, err
	}
	s.status.Suspend(s.writeHeader)

	r := s.panelRun(promptPath)
	outcomes := r.fanOut(ctx, s.panel, cwds)
	if ctx.Err() != nil {
		s.status.Clear()
		s.say("panel: interrupted; the worktrees are being removed")
		return ExitInterrupted, nil
	}
	return s.finish(ctx, r, outcomes, synthCwd)
}

// checkouts is where each panelist and the synthesis run. A committed
// target gets one worktree for each panelist and one more for the
// synthesis: the synthesizer checks findings against the reviewed code,
// and a panelist's worktree may hold its own investigation edits. A target
// in the working tree has no commit to pin, so everyone reads the person's
// checkout.
func (s prepared) checkouts(ctx context.Context) ([]string, string, Worktrees, error) {
	if s.resolved.SHA == "" {
		cwds := make([]string, len(s.panel))
		for i := range cwds {
			cwds[i] = s.root
		}
		return cwds, s.root, Worktrees{}, nil
	}
	ids := make([]string, 0, len(s.panel)+1)
	for _, p := range s.panel {
		ids = append(ids, p.ID)
	}
	// Only when there is a synthesis to run: one more checkout of a large
	// repository is the slowest thing here.
	if s.cfg.Synthesize {
		ids = append(ids, "synthesis")
	}
	wts, err := CreateWorktrees(ctx, s.d.Git, s.root, s.dir, ids, s.resolved.SHA, s.status.Step)
	if err != nil {
		return nil, "", wts, err
	}
	synthCwd := s.root
	if s.cfg.Synthesize {
		synthCwd = wts.Dirs[len(s.panel)]
	}
	return wts.Dirs[:len(s.panel)], synthCwd, wts, nil
}

func (s prepared) writeHeader() {
	ids := make([]string, len(s.panel))
	for i, p := range s.panel {
		ids[i] = p.ID
	}
	var b strings.Builder
	b.WriteString("# Panel review\n\n")
	fmt.Fprintf(&b, "- Target: %s\n", s.resolved.Label)
	fmt.Fprintf(&b, "- Panelists: %s\n", strings.Join(ids, ", "))
	fmt.Fprintf(&b, "- Outputs: `%s`\n", s.dir)
	if s.cfg.Focus != "" {
		fmt.Fprintf(&b, "- Focus: %s\n", sanitize.Line(s.cfg.Focus))
	}
	b.WriteString("\n")
	_, _ = fmt.Fprint(s.d.Stdout, b.String())
}

func (s prepared) panelRun(promptPath string) panelRun {
	return panelRun{
		launch: launch{
			starter:     s.d.Starter,
			dashp:       s.d.dashp(),
			outDir:      s.dir,
			timeoutSecs: s.cfg.TimeoutSecs,
			grace:       review.GuardGrace,
		},
		isolated:   s.resolved.Isolated,
		promptPath: promptPath,
		status:     s.status,
		out:        s.d.Stdout,
		log:        s.d.Stderr,
	}
}

// finish synthesizes the outcomes, or stops when there is nothing to
// synthesize or nobody asked for it, and records the run either way.
func (s prepared) finish(ctx context.Context, r panelRun, outcomes []Outcome, synthCwd string) (int, error) {
	answered := countAnswered(outcomes)
	if answered == 0 {
		s.status.Clear()
		s.record(outcomes, "")
		s.say("error: no panelist returned a review; there is nothing to synthesize")
		s.say("  what each one did is in " + s.dir)
		return ExitFailed, nil
	}
	if !s.cfg.Synthesize {
		s.status.Clear()
		s.record(outcomes, "")
		s.say("panel: --no-synthesis, stopping after " + words.Count(answered, "report"))
		return ExitOK, nil
	}
	report, err := r.synthesize(ctx, s.cfg.Synth, synthCwd, SynthesisPrompt(s.resolved, outcomes, s.cfg.Focus))
	if err != nil {
		// A synthesis the run stopped on purpose is an interrupt: the
		// reports above are real and already printed.
		if ctx.Err() != nil || errors.Is(err, errSynthesisInterrupted) {
			s.status.Clear()
			s.say("panel: interrupted during synthesis")
			return ExitInterrupted, nil
		}
		// The panelists answered, and a failed synthesis does not erase
		// that: their launches still count toward availability.
		s.record(outcomes, "")
		return ExitFailed, err
	}
	s.status.Clear()
	_, _ = fmt.Fprintf(s.d.Stdout, "# Synthesis\n\n%s\n\n---\n%d of %d answered · outputs: `%s`\n",
		sanitize.Block(report), answered, len(outcomes), s.dir)
	s.record(outcomes, report)
	return ExitOK, nil
}

// record appends the run to the ledger. Best effort: a history that could
// not be written is a note, not a failed review.
func (s prepared) record(outcomes []Outcome, synthesis string) {
	path, ok := ledger.Path(s.d.Env)
	if !ok {
		return
	}
	base := ledger.Panel{
		Repo:       repoSlug(s.d.Git, s.root),
		PID:        s.d.PID,
		StartedAt:  s.started,
		FinishedAt: time.Now(),
		SynthModel: s.cfg.Synth.Model,
	}
	if err := ledger.Append(path, panelRecord(base, outcomes, synthesis)); err != nil {
		s.say(fmt.Sprintf("panel: could not record this run in the ledger at %s: %v", path, err))
	}
}

// say writes one line to stderr.
func (s prepared) say(line string) {
	_, _ = fmt.Fprintln(s.d.Stderr, line)
}

// resolvingStep is what the run says while it works out the target. An
// audit lists files; everything else builds a diff.
func resolvingStep(t Target) string {
	if t.Kind == Tree {
		return "listing the files"
	}
	return "building the diff"
}
