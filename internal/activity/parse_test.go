package activity

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestAToolCallIsSummarizedByWhatAReaderWants(t *testing.T) {
	cases := []struct {
		block, tool, summary string
	}{
		{tool("Bash", `{"command":"cargo test --quiet\necho done","description":"Run the unit tests"}`), "Bash", "Run the unit tests"},
		{tool("Bash", `{"command":"cargo test --quiet\necho done"}`), "Bash", "cargo test --quiet"},
		{tool("Bash", `{"command":"  \n go vet ./...\r\nmore","description":"  "}`), "Bash", "go vet ./..."},
		{tool("Read", `{"file_path":"/repo/src/pool.rs"}`), "Read", "pool.rs"},
		{tool("Edit", `{"file_path":"/repo/src/ui.rs","old_string":"a","new_string":"b"}`), "Edit", "ui.rs"},
		{tool("Write", `{"file_path":"notes.md"}`), "Write", "notes.md"},
		{tool("MultiEdit", `{"file_path":"/a/b/c.go"}`), "MultiEdit", "c.go"},
		{tool("NotebookEdit", `{"notebook_path":"/n/book.ipynb"}`), "NotebookEdit", "book.ipynb"},
		{tool("Grep", `{"pattern":"fn spawn","path":"src"}`), "Grep", "fn spawn"},
		{tool("Glob", `{"pattern":"**/*.go"}`), "Glob", "**/*.go"},
		{tool("Skill", `{"skill":"panel-review","args":"9"}`), "Skill", "panel-review"},
		{tool("Agent", `{"description":"Verify the retry path","prompt":"..."}`), "Agent", "Verify the retry path"},
		{tool("Task", `{"description":"Read the tests"}`), "Task", "Read the tests"},
		{tool("WebFetch", `{"url":"https://example.com/x"}`), "WebFetch", "https://example.com/x"},
		{tool("WebSearch", `{"query":"go json"}`), "WebSearch", "go json"},
		{tool("ToolSearch", `{"query":"select:Read"}`), "ToolSearch", "select:Read"},
		// A tool this does not know is named and nothing more, and an MCP
		// tool is named by its method half.
		{tool("mcp__slack__send", `{"channel":"c"}`), "send", ""},
		{tool("Bewildering", `{"x":1}`), "Bewildering", ""},
		// A field of another type reads as absent, and the rest still counts.
		{tool("Bash", `{"description":5,"command":"ls"}`), "Bash", "ls"},
		{tool("Read", `"not an object"`), "Read", ""},
	}
	for _, c := range cases {
		events := eventsOf(assistant(at, c.block))
		if len(events) != 1 {
			t.Fatalf("%s: got %d events, want 1", c.block, len(events))
		}
		if events[0].Tool != c.tool || events[0].Summary != c.summary {
			t.Errorf("%s: got %q %q, want %q %q", c.block, events[0].Tool, events[0].Summary, c.tool, c.summary)
		}
		if events[0].At.IsZero() {
			t.Errorf("%s: the timestamp was lost", c.block)
		}
	}
}

func TestTextIsItsFirstLineAndThinkingIsNothing(t *testing.T) {
	said := eventsOf(assistant(at, `{"type":"text","text":"\n\nLooking at the diff first.\nThen the tests."}`))
	if len(said) != 1 || said[0].Tool != "" || said[0].Summary != "Looking at the diff first." {
		t.Errorf("text read as %+v", said)
	}
	cases := map[string]string{
		"thinking":       `{"type":"thinking","thinking":"hmm"}`,
		"blank text":     `{"type":"text","text":" \n\t\n"}`,
		"unknown block":  `{"type":"image"}`,
		"a bare string":  `"hello"`,
		"a typeless one": `{"text":"no type"}`,
	}
	for name, block := range cases {
		if got := eventsOf(assistant(at, block)); len(got) != 0 {
			t.Errorf("%s read as %+v", name, got)
		}
	}
}

func TestOneLineCanCarrySeveralEventsInOrder(t *testing.T) {
	line := assistant(at, `{"type":"thinking","thinking":"x"},{"type":"text","text":"Reading."},`+tool("Read", `{"file_path":"a.go"}`))
	got := eventsOf(line)
	if len(got) != 2 || got[0].Tool != "" || got[1].Tool != "Read" {
		t.Errorf("got %+v", got)
	}
}

func TestLinesOfOtherShapesReadAsNothing(t *testing.T) {
	lines := []string{
		`{"type":"user","timestamp":"` + at + `","message":{"role":"user","content":[{"type":"tool_result","content":"ok"}]}}`,
		`{"type":"user","message":{"role":"user","content":"/auto-review 9"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":"a string"}}`,
		`{"type":"atis-latch","atis":{}}`,
		"not json at all",
		"",
	}
	for _, line := range lines {
		if got := eventsOf(line); len(got) != 0 {
			t.Errorf("%q read as %+v", line, got)
		}
	}
}

