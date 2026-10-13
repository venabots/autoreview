// Package ledger is the record of every finished review: one JSON line for
// each, kept across runs.
//
// What each model found, and whether it held up, is only worth anything as a
// series. So it goes somewhere that outlives the run:
// `$XDG_STATE_HOME/autoreview/ledger.jsonl`, or
// `~/.local/state/autoreview/ledger.jsonl`. `$AUTOREVIEW_LEDGER` names a
// different file, and `off` records nothing.
//
// The file is v1's, and v1 still appends to it. A line this package writes
// has the bytes v1 would write for the same record, and a line v1 would
// refuse is refused here too, so the two versions never disagree about what
// the history holds.
package ledger

// Version is the ledger format this build writes. A record with a higher
// number may mean something this build does not know, so Read leaves it out.
const Version = 1

// Record is one finished review: what drove it, who sat on the panel and
// what survived. A nil pointer is a field the record does not have. That is
// not the same as a zero: `"must_fix":0` is a count somebody reported.
type Record struct {
	V uint32
	// ID is stable across imports, so a review recorded twice reads once.
	ID string
	// At is when the review finished, as an epoch second.
	At int64
	// Source is `autoreview`, `panel`, or `transcript` for a review an
	// older version imported from session transcripts.
	Source  string
	Repo    *string
	PR      *uint64
	Session *string
	// Decision is what landed on the PR: approved, commented,
	// changes-requested or none.
	Decision     *string
	Risk         *string
	Counts       *Counts
	CostUSD      *float64
	DurationSecs *uint64
	// DriverModel is the model that ran the review session and wrote the
	// synthesis.
	DriverModel *string
	Panel       []PanelEntry
	Findings    []Finding
	// Reviewed is true when the synthesized review text was there to read
	// findings from. False means the findings list is empty because nothing
	// was read, not because nothing was kept, and the keep rate must not
	// count it. A record from before the field existed reads as true.
	Reviewed bool
}

// Counts is how many findings the review's own trailer claimed, by bucket.
type Counts struct {
	MustFix   *uint64
	ShouldFix *uint64
	Polish    *uint64
}

// PanelEntry is one panelist as launched: the name the run gave it, the
// model it reported, and whether it came back with a review.
type PanelEntry struct {
	Name  string
	Model *string
	OK    *bool
	// Findings is the panelist's own count, before synthesis.
	Findings     *uint64
	Top          *string
	DurationSecs *uint64
	ExitCode     *int32
}

// Finding is one finding the synthesis surfaced, cut down to what the
// statistics need.
type Finding struct {
	Severity  *string
	Location  *string
	FlaggedBy []Source
	// Count is how many panelists raised it. It is at least the length of
	// FlaggedBy, and larger when the synthesis wrote a number with no names.
	Count uint32
	// Dropped marks a finding listed under Disagreements: the synthesis
	// surfaced it to say it did not hold up, so it counts against its
	// sources.
	Dropped bool
}

// Source is one panelist named in a `Flagged by` clause, as the synthesis
// wrote it: `codex (gpt-5.5)` gives both parts, a bare `claude-opus` gives
// only the name.
type Source struct {
	Name  string
	Model *string
}
