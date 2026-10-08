package ledger

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func ptr[T any](v T) *T {
	return &v
}

// roundTrip reads one line and writes it back.
func roundTrip(t *testing.T, line []byte) []byte {
	t.Helper()
	var record Record
	if err := json.Unmarshal(line, &record); err != nil {
		t.Fatalf("the line did not parse: %v", err)
	}
	out, err := record.MarshalJSON()
	if err != nil {
		t.Fatalf("the record did not marshal: %v", err)
	}
	return out
}

// fixtureLines is testdata/v1.jsonl: invented records in every shape the
// owner's ledger holds, written in v1's bytes. v1 0.23.0 reads all of them.
func fixtureLines(t *testing.T) [][]byte {
	t.Helper()
	file, err := os.Open("testdata/v1.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	var lines [][]byte
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, bytes.Clone(scanner.Bytes()))
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return lines
}

func TestALineV1WroteIsWrittenBackWithTheSameBytes(t *testing.T) {
	lines := fixtureLines(t)
	if len(lines) != 9 {
		t.Fatalf("the fixture has %d lines, want 9", len(lines))
	}
	for i, line := range lines {
		if got := roundTrip(t, line); !bytes.Equal(got, line) {
			t.Errorf("line %d changed:\n got  %s\n want %s", i+1, got, line)
		}
	}
}

func TestTextAndNumbersKeepV1Spelling(t *testing.T) {
	const head = `{"v":1,"id":"a","at":1,"source":"autoreview",`
	const tail = `"panel":[],"findings":[],"reviewed":true}`
	cases := []struct {
		name   string
		fields string
	}{
		{"an angle bracket and an ampersand are not escaped", `"repo":"acme/<widgets>&co",`},
		{"a line separator is not escaped", "\"repo\":\"acme/wid\u2028gets\u2029\","},
		{"a control stays escaped", `"repo":"acme\t\u001b[31mwidgets\n",`},
		{"a quote and a backslash stay escaped", `"repo":"acme\\\"widgets\"",`},
		{"text outside ASCII stays as it is", "\"repo\":\"acme/\u00e9\u65e5\U0001F642\","},
		{"a model name with brackets", `"driver_model":"claude-opus-5[1m]",`},
		{"a whole cost keeps its fraction", `"cost_usd":3.0,`},
		{"a zero cost keeps its fraction", `"cost_usd":0.0,`},
		{"a cost with six places", `"cost_usd":2.699934,`},
		{"a cost with seventeen digits", `"cost_usd":10.123456789012344,`},
		{"a small cost", `"cost_usd":0.00001234,`},
		{"a cost too small to write plainly", `"cost_usd":1.5e-6,`},
		{"a cost too large to write plainly", `"cost_usd":1e+16,`},
		{"a zero count is not a missing count", `"counts":{"must_fix":0},`},
		{"counts with nothing in them", `"counts":{},`},
		{"PR zero is not a missing PR", `"pr":0,`},
		{"an empty string is not a missing field", `"repo":"","session":"","decision":"","risk":"",`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			line := []byte(head + c.fields + tail)
			if got := roundTrip(t, line); !bytes.Equal(got, line) {
				t.Errorf("the line changed:\n got  %q\n want %q", got, line)
			}
		})
	}
}

func TestOnlyADirectCallKeepsEveryByte(t *testing.T) {
	// encoding/json rewrites what MarshalJSON returns: json.Marshal escapes
	// `<`, `>` and `&`. What it does to U+2028 has changed between Go
	// releases. So Append calls MarshalJSON itself, and this test says why
	// it must.
	const head = `{"v":1,"id":"a","at":1,"source":"panel","repo":"`
	const tail = `","panel":[],"findings":[],"reviewed":true}`
	for _, repo := range []string{"a<b>&c", "a\u2028b", "a\u2029b"} {
		line := []byte(head + repo + tail)
		var record Record
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatal(err)
		}
		direct, err := record.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(direct, line) {
			t.Errorf("a direct call changed the line:\n got  %q\n want %q", direct, line)
		}
	}

	var record Record
	if err := json.Unmarshal([]byte(head+"a<b"+tail), &record); err != nil {
		t.Fatal(err)
	}
	through, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(through, []byte(`a\u003cb`)) {
		t.Errorf("json.Marshal wrote %q: it no longer escapes <, and this test's reason for being is gone", through)
	}
}

func TestAListIsWrittenAsAnArrayNeverNull(t *testing.T) {
	record := Record{
		V:        Version,
		ID:       "a",
		At:       1,
		Source:   "autoreview",
		Findings: []Finding{{Count: 1}},
	}
	got, err := record.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	want := `{"v":1,"id":"a","at":1,"source":"autoreview","panel":[],"findings":[{"flagged_by":[],"count":1}],"reviewed":false}`
	if string(got) != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if strings.Contains(string(got), "null") {
		t.Errorf("v1 drops a record with a null list: %s", got)
	}
}

