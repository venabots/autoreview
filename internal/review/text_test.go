package review

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// envelopeWith is dash-p's stdout around one answer.
func envelopeWith(t *testing.T, answer string) string {
	t.Helper()
	out, err := json.Marshal(struct {
		Answer string `json:"answer"`
		Status string `json:"exit_status"`
	}{answer, "ok"})
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// saidAt is one assistant message in a transcript. at is "" for an entry
// with no timestamp.
func saidAt(t *testing.T, at, text string) string {
	t.Helper()
	type part struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	type message struct {
		Content []part `json:"content"`
	}
	out, err := json.Marshal(struct {
		Type      string  `json:"type"`
		Timestamp string  `json:"timestamp,omitempty"`
		Message   message `json:"message"`
	}{"assistant", at, message{[]part{{"text", text}}}})
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

const (
	// A tool call, which is not what the reviewer said.
	toolCall = `{"type":"assistant","timestamp":"2026-09-01T10:00:02.000Z","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"gh pr diff 9"}}]}}`
	// What the person typed, with content that is a string and not a list.
	userTurn = `{"type":"user","timestamp":"2026-09-01T10:00:00.000Z","message":{"content":"/auto-review 9"}}`
	// A last line the writer has not finished.
	tornLine = `{"type":"assis`
)

func transcriptWith(t *testing.T, lines ...string) string {
	t.Helper()
	return fileWith(t, strings.Join(lines, "\n"))
}

func at(clock string) time.Time {
	parsed, err := time.Parse(time.RFC3339, clock)
	if err != nil {
		panic(err)
	}
	return parsed
}

func TestATranscriptIsWhatTheReviewerSaid(t *testing.T) {
	path := transcriptWith(t,
		userTurn,
		saidAt(t, "2026-09-01T10:00:01.000Z", "## Overview\n\nThe real review.  \n"),
		toolCall,
		saidAt(t, "2026-09-01T10:00:05.000Z", "The script has exited."),
		saidAt(t, "2026-09-01T10:00:06.000Z", "  \n"),
		tornLine,
	)
	got, ok := ReadTranscript(path, time.Time{})
	want := "## Overview\n\nThe real review.\n\nThe script has exited."
	if !ok || got != want {
		t.Errorf("ReadTranscript = %q, %v; want %q", got, ok, want)
	}
}

func TestATranscriptIsFilteredByTheTimeOfEachEntry(t *testing.T) {
	first := saidAt(t, "2026-09-01T10:00:00.139Z", "The first pass.")
	second := saidAt(t, "2026-09-01T11:00:00.000Z", "The second pass.")
	undated := saidAt(t, "", "No time on this one.")
	cases := []struct {
		name  string
		lines []string
		since time.Time
		want  string
	}{
		{"everything since the first pass started", []string{first, second}, at("2026-09-01T09:59:00Z"), "The first pass.\n\nThe second pass."},
		// A resumed session already holds every earlier review. This pass's
		// file must hold this pass's review, or a run that resumes for days
		// grows one file without end.
		{"only this pass", []string{first, second}, at("2026-09-01T10:30:00Z"), "The second pass."},
		// The file is rewritten shorter, as a compaction would do. The
		// filter is the timestamp and not a place in the file.
		{"after the file was rewritten", []string{second}, at("2026-09-01T10:30:00Z"), "The second pass."},
		// Claude Code writes milliseconds. The comparison is by the second,
		// so an entry in the second the review started in is this review's.
		{"an entry in the same second is kept", []string{first}, at("2026-09-01T10:00:00.900Z"), "The first pass."},
		{"an entry one second early is not", []string{first, second}, at("2026-09-01T10:00:01Z"), "The second pass."},
		{"a time with an offset is read", []string{saidAt(t, "2026-09-01T12:00:00+02:00", "Early."), second}, at("2026-09-01T10:30:00Z"), "The second pass."},
		// A review that appears twice reads better than one that is missing.
		{"an entry with no timestamp is kept", []string{first, undated, second}, at("2026-09-01T10:30:00Z"), "No time on this one.\n\nThe second pass."},
		{"an entry with a time that cannot be read is kept", []string{saidAt(t, "not a time", "Kept."), first}, at("2026-09-01T10:30:00Z"), "Kept."},
		{"nothing since", []string{first, second}, at("2026-09-01T12:00:00Z"), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := ReadTranscript(transcriptWith(t, c.lines...), c.since)
			if got != c.want || ok != (c.want != "") {
				t.Errorf("ReadTranscript = %q, %v; want %q", got, ok, c.want)
			}
		})
	}
}

func TestALineOfAnUnexpectedShapeDoesNotLoseTheTranscript(t *testing.T) {
	cases := []struct {
		name string
		line string
		want string
	}{
		// The day a field changes its type, the message is still read. A
		// reader that dropped the line would lose every message at once and
		// fall back to the sign-off in the envelope.
		{"a timestamp that is a number", `{"type":"assistant","timestamp":1788256800,"message":{"content":[{"type":"text","text":"Kept."}]}}`, "Kept.\n\nAfter."},
		{"a block that is not an object", `{"type":"assistant","message":{"content":["loose",{"type":"text","text":"Kept."}]}}`, "Kept.\n\nAfter."},
		{"a text that is not a string", `{"type":"assistant","message":{"content":[{"type":"text","text":7},{"type":"text","text":"Kept."}]}}`, "Kept.\n\nAfter."},
		{"content that is a string", `{"type":"assistant","message":{"content":"loose"}}`, "After."},
		{"a type that is not a string", `{"type":7,"message":{"content":[{"type":"text","text":"Not an assistant."}]}}`, "After."},
		{"no message", `{"type":"assistant"}`, "After."},
		{"a line that is a list", `[1,2]`, "After."},
		{"a line that is not JSON", `not json`, "After."},
		{"an empty line", ``, "After."},
		{"a line that ends in a carriage return", "{\"type\":\"assistant\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"Kept.\"}]}}\r", "Kept.\n\nAfter."},
		{"thinking is not what the reviewer said", `{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"hm","text":"hidden"}]}}`, "After."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := transcriptWith(t, c.line, saidAt(t, "2026-09-01T10:00:00Z", "After."))
			if got, _ := ReadTranscript(path, time.Time{}); got != c.want {
				t.Errorf("ReadTranscript = %q, want %q", got, c.want)
			}
		})
	}
}

