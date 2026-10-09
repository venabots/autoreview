package activity

import (
	"bytes"
	"errors"
	"io"
	"os"
	"slices"
	"time"
)

// Poll reads what was written since the last poll and returns the next
// Tail. When nothing was written it costs one stat and no open.
//
// Only whole lines are parsed. The bytes after the last newline wait for
// the next poll, and those include any character the writer is in the
// middle of: a newline byte is never part of a longer character, so a
// split character is always in the kept bytes and is decoded once, whole.
// A file that got shorter is a new file under the same name, and is read
// again from its start rather than from the middle of a line.
func (t Tail) Poll(now time.Time) Tail {
	next, ok := t.located(now)
	if !ok {
		return next
	}
	info, err := os.Stat(next.path)
	if err != nil {
		return next
	}
	return next.advance(info.Size())
}

// located is the tail with its file's path known, and false while there is
// no file to read.
func (t Tail) located(now time.Time) (Tail, bool) {
	switch t.kind {
	case plain:
		return t, true
	case transcript:
		if t.path != "" {
			return t, true
		}
		if !t.looked.IsZero() && now.Sub(t.looked) < lookupEvery {
			return t, false
		}
		t.looked = now
		if t.find == nil {
			return t, false
		}
		path, ok := t.find()
		if !ok {
			return t, false
		}
		t.path = path
		return t, true
	}
	return t, false
}

// advance reads the file past the offset, now that it is size bytes long.
//
// The offset moves by what was read, not by the size measured: the read
// runs to the end of the file as it is by then, which is past the size if
// the reviewer wrote another line in between. Trusting the size would hand
// those bytes to the next poll a second time, as duplicate events and
// doubled counts.
func (t Tail) advance(size int64) Tail {
	if t.fromEnd {
		t.fromEnd = false
		t.offset = size
		return t
	}
	if size < t.offset {
		t.offset = 0
		t.partial = nil
	}
	if size == t.offset {
		return t
	}
	chunk, err := readFrom(t.path, t.offset)
	if err != nil {
		return t
	}
	t.offset += int64(len(chunk))
	return t.feed(chunk)
}

func readFrom(path string, offset int64) (chunk []byte, err error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}
	return io.ReadAll(file)
}

// feed takes a chunk of the file: whole lines become events, and the rest
// waits for the next chunk. Every slice it keeps is new, so the Tail the
// poll started from still holds what it held.
func (t Tail) feed(chunk []byte) Tail {
	rest := slices.Concat(t.partial, chunk)
	var fresh []Event
	for {
		line, after, ok := bytes.Cut(rest, []byte("\n"))
		if !ok {
			break
		}
		rest = after
		events := t.lineEvents(line)
		if len(events) == 0 {
			continue
		}
		t.turns++
		for _, event := range events {
			if event.Tool != "" {
				t.toolCalls++
			}
		}
		fresh = append(fresh, events...)
	}
	t.partial = bytes.Clone(rest)
	t.events = lastOf(slices.Concat(t.events, fresh), keep)
	return t
}

func (t Tail) lineEvents(line []byte) []Event {
	if t.kind == plain {
		return plainEvents(line)
	}
	return transcriptEvents(line, t.since)
}

// lastOf is the last n events, in a slice of their own so the dropped ones
// are not kept alive behind it.
func lastOf(events []Event, n int) []Event {
	if len(events) <= n {
		return events
	}
	return slices.Clone(events[len(events)-n:])
}
