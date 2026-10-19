package cli

import (
	"slices"
	"strings"
	"testing"
)

func TestAuthorTakesOneOrMoreLoginsWithOrWithoutAnAt(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want []string
	}{
		{"no flag keeps every author", nil, nil},
		{"one login", []string{"--author=alice"}, []string{"alice"}},
		{"a login with an at", []string{"--author=@alice"}, []string{"alice"}},
		{"a login after a space", []string{"--author", "alice"}, []string{"alice"}},
		{"logins split by commas", []string{"--author=alice, @bob"}, []string{"alice", "bob"}},
		{"the flag given twice", []string{"--author=alice", "--author=bob"}, []string{"alice", "bob"}},
		{"one login in two cases counts once", []string{"--author=Alice,alice"}, []string{"Alice"}},
		{"an app's login", []string{"--author=dependabot[bot]"}, []string{"dependabot[bot]"}},
		{"an enterprise managed login", []string{"--author=mona_acme"}, []string{"mona_acme"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parse(t, c.args...).Authors; !slices.Equal(got, c.want) {
				t.Errorf("Authors = %q, want %q", got, c.want)
			}
		})
	}
}

func TestAuthorRefusesWhatIsNotALogin(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"an empty value", []string{"--author="}, `--author: "" is not a GitHub login`},
		{"an empty login between commas", []string{"--author=alice,,bob"}, `--author: "" is not a GitHub login`},
		{"a lone at", []string{"--author=@"}, `--author: "@" is not a GitHub login`},
		{"a path", []string{"--author=alice/x"}, `--author: "alice/x" is not a GitHub login`},
		{"a leading hyphen", []string{"--author=-alice"}, `--author: "-alice" is not a GitHub login`},
		{"a flag where the login goes", []string{"--author", "--once"}, "--author expects a value, but found the flag --once"},
		{"no value at all", []string{"--author"}, "flag needs an argument: --author"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse(c.args, noEnv)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("Parse(%q) = %v, want an error with %q", c.args, err, c.want)
			}
		})
	}
}

func TestCheckAuthorsRefusesARunThatNamesOnlyYou(t *testing.T) {
	cases := []struct {
		name    string
		authors []string
		refused bool
	}{
		{"every author", nil, false},
		{"someone else", []string{"alice"}, false},
		{"you and someone else", []string{"me", "alice"}, false},
		{"only you", []string{"me"}, true},
		{"only you, in another case", []string{"Me"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := CheckAuthors(c.authors, "me")
			if (err != nil) != c.refused {
				t.Fatalf("CheckAuthors(%q) = %v, want refused %t", c.authors, err, c.refused)
			}
			if err != nil && !strings.Contains(err.Error(), "your own PRs are never reviewed") {
				t.Errorf("the error does not say why: %v", err)
			}
		})
	}
}

func TestTheHelpNamesTheAuthorFlag(t *testing.T) {
	if !strings.Contains(Help, "--author LOGIN      Review only the PRs that LOGIN opened.") {
		t.Error("the help does not name --author")
	}
}
