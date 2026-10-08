package stats

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/venabots/autoreview/internal/ledger/serde"
	"github.com/venabots/autoreview/internal/sanitize"
)

// reportJSON is the report as indented JSON. The keys, their order and the
// spelling of every number are v1's, because a script may read either
// version's output. No name is cut or changed: the reader is a program, and
// it wants the names as the ledger has them.
func reportJSON(r report, generatedAt int64) (string, error) {
	var o serde.Object
	o.Int("generated_at", generatedAt)
	optionalInt(&o, "since", r.since)
	o.String("ledger", r.ledger)
	o.Raw("overview", overviewJSON(r.overview))
	o.Raw("attention", serde.Strings(r.attention))
	o.Raw("unresolved", tallyJSON(r.unresolved))
	cohorts := make([][]byte, len(r.cohorts))
	for i, c := range r.cohorts {
		cohorts[i] = cohortJSON(c)
	}
	o.Raw("cohorts", serde.Array(cohorts))

	var out bytes.Buffer
	if err := json.Indent(&out, terminalSafe(o.Bytes()), "", "  "); err != nil {
		return "", err
	}
	return out.String(), nil
}

// terminalSafe writes every character a terminal would obey as its JSON
// escape. JSON already escapes the C0 controls, but not DEL, the C1
// controls, or the bidi and zero-width marks, and `--json` is as likely to
// be read on a terminal as by a program. An escape keeps the value: a
// parser gives back the same name v1's output gives. Outside a string JSON
// is ASCII, so a character rewritten here is always inside one.
func terminalSafe(doc []byte) []byte {
	out := make([]byte, 0, len(doc))
	for _, r := range string(doc) {
		if obeyed(r) {
			out = appendEscape(out, r)
			continue
		}
		out = utf8.AppendRune(out, r)
	}
	return out
}

// obeyed reports a character that sanitize.Line would drop.
func obeyed(r rune) bool {
	return (r >= utf8.RuneSelf || r == 0x7F) && sanitize.Line(string(r)) == ""
}

func appendEscape(dst []byte, r rune) []byte {
	if high, low := utf16.EncodeRune(r); high != utf8.RuneError {
		return fmt.Appendf(dst, `\u%04x\u%04x`, high, low)
	}
	return fmt.Appendf(dst, `\u%04x`, r)
}

func overviewJSON(v overview) []byte {
	var o serde.Object
	o.Int("runs", int64(v.runs))
	o.Int("repos", int64(v.repos))
	optionalInt(&o, "first_at", v.firstAt)
	optionalInt(&o, "last_at", v.lastAt)
	o.Raw("decisions", tallyJSON(v.decisions))
	o.Raw("risks", tallyJSON(v.risks))
	o.Raw("sources", tallyJSON(v.sources))
	return o.Bytes()
}

// tallyJSON writes a tally with its names in byte order, as v1's sorted map
// does. A Go map has no order of its own.
func tallyJSON(tally map[string]int) []byte {
	var o serde.Object
	for _, name := range slices.Sorted(maps.Keys(tally)) {
		o.Int(name, int64(tally[name]))
	}
	return o.Bytes()
}

func cohortJSON(c cohort) []byte {
	var k serde.Object
	k.String("backend", c.key.backend)
	k.String("model", c.key.model)

	var o serde.Object
	o.Raw("key", k.Bytes())
	o.Uint("runs", c.runs)
	o.Uint("answered", c.answered)
	o.Uint("failed", c.failed)
	o.Uint("raw_findings", c.rawFindings)
	o.Uint("raw_runs", c.rawRuns)
	o.Uint("kept", c.kept)
	o.Uint("kept_unique", c.keptUnique)
	o.Uint("kept_high", c.keptHigh)
	o.Uint("dropped", c.dropped)
	o.Uint("keep_num", c.keepNum)
	o.Uint("keep_den", c.keepDen)
	o.Int("first_at", c.firstAt)
	o.Int("last_at", c.lastAt)
	o.Raw("availability", ratioJSON(c.availability()))
	o.Raw("keep_rate", ratioJSON(c.keepRate()))
	if perRun, ok := c.rawPerRun(); ok {
		o.Float("raw_per_run", perRun)
	} else {
		o.Null("raw_per_run")
	}
	o.String("sample", c.sample())
	if secs, ok := c.medianSecs(); ok {
		o.Uint("median_secs", secs)
	} else {
		o.Null("median_secs")
	}
	return o.Bytes()
}

func ratioJSON(r ratio) []byte {
	var o serde.Object
	o.Uint("numerator", r.numerator)
	o.Uint("denominator", r.denominator)
	if !r.known {
		o.Null("value")
		o.Null("low")
		o.Null("high")
		return o.Bytes()
	}
	o.Float("value", r.value)
	o.Float("low", r.low)
	o.Float("high", r.high)
	return o.Bytes()
}

// optionalInt writes a number, or null when there is none. v1 writes the
// key either way, and a reader may count on it.
func optionalInt(o *serde.Object, name string, value *int64) {
	if value == nil {
		o.Null(name)
		return
	}
	o.Int(name, *value)
}