func TestATranscriptWithNothingSaidIsNothing(t *testing.T) {
	cases := []struct {
		name string
		path string
	}{
		{"no file", filepath.Join(t.TempDir(), "missing.jsonl")},
		{"an empty file", transcriptWith(t)},
		{"tool calls only", transcriptWith(t, userTurn, toolCall, tornLine)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got, ok := ReadTranscript(c.path, time.Time{}); ok || got != "" {
				t.Errorf("ReadTranscript = %q, %v; want nothing", got, ok)
			}
		})
	}
}

func TestTheAnswerIsTheEnvelopesOrTheWholeStdout(t *testing.T) {
	cases := []struct {
		name   string
		stdout string
		want   string
	}{
		{"dash-p's envelope", envelopeWith(t, "## Overview\n\nLooks fine."), "## Overview\n\nLooks fine."},
		// An override prints its review to stdout. There is no envelope to
		// open, and to reject it left those runs with nothing.
		{"an override's prose", "  my own reviewer said this  \n", "my own reviewer said this"},
		// JSON that is not dash-p's envelope is still what the override
		// printed, so it is the review.
		{"an override's own JSON", `{"result":"my reviewer speaks json"}`, `{"result":"my reviewer speaks json"}`},
		{"JSON with an answer that is not text", `{"answer":42}`, `{"answer":42}`},
		{"JSON with a null answer", `{"answer":null}`, `{"answer":null}`},
		{"JSON that is not an object", `[1,2]`, `[1,2]`},
		{"an empty answer", envelopeWith(t, ""), ""},
		{"a blank answer", envelopeWith(t, " \n"), ""},
		{"an empty stdout", "", ""},
		{"a blank stdout", "   \n", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := ReadAnswer(fileWith(t, c.stdout))
			if got != c.want || ok != (c.want != "") {
				t.Errorf("ReadAnswer = %q, %v; want %q", got, ok, c.want)
			}
		})
	}
	if got, ok := ReadAnswer(filepath.Join(t.TempDir(), "pr-9.json")); ok {
		t.Errorf("ReadAnswer read %q from a file that does not exist", got)
	}
}

