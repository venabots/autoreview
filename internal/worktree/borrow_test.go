package worktree

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// wantBorrowed checks a task that runs in your own clean worktree of the
// PR's branch: the worktree is brought up to origin and tracks the PR's
// branch, a plain push lands on the PR, and Finish leaves the worktree and
// the branch where they are.
func wantBorrowed(t *testing.T, f fixture, yours string) {
	t.Helper()
	p := prepareOK(t, f.request("me/work"))
	want := Prepared{Path: yours, Branch: "me/work", Head: f.originHead(t, "me/work"), Borrowed: true}
	if p != want {
		t.Errorf("Prepared = %+v, want %+v", p, want)
	}
	if p.Env() != nil {
		t.Errorf("Env = %v, want nil: the branch has the PR's name", p.Env())
	}
	gone(t, PathFor(f.runRoot, 4))
	if got := mustGit(t, yours, "rev-parse", "HEAD"); got != want.Head {
		t.Errorf("your worktree is at %s, want origin's %s", got, want.Head)
	}
	if got := mustGit(t, yours, "rev-parse", "--abbrev-ref", "@{upstream}"); got != "origin/me/work" {
		t.Errorf("tracks %q, want origin/me/work", got)
	}

	mustGit(t, yours, "commit", "--quiet", "--allow-empty", "-m", "fix")
	mustGit(t, yours, "push", "--quiet")
	if got, want := mustGit(t, yours, "rev-parse", "HEAD"), f.originHead(t, "me/work"); got != want {
		t.Errorf("a plain push did not land on the PR's branch: %s, origin %s", got, want)
	}

	if kept, isKept, err := Finish(context.Background(), Exec, f.clone, p); err != nil || isKept {
		t.Fatalf("Finish = %+v, %v, %v; want nothing kept", kept, isKept, err)
	}
	if _, err := os.Stat(yours); err != nil {
		t.Errorf("your worktree is gone: %v", err)
	}
	if got := mustGit(t, f.clone, "branch", "--list", "me/work"); got == "" {
		t.Error("your branch was deleted")
	}
}

func TestATaskRunsInYourCleanWorktreeOfThePRsBranch(t *testing.T) {
	cases := []struct {
		name string
		make func(t *testing.T, f fixture) string
	}{
		{"your main checkout", func(t *testing.T, f fixture) string {
			mustGit(t, f.clone, "switch", "--quiet", "me/work")
			return f.clone
		}},
		{"another worktree of yours", func(t *testing.T, f fixture) string {
			yours := filepath.Join(f.base, "clone.me-work")
			mustGit(t, f.clone, "worktree", "add", "--quiet", yours, "-b", "me/work", "origin/me/work")
			return yours
		}},
		{"a worktree behind origin that tracks nothing", func(t *testing.T, f fixture) string {
			yours := filepath.Join(f.base, "clone.me-work")
			mustGit(t, f.clone, "worktree", "add", "--quiet", "--no-track", yours, "-b", "me/work", "origin/main")
			return yours
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := setup(t)
			wantBorrowed(t, f, c.make(t, f))
		})
	}
}

// A task commits what it finds. In a worktree with changes of yours, it
// would commit your work in progress with its own.
func TestAWorktreeWithChangesOfYoursIsNotBorrowed(t *testing.T) {
	f := setup(t)
	yours := filepath.Join(f.base, "clone.me-work")
	mustGit(t, f.clone, "worktree", "add", "--quiet", yours, "-b", "me/work", "origin/me/work")
	draft := filepath.Join(yours, "draft.txt")
	if err := os.WriteFile(draft, []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	wantOwnBranch(t, f, yours)
	if _, err := os.Stat(draft); err != nil {
		t.Errorf("your new file is gone: %v", err)
	}
}

func TestABorrowedWorktreeThatTheTaskLeftWorkInIsNamedAndNeverRemoved(t *testing.T) {
	cases := []struct {
		name  string
		leave func(t *testing.T, dir string)
		why   string
	}{
		{"uncommitted changes", func(t *testing.T, dir string) {
			if err := os.WriteFile(filepath.Join(dir, "half.txt"), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			mustGit(t, dir, "add", "half.txt")
		}, WhyUncommitted},
		{"a commit it did not push", func(t *testing.T, dir string) {
			mustGit(t, dir, "commit", "--quiet", "--allow-empty", "-m", "not pushed")
		}, WhyUnpushed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := setup(t)
			mustGit(t, f.clone, "switch", "--quiet", "me/work")
			p := prepareOK(t, f.request("me/work"))
			c.leave(t, p.Path)
			kept, isKept, err := Finish(context.Background(), Exec, f.clone, p)
			if err != nil || !isKept || kept != (Kept{Path: f.clone, Why: c.why}) {
				t.Errorf("Finish = %+v, %v, %v; want it kept for %s", kept, isKept, err, c.why)
			}
			if _, err := os.Stat(f.clone); err != nil {
				t.Errorf("your checkout is gone: %v", err)
			}
		})
	}
}

func TestBorrowingRunsTheseGitStepsInYourWorktree(t *testing.T) {
	yours := t.TempDir()
	r := &recorder{answers: map[string]answer{
		"worktree list --porcelain":                 {out: "worktree " + yours + "\nbranch refs/heads/me/work\n"},
		"rev-parse --abbrev-ref me/work@{upstream}": {err: errNo},
		"rev-parse HEAD":                            {out: "abc123\n"},
	}}
	p, err := Prepare(context.Background(), r.git, fakeRequest())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if want := (Prepared{Path: yours, Branch: "me/work", Head: "abc123", Borrowed: true}); p != want {
		t.Errorf("Prepared = %+v, want %+v", p, want)
	}
	want := []call{
		{root, "worktree list --porcelain"},
		{root, "rev-parse --verify --quiet refs/heads/me/work"},
		{root, "merge-base --is-ancestor refs/heads/me/work origin/me/work"},
		{yours, "status --porcelain --untracked-files=normal"},
		{root, "rev-parse --abbrev-ref me/work@{upstream}"},
		{yours, "merge --quiet --ff-only origin/me/work"},
		{yours, "branch --quiet --set-upstream-to origin/me/work"},
		{yours, "rev-parse HEAD"},
	}
	if got := r.seen()[1:]; !slices.Equal(got, want) {
		t.Errorf("git calls after the fetch:\n got %v\nwant %v", got, want)
	}
}

func TestFinishNeverRemovesABorrowedWorktree(t *testing.T) {
	yours := t.TempDir()
	r := &recorder{answers: map[string]answer{"rev-list --count @{upstream}..HEAD": {out: "0\n"}}}
	p := Prepared{Path: yours, Branch: "me/work", Head: "abc123", Borrowed: true}
	if kept, isKept, err := Finish(context.Background(), r.git, root, p); err != nil || isKept {
		t.Fatalf("Finish = %+v, %v, %v", kept, isKept, err)
	}
	for _, c := range r.seen() {
		if c.args == "worktree remove "+yours || c.args == "branch --quiet -d me/work" {
			t.Errorf("Finish ran %v on a worktree of yours", c)
		}
	}
}
