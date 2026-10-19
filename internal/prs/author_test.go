package prs

import (
	"maps"
	"slices"
	"testing"
)

// The unit fixture: #9 alice, #8 bob, #6 carol, #5 dave (a draft), #4 me,
// #3 dependabot[bot] and #2 erin (approved).
func TestAuthorKeepsOnlyThePRsThoseLoginsOpened(t *testing.T) {
	cases := []struct {
		name string
		opts Options
		want []int
	}{
		{"one author", Options{Me: "me", Authors: []string{"bob"}}, []int{8}},
		{"two authors", Options{Me: "me", Authors: []string{"alice", "carol"}}, []int{9, 6}},
		{"a login in another case", Options{Me: "me", Authors: []string{"BOB"}}, []int{8}},
		{"an author with nothing open", Options{Me: "me", Authors: []string{"zed"}}, []int{}},
		{"an approved PR still needs --all", Options{Me: "me", Authors: []string{"erin"}}, []int{}},
		{"an approved PR with --all", Options{Me: "me", All: true, Authors: []string{"erin"}}, []int{2}},
		{"a bot still needs --dependabot", Options{Me: "me", Authors: []string{"dependabot"}}, []int{}},
		{"a bot by its GraphQL login", Options{Me: "me", Dependabot: true, Authors: []string{"dependabot"}}, []int{3}},
		{"a bot by its REST login", Options{Me: "me", Dependabot: true, Authors: []string{"dependabot[bot]"}}, []int{3}},
		{"a draft stays out", Options{Me: "me", Authors: []string{"dave"}}, []int{}},
		{"your own PR stays out", Options{Me: "me", Authors: []string{"me"}}, []int{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := numbers(classify(fixture(t), "main", c.opts).Reviewable())
			if !slices.Equal(got, c.want) {
				t.Errorf("reviewable = %v, want %v", got, c.want)
			}
		})
	}
}

// --author comes after the draft and own filters and before the two that a
// flag lifts. So a draft or your own PR keeps its reason, and --all or
// --dependabot never uncovers a PR by someone --author leaves out.
func TestAPRByAnotherAuthorStaysInTheListWithItsReason(t *testing.T) {
	opts := Options{Me: "me", All: true, Dependabot: true, Authors: []string{"alice"}}
	found := classify(fixture(t), "main", opts)
	got := map[int]Hidden{}
	for _, p := range found.PRs {
		got[p.Number] = p.Hidden
	}
	want := map[int]Hidden{
		9: NotHidden, 8: HiddenAuthor, 6: HiddenAuthor,
		5: HiddenDraft, 4: HiddenOwn, 3: HiddenAuthor, 2: HiddenAuthor,
	}
	if !maps.Equal(got, want) {
		t.Errorf("reasons = %v, want %v", got, want)
	}
}

// A deleted account has an empty login. Its PRs are not anyone's that
// --author can name.
func TestADeletedAccountsPRIsNotByAnyAuthor(t *testing.T) {
	nodes := change(fixture(t), 9, func(n node) node {
		n.Author.Login = ""
		return n
	})
	opts := Options{Me: "me", Authors: []string{"alice", "ghost"}}
	if got := numbers(classify(nodes, "main", opts).Reviewable()); len(got) != 0 {
		t.Errorf("reviewable = %v, want none", got)
	}
}

func TestHiddenAuthorNamesItself(t *testing.T) {
	if got := HiddenAuthor.String(); got != "other author" {
		t.Errorf("HiddenAuthor prints as %q", got)
	}
}
