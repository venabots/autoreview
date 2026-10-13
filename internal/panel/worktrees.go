package panel

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/venabots/autoreview/internal/sanitize"
	"github.com/venabots/autoreview/internal/worktree"
)

// cleanupTimeout bounds the removal of the worktrees. It runs on a context
// of its own: the run's context is already over after a ctrl-C, which is
// exactly when the worktrees must still go.
const cleanupTimeout = 2 * time.Minute

// Worktrees is the throwaway checkouts of one run, one for each panelist and
// one for the synthesis, all detached at the same commit.
//
// One each and not one shared: panelists run test suites and edit files to
// investigate. In a shared checkout, test runners race on lockfiles and
// build directories, and one panelist's edits leak into another's reading
// of the code, which breaks the independence the panel exists for.
type Worktrees struct {
	root string
	// Dirs holds every path an add was tried for, in order: the ones that
	// failed too, because git can register a worktree partway through.
	Dirs []string
}

// CreateWorktrees makes one worktree for each id under base. step is told
// before each one: N checkouts of the repository are the longest wait
// before any model is asked anything.
//
// The Worktrees comes back with an error too, holding every path tried, so
// the caller removes what was made. A ctrl-C between two adds stops the
// next one.
func CreateWorktrees(ctx context.Context, git worktree.Git, root, base string, ids []string, sha string, step func(string)) (Worktrees, error) {
	// Canonical, because git records a worktree at its real path. On macOS
	// the temp directory is under /var, a link to /private/var, and a path
	// recorded as given would never match what `git worktree list` prints.
	if real, err := filepath.EvalSymlinks(base); err == nil {
		base = real
	}
	made := Worktrees{root: root}
	for n, id := range ids {
		step(fmt.Sprintf("materializing worktree %d of %d", n+1, len(ids)))
		if ctx.Err() != nil {
			return made, fmt.Errorf("interrupted while creating worktrees: %w", ctx.Err())
		}
		dir := filepath.Join(base, "worktree-"+id)
		// Recorded before the add, not after: git registers a worktree
		// partway through, and a failure after that would leave one in the
		// person's repository that nothing knew to remove.
		made.Dirs = append(made.Dirs, dir)
		if _, err := git(ctx, root, "worktree", "add", "--detach", "--quiet", dir, sha); err != nil {
			return made, fmt.Errorf("could not create a worktree for '%s': %s", id, sanitize.Line(err.Error()))
		}
	}
	return made, nil
}

// Remove takes every worktree out of the person's repository. It is quiet
// and best effort: the run has said what it had to say, and a worktree that
// will not go is not worth a second error on top of the first. --force,
// because a panelist may have left edits: that is what its worktree was for.
// --force a second time, because an add that a ctrl-C killed leaves its
// worktree locked, and git removes a locked worktree for nothing less.
// Every path here is one this run made, so no lock on it is a person's.
func (w Worktrees) Remove(git worktree.Git) {
	if len(w.Dirs) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	leaked := false
	for _, dir := range w.Dirs {
		// A refused remove is not proof of a leak: an add can fail before
		// it registers anything, and every path is recorded before its add.
		if _, err := git(ctx, w.root, "worktree", "remove", "--force", "--force", dir); err != nil {
			leaked = leaked || stillRegistered(ctx, git, w.root, dir)
		}
	}
	// Only for a registration git still lists after it refused to remove
	// it. `--expire now` sweeps every registration whose directory is
	// missing right now, the person's own worktree on an unmounted volume
	// too, so it runs in the one case it exists for. Plain prune honors
	// gc.worktreePruneExpire, three months by default, and would leave the
	// very registration it is here to clear.
	if leaked {
		_, _ = git(ctx, w.root, "worktree", "prune", "--expire", "now")
	}
}

// stillRegistered says git still lists dir as a worktree.
func stillRegistered(ctx context.Context, git worktree.Git, root, dir string) bool {
	out, err := git(ctx, root, "worktree", "list", "--porcelain")
	if err != nil {
		return false
	}
	for line := range strings.Lines(string(out)) {
		if strings.TrimSuffix(line, "\n") == "worktree "+dir {
			return true
		}
	}
	return false
}
