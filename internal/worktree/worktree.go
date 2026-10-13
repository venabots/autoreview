// Package worktree makes a worktree for a task on one of your PRs: the PR's
// branch, checked out somewhere of its own, so the task can edit, commit and
// push without touching the checkout you are working in.
//
// The local branch has the PR's own name and tracks it on origin, so the
// plain `git push` the skills run pushes to the PR. Two things would make
// that unsafe, and both are refused rather than worked around: the branch
// checked out in another worktree (it would move under that checkout), and
// a local branch of that name holding commits origin does not have
// (starting the task from origin would throw them away).
package worktree

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Git runs one git command in dir and returns its stdout. Every git call in
// this package goes through one, so a test can record the argv with a fake
// or prove the behavior on a real repo. The real one is Exec.
type Git func(ctx context.Context, dir string, args ...string) ([]byte, error)

// FetchTimeout bounds the fetch of the PR's branch. A remote that stalls
// must not hold the pass open: the pass waits for every task's worktree.
const FetchTimeout = 120 * time.Second

// Request names the PR a task is for and where its worktree goes.
type Request struct {
	// Root is the person's checkout.
	Root string
	// Path is where the worktree goes: PathFor(run root, PR).
	Path string
	PR   int
	// Branch is the PR's head branch.
	Branch string
	// CrossRepo is true for a PR from a fork, whose branch is not on origin.
	CrossRepo bool
}

// Prepared is a worktree made for one task.
type Prepared struct {
	Path string
	// Branch is the local branch checked out in it. Finish needs it to
	// delete a branch this run made.
	Branch string
	// Head is the commit the worktree starts at: the PR's head on origin
	// after the fetch. Whether the task pushed is read against this, not
	// against a list that may be minutes old.
	Head string
	// CreatedBranch is true when Prepare made the local branch. Only then
	// is it deleted with the worktree: a branch that was there before is
	// yours.
	CreatedBranch bool
}

// Kept is a worktree that Finish left in place, because removing it would
// lose work. Why says what work.
type Kept struct {
	Path string
	Why  string
}

// The two kinds of work that keep a worktree.
const (
	WhyUncommitted = "uncommitted changes"
	WhyUnpushed    = "commits that are not pushed"
)

// PathFor is where a task on PR pr runs, under the run directory.
func PathFor(runRoot string, pr int) string {
	return filepath.Join(runRoot, "worktrees", "pr-"+strconv.Itoa(pr))
}

// Note is the line a kept worktree gets, for the log and the view.
func Note(pr int, k Kept) string {
	return "note: PR #" + strconv.Itoa(pr) + "'s worktree has " + k.Why + ": " + k.Path
}

// madeHere reports whether path is a task worktree this tool made:
// run-*/worktrees/pr-N, in a run directory. A worktree of yours that
// happens to be called pr-12 is not, and must never be force-removed.
func madeHere(path string) bool {
	own := filepath.Base(path)
	parent := filepath.Dir(path)
	run := filepath.Base(filepath.Dir(parent))
	n, ok := strings.CutPrefix(own, "pr-")
	return filepath.Base(parent) == "worktrees" &&
		strings.HasPrefix(run, "run-") &&
		ok && n != "" && allDigits(n)
}

func allDigits(s string) bool {
	for _, b := range []byte(s) {
		if b < '0' || b > '9' {
			return false
		}
	}
	return true
}

// run calls git and trims its stdout, as every caller here reads one value.
func run(ctx context.Context, git Git, dir string, args ...string) (string, error) {
	out, err := git(ctx, dir, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
