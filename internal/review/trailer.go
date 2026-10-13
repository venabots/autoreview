package review

import (
	"encoding/json"
	"strings"
	"unicode"

	"github.com/venabots/autoreview/internal/sanitize"
)

// Trailer is the reviewer's own report of how the review ended. The system
// prompt asks the agent to end its reply with a fenced `autoreview` block
// that holds one JSON object.
//
// It is read leniently and it is never trusted. Every field is optional,
// an unknown field is ignored, and a block that does not parse reads as no
// block: a reviewer that never writes one costs a "-" in the summary and
// nothing more. Whether a review landed on the PR is GitHub's to say, in
// Verdict.
//
// A text field is "" when the reviewer did not give it. A count and a flag
// are nil then, because an unreported count is not a zero.
type Trailer struct {
	// Decision is approved, commented, changes-requested or none.
	Decision string    `json:"decision"`
	Risk     string    `json:"risk"`
	Findings *Findings `json:"findings"`
	// Panel is every panelist the review started.
	Panel []Panelist `json:"panel"`
	// Blockers is what stands between the PR and an approval, worst first.
	// It is empty on an approving review, and from a reviewer too old to
	// know the field.
	Blockers []Blocker `json:"blockers"`
}

// Findings is how many findings the synthesis kept, by what they ask for.
type Findings struct {
	MustFix   *uint `json:"must_fix"`
	ShouldFix *uint `json:"should_fix"`
	Polish    *uint `json:"polish"`
}

// Panelist is one reviewer on the panel.
type Panelist struct {
	Name string `json:"name"`
	// Model is the model the panelist says it ran on.
	Model string `json:"model"`
	// OK says the panelist returned a verdict.
	OK       *bool  `json:"ok"`
	Findings *uint  `json:"findings"`
	Top      string `json:"top"`
}

// Blocker is one reason a review did not approve. Domain and Reversible are
// the two facts that tell a misaligned button from a double charge, and a
// reader who triages a pass needs both before the PR is open.
type Blocker struct {
	Severity string `json:"severity"`
	// Domain is money, data, security, correctness, ui, perf or docs.
	Domain string `json:"domain"`
	// Reversible is false when the damage cannot be undone after it lands:
	// money moved, data lost. It is nil when the reviewer did not say.
	Reversible *bool  `json:"reversible"`
	Location   string `json:"location"`
	Gist       string `json:"gist"`
}

// The limits on what a trailer can put on the terminal. The trailer is
// agent output, and no field of it may flood a summary or scroll it away.
const (
	// maxFieldChars is the longest any one reported string can be.
	maxFieldChars = 80
	// maxGistChars gives a blocker's gist more room: it is a sentence and
	// not a label.
	maxGistChars = 120
	maxPanelists = 16
	// maxBlockers is enough to explain any review a person would read.
	maxBlockers = 8
)

const (
	fence      = "```"
	trailerTag = fence + "autoreview"
)

// ParseTrailer is the trailer in a reply, or false when the reply has none.
//
// The last block that parses wins, and the search goes backwards from the
// end. A review that quotes an earlier block reports the newer state, and
// prose that only names the fence tag after the real block must not hide
// it.
//
// Every string in the result is safe to draw: the control bytes that could
// repaint the terminal are gone, each string is cut, and the two lists are
// cut too.
func ParseTrailer(text string) (Trailer, bool) {
	found, ok := lastBlock(text)
	if !ok {
		return Trailer{}, false
	}
	return cleaned(found.trailer), true
}

// StripTrailer is the reply without its trailer blocks.
//
// A block must parse to be removed, so a review can name the fence tag in
// prose and keep everything under it.
//
// Every block goes, wherever it is. A review file is every message joined:
// the review, then the block, then whatever the reviewer said last, and a
// resumed session holds one block for each pass. To remove only a block at
// the end would leave raw JSON in the middle of the file. What follows a
// block is kept.
func StripTrailer(text string) string {
	for {
		found, ok := lastBlock(text)
		if !ok {
			return text
		}
		before := strings.TrimRightFunc(text[:found.start], unicode.IsSpace)
		after := strings.TrimSpace(text[found.end:])
		if after == "" {
			text = before
		} else {
			text = before + "\n\n" + after
		}
	}
}

// block is one trailer block inside a text: text[start:end] is the block
// with both fences.
type block struct {
	start, end int
	trailer    Trailer
}

// lastBlock finds the last block in text that parses.
func lastBlock(text string) (block, bool) {
	searchEnd := len(text)
	for {
		start := strings.LastIndex(text[:searchEnd], trailerTag)
		if start < 0 {
			return block{}, false
		}
		bodyStart := start + len(trailerTag)
		if length := strings.Index(text[bodyStart:], fence); length >= 0 {
			bodyEnd := bodyStart + length
			if trailer, ok := decodeTrailer(text[bodyStart:bodyEnd]); ok {
				return block{start: start, end: bodyEnd + len(fence), trailer: trailer}, true
			}
		}
		searchEnd = start
	}
}

// decodeTrailer reads the JSON between the fences. It must be one object.
// The decoder accepts a bare `null` for a struct and changes nothing, which
// would read as a trailer that says nothing where there is no trailer.
func decodeTrailer(body string) (Trailer, bool) {
	body = strings.TrimSpace(body)
	if !strings.HasPrefix(body, "{") {
		return Trailer{}, false
	}
	var trailer Trailer
	if err := json.Unmarshal([]byte(body), &trailer); err != nil {
		return Trailer{}, false
	}
	return trailer, true
}

// cleaned is the trailer with every string made safe to draw and both
// lists cut. It is done once, here, so no code that draws a trailer has to
// remember to.
func cleaned(t Trailer) Trailer {
	return Trailer{
		Decision: field(t.Decision),
		Risk:     field(t.Risk),
		Findings: t.Findings,
		Panel:    cleanedPanel(t.Panel),
		Blockers: cleanedBlockers(t.Blockers),
	}
}

func cleanedPanel(panel []Panelist) []Panelist {
	if len(panel) == 0 {
		return nil
	}
	kept := panel[:min(len(panel), maxPanelists)]
	out := make([]Panelist, len(kept))
	for i, p := range kept {
		out[i] = Panelist{
			Name:     field(p.Name),
			Model:    field(p.Model),
			OK:       p.OK,
			Findings: p.Findings,
			Top:      field(p.Top),
		}
	}
	return out
}

func cleanedBlockers(blockers []Blocker) []Blocker {
	if len(blockers) == 0 {
		return nil
	}
	kept := blockers[:min(len(blockers), maxBlockers)]
	out := make([]Blocker, len(kept))
	for i, b := range kept {
		out[i] = Blocker{
			Severity:   field(b.Severity),
			Domain:     field(b.Domain),
			Reversible: b.Reversible,
			Location:   field(b.Location),
			Gist:       capped(sanitize.Line(b.Gist), maxGistChars),
		}
	}
	return out
}

// field makes one reported string safe to draw: one line, and no longer
// than a column can hold.
func field(s string) string {
	return capped(sanitize.Line(s), maxFieldChars)
}

// capped is s cut to at most limit characters. It cuts between characters,
// so a cut never leaves half of one.
func capped(s string, limit int) string {
	count := 0
	for i := range s {
		if count == limit {
			return s[:i]
		}
		count++
	}
	return s
}
