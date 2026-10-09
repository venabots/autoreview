package activity

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/venabots/autoreview/internal/sanitize"
)

// entry is the slice of a transcript line the tail reads. Everything else
// on the line (cwd, uuids, the git branch) is ignored.
type entry struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Message   struct {
		Content []block `json:"content"`
	} `json:"message"`
}

// block is one part of an assistant message: a tool call, text, or
// thinking.
type block struct {
	Type  string    `json:"type"`
	Name  string    `json:"name"`
	Text  string    `json:"text"`
	Input toolInput `json:"input"`
}

// toolInput holds every field of a tool's input that a summary can come
// from. Each tool fills only some of them, and the rest stay empty.
type toolInput struct {
	Description  string `json:"description"`
	Command      string `json:"command"`
	FilePath     string `json:"file_path"`
	NotebookPath string `json:"notebook_path"`
	Pattern      string `json:"pattern"`
	Skill        string `json:"skill"`
	URL          string `json:"url"`
	Query        string `json:"query"`
}

// decodeEntry reads one transcript line.
//
// The format is not documented, so a line of another shape reads as
// nothing rather than as an error. A field of an unexpected type is left
// empty and the rest of the line is still used: the decoder reports such a
// field only after it has filled everything else, and to drop the line for
// it would lose every tool call the day one input field changes its type.
func decodeEntry(line []byte) (entry, bool) {
	var e entry
	err := json.Unmarshal(line, &e)
	var wrongType *json.UnmarshalTypeError
	if err != nil && !errors.As(err, &wrongType) {
		return entry{}, false
	}
	return e, true
}

// transcriptEvents is the events one transcript line carries: none for
// anything but an assistant line, and none for one written before since.
//
// Entries older than since belong to the review a resumed session is
// resuming, not to this run. The comparison is by the second, which is how
// exactly the caller knows when the review started. A line with no
// timestamp, or one that cannot be read, is kept.
func transcriptEvents(line []byte, since time.Time) []Event {
	e, ok := decodeEntry(line)
	if !ok || e.Type != "assistant" {
		return nil
	}
	at, err := time.Parse(time.RFC3339, e.Timestamp)
	if err != nil {
		at = time.Time{}
	} else if at.Unix() < since.Unix() {
		return nil
	}
	var events []Event
	for _, b := range e.Message.Content {
		if event, ok := blockEvent(b, at); ok {
			events = append(events, event)
		}
	}
	return events
}

// blockEvent is the event of one block. Thinking, and every block of a
// type this does not know, is no event.
func blockEvent(b block, at time.Time) (Event, bool) {
	switch b.Type {
	case "tool_use":
		return toolEvent(b, at), true
	case "text":
		first, ok := firstNonBlank(b.Text)
		if !ok {
			return Event{}, false
		}
		return Event{At: at, Summary: cut(sanitize.Line(first), summaryWidth)}, true
	}
	return Event{}, false
}

// toolEvent names a tool call and summarizes it.
//
// The name is a model's word for the tool it called, and it reaches the
// terminal like every other part of a row, so it is sanitized here, once.
// That happens before the summary is chosen, or a name carrying a mark
// would match no tool and lose the summary beside it. A name that is
// nothing but marks still leaves a word on the row. The name is cut only
// after the summary is chosen, because the match is on the whole name.
func toolEvent(b block, at time.Time) Event {
	name := strings.TrimSpace(sanitize.Line(b.Name))
	if name == "" {
		name = "tool"
	}
	return Event{At: at, Tool: cut(displayName(name), nameWidth), Summary: summarize(name, b.Input)}
}

// summarize is one line about a tool call, from the part of its input a
// reader wants: the command, the file, the pattern, the skill. A tool this
// does not know is named and nothing more.
func summarize(name string, in toolInput) string {
	return cut(sanitize.Line(summarySource(name, in)), summaryWidth)
}

// summarySource picks the input field that says the most about a call. For
// Bash, the description is what the reviewer meant and the command is what
// it ran; the first says more in fewer columns.
func summarySource(name string, in toolInput) string {
	switch name {
	case "Bash":
		if description := strings.TrimSpace(in.Description); description != "" {
			return description
		}
		return firstLine(strings.TrimSpace(in.Command))
	case "Read", "Edit", "Write", "MultiEdit":
		return basename(strings.TrimSpace(in.FilePath))
	case "NotebookEdit":
		return basename(strings.TrimSpace(in.NotebookPath))
	case "Grep", "Glob":
		return strings.TrimSpace(in.Pattern)
	case "Skill":
		return strings.TrimSpace(in.Skill)
	case "Agent", "Task":
		return strings.TrimSpace(in.Description)
	case "WebFetch":
		return strings.TrimSpace(in.URL)
	case "WebSearch", "ToolSearch":
		return strings.TrimSpace(in.Query)
	}
	return ""
}

// displayName is what to call a tool on a row.
//
// An MCP tool is named for its server and its method both, as in
// mcp__plugin_slack_slack__slack_send_message. The server half is the same
// for every tool that server offers and it arrives first, so a name cut to
// fit would draw every one of them alike. Only that half is dropped. A name
// with nothing after the server half names no method, and keeps what it
// has rather than leaving a blank where a name goes.
func displayName(name string) string {
	rest, ok := strings.CutPrefix(name, "mcp__")
	if !ok {
		return name
	}
	if _, method, ok := strings.Cut(rest, "__"); ok && strings.TrimSpace(method) != "" {
		return method
	}
	return name
}

func basename(path string) string {
	return path[strings.LastIndex(path, "/")+1:]
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSuffix(line, "\r")
}

func firstNonBlank(text string) (string, bool) {
	for line := range strings.SplitSeq(text, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed, true
		}
	}
	return "", false
}

// plainEvents is the event of one line of an override's stderr. A blank
// line is no event, so a reviewer that spaces its output out does not fill
// the detail pane with nothing.
func plainEvents(line []byte) []Event {
	summary := cut(sanitize.Line(string(line)), summaryWidth)
	if strings.TrimSpace(summary) == "" {
		return nil
	}
	return []Event{{Summary: summary}}
}
