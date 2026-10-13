package stats

import (
	"cmp"
	"strings"
)

// key is one model on one backend, spelled one way. The names in a ledger
// are written by an agent, so the same model arrives as `xai/grok-4.6` and
// `grok-4.6`, and the same panelist as `codex` and `codex-gpt-5.6-sol`.
// Without one spelling a model's record is split across several rows, each
// too thin to trust.
type key struct {
	backend string
	model   string
}

// compare orders keys by backend and then by model.
func (k key) compare(other key) int {
	return cmp.Or(cmp.Compare(k.backend, other.backend), cmp.Compare(k.model, other.model))
}

// backends are the programs a panelist runs on. A name that starts with one
// is on it: `codex-gpt-5.6-sol` is on codex.
var backends = [...]string{"codex", "claude", "opencode"}

// noModel is the model of a panelist that never said what it ran.
const noModel = "unknown"

// lower folds only A to Z, as v1 does. strings.ToLower also folds letters
// outside ASCII, and would join two names that v1 keeps apart.
func lower(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}, s)
}

// tidy is a name as the fold compares it: no space around it, lower case.
func tidy(s string) string {
	return lower(strings.TrimSpace(s))
}

// backendOf is the backend a panelist name is on. A name on none of the
// known backends is on whatever comes before its first hyphen.
func backendOf(name string) string {
	name = tidy(name)
	for _, backend := range backends {
		if name == backend || strings.HasPrefix(name, backend+"-") {
			return backend
		}
	}
	first, _, _ := strings.Cut(name, "-")
	return first
}

// canonical is the cohort a panelist belongs to, from the name the run gave
// it and the model it reported. With no model, the model is what the name
// carries after the backend: `codex-gpt-5.6-sol` is gpt-5.6-sol.
func canonical(name string, model *string) key {
	backend := backendOf(name)
	return key{backend: backend, model: normalizeModel(rawModel(tidy(name), backend, model))}
}

func rawModel(name, backend string, model *string) string {
	if model != nil {
		reported := tidy(*model)
		// A "model" that is the panelist's name again says nothing the name
		// did not. The name carries the model.
		if reported != "" && reported != noModel && reported != "?" && reported != name {
			return reported
		}
	}
	if fromName, ok := strings.CutPrefix(name, backend+"-"); ok {
		return fromName
	}
	return noModel
}

// normalizeModel folds the spellings of one model together.
func normalizeModel(raw string) string {
	model := raw
	// A provider prefix says where the model was served from, and that is
	// the backend's business. A local ollama model is the exception: it is
	// not the hosted model of the same name.
	if cut := strings.LastIndex(raw, "/"); cut >= 0 {
		model = raw[cut+1:]
		if raw[:cut] == "ollama" {
			model = "ollama/" + model
		}
	}
	parts := strings.Split(model, "-")
	// A last part of eight digits is a release date
	// (`claude-haiku-4-5-20251001`). The dated snapshot is the same model
	// as the short id.
	if len(parts) >= 3 && digits(parts[len(parts)-1], 8, 8) {
		parts = parts[:len(parts)-1]
	}
	// `claude-fable-5-1` and `claude-fable-5.1` are one model. Only a part
	// of one or two digits is a minor version, so a date never joins as one.
	n := len(parts)
	if n >= 3 && digits(parts[n-1], 1, 2) && digits(parts[n-2], 1, 2) {
		return strings.Join(parts[:n-2], "-") + "-" + parts[n-2] + "." + parts[n-1]
	}
	return strings.Join(parts, "-")
}

// digits reports a string of only digits, between shortest and longest long.
func digits(s string, shortest, longest int) bool {
	if len(s) < shortest || len(s) > longest {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
