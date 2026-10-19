package prs

import (
	"fmt"
	"testing"
)

// The names a failed test prints. A value that printed as a bare number
// would send the reader to the const block to decode it.
func TestEveryValueNamesItself(t *testing.T) {
	cases := []struct {
		value fmt.Stringer
		want  string
	}{
		{New, "NEW"},
		{Updated, "UPDATED"},
		{Seen, "SEEN"},
		{CINone, "none"},
		{CIPassing, "passing"},
		{CIPending, "pending"},
		{CIFailing, "failing"},
		{DecisionNone, "none"},
		{Approved, "approved"},
		{ChangesRequested, "changes requested"},
		{ReviewRequired, "review required"},
		{NotHidden, "not hidden"},
		{HiddenDraft, "draft"},
		{HiddenOwn, "own"},
		{HiddenBot, "bot"},
		{HiddenApproved, "approved"},
		{Declared, "declared"},
		{CarriesTip, "carries tip"},
		{Overlap, "overlap"},
		{Native, "GitHub stack"},
	}
	for _, c := range cases {
		if got := c.value.String(); got != c.want {
			t.Errorf("%T(%d) prints as %q, want %q", c.value, c.value, got, c.want)
		}
	}
}
