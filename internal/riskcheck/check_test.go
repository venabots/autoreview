package riskcheck

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/review"
)

// The approval of another author's PR waits on this check. A repository
// that names no script has no check, and the review decides. Every other
// way the check can end without a rating holds the approval, a GitHub that
// answers 503 included: read as "no REVIEW.md", it would drop the check on
// the day GitHub is slow.
func TestACheckThatEndsWithoutARatingHoldsTheApproval(t *testing.T) {
	named := github{lookup: onMain, at("REVIEW.md", trustedCommit): ok(names)}
	branch := func(name, oid string) answer {
		return ok(`{"data":{"repository":{"defaultBranchRef":{"name":"` + name + `","target":{"oid":"` + oid + `"}}}}}`)
	}
	cases := []struct {
		name     string
		gh       github
		pr       prs.PR
		out      string
		runErr   error
		noScript bool
		why      string
	}{
		{"no REVIEW.md", github{lookup: onMain}, pr9(), "", nil, true, ""},
		{"no script named", named.with(at("REVIEW.md", trustedCommit), ok("# Review guidance\n")), pr9(), "", nil, true, ""},
		{"the default branch does not answer", scripted().with(lookup, fails("gh api: gh: Bad Gateway (HTTP 502)")), pr9(), "", nil, false, "asking GitHub for the default branch: gh api: gh: Bad Gateway (HTTP 502)"},
		{"an answer that is not JSON", scripted().with(lookup, ok("<html>")), pr9(), "", nil, false, "reading GitHub's answer for the default branch"},
		{"no default branch", scripted().with(lookup, ok(`{"data":{"repository":{"defaultBranchRef":null}}}`)), pr9(), "", nil, false, "GitHub gave no default branch"},
		{"a branch name for a commit", scripted().with(lookup, branch("main", "main")), pr9(), "", nil, false, `GitHub gave "main" as the commit of main`},
		{"a short commit", scripted().with(lookup, branch("main", "0123456")), pr9(), "", nil, false, `GitHub gave "0123456" as the commit of main`},
		{"GitHub answers 503", named.with(at("REVIEW.md", trustedCommit), fails("gh api: gh: Server Error (HTTP 503)")), pr9(), "", nil, false, "reading REVIEW.md from main at 0123456789ab: gh api: gh: Server Error (HTTP 503)"},
		{"no backticks", named.with(at("REVIEW.md", trustedCommit), ok("Risk script: bin/pr-risk\n")), pr9(), "", nil, false, "not a plain path"},
		{"outside the repository", named.with(at("REVIEW.md", trustedCommit), ok("Risk script: `../pr-risk`\n")), pr9(), "", nil, false, "not a plain path"},
		{"two scripts", named.with(at("REVIEW.md", trustedCommit), ok(names+"Risk script: `bin/other`\n")), pr9(), "", nil, false, "more than one"},
		{"the script is not on the default branch", named, pr9(), "", nil, false, "reading bin/pr-risk from main at 0123456789ab"},
		{"the files do not list", scripted().with(files, fails("gh api: rate limited")), pr9(), "", nil, false, "listing the PR's files"},
		{"too many files", scripted().with(files, ok(strings.Repeat("f.ts\t\n", maxFiles))), pr9(), "", nil, false, "3000 files or more"},
		{"the script fails", scripted(), pr9(), "", errors.New("exit status 2: no such command"), false, "bin/pr-risk failed: exit status 2"},
		{"a word that is not a rating", scripted(), pr9(), "LOWISH\n", nil, false, `printed "LOWISH" first`},
		{"nothing printed", scripted(), pr9(), "", nil, false, `printed "" first`},
		{"no base", scripted(), prs.PR{Number: 9, Head: "abc123"}, "", nil, false, "no base branch"},
	}
	for _, c := range cases {
		s := &script{out: c.out, err: c.runErr}
		got := checker(t, c.gh, s).Check(context.Background(), c.pr)
		if got.NoScript != c.noScript || got.Risk != review.RiskNone || !strings.Contains(got.Why, c.why) {
			t.Errorf("%s: got %+v, want no script %v and a reason that says %q", c.name, got, c.noScript, c.why)
		}
		if holds := got.Holds(review.RiskMedium); holds == c.noScript {
			t.Errorf("%s: holds %v", c.name, holds)
		}
	}
}

