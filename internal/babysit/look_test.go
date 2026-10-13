package babysit

import (
	"testing"

	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/prs"
)

func TestABabysatPRThatNeedsWorkIsFixedOnceUntilItChanges(t *testing.T) {
	s := babysitting(4)
	s, due := look(s, pr(4))
	wantDue(t, due, []int{4}, "the first look")
	s, due = look(s, pr(4))
	wantDue(t, due, nil, "the same state again is not a change")
	pushed := with(pr(4), func(p *mine.PR) { p.Head, p.UpdatedAt = "sha2", "2026-10-07T11:00:00Z" })
	s, due = look(s, pushed)
	wantDue(t, due, []int{4}, "a push by somebody is a change")
	if got := s.Fixes(4); got != 2 {
		t.Errorf("fixes = %d, want 2", got)
	}
}

func TestAChangeIsTheHeadTheUpdateTimeTheChecksOrTheMerge(t *testing.T) {
	cases := []struct {
		name string
		edit func(*mine.PR)
	}{
		{"head", func(p *mine.PR) { p.Head = "sha2" }},
		{"updatedAt", func(p *mine.PR) { p.UpdatedAt = "2026-10-07T12:00:00Z" }},
		{"checks", func(p *mine.PR) { p.CI = prs.CIFailing }},
		{"merge", func(p *mine.PR) { p.Merge = mine.MergeConflicts }},
	}
	for _, c := range cases {
		s, _ := look(babysitting(4), pr(4))
		_, due := look(s, with(pr(4), c.edit))
		wantDue(t, due, []int{4}, "a new "+c.name)
	}
	s, _ := look(babysitting(4), pr(4))
	_, due := look(s, with(pr(4), func(p *mine.PR) { p.Title, p.Branch = "Renamed", "me/renamed" }))
	wantDue(t, due, nil, "a new title is not a change")
}

func TestTheFixsOwnPushIsNotAChange(t *testing.T) {
	s, due := look(babysitting(4), pr(4))
	wantDue(t, due, []int{4}, "the first look")
	s = s.Finished(4)
	afterFix := with(pr(4), func(p *mine.PR) { p.Head, p.UpdatedAt = "sha-fix", "2026-10-07T10:05:00Z" })
	s, due = look(s, afterFix)
	wantDue(t, due, nil, "the first look after a fix only records it")
	s, due = look(s, afterFix)
	wantDue(t, due, nil, "the second look sees no change")
	_, due = look(s, with(afterFix, func(p *mine.PR) { p.CI = prs.CIFailing }))
	wantDue(t, due, []int{4}, "its checks failing later is a change")
}

