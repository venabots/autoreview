package ledger

import (
	"strconv"
	"time"
)

// sourcePanel is the `source` of a record that `autoreview panel` wrote. v1
// writes the same word, and the statistics count a panel run by it.
const sourcePanel = "panel"

// Panel is what one finished panel run contributes to the ledger. There is
// no PR and no verdict: the target is whatever was reviewed, named by the
// repository.
type Panel struct {
	// Repo is "owner/name" from the remote, or the directory's name.
	Repo string
	// PID is the process that ran the panel. With the start it keys the
	// record, as v1's does.
	PID        int
	StartedAt  time.Time
	FinishedAt time.Time
	Panel      []Panelled
	// Risk is the synthesis's risk, or "" for none.
	Risk string
	// SynthModel is the model the synthesis was pinned to, or "" when it
	// ran on its backend's default.
	SynthModel string
	// Findings and Reviewed are what the findings package read from the
	// synthesis. This package cannot call it: it uses this one's types.
	Findings []Finding
	Reviewed bool
}

// Panelled is one panelist as the run saw it. Only the model is the
// panelist's own claim; the rest the run watched happen.
type Panelled struct {
	Name string
	// Model is what the panelist reported, or "unknown" or "" for none.
	Model    string
	Answered bool
	// Findings and Top are the panelist's own count and worst tag, nil and
	// "" when it did not answer or its report had neither.
	Findings *uint64
	Top      string
	Elapsed  time.Duration
	// Exit is the exit code, or nil for a panelist that left none.
	Exit *int
}

// ForPanel is the record of one panel run, as v1 writes it: the same id, the
// same fields present and absent. v1 still appends to the ledger, and a
// record that differed would change what the statistics mean.
func ForPanel(p Panel) Record {
	return Record{
		V:            Version,
		ID:           sourcePanel + ":" + strconv.FormatInt(p.StartedAt.Unix(), 10) + ":" + strconv.Itoa(p.PID),
		At:           p.FinishedAt.Unix(),
		Source:       sourcePanel,
		Repo:         &p.Repo,
		Risk:         given(p.Risk),
		DurationSecs: unsigned(p.FinishedAt.Unix() - p.StartedAt.Unix()),
		DriverModel:  given(p.SynthModel),
		Panel:        panelledOf(p.Panel),
		Findings:     p.Findings,
		Reviewed:     p.Reviewed,
	}
}

func panelledOf(panel []Panelled) []PanelEntry {
	out := make([]PanelEntry, len(panel))
	for i, p := range panel {
		out[i] = PanelEntry{
			Name:         p.Name,
			Model:        reportedModel(p.Model),
			OK:           &p.Answered,
			Findings:     p.Findings,
			Top:          given(p.Top),
			DurationSecs: unsigned(p.Elapsed / time.Second),
			ExitCode:     exitCode(p.Exit),
		}
	}
	return out
}

// reportedModel is nil for a panelist that named no model. "unknown" is
// what the run writes in its own report for that, and it is not a model.
func reportedModel(model string) *string {
	if model == "unknown" {
		return nil
	}
	return given(model)
}

func exitCode(code *int) *int32 {
	if code == nil {
		return nil
	}
	c := int32(*code)
	return &c
}
