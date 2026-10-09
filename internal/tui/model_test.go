package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/queue"
	"github.com/venabots/autoreview/internal/repo"
)

// fakeEngine is a run that does nothing by itself. A test sets its
// snapshot and reads the commands the view sent.
type fakeEngine struct {
	snap    *engine.Snapshot
	changed chan struct{}
	got     []engine.Command
}

func (f *fakeEngine) Snapshot() *engine.Snapshot { return f.snap }
func (f *fakeEngine) Changed() <-chan struct{}   { return f.changed }
func (f *fakeEngine) Do(c engine.Command)        { f.got = append(f.got, c) }

var start = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

func snapshot() *engine.Snapshot {
	return &engine.Snapshot{
		Seq:        3,
		Phase:      engine.Idle,
		Repo:       repo.Context{Owner: "acme", Name: "widgets"},
		Watch:      2 * time.Minute,
		NextLookAt: start.Add(90 * time.Second),
		Looked:     true,
		Reviews: []queue.Row{
			{PR: prs.PR{Number: 9, Title: "Add retry logic", Author: "alice", Branch: "alice/retry", Base: "main", CI: prs.CIPassing}},
			{PR: prs.PR{Number: 8, Title: "Fix the cache", Author: "bob", Branch: "bob/cache", Base: "main", CI: prs.CIPending, Decision: prs.ChangesRequested}, Wait: queue.WaitChecks},
			{PR: prs.PR{Number: 6, Title: "Tidy the docs", Author: "carol", Branch: "carol/docs", Base: "main", Engagement: prs.Seen}, Wait: queue.WaitSeen},
			{PR: prs.PR{Number: 5, Title: "Bump the SDK", Author: "dave", Branch: "dave/sdk", Base: "main", Decision: prs.Approved}, Wait: queue.WaitApproved},
		},
		MineLooked: true,
		Mine: []mine.PR{
			{Number: 4, Title: "My own work", Branch: "me/my-own-work", Merge: mine.MergeConflicts, CI: prs.CIPassing,
				Reviewers: []mine.Reviewer{{Login: "alice", Review: mine.ReviewApproved}}, Review: mine.ReviewApproved},
			{Number: 3, Title: "Second thing", Branch: "me/second", Merge: mine.MergeClean, ThreadIDs: []string{"T1", "T2"}},
		},
	}
}

type harness struct {
	eng    *fakeEngine
	opened []int
	openEr error
	tabs   []string
	tabErr error
}

func newModel(t *testing.T, w, h int) (Model, *harness) {
	t.Helper()
	hs := &harness{eng: &fakeEngine{snap: snapshot(), changed: make(chan struct{}, 1)}}
	act := Actions{
		Open: func(_ context.Context, pr int) error {
			hs.opened = append(hs.opened, pr)
			return hs.openEr
		},
		OpenTab: func(_ context.Context, label, command string) (string, error) {
			hs.tabs = append(hs.tabs, label+": "+command)
			return "cmux", hs.tabErr
		},
		ResumeCommand: func(dir, reopen string) string { return "cd " + dir + " && " + reopen },
	}
	m := New(hs.eng, act)
	m.now = start
	return send(m, tea.WindowSizeMsg{Width: w, Height: h}), hs
}

func send(m Model, msg tea.Msg) Model {
	next, _ := m.Update(msg)
	return next.(Model)
}

var keys = map[string]tea.KeyPressMsg{
	"j":      {Code: 'j', Text: "j"},
	"k":      {Code: 'k', Text: "k"},
	"g":      {Code: 'g', Text: "g"},
	"G":      {Code: 'g', Text: "G", Mod: tea.ModShift},
	"o":      {Code: 'o', Text: "o"},
	"m":      {Code: 'm', Text: "m"},
	"q":      {Code: 'q', Text: "q"},
	"?":      {Code: '?', Text: "?"},
	"x":      {Code: 'x', Text: "x"},
	"z":      {Code: 'z', Text: "z"},
	"r":      {Code: 'r', Text: "r"},
	"R":      {Code: 'r', Text: "R", Mod: tea.ModShift},
	"f":      {Code: 'f', Text: "f"},
	"l":      {Code: 'l', Text: "l"},
	"enter":  {Code: tea.KeyEnter},
	"tab":    {Code: tea.KeyTab},
	"esc":    {Code: tea.KeyEscape},
	"down":   {Code: tea.KeyDown},
	"left":   {Code: tea.KeyLeft},
	"right":  {Code: tea.KeyRight},
	"space":  {Code: tea.KeySpace, Text: " "},
	"ctrl+c": {Code: 'c', Mod: tea.ModCtrl},
	"ctrl+d": {Code: 'd', Mod: tea.ModCtrl},
	"ctrl+u": {Code: 'u', Mod: tea.ModCtrl},
}

