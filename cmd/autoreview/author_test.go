package main

import (
	"strings"
	"testing"
)

// The sandbox lists #9 by alice, #8 by bob, #6 by carol and the approved
// #5 by dave. A run with no flag reviews #9 and #8.
func TestAuthorLimitsTheRunToThePRsThatLoginOpened(t *testing.T) {
	fixtures := sandbox(t)
	stdout, stderr, code := autoreview(t, "--author=BOB", "--jobs=1")

	if code != 0 || !strings.Contains(stdout, "review list: 1 PR by @BOB\n  #8  ready  @bob  ") {
		t.Errorf("exit %d, stdout:\n%s\nstderr:\n%s\nwant only bob's #8, found though the case differs", code, stdout, stderr)
	}
	calls := dashpCalls(t, fixtures)
	if len(calls) != 1 || !strings.HasSuffix(calls[0], "-- /auto-review 8") {
		t.Errorf("reviews started: %q, want PR #8 and no other", calls)
	}
}

func TestAuthorThatNamesOnlyYouIsRefused(t *testing.T) {
	fixtures := sandbox(t)
	_, stderr, code := autoreview(t, "--author=me")

	if code != 1 || !strings.HasPrefix(stderr, "error: --author names only you (@me), and your own PRs are never reviewed") {
		t.Errorf("exit %d and stderr %q, want a refusal", code, stderr)
	}
	if calls := dashpCalls(t, fixtures); len(calls) != 0 {
		t.Errorf("reviews started: %q, want none", calls)
	}
}
