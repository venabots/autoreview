package review

import (
	"fmt"
	"slices"
	"strings"

	"github.com/venabots/autoreview/internal/session"
)

// backends are the agent CLIs that can drive a review, in the order the
// automatic fallback tries them. The order makes codex the stand-in for
// claude, and claude the stand-in for codex.
var backends = [...]string{"codex", "claude"}

// Orchestrator is the agent CLI that runs the review skill: it starts the
// panel, writes the synthesis and posts the review.
//
// The two backends differ in what dash-p forwards to them. Each difference
// is a method here, so nothing learns of one from a flag that was dropped
// in silence.
type Orchestrator struct {
	// Backend is "claude" or "codex".
	Backend string
	// Model is the model the backend is pinned to, or "" for its default.
	Model string
}

// Claude is the default orchestrator.
func Claude() Orchestrator {
	return Orchestrator{Backend: "claude"}
}

// ParseOrchestrator reads `backend` or `backend:model`.
//
// A backend that cannot be handed a skill by name is refused here, before
// the run spends anything: opencode reviews as a panelist, but nothing can
// tell it which skill to run.
func ParseOrchestrator(raw string) (Orchestrator, error) {
	return parseSpec(raw, "orchestrator")
}

// parseSpec is ParseOrchestrator with the word the error uses for an
// unknown backend, so `--fallback gemini` is not reported as an orchestrator.
func parseSpec(raw, what string) (Orchestrator, error) {
	backend, model, pinned := strings.Cut(raw, ":")
	if !slices.Contains(backends[:], backend) {
		return Orchestrator{}, fmt.Errorf("unknown %s %q (expected one of: %s)", what, backend, strings.Join(backends[:], ", "))
	}
	if pinned && model == "" {
		return Orchestrator{}, fmt.Errorf("orchestrator %q names no model after the colon", raw)
	}
	return Orchestrator{Backend: backend, Model: model}, nil
}

// Label is the spec as a person types it: `claude`, or `codex:gpt-5.5`.
func (o Orchestrator) Label() string {
	if o.Model == "" {
		return o.Backend
	}
	return o.Backend + ":" + o.Model
}

// CLI is the program dash-p drives. It must be on PATH.
func (o Orchestrator) CLI() string {
	return o.Backend
}

// SupportsSessions says dash-p forwards `--session-id` and `--resume` to
// this backend. It forwards them to claude only. A codex review runs in a
// thread that codex allocates and nothing can ask for again, so a session
// flag sent to codex would promise a session that does not exist.
func (o Orchestrator) SupportsSessions() bool {
	return o.Backend == "claude"
}

// SupportsBudget says `--max-budget-usd` reaches this backend. It is a
// claude flag. A codex review runs with no cap, and the run must say so
// instead of sending a cap that is dropped.
func (o Orchestrator) SupportsBudget() bool {
	return o.Backend == "claude"
}

// BudgetNotes says where a cost cap does not apply: under an orchestrator
// that is not claude, and on a review that such a fallback takes over. A
// cap that quietly does not apply is worse than no cap, so the run prints
// these before its first review.
func BudgetNotes(budget string, o Orchestrator, fallback *Orchestrator) []string {
	if budget == "" {
		return nil
	}
	const why = "; dash-p forwards the cap to claude alone"
	notes := []string{}
	if !o.SupportsBudget() {
		notes = append(notes, "--budget is not enforced under "+o.Label()+why)
	}
	if fallback != nil && !fallback.SupportsBudget() {
		notes = append(notes, "--budget is not enforced on a review "+fallback.Label()+" takes over"+why)
	}
	return notes
}

// SupportsSystemPrompt says `--append-system-prompt` reaches this backend.
// For any other backend, what the reviewer must be told goes in the prompt.
func (o Orchestrator) SupportsSystemPrompt() bool {
	return o.Backend == "claude"
}

// DiscoversStagedSkills says this backend finds a skill in the directory a
// run stages and hands over with `--add-dir`.
//
// The staged tree is a `.claude/skills` directory. Claude reads it. Codex
// reads its own roots and its project root, and never a directory added at
// run time, so the flag would only widen what a codex run can write.
func (o Orchestrator) DiscoversStagedSkills() bool {
	return o.Backend == "claude"
}

// SkillsHome is where this backend looks for skills, for the message that
// tells a person how to make the review skills reachable.
func (o Orchestrator) SkillsHome() string {
	if o.Backend == "codex" {
		return "~/.agents/skills"
	}
	return "~/.claude/skills"
}

// SkillPrompt is the prompt that runs a skill by name on one PR.
//
// Claude Code takes a slash command. Codex keeps `/` for its own commands
// and takes `$name`. Both name the skill outright: an unattended review has
// no person to correct a prompt that did not start the skill.
func (o Orchestrator) SkillPrompt(skill string, pr int) string {
	if o.Backend == "codex" {
		return fmt.Sprintf("$%s %d", skill, pr)
	}
	return fmt.Sprintf("/%s %d", skill, pr)
}

// sessionPlaceholder stands for the id in a reopen command that names no
// session, as a legend under the summary does.
const sessionPlaceholder = "<SESSION>"

// ReopenCommand is the command that opens a finished review again:
// `claude --resume <id>` or `codex resume <id>`.
//
// The command is pasted into a shell, and the id can come from a file that
// an agent wrote. An id that is not UUID-shaped is not put on the line: the
// command then carries the word <SESSION>, which is also the form a legend
// wants when it has no id to give.
func (o Orchestrator) ReopenCommand(sessionID string) string {
	id := sessionPlaceholder
	if session.UUIDShaped(sessionID) {
		id = sessionID
	}
	if o.Backend == "codex" {
		return "codex resume " + id
	}
	return "claude --resume " + id
}

// otherThan is the backend an automatic fallback reaches for. To name the
// CLI is what makes "there is no fallback" something a person can fix.
func otherThan(primary Orchestrator) string {
	for _, backend := range backends {
		if backend != primary.Backend {
			return backend
		}
	}
	return "codex"
}
