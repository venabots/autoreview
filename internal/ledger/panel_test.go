package ledger_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/venabots/autoreview/internal/ledger"
)

// v1's "a panel run records what the run saw": every panelist that started
// is in the record, and "unknown" is no model.
func TestAPanelRunIsRecordedAsV1RecordsIt(t *testing.T) {
	findings := uint64(2)
	exit := 1
	p := ledger.Panel{
		Repo:       "acme/widgets",
		PID:        7,
		StartedAt:  time.Unix(100, 0),
		FinishedAt: time.Unix(140, 0),
		Risk:       "HIGH",
		SynthModel: "opus-5",
		Panel: []ledger.Panelled{
			{Name: "codex", Model: "gpt-5.5", Answered: true, Findings: &findings, Top: "HIGH", Elapsed: 40 * time.Second},
			{Name: "claude", Model: "unknown", Elapsed: 3 * time.Second, Exit: &exit},
		},
		Reviewed: true,
	}
	line, err := ledger.ForPanel(p).MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	want := `{"v":1,"id":"panel:100:7","at":140,"source":"panel","repo":"acme/widgets","risk":"HIGH","duration_secs":40,"driver_model":"opus-5",` +
		`"panel":[{"name":"codex","model":"gpt-5.5","ok":true,"findings":2,"top":"HIGH","duration_secs":40},` +
		`{"name":"claude","ok":false,"duration_secs":3,"exit_code":1}],"findings":[],"reviewed":true}`
	if string(line) != want {
		t.Errorf("record:\n%s\nwant:\n%s", line, want)
	}

	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	if err := ledger.Append(path, ledger.ForPanel(p)); err != nil {
		t.Fatal(err)
	}
	back, err := ledger.Read(path)
	if err != nil || len(back) != 1 || back[0].Source != "panel" {
		t.Errorf("read back %+v, %v", back, err)
	}
}
