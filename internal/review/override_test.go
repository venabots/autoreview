package review

import (
	"slices"
	"testing"
	"time"

	"github.com/venabots/autoreview/internal/session"
)

func TestTheGuardIsWhenTheRunKillsTheJobItself(t *testing.T) {
	override := func(r *Request) { r.Override = "my-review" }
	cases := []struct {
		name string
		r    Request
		want time.Duration
	}{
		// dash-p enforces the real limit, so the guard waits past it.
		{"dash-p gets the grace", with(func(r *Request) { r.Timeout = time.Hour }), time.Hour + 5*time.Second},
		{"dash-p with no timeout is never killed", request(), 0},
		{"the guard follows the timeout dash-p was given", with(func(r *Request) { r.Timeout = 1500 * time.Millisecond }), 7 * time.Second},
		// Nothing inside an override enforces the timeout.
		{"an override gets the timeout exactly", with(func(r *Request) { override(r); r.Timeout = time.Hour }), time.Hour},
		{"an override with no timeout is never killed", with(override), 0},
		{"a negative timeout is no timeout", with(func(r *Request) { override(r); r.Timeout = -time.Minute }), 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SpecFor(c.r).Guard; got != c.want {
				t.Errorf("Guard = %s, want %s", got, c.want)
			}
		})
	}
	if GuardGrace != 5*time.Second {
		t.Errorf("GuardGrace = %s, want v1's 5s", GuardGrace)
	}
}

func TestAnOverrideReplacesDashp(t *testing.T) {
	cases := []struct {
		name string
		r    Request
		line string
		env  []string
	}{
		{
			"a first review",
			with(func(r *Request) { r.Override = "my-review"; r.Session.Flag = pin }),
			"my-review 9",
			[]string{"REVIEW_PRS_SESSION_ID=" + sessionID, "REVIEW_PRS_SESSION_RESUME=0"},
		},
		{
			"a resumed review",
			with(func(r *Request) { r.Override = "my-review {} --extra {}"; r.Session.Flag = resume }),
			"my-review 9 --extra 9",
			[]string{"REVIEW_PRS_SESSION_ID=" + sessionID, "REVIEW_PRS_SESSION_RESUME=1"},
		},
		{
			// The contract is that both are always set.
			"no session",
			with(func(r *Request) { r.Override = "my-review"; r.Session = session.Plan{} }),
			"my-review 9",
			[]string{"REVIEW_PRS_SESSION_ID=", "REVIEW_PRS_SESSION_RESUME=0"},
		},
		{
			"an id that is not UUID-shaped is not handed to a shell script",
			with(func(r *Request) { r.Override = "my-review"; r.Session = session.Plan{ID: "$(reboot)", Flag: resume} }),
			"my-review 9",
			[]string{"REVIEW_PRS_SESSION_ID=", "REVIEW_PRS_SESSION_RESUME=0"},
		},
		{
			// The override replaces the reviewer whatever the orchestrator
			// is, and none of dash-p's flags go with it.
			"under codex, with a budget",
			with(func(r *Request) { r.Override = "my-review"; r.Orchestrator = codex(); r.Budget = "2.50" }),
			"my-review 9",
			[]string{"REVIEW_PRS_SESSION_ID=" + sessionID, "REVIEW_PRS_SESSION_RESUME=0"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			spec := SpecFor(c.r)
			if spec.Path != "bash" || !spec.Override {
				t.Errorf("Path = %q, Override = %v; want bash and true", spec.Path, spec.Override)
			}
			if want := []string{"-c", c.line}; !slices.Equal(spec.Args, want) {
				t.Errorf("Args = %q, want %q", spec.Args, want)
			}
			if !slices.Equal(spec.Env, c.env) {
				t.Errorf("Env = %q, want %q", spec.Env, c.env)
			}
			if spec.Dir != "/src/repo" || spec.Stdout != "/run/pr-9.json" || spec.Stderr != "/run/pr-9.log" {
				t.Errorf("Dir, Stdout, Stderr = %q, %q, %q", spec.Dir, spec.Stdout, spec.Stderr)
			}
		})
	}
}

func TestThePRNumberReplacesEveryPlaceholderOrGoesAtTheEnd(t *testing.T) {
	cases := []struct {
		command string
		pr      int
		want    string
	}{
		{"my-review", 9, "my-review 9"},
		{"my-review {} --extra {}", 9, "my-review 9 --extra 9"},
		{"gh pr checkout {} && my-review {}", 12, "gh pr checkout 12 && my-review 12"},
		{"my-review --pr={}", 240, "my-review --pr=240"},
	}
	for _, c := range cases {
		if got := overrideInvocation(c.command, c.pr); got != c.want {
			t.Errorf("overrideInvocation(%q, %d) = %q, want %q", c.command, c.pr, got, c.want)
		}
	}
}
