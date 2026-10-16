package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/venabots/autoreview/internal/config"
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

// A settings file kept in a dotfiles repo is a link to it. Writing must
// change the file the link points at, keep the link, and keep its mode.
func TestWritingTheSettingsKeepsALinkAndTheFilesMode(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "dotfiles", "autoreview.toml")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("[review]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "config.toml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := writeSettings(link, []byte("[review]\njobs = 4\n")); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the link was replaced: %v, %v", info, err)
	}
	raw, err := os.ReadFile(target)
	if err != nil || string(raw) != "[review]\njobs = 4\n" {
		t.Errorf("the target holds %q, %v", raw, err)
	}
	if info, _ := os.Stat(target); info.Mode().Perm() != 0o644 {
		t.Errorf("the target's mode is %v, want 0644 kept", info.Mode().Perm())
	}
}

// The form does not ask for the focus, so the file keeps the person's own
// words, quotes and all, and not the form a prompt carries.
func TestTheValuesTheFormDoesNotAskAreWrittenBackAsTheFileHasThem(t *testing.T) {
	focus := `the "fast" path`
	v, err := valuesOf(config.File{Path: "/c.toml", Found: true, Review: config.Review{Focus: &focus}})
	if err != nil || v.Focus != focus {
		t.Errorf("focus %q, %v; want %q", v.Focus, err, focus)
	}
}
