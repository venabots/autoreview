package panel

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// The environment variables v1 reads, under v1's names, so a shell profile
// written for v1 still sets the same defaults.
const (
	EnvTimeout = "PANEL_TIMEOUT"
	EnvSynth   = "PANEL_SYNTH"
)

const (
	defaultTimeout = "600"
	defaultSynth   = "claude"
	maxTimeout     = 999_999_999
)

// Config is one panel run as the flags asked for it.
type Config struct {
	Target Target
	// Panelists is empty for "every backend on PATH".
	Panelists []Spec
	// Focus is "" when the reviewers were given none.
	Focus       string
	TimeoutSecs int64
	// LogDir is "" for a directory under the system's temp directory.
	LogDir     string
	Synthesize bool
	Synth      Spec
	// synthChosen says --synth or $PANEL_SYNTH named the synthesizer, so
	// the settings file does not.
	synthChosen bool
}

// Parsed is what the arguments asked for: a run, the help or the version.
type Parsed struct {
	Config  Config
	Help    bool
	Version bool
}

// errUnknownArg marks an argument that is no flag at all. The help follows
// it, because the person has the wrong idea of the flags.
var errUnknownArg = errors.New("unknown arg")

// WantsHelp says the error came from an argument that is no flag, so the
// help belongs after it.
func WantsHelp(err error) bool {
	return errors.Is(err, errUnknownArg)
}

// parser is the state of one pass over the arguments.
type parser struct {
	cfg         Config
	timeoutRaw  string
	synthRaw    string
	synthChosen bool
}

// Parse reads the arguments after the word "panel". The parser is written by
// hand, as v1's is: `--tree` takes an optional path that a following flag
// must not be read as, and a flag library would read `--tree --focus` as a
// tree audit of a directory named "--focus".
func Parse(args []string, env func(string) string) (Parsed, error) {
	p := parser{
		cfg:         Config{Target: Target{Kind: Uncommitted}, Synthesize: true},
		timeoutRaw:  orDefault(env(EnvTimeout), defaultTimeout),
		synthRaw:    orDefault(env(EnvSynth), defaultSynth),
		synthChosen: env(EnvSynth) != "",
	}
	for at := 0; at < len(args); at++ {
		switch args[at] {
		case "--help", "-h":
			return Parsed{Help: true}, nil
		case "--version", "-V":
			return Parsed{Version: true}, nil
		}
		next, err := p.take(args, at)
		if err != nil {
			return Parsed{}, err
		}
		at = next
	}
	return p.finish()
}

// take reads the flag at args[at], and its value when it has one. It
// returns the index of the last argument it used.
func (p *parser) take(args []string, at int) (int, error) {
	arg := args[at]
	switch arg {
	case "--uncommitted":
		p.cfg.Target = Target{Kind: Uncommitted}
		return at, nil
	case "--staged":
		p.cfg.Target = Target{Kind: Staged}
		return at, nil
	case "--no-synthesis":
		p.cfg.Synthesize = false
		return at, nil
	case "--tree":
		// The path is optional, so a following flag is left for the next
		// turn of the loop and not taken as the path.
		if at+1 < len(args) && !strings.HasPrefix(args[at+1], "-") {
			p.cfg.Target = Target{Kind: Tree, Path: args[at+1]}
			return at + 1, nil
		}
		p.cfg.Target = Target{Kind: Tree}
		return at, nil
	}
	name, value, inline := strings.Cut(arg, "=")
	if !valued(name) {
		return at, fmt.Errorf("%w: %s", errUnknownArg, arg)
	}
	used := at
	if !inline && at+1 < len(args) {
		value, used = args[at+1], at+1
	}
	if value == "" {
		return at, fmt.Errorf("%s expects a value", name)
	}
	return used, p.set(name, value)
}

func valued(name string) bool {
	switch name {
	case "--base", "--tree", "--panelist", "--focus", "--timeout", "--log-dir", "--synth":
		return true
	}
	return false
}

// set records one flag's value. A value is taken as given, even one that
// starts with a dash: `--base --output=x` reaches the target check, which
// names the cause, and is not misread as a missing value.
func (p *parser) set(name, value string) error {
	switch name {
	case "--base":
		p.cfg.Target = Target{Kind: Base, Ref: value}
	case "--tree":
		p.cfg.Target = Target{Kind: Tree, Path: value}
	case "--panelist":
		spec, err := ParseSpec(value, "panelist")
		if err != nil {
			return err
		}
		p.cfg.Panelists = append(p.cfg.Panelists, spec)
	case "--focus":
		p.cfg.Focus = value
	case "--timeout":
		p.timeoutRaw = value
	case "--log-dir":
		p.cfg.LogDir = value
	case "--synth":
		p.synthRaw, p.synthChosen = value, true
	}
	return nil
}

// finish checks the values that have defaults, after every flag is read, so
// an environment variable with a bad value is refused the same way a flag
// is.
func (p *parser) finish() (Parsed, error) {
	timeout, err := requireInt("--timeout", p.timeoutRaw, 1, maxTimeout)
	if err != nil {
		return Parsed{}, err
	}
	// The synthesizer is a panelist spec too, so "claude:opus-4.8" works and
	// an unknown backend is refused the same way.
	synth, err := ParseSpec(p.synthRaw, "--synth")
	if err != nil {
		return Parsed{}, err
	}
	cfg := p.cfg
	cfg.TimeoutSecs = timeout
	cfg.Synth, cfg.synthChosen = synth, p.synthChosen
	return Parsed{Config: cfg}, nil
}

// requireInt reads a whole number in [lo, hi]. A value too large to parse
// at all is still too large: it must not be reported as below the minimum.
func requireInt(flag, value string, lo, hi int64) (int64, error) {
	digits := value != "" && strings.Trim(value, "0123456789") == ""
	n, err := strconv.ParseInt(value, 10, 64)
	switch {
	case digits && (err != nil || n > hi):
		return 0, fmt.Errorf("%s expects an integer <= %d, got %q", flag, hi, value)
	case !digits || n < lo:
		return 0, fmt.Errorf("%s expects an integer >= %d, got %q", flag, lo, value)
	}
	return n, nil
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
