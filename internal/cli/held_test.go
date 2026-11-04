package cli

import (
	"slices"
	"strings"
	"testing"

	"github.com/venabots/autoreview/internal/config"
)

// The logins come from the file as a person writes them, often with an @.
// A login GitHub would never give is an error that names the key: held
// back in silence, it would hold back nobody.
func TestTheHeldAuthorsComeFromTheFile(t *testing.T) {
	if cfg := parse(t); len(cfg.NoApproveAuthors) != 0 {
		t.Errorf("by default %q are held", cfg.NoApproveAuthors)
	}
	held := withSettings(func(f *config.File) { f.Review.NoApproveAuthors = []string{"@pm-sam", "Alice"} })
	if cfg := parseWith(t, nil, held); !slices.Equal(cfg.NoApproveAuthors, []string{"pm-sam", "Alice"}) {
		t.Errorf("held %q, want pm-sam and Alice", cfg.NoApproveAuthors)
	}
	for _, bad := range []string{"", "pm sam", "pm/sam", "-pm"} {
		_, err := ParseWith(nil, noEnv, withSettings(func(f *config.File) { f.Review.NoApproveAuthors = []string{bad} }))
		want := "review.no_approve_authors in " + settingsPath + ": "
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want an error that starts %q", bad, err, want)
		}
	}
}
