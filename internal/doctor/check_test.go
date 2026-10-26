package doctor

import (
	"context"
	"slices"
	"testing"
)

func checkWith(t *testing.T, d Deps) Summary {
	t.Helper()
	d.Now = noon
	if d.Env == nil {
		d.Env = env("")
	}
	if d.Has == nil {
		d.Has = hasOnly("claude")
	}
	if d.Version == nil {
		d.Version = installed("gh", "dash-p", "git", "claude")
	}
	if d.Probe == nil {
		d.Probe = answers(nil)
	}
	return Diagnose(context.Background(), d)
}

// The view shows what the doctor would say, so nobody has to run it to
// learn the panel is down. Diagnose finds what Run reports.
func TestDiagnoseFindsWhatTheDoctorWouldSay(t *testing.T) {
	healthy := writeLedger(t, review(noon.Unix()-60, 1, "approved", claudeOK, grokOK, glmOK))
	stalled := writeLedger(t, review(noon.Unix()-3600, 9, "commented", claudeOK, grokDown))
	four := "claude:opus codex opencode:xai/grok-4.7 opencode:zai-coding-plan/glm-5.3"
	cases := []struct {
		name string
		d    Deps
		want Summary
	}{
		{"all clear", Deps{Env: ledgerAnd(healthy, four)},
			Summary{Seated: 4, Answering: 4}},
		{"one of four down still approves", Deps{Env: ledgerAnd(healthy, four), Probe: answers(map[string]string{"codex": "usage limit"})},
			Summary{Seated: 4, Answering: 3, Problems: []string{"codex does not answer"}}},
		{"two of four down blocks", Deps{Env: ledgerAnd(healthy, four), Probe: answers(map[string]string{"codex": "x", "opencode:xai/grok-4.7": "y"})},
			Summary{Seated: 4, Answering: 2, Problems: []string{"codex does not answer", "opencode:xai/grok-4.7 does not answer"}, Blocking: true}},
		{"a PR with no verdict", Deps{Env: ledgerAnd(stalled, four)},
			Summary{Seated: 4, Answering: 4, Problems: []string{"1 PR got no verdict"}}},
		{"dash-p is missing", Deps{Env: ledgerAnd(healthy, four), Version: installed("gh", "claude")},
			Summary{Seated: 4, Answering: 4, Problems: []string{"dash-p is not installed"}, Blocking: true}},
	}
	for _, c := range cases {
		got := checkWith(t, c.d)
		if got.Seated != c.want.Seated || got.Answering != c.want.Answering || got.Blocking != c.want.Blocking || !slices.Equal(got.Problems, c.want.Problems) {
			t.Errorf("%s: Diagnose = %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestAPanelThatCannotBeReadBlocks(t *testing.T) {
	got := checkWith(t, Deps{Env: env("nobody:x")})
	if !got.Blocking || len(got.Problems) != 1 {
		t.Errorf("Diagnose = %+v, want one problem that blocks", got)
	}
}

// ledgerAnd is an environment with a ledger and a panel.
func ledgerAnd(path, panelists string) func(string) string {
	return func(k string) string {
		switch k {
		case "AUTOREVIEW_LEDGER":
			return path
		case "PANEL_REVIEW_PANELISTS":
			return panelists
		}
		return ""
	}
}
