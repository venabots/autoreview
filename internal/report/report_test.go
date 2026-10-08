package report

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/review"
)

func ptr[T any](v T) *T { return &v }

func counts(must, should, polish *uint) *review.Trailer {
	return &review.Trailer{Findings: &review.Findings{MustFix: must, ShouldFix: should, Polish: polish}}
}

func TestResult(t *testing.T) {
	cases := []struct {
		job  engine.Job
		want string
	}{
		{engine.Job{State: engine.JobDone}, "done"},
		{engine.Job{State: engine.JobTimedOut}, "timed out"},
		{engine.Job{State: engine.JobFailed, Outcome: "exit 10"}, "failed (exit 10)"},
		{engine.Job{State: engine.JobFailed, Outcome: "stopped"}, "failed (stopped)"},
		{engine.Job{State: engine.JobFinishing}, "finishing"},
		{engine.Job{State: engine.JobRunning}, "running"},
		{engine.Job{State: engine.JobQueued}, "queued"},
	}
	for _, c := range cases {
		if got := Result(c.job); got != c.want {
			t.Errorf("Result(%d) = %q, want %q", c.job.State, got, c.want)
		}
	}
}

func TestVerdictSaysWhenNothingWasPosted(t *testing.T) {
	if got := Verdict(""); got != "nothing posted" {
		t.Errorf("Verdict of nothing = %q", got)
	}
	if got := Verdict("changes requested"); got != "changes requested" {
		t.Errorf("Verdict = %q", got)
	}
}

func TestFindings(t *testing.T) {
	cases := []struct {
		name    string
		trailer *review.Trailer
		want    string
	}{
		{"no trailer", nil, "-"},
		{"a trailer with no counts", &review.Trailer{}, "-"},
		{"all three are zero", counts(ptr(uint(0)), ptr(uint(0)), ptr(uint(0))), "none"},
		{"one bucket", counts(ptr(uint(0)), ptr(uint(2)), ptr(uint(0))), "2 should-fix"},
		{"every bucket", counts(ptr(uint(1)), ptr(uint(2)), ptr(uint(3))), "1 must-fix, 2 should-fix, 3 polish"},
		{"zeros with one bucket left out are not a clean report", counts(ptr(uint(0)), nil, ptr(uint(0))), "-"},
		{"a count beside a bucket left out", counts(nil, ptr(uint(1)), nil), "1 should-fix"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Findings(c.trailer); got != c.want {
				t.Errorf("Findings = %q, want %q", got, c.want)
			}
		})
	}
}

func TestCost(t *testing.T) {
	cases := []struct {
		cost *float64
		want string
	}{{nil, "-"}, {ptr(0.0), "$0.00"}, {ptr(0.416), "$0.42"}, {ptr(12.5), "$12.50"}, {ptr(-1.0), "-"}}
	for _, c := range cases {
		if got := Cost(c.cost); got != c.want {
			t.Errorf("Cost = %q, want %q", got, c.want)
		}
	}
}

func TestPanelist(t *testing.T) {
	cases := []struct {
		name string
		p    review.Panelist
		want string
	}{
		{"with findings", review.Panelist{Name: "codex", Model: "gpt-5.5", OK: ptr(true), Findings: ptr(uint(3)), Top: "MEDIUM"}, "codex (gpt-5.5) 3 findings, top MEDIUM"},
		{"one finding", review.Panelist{Name: "codex", Model: "gpt-5.5", Findings: ptr(uint(1)), Top: "LOW"}, "codex (gpt-5.5) 1 finding, top LOW"},
		{"clean", review.Panelist{Name: "claude", Model: "opus", Findings: ptr(uint(0)), Top: "LOW"}, "claude (opus) clean"},
		{"failed", review.Panelist{Name: "opencode", Model: "glm", OK: ptr(false), Findings: ptr(uint(2))}, "opencode (glm) failed"},
		{"nothing reported", review.Panelist{}, "? (unknown)"},
		{"findings with no top", review.Panelist{Name: "codex", Model: "m", Findings: ptr(uint(2))}, "codex (m) 2 findings"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Panelist(c.p); got != c.want {
				t.Errorf("Panelist = %q, want %q", got, c.want)
			}
		})
	}
}

