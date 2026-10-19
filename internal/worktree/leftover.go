package worktree

import (
	"context"
	"fmt"
)

// heldWork is the work a task worktree holds that removing it would lose:
// WhyUncommitted, WhyUnpushed, or empty for none. When git cannot say, the
// error says so, and the caller keeps the worktree.
//
// An untracked file is not work. A task commits what it means to ship, and
// what it leaves untracked is what its tests and builds wrote, such as a
// __pycache__ in a repo that does not ignore it. Counted as work, such a
// file kept the worktree, and the next task on the PR was refused for the
// rest of the run.
func heldWork(ctx context.Context, git Git, path string) (string, error) {
	changes, err := run(ctx, git, path, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return "", fmt.Errorf("git could not read it: %w", err)
	}
	if changes != "" {
		return WhyUncommitted, nil
	}
	ahead, err := run(ctx, git, path, "rev-list", "--count", "@{upstream}..HEAD")
	if err != nil {
		return "", fmt.Errorf("git could not compare it with origin: %w", err)
	}
	if ahead != "0" {
		return WhyUnpushed, nil
	}
	return "", nil
}

// removeTree removes a task worktree that holds no work. git refuses a
// worktree with untracked files unless forced, and heldWork does not count
// those. Only a worktree this tool made is forced: one of yours never is.
func removeTree(ctx context.Context, git Git, root, path string) error {
	if madeHere(path) {
		_, err := run(ctx, git, root, "worktree", "remove", "--force", path)
		return err
	}
	_, err := run(ctx, git, root, "worktree", "remove", path)
	return err
}

// clearLeftover removes the worktree an earlier task on the PR left at
// r.Path, when it holds no work, so the next task can run there. A task
// keeps its worktree when Finish cannot remove it, and without this every
// later task on the PR would be refused until the run ends.
//
// A leftover that holds work stays, and the refusal names the work. So does
// a path this tool did not make, and one git cannot read. The branch goes
// with the worktree only when it is the run's own branch for the PR: the
// next task makes that branch again, and git refuses to make one that is
// there already.
func clearLeftover(ctx context.Context, git Git, r Request) error {
	if !exists(r.Path) {
		return nil
	}
	if r.Keep {
		return fmt.Errorf("PR #%d's worktree at %s is open to resume its session; remove it to run a task on the PR", r.PR, r.Path)
	}
	still := fmt.Sprintf("PR #%d's worktree from an earlier task is still at %s", r.PR, r.Path)
	if !madeHere(r.Path) {
		return fmt.Errorf("%s", still)
	}
	why, err := heldWork(ctx, git, r.Path)
	if err != nil {
		return fmt.Errorf("%s: %w", still, err)
	}
	if why != "" {
		return fmt.Errorf("PR #%d's worktree from an earlier task has %s: %s", r.PR, why, r.Path)
	}
	branch, err := run(ctx, git, r.Path, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return fmt.Errorf("%s: git could not read its branch: %w", still, err)
	}
	if err := removeTree(ctx, git, r.Root, r.Path); err != nil {
		return fmt.Errorf("%s: git could not remove it: %w", still, err)
	}
	if branch == ownBranch(r) {
		_, _ = git(ctx, r.Root, "branch", "--quiet", "-d", branch)
	}
	return nil
}
