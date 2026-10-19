package tui

import (
	"reflect"
	"testing"

	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/prs"
)

func withBump(pushed bool) func(*engine.Snapshot) {
	return func(s *engine.Snapshot) {
		s.MineBots = true
		s.Mine = append([]mine.PR{{
			Number: 12, Title: "chore(deps): bump left-pad", Branch: "dependabot/npm_and_yarn/left-pad-2.0.0",
			CI: prs.CIFailing, Merge: mine.MergeClean, Bot: true, OthersPushed: pushed,
		}}, s.Mine...)
	}
}

func TestDAsksTheEngineToShowTheBotsPRsAndAgainToHideThem(t *testing.T) {
	m, hs := onMine(t, nil)
	m = press(t, m, "d")
	if want := []engine.Command{engine.IncludeBots{On: true}}; !reflect.DeepEqual(commands(hs), want) {
		t.Errorf("d sent %+v, want %+v", commands(hs), want)
	}
	wantOnScreen(t, m, "showing Dependabot's PRs")

	m, hs = onMine(t, withBump(false))
	press(t, m, "d")
	if want := []engine.Command{engine.IncludeBots{On: false}}; !reflect.DeepEqual(commands(hs), want) {
		t.Errorf("d with the bots shown sent %+v, want %+v", commands(hs), want)
	}
}

func TestDOnTheReviewTabSaysWhereItWorks(t *testing.T) {
	m, hs := newModel(t, 150, 30)
	m = press(t, m, "d")
	if len(commands(hs)) != 0 {
		t.Errorf("d on Review sent %+v, want nothing", commands(hs))
	}
	wantOnScreen(t, m, "tab shows them")
}

func TestTheMaintainTabSaysItShowsTheBotsPRs(t *testing.T) {
	m, _ := onMine(t, nil)
	wantNotOnScreen(t, m, "+dependabot")
	m, _ = onMine(t, withBump(false))
	wantOnScreen(t, m, "Maintain 3 · +dependabot")
}

func TestABotsPRSaysWhoOpenedItInItsRowAndDetail(t *testing.T) {
	m, _ := onMine(t, withBump(false))
	m = press(t, m, "g")
	wantOnScreen(t, m, "@dependabot dependabot/npm_and_yarn/left-", "from      Dependabot")
	wantNotOnScreen(t, m, "no longer rebases it")
}

func TestABotsPRThatWasPushedToSaysDependabotNoLongerRebasesIt(t *testing.T) {
	m, _ := onMine(t, withBump(true))
	m = press(t, m, "g")
	wantOnScreen(t, m, "pushed to by us: Dependabot no longer rebases it")
}

func TestTheMaintainFooterAndKeyHelpNameD(t *testing.T) {
	m, _ := newModel(t, 250, 30)
	m = press(t, m, "tab")
	wantOnScreen(t, m, "d dependabot")
	m = press(t, m, "?")
	wantOnScreen(t, m, "show Dependabot's PRs too")
}
