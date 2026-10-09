package activity

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// at is a timestamp the way Claude Code writes one, with milliseconds.
const at = "2026-09-01T11:00:05.123Z"

// assistant is one transcript line: an assistant message holding block.
func assistant(stamp, block string) string {
	return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"cwd":"/tmp/x","message":{"role":"assistant","content":[%s]}}`, stamp, block)
}

// tool is a tool_use block; input is raw JSON.
func tool(name, input string) string {
	return fmt.Sprintf(`{"type":"tool_use","id":"toolu_1","name":%q,"input":%s}`, name, input)
}

// eventsOf parses one transcript line with nothing filtered by time.
func eventsOf(line string) []Event {
	return transcriptEvents([]byte(line), time.Time{})
}

// clock hands out a time one lookup interval later on every call, so a
// transcript not yet found is looked for again on every poll.
type clock struct{ now time.Time }

func (c *clock) tick() time.Time {
	c.now = c.now.Add(lookupEvery)
	return c.now
}

// file is a path in a fresh temporary directory, not yet written.
func file(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(t.TempDir(), name)
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func appendTo(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// existing finds path once it exists, the way session.Store.Transcript
// finds a transcript.
func existing(path string) func() (string, bool) {
	return func() (string, bool) {
		_, err := os.Stat(path)
		return path, err == nil
	}
}

func summaries(events []Event) []string {
	out := make([]string, len(events))
	for i, e := range events {
		out[i] = e.Summary
	}
	return out
}
