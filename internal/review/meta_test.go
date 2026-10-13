package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/venabots/autoreview/internal/session"
)

// fileWith writes one file into a directory of its own and returns its path.
func fileWith(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const ranIn = "80e25f6a-45b7-5246-a9e2-8feda1021531"

func TestReadMetaTakesWhatASummaryNeeds(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    Meta
		ok      bool
	}{
		{
			"the envelope dash-p writes",
			`{"session_id":"` + ranIn + `","total_cost_usd":0.42,"model_resolved":"claude-fable-5","exit_status":"ok","turns":31}`,
			Meta{SessionID: ranIn, CostUSD: 0.42, Model: "claude-fable-5"},
			true,
		},
		// A codex review reports no cost and no session.
		{"an envelope with nothing in it", `{}`, Meta{}, true},
		{"a null is nothing", `{"session_id":null,"model_resolved":null}`, Meta{}, true},
		{"not JSON", `dash-p: killed`, Meta{}, false},
		{"a torn file", `{"session_id":"80e2`, Meta{}, false},
		{"a field of the wrong type", `{"total_cost_usd":"free"}`, Meta{}, false},
		{"an empty file", ``, Meta{}, false},
		{
			// The model name is drawn on the terminal.
			"a model name cannot draw on the screen",
			`{"model_resolved":"claude\u001b[31m-fable\u202e-5"}`,
			Meta{Model: "claude[31m-fable-5"},
			true,
		},
		{
			"a model name is cut",
			`{"model_resolved":"` + strings.Repeat("m", 300) + `"}`,
			Meta{Model: strings.Repeat("m", 80)},
			true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := ReadMeta(fileWith(t, c.content))
			if got != c.want || ok != c.ok {
				t.Errorf("ReadMeta = %+v, %v; want %+v, %v", got, ok, c.want, c.ok)
			}
		})
	}
}

func TestReadMetaIsNothingWithoutAFile(t *testing.T) {
	// A review killed before dash-p wrote the file has nothing to report.
	if got, ok := ReadMeta(filepath.Join(t.TempDir(), "pr-9.meta.json")); ok {
		t.Errorf("ReadMeta read %+v from a file that does not exist", got)
	}
}

func TestTheSessionASummaryShows(t *testing.T) {
	pinned := session.Plan{ID: sessionID, Flag: session.FlagPin}
	resumed := session.Plan{ID: sessionID, Flag: session.FlagResume}
	noFlag := session.Plan{ID: sessionID}
	cases := []struct {
		name     string
		plan     session.Plan
		meta     *Meta
		override bool
		want     string
	}{
		// The id in the meta file names the session the review ran in.
		{"the meta file's id wins", pinned, &Meta{SessionID: ranIn, CostUSD: 0.42}, false, ranIn},
		{"a review that sent no flag learns its id from the meta file", noFlag, &Meta{SessionID: ranIn}, false, ranIn},
		{"an empty id in the meta file keeps the pinned id", pinned, &Meta{}, false, sessionID},
		{"no meta file keeps the resumed id", resumed, nil, false, sessionID},
		// No flag was sent and nothing usable came back: nothing to promise.
		{"no flag and no id", noFlag, &Meta{}, false, ""},
		{"no flag and no meta file", noFlag, nil, false, ""},
		{"codex starts with no plan", session.Plan{}, nil, false, ""},
		// An override handles its own sessions.
		{"an override shows nothing", pinned, &Meta{SessionID: ranIn}, true, ""},
		// The id would go onto a command line, and the meta file is
		// another program's text.
		{"an id that is not UUID-shaped is not used", noFlag, &Meta{SessionID: "thread_abc123"}, false, ""},
		{"an id that is a command is not used", pinned, &Meta{SessionID: "$(rm -rf ~)-5cba-5d44-ae67-9c390cfe7"}, false, sessionID},
		{"a planned id that is not UUID-shaped was never sent", session.Plan{ID: "nonsense", Flag: session.FlagPin}, nil, false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := SummarySession(c.plan, c.meta, c.override)
			if got != c.want || ok != (c.want != "") {
				t.Errorf("SummarySession = %q, %v; want %q", got, ok, c.want)
			}
		})
	}
}