func TestEveryFieldIsWrittenInV1Order(t *testing.T) {
	record := Record{
		V:            Version,
		ID:           "autoreview:sid:100",
		At:           160,
		Source:       "autoreview",
		Repo:         ptr("acme/widgets"),
		PR:           ptr(uint64(9)),
		Session:      ptr("sid"),
		Decision:     ptr("changes-requested"),
		Risk:         ptr("MEDIUM"),
		Counts:       &Counts{MustFix: ptr(uint64(0)), ShouldFix: ptr(uint64(1)), Polish: ptr(uint64(0))},
		CostUSD:      ptr(0.42),
		DurationSecs: ptr(uint64(12)),
		DriverModel:  ptr("claude-fable-5"),
		Panel: []PanelEntry{{
			Name:         "codex",
			Model:        ptr("gpt-5.5"),
			OK:           ptr(true),
			Findings:     ptr(uint64(1)),
			Top:          ptr("MEDIUM"),
			DurationSecs: ptr(uint64(40)),
			ExitCode:     ptr(int32(0)),
		}},
		Findings: []Finding{{
			Severity:  ptr("MEDIUM"),
			Location:  ptr("src/a.rs:1"),
			FlaggedBy: []Source{{Name: "codex", Model: ptr("gpt-5.5")}, {Name: "claude"}},
			Count:     2,
			Dropped:   true,
		}},
		Reviewed: true,
	}
	got, err := record.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	want := `{"v":1,"id":"autoreview:sid:100","at":160,"source":"autoreview","repo":"acme/widgets","pr":9,` +
		`"session":"sid","decision":"changes-requested","risk":"MEDIUM",` +
		`"counts":{"must_fix":0,"should_fix":1,"polish":0},"cost_usd":0.42,"duration_secs":12,` +
		`"driver_model":"claude-fable-5",` +
		`"panel":[{"name":"codex","model":"gpt-5.5","ok":true,"findings":1,"top":"MEDIUM","duration_secs":40,"exit_code":0}],` +
		`"findings":[{"severity":"MEDIUM","location":"src/a.rs:1",` +
		`"flagged_by":[{"name":"codex","model":"gpt-5.5"},{"name":"claude"}],"count":2,"dropped":true}],` +
		`"reviewed":true}`
	if string(got) != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestARecordFromBeforeTheReviewedFieldReadsAsReviewed(t *testing.T) {
	var record Record
	if err := json.Unmarshal([]byte(`{"v":1,"id":"old","at":1,"source":"transcript"}`), &record); err != nil {
		t.Fatal(err)
	}
	if !record.Reviewed {
		t.Error("Reviewed = false for a record with no such field, want true")
	}
	if len(record.Panel) != 0 || len(record.Findings) != 0 {
		t.Errorf("missing lists read as %v and %v, want none", record.Panel, record.Findings)
	}
}

func TestALineV1RefusesIsRefused(t *testing.T) {
	const ok = `"v":1,"id":"a","at":1,"source":"panel"`
	cases := []struct {
		name string
		line string
	}{
		{"no version", `{"id":"a","at":1,"source":"panel"}`},
		{"no id", `{"v":1,"at":1,"source":"panel"}`},
		{"no time", `{"v":1,"id":"a","source":"panel"}`},
		{"no source", `{"v":1,"id":"a","at":1}`},
		{"a null id", `{"v":1,"id":null,"at":1,"source":"panel"}`},
		{"a null panel", `{` + ok + `,"panel":null}`},
		{"null findings", `{` + ok + `,"findings":null}`},
		{"a null flagged-by list", `{` + ok + `,"findings":[{"flagged_by":null,"count":1}]}`},
		{"a null reviewed", `{` + ok + `,"reviewed":null}`},
		{"a null dropped", `{` + ok + `,"findings":[{"count":1,"dropped":null}]}`},
		{"a null panelist", `{` + ok + `,"panel":[null]}`},
		{"a panelist with no name", `{` + ok + `,"panel":[{"model":"gpt-5.5"}]}`},
		{"a finding with no count", `{` + ok + `,"findings":[{"flagged_by":[]}]}`},
		{"a source with no name", `{` + ok + `,"findings":[{"flagged_by":[{"model":"gpt-5.5"}],"count":1}]}`},
		{"a field name in another case", `{"V":1,"ID":"a","At":1,"Source":"panel"}`},
		{"a field given twice", `{` + ok + `,"id":"b"}`},
		{"a version that is text", `{"v":"1","id":"a","at":1,"source":"panel"}`},
		{"a negative PR", `{` + ok + `,"pr":-1}`},
		{"a fractional count", `{` + ok + `,"findings":[{"count":1.5}]}`},
		{"a count past 32 bits", `{` + ok + `,"findings":[{"count":4294967296}]}`},
		{"an exit code past 32 bits", `{` + ok + `,"panel":[{"name":"codex","exit_code":2147483648}]}`},
		{"a repo that is a number", `{` + ok + `,"repo":7}`},
		{"a list where a record goes", `[1,"a",1,"panel"]`},
		{"null", `null`},
		{"a torn line", `{"v":1,"id":"c"`},
		{"nothing", ``},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var record Record
			if err := json.Unmarshal([]byte(c.line), &record); err == nil {
				t.Errorf("the line parsed as %+v, want an error", record)
			}
		})
	}
}

func TestALineV1ReadsIsRead(t *testing.T) {
	const ok = `"v":1,"id":"a","at":1,"source":"panel"`
	cases := []struct {
		name string
		line string
	}{
		{"a null where a field may be missing", `{` + ok + `,"repo":null,"pr":null,"counts":null,"cost_usd":null}`},
		{"a field this build does not know", `{` + ok + `,"later":{"x":[1,2]},"later":2}`},
		{"a whole number where a float goes", `{` + ok + `,"cost_usd":3}`},
		{"space around the record", "  {" + ok + "}\r"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var record Record
			if err := json.Unmarshal([]byte(c.line), &record); err != nil {
				t.Errorf("the line did not parse: %v", err)
			}
			if record.ID != "a" {
				t.Errorf("ID = %q, want a", record.ID)
			}
		})
	}
}

func TestAFailedReadLeavesTheRecordAsItWas(t *testing.T) {
	record := Record{ID: "kept"}
	if err := json.Unmarshal([]byte(`{"v":1,"id":"other","at":1}`), &record); err == nil {
		t.Fatal("a line with no source parsed")
	}
	if record.ID != "kept" {
		t.Errorf("ID = %q after a failed read, want kept", record.ID)
	}
}
