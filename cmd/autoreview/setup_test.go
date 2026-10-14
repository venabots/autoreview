package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupAsksOnATerminalOnlyAndNeedsAFileToWrite(t *testing.T) {
	sandbox(t)
	path := filepath.Join(t.TempDir(), "autoreview", "config.toml")
	t.Setenv("AUTOREVIEW_CONFIG", path)

	_, stderr, code := autoreview(t, "setup")
	if code != 1 || stderr != "error: autoreview setup asks on a terminal; edit "+path+" by hand\n" {
		t.Errorf("setup off a terminal: exit %d, stderr %q", code, stderr)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("setup off a terminal wrote the file")
	}

	t.Setenv("AUTOREVIEW_CONFIG", "off")
	if _, stderr, code := autoreview(t, "setup"); code != 1 || !strings.Contains(stderr, "there is no settings file to write") {
		t.Errorf("setup with the settings off: exit %d, stderr %q", code, stderr)
	}
	if _, stderr, code := autoreview(t, "9", "setup"); code != 1 || !strings.Contains(stderr, "setup is a subcommand and goes first") {
		t.Errorf("setup after a PR: exit %d, stderr %q", code, stderr)
	}
}

// A run that prints plain lines never asks: cron has nobody to answer, and
// a form there would wait forever.
func TestAPlainRunWithNoSettingsFileNeitherAsksNorWritesOne(t *testing.T) {
	sandbox(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("AUTOREVIEW_CONFIG", path)
	if _, stderr, code := autoreview(t, "9"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("a plain run wrote the settings file")
	}
}
