package doctor

import (
	"fmt"
	"strings"
	"time"

	"github.com/venabots/autoreview/internal/sanitize"
	"github.com/venabots/autoreview/internal/words"
)

// report is everything one doctor found.
type report struct {
	now    time.Time
	seated Seated
	// checks is nil when the panel was not asked.
	checks  []Check
	probed  bool
	window  string
	history History
	// ledger is the file read, or why none was.
	ledger    string
	ledgerErr string
	tools     []Tool
}

// problems is each thing that needs a person, in the order the report
// shows them. The exit status is 1 when there is any.
//
// A model the ledger shows as down counts only when the panel was not
// asked: it may have left the panel since, and the probe has the answer
// for the models that are still on it.
func (r report) problems() []string {
	var out []string
	for _, c := range r.checks {
		if !c.OK {
			out = append(out, specName(c.Spec)+" does not answer")
		}
	}
	if r.ledgerErr != "" {
		out = append(out, "the ledger could not be read")
	}
	if !r.probed {
		for _, m := range r.history.Models {
			if m.Health() == Down {
				out = append(out, m.Model+" on "+m.Backend+" answered no launch")
			}
		}
	}
	if n := len(r.history.Stalled); n > 0 {
		out = append(out, words.Count(n, "PR")+" got no verdict")
	}
	for _, t := range r.tools {
		if t.Required && !t.Found {
			out = append(out, t.Name+" is not installed")
		}
	}
	return out
}

// render is the report as plain lines. Model names, versions and probe
// errors are written by other programs, so each one is cleaned.
func render(r report) string {
	var b strings.Builder
	writePanel(&b, r)
	writeHistory(&b, r)
	writeTools(&b, r.tools)
	writeSummary(&b, r.problems())
	return b.String()
}

func writePanel(b *strings.Builder, r report) {
	if !r.probed {
		fmt.Fprintf(b, "Panel, from %s: not asked (--no-probe)\n", r.seated.Source)
		return
	}
	fmt.Fprintf(b, "Panel, asked now (from %s)\n", r.seated.Source)
	if len(r.checks) == 0 {
		b.WriteString("  no panelist: install claude, codex or opencode\n")
	}
	names := make([]string, len(r.checks))
	for i, c := range r.checks {
		names[i] = sanitize.Line(specName(c.Spec))
	}
	width := widest(names)
	for i, c := range r.checks {
		state, said := "ok  ", "answered in "+words.Duration(c.Elapsed)
		if !c.OK {
			state, said = "DOWN", sanitize.Line(c.Detail)
		}
		fmt.Fprintf(b, "  %s  %-*s  %s\n", state, width, names[i], said)
	}
}

func writeHistory(b *strings.Builder, r report) {
	h := r.history
	fmt.Fprintf(b, "\nLast %s in the ledger\n", r.window)
	if r.ledgerErr != "" {
		fmt.Fprintf(b, "  %s\n", sanitize.Line(r.ledgerErr))
		return
	}
	fmt.Fprintf(b, "  %s, %s\n", words.Count(h.Reviews, "review"), spendLine(h))
	names := make([]string, len(h.Models))
	for i, m := range h.Models {
		names[i] = sanitize.Line(m.Model + " on " + m.Backend)
	}
	width := widest(names)
	for i, m := range h.Models {
		fmt.Fprintf(b, "  %-7s  %-*s  %d of %d answered%s\n",
			healthWord(m.Health()), width, names[i], m.Answered, m.Launched, lastAnswer(m, r.now))
	}
	if len(h.Stalled) > 0 {
		fmt.Fprintf(b, "  no verdict, with a panelist missing (review these again):\n")
	}
	for _, s := range h.Stalled {
		fmt.Fprintf(b, "    %s#%d  %s, %s, %s\n",
			sanitize.Line(s.Repo), s.PR, s.Decision, seatedLine(s), stamp(s.At, r.now))
	}
}

// seatedLine is how much of the panel answered. A review that recorded no
// panel never reached one, and "0 of 0 answered" would read as a full panel.
func seatedLine(s Stalled) string {
	if s.Seated == 0 {
		return "no panel recorded"
	}
	return fmt.Sprintf("%d of %d answered", s.Answered, s.Seated)
}

func spendLine(h History) string {
	if h.Reviews == 0 {
		return "$0.00"
	}
	return fmt.Sprintf("$%.2f ($%.2f a review)", h.SpendUSD, h.SpendUSD/float64(h.Reviews))
}

func healthWord(h Health) string {
	switch h {
	case Down:
		return "DOWN"
	case Failing:
		return "FAILING"
	}
	return "ok"
}

// lastAnswer is said only for a model that is not healthy: it is when the
// trouble started.
func lastAnswer(m Model, now time.Time) string {
	switch {
	case m.Health() == Healthy:
		return ""
	case m.LastAnswer == 0:
		return ", never answered"
	}
	return ", last answer " + stamp(m.LastAnswer, now)
}

// stamp is an epoch second in the person's own zone.
func stamp(at int64, now time.Time) string {
	return time.Unix(at, 0).In(now.Location()).Format("01-02 15:04")
}

func writeTools(b *strings.Builder, tools []Tool) {
	b.WriteString("\nTools\n")
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name
	}
	width := widest(names)
	for _, t := range tools {
		said := sanitize.Line(t.Version)
		if !t.Found {
			said = "not installed"
		}
		fmt.Fprintf(b, "  %-*s  %s\n", width, t.Name, said)
	}
}

func writeSummary(b *strings.Builder, problems []string) {
	if len(problems) == 0 {
		b.WriteString("\nAll clear.\n")
		return
	}
	fmt.Fprintf(b, "\n%s:\n", words.Count(len(problems), "problem"))
	for _, p := range problems {
		fmt.Fprintf(b, "  - %s\n", sanitize.Line(p))
	}
}

func widest(names []string) int {
	w := 0
	for _, n := range names {
		w = max(w, len([]rune(n)))
	}
	return w
}
