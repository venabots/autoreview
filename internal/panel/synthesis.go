package panel

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/venabots/autoreview/internal/proc"
	"github.com/venabots/autoreview/internal/sanitize"
	"github.com/venabots/autoreview/internal/words"
)

// errSynthesisInterrupted is a synthesis the run stopped on purpose.
var errSynthesisInterrupted = errors.New("interrupted before the synthesis started")

// rosterLine is one line about a panelist outside the fenced reports. The
// model and the failure are process output, so a newline or an escape in
// either would forge structure the synthesizer reads as ours. sanitize.Line
// drops every control character, newlines too.
func rosterLine(o Outcome) string {
	return fmt.Sprintf("- %s (%s): %s\n", o.ID, sanitize.Line(o.Model), sanitize.Line(o.Failure))
}

// SynthesisPrompt is the synthesizer's input: what was reviewed, who
// answered and who did not, the diff, and every report as it came back.
//
// The roster is not decoration. "3 of 6 returned" changes what consensus
// means, and a synthesizer never told that a panelist died would read its
// silence as agreement.
func SynthesisPrompt(r Resolved, outcomes []Outcome, focus string) string {
	var b strings.Builder
	if r.Subject.Audit {
		b.WriteString(auditSynthesisTemplate)
	} else {
		b.WriteString(synthesisTemplate)
	}
	b.WriteString("\n\n## This run\n\n")
	fmt.Fprintf(&b, "- Target line to quote: %s\n", r.Label)
	fmt.Fprintf(&b, "- Panelists: %d of %d returned a review\n", countAnswered(outcomes), len(outcomes))
	if focus != "" {
		fmt.Fprintf(&b, "- The reviewers were asked to focus on: %s\n", focus)
	}
	b.WriteString(roster(outcomes))
	b.WriteString(subjectForSynthesis(r))
	b.WriteString("\n## Panelist reports\n")
	for _, o := range outcomes {
		if o.Answered() {
			b.WriteString(fencedReport(o))
		}
	}
	return b.String()
}

// roster names the panelists that contributed nothing, and those that
// answered with a bad exit. The second still count toward consensus: the
// reader is told the run was not clean, so a truncated review is not
// mistaken for a thorough one.
func roster(outcomes []Outcome) string {
	var silent, unclean strings.Builder
	for _, o := range outcomes {
		switch {
		case !o.Answered():
			silent.WriteString(rosterLine(o))
		case !o.Clean():
			unclean.WriteString(rosterLine(o))
		}
	}
	var b strings.Builder
	if silent.Len() > 0 {
		b.WriteString("\nThese panelists contributed nothing. Do not count them toward consensus:\n\n" + silent.String())
	}
	if unclean.Len() > 0 {
		b.WriteString("\nThese panelists returned a review but did not exit cleanly. Count them, and treat a report that stops mid-sentence as truncated:\n\n" + unclean.String())
	}
	return b.String()
}

// subjectForSynthesis is the diff, or for an audit the file list. The
// synthesizer is read-only and cannot run git, so without the diff it could
// not tell a line this change touched from one it did not. The untracked
// files follow, since a finding can point at one that no diff covers.
func subjectForSynthesis(r Resolved) string {
	var b strings.Builder
	if r.Subject.Audit {
		b.WriteString("\n" + filesSection(r.Subject.Scope, r.Subject.Files))
	} else {
		b.WriteString("\n## The diff under review\n\n" + fencedDiff(r.Subject.Diff))
	}
	if len(r.Untracked) > 0 {
		b.WriteString("\n" + untrackedNote(r.Untracked))
	}
	return b.String()
}

// fencedReport is one panelist's report in a fence it cannot close. The
// report is untrusted text shaped by whatever was in the diff, and unfenced
// it could forge a heading or a roster line the synthesizer would take as
// ours.
func fencedReport(o Outcome) string {
	fence := fenceFor(o.Stdout)
	return fmt.Sprintf("\n### %s / %s\n\n%stext\n%s\n%s\n", o.ID, sanitize.Line(o.Model), fence, strings.TrimRightFunc(o.Stdout, unicode.IsSpace), fence)
}

func countAnswered(outcomes []Outcome) int {
	n := 0
	for _, o := range outcomes {
		if o.Answered() {
			n++
		}
	}
	return n
}

// synthArgs is the dash-p argv for the synthesis. Read-only on purpose:
// checking a finding needs Read and Grep and nothing more, and the
// synthesizer has no business editing the tree it is judging.
func synthArgs(synth Spec, cwd string, timeoutSecs int64) []string {
	args := []string{
		"--harness=" + synth.Backend,
		"--output-format=text",
		"--timeout=" + strconv.FormatInt(timeoutSecs, 10),
		"--cwd=" + cwd,
		"--perms=read-only",
	}
	if synth.Model != "" {
		args = append(args, "--model="+synth.Model)
	}
	return args
}

// synthesize runs the one model call that turns the reviews into a report.
// It is supervised like a panelist: it is the same kind of process with the
// same ways to fail, and a wedge here, after every panelist is paid for, is
// the most expensive place in the run to hang.
func (r panelRun) synthesize(ctx context.Context, synth Spec, cwd, prompt string) (string, error) {
	promptPath := filepath.Join(r.outDir, "synthesis.prompt")
	if err := os.WriteFile(promptPath, []byte(prompt), 0o600); err != nil {
		return "", fmt.Errorf("writing the synthesis prompt: %w", err)
	}
	// A signal that came while the panel was collected must not start one
	// more model call.
	if ctx.Err() != nil {
		return "", errSynthesisInterrupted
	}
	r.status.Step("synthesizing with " + synth.Backend)
	stdoutPath, stderrPath := r.files("synthesis")
	p, err := r.starter.Start(proc.Command{
		Path:   r.dashp,
		Args:   synthArgs(synth, cwd, r.timeoutSecs),
		Stdin:  promptPath,
		Stdout: stdoutPath,
		Stderr: stderrPath,
	})
	if err != nil {
		return "", fmt.Errorf("starting the synthesis: %w", err)
	}
	started := time.Now()
	stopTicking := r.tickWhile(func() string {
		return fmt.Sprintf("synthesizing with %s, %s", synth.Backend, words.Duration(time.Since(started)))
	})
	e := supervise(ctx, p, r.limit())
	stopTicking()
	// Output that is not valid UTF-8 is still the report. Refusing it would
	// report a finished synthesis as a failure.
	text := strings.TrimSpace(strings.ToValidUTF8(readText(stdoutPath), "�"))
	if e.stopped != "" || e.exit.Signaled || e.exit.Code != 0 || text == "" {
		return "", r.synthesisFailed(e, stderrPath)
	}
	return text, nil
}

// synthesisFailed says why the synthesis produced no report, and that the
// panelist reports already printed are still the whole review.
func (r panelRun) synthesisFailed(e ended, stderrPath string) error {
	why := e.stopped
	if why == "" {
		why = "exit " + (Outcome{Exit: e.exit.Code, Exited: !e.exit.Signaled}).exitWord()
	}
	said := strings.TrimPrefix(detail(readText(stderrPath)), " and no error detail captured")
	return fmt.Errorf("the synthesis failed (%s)%s. The panelist reports above are still the full reviews, and %s holds them", why, sanitize.Line(said), r.outDir)
}

// tickWhile redraws the spinner with what message says until the returned
// function is called.
func (r panelRun) tickWhile(message func() string) func() {
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(tickEvery)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				r.status.Tick(message())
			}
		}
	}()
	return func() {
		close(stop)
		<-done
	}
}