func TestTheReviewComesOutOfTheEnvelopeWithoutItsTrailer(t *testing.T) {
	cases := []struct {
		name   string
		stdout string
		want   string
	}{
		{"the trailer is taken off the end", envelopeWith(t, "## Overview\n\nLooks fine.\n\n"+fenced(`{"decision":"none"}`)), "## Overview\n\nLooks fine."},
		// No trailer is ordinary: an override promises none.
		{"no trailer", envelopeWith(t, "just prose"), "just prose"},
		{
			"prose that names the fence keeps the review under it",
			envelopeWith(t, "The reviewer emits a ```autoreview block.\n\nReal finding here."),
			"The reviewer emits a ```autoreview block.\n\nReal finding here.",
		},
		{
			// A quoted example that parses goes too. An example lost from a
			// file is a smaller loss than raw JSON in every review.
			"a block in the middle goes and both sides stay",
			envelopeWith(t, "I would emit:\n\n"+fenced(`{"decision":"none"}`)+"\n\nBut the real finding is here."),
			"I would emit:\n\nBut the real finding is here.",
		},
		{"a review does not open with the gap the trailer left", envelopeWith(t, fenced(`{"decision":"none"}`)+"\n\nThe review."), "The review."},
		// Nothing to write, and not an empty file.
		{"a trailer and nothing else", envelopeWith(t, fenced(`{}`)), ""},
		{"an override's prose", "  my own reviewer said this  \n", "my own reviewer said this"},
		{"an override's JSON", `{"result":"my reviewer speaks json"}`, `{"result":"my reviewer speaks json"}`},
		{"stdout that is not JSON", "not json", "not json"},
		{"an empty stdout", "   \n", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := ReadReview(fileWith(t, c.stdout), "", time.Time{})
			if got != c.want || ok != (c.want != "") {
				t.Errorf("ReadReview = %q, %v; want %q", got, ok, c.want)
			}
		})
	}
}

func TestAReviewSurvivesTheReviewerSigningOffAfterIt(t *testing.T) {
	// A panel review is many messages: the panelist sections, the
	// synthesis, then whatever the reviewer says last. The envelope holds
	// only that last message, so a sign-off left a review file of one
	// sentence about a script.
	const signoff = "The panel-review script has exited cleanly (exit 0)."
	stdout := fileWith(t, envelopeWith(t, signoff))
	transcript := transcriptWith(t,
		saidAt(t, "2026-09-01T10:00:00.000Z", "## Overview\n\nThe real review.\n\n"+fenced(`{"decision":"commented"}`)),
		toolCall,
		saidAt(t, "2026-09-01T10:00:05.000Z", signoff),
		tornLine,
	)

	// With no transcript there is only the envelope, which is the bug.
	if got, _ := ReadReview(stdout, "", time.Time{}); got != signoff {
		t.Errorf("with no transcript ReadReview = %q, want the envelope's answer", got)
	}

	got, ok := ReadReview(stdout, transcript, at("2026-09-01T09:59:00Z"))
	if want := "## Overview\n\nThe real review.\n\n" + signoff; !ok || got != want {
		t.Errorf("ReadReview = %q, want %q", got, want)
	}
	if strings.Contains(got, "tool_use") || strings.Contains(got, "autoreview") {
		t.Errorf("the review holds a tool call or the trailer: %q", got)
	}
}

func TestAReviewFallsBackToTheAnswer(t *testing.T) {
	stdout := fileWith(t, envelopeWith(t, "The answer."))
	old := transcriptWith(t, saidAt(t, "2026-09-01T10:00:00Z", "An earlier pass."))
	cases := []struct {
		name       string
		transcript string
		since      time.Time
	}{
		{"a transcript that is not there", filepath.Join(t.TempDir(), "gone.jsonl"), time.Time{}},
		{"a transcript with nothing since the review started", old, at("2026-09-01T11:00:00Z")},
		{"a transcript with no assistant message", transcriptWith(t, userTurn, toolCall), time.Time{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got, ok := ReadReview(stdout, c.transcript, c.since); !ok || got != "The answer." {
				t.Errorf("ReadReview = %q, %v; want the answer", got, ok)
			}
		})
	}
	if got, ok := ReadReview(filepath.Join(t.TempDir(), "gone.json"), "", time.Time{}); ok {
		t.Errorf("ReadReview read %q with no stdout and no transcript", got)
	}
}

