package activity

import (
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestAFileIsFollowedFromWhereTheLastPollLeftOff(t *testing.T) {
	path := file(t, "t.jsonl")
	c := &clock{}
	tail := Transcript(existing(path), time.Time{}, false)
	tail = tail.Poll(c.tick())
	if len(tail.Events()) != 0 || tail.Source() != "waiting for its transcript" {
		t.Fatalf("before the file exists: %+v, %q", tail.Events(), tail.Source())
	}
	write(t, path, assistant(at, tool("Read", `{"file_path":"a.rs"}`))+"\n")
	tail = tail.Poll(c.tick())
	if len(tail.Events()) != 1 || tail.Source() != "" {
		t.Fatalf("after the first line: %+v, %q", tail.Events(), tail.Source())
	}
	tail = tail.Poll(c.tick())
	if len(tail.Events()) != 1 {
		t.Fatalf("nothing new was written, yet the events are %+v", tail.Events())
	}
	appendTo(t, path, assistant(at, tool("Grep", `{"pattern":"x"}`))+"\n")
	tail = tail.Poll(c.tick())
	if got := tail.Events(); len(got) != 2 || tail.CurrentTool() != "Grep" {
		t.Errorf("after an append: %+v", got)
	}
}

func TestALineSplitAcrossTwoPollsIsReadWhole(t *testing.T) {
	path := file(t, "t.jsonl")
	line := assistant(at, tool("Read", `{"file_path":"a.rs"}`))
	write(t, path, line[:20])
	c := &clock{}
	tail := Transcript(existing(path), time.Time{}, false).Poll(c.tick())
	if len(tail.Events()) != 0 {
		t.Fatalf("half a line read as %+v", tail.Events())
	}
	appendTo(t, path, line[20:]+"\n")
	tail = tail.Poll(c.tick())
	if len(tail.Events()) != 1 || tail.CurrentTool() != "Read" || tail.Turns() != 1 || tail.ToolCalls() != 1 {
		t.Errorf("got %+v, %d turns, %d tool calls", tail.Events(), tail.Turns(), tail.ToolCalls())
	}
}

func TestAHalfWrittenCharacterWaitsAndIsReadOnceWhole(t *testing.T) {
	path := file(t, "t.jsonl")
	first := assistant(at, tool("Read", `{"file_path":"a.rs"}`)) + "\n"
	write(t, path, first)
	c := &clock{}
	tail := Transcript(existing(path), time.Time{}, false).Poll(c.tick())
	more := assistant(at, tool("Grep", `{"pattern":"…x"}`)) + "\n"
	split := strings.Index(more, "…") + 1
	appendTo(t, path, more[:split])
	tail = tail.Poll(c.tick())
	if len(tail.Events()) != 1 {
		t.Fatalf("an event was made from half a line: %+v", tail.Events())
	}
	appendTo(t, path, more[split:])
	tail = tail.Poll(c.tick())
	got := tail.Events()
	if len(got) != 2 || tail.Turns() != 2 || got[1].Summary != "…x" {
		t.Errorf("got %+v and %d turns, want the second line once with its character whole", got, tail.Turns())
	}
}

func TestAnInvalidByteDoesNotStopTheTail(t *testing.T) {
	path := file(t, "pr-9.log")
	cases := []struct {
		why, text string
		want      []string
	}{
		{"a byte no character starts with", "a\xffb\n", []string{"a\uFFFDb"}},
		{"a start no byte can finish", "c\xe0\x80\n", []string{"c\uFFFD\uFFFD"}},
		{"and the lines after it", "next\n", []string{"next"}},
	}
	tail := Plain(path, time.Time{})
	var want []string
	for _, c := range cases {
		appendTo(t, path, c.text)
		tail = tail.Poll(time.Time{})
		want = append(want, c.want...)
		if got := summaries(tail.Events()); !slices.Equal(got, want) {
			t.Errorf("%s: got %q, want %q", c.why, got, want)
		}
	}
}

func TestTheTailKeepsTheLastEventsOnly(t *testing.T) {
	path := file(t, "t.jsonl")
	var b strings.Builder
	for i := range keep + 3 {
		b.WriteString(assistant(at, tool("Bash", `{"command":"step `+strconv.Itoa(i)+`"}`)) + "\n")
	}
	write(t, path, b.String())
	tail := Transcript(existing(path), time.Time{}, false).Poll(time.Time{})
	got := tail.Events()
	if len(got) != keep || got[len(got)-1].Summary != "step "+strconv.Itoa(keep+2) {
		t.Errorf("kept %d events ending %q", len(got), got[len(got)-1].Summary)
	}
	if tail.ToolCalls() != keep+3 || tail.Turns() != keep+3 {
		t.Errorf("counted %d tool calls and %d turns, want %d", tail.ToolCalls(), tail.Turns(), keep+3)
	}
}

func TestTextAfterAToolCallEndsIt(t *testing.T) {
	path := file(t, "t.jsonl")
	write(t, path, assistant(at, tool("Bash", `{"command":"ls"}`))+"\n")
	c := &clock{}
	tail := Transcript(existing(path), time.Time{}, false).Poll(c.tick())
	if tail.CurrentTool() != "Bash" {
		t.Fatalf("current tool is %q", tail.CurrentTool())
	}
	appendTo(t, path, assistant(at, `{"type":"text","text":"done"}`)+"\n")
	if tail = tail.Poll(c.tick()); tail.CurrentTool() != "" {
		t.Errorf("current tool is %q after text", tail.CurrentTool())
	}
	if (Tail{}).CurrentTool() != "" {
		t.Error("an empty tail is in a tool")
	}
}

func TestAResumedTranscriptIsFollowedFromItsEnd(t *testing.T) {
	path := file(t, "t.jsonl")
	old := assistant("2026-09-01T10:00:00.000Z", tool("Read", `{"file_path":"old.rs"}`))
	write(t, path, old+"\n"+old+"\n")
	c := &clock{}
	tail := Transcript(existing(path), time.Time{}, true).Poll(c.tick())
	if len(tail.Events()) != 0 || tail.Source() != "" {
		t.Fatalf("history was read: %+v, %q", tail.Events(), tail.Source())
	}
	appendTo(t, path, assistant(at, tool("Grep", `{"pattern":"new"}`))+"\n")
	tail = tail.Poll(c.tick())
	if len(tail.Events()) != 1 || tail.CurrentTool() != "Grep" {
		t.Errorf("got %+v", tail.Events())
	}
}

func TestAFileThatGotShorterIsReadFromItsStart(t *testing.T) {
	path := file(t, "pr-9.log")
	write(t, path, "a long first line\nand a partial")
	tail := Plain(path, time.Time{}).Poll(time.Time{})
	write(t, path, "fresh\n")
	tail = tail.Poll(time.Time{})
	want := []string{"a long first line", "fresh"}
	if got := summaries(tail.Events()); !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAPlainLogIsOneEventPerLine(t *testing.T) {
	path := file(t, "pr-9.log")
	tail := Plain(path, time.Time{}).Poll(time.Time{})
	if len(tail.Events()) != 0 {
		t.Fatalf("a missing log read as %+v", tail.Events())
	}
	write(t, path, "checking out\n\nrunning the suite\n")
	tail = tail.Poll(time.Time{})
	want := []string{"checking out", "running the suite"}
	if got := summaries(tail.Events()); !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	if tail.CurrentTool() != "" || tail.ToolCalls() != 0 || tail.Turns() != 2 {
		t.Errorf("a plain log counted %d tool calls and %d turns", tail.ToolCalls(), tail.Turns())
	}
}

func TestATranscriptIsLookedForAtMostOnceASecond(t *testing.T) {
	path := file(t, "t.jsonl")
	calls := 0
	find := func() (string, bool) {
		calls++
		return existing(path)()
	}
	start := time.Date(2026, 9, 1, 11, 0, 0, 0, time.UTC)
	steps := []struct {
		after     time.Duration
		writeNow  bool
		wantCalls int
	}{
		{0, false, 1},
		{500 * time.Millisecond, false, 1},
		{999 * time.Millisecond, false, 1},
		{time.Second, true, 2},
		{1500 * time.Millisecond, false, 2},
		{5 * time.Second, false, 2},
	}
	tail := Transcript(find, time.Time{}, false)
	for _, s := range steps {
		if s.writeNow {
			write(t, path, assistant(at, tool("Read", `{}`))+"\n")
		}
		tail = tail.Poll(start.Add(s.after))
		if calls != s.wantCalls {
			t.Errorf("at +%v the transcript was looked for %d times, want %d", s.after, calls, s.wantCalls)
		}
	}
	if len(tail.Events()) != 1 {
		t.Errorf("the file found was not read: %+v", tail.Events())
	}
}

func TestATranscriptWithNoWayToFindItStaysWaiting(t *testing.T) {
	tail := Transcript(nil, time.Time{}, false).Poll(time.Now())
	if tail.Source() != "waiting for its transcript" || len(tail.Events()) != 0 {
		t.Errorf("got %q, %+v", tail.Source(), tail.Events())
	}
}

func TestAFoundTranscriptThatVanishesReadsAsNothingNew(t *testing.T) {
	path := file(t, "t.jsonl")
	tail := Transcript(func() (string, bool) { return path, true }, time.Time{}, false).Poll(time.Time{})
	if len(tail.Events()) != 0 || tail.Source() != "waiting for its transcript" {
		t.Errorf("got %q, %+v", tail.Source(), tail.Events())
	}
}

func TestPollLeavesTheTailItWasCalledOnAlone(t *testing.T) {
	path := file(t, "pr-9.log")
	write(t, path, "one\n")
	before := Plain(path, time.Time{}).Poll(time.Time{})
	appendTo(t, path, "two\nthr")
	after := before.Poll(time.Time{})
	if got := summaries(before.Events()); !slices.Equal(got, []string{"one"}) || before.Turns() != 1 {
		t.Errorf("the earlier tail changed to %q, %d turns", got, before.Turns())
	}
	if got := summaries(after.Events()); !slices.Equal(got, []string{"one", "two"}) {
		t.Errorf("the next tail holds %q", got)
	}
	events := after.Events()
	events[0].Summary = "changed"
	if after.Events()[0].Summary != "one" {
		t.Error("changing what Events returned changed the tail")
	}
}

func TestASilentTailSaysWhyAndReadsNothing(t *testing.T) {
	cases := []struct {
		name        string
		tail        Tail
		source, why string
	}{
		{"silent", Silent("the reviewer is not claude"), "", "the reviewer is not claude"},
		{"zero", Tail{}, "", ""},
		{"plain", Plain("/x", time.Time{}), "following the reviewer's stderr", ""},
		{"transcript", Transcript(nil, time.Time{}, false), "waiting for its transcript", ""},
	}
	for _, c := range cases {
		polled := c.tail.Poll(time.Now())
		if polled.Source() != c.source || polled.Why() != c.why || len(polled.Events()) != 0 {
			t.Errorf("%s: source %q, why %q, events %+v", c.name, polled.Source(), polled.Why(), polled.Events())
		}
	}
}
