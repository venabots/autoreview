package worktree

import (
	"context"
	"fmt"
)

// Reopen makes a task's worktree again at r.Path after Finish removed it,
// so that the task's session can be resumed. A session belongs to the
// directory it ran in, and in any other directory it is not found.
//
// It is Prepare, with two refusals first. Something at r.Path now is not
// this run's to clear: it may be yours. A branch checked out in a worktree
// of yours would be borrowed, and the session would then open in your
// worktree, where it never ran; the borrow would also move your worktree
// up to origin for nothing.
func Reopen(ctx context.Context, git Git, r Request) (Prepared, error) {
	repoLock.Lock()
	defer repoLock.Unlock()
	if err := refuseBeforeGit(r); err != nil {
		return Prepared{}, err
	}
	if exists(r.Path) {
		return Prepared{}, fmt.Errorf("PR #%d's worktree cannot be made again: something else is at %s now", r.PR, r.Path)
	}
	at, err := checkedOutAt(ctx, git, r.Root, r.Branch)
	if err != nil {
		return Prepared{}, err
	}
	if at != "" {
		return Prepared{}, fmt.Errorf("PR #%d's branch %s is checked out at %s, so its worktree cannot be made again at %s", r.PR, r.Branch, at, r.Path)
	}
	return prepare(ctx, git, r)
}
