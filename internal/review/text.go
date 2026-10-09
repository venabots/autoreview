package review

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/venabots/autoreview/internal/sanitize"
)

// ReadReview is everything the reviewer said, without the trailer. It is
// what a person reads when the review did not land on the PR. It is false
// when the reviewer said nothing, so no empty file gets written.
//
// The session transcript is read first, and dash-p's envelope second. The
// envelope holds only the reviewer's final message, and a panel review is
// many messages: the panelist sections, the synthesis, then whatever the
// model says last. A reviewer that signs off after its review leaves the
// envelope with the sign-off and nothing else.
//
// transcriptPath is "" when the review has no transcript. The text is the
// reviewer's own: pass it through sanitize.Block before it is drawn.
func ReadReview(stdoutPath, transcriptPath string, since time.Time) (string, bool) {
	text, ok := said(stdoutPath, transcriptPath, since)
	if !ok {
		return "", false
	}
	body := strings.TrimSpace(StripTrailer(text))
	return body, body != ""
}

// ReadTrailer is the trailer of a finished review: from the session
// transcript when it has one, and from dash-p's envelope otherwise.
//
// The transcript is first for the reason ReadReview gives. The trailer is
// asked for at the end of the review, and a reviewer that says anything
// after it pushes the block out of the envelope and into an earlier
// message.
func ReadTrailer(stdoutPath, transcriptPath string, since time.Time) (Trailer, bool) {
	if transcriptPath != "" {
		if text, ok := ReadTranscript(transcriptPath, since); ok {
			if trailer, ok := ParseTrailer(text); ok {
				return trailer, true
			}
		}
	}
	answer, ok := ReadAnswer(stdoutPath)
	if !ok {
		return Trailer{}, false
	}
	return ParseTrailer(answer)
}

// said is the reviewer's words: the transcript when it has any, and the
// answer otherwise.
func said(stdoutPath, transcriptPath string, since time.Time) (string, bool) {
	if transcriptPath != "" {
		if text, ok := ReadTranscript(transcriptPath, since); ok {
			return text, true
		}
	}
	return ReadAnswer(stdoutPath)
}

// envelope is the part of dash-p's stdout that holds the reviewer's words.
// Answer is a pointer so that a stdout with no answer in it is told from an
// envelope whose answer is empty.
type envelope struct {
	Answer *string `json:"answer"`
}

// envelopeAnswer is the answer of dash-p's envelope, and false for a stdout
// that is not the envelope.
func envelopeAnswer(raw []byte) (string, bool) {
	var e envelope
	if err := json.Unmarshal(raw, &e); err != nil || e.Answer == nil {
		return "", false
	}
	return *e.Answer, true
}

// ReadAnswer is the final message of a review, from dash-p's envelope. It
// is false when there is nothing to read.
//
// A command override promises no envelope. A stdout that is not one, prose
// or JSON with no answer in it, is taken as the review, because that is
// what the override printed. To reject it would leave those runs with no
// review file at all.
func ReadAnswer(stdoutPath string) (string, bool) {
	raw, err := os.ReadFile(stdoutPath)
	if err != nil {
		return "", false
	}
	text, isEnvelope := envelopeAnswer(raw)
	if !isEnvelope {
		text = strings.TrimSpace(string(raw))
	}
	if strings.TrimSpace(text) == "" {
		return "", false
	}
	return text, true
}

// AgentError is why a failed review failed, in the harness's own words. On
// an agent error the answer of dash-p's envelope holds claude's error text,
// for example a usage-limit notice, and not a review. Without it, exit 10
// is a number the reader has to go and decode.
//
// Only the envelope is read. An override's stdout is its review and not a
// reason, so a stdout that is not the envelope gives no reason.
//
// The result is one line, cleaned and cut like every other string an agent
// wrote. The whitespace is folded first: the cleaning drops a newline, and
// that would join the last word of one line to the first word of the next.
func AgentError(stdoutPath string) (string, bool) {
	raw, err := os.ReadFile(stdoutPath)
	if err != nil {
		return "", false
	}
	answer, ok := envelopeAnswer(raw)
	if !ok {
		return "", false
	}
	oneLine := strings.Join(strings.Fields(answer), " ")
	reason := capped(sanitize.Line(oneLine), maxFieldChars)
	return reason, reason != ""
}

// transcriptEntry is one line of a Claude Code session transcript.
type transcriptEntry struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Message   struct {
		Content []contentBlock `json:"content"`
	} `json:"message"`
}

// contentBlock is one part of a message: text, a tool call or a tool
// result.
type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ReadTranscript is every assistant message written at or after since, in
// order, with a blank line between them. It is false when there is none.
//
// Tool calls and tool results are left out: what is wanted is what the
// reviewer said. A line that does not parse is skipped and the read goes
// on, because another program writes the file while this one reads it and
// a torn last line is ordinary.
//
// The filter is the entry's own timestamp and not its place in the file. A
// resumed session already holds every earlier review, and those belong to
// the passes that wrote them. A byte offset would say the same until the
// file was compacted or rewritten, and then it would be wrong.
//
// An entry with no timestamp, or one that cannot be read, is kept. A
// review that appears twice reads better than a review that is missing.
// The comparison is by the second, which is how exactly the caller knows
// when the review started.
func ReadTranscript(path string, since time.Time) (string, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var parts []string
	for line := range bytes.SplitSeq(raw, []byte("\n")) {
		entry, ok := decodeEntry(line)
		if !ok || entry.Type != "assistant" || writtenBefore(entry.Timestamp, since) {
			continue
		}
		parts = append(parts, textsOf(entry)...)
	}
	return strings.Join(parts, "\n\n"), len(parts) > 0
}

// decodeEntry reads one transcript line.
//
// A field of an unexpected type is left at its zero value and the rest of
// the line is still used. The decoder reports such a field as an error
// after it has filled everything else, and to drop the line for it would
// lose a whole message the day one field changes its type.
func decodeEntry(line []byte) (transcriptEntry, bool) {
	var entry transcriptEntry
	err := json.Unmarshal(line, &entry)
	var wrongType *json.UnmarshalTypeError
	if err != nil && !errors.As(err, &wrongType) {
		return transcriptEntry{}, false
	}
	return entry, true
}

// writtenBefore says the entry is known to be older than since.
//
// Claude Code writes the time with milliseconds ("...T06:54:13.139Z"). A
// parser that wants whole seconds fails on every one of them, and then
// every entry is kept and the filter does nothing, with no sign of it.
func writtenBefore(timestamp string, since time.Time) bool {
	at, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return false
	}
	return at.Unix() < since.Unix()
}

func textsOf(entry transcriptEntry) []string {
	var texts []string
	for _, part := range entry.Message.Content {
		if part.Type != "text" {
			continue
		}
		if text := strings.TrimSpace(part.Text); text != "" {
			texts = append(texts, text)
		}
	}
	return texts
}
