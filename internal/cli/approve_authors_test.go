package cli

import (
	"slices"
	"strings"
	"testing"

	"github.com/venabots/autoreview/internal/config"
	"github.com/venabots/autoreview/internal/review"
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

// The ceiling comes from the file. A word it does not know, or a ceiling
// with nobody named, is an error that names the key: either one kept in
// silence leaves a person sure that a migration waits for them when it
// does not.
func TestTheRiskCeilingForOtherAuthorsComesFromTheFile(t *testing.T) {
	if cfg := parse(t); cfg.ApproveOthersUpTo != review.RiskNone {
		t.Errorf("by default other authors are approved up to %s", cfg.ApproveOthersUpTo)
	}
	set := func(authors []string, ceiling string) config.File {
		return withSettings(func(f *config.File) { f.Review.ApproveAuthors, f.Review.ApproveOthersUpTo = authors, &ceiling })
	}
	if cfg := parseWith(t, nil, set([]string{"alice"}, "medium")); cfg.ApproveOthersUpTo != review.RiskMedium {
		t.Errorf("approve_others_up_to = \"medium\" reads as %s", cfg.ApproveOthersUpTo)
	}
	if cfg := parseWith(t, nil, set(nil, "none")); cfg.ApproveOthersUpTo != review.RiskNone {
		t.Errorf("approve_others_up_to = \"none\" with nobody named reads as %s", cfg.ApproveOthersUpTo)
	}
	cases := []struct {
		authors []string
		ceiling string
		want    string
	}{
		{[]string{"alice"}, "high", `expects none, low or medium, got "high"`},
		{nil, "medium", "review.approve_authors names nobody"},
	}
	for _, c := range cases {
		_, err := ParseWith(nil, noEnv, set(c.authors, c.ceiling))
		prefix := "review.approve_others_up_to in " + settingsPath
		if err == nil || !strings.HasPrefix(err.Error(), prefix) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q with %q named: %v, want an error that starts %q and says %q", c.ceiling, c.authors, err, prefix, c.want)
		}
	}
}
