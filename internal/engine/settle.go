package engine

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/venabots/autoreview/internal/proc"
	"github.com/venabots/autoreview/internal/review"
	"github.com/venabots/autoreview/internal/rundir"
	"github.com/venabots/autoreview/internal/sanitize"
	"github.com/venabots/autoreview/internal/session"
)

// facts is what a review's monitor knows about it. They are fixed when the
// review starts, so the monitor reads no state the engine may change.
type facts struct {
	view Job
	plan session.Plan
	spec review.Spec
	pass rundir.Pass
}

// outcome is what a finished review left behind, read off the engine's
// goroutine. The engine uses it only when the review ran to its end.
type outcome struct {
	verdict string
	trailer *review.Trailer
	// text is the review as written, for the ledger.
	text    string
	hasText bool
	// agentError is why the harness said a failed review failed.
	agentError string
	notes      []string
}

// monitor waits for one review and reports twice: when its process ends,
// and when what it left behind has been read. Both go through the one
// inbox, in that order, so the engine always sees the end before the
// result.
func (e *Engine) monitor(ctx context.Context, f facts, p proc.Proc) {
	exit := p.Wait()
	elapsed := time.Since(f.view.StartedAt)
	meta := readMeta(f)
	e.in.put(jobReaped{pr: f.view.PR, exit: exit, elapsed: elapsed, meta: meta})
	e.in.put(jobSettled{pr: f.view.PR, outcome: e.safeOutcome(ctx, f, exit, meta)})
}

// readMeta reads what the harness wrote about the job. An override
// promises no such file.
func readMeta(f facts) *review.Meta {
	if f.spec.Override {
		return nil
	}
	meta, ok := review.ReadMeta(f.pass.Meta(f.view.PR))
	if !ok {
		return nil
	}
	return &meta
}

// safeOutcome turns a panic while reading a review's results into a note.
// A monitor that died would leave its review finishing forever, and the
// run would never end.
func (e *Engine) safeOutcome(ctx context.Context, f facts, exit proc.Exit, meta *review.Meta) (out outcome) {
	defer func() {
		if p := recover(); p != nil {
			out = outcome{notes: []string{fmt.Sprintf("could not read the results of PR #%d: %v", f.view.PR, p)}}
		}
	}()
	return e.outcomeOf(ctx, f, exit, meta)
}

func (e *Engine) outcomeOf(ctx context.Context, f facts, exit proc.Exit, meta *review.Meta) outcome {
	pr := f.view.PR
	override := f.spec.Override
	if exit.Signaled || exit.Code != 0 {
		if override {
			return outcome{}
		}
		// The status says a review failed. This says which way, which is
		// the difference between "try again later" and "stop".
		msg, _ := review.AgentError(f.spec.Stdout)
		return outcome{agentError: msg}
	}

	out := outcome{}
	sid, _ := review.SummarySession(f.plan, meta, override)
	transcript := ""
	if sid != "" {
		transcript, _ = e.deps.Transcript(sid)
	}
	since := f.view.StartedAt

	out.text, out.hasText = review.ReadReview(f.spec.Stdout, transcript, since)
	if out.hasText {
		if err := os.WriteFile(f.pass.Review(pr), []byte(out.text+"\n"), 0o600); err != nil {
			out.notes = append(out.notes, fmt.Sprintf("could not write the review of PR #%d: %s", pr, sanitize.Line(err.Error())))
		}
	}
	if !override {
		if trailer, ok := review.ReadTrailer(f.spec.Stdout, transcript, since); ok {
			out.trailer = &trailer
		}
	}
	if sid != "" && f.view.Orchestrator.SupportsSessions() {
		// Kept for a person, and for a later pass to resume. A failure to
		// write it costs that and nothing else.
		_ = e.deps.Run.RecordSession(pr, sid)
	}
	verdict, notes := e.verdict(ctx, pr, since, out.trailer)
	out.verdict = verdict
	out.notes = append(out.notes, notes...)
	return out
}

// verdict is what landed on the PR. GitHub's answer outranks the
// reviewer's own word, and an answer that says nothing landed vetoes a
// claim that something did.
//
// Under --no-post nothing was posted, so nobody is asked and the
// reviewer's claim is not read.
func (e *Engine) verdict(ctx context.Context, pr int, since time.Time, trailer *review.Trailer) (string, []string) {
	if e.cfg.NoPost {
		return "", nil
	}
	if e.quitting.Load() {
		// The run is ending. The reviewer's own word is better than a
		// question nobody will wait for.
		verdict, _ := review.ResolveVerdict(review.Readback{Kind: review.ReadbackFailed}, trailer)
		return verdict, nil
	}
	readback := e.deps.Verdict(ctx, pr, since)
	verdict, _ := review.ResolveVerdict(readback, trailer)
	notes := []string{}
	if claim, ok := review.VetoedClaim(readback, trailer); ok {
		notes = append(notes, fmt.Sprintf("PR #%d: the reviewer reported %q, but GitHub shows no review from you; the verdict is left empty", pr, claim))
	}
	if readback.Kind == review.ReadbackFailed {
		notes = append(notes, fmt.Sprintf("could not read PR #%d's review back from GitHub; its verdict is the reviewer's own word", pr))
	}
	return verdict, notes
}
