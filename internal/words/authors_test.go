package words

import "testing"

func TestAuthorsReadsAsEnglish(t *testing.T) {
	cases := []struct {
		logins []string
		want   string
	}{
		{nil, ""},
		{[]string{"alice"}, "by @alice"},
		{[]string{"alice", "bob"}, "by @alice or @bob"},
		{[]string{"alice", "bob", "carol"}, "by @alice, @bob or @carol"},
	}
	for _, c := range cases {
		if got := Authors(c.logins); got != c.want {
			t.Errorf("Authors(%q) = %q, want %q", c.logins, got, c.want)
		}
	}
}