func TestAPRWithNothingToFixIsLeftAlone(t *testing.T) {
	clean := with(pr(4), func(p *mine.PR) { p.ThreadIDs = nil })
	_, due := look(everything(), clean)
	wantDue(t, due, nil, "a clean PR")
	cases := []struct {
		name string
		edit func(*mine.PR)
		want bool
	}{
		{"conflicts", func(p *mine.PR) { p.Merge = mine.MergeConflicts }, true},
		{"failing checks", func(p *mine.PR) { p.CI = prs.CIFailing }, true},
		{"requested changes", func(p *mine.PR) { p.Review = mine.ReviewChangesRequested }, true},
		{"an open thread", func(p *mine.PR) { p.ThreadIDs = []string{"t1"} }, true},
		{"pending checks", func(p *mine.PR) { p.CI = prs.CIPending }, false},
		{"a draft", func(p *mine.PR) { p.Draft = true }, false},
		{"an approval", func(p *mine.PR) { p.Review = mine.ReviewApproved }, false},
	}
	for _, c := range cases {
		if got := NeedsWork(with(clean, c.edit)); got != c.want {
			t.Errorf("NeedsWork with %s = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestTheStreakStopsAtTheCapUntilSomeoneElseActs(t *testing.T) {
	s := babysitting(4)
	var due []int
	for i := range MaxStreak {
		s, due = look(s, head(pr(4), shaN(i)))
		wantDue(t, due, []int{4}, "a fix under the cap")
	}
	if got := s.Streak(4); got != MaxStreak {
		t.Errorf("streak = %d, want %d", got, MaxStreak)
	}
	s, due = look(s, head(pr(4), "sha-more"))
	wantDue(t, due, nil, "the cap holds")
	newThread := with(pr(4), func(p *mine.PR) { p.Head, p.ThreadIDs = "sha-more2", []string{"t1", "t9"} })
	s, due = look(s, newThread)
	wantDue(t, due, []int{4}, "a reviewer's new thread starts the count again")
	if got := s.Streak(4); got != 1 {
		t.Errorf("streak after the reset = %d, want 1", got)
	}
}

func TestAChangedReviewDecisionStartsTheCountAgain(t *testing.T) {
	s := babysitting(4)
	for i := range MaxStreak {
		s, _ = look(s, head(pr(4), shaN(i)))
	}
	asked := with(pr(4), func(p *mine.PR) { p.Head, p.Review = "sha-x", mine.ReviewChangesRequested })
	_, due := look(s, asked)
	wantDue(t, due, []int{4}, "a reviewer asked for changes")
}

func TestAReviewerWhoActsDuringAFixGetsAFix(t *testing.T) {
	s, due := look(babysitting(4), pr(4))
	wantDue(t, due, []int{4}, "the first look")
	s = s.Finished(4)
	newThread := with(pr(4), func(p *mine.PR) { p.Head, p.ThreadIDs = "sha-fix", []string{"t1", "t2"} })
	_, due = look(s, newThread)
	wantDue(t, due, []int{4}, "not swallowed by the look after the fix")
}

func TestTheLookAfterAFixWaitsForChecksToSettle(t *testing.T) {
	asked := with(pr(4), func(p *mine.PR) { p.Review, p.ThreadIDs = mine.ReviewChangesRequested, nil })
	s, due := look(babysitting(4), asked)
	wantDue(t, due, []int{4}, "the first look")
	s = s.Finished(4)
	pending := with(asked, func(p *mine.PR) { p.Head, p.CI, p.Merge = "sha-fix", prs.CIPending, mine.MergeUnknown })
	s, due = look(s, pending)
	wantDue(t, due, nil, "checks still running")
	settled := head(asked, "sha-fix")
	s, due = look(s, settled)
	wantDue(t, due, nil, "checks settling is not a change")
	_, due = look(s, settled)
	wantDue(t, due, nil, "nor is the same state again")
}

func TestANewThreadCountsEvenWhenAnotherClosed(t *testing.T) {
	three := with(pr(4), func(p *mine.PR) { p.ThreadIDs = []string{"a", "b", "c"} })
	s := babysitting(4)
	var due []int
	for i := range MaxStreak {
		s, due = look(s, head(three, shaN(i)))
		wantDue(t, due, []int{4}, "a fix under the cap")
	}
	swapped := with(pr(4), func(p *mine.PR) { p.Head, p.ThreadIDs = "sha-x", []string{"d"} })
	_, due = look(s, swapped)
	wantDue(t, due, []int{4}, "thread d is new, though the count went down")
}

func TestTheCapStartsAgainOnceThePRCameClean(t *testing.T) {
	s := babysitting(4)
	for i := range MaxStreak {
		s, _ = look(s, head(pr(4), shaN(i)))
	}
	clean := with(pr(4), func(p *mine.PR) { p.Head, p.ThreadIDs = "clean", nil })
	s, due := look(s, clean)
	wantDue(t, due, nil, "a clean PR")
	conflicts := with(clean, func(p *mine.PR) { p.Merge = mine.MergeConflicts })
	_, due = look(s, conflicts)
	wantDue(t, due, []int{4}, "new trouble after a clean look")
}

func TestAThreadOpenedWhileTheFixRunsIsNewWorkAfterIt(t *testing.T) {
	s, due := look(babysitting(4), pr(4))
	wantDue(t, due, []int{4}, "the first look")
	two := with(pr(4), func(p *mine.PR) { p.ThreadIDs = []string{"t1", "t2"} })
	s, due = s.Look([]mine.PR{two}, func(int) bool { return true })
	wantDue(t, due, nil, "the fix is running")
	s = s.Finished(4)
	_, due = look(s, head(two, "sha-fix"))
	wantDue(t, due, []int{4}, "t2 arrived during the fix and is new")
}

func TestABuildOrAConflictTheFixCausedGetsAFix(t *testing.T) {
	cases := []struct {
		name string
		edit func(*mine.PR)
	}{
		{"a red build", func(p *mine.PR) { p.CI = prs.CIFailing }},
		{"a conflict", func(p *mine.PR) { p.Merge = mine.MergeConflicts }},
	}
	for _, c := range cases {
		s, _ := look(babysitting(4), pr(4))
		s = s.Finished(4)
		_, due := look(s, with(head(pr(4), "sha-fix"), c.edit))
		wantDue(t, due, []int{4}, c.name+" that was not there before the fix")
	}
}

func TestABuildThatWasRedBeforeTheFixIsItsBaseline(t *testing.T) {
	red := with(pr(4), func(p *mine.PR) { p.CI = prs.CIFailing })
	s, _ := look(babysitting(4), red)
	s = s.Finished(4)
	_, due := look(s, head(red, "sha-fix"))
	wantDue(t, due, nil, "the fix could not mend it, and did not break it")
}

func TestABuildTheFixBrokeAtTheCapGetsNoFix(t *testing.T) {
	s := babysitting(4)
	for i := range MaxStreak {
		s, _ = look(s, head(pr(4), shaN(i)))
	}
	s = s.Finished(4)
	red := with(pr(4), func(p *mine.PR) { p.Head, p.CI = "sha-fix", prs.CIFailing })
	_, due := look(s, red)
	wantDue(t, due, nil, "the cap holds for a fix the fix needs")
}

func TestAPendingBuildDoesNotResetTheCap(t *testing.T) {
	red := func(sha string) mine.PR {
		return with(pr(4), func(p *mine.PR) { p.Head, p.CI, p.ThreadIDs = sha, prs.CIFailing, nil })
	}
	s := babysitting(4)
	var due []int
	for i := range MaxStreak {
		s, due = look(s, red(shaN(i)))
		wantDue(t, due, []int{4}, "a fix under the cap")
		pending := with(pr(4), func(p *mine.PR) { p.Head, p.CI, p.ThreadIDs = "p"+shaN(i), prs.CIPending, nil })
		s, _ = look(s, pending)
	}
	_, due = look(s, red("sha99"))
	wantDue(t, due, nil, "the cap holds through pending checks")
}

func TestACheckThatNeverFinishesDoesNotStopBabysitting(t *testing.T) {
	s, due := look(babysitting(4), pr(4))
	wantDue(t, due, []int{4}, "the first look")
	s = s.Finished(4)
	stuck := with(pr(4), func(p *mine.PR) { p.Head, p.CI = "sha-fix", prs.CIPending })
	for range SettleLooks + 1 {
		s, due = look(s, stuck)
		wantDue(t, due, nil, "a check still running")
	}
	conflicts := with(stuck, func(p *mine.PR) { p.Merge = mine.MergeConflicts })
	_, due = look(s, conflicts)
	wantDue(t, due, []int{4}, "a later conflict still gets a fix")
}

func TestAStaleApprovalDismissedByThePushIsNotAReviewer(t *testing.T) {
	s := babysitting(4)
	var due []int
	for i := range MaxStreak {
		approved := with(pr(4), func(p *mine.PR) { p.Head, p.Review = shaN(i), mine.ReviewApproved })
		s, due = look(s, approved)
		wantDue(t, due, []int{4}, "a fix under the cap")
	}
	dismissed := with(pr(4), func(p *mine.PR) { p.Head, p.Review = "sha-x", mine.ReviewRequired })
	_, due = look(s, dismissed)
	wantDue(t, due, nil, "the cap still holds")
}

func TestAPRFromAForkIsNeverScheduled(t *testing.T) {
	fork := with(pr(4), func(p *mine.PR) { p.CrossRepo = true })
	all, due := look(everything(), fork)
	wantDue(t, due, nil, "a fork")
	if got := all.Fixes(4); got != 0 {
		t.Errorf("fixes on a fork = %d, want 0", got)
	}
	if got := all.Listed([]mine.PR{fork}); len(got) != 0 {
		t.Errorf("a fork is listed as babysat: %v", got)
	}
}

func TestABusyPRWaitsAndAClosedOneLeaves(t *testing.T) {
	s := babysitting(4)
	s, due := s.Look([]mine.PR{pr(4)}, func(n int) bool { return n == 4 })
	wantDue(t, due, nil, "a job on it is running")
	s, due = look(s, pr(4))
	wantDue(t, due, []int{4}, "it is fixed at the next look")
	s, _ = look(s)
	if s.Babysat(4) || s.Any() {
		t.Error("a merged or closed PR is still babysat")
	}
	if got := s.Fixes(4); got != 0 {
		t.Errorf("a closed PR keeps %d fixes", got)
	}
}

func TestDueFollowsTheOrderOfTheList(t *testing.T) {
	_, due := look(babysitting(4, 7, 5), pr(7), pr(5), pr(6), pr(4))
	wantDue(t, due, []int{7, 5, 4}, "three babysat PRs and one not")
}

func TestFinishedOnAPRWithNoHistoryChangesNothing(t *testing.T) {
	s := babysitting(4)
	if got := s.Finished(4); got.watches[4].settle {
		t.Error("a PR babysitting never looked at was marked as just fixed")
	}
}
