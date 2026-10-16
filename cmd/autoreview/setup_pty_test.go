package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/venabots/autoreview/internal/config"
)

// pressEnterUntil presses enter, a little apart, until path exists. Each
// enter takes the value a field shows and goes to the next one.
func (term *terminal) pressEnterUntil(path string) {
	term.t.Helper()
	for range 40 {
		if _, err := os.Stat(path); err == nil {
			return
		}
		term.press("\r")
		time.Sleep(150 * time.Millisecond)
	}
	term.t.Fatalf("%s was never written; the terminal drew:\n%q", path, term.output())
}

func TestTheFirstRunInTheViewAsksForTheSettingsAndWritesThem(t *testing.T) {
	sandbox(t)
	path := filepath.Join(t.TempDir(), "autoreview", "config.toml")
	t.Setenv("AUTOREVIEW_CONFIG", path)

	term := inTerminal(t, "--once", "4")
	term.waitFor("autoreview settings")
	if strings.Contains(term.output(), enterAltScreen) {
		t.Fatal("the view opened before the settings were asked")
	}
	term.pressEnterUntil(path)
	term.waitFor(enterAltScreen)
	time.Sleep(500 * time.Millisecond)
	term.press("qq")
	drawn := term.finish()

	if !strings.Contains(drawn, "wrote "+path) || !strings.Contains(drawn, "exit=0") {
		t.Errorf("the first run did not say it wrote the settings, or did not end well:\n%s", drawn[max(len(drawn)-600, 0):])
	}
	file, err := config.Load(path)
	if err != nil || !file.Found || *file.Review.Orchestrator != "claude" || *file.Loop.MaxPasses != 3 || *file.Review.Verdicts != "approve-and-request-changes" {
		t.Errorf("the written settings: %+v, %v", file, err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("the settings file has mode %v, want 0600", info.Mode().Perm())
	}
	wantCooked(t, drawn)
}

func TestHeadlessOnATerminalNeverAsksForTheSettings(t *testing.T) {
	sandbox(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("AUTOREVIEW_CONFIG", path)

	drawn := inTerminal(t, "--headless", "9").finish()
	if strings.Contains(drawn, "autoreview settings") || !strings.Contains(drawn, "exit=0") {
		t.Errorf("--headless on a terminal:\n%s", drawn)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("--headless wrote the settings file")
	}
}

func TestSetupOnATerminalWritesTheFileAndCtrlCWritesNothing(t *testing.T) {
	sandbox(t)
	path := filepath.Join(t.TempDir(), "autoreview", "config.toml")
	t.Setenv("AUTOREVIEW_CONFIG", path)

	stopped := inTerminal(t, "setup")
	stopped.waitFor("autoreview settings")
	stopped.press("\x03")
	if drawn := stopped.finish(); !strings.Contains(drawn, "exit=130") {
		t.Errorf("ctrl-C in setup:\n%s", drawn)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("ctrl-C in setup wrote the file")
	}

	term := inTerminal(t, "setup")
	term.waitFor("autoreview settings")
	term.pressEnterUntil(path)
	if drawn := term.finish(); !strings.Contains(drawn, "wrote "+path) || !strings.Contains(drawn, "exit=0") {
		t.Errorf("setup:\n%s", drawn)
	}
}
