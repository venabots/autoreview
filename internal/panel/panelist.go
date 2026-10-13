package panel

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Backends are the agent CLIs a panel can seat, probed in this order. It is
// also the order they appear in the report.
var Backends = []string{"codex", "claude", "opencode"}

// unknownModel is what an outcome says when the panelist named no model.
const unknownModel = "unknown"

// maxModelRunes bounds the model a panelist reports. It goes into a section
// heading, a progress line and the synthesis roster, and a first line of
// ten thousand characters must not decide how wide any of them are.
const maxModelRunes = 80

// Spec is a panelist as asked for on the command line: a backend, and the
// model to pin it to, or "" for the backend's own default.
type Spec struct {
	Backend string
	Model   string
}

// Panelist is a spec as run: with the id that names its files, its
// worktree and its section heading.
type Panelist struct {
	ID      string
	Backend string
	Model   string
}

// ParseSpec reads `backend` or `backend:model`. what names the flag in an
// error, so a bad --synth is not reported as a bad panelist.
func ParseSpec(raw, what string) (Spec, error) {
	backend, model, pinned := strings.Cut(raw, ":")
	if !slices.Contains(Backends, backend) {
		return Spec{}, fmt.Errorf("unknown %s backend %q (expected one of: %s)", what, backend, strings.Join(Backends, ", "))
	}
	if pinned && model == "" {
		return Spec{}, fmt.Errorf("%s %q names no model after the colon", what, raw)
	}
	return Spec{Backend: backend, Model: model}, nil
}

// Autodetect is every backend that has finds on PATH, each on its own
// default model. It is the panel when nobody names one.
func Autodetect(has func(string) bool) []Spec {
	var found []Spec
	for _, backend := range Backends {
		if has(backend) {
			found = append(found, Spec{Backend: backend})
		}
	}
	return found
}

// ResolvePanel gives each spec an id. Two panelists that shared an id would
// write the same output files and overwrite each other's review, so the
// model is part of the id when there is one and a number settles the rest.
func ResolvePanel(specs []Spec) []Panelist {
	out := make([]Panelist, 0, len(specs))
	used := map[string]bool{}
	for _, spec := range specs {
		base := spec.Backend
		if spec.Model != "" {
			base += "-" + pathSafe(spec.Model)
		}
		id := base
		for n := 2; used[id]; n++ {
			id = base + "-" + strconv.Itoa(n)
		}
		used[id] = true
		out = append(out, Panelist{ID: id, Backend: spec.Backend, Model: spec.Model})
	}
	return out
}

// pathSafe turns every byte outside [A-Za-z0-9._] into a dash. The id goes
// into file names and `git worktree add`, so a model like `zai/glm-5.3` or
// `../etc` must not carry its separators there.
func pathSafe(raw string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_':
			return r
		}
		return '-'
	}, raw)
}

// DashpArgs is the dash-p argv for one panelist, every flag in the
// one-token `--flag=value` form dash-p reads for its own flags and forwards
// for the rest.
//
// No prompt rides in it: the prompt arrives on stdin, because a large diff
// in one argument fails the exec on Linux and works on macOS, which is how
// that bug would reach a Linux user unnoticed. `--output-format=text` makes
// stdout the panelist's final message and nothing else, whose first line is
// the `Model:` line the report reads back.
func DashpArgs(p Panelist, cwd string, timeoutSecs int64, isolated bool) []string {
	args := []string{
		"--harness=" + p.Backend,
		"--output-format=text",
		"--timeout=" + strconv.FormatInt(timeoutSecs, 10),
		"--cwd=" + cwd,
	}
	if p.Model != "" {
		args = append(args, "--model="+p.Model)
	}
	if isolated {
		// A throwaway worktree: the panelist may run the tests and edit
		// files to investigate, and nothing it does outlives the run.
		return append(args, "--dangerously-skip-permissions")
	}
	// The person's own working tree. Read, grep, reason: nothing else.
	return append(args, "--perms=read-only")
}

// ExtractModel is the model a panelist says it ran on. Every prompt asks
// for `Model: <id>` as the first line. Only the first line counts: a
// `Model:` further down is the panelist quoting something.
func ExtractModel(stdout string) (string, bool) {
	first, _, _ := strings.Cut(stdout, "\n")
	rest, ok := strings.CutPrefix(strings.TrimSpace(first), "Model:")
	rest = strings.TrimSpace(rest)
	if !ok || rest == "" {
		return "", false
	}
	runes := []rune(rest)
	return string(runes[:min(len(runes), maxModelRunes)]), true
}
