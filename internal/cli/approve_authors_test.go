package cli

import (
	"slices"
	"strings"
	"testing"

	"github.com/venabots/autoreview/internal/config"
)

// The logins come from the file as a person writes them, often with an @.
// A login GitHub would never give is an error that names the key: kept in
// silence, it would match nobody, and no PR would get an approval.
func TestTheAuthorsAReviewMayApproveComeFromTheFile(t *testing.T) {
	if cfg := parse(t); len(cfg.ApproveAuthors) != 0 {
		t.Errorf("by default only %q may be approved", cfg.ApproveAuthors)
	}
	named := withSettings(func(f *config.File) {
		f.Review.ApproveAuthors = []string{"@alice", "Bob", "dependabot[bot]"}
	})
	if cfg := parseWith(t, nil, named); !slices.Equal(cfg.ApproveAuthors, []string{"alice", "Bob", "dependabot[bot]"}) {
		t.Errorf("may approve %q, want alice, Bob and dependabot[bot]", cfg.ApproveAuthors)
	}
	for _, bad := range []string{"", "pm sam", "pm/sam", "-pm"} {
		_, err := ParseWith(nil, noEnv, withSettings(func(f *config.File) { f.Review.ApproveAuthors = []string{bad} }))
		want := "review.approve_authors in " + settingsPath + ": "
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want an error that starts %q", bad, err, want)
		}
	}
}
