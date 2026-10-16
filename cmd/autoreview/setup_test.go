package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// A link made before its target exists, as a dotfiles install can leave,
// still stays a link: the write creates the file it points at.
func TestWritingTheSettingsThroughALinkWhoseTargetIsMissingKeepsTheLink(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "config.toml")
	if err := os.Symlink(filepath.Join("dotfiles", "autoreview.toml"), link); err != nil {
		t.Fatal(err)
	}
	if err := writeSettings(link, []byte("[review]\n")); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the link was replaced: %v, %v", info, err)
	}
	if raw, err := os.ReadFile(filepath.Join(dir, "dotfiles", "autoreview.toml")); err != nil || string(raw) != "[review]\n" {
		t.Errorf("the target holds %q, %v", raw, err)
	}
}

func TestABrokenFileSaysHowToRepairIt(t *testing.T) {
	err := withRepairHint(errors.New("/c.toml sets loop.max_pases, which is not a setting"), "/c.toml")
	if want := "/c.toml sets loop.max_pases, which is not a setting; fix /c.toml or delete it, then run autoreview setup again"; err.Error() != want {
		t.Errorf("the error is %q, want %q", err, want)
	}
}

// A link that starts with .. is read from the folder the link is in, after
// any link in that folder's path, as the system reads it. Joined as text,
// the write would go to another file.
func TestALinkWithDotDotInALinkedFolderIsFollowedAsTheSystemDoes(t *testing.T) {
	for _, exists := range []bool{true, false} {
		root := t.TempDir()
		real, linked := filepath.Join(root, "dot", "app"), filepath.Join(root, "cfg", "app")
		for _, dir := range []string{real, filepath.Dir(linked)} {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Symlink(real, linked); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join("..", "shared.toml"), filepath.Join(real, "config.toml")); err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(root, "dot", "shared.toml")
		if exists {
			write(t, filepath.Dir(want), "shared.toml", "[review]\n")
		}
		if err := writeSettings(filepath.Join(linked, "config.toml"), []byte("[review]\njobs = 4\n")); err != nil {
			t.Fatal(err)
		}
		if raw, err := os.ReadFile(want); err != nil || string(raw) != "[review]\njobs = 4\n" {
			t.Errorf("target there %v: %s holds %q, %v", exists, want, raw, err)
		}
		if _, err := os.Stat(filepath.Join(root, "cfg", "shared.toml")); err == nil {
			t.Errorf("target there %v: the write went next to the link's folder name, not where the system reads it", exists)
		}
	}
}

// A link whose own text climbs out of a linked folder, as alias/../new.toml
// does when alias is a link, is followed one part at a time, as the system
// follows it. Cleaned as text, alias/.. would vanish.
func TestALinkThatClimbsOutOfALinkedFolderIsFollowedPartByPart(t *testing.T) {
	root := t.TempDir()
	app := filepath.Join(root, "dot", "app")
	cfg := filepath.Join(root, "cfg")
	for _, dir := range []string{app, cfg} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(app, filepath.Join(cfg, "alias")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("alias/../new.toml", filepath.Join(cfg, "config.toml")); err != nil {
		t.Fatal(err)
	}
	if err := writeSettings(filepath.Join(cfg, "config.toml"), []byte("[review]\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "dot", "new.toml")); err != nil {
		t.Errorf("the write did not reach dot/new.toml: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg, "new.toml")); err == nil {
		t.Error("the write went beside the link, where alias/.. reads as nothing")
	}
}

// autoreview setup starts from a file that the run accepts. A clash that
// only the defaults make, with no variable to move them, is the run's to
// report, not a reason to refuse the form.
func TestSetupStartsFromAFileThatOnlyTheDefaultsMakeClash(t *testing.T) {
	fallback := "claude"
	v, err := valuesOf(config.File{Path: "/c.toml", Found: true, Review: config.Review{Fallback: &fallback}})
	if err != nil || v.Fallback != "claude" || v.Orchestrator != "claude" {
		t.Errorf("valuesOf = %+v, %v", v, err)
	}
}

// A folder in the link's text can itself be a link to a folder that does
// not exist. A file written where the link's text leads could not be read
// back through the link, and the next run would ask again. So the write is
// refused, and nothing is left beside the link or where it points.
func TestALinkThatCannotLeadToTheWrittenFileIsRefused(t *testing.T) {
	root := t.TempDir()
	cfg := filepath.Join(root, "cfg")
	if err := os.MkdirAll(cfg, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "dot", "app"), filepath.Join(cfg, "alias")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("alias/../new.toml", filepath.Join(cfg, "config.toml")); err != nil {
		t.Fatal(err)
	}
	err := writeSettings(filepath.Join(cfg, "config.toml"), []byte("[review]\n"))
	if err == nil || !strings.Contains(err.Error(), "does not lead to") {
		t.Errorf("writeSettings = %v, want a refusal that says the link leads nowhere", err)
	}
	for _, left := range []string{filepath.Join(root, "dot", "new.toml"), filepath.Join(cfg, "new.toml")} {
		if _, err := os.Stat(left); err == nil {
			t.Errorf("%s was left behind", left)
		}
	}
}

// Two folders that are links to each other must end the walk, as the
// system ends it, and not recurse until the stack runs out.
func TestFoldersThatLinkToEachOtherEndTheWalk(t *testing.T) {
	cfg := t.TempDir()
	if err := os.Symlink("b/x", filepath.Join(cfg, "a")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a/x", filepath.Join(cfg, "b")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a/../config.toml", filepath.Join(cfg, "config.toml")); err != nil {
		t.Fatal(err)
	}
	done := make(chan string, 1)
	go func() { done <- linkTarget(filepath.Join(cfg, "config.toml")) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the walk through two folders that link to each other did not end")
	}
}
