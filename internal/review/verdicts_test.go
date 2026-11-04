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
		"request-changes-only":        VerdictsRequestChangesOnly,
	} {
		got, err := ParseVerdicts(raw)
		if err != nil || got != want || got.String() != raw {
			t.Errorf("ParseVerdicts(%q) = %v, %v; String %q", raw, got, err, got.String())
		}
	}
	_, err := ParseVerdicts("never")
	if err == nil || !strings.Contains(err.Error(), "approve-and-request-changes, approve-only, comment-only or request-changes-only") {
		t.Errorf("ParseVerdicts(never) = %v, want the four levels named", err)
	}
}

// A PR held for a person still gets a request for changes on a blocking
// finding. Only the approval is held back, so each level loses its
// approval and keeps the rest.
func TestALevelWithoutTheApprovalKeepsWhatElseItAllows(t *testing.T) {
	for from, want := range map[Verdicts]Verdicts{
		VerdictsAll:                VerdictsRequestChangesOnly,
		VerdictsApproveOnly:        VerdictsCommentOnly,
		VerdictsCommentOnly:        VerdictsCommentOnly,
		VerdictsRequestChangesOnly: VerdictsRequestChangesOnly,
	} {
		if got := from.WithoutApproval(); got != want || got.Approves() {
			t.Errorf("%s without approval = %s (approves %v), want %s", from, got, got.Approves(), want)
		}
	}
	if !VerdictsRequestChangesOnly.Allows("changes requested") || VerdictsRequestChangesOnly.Allows("approved") {
		t.Error("request-changes-only must allow a request for changes and no approval")
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
