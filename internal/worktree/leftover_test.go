package worktree

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pycache is what a test run leaves in a repo that does not ignore it: a
// file nobody wrote and nobody will commit.
func pycache(t *testing.T, dir string) string {
	t.Helper()
	cache := filepath.Join(dir, "__pycache__")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(cache, "scripts.cpython-314.pyc")
	if err := os.WriteFile(file, []byte("bytecode"), 0o600); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestAnUntrackedFileAloneDoesNotKeepTheWorktree(t *testing.T) {
	f := setup(t)
	p := prepareOK(t, f.request("me/work"))
	pycache(t, p.Path)
	if kept, isKept, err := Finish(context.Background(), Exec, f.clone, p); err != nil || isKept {
		t.Fatalf("Finish = %+v, %v, %v; want it removed", kept, isKept, err)
	}
	gone(t, p.Path)
}

func TestAnEarlierTasksWorktreeThatHoldsNoWorkMakesWayForTheNextTask(t *testing.T) {
	f := setup(t)
	first := prepareOK(t, f.request("me/work"))
	mustGit(t, first.Path, "commit", "--quiet", "--allow-empty", "-m", "fix")
	mustGit(t, first.Path, "push", "--quiet")
	cache := pycache(t, first.Path)

	next := prepareOK(t, f.request("me/work"))
	if next.Path != first.Path {
		t.Errorf("Path = %q, want %q", next.Path, first.Path)
	}
	if got, want := mustGit(t, next.Path, "rev-parse", "HEAD"), f.originHead(t, "me/work"); got != want {
		t.Errorf("HEAD = %s, want origin's %s", got, want)
	}
	if _, err := os.Stat(cache); err == nil {
		t.Error("the earlier task's untracked file is still there")
	}
}

// PR #2660: a fix on a branch of its own pushed, left a __pycache__ file,
// and every later fix on the PR was refused for the rest of the run.
func TestAnEarlierTasksOwnBranchGoesWithItsWorktree(t *testing.T) {
	f := setup(t)
	mustGit(t, f.clone, "branch", "--quiet", "--track", "me/work", "origin/me/work")
	yours := filepath.Join(f.base, "yours")
	mustGit(t, f.clone, "worktree", "add", "--quiet", yours, "me/work")
	if err := os.WriteFile(filepath.Join(yours, "draft.txt"), []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	first := prepareOK(t, f.request("me/work"))
	if !first.OwnBranch {
		t.Fatalf("Prepared = %+v, want a branch of its own", first)
	}
	pycache(t, first.Path)

	next := prepareOK(t, f.request("me/work"))
	if next.Branch != first.Branch || !next.OwnBranch {
		t.Errorf("Prepared = %+v, want the branch %s again", next, first.Branch)
	}
}

func TestAnEarlierTasksWorktreeThatHoldsWorkIsKeptAndNamed(t *testing.T) {
	cases := []struct {
		name  string
		leave func(t *testing.T, dir string)
		why   string
	}{
		{"a staged file", func(t *testing.T, dir string) {
			if err := os.WriteFile(filepath.Join(dir, "half.txt"), []byte("edit"), 0o600); err != nil {
				t.Fatal(err)
			}
			mustGit(t, dir, "add", "half.txt")
		}, WhyUncommitted},
		{"a commit not pushed", func(t *testing.T, dir string) {
			mustGit(t, dir, "commit", "--quiet", "--allow-empty", "-m", "not pushed")
		}, WhyUnpushed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := setup(t)
			r := f.request("me/work")
			first := prepareOK(t, r)
			c.leave(t, first.Path)
			want := "PR #4's worktree from an earlier task has " + c.why + ": " + first.Path
			if got := prepareErr(t, r); got != want {
				t.Errorf("error = %q, want %q", got, want)
			}
			if _, err := os.Stat(first.Path); err != nil {
				t.Errorf("the worktree with work in it is gone: %v", err)
			}
		})
	}
}

func TestAnEarlierTasksPathThatGitCannotReadIsRefused(t *testing.T) {
	f := setup(t)
	r := f.request("me/work")
	if err := os.MkdirAll(r.Path, 0o755); err != nil {
		t.Fatal(err)
	}
	want := "PR #4's worktree from an earlier task is still at " + r.Path + ": "
	if got := prepareErr(t, r); !strings.HasPrefix(got, want) {
		t.Errorf("error = %q, want it to start %q", got, want)
	}
	if _, err := os.Stat(r.Path); err != nil {
		t.Errorf("the path was removed: %v", err)
	}
}
