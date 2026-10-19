package babysit

import (
	"slices"
	"testing"

	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/task"
)

// bump is a Dependabot PR whose build fails.
func bump(n int) mine.PR {
	return with(pr(n), func(p *mine.PR) {
		p.Branch, p.ThreadIDs, p.CI, p.Bot = "dependabot/npm_and_yarn/left-pad-2.0.0", nil, prs.CIFailing, true
	})
}

// Autofix on for all is on when the view opens. Showing Dependabot's PRs
// must not start a fix on every failing bump at once: a bot's PR gets
// autofix only when a person turns it on for that PR.
func TestAutofixForAllLeavesABotsPRAlone(t *testing.T) {
	s, due := look(everything(), pr(4), bump(12))
	wantDue(t, due, []int{4}, "all on, with a bot's PR that needs work")
	if s.Covers(bump(12)) {
		t.Error("autofix for all covers a bot's PR")
	}
	if got := s.Listed([]mine.PR{pr(4), bump(12)}); len(got) != 1 || got[0].PR != 4 {
		t.Errorf("Listed = %+v, want only your PR #4", got)
	}
}

func TestAOnABotsPRAutofixesIt(t *testing.T) {
	for _, start := range []struct {
		name  string
		state State
	}{{"nothing on", State{}}, {"all on", everything()}} {
		t.Run(start.name, func(t *testing.T) {
			c, err := start.state.Toggle(bump(12))
			if err != nil || !c.On {
				t.Fatalf("a on the bot's PR = %+v, %v; want a change that turns it on", c, err)
			}
			s, err := start.state.Apply(c, []mine.PR{pr(4), bump(12)})
			if err != nil {
				t.Fatal(err)
			}
			_, due := look(s, bump(12))
			wantDue(t, due, []int{12}, "the bot's PR after a on it")
		})
	}
}

func TestAAgainOnABotsPRStopsItsAutofix(t *testing.T) {
	list := []mine.PR{pr(4), bump(12)}
	s, _ := everything().Apply(One(12, true), list)
	c, err := s.Toggle(bump(12))
	if err != nil || c.On {
		t.Fatalf("a again = %+v, %v; want a change that turns it off", c, err)
	}
	s, _ = s.Apply(c, list)
	if s.Covers(bump(12)) || !s.Covers(pr(4)) {
		t.Errorf("after a again: bot covered %v, your PR covered %v; want false, true", s.Covers(bump(12)), s.Covers(pr(4)))
	}
}

// Stopping one PR while all are on keeps the rest of your PRs on. It must
// not turn autofix on for the bots' PRs that were listed beside them.
func TestStoppingOneOfAllDoesNotStartTheBotsPRs(t *testing.T) {
	list := []mine.PR{pr(4), pr(5), bump(12)}
	s, _ := everything().Apply(One(4, false), list)
	if s.Covers(pr(4)) || !s.Covers(pr(5)) || s.Covers(bump(12)) {
		t.Errorf("after stopping #4 of all: 4 %v, 5 %v, bot 12 %v; want false, true, false",
			s.Covers(pr(4)), s.Covers(pr(5)), s.Covers(bump(12)))
	}
}

// F is a key a person presses on what the tab shows, so it fixes a bot's
// PR that needs work, as it fixes yours.
func TestFixAllFixesABotsPRThatNeedsWork(t *testing.T) {
	got := ToFix([]mine.PR{bump(12), with(bump(13), func(p *mine.PR) { p.CI = prs.CIPassing })}, nil)
	if want := []Due{{PR: 12, Kind: task.Fix}}; !slices.Equal(got, want) {
		t.Errorf("ToFix = %+v, want %+v", got, want)
	}
}
