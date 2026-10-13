package worktree

import (
	"context"
	"fmt"
)

// Finish removes the worktree when the task left nothing behind, and keeps
// it when it did: uncommitted changes, or commits it never pushed. A
// worktree is never removed on a guess, so when git cannot say, it is kept
// too.
//
// The bool is true when the worktree is kept. An error means git failed;
// the worktree is then kept, and Kept.Why holds the same sentence, so a
// caller that only prints the note still says why.
//
// The branch goes with the worktree only when this run made it, and with
// -d, not -D: -d refuses a branch that is not fully pushed, which the
// checks before it already ruled out, so a refusal there loses nothing.
func Finish(ctx context.Context, git Git, root string, p Prepared) (Kept, bool, error) {
	// --untracked-files is spelled out. With status.showUntrackedFiles=no
	// in the person's config, a plain status lists no new file, and a file
	// the task made and did not commit would go with the worktree.
	changes, err := run(ctx, git, p.Path, "status", "--porcelain", "--untracked-files=normal")
	if err != nil {
		return keptOnError(p, "git could not read it", err)
	}
	if changes != "" {
		return Kept{Path: p.Path, Why: WhyUncommitted}, true, nil
	}
	ahead, err := run(ctx, git, p.Path, "rev-list", "--count", "@{upstream}..HEAD")
	if err != nil {
		return keptOnError(p, "git could not compare it with origin", err)
	}
	if ahead != "0" {
		return Kept{Path: p.Path, Why: WhyUnpushed}, true, nil
	}
	if _, err := run(ctx, git, root, "worktree", "remove", p.Path); err != nil {
		return keptOnError(p, "git could not remove it", err)
	}
	if p.CreatedBranch {
		_, _ = git(ctx, root, "branch", "--quiet", "-d", p.Branch)
	}
	return Kept{}, false, nil
}

func keptOnError(p Prepared, what string, err error) (Kept, bool, error) {
	why := fmt.Sprintf("%s: %v", what, err)
	return Kept{Path: p.Path, Why: why}, true, fmt.Errorf("%s: %w", what, err)
}
