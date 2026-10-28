package doctor

import (
	"slices"
	"testing"
)

// dash-p 0.4 started opencode 2.x outside --cwd, so an opencode panelist
// reviewed the wrong directory and nothing failed. Homebrew cannot pin a
// dependency's version, so the doctor names a dash-p that is too old.
func TestADashpOlderThanTheOneAutoreviewNeedsIsAProblem(t *testing.T) {
	cases := []struct {
		said string
		want []string
	}{
		{"dash-p 0.4.0", []string{"dash-p 0.4.0 is older than 0.5.0, which autoreview needs"}},
		{"dash-p 0.5.0", nil},
		{"dash-p 0.10.1", nil},
		{"dash-p 1.0.0-rc.1", nil},
		{"dash-p (built from source)", nil},
	}
	for _, c := range cases {
		version := func(name string) (string, bool) {
			if name == "dash-p" {
				return c.said, true
			}
			return name + " 1.0", true
		}
		r := report{tools: toolsOf(version), probed: true}
		if got := r.problems(); !slices.Equal(got, c.want) {
			t.Errorf("%q: problems %q, want %q", c.said, got, c.want)
		}
	}
}
