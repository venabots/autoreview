// Package review is one review job as data: the command line that starts
// it, what its exit status means, and what it left behind.
//
// A review is one dash-p process. dash-p drives claude or codex with a
// prompt that names a skill, enforces the timeout, and reports the result
// in a stable exit code: 0 ok, 10 agent error, 20 timeout. A command
// override replaces dash-p with a shell line, and is judged by its exit
// status only.
//
// Nothing here starts, waits for or kills a process. The engine does that
// from a Spec, so a test puts its fake where a process starts and every
// rule in this package runs without one. The one call that leaves the
// process is the readback in Verdict, and it goes through a gh.Runner.
package review

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/venabots/autoreview/internal/session"
)

// timeoutDisabled is what dash-p gets when the run has no timeout. It is
// still a value: dash-p's own default is 300 seconds, and a review that
// takes an hour would be cut off with nothing to say why.
const timeoutDisabled = 999_999_999

// GuardGrace is how long after dash-p's own timeout the run waits before it
// kills dash-p itself. dash-p enforces the real limit. The guard covers one
// known gap: dash-p waits without limit when the agent closes stdout and
// does not exit.
const GuardGrace = 5 * time.Second

// Request is everything that decides how one review starts.
type Request struct {
	// Task is the installed skill a task on one of your own PRs runs, or
	// empty for a review. A task writes no review: it gets no trailer
	// request, no focus and no staged reviewers, and no override replaces
	// it.
	Task         string
	PR           int
	Orchestrator Orchestrator
	// Session is the plan this review starts with. Only claude can be given
	// a session, so the plan reaches the command line under claude alone.
	Session session.Plan
	// NoPost runs the skill that has no posting step.
	NoPost bool
	// Focus is guidance for the reviewers, already validated. "" means none.
	Focus string
	// Timeout is the limit for the review. 0 disables it.
	Timeout time.Duration
	// Budget is the cost cap as the person gave it, for example "2.50",
	// already validated. "" means none.
	Budget string
	// SkillsDir is the staged agent directory, or "" when nothing was
	// staged.
	SkillsDir string
	// Override is a shell command that replaces dash-p, or "" for the
	// built-in reviewer.
	Override string
	// Dashp is the path of the dash-p binary.
	Dashp string
	// Panelists is the panel the review seats, each backend[:model], or
	// nil for the one panel-review.sh picks itself.
	Panelists []string
	// Dir is the working directory: the repo root.
	Dir string
	// Stdout, Stderr and Meta are files in the run directory.
	Stdout, Stderr, Meta string
}

// Spec is one process to start. It is data, so the code that starts a
// process is the only place a test needs a fake.
type Spec struct {
	// Path is the program: dash-p, or bash for an override.
	Path string
	// Args is the argv after the program.
	Args []string
	// Env is the "K=V" entries to add to the environment.
	Env []string
	Dir string
	// Stdout and Stderr are the files the process writes to.
	Stdout string
	Stderr string
	// Guard is when the run kills the process itself, measured from its
	// start. 0 means never.
	Guard time.Duration
	// Override says the process is the person's own command and not dash-p,
	// which changes what its exit status means.
	Override bool
}

// SpecFor is the process that runs one review.
func SpecFor(r Request) Spec {
	if r.Override != "" && r.Task == "" {
		return overrideSpec(r)
	}
	return Spec{
		Path:   r.Dashp,
		Args:   dashpArgs(r),
		Env:    panelEnv(r),
		Dir:    r.Dir,
		Stdout: r.Stdout,
		Stderr: r.Stderr,
		Guard:  dashpGuard(r.Timeout),
	}
}

// panelEnv hands the person's panel to panel-review.sh, which reads it from
// $PANEL_REVIEW_PANELISTS when no --panelist is given. A task seats no
// panel. The specs are joined by spaces, as the script splits them.
func panelEnv(r Request) []string {
	if r.Task != "" || len(r.Panelists) == 0 {
		return nil
	}
	return []string{"PANEL_REVIEW_PANELISTS=" + strings.Join(r.Panelists, " ")}
}

// Skill is the skill a review runs.
//
// Under NoPost it is panel-review, which reviews and reports and has no
// posting step to skip. To tell auto-review not to post would leave a model
// with write access and an instruction, and that is not the same as a model
// that cannot post.
//
// A resumed session already holds the earlier review. That is what
// recheck-pr needs, and what a fresh review would discard. The skill is
// recheck-pr only when the resume flag reaches the reviewer: a recheck in a
// session with no earlier review has nothing to check.
func Skill(r Request) string {
	switch {
	case r.Task != "":
		return r.Task
	case r.NoPost:
		return "panel-review"
	case sentFlag(r) == session.FlagResume:
		return "recheck-pr"
	}
	return "auto-review"
}

