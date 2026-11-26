package riskcheck

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func shellScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pr-risk")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

// A real script reads its stdin and its variables, and prints the rating.
func TestARealScriptReadsThePathsAndPrintsItsRating(t *testing.T) {
	path := shellScript(t, `n=$(wc -l | tr -d ' '); echo HIGH; echo "$n paths in PR $AUTOREVIEW_PR"`)
	out, err := Exec(context.Background(), Script{Path: path, Dir: t.TempDir(), Env: []string{"AUTOREVIEW_PR=9"}, Stdin: []byte("a\nb\n")})
	if err != nil || string(out) != "HIGH\n2 paths in PR 9\n" {
		t.Errorf("got %q, %v", out, err)
	}
}

// A script that fails says why in its own words, which the log shows.
func TestAFailedScriptSaysWhyFromItsStderr(t *testing.T) {
	path := shellScript(t, "echo 'CODEOWNERS is missing' >&2; exit 3\n")
	_, err := Exec(context.Background(), Script{Path: path, Dir: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "exit status 3: CODEOWNERS is missing") {
		t.Errorf("got %v", err)
	}
}

// A script that hangs, and the child it started, are killed at the
// deadline: the look waits for the check, and must not wait forever.
func TestAScriptThatHangsIsKilledAtTheDeadline(t *testing.T) {
	path := shellScript(t, "sleep 30 & sleep 30\n")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := Exec(ctx, Script{Path: path, Dir: t.TempDir()})
	if err == nil || err.Error() != "it timed out" {
		t.Errorf("got %v", err)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("the script was killed after %s", took)
	}
}

// The script's environment is the one it is handed and nothing more. An
// empty one stays empty: left to os/exec, a nil one would be autoreview's
// whole environment, tokens and all.
func TestARealScriptSeesOnlyTheEnvironmentItIsHanded(t *testing.T) {
	t.Setenv("GH_TOKEN", "ghp_approving")
	t.Setenv("AUTOREVIEW_TEST_SECRET", "secret")
	path := shellScript(t, "echo LOW; /usr/bin/env\n")
	cases := []struct {
		name string
		env  []string
		want []string
	}{
		{"a few variables", []string{"PATH=/usr/bin:/bin", "AUTOREVIEW_PR=9"}, []string{"PATH=/usr/bin:/bin", "AUTOREVIEW_PR=9"}},
		{"none", nil, nil},
	}
	for _, c := range cases {
		out, err := Exec(context.Background(), Script{Path: path, Dir: t.TempDir(), Env: c.env})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")[1:]
		for _, line := range lines {
			if strings.Contains(line, "ghp_approving") || strings.Contains(line, "secret") {
				t.Errorf("%s: the script saw %q", c.name, line)
			}
		}
		for _, want := range c.want {
			if !slices.Contains(lines, want) {
				t.Errorf("%s: the script's environment %q lacks %s", c.name, lines, want)
			}
		}
	}
}
