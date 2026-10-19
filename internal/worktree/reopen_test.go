package worktree

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func reopenOK(t *testing.T, r Request) Prepared {
	t.Helper()
	p, err := Reopen(context.Background(), Exec, r)
	if err != nil {
		t.Fatalf("Reopen: %v", err)
	}
	return p
}

func reopenErr(t *testing.T, r Request) string {
	t.Helper()
	_, err := Reopen(context.Background(), Exec, r)
	if err == nil {
		t.Fatal("Reopen made a worktree it should have refused")
	}
	return err.Error()
}

// madeAndRemoved runs a task's worktree through to its end, as a task that
// pushed everything leaves it: removed.
func madeAndRemoved(t *testing.T, f fixture) Request {
	t.Helper()
	r := f.request("me/work")
	p := prepareOK(t, r)
	if _, isKept, err := Finish(context.Background(), Exec, f.clone, p); err != nil || isKept {
		t.Fatalf("Finish kept it: %v, %v", isKept, err)
	}
	gone(t, r.Path)
	return r
}

func TestAWorktreeIsMadeAgainAtItsOldPathOnThePRsBranchAtOriginsHead(t *testing.T) {
	f := setup(t)
	r := madeAndRemoved(t, f)
	seed := filepath.Join(f.base, "seed")
	mustGit(t, seed, "commit", "--quiet", "--allow-empty", "-m", "pushed after the task")
	mustGit(t, seed, "push", "--quiet", "origin", "me/work")

	p := reopenOK(t, r)
	if p.Path != r.Path {
		t.Errorf("Path = %q, want the old path %q: a session resumes only where it ran", p.Path, r.Path)
	}
	if got := mustGit(t, p.Path, "rev-parse", "--abbrev-ref", "HEAD"); got != "me/work" {
		t.Errorf("checked out %q, want me/work", got)
	}
	head := mustGit(t, seed, "rev-parse", "HEAD")
	if got := mustGit(t, p.Path, "rev-parse", "HEAD"); got != head || p.Head != head {
		t.Errorf("HEAD = %s, Head = %s, want origin's newest %s", got, p.Head, head)
	}
}

func TestAWorktreeIsNotMadeAgainOverSomethingAtItsPath(t *testing.T) {
	f := setup(t)
	r := madeAndRemoved(t, f)
	if err := os.MkdirAll(r.Path, 0o755); err != nil {
		t.Fatal(err)
	}
	notes := filepath.Join(r.Path, "notes.txt")
	if err := os.WriteFile(notes, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if e, want := reopenErr(t, r), "something else is at "+r.Path+" now"; !strings.Contains(e, want) {
		t.Errorf("error = %q, want it to say %q", e, want)
	}
	if got, err := os.ReadFile(notes); err != nil || string(got) != "mine" {
		t.Errorf("what was at the path changed: %q, %v", got, err)
	}
}

func TestAWorktreeIsNotMadeAgainWhileItsBranchIsCheckedOutElsewhere(t *testing.T) {
	f := setup(t)
	r := madeAndRemoved(t, f)
	mustGit(t, f.clone, "branch", "--quiet", "--track", "me/work", "origin/me/work")
	mustGit(t, f.clone, "reset", "--quiet", "--hard", "HEAD")
	yours := filepath.Join(f.base, "clone.me-work")
	mustGit(t, f.clone, "worktree", "add", "--quiet", yours, "me/work")
	mustGit(t, yours, "reset", "--quiet", "--hard", "HEAD~1")
	before := mustGit(t, yours, "rev-parse", "HEAD")

	want := "PR #4's branch me/work is checked out at " + yours + ", so its worktree cannot be made again at " + r.Path
	if e := reopenErr(t, r); e != want {
		t.Errorf("error = %q, want %q", e, want)
	}
	gone(t, r.Path)
	if got := mustGit(t, yours, "rev-parse", "HEAD"); got != before {
		t.Errorf("your worktree moved from %s to %s", before, got)
	}
}

func TestAWorktreeIsNotMadeAgainForABranchGoneFromOrigin(t *testing.T) {
	f := setup(t)
	r := madeAndRemoved(t, f)
	mustGit(t, filepath.Join(f.base, "seed"), "push", "--quiet", "origin", "--delete", "me/work")
	if e, want := reopenErr(t, r), "could not fetch PR #4's branch me/work"; !strings.HasPrefix(e, want) {
		t.Errorf("error = %q, want it to start %q", e, want)
	}
	gone(t, r.Path)
}

func TestAWorktreeIsNotMadeAgainForAPRFromAFork(t *testing.T) {
	f := setup(t)
	r := f.request("me/work")
	r.CrossRepo = true
	if e, want := reopenErr(t, r), "PR #4 is from a fork; its branch is not on origin"; e != want {
		t.Errorf("error = %q, want %q", e, want)
	}
}

func TestAWorktreeMadeAgainIsNotClearedForTheNextTask(t *testing.T) {
	f := setup(t)
	r := madeAndRemoved(t, f)
	reopenOK(t, r)

	next := r
	next.Keep = true
	want := "PR #4's worktree at " + r.Path + " is open to resume its session; remove it to run a task on the PR"
	if e := prepareErr(t, next); e != want {
		t.Errorf("error = %q, want %q", e, want)
	}
	if _, err := os.Stat(r.Path); err != nil {
		t.Errorf("the worktree made again is gone: %v", err)
	}
}

func TestATaskThatKeepsAWorktreeThatIsGoneMakesOneAsUsual(t *testing.T) {
	f := setup(t)
	r := f.request("me/work")
	r.Keep = true
	if p := prepareOK(t, r); p.Path != r.Path {
		t.Errorf("Path = %q, want %q", p.Path, r.Path)
	}
}
