package plain

import (
	"strings"
	"testing"
)

func TestTheReviewListNamesTheAuthorsItKeeps(t *testing.T) {
	s := snapshot()
	s.Authors = []string{"alice", "bob"}
	if out, _ := lists(t, s); !strings.Contains(out, "\nreview list: 2 PRs by @alice or @bob\n") {
		t.Errorf("Lists printed\n%s\nwant a header that names the authors", out)
	}
}
