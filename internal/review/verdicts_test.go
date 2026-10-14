package review

import (
	"strings"
	"testing"
)

func TestTheVerdictLevelsReadAsTheSettingsFileWritesThem(t *testing.T) {
	for raw, want := range map[string]Verdicts{
		"approve-and-request-changes": VerdictsAll,
		"approve-only":                VerdictsApproveOnly,
		"comment-only":                VerdictsCommentOnly,
	} {
		got, err := ParseVerdicts(raw)
		if err != nil || got != want || got.String() != raw {
			t.Errorf("ParseVerdicts(%q) = %v, %v; String %q", raw, got, err, got.String())
		}
	}
	_, err := ParseVerdicts("never")
	if err == nil || !strings.Contains(err.Error(), "approve-and-request-changes, approve-only or comment-only") {
		t.Errorf("ParseVerdicts(never) = %v, want the three levels named", err)
	}
}

// The option reaches the two skills that submit a verdict, and no other.
// The default sends nothing, so the prompt stays what it was before the
// setting existed.
func TestTheVerdictLevelIsAnOptionOfTheSkillsThatGiveAVerdict(t *testing.T) {
	cases := []struct {
		name string
		r    Request
		want string
	}{
		{"the default", with(func(r *Request) { r.Verdicts = VerdictsAll }), "/auto-review 9"},
		{"approve only", with(func(r *Request) { r.Verdicts = VerdictsApproveOnly }), "/auto-review 9 --verdicts approve-only"},
		{"comment only, with a focus", with(func(r *Request) { r.Verdicts, r.Focus = VerdictsCommentOnly, "the migration" }),
			`/auto-review 9 --verdicts comment-only --focus "the migration"`},
		{"under codex", with(func(r *Request) { r.Orchestrator, r.Verdicts = codex(), VerdictsCommentOnly }), "$auto-review 9 --verdicts comment-only"},
		{"no post", with(func(r *Request) { r.NoPost, r.Verdicts = true, VerdictsCommentOnly }), "/panel-review 9"},
		{"a task", with(func(r *Request) { r.Task, r.Verdicts = "babysit-pr", VerdictsCommentOnly }), "/babysit-pr 9"},
	}
	for _, c := range cases {
		if got := Prompt(c.r); got != c.want {
			t.Errorf("%s: Prompt = %q, want %q", c.name, got, c.want)
		}
	}
}
