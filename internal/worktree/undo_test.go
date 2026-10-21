package worktree

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// lockedConfigDuringAdd is git with another writer holding .git/config
// while the worktree is added: git makes the branch, then cannot record
// what it tracks.
func lockedConfigDuringAdd(t *testing.T, clone string) Git {
	t.Helper()
	lock := filepath.Join(clone, ".git", "config.lock")
	return func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		if len(args) < 2 || args[0] != "worktree" || args[1] != "add" {
			return Exec(ctx, dir, args...)
		}
		if err := os.WriteFile(lock, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = os.Remove(lock) }()
		return Exec(ctx, dir, args...)
	}
}

// PR #2931: the add made the branch and failed to track origin. Without
// the tracking, git branch -d compared the branch with your HEAD and kept
// it, and every later task on the PR was refused: "a branch named
// 'autoreview/run-1977872589/pr-2931' already exists".
func TestAnAddThatFailsAfterMakingTheBranchTakesTheBranchBack(t *testing.T) {
	f := setup(t)
	mustGit(t, f.clone, "branch", "--quiet", "--track", "me/work", "origin/main")
	r := f.request("me/work")
	if _, err := Prepare(context.Background(), lockedConfigDuringAdd(t, f.clone), r); err == nil {
		t.Fatal("Prepare made a worktree while .git/config was locked")
	}
	if got := mustGit(t, f.clone, "branch", "--list", ownBranch(r)); got != "" {
		t.Errorf("the branch it made is still there: %q", got)
	}
	if next := prepareOK(t, r); !next.OwnBranch {
		t.Errorf("Prepared = %+v, want a branch of its own", next)
	}
}