func jobs() []engine.Job {
	return []engine.Job{
		{
			PR: 9, State: engine.JobDone, Verdict: "commented", Elapsed: 63 * time.Second,
			Cost: ptr(0.42), Model: "claude-fable-5", Session: "7442b624-5cba-5d44-ae67-9c390cfe70a1",
			Orchestrator: review.Claude(),
			Trailer: &review.Trailer{
				Risk:     "MEDIUM",
				Findings: &review.Findings{MustFix: ptr(uint(0)), ShouldFix: ptr(uint(1)), Polish: ptr(uint(0))},
				Panel: []review.Panelist{
					{Name: "codex", Model: "gpt-5.5", OK: ptr(true), Findings: ptr(uint(1)), Top: "MEDIUM"},
					{Name: "claude", Model: "opus", OK: ptr(true), Findings: ptr(uint(0))},
				},
				Blockers: []review.Blocker{{Severity: "MEDIUM", Domain: "money", Reversible: ptr(false), Location: "src/pay.rs:88", Gist: "a retried checkout charges the card twice"}},
			},
		},
		{PR: 108, State: engine.JobFailed, Outcome: "exit 10", Error: "You've hit your session limit", Elapsed: 3 * time.Second, Orchestrator: review.Claude()},
		{PR: 7, State: engine.JobFailed, Outcome: "exit 10", Error: "You've hit your session limit", Elapsed: 2 * time.Second, Orchestrator: review.Claude()},
		{PR: 6, State: engine.JobTimedOut, Elapsed: time.Hour, Orchestrator: review.Claude()},
	}
}

func TestSummary(t *testing.T) {
	want := `PR    RESULT            VERDICT         RISK    FINDINGS      TIME   COST   MODEL           SESSION
#9    done              commented       MEDIUM  1 should-fix  1m03s  $0.42  claude-fable-5  7442b624-5cba-5d44-ae67-9c390cfe70a1
#108  failed (exit 10)  nothing posted  -       -             3s     -      -               -
#7    failed (exit 10)  nothing posted  -       -             2s     -      -               -
#6    timed out         nothing posted  -       -             1h00m  -      -               -
#9 not approved yet because:
  - [MEDIUM] (money, irreversible) src/pay.rs:88 — a retried checkout charges the card twice
panel #9: codex (gpt-5.5) 1 finding, top MEDIUM; claude (opus) clean
error #108 #7: You've hit your session limit
`
	if got := Summary(jobs()); got != want {
		t.Errorf("Summary:\n%s\nwant:\n%s", got, want)
	}
}

func TestSummaryNamesACleanReviewThatTooFewAnswered(t *testing.T) {
	clean := []engine.Job{{
		PR: 9, State: engine.JobDone, Verdict: "commented",
		Trailer: &review.Trailer{
			Findings: &review.Findings{MustFix: ptr(uint(0)), ShouldFix: ptr(uint(0)), Polish: ptr(uint(1))},
			Panel: []review.Panelist{
				{Name: "a", Model: "m", OK: ptr(true)}, {Name: "b", Model: "m", OK: ptr(true)},
				{Name: "c", Model: "m", OK: ptr(false)}, {Name: "d", Model: "m", OK: ptr(false)},
			},
		},
	}}
	want := "#9 looks clean, but only 2 of 4 reviewers answered; approval needs 3\n"
	got := Summary(clean)
	if !strings.Contains(got, want) {
		t.Errorf("Summary:\n%s\nwant a line %q", got, want)
	}
}

func TestSummaryOfNoJobsIsOnlyTheHeader(t *testing.T) {
	if got := Summary(nil); got != "PR  RESULT  VERDICT  RISK  FINDINGS  TIME  COST  MODEL  SESSION\n" {
		t.Errorf("Summary(nil) = %q", got)
	}
}

func TestAlignMeasuresDisplayWidth(t *testing.T) {
	got := align([][]string{{"MODEL", "X"}, {"日本", "y"}, {"a", "z"}})
	if want := "MODEL  X\n日本   y\na      z"; got != want {
		t.Errorf("align = %q, want %q", got, want)
	}
}