func press(t *testing.T, m Model, names ...string) Model {
	t.Helper()
	for _, name := range names {
		key, ok := keys[name]
		if !ok {
			t.Fatalf("the test has no key named %q", name)
		}
		m = send(m, key)
	}
	return m
}

func screen(m Model) string {
	return ansi.Strip(m.View().Content)
}

func wantOnScreen(t *testing.T, m Model, wants ...string) {
	t.Helper()
	got := screen(m)
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("the screen does not show %q:\n%s", want, got)
		}
	}
}

func wantNotOnScreen(t *testing.T, m Model, unwanted ...string) {
	t.Helper()
	got := screen(m)
	for _, text := range unwanted {
		if strings.Contains(got, text) {
			t.Errorf("the screen shows %q:\n%s", text, got)
		}
	}
}

func TestTheKeyNamesMatchWhatBubbleTeaSends(t *testing.T) {
	want := map[string]intent{
		"j": intentDown, "k": intentUp, "g": intentTop, "G": intentBottom, "down": intentDown,
		"left": intentFirstTab, "right": intentSecondTab,
		"o": intentOpen, "m": intentMouse, "q": intentQuit, "?": intentKeys, "x": intentStop, "z": intentNone,
		"r": intentResume, "R": intentReviewNow, "f": intentFocus, "l": intentLog, "enter": intentNone,
		"tab": intentSwitchTab, "esc": intentBack, "space": intentPageDown,
		"ctrl+c": intentInterrupt, "ctrl+d": intentPageDown, "ctrl+u": intentPageUp,
	}
	for name, key := range keys {
		if got := intentFor(key); got != want[name] {
			t.Errorf("%s (which Bubble Tea names %q) asks for %d, want %d", name, key.String(), got, want[name])
		}
	}
}

func TestTheFirstFrameShowsTheRunAndTheFirstPR(t *testing.T) {
	m, _ := newModel(t, 120, 30)
	wantOnScreen(t, m,
		"autoreview · acme/widgets · read-only · lists every 2m",
		" Review 4 ", " My PRs 2 ",
		"○ #9 · ready", "@alice alice/retry",
		"✗ #8 · CI pending", "○ #6 · seen", "✓ #5 · approved",
		"#9 Add retry logic", "@alice · alice/retry",
		"checks    passing", "activity  new to you", "base      main",
		"2 waiting · next look in 1m30s",
		"q quit", "? keys",
	)
}

func TestTheTabBarDoesNotExplainTheTabKey(t *testing.T) {
	m, _ := newModel(t, 120, 30)
	wantNotOnScreen(t, m, "tab switches")
	wantOnScreen(t, m, "tab my PRs")
}

func TestEveryFrameFillsTheScreenExactly(t *testing.T) {
	for _, size := range [][2]int{{120, 30}, {90, 10}, {89, 24}, {80, 24}, {40, 12}, {20, 5}, {10, 3}, {10, 2}, {10, 1}, {200, 60}} {
		m, _ := newModel(t, size[0], size[1])
		for _, state := range []Model{m, press(t, m, "tab"), press(t, m, "?")} {
			lines := strings.Split(state.View().Content, "\n")
			if len(lines) != size[1] {
				t.Errorf("%dx%d: the frame has %d lines", size[0], size[1], len(lines))
			}
			for i, l := range lines {
				if w := ansi.StringWidth(l); w != size[0] {
					t.Errorf("%dx%d: line %d is %d columns wide: %q", size[0], size[1], i, w, ansi.Strip(l))
				}
			}
		}
	}
}

func TestTheViewDrawsNothingBeforeItKnowsTheSize(t *testing.T) {
	hs := &fakeEngine{snap: snapshot(), changed: make(chan struct{}, 1)}
	if got := New(hs, Actions{}).View().Content; got != "" {
		t.Errorf("before a size arrives the frame is %q, want nothing", got)
	}
}

func TestMovingTheSelectionChangesTheDetail(t *testing.T) {
	m, _ := newModel(t, 120, 30)

	m = press(t, m, "j")
	wantOnScreen(t, m, "#8 Fix the cache", "held until its checks pass (checks pending); next look in 1m30s", "review    changes requested")

	m = press(t, m, "G")
	wantOnScreen(t, m, "#5 Bump the SDK", "approved already; the sweep leaves those alone")

	m = press(t, m, "j")
	wantOnScreen(t, m, "#5 Bump the SDK")

	m = press(t, m, "g", "k")
	wantOnScreen(t, m, "#9 Add retry logic")
}

func TestTabShowsYourPRsAndEachTabKeepsItsSelection(t *testing.T) {
	m, _ := newModel(t, 120, 30)
	m = press(t, m, "j", "tab")
	wantOnScreen(t, m,
		"✓ #4 · conflicts", "me/my-own-work", "○ #3 · 2 threads",
		"#4 My own work", "review    approved by @alice", "merge     conflicts with its base",
		"tab review",
	)
	wantNotOnScreen(t, m, "@alice alice/retry")

	m = press(t, m, "j")
	wantOnScreen(t, m, "#3 Second thing", "threads   2 unresolved", "merge     merges cleanly")

	m = press(t, m, "tab")
	wantOnScreen(t, m, "#8 Fix the cache", "tab my PRs")

	m = press(t, m, "tab")
	wantOnScreen(t, m, "#3 Second thing")
}

