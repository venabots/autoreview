package words

import (
	"testing"
	"time"

	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/queue"
	"github.com/venabots/autoreview/internal/rebase"
)

func TestARowThatWaitsOnARebaseSaysWhoItWaitsFor(t *testing.T) {
	cases := []struct {
		state rebase.State
		want  string
	}{
		{rebase.Checking, "checking for a rebase"},
		{rebase.Asked, "asked Dependabot to rebase"},
		{rebase.AskedBefore, "asked Dependabot to rebase"},
		{rebase.WouldAsk, "would ask Dependabot to rebase"},
		{rebase.OthersCommitted, "needs a person"},
	}
	for _, c := range cases {
		row := queue.Row{PR: prs.PR{Number: 3}, Wait: queue.WaitRebase, Rebase: c.state}
		if got := Wait(row, time.Time{}); got != c.want {
			t.Errorf("%v: Wait is %q, want %q", c.state, got, c.want)
		}
	}
}

func TestTheRebaseNoteSaysWhatWasAskedAndWhy(t *testing.T) {
	conflicts := prs.PR{Number: 3, Base: "main", Conflicts: true}
	failing := prs.PR{Number: 3, Base: "main", CI: prs.CIFailing}
	cases := []struct {
		pr    prs.PR
		state rebase.State
		want  string
	}{
		{conflicts, rebase.Asked, "asked Dependabot to rebase PR #3: it conflicts with main"},
		{failing, rebase.Asked, "asked Dependabot to rebase PR #3: its checks fail and main has moved on"},
		{conflicts, rebase.WouldAsk, "would ask Dependabot to rebase PR #3: it conflicts with main (--no-post)"},
		{conflicts, rebase.AskedBefore, "PR #3 waits for Dependabot: you asked it to rebase already"},
		{conflicts, rebase.OthersCommitted, "PR #3 needs a person: somebody other than Dependabot committed to it, so Dependabot will not rebase it"},
		{failing, rebase.NotBehind, ""},
		{conflicts, rebase.Checking, ""},
	}
	for _, c := range cases {
		if got := RebaseNote(c.pr, c.state); got != c.want {
			t.Errorf("%v: RebaseNote is %q, want %q", c.state, got, c.want)
		}
	}
}
