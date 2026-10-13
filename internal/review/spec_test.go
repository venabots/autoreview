package review

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/venabots/autoreview/internal/session"
)

const (
	pin    = session.FlagPin
	resume = session.FlagResume
)

// request is a first review of PR 9 under claude, with nothing asked for.
func request() Request {
	return Request{
		PR:           9,
		Orchestrator: Claude(),
		Session:      session.Plan{ID: sessionID},
		Dashp:        "/opt/bin/dash-p",
		Dir:          "/src/repo",
		Stdout:       "/run/pr-9.json",
		Stderr:       "/run/pr-9.log",
		Meta:         "/run/pr-9.meta.json",
	}
}

func with(edit func(*Request)) Request {
	r := request()
	edit(&r)
	return r
}

// head is the argv every built-in review starts with, after `-H backend`.
func head(backend, timeout string) []string {
	return []string{
		"-H", backend,
		"--output-format", "json",
		"--meta-file", "/run/pr-9.meta.json",
		"--timeout", timeout,
		"--dangerously-skip-permissions",
	}
}

func TestTheDashpArgvIsV1s(t *testing.T) {
	cases := []struct {
		name string
		r    Request
		want []string
	}{
		{
			"a first review under claude",
			request(),
			slices.Concat(head("claude", "999999999"), []string{
				"--append-system-prompt=" + TrailerInstruction,
				"--", "/auto-review 9",
			}),
		},
		{
			"everything a claude review can be given",
			with(func(r *Request) {
				r.Orchestrator.Model = "opus-4.8"
				r.Session.Flag = pin
				r.Timeout = time.Hour
				r.Budget = "2.50"
				r.SkillsDir = "/run/agent"
				r.Focus = "be strict about the migration"
			}),
			slices.Concat(head("claude", "3600"), []string{
				"--model", "opus-4.8",
				"--append-system-prompt=" + TrailerInstruction,
				"--add-dir=/run/agent",
				"--session-id", sessionID,
				"--max-budget-usd=2.50",
				"--", `/auto-review 9 --focus "be strict about the migration"`,
			}),
		},
		{
			"a resumed review",
			with(func(r *Request) { r.Session.Flag = resume }),
			slices.Concat(head("claude", "999999999"), []string{
				"--append-system-prompt=" + TrailerInstruction,
				"--resume", sessionID,
				"--", "/recheck-pr 9",
			}),
		},
		{
			// dash-p forwards the session, the budget, the system prompt
			// and the staged directory to claude alone. Each one sent to
			// codex would be dropped, and the summary would promise a
			// session and a cap that never existed.
			"codex gets what dash-p can forward to it",
			with(func(r *Request) {
				r.Orchestrator = Orchestrator{Backend: "codex", Model: "gpt-5.5"}
				r.Session.Flag = pin
				r.Timeout = time.Hour
				r.Budget = "2.50"
				r.SkillsDir = "/run/agent"
			}),
			slices.Concat(head("codex", "3600"), []string{
				"--model", "gpt-5.5",
				"--", "$auto-review 9\n\n" + TrailerInstruction,
			}),
		},
		{
			"a review that does not post",
			with(func(r *Request) { r.NoPost = true }),
			slices.Concat(head("claude", "999999999"), []string{
				"--append-system-prompt=" + TrailerInstruction,
				"--", "/panel-review 9",
			}),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			spec := SpecFor(c.r)
			if !slices.Equal(spec.Args, c.want) {
				t.Errorf("Args =\n%q\nwant\n%q", spec.Args, c.want)
			}
			if spec.Path != "/opt/bin/dash-p" || spec.Override {
				t.Errorf("Path = %q, Override = %v; want dash-p", spec.Path, spec.Override)
			}
			if spec.Dir != "/src/repo" || spec.Stdout != "/run/pr-9.json" || spec.Stderr != "/run/pr-9.log" {
				t.Errorf("Dir, Stdout, Stderr = %q, %q, %q", spec.Dir, spec.Stdout, spec.Stderr)
			}
			if len(spec.Env) != 0 {
				t.Errorf("Env = %q, want nothing added for dash-p", spec.Env)
			}
		})
	}
}

// after is the argument that follows flag.
func after(args []string, flag string) (string, bool) {
	i := slices.Index(args, flag)
	if i < 0 || i+1 >= len(args) {
		return "", false
	}
	return args[i+1], true
}

func hasPrefixed(args []string, prefix string) bool {
	return slices.ContainsFunc(args, func(arg string) bool { return strings.HasPrefix(arg, prefix) })
}

