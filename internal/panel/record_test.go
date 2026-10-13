package panel

import (
	"strings"
	"testing"
	"time"

	"github.com/venabots/autoreview/internal/ledger"
)

func TestAPanelRunRecordsWhatTheRunSaw(t *testing.T) {
	outcomes := []Outcome{
		{ID: "codex", Model: "gpt-5.5", Exit: 0, Exited: true, Stdout: "Model: gpt-5.5\n- [LOW] a.rs:1 — nit\n- [HIGH] b.rs:2 — bug", Elapsed: 40 * time.Second},
		{ID: "claude", Model: "unknown", Exit: 1, Exited: true, Failure: "no output (exit 1)", Elapsed: 3 * time.Second},
		{ID: "opencode", Model: "unknown", Failure: "killed before it finished"},
	}
	synthesis := "### Risk\nHIGH\n### must-fix\n- [HIGH] b.rs:2 — bug. Flagged by: codex (gpt-5.5)"
	base := ledger.Panel{Repo: "acme/widgets", PID: 42, StartedAt: time.Unix(100, 0), FinishedAt: time.Unix(160, 0), SynthModel: "opus-5"}

	line, err := panelRecord(base, outcomes, synthesis).MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	want := `{"v":1,"id":"panel:100:42","at":160,"source":"panel","repo":"acme/widgets","risk":"HIGH","duration_secs":60,"driver_model":"opus-5",` +
		`"panel":[{"name":"codex","model":"gpt-5.5","ok":true,"findings":2,"top":"HIGH","duration_secs":40,"exit_code":0},` +
		`{"name":"claude","ok":false,"duration_secs":3,"exit_code":1},` +
		`{"name":"opencode","ok":false,"duration_secs":0}],` +
		`"findings":[{"severity":"HIGH","location":"b.rs:2","flagged_by":[{"name":"codex","model":"gpt-5.5"}],"count":1}],"reviewed":true}`
	if string(line) != want {
		t.Errorf("record:\n%s\nwant:\n%s", line, want)
	}
}

func TestARunWithNoSynthesisIsRecordedAsUnread(t *testing.T) {
	outcomes := []Outcome{{ID: "codex", Model: "m", Exited: true, Stdout: "Model: m\nNO_FINDINGS"}}
	r := panelRecord(ledger.Panel{Repo: "repo", StartedAt: time.Unix(1, 0), FinishedAt: time.Unix(1, 0)}, outcomes, "")
	if r.Reviewed || r.Risk != nil || len(r.Findings) != 0 || r.DriverModel != nil || *r.Panel[0].Findings != 0 {
		t.Errorf("%+v", r)
	}
}

func TestARemoteNamesTheRepository(t *testing.T) {
	cases := []struct {
		url  string
		want string
		ok   bool
	}{
		{"git@github.com:acme/widgets.git\n", "acme/widgets", true},
		{"https://github.com/acme/widgets", "acme/widgets", true},
		{"https://github.com/acme/widgets/", "acme/widgets", true},
		{"ssh://git@host:22/acme/widgets.git", "acme/widgets", true},
		{"host:widgets", "host/widgets", true},
		{"widgets", "", false},
		{"/widgets", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := slugOfRemote(c.url)
		if got != c.want || ok != c.ok {
			t.Errorf("slugOfRemote(%q) = %q, %v", c.url, got, ok)
		}
	}
}

func TestWithNoRemoteTheRepositoryIsTheDirectoryName(t *testing.T) {
	if got := repoSlug(fakeGit{}.run, "/home/me/widgets"); got != "widgets" {
		t.Errorf("repoSlug = %q", got)
	}
	git := fakeGit{"config --get remote.origin.url": "git@github.com:acme/widgets.git\n"}
	if got := repoSlug(git.run, "/tmp/worktree-x"); !strings.EqualFold(got, "acme/widgets") {
		t.Errorf("repoSlug = %q", got)
	}
}
