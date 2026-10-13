package gh

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeGh puts a script named gh first on PATH for one test.
func fakeGh(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestExecReturnsWhatGhPrinted(t *testing.T) {
	fakeGh(t, `echo "$@"`)
	out, err := Exec(context.Background(), "api", "user", "--jq", ".login")
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if got := string(out); got != "api user --jq .login\n" {
		t.Errorf("Exec returned %q", got)
	}
}

func TestExecTellsGhNotToPromptAndGivesItNoInput(t *testing.T) {
	fakeGh(t, `echo "$GH_PROMPT_DISABLED $GH_NO_UPDATE_NOTIFIER"; if read line; then echo "read: $line"; else echo "no input"; fi`)
	out, err := Exec(context.Background(), "pr", "view")
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if got := string(out); got != "1 1\nno input\n" {
		t.Errorf("gh saw %q, want prompts off and an empty stdin", got)
	}
}

func TestAFailedCallSaysWhichCommandAndWhy(t *testing.T) {
	fakeGh(t, `echo "  HTTP 401: Bad credentials" >&2; echo "second line" >&2; exit 1`)
	_, err := Exec(context.Background(), "api", "graphql", "-f", "query=a very long query")
	if err == nil {
		t.Fatal("Exec hid a failed gh")
	}
	if got, want := err.Error(), "gh api graphql: HTTP 401: Bad credentials"; got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
}

func TestAFailedCallWithNothingOnStderrStillSaysWhichCommand(t *testing.T) {
	fakeGh(t, `exit 3`)
	_, err := Exec(context.Background(), "repo")
	if err == nil || !strings.HasPrefix(err.Error(), "gh repo: ") {
		t.Errorf("error = %v, want one that starts with the command", err)
	}
}

func TestADeadlineStopsGhAndWhatItStarted(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "child-finished")
	fakeGh(t, `(sleep 2; touch "`+marker+`") & wait`)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	began := time.Now()
	_, err := Exec(ctx, "api", "graphql")

	if err == nil || err.Error() != "gh api graphql timed out" {
		t.Errorf("error = %v, want a timeout that names the command", err)
	}
	if took := time.Since(began); took > 1500*time.Millisecond {
		t.Errorf("Exec returned after %v, want it to stop at the deadline", took)
	}
	time.Sleep(2500 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Error("a process gh started outlived the deadline: the group was not killed")
	}
}

func TestACallWithNoDeadlineGetsTheDefault(t *testing.T) {
	ctx, cancel := withDefaultDeadline(context.Background())
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > DefaultTimeout || time.Until(deadline) < DefaultTimeout-time.Second {
		t.Errorf("deadline = %v, %v; want about %v from now", deadline, ok, DefaultTimeout)
	}

	short, cancelShort := context.WithTimeout(context.Background(), time.Second)
	defer cancelShort()
	kept, cancelKept := withDefaultDeadline(short)
	defer cancelKept()
	if a, _ := kept.Deadline(); a != mustDeadline(t, short) {
		t.Error("a caller's own deadline was replaced")
	}
}

func mustDeadline(t *testing.T, ctx context.Context) time.Time {
	t.Helper()
	d, ok := ctx.Deadline()
	if !ok {
		t.Fatal("the context has no deadline")
	}
	return d
}
