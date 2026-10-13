package rundir

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func TestEachRunGetsADirectoryOfItsOwn(t *testing.T) {
	logDir := t.TempDir()
	first, err := New(logDir, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	second, err := New(logDir, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if first.Root == second.Root {
		t.Errorf("two runs under one log directory share %s", first.Root)
	}
	for _, run := range []Run{first, second} {
		if filepath.Dir(run.Root) != logDir || !strings.HasPrefix(filepath.Base(run.Root), "run-") {
			t.Errorf("the run directory is %s, want run-* under the log directory", run.Root)
		}
		if got := mode(t, run.Root); got != 0o700 {
			t.Errorf("the run directory has mode %o, want 700", got)
		}
	}
}

func TestALogDirectoryThatDoesNotExistIsMade(t *testing.T) {
	logDir := filepath.Join(t.TempDir(), "a", "b")
	run, err := New(logDir, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if filepath.Dir(run.Root) != logDir {
		t.Errorf("the run directory is %s, want it under %s", run.Root, logDir)
	}
}

// Git makes a worktree relative to the repo root, and a reviewer starts
// relative to the directory the person stands in. A run directory that
// stayed relative would name two places.
func TestARelativeLogDirectoryBecomesAbsolute(t *testing.T) {
	t.Chdir(t.TempDir())
	run, err := New("logs", "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !filepath.IsAbs(run.Root) {
		t.Errorf("the run directory is %s, want an absolute path", run.Root)
	}
	if _, err := os.Stat(filepath.Join("logs", filepath.Base(run.Root))); err != nil {
		t.Errorf("the run directory is not under ./logs: %v", err)
	}
}

func TestWithNoLogDirectoryTheRunGoesUnderTheTempDirectory(t *testing.T) {
	temp := t.TempDir()
	run, err := New("", temp)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	parent := filepath.Dir(run.Root)
	if filepath.Dir(parent) != temp || !strings.HasPrefix(filepath.Base(parent), "autoreview.") {
		t.Errorf("the run directory is %s, want <temp>/autoreview.*/run-*", run.Root)
	}
	if got := mode(t, parent); got != 0o700 {
		t.Errorf("the made log directory has mode %o, want 700", got)
	}
}

func TestNewSaysWhyItCouldNotMakeTheDirectory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(filepath.Join(file, "logs"), ""); err == nil || !strings.Contains(err.Error(), "making the log directory") {
		t.Errorf("New under a file: %v", err)
	}
	if _, err := New("", filepath.Join(file, "tmp")); err == nil || !strings.Contains(err.Error(), "making a log directory") {
		t.Errorf("New with a bad temp directory: %v", err)
	}
}

func TestThePathsOfARun(t *testing.T) {
	run := Run{Root: "/logs/run-1"}
	cases := map[string]string{
		run.Log():          "/logs/run-1/autoreview.log",
		run.Agent():        "/logs/run-1/agent",
		run.Worktree(4):    "/logs/run-1/worktrees/pr-4",
		run.SessionFile(9): "/logs/run-1/session-9.id",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("path = %s, want %s", got, want)
		}
	}
}

func TestThePathsOfAPass(t *testing.T) {
	run, err := New(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	pass, err := run.Pass(2)
	if err != nil {
		t.Fatalf("Pass: %v", err)
	}
	if pass.Dir != filepath.Join(run.Root, "pass-2") || mode(t, pass.Dir) != 0o700 {
		t.Errorf("the pass directory is %s with mode %o", pass.Dir, mode(t, pass.Dir))
	}
	cases := map[string]string{
		pass.Stdout(9): "pr-9.json",
		pass.Stderr(9): "pr-9.log",
		pass.Meta(9):   "pr-9.meta.json",
		pass.Review(9): "pr-9.review.md",
	}
	for got, want := range cases {
		if got != filepath.Join(pass.Dir, want) {
			t.Errorf("path = %s, want %s in the pass directory", got, want)
		}
	}

	task, err := pass.Task("fix")
	if err != nil {
		t.Fatalf("Task: %v", err)
	}
	if task.Stderr(4) != filepath.Join(run.Root, "pass-2", "fix", "pr-4.log") {
		t.Errorf("a task's log is %s, want it in a directory of its own under the pass", task.Stderr(4))
	}
	if _, err := run.Pass(2); err != nil {
		t.Errorf("making the same pass again: %v", err)
	}
}

func TestRecordSessionLeavesTheIDAndNoTempFile(t *testing.T) {
	run, err := New(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	const id = "7442b624-5cba-5d44-ae67-9c390cfe70a1"
	for _, write := range []string{"first", id} {
		if err := run.RecordSession(9, write); err != nil {
			t.Fatalf("RecordSession: %v", err)
		}
	}
	got, err := os.ReadFile(run.SessionFile(9))
	if err != nil || string(got) != id {
		t.Errorf("the session file holds %q, %v; want the last id written", got, err)
	}
	if _, err := os.Stat(run.SessionFile(9) + ".tmp"); err == nil {
		t.Error("the temporary file was left behind")
	}
}

func TestArchiveAttemptSetsAsideWhatAFailedAttemptWrote(t *testing.T) {
	run, err := New(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	pass, err := run.Pass(1)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{pass.Stderr(9), pass.Stdout(9)} {
		if err := os.WriteFile(path, []byte("from claude"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	pass.ArchiveAttempt(9, "claude")

	for _, name := range []string{"pr-9.claude.log", "pr-9.claude.json"} {
		if got, err := os.ReadFile(filepath.Join(pass.Dir, name)); err != nil || string(got) != "from claude" {
			t.Errorf("%s holds %q, %v; want the failed attempt's file", name, got, err)
		}
	}
	for _, path := range []string{pass.Stderr(9), pass.Stdout(9)} {
		if _, err := os.Stat(path); err == nil {
			t.Errorf("%s is still there: the fallback would append to the failure's file", path)
		}
	}
	if _, err := os.Stat(filepath.Join(pass.Dir, "pr-9.claude.meta.json")); err == nil {
		t.Error("a meta file that was never written was made")
	}
}
