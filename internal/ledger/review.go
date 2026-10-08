package ledger

import (
	"cmp"
	"strconv"
	"strings"
	"time"

	"github.com/venabots/autoreview/internal/review"
)

// sourceReview is the `source` of a record that a review pass wrote. v1
// writes the same word, and the statistics tell a review pass from a panel
// run by it.
const sourceReview = "autoreview"

// noName is what v1 writes for a panelist whose trailer entry has no name.
// A record must give every panelist a name, or v1 refuses the whole line.
const noName = "?"

// Review is what one finished review contributes to the ledger.
//
// A text field is "" when the review has no such thing, and the record then
// has no such field. Only a review that reported a trailer is recorded: a
// review with no panel says nothing about the models.
type Review struct {
	// Repo is the repository as "owner/name".
	Repo string
	PR   int
	// Session is the session the review ran in, or "" when it has none.
	Session   string
	StartedAt time.Time
	// FinishedAt is the record's `at`.
	FinishedAt time.Time
	// Verdict is the resolved verdict in the words a summary shows:
	// "approved", "changes requested", "commented", or "" when nothing was
	// posted. It is GitHub's answer first and the trailer's own claim second,
	// so the record never says a review landed because the reviewer said so.
	Verdict string
	Trailer review.Trailer
	// Cost is nil when the harness reported none. v1 records no cost of
	// zero: a harness that does not count writes a zero.
	Cost    *float64
	Elapsed time.Duration
	// Model is the model that drove the review, or "" when it is unknown.
	Model string
	// Findings is what findings.Parse read from the review text, and nothing
	// when there was no text. The caller reads it: the findings package uses
	// the types of this one, so this one cannot call it.
	Findings []Finding
	// Reviewed says the review text was there to read findings from, and
	// was a synthesis: findings.IsSynthesis answers that. A sign-off is a
	// text too. If it counted, a review whose findings were never read would
	// read as a review that kept none of them, and sink the keep rate.
	Reviewed bool
}

// ForReview is the record of one review of one PR, as v1 writes it: the same
// id, the same decision word, and the same fields present and absent. v1
// still appends to the ledger, and the statistics fold both versions' lines
// into one score. A record that differed would change what the score means.
//
// The id is the same for the same review, so a review recorded twice reads
// once. It names the session when there is one and the PR when there is
// none, with the second the review started.
func ForReview(r Review) Record {
	return Record{
		V:            Version,
		ID:           reviewID(r),
		At:           r.FinishedAt.Unix(),
		Source:       sourceReview,
		Repo:         &r.Repo,
		PR:           unsigned(r.PR),
		Session:      given(r.Session),
		Decision:     decisionOf(r.Verdict),
		Risk:         given(r.Trailer.Risk),
		Counts:       countsOf(r.Trailer.Findings),
		CostUSD:      r.Cost,
		DurationSecs: unsigned(r.Elapsed / time.Second),
		DriverModel:  given(r.Model),
		Panel:        panelOf(r.Trailer.Panel),
		Findings:     r.Findings,
		Reviewed:     r.Reviewed,
	}
}

func reviewID(r Review) string {
	key := r.Session
	if key == "" {
		key = r.Repo + "#" + strconv.FormatUint(*unsigned(r.PR), 10)
	}
	return sourceReview + ":" + key + ":" + strconv.FormatInt(r.StartedAt.Unix(), 10)
}

// decisionOf is the record's decision, or nil when nothing was posted.
func decisionOf(verdict string) *string {
	if verdict == "" {
		return nil
	}
	token := decisionToken(verdict)
	return &token
}

// decisionToken gives each decision one spelling. A summary says "changes
// requested". The trailer and the ledger say `changes-requested`, and the
// statistics count by that word.
func decisionToken(verdict string) string {
	// Only A to Z are lowered, as in v1. strings.ToLower lowers more.
	lowered := strings.Map(func(r rune) rune {
		if 'A' <= r && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}, strings.TrimSpace(verdict))
	return strings.ReplaceAll(lowered, " ", "-")
}

// countsOf is the counts the trailer claimed, or nil when it claimed none. A
// count the reviewer did not report stays missing: it is not a zero.
func countsOf(f *review.Findings) *Counts {
	if f == nil {
		return nil
	}
	return &Counts{MustFix: wide(f.MustFix), ShouldFix: wide(f.ShouldFix), Polish: wide(f.Polish)}
}

// panelOf is the panel as the trailer reported it. The run did not time the
// panelists or see them exit, so the record has no duration and no exit
// code for them.
func panelOf(panel []review.Panelist) []PanelEntry {
	out := make([]PanelEntry, len(panel))
	for i, p := range panel {
		out[i] = PanelEntry{
			Name:     cmp.Or(p.Name, noName),
			Model:    given(p.Model),
			OK:       p.OK,
			Findings: wide(p.Findings),
			Top:      given(p.Top),
		}
	}
	return out
}

// given is nil for a text the review does not have, so the record leaves
// the field out. A pointer to "" would write an empty string for every field
// nobody reported.
//
// One case is not v1's. review.Trailer holds a field the reviewer left out
// and a field the reviewer gave as "" the same way, so both are left out
// here. v1 writes the second as an empty string.
func given(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func wide(n *uint) *uint64 {
	if n == nil {
		return nil
	}
	w := uint64(*n)
	return &w
}

// unsigned is n as the record holds a PR number and a duration. A negative
// one is written as zero. Converted as it is, it would wrap to a number of
// twenty digits.
func unsigned[T ~int | ~int64](n T) *uint64 {
	u := uint64(max(n, 0))
	return &u
}
