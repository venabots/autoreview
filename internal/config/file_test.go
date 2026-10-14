package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fileWith(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// The first run has no file. That is a run with no settings, not a fault.
func TestAMissingFileHasNoSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "autoreview", "config.toml")
	f, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if f.Found || f.Path != path || f.Review.Jobs != nil || f.Loop.Watch != nil || f.PRs.Approved != nil {
		t.Errorf("a missing file read as %+v", f)
	}
}

func TestAFileSetsTheKeysItNamesAndNoOthers(t *testing.T) {
	f, err := Load(fileWith(t, `
[review]
orchestrator = "codex:gpt-5.5"
jobs = 0

[loop]
rest = "1h"

[prs]
dependabot = false
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !f.Found || *f.Review.Orchestrator != "codex:gpt-5.5" || *f.Loop.Rest != "1h" {
		t.Errorf("the file read as %+v", f)
	}
	if f.Review.Jobs == nil || *f.Review.Jobs != 0 {
		t.Error("a key set to 0 read as a key that is not set")
	}
	if f.PRs.Dependabot == nil || *f.PRs.Dependabot {
		t.Error("a key set to false read as a key that is not set")
	}
	if f.Review.Fallback != nil || f.Loop.Watch != nil || f.PRs.Approved != nil {
		t.Errorf("a key the file does not name has a value: %+v", f)
	}
}

// A misspelled key that was dropped would leave the run on the default with
// no sign that the file said anything else.
func TestAnUnknownKeyIsAnErrorThatNamesIt(t *testing.T) {
	path := fileWith(t, "[loop]\nmax_pases = 5\n")
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "loop.max_pases") || !strings.Contains(err.Error(), path) {
		t.Errorf("Load = %v, want an error that names loop.max_pases and the file", err)
	}
}

func TestAValueOfTheWrongTypeNamesItsKey(t *testing.T) {
	path := fileWith(t, "[review]\njobs = \"four\"\n")
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "review.jobs") || !strings.Contains(err.Error(), path) {
		t.Errorf("Load = %v, want an error that names review.jobs and the file", err)
	}
}