func TestTheTrailerIsReadFromTheTranscriptBeforeTheAnswer(t *testing.T) {
	const signoff = "The script has exited."
	review := saidAt(t, "2026-09-01T10:00:00Z", "The review.\n\n"+fenced(`{"decision":"approved","risk":"LOW"}`))
	earlier := saidAt(t, "2026-09-01T08:00:00Z", "An earlier pass.\n\n"+fenced(`{"decision":"changes-requested"}`))
	since := at("2026-09-01T09:59:00Z")
	cases := []struct {
		name       string
		stdout     string
		transcript string
		want       string
	}{
		// A reviewer that says anything after the block pushes it out of
		// the envelope and into an earlier message.
		{"a sign-off pushed the block out of the answer", envelopeWith(t, signoff), transcriptWith(t, review, saidAt(t, "2026-09-01T10:00:05Z", signoff)), "approved"},
		{"the transcript outranks the answer", envelopeWith(t, fenced(`{"decision":"none"}`)), transcriptWith(t, review), "approved"},
		{"an earlier pass's block is not this pass's", envelopeWith(t, fenced(`{"decision":"commented"}`)), transcriptWith(t, earlier), "commented"},
		{"a transcript with no block falls back to the answer", envelopeWith(t, fenced(`{"decision":"commented"}`)), transcriptWith(t, saidAt(t, "2026-09-01T10:00:00Z", "No block.")), "commented"},
		{"no transcript reads the answer", envelopeWith(t, "Done.\n\n"+fenced(`{"decision":"commented"}`)), "", "commented"},
		{"a transcript that is not there reads the answer", envelopeWith(t, fenced(`{"decision":"commented"}`)), filepath.Join(t.TempDir(), "gone.jsonl"), "commented"},
		{"no block anywhere", envelopeWith(t, signoff), transcriptWith(t, saidAt(t, "2026-09-01T10:00:00Z", "No block.")), ""},
		{"an empty stdout and no transcript", "", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			trailer, ok := ReadTrailer(fileWith(t, c.stdout), c.transcript, since)
			if trailer.Decision != c.want || ok != (c.want != "") {
				t.Errorf("ReadTrailer = %q, %v; want %q", trailer.Decision, ok, c.want)
			}
		})
	}
}

func TestAFailureReasonComesOutOfTheEnvelope(t *testing.T) {
	// The shape a usage limit leaves: dash-p exits 10 and the envelope's
	// answer holds claude's own notice.
	const limit = "You've hit your session limit · resets 12pm (America/New_York)"
	cases := []struct {
		name   string
		stdout string
		want   string
	}{
		{"a usage limit", envelopeWith(t, limit), limit},
		// Many lines of crash text become the words on one line.
		{"many lines", envelopeWith(t, "API Error: 500\n   something broke\nbadly"), "API Error: 500 something broke badly"},
		// A killed job and a timeout leave an empty stdout.
		{"an empty stdout", "", ""},
		{"an empty answer", envelopeWith(t, ""), ""},
		{"an answer of control bytes only", envelopeWith(t, "\x1b\x07 \n"), ""},
		// An override's stdout is its review and not a reason.
		{"an override's prose", "my reviewer's prose", ""},
		{"an override's JSON", `{"result":"failed"}`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := AgentError(fileWith(t, c.stdout))
			if got != c.want || ok != (c.want != "") {
				t.Errorf("AgentError = %q, %v; want %q", got, ok, c.want)
			}
		})
	}
	if got, ok := AgentError(filepath.Join(t.TempDir(), "pr-9.json")); ok {
		t.Errorf("AgentError read %q from a file that does not exist", got)
	}
}

func TestAFailureReasonIsOneCleanLineOfBoundedLength(t *testing.T) {
	long := strings.Repeat("x", 200) + "\x1b[31m"
	got, ok := AgentError(fileWith(t, envelopeWith(t, long)))
	if !ok || utf8.RuneCountInString(got) != 80 {
		t.Errorf("AgentError is %d characters, want it cut at 80", utf8.RuneCountInString(got))
	}

	got, _ = AgentError(fileWith(t, envelopeWith(t, "rate\x1b[31m limited\u202e\nretry\tlater")))
	if want := "rate[31m limited retry later"; got != want {
		t.Errorf("AgentError = %q, want %q", got, want)
	}
}
