package worktree

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestATaskGetsThePRBranchTrackingOriginInAWorktreeOfItsOwn(t *testing.T) {
	f := setup(t)
	p := prepareOK(t, f.request("me/work"))
	if want := filepath.Join(f.runRoot, "worktrees", "pr-4"); p.Path != want {
		t.Errorf("Path = %q, want %q", p.Path, want)
	}
	if got := mustGit(t, p.Path, "rev-parse", "--abbrev-ref", "HEAD"); got != "me/work" {
		t.Errorf("checked out %q, want me/work", got)
	}
	if got := mustGit(t, p.Path, "rev-parse", "--abbrev-ref", "@{upstream}"); got != "origin/me/work" {
		t.Errorf("tracks %q, want origin/me/work", got)
	}
	head := f.originHead(t, "me/work")
	if got := mustGit(t, p.Path, "rev-parse", "HEAD"); got != head {
		t.Errorf("HEAD = %s, want origin's %s", got, head)
	}
	if p.Head != head {
		t.Errorf("Head = %s, want %s: the head the readback compares with", p.Head, head)
	}
	if !p.CreatedBranch || p.Branch != "me/work" {
		t.Errorf("Prepared = %+v, want the branch it made", p)
	}
	if got := mustGit(t, f.clone, "rev-parse", "--abbrev-ref", "HEAD"); got != "main" {
		t.Errorf("your checkout moved to %q", got)
	}

	mustGit(t, p.Path, "commit", "--quiet", "--allow-empty", "-m", "fix")
	mustGit(t, p.Path, "push", "--quiet")
	if got, want := mustGit(t, p.Path, "rev-parse", "HEAD"), f.originHead(t, "me/work"); got != want {
		t.Errorf("a plain push did not land on the PR's branch: %s, origin %s", got, want)
	}

	kept, isKept, err := Finish(context.Background(), Exec, f.clone, p)
	if err != nil || isKept {
		t.Fatalf("Finish = %+v, %v, %v; want it removed", kept, isKept, err)
	}
	gone(t, p.Path)
	if got := mustGit(t, f.clone, "branch", "--list", "me/work"); got != "" {
		t.Errorf("the branch it made is still there: %q", got)
	}
}

func TestALocalBranchWithUnpushedCommitsIsRefused(t *testing.T) {
	f := setup(t)
	mustGit(t, f.clone, "branch", "--quiet", "me/work", "origin/me/work")
	side := filepath.Join(f.base, "side")
	mustGit(t, f.clone, "worktree", "add", "--quiet", side, "me/work")
	mustGit(t, side, "commit", "--quiet", "--allow-empty", "-m", "mine, not pushed")
	mustGit(t, f.clone, "worktree", "remove", side)
	e := prepareErr(t, f.request("me/work"))
	if want := "your local branch me/work has commits that are not on origin; push or move them first"; e != want {
		t.Errorf("error = %q, want %q", e, want)
	}
}

func TestALocalBranchBehindOriginIsBroughtUpAndKeptAfterwards(t *testing.T) {
	f := setup(t)
	mustGit(t, f.clone, "branch", "--quiet", "me/work", "main")
	p := prepareOK(t, f.request("me/work"))
	if got, want := mustGit(t, p.Path, "rev-parse", "HEAD"), f.originHead(t, "me/work"); got != want {
		t.Errorf("HEAD = %s, want origin's %s", got, want)
	}
	if got := mustGit(t, p.Path, "rev-parse", "--abbrev-ref", "@{upstream}"); got != "origin/me/work" {
		t.Errorf("an untracked branch of yours tracks %q, want origin/me/work", got)
	}
	if p.CreatedBranch {
		t.Error("Prepare says it made a branch that was yours")
	}
	if _, isKept, err := Finish(context.Background(), Exec, f.clone, p); err != nil || isKept {
		t.Fatalf("Finish kept it: %v, %v", isKept, err)
	}
	if got := mustGit(t, f.clone, "branch", "--list", "me/work"); got == "" {
		t.Error("a branch that was yours was deleted")
	}
}

func TestAMissingWorktreeThatIsNotOursIsRefused(t *testing.T) {
	f := setup(t)
	missing := filepath.Join(f.base, "gone")
	mustGit(t, f.clone, "worktree", "add", "--quiet", missing, "-b", "me/work", "origin/me/work")
	if err := os.Rename(missing, filepath.Join(f.base, "moved-away")); err != nil {
		t.Fatal(err)
	}
	e := prepareErr(t, f.request("me/work"))
	if !strings.HasSuffix(e, "which is not there; if it is gone for good, run git worktree prune") {
		t.Errorf("error = %q, want the prune advice: it may be on a disk that is not mounted", e)
	}
}

