// Package notify tells a person what needs them while a run goes on: a fix
// that asks for them, a fix that failed, a panel that went down. The view
// shows these too, but only to a person who looks at it.
//
// The package decides what alerts and queues the alerts. It sends nothing
// itself: each channel is a Sender that the caller hands in.
package notify

import (
	"fmt"
	"strings"

	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/report"
	"github.com/venabots/autoreview/internal/sanitize"
	"github.com/venabots/autoreview/internal/task"
	"github.com/venabots/autoreview/internal/words"
)

// Alert is one thing to tell the person. Urgent asks for their attention
// now, which a channel can show with a sound.
type Alert struct {
	Title  string
	Body   string
	Urgent bool
}

// AlertsFor is the alerts one event raises, or none. Only what needs a
// person alerts: everything else stays in the log.
func AlertsFor(ev engine.Event) []Alert {
	switch ev := ev.(type) {
	case engine.JobFinished:
		return safe(taskAlerts(ev.Job))
	case engine.HealthChanged:
		return safe(healthAlerts(ev))
	}
	return nil
}

func taskAlerts(job engine.Job) []Alert {
	if job.Task == task.Review {
		return nil
	}
	var out []Alert
	switch {
	case job.State == engine.JobFailed:
		out = append(out, Alert{Title: fmt.Sprintf("PR #%d: %s failed (%s)", job.PR, job.Task.Noun(), job.Outcome), Body: job.Error, Urgent: true})
	case job.State == engine.JobTimedOut:
		out = append(out, Alert{Title: fmt.Sprintf("PR #%d: %s timed out", job.PR, job.Task.Noun()), Urgent: true})
	case report.TaskAdvice(job) != "":
		out = append(out, Alert{Title: fmt.Sprintf("PR #%d %s", job.PR, report.TaskAdvice(job)), Body: job.TaskTrailer.Why, Urgent: true})
	}
	if job.Kept != "" {
		out = append(out, Alert{Title: fmt.Sprintf("PR #%d's worktree has %s", job.PR, job.Kept), Body: job.Worktree})
	}
	return out
}

func healthAlerts(ev engine.HealthChanged) []Alert {
	h := ev.After
	switch {
	case h.Blocking:
		return []Alert{{Title: "Reviews cannot approve: " + panelOf(h), Body: strings.Join(h.Problems, "; "), Urgent: true}}
	case len(h.Problems) > 0:
		return []Alert{{Title: "Doctor: " + words.Count(len(h.Problems), "problem"), Body: strings.Join(h.Problems, "; ")}}
	case len(ev.Before.Problems) > 0 || ev.Before.Blocking:
		return []Alert{{Title: "Doctor: all clear again", Body: panelOf(h)}}
	}
	return nil
}

func panelOf(h engine.Health) string {
	if h.Seated == 0 {
		return "no panel"
	}
	return fmt.Sprintf("panel %d/%d", h.Answering, h.Seated)
}

// safe is the alerts with every byte that could repaint a terminal gone.
func safe(alerts []Alert) []Alert {
	out := make([]Alert, len(alerts))
	for i, a := range alerts {
		out[i] = Alert{Title: sanitize.Line(a.Title), Body: sanitize.Line(a.Body), Urgent: a.Urgent}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
