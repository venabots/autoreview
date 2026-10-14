package babysit

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/venabots/autoreview/internal/mine"
)

func TestStoppingOneOfAllNeverBabysitsAFork(t *testing.T) {
	list := []mine.PR{pr(4), with(pr(5), func(p *mine.PR) { p.CrossRepo = true })}
	s, err := everything().Apply(One(4, false), list)
	if err != nil {
		t.Fatal(err)
	}
	if s.Any() {
		t.Error("the fork is left behind to keep the run looking")
	}
}

func TestAllCoversPRsOpenedLaterAndStoppingOneKeepsTheRest(t *testing.T) {
	s := everything()
	if !s.Babysat(99) {
		t.Error("a PR opened later is not babysat")
	}
	list := []mine.PR{pr(4), pr(5)}
	s, _ = s.Apply(One(4, false), list)
	if s.All() || s.Babysat(4) || !s.Babysat(5) {
		t.Errorf("after stopping 4 of all: all %v, 4 %v, 5 %v; want false, false, true", s.All(), s.Babysat(4), s.Babysat(5))
	}
	if got, want := s.Listed(list), []Entry{{PR: 5}}; !reflect.DeepEqual(got, want) {
		t.Errorf("listed %v, want %v", got, want)
	}
	s, _ = s.Apply(All(false), list)
	if s.Any() {
		t.Error("B off left something babysat")
	}
}

func TestStoppingOnePRForgetsItsHistory(t *testing.T) {
	s, _ := look(babysitting(4, 5), pr(4), pr(5))
	s, _ = s.Apply(One(4, false), nil)
	if s.Fixes(4) != 0 || s.Fixes(5) != 1 {
		t.Errorf("fixes after stopping 4: 4 has %d, 5 has %d; want 0 and 1", s.Fixes(4), s.Fixes(5))
	}
}

func TestBabysittingAllKeepsWhatEachPRHasHad(t *testing.T) {
	s, _ := look(babysitting(4), pr(4))
	s, _ = s.Apply(All(true), nil)
	if s.Fixes(4) != 1 || s.Streak(4) != 1 {
		t.Errorf("after B: fixes %d, streak %d; want 1 and 1", s.Fixes(4), s.Streak(4))
	}
}

func TestStopEndsAllBabysitting(t *testing.T) {
	s, _ := look(babysitting(4), pr(4))
	s, _ = s.Apply(All(true), nil)
	s = s.Stop()
	if s.Any() || s.All() || s.Babysat(4) || s.Fixes(4) != 0 {
		t.Error("turning the looking off left babysitting on")
	}
}

func TestBRefusesAForkEvenWithAllBabysat(t *testing.T) {
	fork := with(pr(4), func(p *mine.PR) { p.CrossRepo = true })
	for _, s := range []State{{}, everything()} {
		_, err := s.Toggle(fork)
		var refused ForkError
		if !errors.As(err, &refused) || refused.PR != 4 {
			t.Fatalf("Toggle on a fork: err %v, want a ForkError for 4", err)
		}
	}
	want := "PR #4 is from a fork; its branch is not on origin, so nothing can fix it here"
	if got := (ForkError{PR: 4}).Error(); got != want {
		t.Errorf("refusal %q, want %q", got, want)
	}
}

func TestBTurnsOnePROnAndOff(t *testing.T) {
	c, err := State{}.Toggle(pr(4))
	if err != nil || c != One(4, true) {
		t.Fatalf("b on an unbabysat PR: %+v %v, want One(4, true)", c, err)
	}
	s, _ := State{}.Apply(c, nil)
	if c, _ = s.Toggle(pr(4)); c != One(4, false) {
		t.Errorf("b on a babysat PR: %+v, want One(4, false)", c)
	}
	if c, _ = everything().Toggle(pr(4)); c != One(4, false) {
		t.Errorf("b with all babysat: %+v, want One(4, false)", c)
	}
}

func TestBigBTurnsAllOnAndOff(t *testing.T) {
	if got := (State{}).ToggleAll(); got != All(true) {
		t.Errorf("B with all off: %+v, want All(true)", got)
	}
	if got := everything().ToggleAll(); got != All(false) {
		t.Errorf("B with all on: %+v, want All(false)", got)
	}
}

func TestApplyRefusesToBabysitAFork(t *testing.T) {
	list := []mine.PR{with(pr(4), func(p *mine.PR) { p.CrossRepo = true })}
	s, err := State{}.Apply(One(4, true), list)
	if !errors.As(err, new(ForkError)) {
		t.Errorf("err %v, want a ForkError", err)
	}
	if s.Any() {
		t.Error("a refused fork is babysat")
	}
	if _, err := (State{}).Apply(One(4, false), list); err != nil {
		t.Errorf("stopping a fork is refused: %v", err)
	}
}

func TestNoMethodChangesItsReceiverOrItsList(t *testing.T) {
	list := []mine.PR{pr(4), pr(5)}
	kept := slices.Clone(list)
	before, _ := look(babysitting(4, 5), list...)
	snapshot := func(s State) []any {
		return []any{s.All(), s.Babysat(4), s.Babysat(5), s.Fixes(4), s.Streak(4), s.watches[4].settle, s.watches[4].waited}
	}
	want := snapshot(before)
	steps := map[string]func(){
		"Look":         func() { before.Look([]mine.PR{head(pr(4), "sha2")}, nil, MaxStreak) },
		"Look closing": func() { before.Look(nil, nil, MaxStreak) },
		"Finished":     func() { before.Finished(4) },
		"Apply one on": func() { _, _ = before.Apply(One(6, true), list) },
		"Apply one off": func() {
			_, _ = before.Apply(One(4, false), list)
		},
		"Apply all on":  func() { _, _ = before.Apply(All(true), list) },
		"Apply all off": func() { _, _ = before.Apply(All(false), list) },
		"Stop":          func() { before.Stop() },
	}
	for name, step := range steps {
		step()
		if got := snapshot(before); !reflect.DeepEqual(got, want) {
			t.Errorf("%s changed its receiver: %v, want %v", name, got, want)
		}
	}
	if _, ok := before.prs[6]; ok {
		t.Error("Apply wrote into its receiver's set")
	}
	if !reflect.DeepEqual(list, kept) {
		t.Error("a method changed the list it was given")
	}
	settled := before.Finished(4)
	settled.Look(list, nil, MaxStreak)
	if !settled.watches[4].settle {
		t.Error("Look cleared the settle flag on its receiver")
	}
}

func TestTheZeroStateBabysitsNothing(t *testing.T) {
	var s State
	if s.Any() || s.All() || s.Babysat(4) || s.Fixes(4) != 0 || s.Listed([]mine.PR{pr(4)}) != nil {
		t.Error("the zero state babysits something")
	}
	if _, due := look(s, pr(4)); due != nil {
		t.Errorf("the zero state fixes %v", due)
	}
}

func TestEachChangeSaysWhatItDoes(t *testing.T) {
	cases := map[Change]string{
		One(4, true):  "autofixing PR #4: it is fixed when it changes and needs work",
		One(4, false): "autofix is off for PR #4",
		All(true):     "autofixing all your PRs, and any you open: each is fixed when it changes and needs work",
		All(false):    "autofix is off for your PRs",
	}
	for c, want := range cases {
		if got := c.Said(); got != want {
			t.Errorf("%+v said %q, want %q", c, got, want)
		}
	}
}
