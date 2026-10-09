// Package rundir is everything one run writes to disk. Each run gets a
// directory of its own: a --log-dir is a fixed path, and cron makes
// overlapping runs ordinary. Runs that shared one directory would read each
// other's results and report them as their own.
//
// The run directory is made, not named. A directory named for the pid would
// be inherited by whichever later run the kernel hands that pid to.
package rundir

import (
	"fmt"
	"os"
	"path/filepath"
)

// private is the mode of every directory a run makes. Under a shared /tmp
// the run directory holds each diff and each prompt, and the default mode
// would offer all of it to anyone with an account on the machine.
const private = 0o700

// Run is one run's directory.
type Run struct {
	Root string
}

// New makes a run directory. With a log directory it is `<logDir>/run-*`.
// With none it is `<tempDir>/autoreview.*/run-*`, so the two shapes differ
// only in the parent.
func New(logDir, tempDir string) (Run, error) {
	parent := logDir
	if parent == "" {
		made, err := os.MkdirTemp(tempDir, "autoreview.")
		if err != nil {
			return Run{}, fmt.Errorf("making a log directory: %w", err)
		}
		parent = made
	}
	if err := os.MkdirAll(parent, private); err != nil {
		return Run{}, fmt.Errorf("making the log directory: %w", err)
	}
	root, err := os.MkdirTemp(parent, "run-")
	if err != nil {
		return Run{}, fmt.Errorf("making the run directory: %w", err)
	}
	return Run{Root: root}, nil
}

// Log is the run's own log: one plain line for each thing it did. The view
// shows it, and it is what a person reads after a run that went wrong.
func (r Run) Log() string {
	return filepath.Join(r.Root, "autoreview.log")
}

// Agent is where the run's skills are staged. Every reviewer is handed
// this directory.
func (r Run) Agent() string {
	return filepath.Join(r.Root, "agent")
}

// Worktree is where a task on PR pr gets its own checkout.
func (r Run) Worktree(pr int) string {
	return filepath.Join(r.Root, "worktrees", fmt.Sprintf("pr-%d", pr))
}

// SessionFile records the session a review of PR pr ran in.
func (r Run) SessionFile(pr int) string {
	return filepath.Join(r.Root, fmt.Sprintf("session-%d.id", pr))
}

// RecordSession writes the session id for PR pr. It writes a temporary file
// and renames it, so a reader never sees half an id.
func (r Run) RecordSession(pr int, id string) error {
	path := r.SessionFile(pr)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(id), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Pass makes the directory for pass n and returns it.
func (r Run) Pass(n int) (Pass, error) {
	return makePass(filepath.Join(r.Root, fmt.Sprintf("pass-%d", n)))
}

// Pass is the directory one pass writes its job files to.
type Pass struct {
	Dir string
}

// Task is a directory of its own under the pass for one kind of task:
// `pass-1/fix/pr-4.log`. A task's files then never overwrite a review's,
// and every path below keeps its one shape.
func (p Pass) Task(tag string) (Pass, error) {
	return makePass(filepath.Join(p.Dir, tag))
}

func makePass(dir string) (Pass, error) {
	if err := os.MkdirAll(dir, private); err != nil {
		return Pass{}, err
	}
	return Pass{Dir: dir}, nil
}

func (p Pass) file(pr int, suffix string) string {
	return filepath.Join(p.Dir, fmt.Sprintf("pr-%d.%s", pr, suffix))
}

// Stdout is what the reviewer of PR pr printed: its answer, in the
// harness's envelope.
func (p Pass) Stdout(pr int) string { return p.file(pr, "json") }

// Stderr is what the reviewer of PR pr wrote to stderr.
func (p Pass) Stderr(pr int) string { return p.file(pr, "log") }

// Meta is the file the harness writes about the job: the session, the cost
// and the model.
func (p Pass) Meta(pr int) string { return p.file(pr, "meta.json") }

// Review is the review of PR pr as text. Stdout already holds it, wrapped
// in an envelope and JSON-escaped, which a program can read and a person
// cannot.
func (p Pass) Review(pr int) string { return p.file(pr, "review.md") }

// ArchiveAttempt sets aside what a failed attempt wrote, under the name of
// the orchestrator that failed: `pr-9.claude.log`. The fallback's attempt
// can then write the plain names without covering the explanation of the
// failure. A file that was never written has nothing to move.
func (p Pass) ArchiveAttempt(pr int, backend string) {
	for _, suffix := range []string{"log", "json", "meta.json"} {
		_ = os.Rename(p.file(pr, suffix), p.file(pr, backend+"."+suffix))
	}
}
