package babysit

import (
	"testing"

	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/task"
)

// lookOn is one look at list under the setting o, and the fix each PR gets.
func lookOn(o OnApproved, s State, list ...mine.PR) (State, []Due) {
	return s.Look(list, nil, Rules{Cap: MaxStreak, OnApproved: o})
}

func approvedWith(edit func(*mine.PR)) mine.PR {
	return with(pr(4), func(p *mine.PR) {
		p.Review = mine.ReviewApproved
		edit(p)
	})
}

// A review can approve and still leave polish in threads. The person who
// wants that polish done chooses comments or all; the default leaves it.
func TestWhatAutofixFixesOnAnApprovedPRIsTheSetting(t *testing.T) {
	thread := approvedWith(func(*mine.PR) {})
	redBuild := approvedWith(func(p *mine.PR) { p.ThreadIDs, p.CI = nil, prs.CIFailing })
	conflicts := approvedWith(func(p *mine.PR) { p.ThreadIDs, p.Merge = nil, mine.MergeConflicts })
	fix := []Due{{PR: 4, Kind: task.Fix}}
	cases := []struct {
		o    OnApproved
		pr   mine.PR
		what string
		want []Due
	}{
		{ApprovedConflicts, thread, "an open thread", nil},
		{ApprovedConflicts, redBuild, "a red build", nil},
		{ApprovedConflicts, conflicts, "conflicts", []Due{{PR: 4, Kind: task.Conflicts}}},
		{ApprovedComments, thread, "an open thread", fix},
		{ApprovedComments, redBuild, "a red build", nil},
		{ApprovedComments, conflicts, "conflicts", []Due{{PR: 4, Kind: task.Conflicts}}},
		{ApprovedComments, with(thread, func(p *mine.PR) { p.Merge = mine.MergeConflicts }), "a thread and conflicts", fix},
		{ApprovedAll, thread, "an open thread", fix},
		{ApprovedAll, redBuild, "a red build", fix},
		{ApprovedAll, conflicts, "conflicts", fix},
	}
	for _, c := range cases {
		_, due := lookOn(c.o, babysitting(4), c.pr)
		wantKinds(t, due, c.want, c.o.String()+", approved, with "+c.what)
	}
}

// The fix of a thread pushes, and the next look must not take that push
// for a reason to fix again.
func TestAThreadOnAnApprovedPRIsFixedOnceUnderComments(t *testing.T) {
	thread := approvedWith(func(*mine.PR) {})
	s, due := lookOn(ApprovedComments, babysitting(4), thread)
	wantKinds(t, due, []Due{{PR: 4, Kind: task.Fix}}, "the first look")
	s = s.Finished(4)
	left := head(thread, "sha-fix")
	s, due = lookOn(ApprovedComments, s, left)
	wantKinds(t, due, nil, "the look after the fix only records it")
	_, due = lookOn(ApprovedComments, s, left)
	wantKinds(t, due, nil, "the thread the fix left open, again")
}

func TestOnlyAnApprovedPRThatAutofixLeavesAloneIsOnHold(t *testing.T) {
	thread := approvedWith(func(*mine.PR) {})
	for _, c := range []struct {
		o    OnApproved
		want bool
	}{{ApprovedConflicts, true}, {ApprovedComments, true}, {ApprovedAll, false}} {
		s, _ := lookOn(c.o, babysitting(4), thread)
		if got := s.OnHold(4, c.o); got != c.want {
			t.Errorf("%s: OnHold = %v, want %v", c.o, got, c.want)
		}
	}
}

func TestWorkLeftForAPersonFollowsTheSetting(t *testing.T) {
	thread := approvedWith(func(*mine.PR) {})
	redBuild := approvedWith(func(p *mine.PR) { p.ThreadIDs, p.CI = nil, prs.CIFailing })
	cases := []struct {
		o    OnApproved
		pr   mine.PR
		what string
		want bool
	}{
		{ApprovedConflicts, thread, "an open thread", true},
		{ApprovedConflicts, redBuild, "a red build", true},
		{ApprovedComments, thread, "an open thread", false},
		{ApprovedComments, redBuild, "a red build", true},
		{ApprovedAll, redBuild, "a red build", false},
	}
	for _, c := range cases {
		if got := LeftForAPerson(c.pr, c.o); got != c.want {
			t.Errorf("%s, approved, with %s: LeftForAPerson = %v, want %v", c.o, c.what, got, c.want)
		}
	}
}

func TestTheSettingReadsItsThreeWordsAndNothingElse(t *testing.T) {
	for _, o := range []OnApproved{ApprovedConflicts, ApprovedComments, ApprovedAll} {
		if got, err := ParseOnApproved(o.String()); err != nil || got != o {
			t.Errorf("ParseOnApproved(%q) = %v, %v", o.String(), got, err)
		}
	}
	if _, err := ParseOnApproved("polish"); err == nil || err.Error() != `expects conflicts, comments or all, got "polish"` {
		t.Errorf("ParseOnApproved(polish) = %v", err)
	}
}
