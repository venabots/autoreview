package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/queue"
	"github.com/venabots/autoreview/internal/review"
)

const sessionOf6 = "7442b624-5cba-5d44-ae67-9c390cfe70a1"

func ptr[T any](v T) *T { return &v }

// reviewing is a run in the middle of its second pass: PR #9 runs, PR #8
// waits for a slot, and PR #6 was reviewed in the pass before.
func reviewing(t *testing.T) *engine.Snapshot {
	t.Helper()
	run := t.TempDir()
	pass := filepath.Join(run, "pass-1")
	if err := os.MkdirAll(pass, 0o700); err != nil {
		t.Fatal(err)
	}
	text := "# Overview\n\nThe change adds a **retry** to the checkout.\n\n- [MEDIUM] `src/pay.rs:88` charges twice\n"
	if err := os.WriteFile(filepath.Join(pass, "pr-6.review.md"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(run, "autoreview.log"), []byte("skills: bundled (dev)\nstart   #9 @alice (reviewing)\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := snapshot()
	s.Phase = engine.Reviewing
	s.Pass = 2
	s.RunDir = run
	s.Repo.Root = "/sandbox/repo"
	s.Jobs = []engine.Job{
		{PR: 9, Title: "Add retry logic", Author: "alice", State: engine.JobRunning, StartedAt: start.Add(-63 * time.Second), Orchestrator: review.Claude(), PassDir: filepath.Join(run, "pass-2")},
		{PR: 8, Title: "Fix the cache", Author: "bob", State: engine.JobQueued, Orchestrator: review.Claude()},
	}
	s.Earlier = []engine.Job{{
		PR: 6, Title: "Tidy the docs", Author: "carol", State: engine.JobDone, Verdict: "commented",
		StartedAt: start.Add(-10 * time.Minute), Elapsed: 4 * time.Minute, Cost: ptr(0.42), Model: "claude-fable-5",
		Session: sessionOf6, Orchestrator: review.Claude(), PassDir: pass,
		Trailer: &review.Trailer{
			Risk:     "MEDIUM",
			Findings: &review.Findings{MustFix: ptr(uint(0)), ShouldFix: ptr(uint(1)), Polish: ptr(uint(0))},
			Blockers: []review.Blocker{{Severity: "MEDIUM", Domain: "money", Reversible: ptr(false), Location: "src/pay.rs:88", Gist: "a retried checkout charges the card twice"}},
		},
	}}
	return s
}

func reviewingModel(t *testing.T, w, h int) (Model, *harness) {
	t.Helper()
	m, hs := newModel(t, w, h)
	snap := reviewing(t)
	hs.eng.snap = snap
	return send(m, snapshotMsg{snap: snap}), hs
}

func TestTheListShowsWhatTheRunIsDoingFirst(t *testing.T) {
	m, _ := reviewingModel(t, 150, 30)
	lines := strings.Split(screen(m), "\n")
	want := []string{"⠋ #9 · reviewing 1m03s", "· #8 · queued", "○ #6 · commented", "✓ #5 · approved"}
	for i, prefix := range want {
		if got := lines[2+2*i]; !strings.HasPrefix(got, prefix) {
			t.Errorf("list row %d is %q, want it to start %q", i, got, prefix)
		}
	}
	wantOnScreen(t, m,
		"autoreview · acme/widgets · one pass · press w to watch",
		"1 running · 1 queued · 1 finished · next look in 1m30s",
		"reviewing 1m03s · claude", "x x stops it",
	)
}

func TestTheTimeOfARunningReviewMovesWithTheClock(t *testing.T) {
	m, _ := reviewingModel(t, 150, 30)
	m = send(m, tickMsg(start.Add(2*time.Second)))
	wantOnScreen(t, m, "#9 · reviewing 1m05s", "reviewing 1m05s · claude")
}

func TestTheSpinnerTurnsWhileAReviewRunsEvenBetweenLooks(t *testing.T) {
	m, _ := reviewingModel(t, 150, 30)
	if !m.busy() || !m.spinning {
		t.Fatalf("busy %v, spinning %v: a running review does not turn the spinner", m.busy(), m.spinning)
	}
	next, cmd := m.Update(m.spin.Tick())
	if cmd == nil {
		t.Fatal("the spinner stopped while a review runs")
	}
	wantOnScreen(t, next.(Model), "⠙ #9 · reviewing")
}

func TestAFinishedReviewShowsWhatItFoundAndItsText(t *testing.T) {
	m, _ := reviewingModel(t, 150, 40)
	m = press(t, m, "j", "j")
	wantOnScreen(t, m,
		"#6 Tidy the docs", "R reviews it again now",
		"LAST REVIEW", "result    done", "verdict   commented", "risk      MEDIUM", "findings  1 should-fix",
		"spent     4m00s · $0.42 · claude-fable-5", "session   "+sessionOf6,
		"resume    r opens it in a new tab, or run:", "cd /sandbox/repo && claude --resume "+sessionOf6,
		"not approved yet because:", "- [MEDIUM] (money, irreversible) src/pay.rs:88 — a retried checkout charges the card twice",
		"REVIEW", "Overview", "The change adds a retry to the checkout.", "charges twice",
	)
	wantNotOnScreen(t, m, "**retry**", "no review text at")
}

func TestAReviewWhoseTextIsNotThereSaysWhereItWouldBe(t *testing.T) {
	m, hs := reviewingModel(t, 300, 40)
	snap := reviewing(t)
	snap.Earlier[0].PassDir = filepath.Join(snap.RunDir, "pass-9")
	hs.eng.snap = snap
	m = press(t, send(m, snapshotMsg{snap: snap}), "j", "j")
	wantOnScreen(t, m, "no review text at "+filepath.Join(snap.RunDir, "pass-9", "pr-6.review.md"))
}

func TestAFailedReviewShowsItsErrorAndItsLog(t *testing.T) {
	m, hs := reviewingModel(t, 300, 40)
	snap := reviewing(t)
	snap.Earlier[0].State, snap.Earlier[0].Outcome, snap.Earlier[0].Error = engine.JobFailed, "exit 10", "You've hit your session limit"
	snap.Earlier[0].Verdict, snap.Earlier[0].Trailer = "", nil
	hs.eng.snap = snap
	m = press(t, send(m, snapshotMsg{snap: snap}), "j", "j")
	wantOnScreen(t, m, "○ #6 · failed", "result    failed (exit 10)", "error     You've hit your session limit",
		"verdict   nothing posted", "log       "+filepath.Join(snap.Earlier[0].PassDir, "pr-6.log"))
	wantNotOnScreen(t, m, "REVIEW\n")
}

func commands(hs *harness) []engine.Command { return hs.eng.got }

func TestRAsksForAReviewOfTheSelectedPR(t *testing.T) {
	m, hs := reviewingModel(t, 150, 30)

	m = press(t, m, "R")
	wantOnScreen(t, m, "PR #9 is being reviewed already")
	if len(commands(hs)) != 0 {
		t.Fatalf("R on a running review sent %v", commands(hs))
	}

	m = press(t, m, "j", "j", "R")
	wantOnScreen(t, m, "PR #6 is reviewed next")
	if got := commands(hs); len(got) != 1 || got[0] != (engine.ReviewNow{PR: 6}) {
		t.Errorf("the engine was sent %v, want ReviewNow for PR #6", got)
	}

	m = press(t, m, "tab", "R")
	wantOnScreen(t, m, "R reviews other people's work; f fixes your own PR")
	if len(commands(hs)) != 1 {
		t.Errorf("R on your own PR sent the engine %v", commands(hs))
	}
}

func TestXStopsARunningReviewOnTheSecondPress(t *testing.T) {
	m, hs := reviewingModel(t, 150, 30)

	m = press(t, m, "x")
	wantOnScreen(t, m, "press x again to stop PR #9's review")
	if len(commands(hs)) != 0 {
		t.Fatal("one press of x stopped a review")
	}
	m = press(t, m, "x")
	wantOnScreen(t, m, "stopping PR #9's review")
	if got := commands(hs); len(got) != 1 || got[0] != (engine.StopJob{PR: 9}) {
		t.Errorf("the engine was sent %v, want StopJob for PR #9", got)
	}
}

func TestASecondXOnAnotherRowOrTooLateStopsNothing(t *testing.T) {
	m, hs := reviewingModel(t, 150, 30)

	moved := press(t, m, "x", "j", "x")
	wantOnScreen(t, moved, "PR #8 has no review running")
	back := press(t, moved, "k", "x")
	wantOnScreen(t, back, "press x again to stop PR #9's review")

	late := press(t, m, "x")
	late = send(late, tickMsg(start.Add(4*time.Second)))
	late = press(t, late, "x")
	wantOnScreen(t, late, "press x again to stop PR #9's review")

	if len(commands(hs)) != 0 {
		t.Errorf("the engine was sent %v, want nothing", commands(hs))
	}
}

func TestQStopsRunningReviewsOnlyOnTheSecondPress(t *testing.T) {
	m, hs := reviewingModel(t, 150, 30)

	m = press(t, m, "q")
	wantOnScreen(t, m, "press q again to stop 2 reviews and quit")
	if len(commands(hs)) != 0 {
		t.Fatal("one press of q quit a run with reviews alive")
	}
	other := press(t, m, "j", "q")
	wantOnScreen(t, other, "press q again to stop 2 reviews and quit")

	m = press(t, m, "q")
	if got := commands(hs); len(got) != 1 || got[0] != (engine.Quit{}) {
		t.Errorf("the engine was sent %v, want one Quit", got)
	}
	wantOnScreen(t, m, "stopping…")
}

func TestCtrlCQuitsAtOnceEvenWithReviewsRunning(t *testing.T) {
	m, hs := reviewingModel(t, 150, 30)
	press(t, m, "ctrl+c")
	if got := commands(hs); len(got) != 1 || got[0] != (engine.Quit{}) {
		t.Errorf("the engine was sent %v, want one Quit", got)
	}
}

func typeText(t *testing.T, m Model, text string) Model {
	t.Helper()
	for _, r := range text {
		code := r
		if r == ' ' {
			code = tea.KeySpace
		}
		m = send(m, tea.KeyPressMsg{Code: code, Text: string(r)})
	}
	return m
}

func TestFOpensAnEditorForTheFocus(t *testing.T) {
	m, hs := reviewingModel(t, 150, 30)

	m = press(t, m, "f")
	footer := lastLine(m)
	if !strings.HasPrefix(footer, "focus ") || !strings.Contains(footer, "enter to apply · esc to leave it") {
		t.Fatalf("the footer is %q, want the focus editor", footer)
	}
	m = typeText(t, m, "be strict, q")
	if len(commands(hs)) != 0 {
		t.Fatalf("a key typed into the editor reached the engine: %v", commands(hs))
	}
	if footer := lastLine(m); !strings.HasPrefix(footer, "focus be strict, q") {
		t.Errorf("the footer is %q, want what was typed", footer)
	}

	m = press(t, m, "enter")
	if got := commands(hs); len(got) != 1 || got[0] != (engine.SetFocus{Text: "be strict, q"}) {
		t.Errorf("the engine was sent %v, want the focus", got)
	}
	wantOnScreen(t, m, "the reviewers are now told: be strict, q", "q quit")
}

func lastLine(m Model) string {
	lines := strings.Split(screen(m), "\n")
	return lines[len(lines)-1]
}

func TestTheFocusEditorStartsFromTheFocusAsItIs(t *testing.T) {
	m, hs := reviewingModel(t, 150, 30)
	snap := reviewing(t)
	snap.Focus = "the ledger"
	hs.eng.snap = snap
	m = send(m, snapshotMsg{snap: snap})
	wantOnScreen(t, m, "· focus: the ledger")

	m = typeText(t, press(t, m, "f"), " migration")
	if footer := lastLine(m); !strings.HasPrefix(footer, "focus the ledger migration") {
		t.Errorf("the footer is %q, want the old focus with the new words after it", footer)
	}
}

func TestEscLeavesTheFocusAndABlankLineClearsIt(t *testing.T) {
	m, hs := reviewingModel(t, 150, 30)

	left := press(t, typeText(t, press(t, m, "f"), "nothing"), "esc")
	wantOnScreen(t, left, "the focus is unchanged")
	if len(commands(hs)) != 0 {
		t.Fatalf("esc sent the engine %v", commands(hs))
	}

	cleared := press(t, typeText(t, press(t, m, "f"), "  "), "enter")
	wantOnScreen(t, cleared, "the reviewers are told nothing in particular")
	if got := commands(hs); len(got) != 1 || got[0] != (engine.SetFocus{}) {
		t.Errorf("the engine was sent %v, want an empty focus", got)
	}
}

func TestAFocusAPromptCannotCarryKeepsTheEditorOpen(t *testing.T) {
	m, hs := reviewingModel(t, 150, 30)
	m = press(t, typeText(t, press(t, m, "f"), `\`), "enter")
	if len(commands(hs)) != 0 || !m.editing {
		t.Errorf("a focus of one backslash was sent (%v) or the editor closed (%v)", commands(hs), !m.editing)
	}
}

func TestFOnMyPRsFixesAndOpensNoEditor(t *testing.T) {
	m, _ := reviewingModel(t, 150, 30)
	m = press(t, m, "tab", "f")
	if m.editing {
		t.Error("f opened the focus editor on My PRs")
	}
	wantOnScreen(t, m, "fixing PR #4")
}

func TestLShowsTheRunLogAndFollowsIt(t *testing.T) {
	m, hs := reviewingModel(t, 300, 30)
	log := filepath.Join(hs.eng.snap.RunDir, "autoreview.log")

	m = press(t, m, "l")
	wantOnScreen(t, m, "run log · "+log, "skills: bundled (dev)", "start   #9 @alice (reviewing)")
	wantNotOnScreen(t, m, "#9 Add retry logic")

	f, err := os.OpenFile(log, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("done    #9 (1m03s)\x1b[2J\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	m = send(m, tickMsg(start.Add(time.Second)))
	wantOnScreen(t, m, "done    #9 (1m03s)[2J")
	if strings.Contains(m.View().Content, "\x1b[2J") {
		t.Error("an escape sequence in the log reached the screen")
	}

	wantOnScreen(t, press(t, m, "esc"), "#9 Add retry logic")
	wantOnScreen(t, press(t, m, "l"), "#9 Add retry logic")
}

func TestRReopensAFinishedReviewInANewTab(t *testing.T) {
	m, hs := reviewingModel(t, 150, 30)
	m = press(t, m, "j", "j")

	next, cmd := m.Update(keys["r"])
	m = next.(Model)
	wantOnScreen(t, m, "opening PR #6's review in a new tab")
	if cmd == nil {
		t.Fatal("r started nothing")
	}
	m = send(m, cmd())
	want := "PR 6 Resume: cd /sandbox/repo && claude --resume " + sessionOf6
	if len(hs.tabs) != 1 || hs.tabs[0] != want {
		t.Errorf("tabs opened: %q, want %q", hs.tabs, want)
	}
	wantOnScreen(t, m, "opened PR #6's review in a new cmux tab")

	hs.tabErr = errors.New("no terminal that can open a tab\x1b[2J")
	next, cmd = m.Update(keys["r"])
	m = send(next.(Model), cmd())
	wantOnScreen(t, m, "could not reopen PR #6's review: no terminal that can open a tab[2J")
}

func TestRSaysWhyThereIsNothingToResume(t *testing.T) {
	m, hs := reviewingModel(t, 150, 30)

	wantOnScreen(t, press(t, m, "r"), "PR #9 is being reviewed; resume it when the review ends")
	wantOnScreen(t, press(t, m, "j", "r"), "PR #8 has no finished review to resume")
	wantOnScreen(t, press(t, m, "G", "r"), "PR #5 has no finished review to resume")

	snap := reviewing(t)
	snap.Earlier[0].Session = ""
	hs.eng.snap = snap
	m = press(t, send(m, snapshotMsg{snap: snap}), "j", "j", "r")
	wantOnScreen(t, m, "PR #6's review has no session to resume")
	wantOnScreen(t, press(t, m, "tab", "r"), "PR #4 has had no task in this run")
	if len(hs.tabs) != 0 {
		t.Errorf("a tab was opened: %q", hs.tabs)
	}
}

func TestEveryFrameWithReviewsFillsTheScreenExactly(t *testing.T) {
	for _, size := range [][2]int{{150, 40}, {90, 12}, {80, 24}, {40, 10}, {20, 4}} {
		m, _ := reviewingModel(t, size[0], size[1])
		for name, state := range map[string]Model{
			"the list": m, "a finished review": press(t, m, "j", "j"), "the log": press(t, m, "l"),
			"the focus editor": typeText(t, press(t, m, "f"), "be strict about the migration"), "the keys": press(t, m, "?"),
		} {
			lines := strings.Split(state.View().Content, "\n")
			if len(lines) != size[1] {
				t.Errorf("%dx%d, %s: the frame has %d lines", size[0], size[1], name, len(lines))
			}
			for i, l := range lines {
				if w := ansi.StringWidth(l); w != size[0] {
					t.Errorf("%dx%d, %s: line %d is %d columns wide: %q", size[0], size[1], name, i, w, ansi.Strip(l))
				}
			}
		}
	}
}

func TestAReviewOfAPRThatLeftTheListIsStillShown(t *testing.T) {
	m, hs := reviewingModel(t, 150, 30)
	snap := reviewing(t)
	snap.Reviews = snap.Reviews[:2]
	hs.eng.snap = snap
	m = press(t, send(m, snapshotMsg{snap: snap}), "j", "j")
	wantOnScreen(t, m, "○ #6 · commented", "  Tidy the docs", "#6 Tidy the docs", "@carol", "LAST REVIEW")

	m = press(t, m, "R")
	wantOnScreen(t, m, "PR #6 is no longer on the list")
	if len(commands(hs)) != 0 {
		t.Errorf("the engine was sent %v", commands(hs))
	}
}

func TestInTheFocusEditorTheArrowsMoveTheCursorAndNotTheTab(t *testing.T) {
	m, hs := reviewingModel(t, 150, 30)
	m = typeText(t, press(t, m, "f"), "ab")
	m = press(t, m, "left")
	m = typeText(t, m, "X")
	if m.tab != tabReview {
		t.Fatal("an arrow typed into the focus editor changed the tab")
	}
	m = press(t, m, "right", "right", "enter")
	if got := commands(hs); len(got) != 1 || got[0] != (engine.SetFocus{Text: "aXb"}) {
		t.Errorf("the engine was sent %v, want the focus with the X before the b", got)
	}
}

func TestTheHeaderSaysWhetherTheRunWatches(t *testing.T) {
	m, hs := reviewingModel(t, 150, 30)
	snap := reviewing(t)
	snap.Watching, snap.Rest = true, 30*time.Minute
	hs.eng.snap = snap
	m = send(m, snapshotMsg{snap: snap})
	wantOnScreen(t, m, "autoreview · acme/widgets · watching every 2m · a reviewed PR rests 30m")
}

func TestWStartsAndStopsWatching(t *testing.T) {
	m, hs := reviewingModel(t, 150, 30)
	m = press(t, m, "w")
	wantOnScreen(t, m, "watching: reviewing what becomes ready, every 2m")
	snap := reviewing(t)
	snap.Watching = true
	hs.eng.snap = snap
	m = press(t, send(m, snapshotMsg{snap: snap}), "w")
	wantOnScreen(t, m, "no longer watching; R still reviews a PR")
	if got := commands(hs); len(got) != 2 || got[0] != (engine.SetWatch{On: true}) || got[1] != (engine.SetWatch{On: false}) {
		t.Errorf("the engine was sent %v", got)
	}
}

func TestARestingOrCappedPRSaysSoInItsRowAndDetail(t *testing.T) {
	m, hs := reviewingModel(t, 200, 30)
	snap := reviewing(t)
	for i := range snap.Reviews {
		switch snap.Reviews[i].PR.Number {
		case 6:
			snap.Reviews[i].Wait, snap.Reviews[i].Until = queue.WaitResting, start.Add(12*time.Minute)
		case 5:
			snap.Reviews[i].Wait = queue.WaitCapped
		}
	}
	hs.eng.snap = snap
	m = send(m, snapshotMsg{snap: snap})
	wantOnScreen(t, m, "○ #6 · commented · rest 12m00s", "✓ #5 · capped")

	// A capped PR the run never reviewed comes before the finished
	// reviews; the resting PR is one of those.
	m = press(t, m, "j", "j")
	wantOnScreen(t, m, "#5 Bump the SDK", "capped: it has had every review this run gives one PR (--max-passes)")
	m = press(t, m, "G")
	wantOnScreen(t, m, "#6 Tidy the docs", "resting after its review; it may be reviewed again in 12m00s", "R reviews it again now")
}