// The script that runs is the default branch's copy, in the checkout's top
// directory. It is handed every changed path, the old path of a rename
// too, and the PR it rates; its first line is the rating and the rest is
// why.
func TestTheDefaultBranchsScriptRatesThePRFromItsChangedPaths(t *testing.T) {
	g := scripted().with(files, ok("packages/db/src/migrations/0123.ts\t\napps/api/src/money.ts\tapps/api/src/old-money.ts\n"))
	s := &script{out: "HIGH\npackages/db/src/migrations/0123.ts is a migration\n\nmoney moves\n"}
	got := checker(t, g, s).Check(context.Background(), pr9())

	want := review.RiskCheck{Script: "bin/pr-risk", Risk: review.RiskHigh, Why: "packages/db/src/migrations/0123.ts is a migration; money moves"}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if len(s.got) != 1 {
		t.Fatalf("the script ran %d times", len(s.got))
	}
	run := s.got[0]
	if run.body != "#!/bin/sh\necho default branch copy\n" {
		t.Errorf("the script that ran was %q", run.body)
	}
	if run.Dir != "/repo" {
		t.Errorf("the script ran in %q", run.Dir)
	}
	if string(run.Stdin) != "packages/db/src/migrations/0123.ts\napps/api/src/money.ts\napps/api/src/old-money.ts\n" {
		t.Errorf("the script read %q", run.Stdin)
	}
	for _, v := range []string{"AUTOREVIEW_REPO=acme/widgets", "AUTOREVIEW_PR=9", "AUTOREVIEW_BASE=main", "AUTOREVIEW_HEAD=abc123", "AUTOREVIEW_TRUSTED_REF=" + trustedCommit} {
		if !slices.Contains(run.Env, v) {
			t.Errorf("the script's environment %q lacks %s", run.Env, v)
		}
	}
}

// A stacked PR's base is an ordinary branch, and anyone who can push to it
// can change the copy of the script or of REVIEW.md that it holds. The
// PR is rated by the default branch's copy whatever its base holds, a base
// that drops the Risk script line included: read from the base, that PR
// would have no check at all.
func TestAStackedPRIsRatedByTheDefaultBranchsCopy(t *testing.T) {
	stacked := prs.PR{Number: 9, Base: "feature/below", Head: "abc123"}
	cases := []struct {
		name     string
		gh       github
		noScript bool
		ran      string
	}{
		{"the base swaps the script", scripted().
			with(at("REVIEW.md", "feature/below"), ok(names)).
			with(at("bin/pr-risk", "feature/below"), ok("#!/bin/sh\necho LOW\n")),
			false, "#!/bin/sh\necho default branch copy\n"},
		{"the base names another script", scripted().
			with(at("REVIEW.md", "feature/below"), ok("Risk script: `bin/always-low`\n")).
			with(at("bin/always-low", "feature/below"), ok("#!/bin/sh\necho LOW\n")),
			false, "#!/bin/sh\necho default branch copy\n"},
		{"the base drops the line", scripted().
			with(at("REVIEW.md", "feature/below"), ok("# Review guidance\n")),
			false, "#!/bin/sh\necho default branch copy\n"},
		{"the base has no REVIEW.md", scripted(), false, "#!/bin/sh\necho default branch copy\n"},
		{"only the base names a script", github{lookup: onMain}.
			with(at("REVIEW.md", "feature/below"), ok(names)).
			with(at("bin/pr-risk", "feature/below"), ok("#!/bin/sh\necho LOW\n")),
			true, ""},
	}
	for _, c := range cases {
		s := &script{out: "HIGH\nfrom the default branch\n"}
		got := checker(t, c.gh, s).Check(context.Background(), stacked)
		if c.noScript {
			if !got.NoScript || len(s.got) != 0 {
				t.Errorf("%s: got %+v after %d runs, want no script", c.name, got, len(s.got))
			}
			continue
		}
		if got.Risk != review.RiskHigh || got.Script != "bin/pr-risk" {
			t.Errorf("%s: got %+v, want the default branch's bin/pr-risk to rate it high", c.name, got)
		}
		if len(s.got) != 1 || s.got[0].body != c.ran {
			t.Errorf("%s: ran %+v, want the default branch's copy", c.name, s.got)
			continue
		}
		if !slices.Contains(s.got[0].Env, "AUTOREVIEW_BASE=feature/below") {
			t.Errorf("%s: the script was not told the PR's base: %q", c.name, s.got[0].Env)
		}
	}
}

// Read by a branch's name, REVIEW.md and the script could come from two
// commits, if a push lands between the reads. Both are read at the one
// commit the check began with, whatever the PR's base, and never by name.
func TestTheScriptAndREVIEWmdAreReadAtTheDefaultBranchsCommit(t *testing.T) {
	for _, base := range []string{"main", "feature/below"} {
		var calls [][]string
		g := scripted()
		record := func(ctx context.Context, args ...string) ([]byte, error) {
			calls = append(calls, args)
			return g.run(ctx, args...)
		}
		c := checker(t, g, &script{out: "LOW\n"})
		c.GH = record
		got := c.Check(context.Background(), prs.PR{Number: 9, Base: base, Head: "abc123"})
		if got.Risk != review.RiskLow {
			t.Errorf("base %s: got %+v", base, got)
		}
		if lookups := slices.IndexFunc(calls, func(args []string) bool { return callOf(args) == lookup }); lookups != 0 {
			t.Errorf("base %s: the default branch was asked for at call %d, want first: %q", base, lookups, calls)
		}
		for _, path := range []string{"REVIEW.md", "bin/pr-risk"} {
			read := slices.IndexFunc(calls, func(args []string) bool { return callOf(args) == at(path, trustedCommit) })
			if read < 0 {
				t.Errorf("base %s: %s was not read at %s: %q", base, path, trustedCommit, calls)
			}
		}
		for _, args := range calls {
			if slices.Contains(args, "ref="+base) {
				t.Errorf("base %s: a file was read by the branch's name: %q", base, args)
			}
		}
	}
}
