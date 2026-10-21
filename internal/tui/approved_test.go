package tui

import (
	"reflect"
	"testing"

	"github.com/venabots/autoreview/internal/babysit"
	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/task"
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
		"threads   2 unresolved, optional now: f answers them",
	)
}

func TestABabysatPRThatIsApprovedSaysOnlyConflictsGetAFix(t *testing.T) {
	m, _ := onMine(t, func(s *engine.Snapshot) {
		approvedWithThreads(s)
		s.Babysit, _ = s.Babysit.Apply(babysit.All(true), s.Mine)
	})
	wantOnScreen(t, m, "✓ #4 · autofix · approved · 2 threads open", "autofix   on · approved, so only conflicts get a fix; f fixes the rest")
}

func TestAnApprovedPRWithNothingLeftDoesNotPointAtTheFixKey(t *testing.T) {
	m, _ := onMine(t, func(s *engine.Snapshot) {
		approvedWithThreads(s)
		s.Mine[0].ThreadIDs = nil
		s.Babysit, _ = s.Babysit.Apply(babysit.All(true), s.Mine)
	})
	wantOnScreen(t, m, "autofix   on · approved, so only conflicts get a fix")
	wantNotOnScreen(t, m, "f fixes the rest")
}

// A fix a person asks for runs on an approved PR. Only autofix holds back.
func TestFFixesAnApprovedPR(t *testing.T) {
	m, hs := onMine(t, approvedWithThreads)
	m = press(t, m, "f")
	want := []engine.Command{engine.RunTask{Request: task.Request{PR: 4, Kind: task.Fix, Title: "My own work", Branch: "me/my-own-work"}}}
	if !reflect.DeepEqual(commands(hs), want) {
		t.Errorf("f sent %+v, want %+v", commands(hs), want)
	}
	wantOnScreen(t, m, "fixing PR #4")
}

// Turning autofix on for an approved PR with work left starts nothing. The
// flash says why, and which key fixes it now.
func TestAOnAnApprovedPRSaysWhyNothingStarts(t *testing.T) {
	m, _ := onMine(t, approvedWithThreads)
	m = pressMine(t, m, "a")
	wantOnScreen(t, m, "autofixing PR #4; it is approved, so autofix fixes only its conflicts: f fixes the rest")
}

func TestAnApprovalWhereNoRuleAsksForOneHasTheApprovedMark(t *testing.T) {
	m, _ := onMine(t, func(s *engine.Snapshot) {
		approvedWithThreads(s)
		s.Mine[0].Review, s.Mine[0].ThreadIDs = mine.ReviewNone, nil
	})
	wantOnScreen(t, m, "✓ #4 · approved")
}

// The view says what the run does: under comments or all, the threads on
// an approved PR are autofix's to fix, and the row does not send the
// person to f for them.
func TestTheAutofixLineFollowsWhatAutofixFixesOnAnApprovedPR(t *testing.T) {
	cases := []struct {
		o        babysit.OnApproved
		redBuild bool
		want     string
		not      string
	}{
		{babysit.ApprovedComments, false, "autofix   on", "approved, so only"},
		{babysit.ApprovedComments, true, "autofix   on · approved, so only conflicts and review comments get a fix; f fixes the rest", ""},
		{babysit.ApprovedAll, true, "autofix   on", "approved, so only"},
	}
	for _, c := range cases {
		m, _ := onMine(t, func(s *engine.Snapshot) {
			approvedWithThreads(s)
			if c.redBuild {
				s.Mine[0].ThreadIDs, s.Mine[0].CI = nil, prs.CIFailing
			}
			s.AutofixApproved = c.o
			s.Babysit, _ = s.Babysit.Apply(babysit.All(true), s.Mine)
		})
		wantOnScreen(t, m, c.want)
		if c.not != "" {
			wantNotOnScreen(t, m, c.not)
		}
	}
}

func TestAOnAnApprovedPRThatAutofixWillFixSaysNothingOfF(t *testing.T) {
	m, _ := onMine(t, func(s *engine.Snapshot) {
		approvedWithThreads(s)
		s.AutofixApproved = babysit.ApprovedComments
	})
	m = pressMine(t, m, "a")
	wantNotOnScreen(t, m, "f fixes the rest")
	wantOnScreen(t, m, "autofix")
}
