package tui

import (
	"fmt"
	"strconv"
	"time"

	"github.com/venabots/autoreview/internal/activity"
	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/sanitize"
	"github.com/venabots/autoreview/internal/words"
)

// shownEvents is how many of a running review's events the pane lists.
const shownEvents = 50

// followed is the tail of one running review. key names the review, so a
// second review of the same PR starts a tail of its own.
type followed struct {
	key  string
	tail activity.Tail
}

func followKey(job engine.Job) string {
	return strconv.Itoa(job.PR) + "@" + strconv.FormatInt(job.StartedAt.UnixNano(), 10)
}

// tailFor is how one running review is followed: through its session's
// transcript, through its stderr when it keeps no transcript, or not at
// all when nothing says what it is doing until it ends.
func (m Model) tailFor(job engine.Job) activity.Tail {
	switch {
	case job.FollowStderr:
		return activity.Plain(job.Stderr, job.StartedAt)
	case job.Follow != "" && m.act.Transcript != nil:
		id, find := job.Follow, m.act.Transcript
		return activity.Transcript(func() (string, bool) { return find(id) }, job.StartedAt, job.FollowFromEnd)
	}
	return activity.Silent("claude named this session itself; its transcript is found when the review ends")
}

// polled reads what each running review has done since the last tick. A
// review that is no longer running is no longer followed.
func (m Model) polled() Model {
	next := map[int]followed{}
	for _, job := range m.snap.Jobs {
		if job.State != engine.JobRunning {
			continue
		}
		f, ok := m.tails[job.PR]
		if !ok || f.key != followKey(job) {
			f = followed{key: followKey(job), tail: m.tailFor(job)}
		}
		next[job.PR] = followed{key: f.key, tail: f.tail.Poll(m.now)}
	}
	m.tails = next
	return m
}

// activityLines is what a running review is doing: where that is read
// from, how far it has got, and its newest events first.
func activityLines(job engine.Job, f followed, now time.Time) []line {
	t := f.tail
	lines := []line{note(sourceLine(job, t), dimStyle)}
	switch {
	case t.Why() != "":
		return lines
	case t.Turns() == 0:
		return append(lines, note("waiting for the first turn", dimStyle))
	}
	lines = append(lines,
		note(fmt.Sprintf("%s · %s", words.Count(t.Turns(), "turn"), words.Count(t.ToolCalls(), "tool call")), dimStyle),
		line{},
		line{styled("ACTIVITY", headingStyle)},
	)
	events := t.Events()
	for i := len(events) - 1; i >= 0 && len(lines) < shownEvents+4; i-- {
		lines = append(lines, eventLine(events[i], now))
	}
	return lines
}

// sourceLine says where the activity is read from.
func sourceLine(job engine.Job, t activity.Tail) string {
	switch {
	case t.Why() != "":
		return t.Why()
	case job.FollowStderr:
		return t.Source()
	case t.Source() != "":
		return "session " + job.Follow + " · " + t.Source()
	}
	return "session " + job.Follow
}

// eventLine is one event: how long ago, the tool or "said" for text, and
// what it was about.
func eventLine(ev activity.Event, now time.Time) line {
	ago := ""
	if !ev.At.IsZero() {
		ago = words.Duration(now.Sub(ev.At)) + " ago"
	}
	tool := ev.Tool
	if tool == "" {
		tool = "said"
	}
	return line{
		styled(fmt.Sprintf("%9s", ago), dimStyle),
		text("  "),
		styled(fmt.Sprintf("%-8s", sanitize.Line(tool)), cyanStyle),
		text(" "),
		text(sanitize.Line(ev.Summary)),
	}
}