func TestErrorLinesGroupTheSameReason(t *testing.T) {
	list := []engine.Job{
		{PR: 1, State: engine.JobFailed, Error: "usage limit"},
		{PR: 2, State: engine.JobFailed, Error: "API error"},
		{PR: 3, State: engine.JobFailed, Error: "usage limit"},
		{PR: 4, State: engine.JobFailed},
		{PR: 5, State: engine.JobDone, Error: "usage limit"},
		{PR: 6, State: engine.JobTimedOut, Error: "usage limit"},
	}
	want := []string{"error #1 #3: usage limit", "error #2: API error"}
	if got := ErrorLines(list); !slices.Equal(got, want) {
		t.Errorf("ErrorLines = %q, want %q", got, want)
	}
}

func TestReopenHints(t *testing.T) {
	codex := review.Orchestrator{Backend: "codex"}
	cases := []struct {
		name string
		jobs []engine.Job
		want []string
	}{
		{"no session at all still says how", []engine.Job{{PR: 1}}, []string{"reopen any review with: claude --resume <SESSION>"}},
		{"claude", []engine.Job{{PR: 1, Session: "s", Orchestrator: review.Claude()}, {PR: 2, Session: "t", Orchestrator: review.Claude()}},
			[]string{"reopen any review with: claude --resume <SESSION>"}},
		{"both agents", []engine.Job{{PR: 1, Session: "s", Orchestrator: codex}, {PR: 2, Session: "t", Orchestrator: review.Claude()}},
			[]string{"reopen any review with: codex resume <SESSION>", "  or, for a claude review: claude --resume <SESSION>"}},
		{"an agent whose review has no session is not named", []engine.Job{{PR: 1, Orchestrator: codex}, {PR: 2, Session: "t", Orchestrator: review.Claude()}},
			[]string{"reopen any review with: claude --resume <SESSION>"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ReopenHints(c.jobs); !slices.Equal(got, c.want) {
				t.Errorf("ReopenHints = %q, want %q", got, c.want)
			}
		})
	}
}

func TestProgress(t *testing.T) {
	began := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	now := began.Add(63 * time.Second)
	cases := []struct {
		job  engine.Job
		want string
	}{
		{engine.Job{State: engine.JobQueued}, "queued"},
		{engine.Job{State: engine.JobRunning, StartedAt: began}, "reviewing 1m03s"},
		{engine.Job{State: engine.JobRunning, StartedAt: began, Resumed: true}, "rechecking 1m03s"},
		{engine.Job{State: engine.JobFinishing, StartedAt: began}, "finishing"},
	}
	for _, c := range cases {
		if got := Progress(c.job, now); got != c.want {
			t.Errorf("Progress = %q, want %q", got, c.want)
		}
	}
}

func TestOutcome(t *testing.T) {
	short := &review.Trailer{
		Findings: &review.Findings{MustFix: ptr(uint(0)), ShouldFix: ptr(uint(0)), Polish: ptr(uint(0))},
		Panel:    []review.Panelist{{Name: "a", OK: ptr(true)}, {Name: "b", OK: ptr(true)}, {Name: "c", OK: ptr(false)}, {Name: "d", OK: ptr(false)}},
	}
	cases := []struct {
		name string
		job  engine.Job
		want string
	}{
		{"approved", engine.Job{State: engine.JobDone, Verdict: "approved"}, "approved"},
		{"changes requested, cut to fit", engine.Job{State: engine.JobDone, Verdict: "changes requested"}, "changes req"},
		{"commented", engine.Job{State: engine.JobDone, Verdict: "commented"}, "commented"},
		{"nothing landed", engine.Job{State: engine.JobDone}, "no verdict"},
		{"clean, but too few answered", engine.Job{State: engine.JobDone, Verdict: "commented", Trailer: short}, "clean 2/4"},
		{"approved outranks a short panel", engine.Job{State: engine.JobDone, Verdict: "approved", Trailer: short}, "approved"},
		{"timed out", engine.Job{State: engine.JobTimedOut}, "timed out"},
		{"stopped by a person", engine.Job{State: engine.JobFailed, Outcome: "stopped"}, "stopped"},
		{"failed", engine.Job{State: engine.JobFailed, Outcome: "exit 10"}, "failed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Outcome(c.job); got != c.want {
				t.Errorf("Outcome = %q, want %q", got, c.want)
			}
		})
	}
}
