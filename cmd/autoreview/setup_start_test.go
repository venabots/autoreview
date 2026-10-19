package main

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/venabots/autoreview/internal/config"
	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/ledger"
	"github.com/venabots/autoreview/internal/setup"
)

// Each question names a default. It is the run's own, read from no file
// at all, so a change to a default in the code changes the form too.
func TestTheFormNamesTheDefaultsOfARunWithNoSettingsFile(t *testing.T) {
	jobs := 5
	s, err := formStart(config.File{Path: "/c.toml", Found: true, Review: config.Review{Jobs: &jobs}}, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	none, err := valuesOf(config.File{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.Defaults, none) || s.Current.Jobs != 5 || s.Defaults.Jobs == 5 {
		t.Errorf("Defaults = %+v, Current.Jobs = %d; want the values of no file, and the file's 5 as the answer", s.Defaults, s.Current.Jobs)
	}
}

// Leaving without saving is a choice on the last step, not a failure, and
// it still says that nothing was written.
func TestLeavingTheFormWithoutSavingEndsWellAndSaysSo(t *testing.T) {
	var stderr strings.Builder
	if code := setupEnded(fmt.Errorf("the form: %w", setup.ErrNotSaved), &stderr); code != engine.ExitOK {
		t.Errorf("exit %d, want %d", code, engine.ExitOK)
	}
	if !strings.Contains(stderr.String(), "nothing was written") {
		t.Errorf("stderr = %q, want it to say nothing was written", stderr.String())
	}
}

func TestTheFormOffersTheModelsTheLedgerHasSeated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	model, ok := "gpt-5.6-sol", true
	if err := ledger.Append(path, ledger.Record{At: 10, Panel: []ledger.PanelEntry{{Name: "codex", Model: &model, OK: &ok}}}); err != nil {
		t.Fatal(err)
	}
	env := func(name string) string {
		if name == ledger.EnvLedger {
			return path
		}
		return ""
	}
	s, err := formStart(config.File{Path: "/c.toml"}, env)
	if err != nil {
		t.Fatal(err)
	}
	if want := []setup.Seen{{Spec: "codex:gpt-5.6-sol", Launched: 1, Answered: 1}}; !reflect.DeepEqual(s.Seen, want) {
		t.Errorf("Seen = %+v, want %+v", s.Seen, want)
	}
	// A ledger that cannot be read offers nothing, and stops nothing.
	missing := func(name string) string {
		if name == ledger.EnvLedger {
			return filepath.Join(t.TempDir(), "none.jsonl")
		}
		return ""
	}
	if s, err := formStart(config.File{Path: "/c.toml"}, missing); err != nil || s.Seen != nil {
		t.Errorf("formStart = %+v, %v; want no history and no error", s.Seen, err)
	}
}
