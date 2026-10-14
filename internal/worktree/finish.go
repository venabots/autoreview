package worktree

import (
	"context"
	"fmt"
)

// Finish removes the worktree when the task left no work behind, and keeps
// it when it did: changes to tracked files, or commits it never pushed. A
// file the task left untracked is not work (see heldWork). A worktree is
// never removed on a guess, so when git cannot say, it is kept too.
//
// A borrowed worktree is yours, so it always stays, and so does its branch.
// What the task left in it is still named, so you learn of it.
//
// The bool is true when the worktree is kept. An error means git failed;
// the worktree is then kept, and Kept.Why holds the same sentence, so a
// caller that only prints the note still says why.
//
// The branch goes with the worktree only when this run made it, and with
// -d, not -D: -d refuses a branch that is not fully pushed, which the
// checks before it already ruled out, so a refusal there loses nothing.
func Finish(ctx context.Context, git Git, root string, p Prepared) (Kept, bool, error) {
	why, err := heldWork(ctx, git, p.Path)
	if err != nil {
		return keptOnError(p, err)
	}
	if why != "" {
		return Kept{Path: p.Path, Why: why}, true, nil
	}
	if p.Borrowed {
		return Kept{}, false, nil
	}
	if err := removeTree(ctx, git, root, p.Path); err != nil {
		return keptOnError(p, fmt.Errorf("git could not remove it: %w", err))
	}
	if p.CreatedBranch {
		_, _ = git(ctx, root, "branch", "--quiet", "-d", p.Branch)
	}
	return Kept{}, false, nil
}

func keptOnError(p Prepared, err error) (Kept, bool, error) {
	return Kept{Path: p.Path, Why: err.Error()}, true, err
}
