package doctor

import (
	"errors"
	"fmt"
	"strings"

	"github.com/venabots/autoreview/internal/stats"
)

// defaultSince is the window when nobody names one: a day is long enough
// to see a model that stopped answering, and short enough that a fixed one
// reads as fixed.
const defaultSince = "1d"

// options is what one `autoreview doctor` was asked for.
type options struct {
	since   int64
	window  string
	noProbe bool
	ledger  string
	help    bool
}

// errUnknownArg marks an argument that is no flag at all. The caller
// prints the help after it.
var errUnknownArg = errors.New("unknown arg")

// parse reads the arguments after the word "doctor". It reads them as stats
// does, so `--since --no-probe` is a forgotten value and not a window.
func parse(args []string, now int64) (options, error) {
	opts := options{window: defaultSince}
	rest := args
	for len(rest) > 0 {
		arg := rest[0]
		rest = rest[1:]
		switch arg {
		case "--help", "-h":
			return options{help: true}, nil
		case "--no-probe":
			opts.noProbe = true
			continue
		}
		name, value, inline := strings.Cut(arg, "=")
		if name != "--since" && name != "--ledger" {
			return options{}, fmt.Errorf("%w: %s", errUnknownArg, arg)
		}
		if !inline && len(rest) > 0 {
			value, rest = rest[0], rest[1:]
		}
		if strings.HasPrefix(value, "--") || value == "" {
			return options{}, fmt.Errorf("%s expects a value", name)
		}
		if name == "--ledger" {
			opts.ledger = value
			continue
		}
		opts.window = value
	}
	since, err := stats.ParseSince(opts.window, now)
	opts.since = since
	return opts, err
}
