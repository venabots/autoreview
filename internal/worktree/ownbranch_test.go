package worktree

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// pushAsTheTask runs the plain `git push` the skills run, with the
// environment the task process gets.
func pushAsTheTask(t *testing.T, p Prepared) {
	t.Helper()
	cmd := exec.Command("git", "-C", p.Path, "push", "--quiet")
	cmd.Env = append(os.Environ(), p.Env()...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git push: %v: %s", err, out)
	}
}

// wantOwnBranch checks a task that could not use your branch: it runs on a
// branch of its own that tracks the PR's, its push lands on the PR, your
// checkout does not move, and Finish deletes only its own branch.
func wantOwnBranch(t *testing.T, f fixture, yours string) {
	t.Helper()
	p := prepareOK(t, f.request("me/work"))
	own := "autoreview/run-test/pr-4"
	if p.Branch != own || !p.CreatedBranch || !p.OwnBranch {
		t.Errorf("Prepared = %+v, want a branch of its own, %s", p, own)
	}
	if got := mustGit(t, p.Path, "rev-parse", "--abbrev-ref", "HEAD"); got != own {
		t.Errorf("checked out %q, want %s", got, own)
	}
	if got := mustGit(t, p.Path, "rev-parse", "--abbrev-ref", "@{upstream}"); got != "origin/me/work" {
		t.Errorf("tracks %q, want origin/me/work", got)
	}
	if p.Head != f.originHead(t, "me/work") {
		t.Errorf("Head = %s, want origin's %s", p.Head, f.originHead(t, "me/work"))
	}
	before := mustGit(t, f.clone, "rev-parse", "refs/heads/me/work")
	checkedOut := mustGit(t, yours, "rev-parse", "--abbrev-ref", "HEAD")

	mustGit(t, p.Path, "commit", "--quiet", "--allow-empty", "-m", "fix")
	pushAsTheTask(t, p)
	if got, want := mustGit(t, p.Path, "rev-parse", "HEAD"), f.originHead(t, "me/work"); got != want {
		t.Errorf("a plain push did not land on the PR's branch: %s, origin %s", got, want)
	}
	if got := mustGit(t, f.clone, "rev-parse", "refs/heads/me/work"); got != before {
		t.Errorf("your branch moved from %s to %s", before, got)
	}
	if got := mustGit(t, yours, "rev-parse", "--abbrev-ref", "HEAD"); got != checkedOut {
		t.Errorf("your checkout moved to %q", got)
	}

	if kept, isKept, err := Finish(context.Background(), Exec, f.clone, p); err != nil || isKept {
		t.Fatalf("Finish = %+v, %v, %v; want it removed", kept, isKept, err)
	}
	gone(t, p.Path)
	if got := mustGit(t, f.clone, "branch", "--list", own); got != "" {
		t.Errorf("the branch it made is still there: %q", got)
	}
	if got := mustGit(t, f.clone, "branch", "--list", "me/work"); got == "" {
		t.Error("your branch was deleted")
	}
}

// A branch that tracks somewhere else would push the task's work there,
// also from a worktree of yours that is clean.
func TestABranchCheckedOutElsewhereThatTracksSomewhereElseGetsTheTaskABranchOfItsOwn(t *testing.T) {
	f := setup(t)
	yours := filepath.Join(f.base, "clone.me-work")
	mustGit(t, f.clone, "worktree", "add", "--quiet", "--track", yours, "-b", "me/work", "origin/main")
	wantOwnBranch(t, f, yours)
}

func TestALocalBranchThatTracksSomewhereElseGetsTheTaskABranchOfItsOwn(t *testing.T) {
	f := setup(t)
	mustGit(t, f.clone, "branch", "--quiet", "--track", "me/work", "origin/main")
	wantOwnBranch(t, f, f.clone)
	if got := mustGit(t, f.clone, "rev-parse", "--abbrev-ref", "me/work@{upstream}"); got != "origin/main" {
		t.Errorf("what your branch tracks changed to %q", got)
	}
}

func TestUnpushedCommitsOnABranchCheckedOutElsewhereAreStillRefused(t *testing.T) {
	f := setup(t)
	mustGit(t, f.clone, "switch", "--quiet", "me/work")
	mustGit(t, f.clone, "commit", "--quiet", "--allow-empty", "-m", "mine, not pushed")
	e := prepareErr(t, f.request("me/work"))
	if want := "your local branch me/work has commits that are not on origin; push or move them first"; e != want {
		t.Errorf("error = %q, want %q", e, want)
	}
	gone(t, PathFor(f.runRoot, 4))
}

