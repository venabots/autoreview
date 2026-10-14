package babysit

import (
	"testing"

	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/task"
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
	s, busy := s.Look([]mine.PR{two}, func(int) bool { return true })
	wantKinds(t, busy, nil, "the fix is running")
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

// A push can dismiss an approval, so nothing pushes to an approved PR
// unless a person asks for it: not for a thread, and not for a red build
// either.
func TestAnApprovedPRGetsNoFix(t *testing.T) {
	cases := []struct {
		name string
		edit func(*mine.PR)
	}{
		{"an open thread", func(*mine.PR) {}},
		{"failing checks", func(p *mine.PR) { p.CI = prs.CIFailing }},
		{"an approver where no rule asks for one", func(p *mine.PR) {
			p.Review, p.Reviewers = mine.ReviewNone, []mine.Reviewer{{Login: "alice", Review: mine.ReviewApproved}}
		}},
	}
	for _, c := range cases {
		approved := with(pr(4), func(p *mine.PR) {
			p.Review = mine.ReviewApproved
			c.edit(p)
		})
		s, due := look(babysitting(4), approved)
		wantDue(t, due, nil, "approved, with "+c.name)
		if !s.OnHold(4) {
			t.Errorf("approved, with %s: the PR is not on hold", c.name)
		}
		_, due = look(s, head(approved, "sha2"))
		wantDue(t, due, nil, "approved and changed, with "+c.name)
	}
	if s, _ := look(babysitting(4), pr(4)); s.OnHold(4) {
		t.Error("a PR nobody approved is on hold")
	}
}

// An approved PR with conflicts cannot merge, so a push that dismisses the
// approval loses nothing a person still has. It gets a fix of the
// conflicts only: the threads and the build stay for a person.
func TestAnApprovedPRWithConflictsGetsOnlyAConflictFix(t *testing.T) {
	cases := []struct {
		name string
		edit func(*mine.PR)
	}{
		{"an open thread", func(*mine.PR) {}},
		{"failing checks", func(p *mine.PR) { p.CI, p.ThreadIDs = prs.CIFailing, nil }},
		{"nothing else", func(p *mine.PR) { p.ThreadIDs = nil }},
	}
	for _, c := range cases {
		approved := with(pr(4), func(p *mine.PR) {
			p.Review, p.Merge = mine.ReviewApproved, mine.MergeConflicts
			c.edit(p)
		})
		s, due := lookDue(babysitting(4), approved)
		wantKinds(t, due, []Due{{PR: 4, Kind: task.Conflicts}}, "approved and conflicting, with "+c.name)
		s, due = lookDue(s, approved)
		wantKinds(t, due, nil, "the same conflicts again, with "+c.name)
		if got := s.Fixes(4); got != 1 {
			t.Errorf("approved, with %s: fixes = %d, want 1", c.name, got)
		}
	}
	unapproved := with(pr(4), func(p *mine.PR) { p.Merge = mine.MergeConflicts })
	_, due := lookDue(babysitting(4), unapproved)
	wantKinds(t, due, []Due{{PR: 4, Kind: task.Fix}}, "conflicts on a PR nobody approved")
}

func TestAConflictThatComesAfterTheApprovalGetsAConflictFix(t *testing.T) {
	approved := with(pr(4), func(p *mine.PR) { p.Review = mine.ReviewApproved })
	s, due := lookDue(babysitting(4), approved)
	wantKinds(t, due, nil, "approved, with an open thread")
	conflicts := with(approved, func(p *mine.PR) { p.Merge = mine.MergeConflicts })
	s, due = lookDue(s, conflicts)
	wantKinds(t, due, []Due{{PR: 4, Kind: task.Conflicts}}, "the base moved under the approval")

	s = s.Finished(4)
	merged := with(approved, func(p *mine.PR) { p.Head = "sha-merge" })
	s, due = lookDue(s, merged)
	wantKinds(t, due, nil, "the look after the conflict fix only records it")
	_, due = lookDue(s, with(merged, func(p *mine.PR) { p.Merge = mine.MergeConflicts }))
	wantKinds(t, due, []Due{{PR: 4, Kind: task.Conflicts}}, "a new conflict later")
}

func TestAConflictTheFixLeftOnAnApprovedPRGetsAConflictFix(t *testing.T) {
	s, due := look(babysitting(4), pr(4))
	wantDue(t, due, []int{4}, "the first look")
	s = s.Finished(4)
	approved := with(pr(4), func(p *mine.PR) {
		p.Head, p.Review, p.Merge = "sha-fix", mine.ReviewApproved, mine.MergeConflicts
	})
	_, got := lookDue(s, approved)
	wantKinds(t, got, []Due{{PR: 4, Kind: task.Conflicts}}, "conflicts the fix left, on a PR approved since")
}

func TestAnApprovalStopsTheFixesThatWereRunning(t *testing.T) {
	s, due := look(babysitting(4), pr(4))
	wantDue(t, due, []int{4}, "the first look")
	s = s.Finished(4)
	approved := with(pr(4), func(p *mine.PR) { p.Head, p.Review, p.CI = "sha-fix", mine.ReviewApproved, prs.CIFailing })
	s, due = look(s, approved)
	wantDue(t, due, nil, "a build the fix broke, on a PR approved since")
	_, due = look(s, with(approved, func(p *mine.PR) { p.ThreadIDs = []string{"t1", "t2"} }))
	wantDue(t, due, nil, "a new thread on an approved PR")
}

func TestAPRThatLosesItsApprovalIsFixedAgain(t *testing.T) {
	approved := with(pr(4), func(p *mine.PR) { p.Review = mine.ReviewApproved })
	s, _ := look(babysitting(4), approved)
	asked := with(pr(4), func(p *mine.PR) { p.Review = mine.ReviewChangesRequested })
	s, due := look(s, asked)
	wantDue(t, due, []int{4}, "a reviewer asked for changes after the approval")
	if s.OnHold(4) {
		t.Error("the PR is still on hold")
	}

	alice := mine.Reviewer{Login: "alice", Review: mine.ReviewApproved}
	noRule := with(pr(4), func(p *mine.PR) { p.Review, p.Reviewers = mine.ReviewNone, []mine.Reviewer{alice} })
	s, _ = look(babysitting(4), noRule)
	bob := mine.Reviewer{Login: "bob", Review: mine.ReviewChangesRequested}
	_, due = look(s, with(noRule, func(p *mine.PR) { p.Reviewers = []mine.Reviewer{alice, bob} }))
	wantDue(t, due, []int{4}, "a second reviewer asked for changes where no rule reports it")

	s, _ = look(babysitting(4), approved)
	_, due = look(s, with(pr(4), func(p *mine.PR) { p.Head = "sha-yours" }))
	wantDue(t, due, []int{4}, "your own push dismissed the approval")
}

// A fix a person starts on an approved PR pushes, and the push dismisses
// the approval on some repos. That is the fix's own doing: it must not read
// as a reviewer who acted, or a second fix would follow the first at once.
func TestAnApprovalTheFixsOwnPushDismissedIsNotAReviewer(t *testing.T) {
	approved := with(pr(4), func(p *mine.PR) { p.Review = mine.ReviewApproved })
	s, due := look(babysitting(4), approved)
	wantDue(t, due, nil, "an approved PR")
	s = s.Finished(4)
	dismissed := with(pr(4), func(p *mine.PR) { p.Head, p.Review = "sha-fix", mine.ReviewRequired })
	s, due = look(s, dismissed)
	wantDue(t, due, nil, "the look after the fix only records it")
	_, due = look(s, dismissed)
	wantDue(t, due, nil, "the same state again is not a change")
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
	s, busy := s.Look([]mine.PR{pr(4)}, func(n int) bool { return n == 4 })
	wantKinds(t, busy, nil, "a job on it is running")
	s, due := look(s, pr(4))
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