func TestAnOlderEntryBelongsToAnotherRun(t *testing.T) {
	line := []byte(assistant(at, tool("Read", `{"file_path":"a.rs"}`)))
	cases := []struct {
		name  string
		since time.Time
		kept  bool
	}{
		{"the same second, despite its milliseconds", time.Unix(1_788_260_405, 900_000_000), true},
		{"the second before", time.Unix(1_788_260_404, 0), true},
		{"the second after", time.Unix(1_788_260_406, 0), false},
	}
	for _, c := range cases {
		if got := len(transcriptEvents(line, c.since)) == 1; got != c.kept {
			t.Errorf("%s: kept is %v, want %v", c.name, got, c.kept)
		}
	}
}

func TestALineWithNoTimestampIsKeptWithAZeroTime(t *testing.T) {
	cases := []string{
		`{"type":"assistant","message":{"content":[` + tool("Read", `{"file_path":"a.rs"}`) + `]}}`,
		assistant("yesterday", tool("Read", `{"file_path":"a.rs"}`)),
	}
	for _, line := range cases {
		got := transcriptEvents([]byte(line), time.Unix(1_788_260_405, 0))
		if len(got) != 1 || !got[0].At.IsZero() {
			t.Errorf("%s read as %+v", line, got)
		}
	}
	got := eventsOf(assistant(at, tool("Read", `{}`)))
	want := time.Date(2026, 9, 1, 11, 0, 5, 123_000_000, time.UTC)
	if !got[0].At.Equal(want) {
		t.Errorf("At is %v, want %v", got[0].At, want)
	}
}

func TestSummariesLoseTheirControlBytesAndTheirLength(t *testing.T) {
	events := eventsOf(assistant(at, tool("Bash", `{"command":"echo \u001b[31mred\u001b[0m"}`)))
	if events[0].Summary != "echo [31mred[0m" {
		t.Errorf("got %q", events[0].Summary)
	}
	long := strings.Repeat("x", summaryWidth+50)
	cases := []string{
		tool("Bash", `{"command":"`+long+`"}`),
		`{"type":"text","text":"` + long + `"}`,
	}
	for _, block := range cases {
		got := eventsOf(assistant(at, block))[0].Summary
		if n := utf8.RuneCountInString(got); n != summaryWidth || !strings.HasSuffix(got, "…") {
			t.Errorf("a long summary is %d characters, want %d ending in an ellipsis", n, summaryWidth)
		}
	}
}

func TestAToolNameLosesItsControlBytes(t *testing.T) {
	cases := []struct {
		why, block, tool, summary string
	}{
		{"cleaned before the summary is chosen", `{"type":"tool_use","name":"Ba\u202Esh","input":{"command":"ls"}}`, "Bash", "ls"},
		{"a name of marks leaves a word", `{"type":"tool_use","name":"\u202E\u200B","input":{}}`, "tool", ""},
		{"a missing name leaves a word", `{"type":"tool_use","input":{}}`, "tool", ""},
		{"space is not part of a name", `{"type":"tool_use","name":" Read ","input":{"file_path":"/x/a.rs"}}`, "Read", "a.rs"},
	}
	for _, c := range cases {
		got := eventsOf(assistant(at, c.block))[0]
		if got.Tool != c.tool || got.Summary != c.summary {
			t.Errorf("%s: got %q %q, want %q %q", c.why, got.Tool, got.Summary, c.tool, c.summary)
		}
	}
}

func TestAnMCPToolIsNamedByItsMethod(t *testing.T) {
	nameOf := func(name string) string {
		return eventsOf(assistant(at, tool(name, `{}`)))[0].Tool
	}
	cases := []struct{ name, want string }{
		{"mcp__plugin_slack_slack__slack_send_message", "slack_send_message"},
		{"Bash", "Bash"},
		{"mcp__github__list__issues", "list__issues"},
		{"mcp__", "mcp__"},
		{"mcp__server__", "mcp__server__"},
		{"mcp__server__ ", "mcp__server__"},
	}
	for _, c := range cases {
		if got := nameOf(c.name); got != c.want {
			t.Errorf("%q draws as %q, want %q", c.name, got, c.want)
		}
	}
	send := nameOf("mcp__plugin_slack_slack__slack_send_message")
	update := nameOf("mcp__plugin_slack_slack__slack_update_list_record")
	if send == update {
		t.Errorf("two tools on one server draw alike: %q", send)
	}
	for _, name := range []string{send, update, nameOf(strings.Repeat("n", 40))} {
		if utf8.RuneCountInString(name) > nameWidth {
			t.Errorf("%q is wider than %d", name, nameWidth)
		}
	}
}

func TestAPlainLineIsItsTextAndABlankLineIsNothing(t *testing.T) {
	cases := []struct {
		line string
		want []string
	}{
		{"running the suite", []string{"running the suite"}},
		{"done\r", []string{"done"}},
		{"\x1b[2Kred", []string{"[2Kred"}},
		{"", nil},
		{" \t ", nil},
		{"\u200B", nil},
	}
	for _, c := range cases {
		got := summaries(plainEvents([]byte(c.line)))
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("%q read as %q, want %q", c.line, got, c.want)
		}
	}
}
