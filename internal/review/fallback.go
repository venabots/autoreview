package review

import "fmt"

// FallbackKind is which stand-in a person asked for.
type FallbackKind int

const (
	// FallbackAuto takes whichever other backend is installed. It is the
	// zero value, because a run that names no fallback wants one.
	FallbackAuto FallbackKind = iota
	// FallbackNone turns the stand-in off.
	FallbackNone
	// FallbackSpec names the stand-in.
	FallbackSpec
)

// Fallback is the orchestrator that takes over when the first one exits 10,
// as the person asked for it and before PATH is read.
//
// Exit 10 is the agent session failing: an outage or a usage limit. Nothing
// about the PR caused it, so the review is worth one more attempt under
// another provider.
//
// Auto becomes a backend only when the run starts. A decision at parse time
// would refuse `--help` on a machine with no codex, and would make the
// tests depend on the PATH of whoever runs them.
type Fallback struct {
	Kind FallbackKind
	// Spec is the named stand-in. It is set for FallbackSpec only.
	Spec Orchestrator
}

// ParseFallback reads `none`, `off`, `auto` or an orchestrator spec.
func ParseFallback(raw string) (Fallback, error) {
	switch raw {
	case "none", "off":
		return Fallback{Kind: FallbackNone}, nil
	case "auto":
		return Fallback{Kind: FallbackAuto}, nil
	}
	spec, err := parseSpec(raw, "fallback")
	if err != nil {
		return Fallback{}, err
	}
	return Fallback{Kind: FallbackSpec, Spec: spec}, nil
}

// Resolve is the fallback this run uses, and false when it has none.
// installed says a CLI is on PATH; the caller hands it in so the choice has
// a test.
//
// Auto takes the first backend that is not the primary's and is installed.
// It never carries the primary's model over: the stand-in runs on its own
// default.
//
// A named spec is returned as it is, installed or not. The caller checks
// it, because a person who named a fallback wants to hear that it is
// missing, and a person who named none wants the run to go ahead.
func (f Fallback) Resolve(primary Orchestrator, installed func(cli string) bool) (Orchestrator, bool) {
	switch f.Kind {
	case FallbackNone:
		return Orchestrator{}, false
	case FallbackSpec:
		return f.Spec, true
	}
	for _, backend := range backends {
		if backend != primary.Backend && installed(backend) {
			return Orchestrator{Backend: backend}, true
		}
	}
	return Orchestrator{}, false
}

// Describe is the words the first line of a run uses for the stand-in.
// resolved is what Resolve returned, or nil when it returned none.
//
// It says why there is no fallback. A bare "none" reads as a choice, when
// the cause can be a CLI that is not installed, and the person who wanted a
// fallback would learn that only from the pass that needed one.
func (f Fallback) Describe(primary Orchestrator, resolved *Orchestrator) string {
	switch {
	case resolved != nil:
		return resolved.Label()
	case f.Kind == FallbackNone:
		return "none"
	}
	return fmt.Sprintf("none (%s is not installed)", otherThan(primary))
}
