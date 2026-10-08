package ledger

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// UnmarshalJSON reads a record by v1's rules. `v`, `id`, `at` and `source`
// must be there. `panel` and `findings` may be missing but not null.
// `reviewed` is true when it is missing. encoding/json alone is more
// forgiving than v1 (a missing field is a zero, a null is ignored, `ID`
// matches `id`), and a record v2 accepted that v1 skips would make the two
// versions report different numbers from one file.
func (r *Record) UnmarshalJSON(data []byte) error {
	f, err := readFields(data)
	if err != nil {
		return err
	}
	out := Record{Reviewed: true}
	err = errors.Join(
		f.required("v", &out.V),
		f.required("id", &out.ID),
		f.required("at", &out.At),
		f.required("source", &out.Source),
		f.optional("repo", &out.Repo),
		f.optional("pr", &out.PR),
		f.optional("session", &out.Session),
		f.optional("decision", &out.Decision),
		f.optional("risk", &out.Risk),
		f.optional("counts", &out.Counts),
		f.optional("cost_usd", &out.CostUSD),
		f.optional("duration_secs", &out.DurationSecs),
		f.optional("driver_model", &out.DriverModel),
		f.defaulted("panel", &out.Panel),
		f.defaulted("findings", &out.Findings),
		f.defaulted("reviewed", &out.Reviewed),
	)
	if err != nil {
		return err
	}
	*r = out
	return nil
}

// UnmarshalJSON reads the counts. Each one may be missing.
func (c *Counts) UnmarshalJSON(data []byte) error {
	f, err := readFields(data)
	if err != nil {
		return err
	}
	var out Counts
	err = errors.Join(
		f.optional("must_fix", &out.MustFix),
		f.optional("should_fix", &out.ShouldFix),
		f.optional("polish", &out.Polish),
	)
	if err != nil {
		return err
	}
	*c = out
	return nil
}

// UnmarshalJSON reads a panelist. Only the name must be there.
func (p *PanelEntry) UnmarshalJSON(data []byte) error {
	f, err := readFields(data)
	if err != nil {
		return err
	}
	var out PanelEntry
	err = errors.Join(
		f.required("name", &out.Name),
		f.optional("model", &out.Model),
		f.optional("ok", &out.OK),
		f.optional("findings", &out.Findings),
		f.optional("top", &out.Top),
		f.optional("duration_secs", &out.DurationSecs),
		f.optional("exit_code", &out.ExitCode),
	)
	if err != nil {
		return err
	}
	*p = out
	return nil
}

// UnmarshalJSON reads a finding. Only the count must be there.
func (fd *Finding) UnmarshalJSON(data []byte) error {
	f, err := readFields(data)
	if err != nil {
		return err
	}
	var out Finding
	err = errors.Join(
		f.optional("severity", &out.Severity),
		f.optional("location", &out.Location),
		f.defaulted("flagged_by", &out.FlaggedBy),
		f.required("count", &out.Count),
		f.defaulted("dropped", &out.Dropped),
	)
	if err != nil {
		return err
	}
	*fd = out
	return nil
}

// UnmarshalJSON reads a source. Only the name must be there.
func (s *Source) UnmarshalJSON(data []byte) error {
	f, err := readFields(data)
	if err != nil {
		return err
	}
	var out Source
	err = errors.Join(
		f.required("name", &out.Name),
		f.optional("model", &out.Model),
	)
	if err != nil {
		return err
	}
	*s = out
	return nil
}

// fields is one JSON object, split into its fields by their exact names.
type fields struct {
	raw map[string]json.RawMessage
	// twice holds the names that came more than once. v1 refuses a record
	// that repeats a field it reads, where encoding/json keeps the last.
	twice map[string]bool
}

// readFields splits an object. Anything that is not an object is refused:
// encoding/json reads `null` into a struct as no change at all, which would
// let `"panel":[null]` through as a panelist with no name.
func readFields(data []byte) (fields, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	if first, err := dec.Token(); err != nil || first != json.Delim('{') {
		return fields{}, errors.New("not a JSON object")
	}
	f := fields{raw: map[string]json.RawMessage{}, twice: map[string]bool{}}
	for dec.More() {
		token, err := dec.Token()
		if err != nil {
			return fields{}, err
		}
		name, _ := token.(string)
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return fields{}, err
		}
		if _, seen := f.raw[name]; seen {
			f.twice[name] = true
		}
		f.raw[name] = value
	}
	return f, nil
}

// required reads a field v1 will not read a record without.
func (f fields) required(name string, into any) error {
	if _, ok := f.raw[name]; !ok {
		return fmt.Errorf("missing field %q", name)
	}
	return f.defaulted(name, into)
}

// defaulted reads a field that may be missing but, when it is there, must
// hold a value. encoding/json would take a null as "leave the default".
func (f fields) defaulted(name string, into any) error {
	raw, ok := f.raw[name]
	if ok && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return fmt.Errorf("field %q is null", name)
	}
	return f.optional(name, into)
}

// optional reads a field that may be missing or null. into is a pointer to
// the pointer field, so both leave it nil.
func (f fields) optional(name string, into any) error {
	raw, ok := f.raw[name]
	if !ok {
		return nil
	}
	if f.twice[name] {
		return fmt.Errorf("duplicate field %q", name)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("field %q: %w", name, err)
	}
	return nil
}
