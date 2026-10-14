package babysit

import (
	"testing"

	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/task"
)

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
