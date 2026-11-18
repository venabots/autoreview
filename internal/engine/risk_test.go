package engine

import (
	"context"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/review"
)

func byAuthor(author string) func() (prs.Listing, error) {
	return func() (prs.Listing, error) {
		return prs.Listing{PRs: []prs.PR{{Number: 9, Author: author, Base: "main", Head: "sha9", Engagement: prs.New, CI: prs.CIPassing}}}, nil
	}
}

func upToMedium(cfg *Config) {
	cfg.ApproveAuthors, cfg.ApproveOthersUpTo = []string{"alice"}, review.RiskMedium
}

// The repository's risk script rates another author's PR before its review
// starts. A rating within the ceiling leaves the review to decide; a rating
// above it, or a check that failed, takes the approval away before any
// model runs, and the log says why. A repository with no script leaves the
// review to decide.
func TestTheRiskScriptSetsTheLevelOfAnotherAuthorsReview(t *testing.T) {
	cases := []struct {
		name   string
		check  func() review.RiskCheck
		inArgv string
		note   string
	}{
		{"no script", func() review.RiskCheck { return review.RiskCheck{NoScript: true} }, "--max-risk medium", ""},
		{"rated low", func() review.RiskCheck { return review.RiskCheck{Script: "bin/pr-risk", Risk: review.RiskLow} }, "--max-risk medium", ""},
		{"rated high", func() review.RiskCheck {
			return review.RiskCheck{Script: "bin/pr-risk", Risk: review.RiskHigh, Why: "packages/db/src/migrations/0123.ts is a migration"}
		}, "--verdicts request-changes-only",
			"PR #9 waits for a person to approve it: bin/pr-risk rated it high, above the ceiling of medium: packages/db/src/migrations/0123.ts is a migration"},
		{"failed", func() review.RiskCheck {
			return review.RiskCheck{Why: "reading REVIEW.md from main: gh api: gh: Server Error (HTTP 503)"}
		}, "--verdicts request-changes-only",
			"PR #9 waits for a person to approve it: its risk check failed: reading REVIEW.md from main: gh api: gh: Server Error (HTTP 503)"},
		{"panicked", func() review.RiskCheck { panic("boom") }, "--verdicts request-changes-only",
			"PR #9 waits for a person to approve it: its risk check failed: the look failed unexpectedly: boom"},
	}
	for _, c := range cases {
		synctest.Test(t, func(t *testing.T) {
			risk := func(_ context.Context, pr prs.PR) review.RiskCheck { return c.check() }
			b := newBench(t, setup{list: byAuthor("pm-sam"), edit: upToMedium, deps: func(d *Deps) { d.Risk = risk }})
			synctest.Wait()
			argv := strings.Join(b.reviewer.command(t, 9).Args, " ")
			if !strings.Contains(argv, c.inArgv) {
				t.Errorf("%s: the prompt is %q, want %q", c.name, argv[max(len(argv)-60, 0):], c.inArgv)
			}
			held := slices.ContainsFunc(b.notes(), func(n string) bool { return strings.HasPrefix(n, "PR #9 waits") })
			if held != (c.note != "") || c.note != "" && !slices.Contains(b.notes(), c.note) {
				t.Errorf("%s: notes %q, want %q", c.name, b.notes(), c.note)
			}
			b.reviewer.finish(t, 9, 0, answer(9, "commented"))
			<-b.results
		})
	}
}

// A named author's PR is approved at any risk, so its script never runs.
func TestANamedAuthorsPRIsNotRiskChecked(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var asked atomic.Int32
		risk := func(context.Context, prs.PR) review.RiskCheck { asked.Add(1); return review.RiskCheck{} }
		b := newBench(t, setup{list: byAuthor("alice"), edit: upToMedium, deps: func(d *Deps) { d.Risk = risk }})
		synctest.Wait()
		argv := strings.Join(b.reviewer.command(t, 9).Args, " ")
		if asked.Load() != 0 || strings.Contains(argv, "--verdicts") || strings.Contains(argv, "--max-risk") {
			t.Errorf("checked %d times; the prompt is %q", asked.Load(), argv[max(len(argv)-60, 0):])
		}
		b.reviewer.finish(t, 9, 0, answer(9, "approved"))
		<-b.results
	})
}

// The look waits for the check, so the review starts with the level the
// script set, and never before it.
func TestTheReviewWaitsForTheRiskCheck(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		risk := func(context.Context, prs.PR) review.RiskCheck {
			<-release
			return review.RiskCheck{Script: "bin/pr-risk", Risk: review.RiskHigh}
		}
		b := newBench(t, setup{list: byAuthor("pm-sam"), edit: upToMedium, deps: func(d *Deps) { d.Risk = risk }})
		synctest.Wait()
		if slices.Contains(b.journal(), "start 9") {
			t.Fatal("the review started before its risk check answered")
		}
		close(release)
		synctest.Wait()
		if argv := strings.Join(b.reviewer.command(t, 9).Args, " "); !strings.Contains(argv, "--verdicts request-changes-only") {
			t.Errorf("the prompt is %q", argv[max(len(argv)-60, 0):])
		}
		b.reviewer.finish(t, 9, 0, answer(9, "commented"))
		<-b.results
	})
}
