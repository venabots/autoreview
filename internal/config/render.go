package config

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// Values is every setting with a value: the file the setup step writes.
type Values struct {
	Orchestrator string
	Fallback     string
	Jobs         int
	Timeout      int
	Budget       string
	Focus        string
	Verdicts     string

	Watch     string
	Rest      string
	MaxPasses int
	MaxIdle   int
	CIWait    string

	Approved   bool
	Dependabot bool
	Stacked    bool
	SkipCI     bool

	AutofixCap int

	Panelists []string
	Synth     string

	// Set is the keys the person chose: in the form, or in the file before
	// it. Every other key is written as a comment that shows the default,
	// so a later default reaches a person who never chose a value.
	Set map[string]bool
}

// Mark is what goes before the key's line: nothing for a key the person
// chose, and a comment mark for one that keeps the default.
func (v Values) Mark(key string) string {
	if v.Set[key] {
		return ""
	}
	return "# "
}

// fileTemplate writes every key with a comment that says what it does, so
// the file explains itself to the person who opens it in an editor.
var fileTemplate = template.Must(template.New("config.toml").Funcs(template.FuncMap{"q": quoted, "list": list}).Parse(
	`# autoreview settings. You can edit this file. ` + "`autoreview setup`" + ` writes it
# again from its answers, and does not keep comments that you add.
# A flag wins over its $AUTOREVIEW_* variable, and a variable wins over this
# file. The file never makes a run loop by itself. A key in a comment is not
# set: the run uses the built-in default that the comment shows.

[review]
# The agent that runs each review: claude or codex, with an optional model,
# as in codex:gpt-5.5.
{{.Mark "review.orchestrator"}}orchestrator = {{q .Orchestrator}}
# The agent that retries a review whose agent failed: auto, none, or a name.
{{.Mark "review.fallback"}}fallback = {{q .Fallback}}
# Reviews that run at once. A review is itself several agents.
{{.Mark "review.jobs"}}jobs = {{.Jobs}}
# Seconds before a review is stopped. 0 means no limit.
{{.Mark "review.timeout"}}timeout = {{.Timeout}}
# The most one review may spend, in USD, with claude. Empty means no cap.
{{.Mark "review.budget"}}budget = {{q .Budget}}
# What the reviewers look at in every run. Empty means nothing in particular.
{{.Mark "review.focus"}}focus = {{q .Focus}}
# The verdicts a review may post: approve-and-request-changes, approve-only
# (a blocking finding is a comment review), or comment-only (no verdict).
{{.Mark "review.verdicts"}}verdicts = {{q .Verdicts}}

[loop]
# The wait between two looks at the PR list. A bare number is minutes; 30m,
# 1h and 2d also work.
{{.Mark "loop.watch"}}watch = {{q .Watch}}
# How long a reviewed PR rests before it may be reviewed again.
{{.Mark "loop.rest"}}rest = {{q .Rest}}
# Reviews one PR gets in a run. While the run watches, a push starts the
# count again.
{{.Mark "loop.max_passes"}}max_passes = {{.MaxPasses}}
# Looks in a row that find nothing to review before --babysit stops.
{{.Mark "loop.max_idle"}}max_idle = {{.MaxIdle}}
# How long a run that looks once waits for checks that are still running.
{{.Mark "loop.ci_wait"}}ci_wait = {{q .CIWait}}

[prs]
# Review PRs that are approved already.
{{.Mark "prs.approved"}}approved = {{.Approved}}
# Review Dependabot's PRs.
{{.Mark "prs.dependabot"}}dependabot = {{.Dependabot}}
# Review a PR that sits on another open PR.
{{.Mark "prs.stacked"}}stacked = {{.Stacked}}
# Review a PR whose checks have not passed, and do not wait for them.
{{.Mark "prs.skip_ci"}}skip_ci = {{.SkipCI}}

[mine]
# The most fixes one of your PRs gets in a row while nobody else acts on it.
{{.Mark "mine.autofix_cap"}}autofix_cap = {{.AutofixCap}}

[panel]
# The models that review together, each backend[:model], for example
# ["claude:opus-4.8", "codex", "opencode:glm-5.2"]. Empty is every backend on
# PATH. Name at least 2: auto-review approves only when several review.
{{.Mark "panel.panelists"}}panelists = {{list .Panelists}}
# The model that writes the synthesis of ` + "`autoreview panel`" + `.
{{.Mark "panel.synth"}}synth = {{q .Synth}}
`))

// Render is the settings file for v.
func Render(v Values) ([]byte, error) {
	var b bytes.Buffer
	if err := fileTemplate.Execute(&b, v); err != nil {
		return nil, fmt.Errorf("writing the settings: %w", err)
	}
	return b.Bytes(), nil
}

// quoted is s as a TOML basic string. A quote, a backslash or a control
// character in a focus must not end the string early or break the file.
func quoted(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, "\\u%04X", r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func list(items []string) string {
	parts := make([]string, len(items))
	for i, item := range items {
		parts[i] = quoted(item)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
