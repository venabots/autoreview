package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func settingsFile(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AUTOREVIEW_CONFIG", path)
	return path
}

// A file that says something the run cannot read stops the run. A cron job
// must not go ahead on values the person did not choose.
func TestASettingsFileThatCannotBeReadStopsTheRunButNotTheHelp(t *testing.T) {
	sandbox(t)
	path := settingsFile(t, "[loop]\nmax_pases = 2\n")

	stdout, stderr, code := autoreview(t, "--headless")
	if code != 1 || stdout != "" || !strings.Contains(stderr, path+" sets loop.max_pases, which is not a setting") {
		t.Errorf("an unknown key: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	for _, flag := range []string{"--help", "--version"} {
		if _, stderr, code := autoreview(t, flag); code != 0 {
			t.Errorf("%s with a broken settings file: exit %d, stderr %q", flag, code, stderr)
		}
	}

	settingsFile(t, "[review]\njobs = 0\n")
	_, stderr, code = autoreview(t, "--headless")
	if code != 1 || !strings.Contains(stderr, "review.jobs in ") || !strings.Contains(stderr, "--jobs expects an integer >= 1") {
		t.Errorf("a bad value: exit %d, stderr %q", code, stderr)
	}
}

// The checks that join two values see both: a file that moves the
// orchestrator to codex lets a flag name claude as the fallback.
func TestAFlagIsCheckedWithTheFilesValuesAndNotWithTheDefaults(t *testing.T) {
	sandbox(t)
	settingsFile(t, "[review]\norchestrator = \"codex\"\n")
	if stdout, stderr, code := autoreview(t, "--fallback", "claude", "9"); code != 0 || !strings.Contains(stdout, "fallback: claude") {
		t.Errorf("exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
}
