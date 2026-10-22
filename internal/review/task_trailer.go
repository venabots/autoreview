package review

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/venabots/autoreview/internal/sanitize"
)

// TaskTrailerInstruction asks a task on one of your PRs for its trailer. A
// task can end with nothing pushed for reasons only the agent knows: the
// base already has the change, or a person must choose. Without the
// trailer the view said "nothing pushed" and the PR looked forgotten. It
// is one line, as TrailerInstruction is.
const TaskTrailerInstruction = "When your reply concludes this task, end it with a fenced code block tagged autoreview-task containing exactly one JSON object shaped like {\"outcome\":\"pushed|nothing-needed|should-close|needs-person\",\"why\":\"main already has this change, from #2943\"}. outcome: pushed = you pushed to the PR; nothing-needed = the PR needed nothing from this task; should-close = the PR should be closed and not merged, for example because its base branch already has its change; needs-person = a person must decide something before the PR can go on. why is one short sentence that a person reads in a list. No prose inside the block."

// The outcomes a task reports.
const (
	TaskPushed        = "pushed"
	TaskNothingNeeded = "nothing-needed"
	TaskShouldClose   = "should-close"
	TaskNeedsPerson   = "needs-person"
)

var taskOutcomes = []string{TaskPushed, TaskNothingNeeded, TaskShouldClose, TaskNeedsPerson}

const taskTrailerTag = fence + "autoreview-task"

// TaskTrailer is a task's own report of how it ended. As with Trailer, it
// is read leniently and never trusted for what GitHub can say: whether the
// task pushed is read back from GitHub. An outcome the run does not know
// reads as "".
type TaskTrailer struct {
	Outcome string `json:"outcome"`
	Why     string `json:"why"`
}

// ParseTaskTrailer is the task trailer in a reply, or false when it has
// none. The last block that parses wins, as in ParseTrailer. Why is safe
// to draw.
func ParseTaskTrailer(text string) (TaskTrailer, bool) {
	_, _, t, ok := lastFenced(text, taskTrailerTag, decodeTaskTrailer)
	if !ok {
		return TaskTrailer{}, false
	}
	if !slices.Contains(taskOutcomes, t.Outcome) {
		t.Outcome = ""
	}
	return TaskTrailer{Outcome: t.Outcome, Why: capped(sanitize.Line(t.Why), maxGistChars)}, true
}

// ReadTaskTrailer is the trailer of a finished task, from dash-p's
// envelope.
func ReadTaskTrailer(stdoutPath string) (TaskTrailer, bool) {
	answer, ok := ReadAnswer(stdoutPath)
	if !ok {
		return TaskTrailer{}, false
	}
	return ParseTaskTrailer(answer)
}

func decodeTaskTrailer(body string) (TaskTrailer, bool) {
	body = strings.TrimSpace(body)
	if !strings.HasPrefix(body, "{") {
		return TaskTrailer{}, false
	}
	var t TaskTrailer
	if err := json.Unmarshal([]byte(body), &t); err != nil {
		return TaskTrailer{}, false
	}
	return t, true
}