func TestQuestionMarkShowsTheKeysAndEscPutsThemAway(t *testing.T) {
	m, _ := newModel(t, 120, 30)

	m = press(t, m, "?")
	wantOnScreen(t, m, "KEYS", "open the selected PR in the browser", "○ #9 · ready")
	wantNotOnScreen(t, m, "#9 Add retry logic")

	wantOnScreen(t, press(t, m, "esc"), "#9 Add retry logic")
	wantOnScreen(t, press(t, m, "?"), "#9 Add retry logic")
}

func TestTheSelectionStaysOnItsPRWhenTheRowsMove(t *testing.T) {
	m, hs := newModel(t, 120, 30)
	m = press(t, m, "j")

	next := snapshot()
	next.Reviews = []queue.Row{next.Reviews[2], next.Reviews[0], next.Reviews[1]}
	hs.eng.snap = next
	m = send(m, snapshotMsg{snap: next})
	wantOnScreen(t, m, "#8 Fix the cache")

	gone := snapshot()
	gone.Reviews = gone.Reviews[2:]
	m = send(m, snapshotMsg{snap: gone})
	wantOnScreen(t, m, "#6 Tidy the docs", " Review 2 ")
}

func TestQuitAsksTheEngineAndClosesWhenTheRunHasEnded(t *testing.T) {
	for _, key := range []string{"q", "ctrl+c"} {
		t.Run(key, func(t *testing.T) {
			m, hs := newModel(t, 120, 30)

			next, cmd := m.Update(keys[key])
			m = next.(Model)
			if len(hs.eng.got) != 1 || hs.eng.got[0] != (engine.Quit{}) {
				t.Fatalf("the engine was sent %v, want one Quit", hs.eng.got)
			}
			if cmd != nil {
				t.Error("the view closed before the engine said the run had ended")
			}
			wantOnScreen(t, m, "stopping…")
			if m.View().MouseMode != tea.MouseModeNone {
				t.Error("the mouse is still captured while the view is closing")
			}

			done := snapshot()
			done.Phase = engine.Done
			_, cmd = m.Update(snapshotMsg{snap: done})
			if cmd == nil {
				t.Fatal("the view did not close on the snapshot that says Done")
			}
			if _, ok := cmd().(tea.QuitMsg); !ok {
				t.Errorf("the command on Done is %T, want a quit", cmd())
			}
		})
	}
}

func TestASnapshotThatIsNotDoneWaitsForTheNext(t *testing.T) {
	m, hs := newModel(t, 120, 30)
	_, cmd := m.Update(snapshotMsg{snap: snapshot()})
	if cmd == nil {
		t.Fatal("the view stopped listening to the engine")
	}
	later := snapshot()
	later.Seq = 99
	hs.eng.snap = later
	hs.eng.changed <- struct{}{}
	msg, ok := cmd().(snapshotMsg)
	if !ok || msg.snap.Seq != 99 {
		t.Errorf("the command delivered %+v, want the newest snapshot", msg)
	}
}

func TestOOpensTheSelectedPR(t *testing.T) {
	m, hs := newModel(t, 120, 30)
	m = press(t, m, "j")

	next, cmd := m.Update(keys["o"])
	m = next.(Model)
	wantOnScreen(t, m, "opening PR #8 in the browser")
	if cmd == nil {
		t.Fatal("o started nothing")
	}
	m = send(m, cmd())
	if len(hs.opened) != 1 || hs.opened[0] != 8 {
		t.Errorf("opened %v, want PR #8", hs.opened)
	}
	wantOnScreen(t, m, "opened PR #8 in the browser")
}

func TestOSaysWhenGhCouldNotOpenThePR(t *testing.T) {
	m, hs := newModel(t, 120, 30)
	hs.openEr = errors.New("gh pr view: no browser\x1b[2J")
	next, cmd := m.Update(keys["o"])
	m = send(next.(Model), cmd())
	wantOnScreen(t, m, "gh could not open PR #9: gh pr view: no browser[2J")
}

func TestOWithNoPRSaysSo(t *testing.T) {
	m, hs := newModel(t, 120, 30)
	empty := snapshot()
	empty.Reviews = nil
	m = send(m, snapshotMsg{snap: empty})

	next, cmd := m.Update(keys["o"])
	if cmd != nil || len(hs.opened) != 0 {
		t.Error("o opened something with no PR selected")
	}
	wantOnScreen(t, next.(Model), "there is no PR to open", "no PRs to show")
}