func TestTheTimeoutAndTheMetaFileAreAlwaysSent(t *testing.T) {
	cases := []struct {
		name    string
		r       Request
		timeout string
	}{
		// dash-p's own default is 300 seconds, so "no timeout" must still
		// be a value.
		{"no timeout", request(), "999999999"},
		{"a negative timeout is no timeout", with(func(r *Request) { r.Timeout = -time.Second }), "999999999"},
		{"an hour", with(func(r *Request) { r.Timeout = time.Hour }), "3600"},
		{"codex with no timeout", with(func(r *Request) { r.Orchestrator = codex() }), "999999999"},
		// Rounded down, this would be 0, which turns the timeout off.
		{"part of a second counts as one", with(func(r *Request) { r.Timeout = 300 * time.Millisecond }), "1"},
		{"a part over a whole second is the next one", with(func(r *Request) { r.Timeout = 1500 * time.Millisecond }), "2"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			args := SpecFor(c.r).Args
			if got, _ := after(args, "--timeout"); got != c.timeout {
				t.Errorf("--timeout %q, want %q", got, c.timeout)
			}
			// Stdout is empty after a timeout, so the meta file is then the
			// only result there is.
			if got, _ := after(args, "--meta-file"); got != c.r.Meta {
				t.Errorf("--meta-file %q, want %q", got, c.r.Meta)
			}
		})
	}
}

func TestAtMostOneSessionFlagIsSentAndOnlyToClaude(t *testing.T) {
	cases := []struct {
		name      string
		r         Request
		flag, id  string
		wantSkill string
	}{
		{"an earlier session that was not asked for gets no flag", request(), "", "", "auto-review"},
		{"a first review pins its id", with(func(r *Request) { r.Session.Flag = pin }), "--session-id", sessionID, "auto-review"},
		{"a resumed review resumes", with(func(r *Request) { r.Session.Flag = resume }), "--resume", sessionID, "recheck-pr"},
		{"codex is never pinned", with(func(r *Request) { r.Orchestrator = codex(); r.Session.Flag = pin }), "", "", "auto-review"},
		// A recheck in a thread with no earlier review has nothing to check.
		{"codex is never resumed, so it reviews from scratch", with(func(r *Request) { r.Orchestrator = codex(); r.Session.Flag = resume }), "", "", "auto-review"},
		{"a pin with no id sends nothing", with(func(r *Request) { r.Session = session.Plan{Flag: pin} }), "", "", "auto-review"},
		{"an id that is not UUID-shaped stays off the command line", with(func(r *Request) { r.Session = session.Plan{ID: "--model x", Flag: resume} }), "", "", "auto-review"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			args := SpecFor(c.r).Args
			sent := 0
			for _, flag := range []string{"--session-id", "--resume"} {
				id, ok := after(args, flag)
				if !ok {
					continue
				}
				sent++
				if flag != c.flag || id != c.id {
					t.Errorf("sent %s %q, want %q %q", flag, id, c.flag, c.id)
				}
			}
			if want := min(len(c.flag), 1); sent != want {
				t.Errorf("%d session flags in %q, want %d", sent, args, want)
			}
			if got := Skill(c.r); got != c.wantSkill {
				t.Errorf("Skill = %q, want %q", got, c.wantSkill)
			}
		})
	}
}

func TestAFlagDashpDoesNotKnowIsOneToken(t *testing.T) {
	// dash-p forwards an unknown flag only in the --flag=value form. The
	// two-token form is dropped, and a dropped budget cap is the failure
	// the cap exists to prevent.
	args := SpecFor(with(func(r *Request) {
		r.Budget = "2.50"
		r.SkillsDir = "/run/agent dir"
	})).Args
	for _, flag := range []string{"--max-budget-usd", "--add-dir", "--append-system-prompt"} {
		if slices.Contains(args, flag) {
			t.Errorf("%s is a token of its own in %q", flag, args)
		}
		if !hasPrefixed(args, flag+"=") {
			t.Errorf("no %s=value token in %q", flag, args)
		}
	}
	if !slices.Contains(args, "--add-dir=/run/agent dir") {
		t.Errorf("the staged directory is not one token in %q", args)
	}
}

func TestARunThatStagedNothingHandsTheReviewerNothing(t *testing.T) {
	if args := SpecFor(request()).Args; hasPrefixed(args, "--add-dir") {
		t.Errorf("--add-dir in %q with nothing staged", args)
	}
	if args := SpecFor(request()).Args; hasPrefixed(args, "--max-budget-usd") {
		t.Errorf("--max-budget-usd in %q with no budget", args)
	}
	if args := SpecFor(request()).Args; slices.Contains(args, "--model") {
		t.Errorf("--model in %q with no model pinned", args)
	}
}

func TestThePromptIsTheLastArgumentAfterTheSeparator(t *testing.T) {
	for _, r := range []Request{
		request(),
		with(func(r *Request) { r.Orchestrator = codex() }),
		with(func(r *Request) { r.Focus = "the 'fast' path" }),
	} {
		args := SpecFor(r).Args
		if args[len(args)-2] != "--" {
			t.Errorf("the argument before the prompt is %q, want --", args[len(args)-2])
		}
		if !strings.HasPrefix(args[len(args)-1], Prompt(r)) {
			t.Errorf("the last argument %q does not start with the prompt %q", args[len(args)-1], Prompt(r))
		}
	}
}

