package engine

import (
	"slices"
	"testing"
	"testing/synctest"

	"github.com/venabots/autoreview/internal/prs"
)

// A PR named on the command line that --author leaves out is not reviewed.
// The run says who opened it, so the person can see which of the two
// things they asked for won, and the exit fails as for a draft.
func TestANamedPRThatAuthorLeavesOutIsSaidAndFailsTheRun(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		list := func() (prs.Listing, error) {
			return prs.Listing{PRs: []prs.PR{
				{Number: 9, Author: "bob", Engagement: prs.New, CI: prs.CIPassing},
				{Number: 8, Author: "alice", Engagement: prs.New, CI: prs.CIPassing, Hidden: prs.HiddenAuthor},
			}}, nil
		}
		b := newBench(t, setup{list: list, edit: func(c *Config) { c.Only = []int{9, 8} }})
		b.reviewer.finish(t, 9, 0, answer(9, "approved"))
		result := <-b.results

		if result.Code != ExitFailed {
			t.Errorf("code %d, want %d: PR #8 was asked for and not reviewed", result.Code, ExitFailed)
		}
		if want := "PR #8 was asked for but is not reviewed: @alice opened it, and --author leaves it out"; !slices.Contains(b.notes(), want) {
			t.Errorf("notes = %q, want %q", b.notes(), want)
		}
		if got := b.reviewer.started(); !slices.Equal(got, []int{9}) {
			t.Errorf("started %v, want only PR #9", got)
		}
	})
}

func TestTheSnapshotNamesTheAuthorsForTheViews(t *testing.T) {
	cfg := config(0)
	cfg.Authors = []string{"bob"}
	if got := New(cfg, Deps{}).Snapshot().Authors; !slices.Equal(got, []string{"bob"}) {
		t.Errorf("Authors = %q, want [bob]", got)
	}
}
