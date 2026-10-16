package doctor

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/venabots/autoreview/internal/ledger"
	"github.com/venabots/autoreview/internal/panel"
)

var noon = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

func writeLedger(t *testing.T, records ...ledger.Record) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	for i, r := range records {
		r.V, r.ID, r.Source = 1, "id-"+string(rune('a'+i)), "autoreview"
		if err := ledger.Append(path, r); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func installed(names ...string) func(string) (string, bool) {
	return func(n string) (string, bool) {
		for _, have := range names {
			if have == n {
				return n + " 1.0", true
			}
		}
		return "", false
	}
}

func answers(down map[string]string) func(context.Context, []panel.Spec) []Check {
	return func(_ context.Context, specs []panel.Spec) []Check {
		out := make([]Check, len(specs))
		for i, s := range specs {
			why, isDown := down[specName(s)]
			out[i] = Check{Spec: s, OK: !isDown, Elapsed: 3 * time.Second, Detail: why}
		}
		return out
	}
}

type ran struct {
	code           int
	stdout, stderr string
}

func doctorRun(t *testing.T, args []string, d Deps) ran {
	t.Helper()
	var out, errOut strings.Builder
	d.Stdout, d.Stderr, d.Now = &out, &errOut, noon
	if d.Env == nil {
		d.Env = func(string) string { return "" }
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
	code := Run(context.Background(), args, d)
	return ran{code, out.String(), errOut.String()}
}

func env(panelists string) func(string) string {
	return func(k string) string {
		if k == "PANEL_REVIEW_PANELISTS" {
			return panelists
		}
		return ""
	}
}

func TestAHealthyPanelAndLedgerIsAllClear(t *testing.T) {
	path := writeLedger(t, review(noon.Unix()-60, 1, "approved", claudeOK))
	r := doctorRun(t, []string{"--ledger", path}, Deps{})
	if r.code != ExitOK || !strings.Contains(r.stdout, "All clear.") {
		t.Errorf("exit %d, stdout:\n%s", r.code, r.stdout)
	}
	if !strings.Contains(r.stdout, "ok    claude  answered in 3s") {
		t.Errorf("the probe line is missing:\n%s", r.stdout)
	}
}

func TestADownPanelistIsAProblemWithItsReason(t *testing.T) {
	path := writeLedger(t)
	r := doctorRun(t, []string{"--ledger", path}, Deps{
		Env:   env("claude:opus opencode:xai/grok-4.7"),
		Probe: answers(map[string]string{"opencode:xai/grok-4.7": "exit 1: out of credits\x1b[2J"}),
	})
	if r.code != ExitProblem {
		t.Errorf("exit %d, want %d", r.code, ExitProblem)
	}
	for _, want := range []string{
		"Panel, asked now (from $PANEL_REVIEW_PANELISTS)",
		"DOWN  opencode:xai/grok-4.7  exit 1: out of credits[2J",
		"opencode:xai/grok-4.7 does not answer",
	} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, r.stdout)
		}
	}
	if strings.Contains(r.stdout, "\x1b") {
		t.Error("a probe's own text reached the terminal with its escape")
	}
}

func TestTheLedgerSectionNamesTheModelsAndTheStalledPRs(t *testing.T) {
	path := writeLedger(t,
		review(noon.Unix()-3*86_400, 7, "approved", claudeOK, grokOK),
		review(noon.Unix()-3600, 9, "commented", claudeOK, grokDown),
	)
	r := doctorRun(t, []string{"--ledger", path}, Deps{})
	for _, want := range []string{
		"Last 1d in the ledger",
		"1 review, $1.50 ($1.50 a review)",
		"DOWN     grok-4.7 on opencode       0 of 1 answered, last answer 10-12 12:00",
		"acme/widgets#9  commented, 1 of 2 answered, 10-15 11:00",
		"1 PR got no verdict",
	} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, r.stdout)
		}
	}
	if r.code != ExitProblem {
		t.Errorf("a stalled PR exited %d, want %d", r.code, ExitProblem)
	}
}