func TestCodexIsAskedForTheTrailerInThePrompt(t *testing.T) {
	r := with(func(r *Request) { r.Orchestrator = codex(); r.Focus = "be strict" })
	args := SpecFor(r).Args
	prompt := args[len(args)-1]

	// The skill line is first and alone, so it is read as the instruction.
	line, rest, _ := strings.Cut(prompt, "\n")
	if line != `$auto-review 9 --focus "be strict"` {
		t.Errorf("the first line is %q, want the skill line alone", line)
	}
	if rest != "\n"+TrailerInstruction {
		t.Errorf("after the skill line comes %q, want a blank line and the trailer request", rest)
	}
	if hasPrefixed(args, "--append-system-prompt") {
		t.Errorf("a system prompt was sent to codex, which never receives it: %q", args)
	}
	if strings.Contains(Prompt(r), TrailerInstruction) {
		t.Error("Prompt holds the trailer request, which is dash-p's part of the argv")
	}
}

func TestTheSkillFollowsTheRun(t *testing.T) {
	cases := []struct {
		name  string
		r     Request
		skill string
	}{
		{"a first review posts", request(), "auto-review"},
		{"a pinned review is still a first review", with(func(r *Request) { r.Session.Flag = pin }), "auto-review"},
		{"a resumed session is re-checked", with(func(r *Request) { r.Session.Flag = resume }), "recheck-pr"},
		// Every path that would post goes to the skill that cannot.
		{"no-post reviews with no posting step", with(func(r *Request) { r.NoPost = true }), "panel-review"},
		{"a recheck posts too, so no-post outranks it", with(func(r *Request) { r.NoPost = true; r.Session.Flag = resume }), "panel-review"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Skill(c.r); got != c.skill {
				t.Errorf("Skill = %q, want %q", got, c.skill)
			}
			if want := "/" + c.skill + " 9"; Prompt(c.r) != want {
				t.Errorf("Prompt = %q, want %q", Prompt(c.r), want)
			}
		})
	}
}

func TestAFocusIsAnOptionTheSkillsUnderstand(t *testing.T) {
	cases := []struct {
		name string
		r    Request
		want string
	}{
		{"under claude", with(func(r *Request) { r.Focus = "be strict about the migration" }), `/auto-review 9 --focus "be strict about the migration"`},
		{"under codex", with(func(r *Request) { r.Orchestrator = codex(); r.Focus = "be strict" }), `$auto-review 9 --focus "be strict"`},
		// The parser made the text safe. The prompt only wraps it.
		{"a quote does not close the option", with(func(r *Request) { r.Focus = "the 'fast' path" }), `/auto-review 9 --focus "the 'fast' path"`},
		{"no focus", request(), "/auto-review 9"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Prompt(c.r)
			if got != c.want {
				t.Errorf("Prompt = %q, want %q", got, c.want)
			}
			// The prompt is one argv element and one line in a call log.
			if strings.Contains(got, "\n") {
				t.Errorf("Prompt %q is more than one line", got)
			}
		})
	}
}

func TestATaskRunsItsSkillAndNothingElse(t *testing.T) {
	r := Request{
		PR: 4, Orchestrator: Claude(), Task: "babysit-pr", Focus: "be strict", SkillsDir: "/run/agent",
		Override: "my-review", Dashp: "dash-p", Dir: "/run/worktrees/pr-4", Meta: "/m.json",
		Session: session.Plan{ID: "0d0d0d0d-1111-4222-8333-444455556666", Flag: session.FlagPin},
	}
	spec := SpecFor(r)
	args := strings.Join(spec.Args, " ")
	if spec.Path != "dash-p" || spec.Override || spec.Dir != "/run/worktrees/pr-4" {
		t.Errorf("spec = %+v, want dash-p in the worktree, whatever the override says", spec)
	}
	if !strings.HasSuffix(args, "--session-id 0d0d0d0d-1111-4222-8333-444455556666 -- /babysit-pr 4") {
		t.Errorf("argv ends %q, want the pinned session and the skill alone", args[max(len(args)-80, 0):])
	}
	for _, unwanted := range []string{"--append-system-prompt", "--add-dir", "--focus"} {
		if strings.Contains(args, unwanted) {
			t.Errorf("a task was sent %s: %s", unwanted, args)
		}
	}
	r.Orchestrator = Orchestrator{Backend: "codex"}
	if got := SpecFor(r).Args; got[len(got)-1] != "$babysit-pr 4" {
		t.Errorf("a codex task's prompt is %q, want the skill and no trailer request", got[len(got)-1])
	}
}