// Prompt is the prompt for one review: the skill by name, then the focus.
//
// The focus travels as the `--focus` option that panel-review and
// auto-review define, and not as prose a skill would have to interpret.
// recheck-pr does not define it and reads it as guidance. The prompt stays
// one line, because it is one argv element and one line in a call log.
func Prompt(r Request) string {
	base := r.Orchestrator.SkillPrompt(Skill(r), r.PR)
	if r.Focus == "" || r.Task != "" {
		return base
	}
	return fmt.Sprintf("%s --focus \"%s\"", base, r.Focus)
}

// dashpArgs is the argv for the built-in reviewer, in v1's order.
//
// `--meta-file` is always sent: stdout is empty after a timeout and after an
// interrupt, so the meta file is then the only result there is to read.
// `--timeout` is always sent for the reason timeoutDisabled gives.
//
// A flag that dash-p does not know is forwarded only in the one-token
// `--flag=value` form, and it is forwarded to claude only. A budget cap
// that is dropped in silence on an unattended run is the failure the cap
// exists to prevent, so the cap uses that form, and no such flag is sent to
// a backend that would not receive it.
func dashpArgs(r Request) []string {
	o := r.Orchestrator
	args := []string{
		"-H", o.Backend,
		"--output-format", "json",
		"--meta-file", r.Meta,
		"--timeout", strconv.FormatInt(dashpTimeout(r.Timeout), 10),
		"--dangerously-skip-permissions",
	}
	if o.Model != "" {
		args = append(args, "--model", o.Model)
	}
	if o.SupportsSystemPrompt() && r.Task == "" {
		args = append(args, "--append-system-prompt="+TrailerInstruction)
	}
	if r.SkillsDir != "" && o.DiscoversStagedSkills() && r.Task == "" {
		args = append(args, "--add-dir="+r.SkillsDir)
	}
	args = append(args, sessionArgs(r)...)
	if r.Budget != "" && o.SupportsBudget() {
		args = append(args, "--max-budget-usd="+r.Budget)
	}
	return append(args, "--", dashpPrompt(r))
}

// dashpPrompt is the prompt as dash-p receives it.
//
// Under claude the request for the trailer is in the system prompt, so the
// slash command is the whole prompt and nothing can stop it from starting
// the skill. No system prompt reaches codex, so there the request follows
// the skill line as a paragraph of its own. The skill line stays first and
// alone, so it is read as the instruction.
func dashpPrompt(r Request) string {
	if r.Orchestrator.SupportsSystemPrompt() || r.Task != "" {
		return Prompt(r)
	}
	return Prompt(r) + "\n\n" + TrailerInstruction
}

// sentFlag is the session flag that reaches the reviewer. It is FlagNone
// for a backend that takes no session, and for an id that is not
// UUID-shaped: such an id never goes onto a command line.
func sentFlag(r Request) session.Flag {
	if !r.Orchestrator.SupportsSessions() || !session.UUIDShaped(r.Session.ID) {
		return session.FlagNone
	}
	return r.Session.Flag
}

// sessionArgs is at most one session flag. `--session-id` and `--resume`
// together is an error in claude, and one flag value cannot ask for both.
func sessionArgs(r Request) []string {
	switch sentFlag(r) {
	case session.FlagPin:
		return []string{"--session-id", r.Session.ID}
	case session.FlagResume:
		return []string{"--resume", r.Session.ID}
	}
	return nil
}

// dashpTimeout is the timeout in the whole seconds dash-p counts in. A part
// of a second counts as a full one: to round it down would turn a short
// timeout into 0, and 0 is how a person turns the timeout off.
func dashpTimeout(d time.Duration) int64 {
	if d <= 0 {
		return timeoutDisabled
	}
	return int64((d + time.Second - 1) / time.Second)
}

// dashpGuard is the timeout dash-p was given, plus the grace.
func dashpGuard(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	return time.Duration(dashpTimeout(d))*time.Second + GuardGrace
}

// overrideSpec is the person's own command, run by bash.
//
// An override handles its own sessions. It gets the planned id and a resume
// flag in its environment under the names v1 used, which are a contract
// with every override already written. Both variables are always set, the
// id to "" when there is none to give.
//
// Nothing inside an override enforces the timeout, so its guard is the
// timeout exactly.
func overrideSpec(r Request) Spec {
	id, resume := "", "0"
	if session.UUIDShaped(r.Session.ID) {
		id = r.Session.ID
		if r.Session.Resumes() {
			resume = "1"
		}
	}
	return Spec{
		Path:     "bash",
		Args:     []string{"-c", overrideInvocation(r.Override, r.PR)},
		Env:      []string{"REVIEW_PRS_SESSION_ID=" + id, "REVIEW_PRS_SESSION_RESUME=" + resume},
		Dir:      r.Dir,
		Stdout:   r.Stdout,
		Stderr:   r.Stderr,
		Guard:    max(r.Timeout, 0),
		Override: true,
	}
}

// overrideInvocation is the shell line an override runs. The PR number
// replaces every "{}", or goes at the end when the command has none.
func overrideInvocation(command string, pr int) string {
	number := strconv.Itoa(pr)
	if strings.Contains(command, "{}") {
		return strings.ReplaceAll(command, "{}", number)
	}
	return command + " " + number
}
