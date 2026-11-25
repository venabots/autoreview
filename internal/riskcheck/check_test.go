package riskcheck

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/repo"
	"github.com/venabots/autoreview/internal/review"
)

const (
	reviewMD = "repos/acme/widgets/contents/REVIEW.md"
	scriptAt = "repos/acme/widgets/contents/bin/pr-risk"
	filesAt  = "repos/acme/widgets/pulls/9/files?per_page=100"
)

// github answers each call by the path it asks for. A path with no answer
// is a 404, as GitHub gives for a file that is not there.
type github map[string]struct {
	out string
	err error
}

func (g github) run(_ context.Context, args ...string) ([]byte, error) {
	for _, arg := range args {
		if answer, ok := g[arg]; ok {
			return []byte(answer.out), answer.err
		}
	}
	return nil, errors.New("gh api: gh: Not Found (HTTP 404)")
}

func ok(out string) struct {
	out string
	err error
} {
	return struct {
		out string
		err error
	}{out: out}
}

func fails(msg string) struct {
	out string
	err error
} {
	return struct {
		out string
		err error
	}{err: errors.New(msg)}
}

// script is a fake run of the script: it keeps what it was handed and
// prints out, or fails with err.
type script struct {
	got []Script
	out string
	err error
}

func (s *script) run(_ context.Context, sc Script) ([]byte, error) {
	s.got = append(s.got, sc)
	return []byte(s.out), s.err
}

func pr9() prs.PR { return prs.PR{Number: 9, Base: "main", Head: "abc123"} }

func checker(t *testing.T, g github, s *script) Checker {
	return Checker{GH: g.run, Repo: repo.Context{Owner: "acme", Name: "widgets", Root: "/repo"}, Dir: t.TempDir(), Run: s.run}
}

const names = "# Review guidance\n\nRisk script: `bin/pr-risk`\n"

// The approval of another author's PR waits on this check. A repository
// that names no script has no check, and the review decides. Every other
// way the check can end without a rating holds the approval, a GitHub that
// answers 503 included: read as "no REVIEW.md", it would drop the check on
// the day GitHub is slow.
func TestACheckThatEndsWithoutARatingHoldsTheApproval(t *testing.T) {
	scripted := func(out string) github {
		return github{reviewMD: ok(names), scriptAt: ok("#!/bin/sh\n"), filesAt: ok("a.ts\t\n")}
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
		{"no REVIEW.md", github{}, pr9(), "", nil, true, ""},
		{"no script named", github{reviewMD: ok("# Review guidance\n")}, pr9(), "", nil, true, ""},
		{"GitHub answers 503", github{reviewMD: fails("gh api: gh: Server Error (HTTP 503)")}, pr9(), "", nil, false, "HTTP 503"},
		{"no backticks", github{reviewMD: ok("Risk script: bin/pr-risk\n")}, pr9(), "", nil, false, "not a plain path"},
		{"outside the repository", github{reviewMD: ok("Risk script: `../pr-risk`\n")}, pr9(), "", nil, false, "not a plain path"},
		{"two scripts", github{reviewMD: ok(names + "Risk script: `bin/other`\n")}, pr9(), "", nil, false, "more than one"},
		{"the script is not on the base", github{reviewMD: ok(names)}, pr9(), "", nil, false, "reading bin/pr-risk from main"},
		{"the files do not list", github{reviewMD: ok(names), scriptAt: ok("#!/bin/sh\n"), filesAt: fails("gh api: rate limited")}, pr9(), "", nil, false, "listing the PR's files"},
		{"too many files", github{reviewMD: ok(names), scriptAt: ok("#!/bin/sh\n"), filesAt: ok(strings.Repeat("f.ts\t\n", maxFiles))}, pr9(), "", nil, false, "3000 files or more"},
		{"the script fails", scripted(""), pr9(), "", errors.New("exit status 2: no such command"), false, "bin/pr-risk failed: exit status 2"},
		{"a word that is not a rating", scripted(""), pr9(), "LOWISH\n", nil, false, `printed "LOWISH" first`},
		{"nothing printed", scripted(""), pr9(), "", nil, false, `printed "" first`},
		{"no base", github{}, prs.PR{Number: 9, Head: "abc123"}, "", nil, false, "no base branch"},
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

// The script that runs is the base's copy, in the checkout's top
// directory. It is handed every changed path, the old path of a rename
// too, and the PR it rates; its first line is the rating and the rest is
// why.
func TestTheBasesScriptRatesThePRFromItsChangedPaths(t *testing.T) {
	g := github{
		reviewMD: ok(names),
		scriptAt: ok("#!/bin/sh\necho base copy\n"),
		filesAt:  ok("packages/db/src/migrations/0123.ts\t\napps/api/src/money.ts\tapps/api/src/old-money.ts\n"),
	}
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
	if run.Dir != "/repo" {
		t.Errorf("the script ran in %q", run.Dir)
	}
	if string(run.Stdin) != "packages/db/src/migrations/0123.ts\napps/api/src/money.ts\napps/api/src/old-money.ts\n" {
		t.Errorf("the script read %q", run.Stdin)
	}
	for _, v := range []string{"AUTOREVIEW_REPO=acme/widgets", "AUTOREVIEW_PR=9", "AUTOREVIEW_BASE=main", "AUTOREVIEW_HEAD=abc123"} {
		if !slices.Contains(run.Env, v) {
			t.Errorf("the script's environment %q lacks %s", run.Env, v)
		}
	}
}

// A PR could edit the script to print LOW. The script is read from the
// base, by its path, with the base as the ref.
func TestTheScriptAndREVIEWmdAreReadFromTheBase(t *testing.T) {
	var calls [][]string
	g := github{reviewMD: ok(names), scriptAt: ok("#!/bin/sh\n"), filesAt: ok("a.ts\t\n")}
	record := func(ctx context.Context, args ...string) ([]byte, error) {
		calls = append(calls, args)
		return g.run(ctx, args...)
	}
	c := checker(t, g, &script{out: "LOW\n"})
	c.GH = record
	c.Check(context.Background(), pr9())
	for _, path := range []string{reviewMD, scriptAt} {
		i := slices.IndexFunc(calls, func(args []string) bool { return slices.Contains(args, path) })
		if i < 0 || !slices.Contains(calls[i], "ref=main") {
			t.Errorf("%s was not read from main: %q", path, calls)
		}
	}
}
