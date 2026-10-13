package activity

import (
	"slices"
	"time"
)

type kind int

const (
	silent kind = iota
	transcript
	plain
)

// Tail follows one file incrementally.
//
// It is a value. Poll returns the next Tail and never changes the one it
// was called on, so a snapshot that holds a Tail cannot see it change
// under it while the monitor polls. The zero Tail follows nothing.
type Tail struct {
	kind kind
	why  string
	// find locates a transcript. It is asked at most once every
	// lookupEvery until it answers, and never again after that.
	find   func() (string, bool)
	looked time.Time
	path   string
	// fromEnd starts at the end of the file as first seen. A resumed
	// session's transcript already holds every earlier pass, which since
	// would filter out one parsed line at a time. Skipping it is the
	// difference between one stat and re-reading the history on every pass
	// of a run that lasts all day.
	fromEnd bool
	since   time.Time
	offset  int64
	// partial is the bytes after the last newline read: a line the writer
	// had not finished, which may end in part of a character.
	partial   []byte
	events    []Event
	turns     int
	toolCalls int
}

// Transcript follows the transcript of the session a review runs in.
//
// find locates the file. It is looked for at most once a second until it
// is found. Entries older than since are dropped. fromEnd skips what the
// file held when it was first found, for a resumed session.
func Transcript(find func() (string, bool), since time.Time, fromEnd bool) Tail {
	return Tail{kind: transcript, find: find, since: since, fromEnd: fromEnd}
}

// Plain follows an override's stderr: every non-blank line is an event.
//
// A stderr line carries no time, so since filters nothing. The log is made
// fresh for each pass, so it holds no earlier run to filter out.
func Plain(path string, _ time.Time) Tail {
	return Tail{kind: plain, path: path}
}

// Silent is a review with nothing to follow, for example a session claude
// named itself, whose transcript is only found when the review ends.
func Silent(why string) Tail {
	return Tail{kind: silent, why: why}
}

// Events is what the reviewer did, newest last, at most keep of them. The
// slice is a copy, so a caller that changes it changes no Tail.
func (t Tail) Events() []Event {
	return slices.Clone(t.events)
}

// CurrentTool is the tool the reviewer is in right now: the last event,
// when it was a tool call. Text after a tool call means the call is over.
func (t Tail) CurrentTool() string {
	if len(t.events) == 0 {
		return ""
	}
	return t.events[len(t.events)-1].Tool
}

// Turns counts the assistant lines that said or did something. Thinking is
// not a turn.
func (t Tail) Turns() int {
	return t.turns
}

// ToolCalls counts every tool call read, including those no longer kept.
func (t Tail) ToolCalls() int {
	return t.toolCalls
}

// Source is what the view says the tail follows, after "session <id>" for
// a transcript. It is "" for a transcript once found and for a silent
// tail, whose reason is Why. v1 drew "session <id>" for a found
// transcript and "session <id> · waiting for its transcript" before.
func (t Tail) Source() string {
	switch t.kind {
	case transcript:
		if t.found() {
			return ""
		}
		return "waiting for its transcript"
	case plain:
		return "following the reviewer's stderr"
	}
	return ""
}

// Why is why a silent tail follows nothing, and "" for any other tail.
func (t Tail) Why() string {
	if t.kind != silent {
		return ""
	}
	return t.why
}

// found says the file has been seen with something in it.
func (t Tail) found() bool {
	return t.path != "" && t.offset > 0
}