func TestOnlyABranchOfItsOwnAddsToTheTasksEnvironment(t *testing.T) {
	if env := (Prepared{Branch: "me/work"}).Env(); env != nil {
		t.Errorf("Env = %v for the PR's own branch, want nil", env)
	}
	want := []string{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=push.default", "GIT_CONFIG_VALUE_0=upstream"}
	if env := (Prepared{Branch: "autoreview/run-1/pr-4", OwnBranch: true}).Env(); !slices.Equal(env, want) {
		t.Errorf("Env = %v, want %v", env, want)
	}
}

func TestTheOwnBranchIsNamedForTheRunAndThePR(t *testing.T) {
	got := ownBranch(Request{Path: filepath.Join("/logs", "run-EZd7PG", "worktrees", "pr-2423"), PR: 2423})
	if want := "autoreview/run-EZd7PG/pr-2423"; got != want {
		t.Errorf("ownBranch = %q, want %q", got, want)
	}
	if strings.ContainsAny(got, " ~^:?*[\\") {
		t.Errorf("%q is not a name git takes", got)
	}
}

func TestABranchOfItsOwnIsMadeFromOriginAndUndoneWithItsOwnName(t *testing.T) {
	elsewhere := t.TempDir()
	own := "autoreview/run-1/pr-4"
	add := "worktree add --quiet --track -b " + own + " " + wt + " origin/me/work"
	cases := []struct {
		name    string
		answers map[string]answer
		want    []call
	}{
		{"checked out in another worktree that has changes", map[string]answer{
			"worktree list --porcelain":                   {out: "worktree " + elsewhere + "\nbranch refs/heads/me/work\n"},
			"status --porcelain --untracked-files=normal": {out: "?? draft.txt\n"},
			"rev-parse HEAD":                              {out: "abc123\n"},
		}, []call{
			{root, "worktree list --porcelain"},
			{root, "rev-parse --verify --quiet refs/heads/me/work"},
			{root, "merge-base --is-ancestor refs/heads/me/work origin/me/work"},
			{elsewhere, "status --porcelain --untracked-files=normal"},
			{root, "rev-parse --abbrev-ref me/work@{upstream}"},
			{root, add},
			{wt, "rev-parse HEAD"},
		}},
		{"tracking somewhere else", map[string]answer{
			"rev-parse --abbrev-ref me/work@{upstream}": {out: "origin/main\n"},
			"rev-parse HEAD": {out: "abc123\n"},
		}, []call{
			{root, "worktree list --porcelain"},
			{root, "rev-parse --verify --quiet refs/heads/me/work"},
			{root, "merge-base --is-ancestor refs/heads/me/work origin/me/work"},
			{root, "rev-parse --abbrev-ref me/work@{upstream}"},
			{root, add},
			{wt, "rev-parse HEAD"},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := &recorder{answers: c.answers}
			p, err := Prepare(context.Background(), r.git, fakeRequest())
			if err != nil {
				t.Fatalf("Prepare: %v", err)
			}
			want := Prepared{Path: wt, Branch: own, Head: "abc123", CreatedBranch: true, OwnBranch: true}
			if p != want {
				t.Errorf("Prepared = %+v, want %+v", p, want)
			}
			if got := r.seen()[1:]; !slices.Equal(got, c.want) {
				t.Errorf("git calls after the fetch:\n got %v\nwant %v", got, c.want)
			}
		})
	}
}

func TestAFailedOwnBranchIsUndoneAndYourBranchStays(t *testing.T) {
	own := "autoreview/run-1/pr-4"
	r := &recorder{answers: map[string]answer{
		"rev-parse --abbrev-ref me/work@{upstream}": {out: "origin/main\n"},
		"rev-parse HEAD": {err: errNo},
	}}
	if _, err := Prepare(context.Background(), r.git, fakeRequest()); err == nil {
		t.Fatal("Prepare made a worktree whose head it could not read")
	}
	undo := []call{
		{root, "worktree remove --force " + wt},
		{root, "merge-base --is-ancestor " + own + " origin/me/work"},
		{root, "branch --quiet -D " + own},
	}
	if seen := r.seen(); !slices.Equal(seen[len(seen)-3:], undo) {
		t.Errorf("undo = %v, want %v", seen[len(seen)-3:], undo)
	}
}
