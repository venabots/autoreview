package tui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/queue"
	"github.com/venabots/autoreview/internal/review"
)

// listOrder is the PR numbers of the Review tab's rows, top to bottom.
func listOrder(s *engine.Snapshot) []int {
	out := []int{}
	for _, row := range reviewRowsOf(s) {
		out = append(out, row.pr)
	}
	return out
}

// listHeads is the first line of each row on the screen.
func listHeads(m Model, rows int) []string {
	lines := strings.Split(screen(m), "\n")
	out := []string{}
	for i := range rows {
		head, _, _ := strings.Cut(lines[2+2*i], "│")
		out = append(out, strings.TrimSpace(head))
	}
	return out
}

func TestROnAStackedPRDuringAPassMovesItUnderTheRunningReviews(t *testing.T) {
	m, hs := reviewingModel(t, 150, 30)
	snap := reviewing(t)
	snap.Reviews = append(slices.Clone(snap.Reviews), queue.Row{
		PR:   prs.PR{Number: 7, Title: "Stack the tables", Author: "erin", Branch: "erin/tables", Base: "main", Parent: &prs.Parent{Number: 8, Evidence: prs.Overlap, Shared: 2}},
		Wait: queue.WaitStacked,
	})
	hs.eng.snap = snap
	m = send(m, snapshotMsg{snap: snap})
	if got := listOrder(snap); !slices.Equal(got, []int{9, 8, 7, 6, 5}) {
		t.Fatalf("before R the list is %v", got)
	}

	m = press(t, m, "j", "j", "R")
	if !slices.Contains(hs.eng.got, engine.Command(engine.ReviewNow{PR: 7})) {
		t.Fatalf("R sent %v, want a review of PR #7 now", hs.eng.got)
	}
	joined := reviewing(t)
	joined.Reviews = snap.Reviews
	joined.Jobs = []engine.Job{
		joined.Jobs[0],
		{PR: 7, Title: "Stack the tables", Author: "erin", State: engine.JobQueued, Orchestrator: review.Claude()},
		joined.Jobs[1],
	}
	hs.eng.snap = joined
	m = send(m, snapshotMsg{snap: joined})

	want := []string{"⠋ #9 · reviewing 1m03s", "· #7 · queued", "· #8 · queued", "○ #6 · commented", "✓ #5 · approved"}
	if got := listHeads(m, len(want)); !slices.Equal(got, want) {
		t.Errorf("after R the list is\n%q\nwant\n%q", got, want)
	}
}

func TestAReviewedPRThatIsDueAgainSitsWithTheRowsDueNextInTheOrderTheyStart(t *testing.T) {
	s := reviewing(t)
	s.Jobs = nil
	s.Phase = engine.Idle
	s.Watching = true
	s.Reviews = []queue.Row{
		{PR: prs.PR{Number: 10, Title: "New work", Author: "frank", Branch: "frank/new"}},
		{PR: prs.PR{Number: 6, Title: "Tidy the docs", Author: "carol", Branch: "carol/docs"}},
		{PR: prs.PR{Number: 11, Title: "More new work", Author: "gus", Branch: "gus/more"}},
		{PR: prs.PR{Number: 8, Title: "Fix the cache", Author: "bob", Branch: "bob/cache", CI: prs.CIPending}, Wait: queue.WaitChecks},
		{PR: prs.PR{Number: 5, Title: "Bump the SDK", Author: "dave", Decision: prs.Approved}, Wait: queue.WaitApproved},
	}
	if got := listOrder(s); !slices.Equal(got, []int{10, 6, 11, 8, 5}) {
		t.Errorf("the list is %v, want the rows due next in the order the next pass starts them, PR #6 among them", got)
	}

	m, hs := newModel(t, 150, 30)
	hs.eng.snap = s
	m = send(m, snapshotMsg{snap: s})
	wantOnScreen(t, m, "#6 · commented · ready")

	s.Watching = false
	if got := listOrder(s); !slices.Equal(got, []int{10, 11, 8, 6, 5}) {
		t.Errorf("in a run that does not watch the list is %v, want PR #6 with the finished reviews: no next pass comes by itself", got)
	}
}

func TestAReviewedPRThatRestsStaysWithTheFinishedReviews(t *testing.T) {
	s := reviewing(t)
	s.Jobs = nil
	s.Phase = engine.Idle
	s.Reviews = []queue.Row{
		{PR: prs.PR{Number: 10, Title: "New work", Author: "frank"}},
		{PR: prs.PR{Number: 6, Title: "Tidy the docs", Author: "carol"}, Wait: queue.WaitResting, Until: start.Add(10 * time.Minute)},
		{PR: prs.PR{Number: 8, Title: "Fix the cache", Author: "bob", CI: prs.CIPending}, Wait: queue.WaitChecks},
	}
	if got := listOrder(s); !slices.Equal(got, []int{10, 8, 6}) {
		t.Errorf("the list is %v, want PR #6, which rests, after the rows that wait to start", got)
	}
}
