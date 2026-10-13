package ledger

import "testing"

func envOf(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

func TestThePathIsTheOverrideThenXDGThenHome(t *testing.T) {
	cases := []struct {
		name   string
		env    map[string]string
		want   string
		wantOK bool
	}{
		{
			name:   "the override wins",
			env:    map[string]string{"AUTOREVIEW_LEDGER": "/x/l.jsonl", "XDG_STATE_HOME": "/s", "HOME": "/h"},
			want:   "/x/l.jsonl",
			wantOK: true,
		},
		{
			name:   "then the XDG state directory",
			env:    map[string]string{"XDG_STATE_HOME": "/s", "HOME": "/h"},
			want:   "/s/autoreview/ledger.jsonl",
			wantOK: true,
		},
		{
			name:   "then the home directory",
			env:    map[string]string{"HOME": "/h"},
			want:   "/h/.local/state/autoreview/ledger.jsonl",
			wantOK: true,
		},
		{
			name:   "an empty override counts as unset",
			env:    map[string]string{"AUTOREVIEW_LEDGER": "", "HOME": "/h"},
			want:   "/h/.local/state/autoreview/ledger.jsonl",
			wantOK: true,
		},
		{
			name:   "an empty XDG directory counts as unset",
			env:    map[string]string{"XDG_STATE_HOME": "", "HOME": "/h"},
			want:   "/h/.local/state/autoreview/ledger.jsonl",
			wantOK: true,
		},
		{
			name:   "a directory that ends in a slash gets no second one",
			env:    map[string]string{"XDG_STATE_HOME": "/s/"},
			want:   "/s/autoreview/ledger.jsonl",
			wantOK: true,
		},
		{
			name:   "a relative override is kept as it was given",
			env:    map[string]string{"AUTOREVIEW_LEDGER": "./l.jsonl"},
			want:   "./l.jsonl",
			wantOK: true,
		},
		{name: "off records nothing", env: map[string]string{"AUTOREVIEW_LEDGER": "off", "HOME": "/h"}},
		{name: "off in capitals records nothing", env: map[string]string{"AUTOREVIEW_LEDGER": "OFF", "HOME": "/h"}},
		{name: "off in mixed case records nothing", env: map[string]string{"AUTOREVIEW_LEDGER": "Off", "HOME": "/h"}},
		{name: "no home is nowhere to put it", env: map[string]string{}},
		{name: "an empty home is nowhere to put it", env: map[string]string{"HOME": ""}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := Path(envOf(c.env))
			if got != c.want || ok != c.wantOK {
				t.Errorf("Path() = %q, %v, want %q, %v", got, ok, c.want, c.wantOK)
			}
		})
	}
}