func TestAReviewWithNoPanelSaysSoAndNotZeroOfZero(t *testing.T) {
	path := writeLedger(t, review(noon.Unix()-60, 4, "none"))
	r := doctorRun(t, []string{"--ledger", path}, Deps{})
	if !strings.Contains(r.stdout, "acme/widgets#4  none, no panel recorded") {
		t.Errorf("stdout:\n%s", r.stdout)
	}
}

func TestADownModelCountsOnlyWhenThePanelWasNotAsked(t *testing.T) {
	path := writeLedger(t, review(noon.Unix()-60, 9, "approved", claudeOK, grokDown, glmOK))
	asked := doctorRun(t, []string{"--ledger", path}, Deps{})
	if asked.code != ExitOK {
		t.Errorf("a model the probe did not seat failed the run:\n%s", asked.stdout)
	}
	notAsked := doctorRun(t, []string{"--ledger", path, "--no-probe"}, Deps{
		Probe: func(context.Context, []panel.Spec) []Check { t.Fatal("--no-probe asked the panel"); return nil },
	})
	if notAsked.code != ExitProblem || !strings.Contains(notAsked.stdout, "grok-4.7 on opencode answered no launch") {
		t.Errorf("exit %d, stdout:\n%s", notAsked.code, notAsked.stdout)
	}
}

func TestAMissingRequiredProgramIsAProblemAndAMissingAgentIsNot(t *testing.T) {
	path := writeLedger(t)
	r := doctorRun(t, []string{"--ledger", path}, Deps{Version: installed("gh", "claude")})
	if !strings.Contains(r.stdout, "dash-p is not installed") || strings.Contains(r.stdout, "codex is not installed") {
		t.Errorf("stdout:\n%s", r.stdout)
	}
	if !strings.Contains(r.stdout, "opencode  not installed") {
		t.Errorf("the tools list does not say opencode is missing:\n%s", r.stdout)
	}
}

func TestALedgerThatCannotBeReadStillShowsTheProbe(t *testing.T) {
	r := doctorRun(t, []string{"--ledger", t.TempDir()}, Deps{})
	if r.code != ExitProblem || !strings.Contains(r.stdout, "ok    claude") || !strings.Contains(r.stdout, "could not read") {
		t.Errorf("exit %d, stdout:\n%s", r.code, r.stdout)
	}
}

func TestTheLedgerOffIsSaidAndNotRead(t *testing.T) {
	r := doctorRun(t, nil, Deps{Env: func(k string) string {
		if k == ledger.EnvLedger {
			return "off"
		}
		return ""
	}})
	if !strings.Contains(r.stdout, "the ledger is off") {
		t.Errorf("stdout:\n%s", r.stdout)
	}
}

func TestHelpAndBadArgumentsAreAnsweredBeforeAnythingRuns(t *testing.T) {
	never := Deps{Probe: func(context.Context, []panel.Spec) []Check { t.Fatal("probed"); return nil }}
	help := doctorRun(t, []string{"--help"}, never)
	if help.code != ExitOK || help.stdout != Help {
		t.Errorf("--help: exit %d, stdout %q", help.code, help.stdout)
	}
	bad := doctorRun(t, []string{"--json"}, never)
	if bad.code != ExitProblem || !strings.Contains(bad.stderr, "unknown arg: --json") || !strings.Contains(bad.stderr, "Usage:") {
		t.Errorf("--json: exit %d, stderr %q", bad.code, bad.stderr)
	}
}

func TestABadPanelIsAnErrorThatNamesIt(t *testing.T) {
	r := doctorRun(t, []string{"--ledger", writeLedger(t)}, Deps{Env: env("gemini")})
	if r.code != ExitProblem || !strings.Contains(r.stderr, "unknown panelist backend") {
		t.Errorf("exit %d, stderr %q", r.code, r.stderr)
	}
}
