package words

// TaskAdvice is what a task's report asks of the person, in the words a
// row and a log line use, or "" when it asks nothing. outcome is one of
// the outcomes review.TaskTrailerInstruction names.
func TaskAdvice(outcome string) string {
	switch outcome {
	case "should-close":
		return "should close"
	case "needs-person":
		return "needs you"
	}
	return ""
}
