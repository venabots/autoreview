package stats

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// options is what one `autoreview stats` was asked for.
type options struct {
	// since is an epoch second. Reviews before it are left out.
	since *int64
	// repo is a part of a repo name. Empty means every repo.
	repo string
	json bool
	// ledger is the file to read. Empty means the usual one.
	ledger string
	help   bool
}

// errUnknownArg marks an argument that is no flag at all. The caller prints
// the help after it, because the user has the wrong idea of the flags.
var errUnknownArg = errors.New("unknown arg")

// parse reads the arguments after the word "stats". The parser is written by
// hand, as v1's is: v1 refuses `--repo --json` as a forgotten value, and a
// flag library would read `--json` as the repo name and report on nothing.
func parse(args []string, now int64) (options, error) {
	var opts options
	rest := args
	for len(rest) > 0 {
		arg := rest[0]
		rest = rest[1:]
		switch arg {
		case "--help", "-h":
			return options{help: true}, nil
		case "--json":
			opts.json = true
			continue
		}
		name, value, inline := strings.Cut(arg, "=")
		if name != "--since" && name != "--repo" && name != "--ledger" {
			return options{}, fmt.Errorf("%w: %s", errUnknownArg, arg)
		}
		if !inline && len(rest) > 0 {
			value, rest = rest[0], rest[1:]
		}
		if err := requireValue(name, value); err != nil {
			return options{}, err
		}
		next, err := opts.with(name, value, now)
		if err != nil {
			return options{}, err
		}
		opts = next
	}
	return opts, nil
}

// requireValue refuses a flag with no value. A value that is itself a flag
// is a forgotten argument: `--repo --json` must not report on a repo named
// "--json". Both spellings are checked, so `--repo=` is refused as
// `--repo ""` is.
func requireValue(flag, value string) error {
	switch {
	case strings.HasPrefix(value, "--"):
		return fmt.Errorf("%s expects a value, but found the flag %s", flag, value)
	case value == "":
		return fmt.Errorf("%s expects a value", flag)
	}
	return nil
}

// with is the options with one more flag set. It returns a new value.
func (o options) with(flag, value string, now int64) (options, error) {
	switch flag {
	case "--since":
		since, err := parseSince(value, now)
		if err != nil {
			return options{}, err
		}
		o.since = &since
	case "--repo":
		o.repo = value
	case "--ledger":
		o.ledger = value
	}
	return o, nil
}

const (
	daySecs  int64 = 86_400
	weekSecs       = 7 * daySecs
)

// parseSince reads `7d`, `2w` or `YYYY-MM-DD` as the epoch second it names.
func parseSince(raw string, now int64) (int64, error) {
	bad := fmt.Errorf("--since expects a span like 7d or 2w, or a date like 2026-08-01, got %q", raw)
	if len(raw) == 10 && isASCII(raw) {
		// time.Parse checks the calendar as well as the shape. A date that
		// does not exist (month 13, or 2026-02-30) is refused; it must not
		// land on another day.
		day, err := time.Parse(time.DateOnly, raw)
		if err != nil {
			return 0, bad
		}
		return day.Unix(), nil
	}
	// The unit is the last character, not the last byte. A multibyte last
	// character must be refused, not split in the middle.
	unit, size := utf8.DecodeLastRuneInString(raw)
	count, err := strconv.ParseInt(raw[:len(raw)-size], 10, 64)
	if err != nil || count <= 0 {
		return 0, bad
	}
	per, ok := map[rune]int64{'d': daySecs, 'w': weekSecs}[unit]
	// A huge span must give the error. It must not overflow into a cutoff
	// in the future that hides every review.
	if !ok || count > math.MaxInt64/per || now < math.MinInt64+count*per {
		return 0, bad
	}
	return now - count*per, nil
}

func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}
