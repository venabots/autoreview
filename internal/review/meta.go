package review

import (
	"encoding/json"
	"os"

	"github.com/venabots/autoreview/internal/session"
)

// Meta is the part of dash-p's meta file that a summary needs.
type Meta struct {
	// SessionID is the session the review ran in, as the harness reported
	// it. It is other people's text until SummarySession has checked it.
	SessionID string
	// CostUSD is dash-p's own accounting, which is claude's. Only the
	// built-in reviewer under claude reports one.
	CostUSD float64
	// Model is the model the harness ran, for example "claude-fable-5".
	Model string
}

// metaFile is the meta file as dash-p writes it.
type metaFile struct {
	SessionID    string  `json:"session_id"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	// ModelResolved is the model the harness ran, which can differ from the
	// model that was asked for.
	ModelResolved string `json:"model_resolved"`
}

// ReadMeta reads the meta file of one review. It is false when there is no
// file or the file is not the envelope: a review that was killed before
// dash-p wrote it has no cost and no session to report.
//
// The model name is written by another program and is drawn on the
// terminal, so it is cleaned and cut here, where every reader gets the same
// safe value.
func ReadMeta(path string) (Meta, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Meta{}, false
	}
	var file metaFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return Meta{}, false
	}
	return Meta{
		SessionID: file.SessionID,
		CostUSD:   file.TotalCostUSD,
		Model:     field(file.ModelResolved),
	}, true
}

// SummarySession is the session a summary shows for a finished review, and
// the id that is recorded for the next pass. It is false when there is no
// session to promise.
//
//   - An override handles its own sessions, so there is nothing to show.
//   - A UUID-shaped id in the meta file names the session the review ran
//     in. A review that sent no flag cannot learn its id any other way. An
//     id of any other shape is not used: it would go onto a command line.
//   - Without one, a review that sent a flag keeps the id it planned.
//   - No flag and no id from the meta file: nothing.
//
// plan must be the plan the review started with. A backend that takes no
// session starts with the zero Plan, and a planned id that was never sent
// must not be shown.
func SummarySession(plan session.Plan, meta *Meta, override bool) (string, bool) {
	switch {
	case override:
		return "", false
	case meta != nil && session.UUIDShaped(meta.SessionID):
		return meta.SessionID, true
	case plan.Flag == session.FlagNone:
		return "", false
	}
	if !session.UUIDShaped(plan.ID) {
		return "", false
	}
	return plan.ID, true
}