// Some people set status.showUntrackedFiles=no in a large repo. A plain
// `git status` then lists no new file, and `git worktree remove` asks the
// same question the same way. A file the task made and did not commit would
// go with the worktree.
func TestANewFileKeepsTheWorktreeWhenGitIsToldToHideUntrackedFiles(t *testing.T) {
	f := setup(t)
	mustGit(t, f.clone, "config", "status.showUntrackedFiles", "no")
	p := prepareOK(t, f.request("me/work"))
	if err := os.WriteFile(filepath.Join(p.Path, "half.txt"), []byte("edit"), 0o600); err != nil {
		t.Fatal(err)
	}
	want := Kept{Path: p.Path, Why: "uncommitted changes"}
	if kept, isKept, err := Finish(context.Background(), Exec, f.clone, p); kept != want || !isKept || err != nil {
		t.Errorf("Finish = %+v, %v, %v; want %+v kept", kept, isKept, err, want)
	}
	if _, err := os.Stat(filepath.Join(p.Path, "half.txt")); err != nil {
		t.Errorf("the new file is gone: %v", err)
	}
}

func TestWorkLeftBehindKeepsTheWorktree(t *testing.T) {
	f := setup(t)
	p := prepareOK(t, f.request("me/work"))
	if err := os.WriteFile(filepath.Join(p.Path, "half.txt"), []byte("edit"), 0o600); err != nil {
		t.Fatal(err)
	}
	want := Kept{Path: p.Path, Why: "uncommitted changes"}
	if kept, isKept, err := Finish(context.Background(), Exec, f.clone, p); kept != want || !isKept || err != nil {
		t.Errorf("Finish = %+v, %v, %v; want %+v kept", kept, isKept, err, want)
	}

	mustGit(t, p.Path, "add", "half.txt")
	mustGit(t, p.Path, "commit", "--quiet", "-m", "done, not pushed")
	want = Kept{Path: p.Path, Why: "commits that are not pushed"}
	kept, isKept, err := Finish(context.Background(), Exec, f.clone, p)
	if kept != want || !isKept || err != nil {
		t.Errorf("Finish = %+v, %v, %v; want %+v kept", kept, isKept, err, want)
	}
	if _, err := os.Stat(p.Path); err != nil {
		t.Errorf("the worktree with work in it is gone: %v", err)
	}
	if got, want := Note(4, kept), "note: PR #4's worktree has commits that are not pushed: "+p.Path; got != want {
		t.Errorf("Note = %q, want %q", got, want)
	}
}

func TestOnlyAStaleWorktreeThisToolMadeIsCleared(t *testing.T) {
	f := setup(t)
	mine := filepath.Join(f.base, "run-old", "worktrees", "pr-9")
	mustGit(t, f.clone, "worktree", "add", "--quiet", mine, "-b", "me/work", "origin/me/work")
	if err := os.RemoveAll(mine); err != nil {
		t.Fatal(err)
	}
	mustGit(t, f.clone, "branch", "--quiet", "--unset-upstream", "me/work")
	p := prepareOK(t, f.request("me/work"))
	if strings.Contains(mustGit(t, f.clone, "worktree", "list", "--porcelain"), mine) {
		t.Error("the stale record of an old task worktree is still there")
	}
	if p.CreatedBranch {
		t.Error("the branch the old task left is not this run's to delete")
	}
}

func TestAFailedHookAfterTheWorktreeIsMadeLeavesNothingBehind(t *testing.T) {
	f := setup(t)
	hook := filepath.Join(f.clone, ".git", "hooks", "post-checkout")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	e := prepareErr(t, f.request("me/work"))
	if !strings.HasPrefix(e, "could not make a worktree for PR #4") {
		t.Errorf("error = %q", e)
	}
	gone(t, PathFor(f.runRoot, 4))
	if got := mustGit(t, f.clone, "branch", "--list", "me/work"); got != "" {
		t.Errorf("the branch it made is still there: %q", got)
	}
}

func TestAFailedFetchSaysWhyInGitsWords(t *testing.T) {
	f := setup(t)
	e := prepareErr(t, f.request("me/not-there"))
	said, ok := strings.CutPrefix(e, "could not fetch PR #4's branch me/not-there: ")
	if !ok {
		t.Fatalf("error = %q", e)
	}
	if !strings.HasPrefix(said, "fatal:") || !strings.Contains(said, "me/not-there") {
		t.Errorf("git's own words, naming the ref it could not find: %q", said)
	}
}

func TestAnEarlierTasksWorktreeStillOnDiskIsRefused(t *testing.T) {
	f := setup(t)
	r := f.request("me/work")
	if err := os.MkdirAll(r.Path, 0o755); err != nil {
		t.Fatal(err)
	}
	if want := "PR #4's worktree from an earlier task is still at " + r.Path; prepareErr(t, r) != want {
		t.Errorf("error = %q, want %q", prepareErr(t, r), want)
	}
}

func TestFinishKeepsAWorktreeGitCannotRead(t *testing.T) {
	f := setup(t)
	p := Prepared{Path: filepath.Join(f.base, "nowhere"), Branch: "me/work", CreatedBranch: true}
	kept, isKept, err := Finish(context.Background(), Exec, f.clone, p)
	if err == nil || !isKept {
		t.Fatalf("Finish = %+v, %v, %v; want it kept with an error", kept, isKept, err)
	}
	if !strings.HasPrefix(kept.Why, "git could not read it: ") || kept.Why != err.Error() {
		t.Errorf("Why = %q, err = %q; want the same sentence in both", kept.Why, err)
	}
}
