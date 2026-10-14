package tui

import (
	"testing"

	"github.com/venabots/autoreview/internal/babysit"
	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/prs"
)

func approvedWithThreads(s *engine.Snapshot) {
	s.Mine = []mine.PR{{
		Number: 4, Title: "My own work", Branch: "me/my-own-work", Merge: mine.MergeClean, CI: prs.CIPassing,
		Review: mine.ReviewApproved, Reviewers: []mine.Reviewer{{Login: "alice", Review: mine.ReviewApproved}},
		ThreadIDs: []string{"T1", "T2"},
	}}
}

// An approved PR needs nothing more from you. The threads that are still
// open are shown, so a person can decide to answer them.
func TestAnApprovedPRWithOpenThreadsReadsAsApproved(t *testing.T) {
	m, _ := onMine(t, approvedWithThreads)
	wantOnScreen(t, m,
		"✓ #4 · approved · 2 threads open",
		"review    approved by @alice",
		"threads   2 unresolved, optional now: c answers them",
	)
}

func TestABabysatPRThatIsApprovedSaysOnlyConflictsGetAFix(t *testing.T) {
	m, _ := onMine(t, func(s *engine.Snapshot) {
		approvedWithThreads(s)
		s.Babysit, _ = s.Babysit.Apply(babysit.All(true), s.Mine)
	})
	wantOnScreen(t, m, "✓ #4 · autofix · approved · 2 threads open", "autofix   on · approved, so only conflicts get a fix")
}

func TestAnApprovalWhereNoRuleAsksForOneHasTheApprovedMark(t *testing.T) {
	m, _ := onMine(t, func(s *engine.Snapshot) {
		approvedWithThreads(s)
		s.Mine[0].Review, s.Mine[0].ThreadIDs = mine.ReviewNone, nil
	})
	wantOnScreen(t, m, "✓ #4 · approved")
}
