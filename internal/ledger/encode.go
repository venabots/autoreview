package ledger

import "github.com/venabots/autoreview/internal/ledger/serde"

// MarshalJSON writes the record with the bytes v1 writes: v1's field order,
// its float spelling and its string escapes. A missing field is left out and
// a list is always an array.
//
// Call it directly when the bytes matter, as Append does. encoding/json runs
// the result through its own escaper: json.Marshal rewrites `<`, `>` and
// `&`, and even an encoder with SetEscapeHTML(false) rewrites U+2028 and
// U+2029. The value is the same and the bytes are not.
func (r Record) MarshalJSON() ([]byte, error) {
	var o serde.Object
	o.Uint("v", uint64(r.V))
	o.String("id", r.ID)
	o.Int("at", r.At)
	o.String("source", r.Source)
	optional("repo", r.Repo, o.String)
	optional("pr", r.PR, o.Uint)
	optional("session", r.Session, o.String)
	optional("decision", r.Decision, o.String)
	optional("risk", r.Risk, o.String)
	if r.Counts != nil {
		o.Raw("counts", r.Counts.json())
	}
	optional("cost_usd", r.CostUSD, o.Float)
	optional("duration_secs", r.DurationSecs, o.Uint)
	optional("driver_model", r.DriverModel, o.String)
	o.Raw("panel", array(r.Panel, PanelEntry.json))
	o.Raw("findings", array(r.Findings, Finding.json))
	o.Bool("reviewed", r.Reviewed)
	return o.Bytes(), nil
}

// MarshalJSON writes the counts as v1 does. With no count at all it is `{}`.
func (c Counts) MarshalJSON() ([]byte, error) {
	return c.json(), nil
}

// MarshalJSON writes the panelist as v1 does.
func (p PanelEntry) MarshalJSON() ([]byte, error) {
	return p.json(), nil
}

// MarshalJSON writes the finding as v1 does. `dropped` is written only when
// it is true.
func (f Finding) MarshalJSON() ([]byte, error) {
	return f.json(), nil
}

// MarshalJSON writes the source as v1 does.
func (s Source) MarshalJSON() ([]byte, error) {
	return s.json(), nil
}

func (c Counts) json() []byte {
	var o serde.Object
	optional("must_fix", c.MustFix, o.Uint)
	optional("should_fix", c.ShouldFix, o.Uint)
	optional("polish", c.Polish, o.Uint)
	return o.Bytes()
}

func (p PanelEntry) json() []byte {
	var o serde.Object
	o.String("name", p.Name)
	optional("model", p.Model, o.String)
	optional("ok", p.OK, o.Bool)
	optional("findings", p.Findings, o.Uint)
	optional("top", p.Top, o.String)
	optional("duration_secs", p.DurationSecs, o.Uint)
	if p.ExitCode != nil {
		o.Int("exit_code", int64(*p.ExitCode))
	}
	return o.Bytes()
}

func (f Finding) json() []byte {
	var o serde.Object
	optional("severity", f.Severity, o.String)
	optional("location", f.Location, o.String)
	o.Raw("flagged_by", array(f.FlaggedBy, Source.json))
	o.Uint("count", uint64(f.Count))
	if f.Dropped {
		o.Bool("dropped", true)
	}
	return o.Bytes()
}

func (s Source) json() []byte {
	var o serde.Object
	o.String("name", s.Name)
	optional("model", s.Model, o.String)
	return o.Bytes()
}

// optional writes a field only when the record has it. Writing `null` for a
// missing field would change the bytes of every line v1 wrote.
func optional[T any](name string, value *T, put func(string, T)) {
	if value != nil {
		put(name, *value)
	}
}

// array writes a list. A nil slice is `[]`: v1's reader refuses `null` for a
// list and silently drops the whole record.
func array[T any](items []T, encode func(T) []byte) []byte {
	encoded := make([][]byte, len(items))
	for i, item := range items {
		encoded[i] = encode(item)
	}
	return serde.Array(encoded)
}
