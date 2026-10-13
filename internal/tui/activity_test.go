package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/review"
)

const followed9 = "7442b624-5cba-5d44-ae67-9c390cfe70a1"

// following is a view of a run whose PR #9 is reviewed in session
// followed9, with that session's transcript at the path it returns.
func following(t *testing.T, edit func(*engine.Job)) (Model, *harness, string) {
	t.Helper()
	transcript := filepath.Join(t.TempDir(), followed9+".jsonl")
	m, hs := reviewingModel(t, 200, 40)
	m.act.Transcript = func(id string) (string, bool) { return transcript, id == followed9 }
	snap := reviewing(t)
	snap.Jobs[0].Follow = followed9
	if edit != nil {
		edit(&snap.Jobs[0])
	}
	hs.eng.snap = snap
	return send(m, snapshotMsg{snap: snap}), hs, transcript
}

func appendTo(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}
}

func TestARunningReviewShowsWhatItIsDoing(t *testing.T) {
	m, _, transcript := following(t, nil)
	m = send(m, tickMsg(start.Add(time.Second)))
	wantOnScreen(t, m, "session "+followed9+" · waiting for its transcript")

	appendTo(t, transcript, `{"type":"assistant","timestamp":"2026-10-08T12:00:01.500Z","message":{"content":[{"type":"text","text":"Reading the diff.\nmore"}]}}
{"type":"user","message":{"content":[{"type":"tool_result"}]}}
{"type":"assistant","timestamp":"2026-10-08T12:00:03Z","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"cargo test --quiet\nsecond line"}}]}}
`)
	m = send(m, tickMsg(start.Add(5*time.Second)))
	wantOnScreen(t, m,
		"session "+followed9,
		"2 turns · 1 tool call",
		"ACTIVITY",
		"2s ago  Bash     cargo test --quiet",
		"3s ago  said     Reading the diff.",
	)
	wantNotOnScreen(t, m, "second line", "waiting for its transcript")

	appendTo(t, transcript, `{"type":"assistant","timestamp":"2026-10-08T12:00:06Z","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/src/pay.rs"}}]}}
`)
	m = send(m, tickMsg(start.Add(7*time.Second)))
	wantOnScreen(t, m, "3 turns · 2 tool calls", "1s ago  Read     pay.rs")
}

func TestAReviewThatNamedItsOwnSessionSaysWhyItShowsNothing(t *testing.T) {
	m, _, _ := following(t, func(j *engine.Job) { j.Follow = "" })
	m = send(m, tickMsg(start.Add(time.Second)))
	wantOnScreen(t, m, "claude named this session itself; its transcript is found when the review ends")
	wantNotOnScreen(t, m, "ACTIVITY")
}

func TestAnOverrideIsFollowedThroughItsStderr(t *testing.T) {
	stderr := filepath.Join(t.TempDir(), "pr-9.log")
	m, _, _ := following(t, func(j *engine.Job) {
		j.Follow, j.FollowStderr, j.Stderr = "", true, stderr
	})
	appendTo(t, stderr, "checking out the PR\n\nrunning the linters\n")
	m = send(m, tickMsg(start.Add(time.Second)))
	wantOnScreen(t, m, "following the reviewer's stderr", "said     running the linters", "said     checking out the PR")
}

func TestASecondReviewOfThePRIsFollowedAfresh(t *testing.T) {
	m, hs, transcript := following(t, nil)
	appendTo(t, transcript, `{"type":"assistant","timestamp":"2026-10-08T12:00:01Z","message":{"content":[{"type":"text","text":"first review"}]}}
`)
	m = send(m, tickMsg(start.Add(2*time.Second)))
	wantOnScreen(t, m, "first review")

	snap := reviewing(t)
	snap.Jobs[0].Follow = followed9
	snap.Jobs[0].StartedAt = start.Add(time.Minute)
	hs.eng.snap = snap
	m = send(m, snapshotMsg{snap: snap})
	m = send(m, tickMsg(start.Add(61*time.Second)))
	wantNotOnScreen(t, m, "first review")
}

func TestARetrySaysWhichAgentFailed(t *testing.T) {
	first := &engine.Attempt{Orchestrator: review.Claude(), Outcome: "exit 10", Error: "usage limit"}
	codexAgent := review.Orchestrator{Backend: "codex"}

	running, _, _ := following(t, func(j *engine.Job) { j.Orchestrator, j.First = codexAgent, first })
	wantOnScreen(t, running, "reviewing 1m03s · codex after claude failed")

	m, hs := reviewingModel(t, 200, 40)
	snap := reviewing(t)
	snap.Jobs[1].Orchestrator, snap.Jobs[1].First = codexAgent, first
	snap.Earlier[0].Orchestrator, snap.Earlier[0].First = codexAgent, first
	hs.eng.snap = snap
	m = send(m, snapshotMsg{snap: snap})
	wantOnScreen(t, press(t, m, "j"), "queued · claude failed; waiting for a slot to retry with codex")
	wantOnScreen(t, press(t, m, "j", "j"), "fallback  claude exit 10: usage limit; retried with codex")
}
