package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// withOrigin gives the sandbox checkout an origin that has your PR's
// branch, so a task can make its worktree from it.
func withOrigin(t *testing.T) {
	t.Helper()
	origin := t.TempDir()
	gitIn := func(dir string, args ...string) {
		t.Helper()
		full := append([]string{"-C", dir, "-c", "user.name=Sandbox", "-c", "user.email=sandbox@example.com"}, args...)
		if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	gitIn(origin, "init", "--quiet", "--bare")
	here, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	gitIn(here, "commit", "--quiet", "--allow-empty", "-m", "start")
	gitIn(here, "remote", "add", "origin", origin)
	gitIn(here, "push", "--quiet", "origin", "HEAD:refs/heads/main", "HEAD:refs/heads/me/my-own-work")
}

// installSkill puts a skill where claude looks for one.
func installSkill(t *testing.T, skill string) {
	t.Helper()
	dir := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "skills", skill)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "SKILL.md", "# "+skill+"\n")
}

func waitForCall(t *testing.T, fixtures, text string) {
	t.Helper()
	for range 500 {
		for _, call := range dashpCalls(t, fixtures) {
			if strings.Contains(call, text) {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("dash-p was never called with %q: %q", text, dashpCalls(t, fixtures))
}

func TestFOnMyPRsFixesThePRInAWorktreeAndReportsWhatItPushed(t *testing.T) {
	fixtures := sandbox(t)
	withOrigin(t)
	installSkill(t, "babysit-pr")
	term := inTerminal(t, "--once", "4")
	term.waitFor(enterAltScreen)
	time.Sleep(1500 * time.Millisecond)
	term.press("\tf")
	waitForCall(t, fixtures, "/babysit-pr 4")
	time.Sleep(1500 * time.Millisecond)
	term.press("qq")
	drawn := term.finish()

	after := drawn[strings.LastIndex(drawn, leaveAltScreen):]
	for _, want := range []string{"1 task: 1 pushed · summary in ", "exit=0", "leftover=0"} {
		if !strings.Contains(after, want) {
			t.Errorf("after the view closed the terminal lacks %q:\n%s", want, after)
		}
	}
	logs, _ := filepath.Glob(filepath.Join(fixtures, "logs", "run-*", "autoreview.log"))
	if len(logs) != 1 {
		t.Fatalf("run logs: %v", logs)
	}
	log, _ := os.ReadFile(logs[0])
	for _, want := range []string{"start   #4 (fixing)", "done    #4 (fix, ", "): pushed", "PR  TASK  RESULT  BRANCH", "task logs: "} {
		if !strings.Contains(string(log), want) {
			t.Errorf("the run log lacks %q:\n%s", want, log)
		}
	}
	tree := filepath.Join(filepath.Dir(logs[0]), "worktrees", "pr-4")
	if _, err := os.Stat(tree); !os.IsNotExist(err) {
		t.Errorf("the worktree of a task that left nothing behind is still there: %s (%v)", tree, err)
	}
	wantCooked(t, drawn)
}

func TestATaskWhoseSkillIsMissingIsRefusedInTheView(t *testing.T) {
	fixtures := sandbox(t)
	term := inTerminal(t, "--once", "4")
	term.waitFor(enterAltScreen)
	time.Sleep(1500 * time.Millisecond)
	term.press("\tf")
	time.Sleep(500 * time.Millisecond)
	term.press("qq")
	drawn := term.finish()
	if calls := dashpCalls(t, fixtures); len(calls) != 0 {
		t.Errorf("a task with no skill ran: %q", calls)
	}
	after := drawn[strings.LastIndex(drawn, leaveAltScreen):]
	if !strings.Contains(after, "exit=0") || strings.Contains(after, "task") {
		t.Errorf("after the view closed:\n%s\nwant exit 0 and no task", after)
	}
}
