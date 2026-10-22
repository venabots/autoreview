package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/venabots/autoreview/internal/words"
)

func TestATaskTrailerIsReadFromTheEndOfTheReply(t *testing.T) {
	cases := []struct {
		name  string
		reply string
		want  TaskTrailer
		found bool
	}{
		{"a should-close block", "I stopped.\n\n```autoreview-task\n{\"outcome\":\"should-close\",\"why\":\"main already has this change, from #2943\"}\n```",
			TaskTrailer{Outcome: TaskShouldClose, Why: "main already has this change, from #2943"}, true},
		{"the last block wins", "```autoreview-task\n{\"outcome\":\"pushed\"}\n```\nthen\n```autoreview-task\n{\"outcome\":\"needs-person\",\"why\":\"pick a migration\"}\n```",
			TaskTrailer{Outcome: TaskNeedsPerson, Why: "pick a migration"}, true},
		{"an outcome it does not know reads as none", "```autoreview-task\n{\"outcome\":\"close-it-now\",\"why\":\"x\"}\n```",
			TaskTrailer{Why: "x"}, true},
		{"a review trailer is not a task trailer", "```autoreview\n{\"decision\":\"approved\"}\n```", TaskTrailer{}, false},
		{"no block", "I pushed the merge.", TaskTrailer{}, false},
		{"a block that does not parse", "```autoreview-task\nnot json\n```", TaskTrailer{}, false},
	}
	for _, c := range cases {
		got, found := ParseTaskTrailer(c.reply)
		if got != c.want || found != c.found {
			t.Errorf("%s: ParseTaskTrailer = %+v, %v; want %+v, %v", c.name, got, found, c.want, c.found)
		}
	}
}

// The why is agent output that reaches the terminal: it is made safe and
// cut before anything draws it.
func TestATaskTrailersWhyIsSafeToDraw(t *testing.T) {
	reply := "```autoreview-task\n{\"outcome\":\"should-close\",\"why\":\"bad \\u001b[2J" + strings.Repeat("x", 300) + "\"}\n```"
	got, _ := ParseTaskTrailer(reply)
	if strings.ContainsRune(got.Why, '\x1b') || len([]rune(got.Why)) > maxGistChars {
		t.Errorf("why = %q", got.Why)
	}
}

func TestATaskTrailerIsReadFromDashpsEnvelope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pr-4.json")
	envelope := `{"answer":"I stopped.\n\n` + "```autoreview-task\\n{\\\"outcome\\\":\\\"should-close\\\",\\\"why\\\":\\\"superseded\\\"}\\n```" + `"}`
	if err := os.WriteFile(path, []byte(envelope), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, ok := ReadTaskTrailer(path); !ok || got.Outcome != TaskShouldClose || got.Why != "superseded" {
		t.Errorf("ReadTaskTrailer = %+v, %v", got, ok)
	}
	if _, ok := ReadTaskTrailer(filepath.Join(t.TempDir(), "missing.json")); ok {
		t.Error("a missing stdout read as a trailer")
	}
}

func TestEveryOutcomeTheInstructionNamesIsOneTheRunReads(t *testing.T) {
	for _, o := range []string{TaskPushed, TaskNothingNeeded, TaskShouldClose, TaskNeedsPerson} {
		if !strings.Contains(TaskTrailerInstruction, o) {
			t.Errorf("the instruction does not name %s", o)
		}
	}
	if strings.Contains(TaskTrailerInstruction, "\n") {
		t.Error("the instruction has a newline; it travels as one argv token")
	}
	if words.TaskAdvice(TaskShouldClose) != "should close" || words.TaskAdvice(TaskNeedsPerson) != "needs you" {
		t.Error("the words for the outcomes a person acts on changed")
	}
	if words.TaskAdvice(TaskPushed) != "" || words.TaskAdvice(TaskNothingNeeded) != "" {
		t.Error("an outcome that needs nothing from a person has a word")
	}
}
