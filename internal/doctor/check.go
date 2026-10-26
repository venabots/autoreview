package doctor

import (
	"context"

	"github.com/venabots/autoreview/internal/cli"
	gate "github.com/venabots/autoreview/internal/review"
	"github.com/venabots/autoreview/internal/sanitize"
)

// Summary is what a doctor found, without the report: for the view, which
// shows it in its footer so nobody has to run the doctor to learn the
// panel is down.
type Summary struct {
	// Seated is the panelists a review would seat, and Answering how many
	// of them answered the probe.
	Seated, Answering int
	// Problems is each thing that needs a person, as the report says them.
	Problems []string
	// Blocking says reviews cannot approve or cannot start: fewer
	// panelists answer than the approval gate needs, or a program a run
	// needs is missing.
	Blocking bool
}

// Diagnose is what `autoreview doctor` with no arguments would find: the panel
// asked now, and the last day of the ledger.
func Diagnose(ctx context.Context, d Deps) Summary {
	seated, err := panelOf(d.Env(cli.EnvPanelists), d.Panelists, d.Has)
	if err != nil {
		return Summary{Problems: []string{sanitize.Line(err.Error())}, Blocking: true}
	}
	opts, err := parse(nil, d.Now.Unix())
	if err != nil {
		return Summary{Problems: []string{sanitize.Line(err.Error())}, Blocking: true}
	}
	r := report{now: d.Now, seated: seated, probed: true, window: opts.window, tools: toolsOf(d.Version)}
	r = withLedger(r, opts, d.Env)
	r.checks = d.Probe(ctx, seated.Specs)
	answering := 0
	for _, c := range r.checks {
		if c.OK {
			answering++
		}
	}
	launched := len(seated.Specs)
	return Summary{
		Seated:    launched,
		Answering: answering,
		Problems:  r.problems(),
		Blocking:  answering < gate.Quorum(launched) || missingRequired(r.tools),
	}
}

func missingRequired(tools []Tool) bool {
	for _, t := range tools {
		if t.Required && !t.Found {
			return true
		}
	}
	return false
}
