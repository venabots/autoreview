package tui

import (
	"reflect"
	"testing"

	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/task"
)

func TestFAsksTheEngineToFixEveryPRThatNeedsIt(t *testing.T) {
	m, hs := onMine(t, nil)
	m = pressMine(t, m, "F")
	if want := []engine.Command{engine.FixAll{}}; !reflect.DeepEqual(commands(hs), want) {
		t.Errorf("F sent %+v, want %+v", commands(hs), want)
	}
	wantOnScreen(t, m, "fixing 2 PRs of yours")
}

func TestFIsRefusedInTheViewWhenThereIsNothingToSend(t *testing.T) {
	clean := func(s *engine.Snapshot) {
		s.Mine = s.Mine[1:]
		s.Mine[0].ThreadIDs = nil
	}
	refused := func(s *engine.Snapshot) {
		s.Refusals = map[task.Kind]string{task.Fix: "--no-post leaves every PR alone"}
	}
	cases := []struct {
		name string
		edit func(*engine.Snapshot)
		want string
	}{
		{"a run that may not change PRs", refused, "not fixing your PRs: --no-post leaves every PR alone"},
		{"no PR that needs a fix", clean, "none of your PRs needs a fix now"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, hs := onMine(t, c.edit)
			m = pressMine(t, m, "F")
			if len(commands(hs)) != 0 {
				t.Errorf("F sent %+v", commands(hs))
			}
			wantOnScreen(t, m, c.want)
		})
	}
}

func TestFOnTheReviewTabSaysWhereItWorks(t *testing.T) {
	m, hs := newModel(t, 150, 30)
	m = pressMine(t, m, "F")
	if len(commands(hs)) != 0 {
		t.Errorf("F sent %+v from the Review tab", commands(hs))
	}
	wantOnScreen(t, m, "work on the Maintain tab")
}
