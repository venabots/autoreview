package worktree

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExecRunsGitInTheDirectoryItIsGiven(t *testing.T) {
	isolate(t)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "init", "--quiet")
	out, err := Exec(context.Background(), dir, "rev-parse", "--show-toplevel")
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != dir {
		t.Errorf("git ran in %q, want %q", got, dir)
	}
}

func TestExecTellsGitNotToPromptAndGivesItNoInput(t *testing.T) {
	isolate(t)
	alias := `alias.probe=!echo "$GIT_TERMINAL_PROMPT"; if read line; then echo "read: $line"; else echo "no input"; fi`
	out, err := Exec(context.Background(), t.TempDir(), "-c", alias, "probe")
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if got := string(out); got != "0\nno input\n" {
		t.Errorf("git saw %q, want prompts off and an empty stdin", got)
	}
}

func TestExecRunsGitWithNoControllingTerminal(t *testing.T) {
	isolate(t)
	out, err := Exec(context.Background(), t.TempDir(), "-c", `alias.tty=!(: < /dev/tty) 2>/dev/null && echo has || echo none`, "tty")
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "none" {
		t.Errorf("git could open /dev/tty: %q; ssh would prompt there", got)
	}
}

func TestExecKillsGitAndWhatItStartedWhenTheContextEnds(t *testing.T) {
	isolate(t)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := Exec(ctx, t.TempDir(), "-c", "alias.slow=!sleep 30", "slow")
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("Exec took %v after its deadline", took)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error = %v, want one that wraps the deadline", err)
	}
}

func TestAFailedGitSaysWhatGitSaid(t *testing.T) {
	isolate(t)
	cases := []struct {
		name  string
		alias string
		want  string
	}{
		{"its first line", `alias.f=!echo "  error: something" >&2; echo second >&2; exit 1`, "error: something"},
		{"its fatal line with the cause before it", `alias.f=!echo "Permission denied" >&2; echo "fatal: no" >&2; echo hint >&2; exit 128`, "Permission denied fatal: no"},
		{"the command when stderr is empty", `alias.f=!exit 3`, "git f failed (exit status 3)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Exec(context.Background(), t.TempDir(), "-c", c.alias, "f")
			if err == nil || err.Error() != c.want {
				t.Errorf("error = %v, want %q", err, c.want)
			}
		})
	}
}

func TestAGitThatCannotStartSaysSo(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := Exec(context.Background(), t.TempDir(), "status")
	if err == nil || !strings.HasPrefix(err.Error(), "could not run git: ") {
		t.Errorf("error = %v", err)
	}
}

func TestTheReasonIsGitsFatalLineNotItsHints(t *testing.T) {
	cases := []struct {
		name, stderr, want string
	}{
		{"an ssh refusal keeps its cause",
			"git@github.com: Permission denied (publickey).\nfatal: Could not read from remote repository.\n\nPlease make sure you have the correct access rights\nand the repository exists.\n",
			"git@github.com: Permission denied (publickey). fatal: Could not read from remote repository."},
		{"a fatal first line stands alone", "fatal: bad\nhint: x\n", "fatal: bad"},
		{"no fatal line gives the first", "error: something\n", "error: something"},
		{"blank stderr gives nothing", "\n\n", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := reason(c.stderr); got != c.want {
				t.Errorf("reason = %q, want %q", got, c.want)
			}
		})
	}
}

func TestOnlyATaskWorktreeThisToolMadeCountsAsOurs(t *testing.T) {
	cases := []struct {
		path string
		ours bool
	}{
		{"/tmp/run-x/worktrees/pr-4", true},
		{"/Volumes/usb/work", false},
		{"/Volumes/usb/worktrees/pr-12", false},
		{"/tmp/run-x/worktrees/pr-", false},
		{"/tmp/run-x/worktrees/pr-4a", false},
		{"/tmp/run-x/trees/pr-4", false},
	}
	for _, c := range cases {
		if got := madeHere(c.path); got != c.ours {
			t.Errorf("madeHere(%q) = %v, want %v", c.path, got, c.ours)
		}
	}
}

func TestPathForPutsTheWorktreeUnderTheRun(t *testing.T) {
	if got := PathFor("/runs/run-1", 12); got != "/runs/run-1/worktrees/pr-12" {
		t.Errorf("PathFor = %q", got)
	}
	if !madeHere(PathFor("/runs/run-1", 12)) {
		t.Error("a path this tool makes does not count as its own")
	}
}
