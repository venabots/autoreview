package cli

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// The variables that set a default for a flag. A flag wins over its
// variable, and an empty variable counts as unset.
const (
	EnvJobs         = "AUTOREVIEW_JOBS"
	EnvTimeout      = "AUTOREVIEW_TIMEOUT"
	EnvBudget       = "AUTOREVIEW_MAX_BUDGET_USD"
	EnvLogDir       = "AUTOREVIEW_LOG_DIR"
	EnvSkills       = "AUTOREVIEW_SKILLS"
	EnvOrchestrator = "AUTOREVIEW_ORCHESTRATOR"
	EnvFallback     = "AUTOREVIEW_FALLBACK"
	// EnvOverride names a shell command that replaces the built-in
	// reviewer. The PR number replaces each {} in it.
	EnvOverride = "AUTOREVIEW_CMD"
	// EnvRetiredOverride is the variable 0.24 read in place of EnvOverride
	// for a run with nobody at the terminal. Nothing reads its value now.
	EnvRetiredOverride = "AUTOREVIEW_AUTO_CMD"
)

const (
	defaultJobs    = "2"
	defaultTimeout = "3600"
	// MaxJobs, MaxPasses and MaxAutofixCap are the largest values a run
	// takes. The settings form checks its answers against them.
	MaxJobs    = 1024
	maxTimeout = 999_999_999
	// focusMax is the most text a prompt carries as a focus.
	focusMax = 2000
)

// chosen is the value that applies: the flag when it was given, else the
// variable, else the default.
func chosen(flag string, given bool, fromEnv, fallback string) string {
	switch {
	case given:
		return flag
	case fromEnv != "":
		return fromEnv
	}
	return fallback
}

// refuseFlagAsValue catches `--focus --no-post`. The parser would take the
// second flag as the first one's value, and the run would start with a
// focus of "--no-post" and post after all.
func refuseFlagAsValue(name, value string) error {
	if strings.HasPrefix(value, "--") {
		return fmt.Errorf("--%s expects a value, but found the flag %s", name, value)
	}
	return nil
}

// count reads a whole number within bounds. Both bounds are named in the
// error, so a person does not have to find them by trial.
func count(name, raw string, least, most int) (int, error) {
	return countOf("--"+name, raw, least, most)
}

// countOf is count for a value that the error calls what.
func countOf(what, raw string, least, most int) (int, error) {
	n, err := strconv.Atoi(raw)
	if err != nil || !allDigits(raw) || n < least {
		return 0, fmt.Errorf("%s expects an integer >= %d, got %q", what, least, raw)
	}
	if n > most {
		return 0, fmt.Errorf("%s expects an integer <= %d, got %q", what, most, raw)
	}
	return n, nil
}

// budget checks a cost cap. It stays text: it is passed on as written, and
// "2.50" must not become "2.5" on the way.
//
// A cap of zero is refused. It would stop every review at its first token,
// and it is far more likely a typo than a wish.
func budget(raw string) (string, error) {
	whole, fraction, hasFraction := strings.Cut(raw, ".")
	ok := allDigits(whole) && (!hasFraction || allDigits(fraction))
	if !ok || strings.Trim(raw, "0.") == "" {
		return "", fmt.Errorf("--budget expects a positive amount in USD, such as 2.50, got %q", raw)
	}
	return raw, nil
}

// ParseFocus checks what the reviewers are told to look at, and returns it
// in the form a prompt carries.
//
// The focus travels inside double quotes on a skill's command line. So a
// double quote becomes a single one and a trailing backslash is dropped:
// either would end the quoting early and turn the rest of the sentence
// into options. White space is collapsed, so a pasted paragraph is one
// line.
func ParseFocus(raw string) (string, error) {
	focus := strings.Join(strings.Fields(raw), " ")
	focus = strings.TrimRight(strings.ReplaceAll(focus, `"`, "'"), `\ `)
	if focus == "" {
		return "", errors.New("--focus expects text, not blank space")
	}
	if n := utf8.RuneCountInString(focus); n > focusMax {
		return "", fmt.Errorf("--focus is %d characters; the most a prompt carries is %d", n, focusMax)
	}
	return focus, nil
}

func seconds(n int) time.Duration {
	return time.Duration(n) * time.Second
}
