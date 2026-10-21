package worktree

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

// PRs #2931 and #2934: two tasks made their worktrees at once, and both
// wrote .git/config. git refused the second: "could not lock config file".
func TestTasksThatStartTogetherAllGetTheirWorktrees(t *testing.T) {
	f := setup(t)
	const prs = 8
	for pr := range prs {
		branch := fmt.Sprintf("me/work-%d", pr)
		mustGit(t, f.clone, "push", "--quiet", "origin", "origin/me/work:refs/heads/"+branch)
		mustGit(t, f.clone, "branch", "--quiet", "--track", branch, "origin/main")
	}
	errs := make([]error, prs)
	var wg sync.WaitGroup
	for pr := range prs {
		wg.Go(func() {
			r := Request{Root: f.clone, Path: PathFor(f.runRoot, pr+1), PR: pr + 1, Branch: fmt.Sprintf("me/work-%d", pr)}
			_, errs[pr] = Prepare(context.Background(), Exec, r)
		})
	}
	wg.Wait()
	for pr, err := range errs {
		if err != nil {
			t.Errorf("PR #%d: %v", pr+1, err)
		}
	}
	if got, err := filepath.Glob(filepath.Join(f.runRoot, "worktrees", "pr-*")); err != nil || len(got) != prs {
		t.Errorf("worktrees = %v, %v; want %d", got, err, prs)
	}
}
