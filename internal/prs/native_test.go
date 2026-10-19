package prs

import (
	"maps"
	"testing"
)

// layer puts the PR at position in GitHub's stack number.
func layer(n node, number, size, position int) node {
	n.Stack = &nativeStack{Number: number, Size: size}
	n.StackEntry = &nativeEntry{Position: position}
	return n
}

func TestAGitHubStackNamesThePRBelowEachLayer(t *testing.T) {
	cases := []struct {
		name string
		prs  []node
		want map[int]Parent
	}{
		{
			name: "each layer sits on the one below it",
			prs: []node{
				layer(pr(40, "main", "a"), 7, 3, 1),
				layer(pr(41, "pr40", "b"), 7, 3, 2),
				layer(pr(42, "pr41", "c"), 7, 3, 3),
			},
			want: map[int]Parent{
				41: {Number: 40, Evidence: Native},
				42: {Number: 41, Evidence: Native},
			},
		},
		{
			// GitHub says what the stack is. Commits the layers share do not
			// turn it into an overlap that holds the upper one.
			name: "a layer that shares commits with the one below is still a layer",
			prs: []node{
				layer(pr(40, "main", "a", "b"), 7, 2, 1),
				layer(pr(41, "main", "a", "b", "c"), 7, 2, 2),
			},
			want: map[int]Parent{41: {Number: 40, Evidence: Native}},
		},
		{
			// The PR below can be merged already, or older than the newest
			// 50. The layer still sits nowhere this list can name.
			name: "a layer whose PR below is not in the list sits on nothing named",
			prs:  []node{layer(pr(42, "pr41", "c"), 7, 3, 3)},
			want: map[int]Parent{},
		},
		{
			name: "two stacks do not mix",
			prs: []node{
				layer(pr(40, "main", "a"), 7, 2, 1),
				layer(pr(50, "main", "x"), 8, 2, 1),
				layer(pr(51, "pr50", "y"), 8, 2, 2),
			},
			want: map[int]Parent{51: {Number: 50, Evidence: Native}},
		},
		{
			// A PR outside any GitHub stack is still worked out from its
			// branches and commits, even when it overlaps a layer.
			name: "a PR outside the stack still overlaps a layer",
			prs: []node{
				layer(pr(40, "main", "a", "b"), 7, 1, 1),
				pr(45, "main", "a", "b", "z"),
			},
			want: map[int]Parent{45: {Number: 40, Evidence: CarriesTip, Shared: 2}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parents(c.prs, "main"); !maps.Equal(got, c.want) {
				t.Errorf("parents = %v, want %v", got, c.want)
			}
		})
	}
}

func TestAPRInAGitHubStackCarriesItsLayer(t *testing.T) {
	nodes := []node{
		layer(pr(40, "main", "a"), 7, 2, 1),
		layer(pr(41, "pr40", "b"), 7, 2, 2),
		pr(9, "main", "q"),
	}
	found := classify(nodes, "main", asMe)
	cases := []struct {
		number int
		want   *Layer
	}{
		{40, &Layer{Stack: 7, Position: 1, Size: 2}},
		{41, &Layer{Stack: 7, Position: 2, Size: 2}},
		{9, nil},
	}
	for _, c := range cases {
		got := find(t, found.PRs, c.number).Layer
		if (got == nil) != (c.want == nil) || (got != nil && *got != *c.want) {
			t.Errorf("#%d has layer %v, want %v", c.number, got, c.want)
		}
	}
}

func TestOnlySharedCommitsHoldAPR(t *testing.T) {
	cases := []struct {
		evidence Evidence
		want     bool
	}{
		{Declared, false},
		{Native, false},
		{CarriesTip, true},
		{Overlap, true},
	}
	for _, c := range cases {
		if got := (Parent{Number: 1, Evidence: c.evidence}).Holds(); got != c.want {
			t.Errorf("a %s parent holds = %v, want %v", c.evidence, got, c.want)
		}
	}
}

func TestAPRInAStackIsReviewedAtOnce(t *testing.T) {
	cases := []struct {
		name  string
		nodes []node
	}{
		{"a declared stack", declaredStack(t)},
		{"a GitHub stack", []node{
			layer(pr(8, "main", "c1", "c2"), 7, 2, 1),
			layer(pr(9, "pr8", "c9"), 7, 2, 2),
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			nine := find(t, classify(c.nodes, "main", asMe).PRs, 9)
			if nine.Parent == nil || nine.Parent.Number != 8 {
				t.Fatalf("#9 sits on %s, want #8", describe(nine.Parent))
			}
			if nine.Stacked(allGates) {
				t.Error("#9 is held; its diff is its own layer, so it is reviewed at once")
			}
			if !nine.Ready(allGates) {
				t.Error("#9 is not ready")
			}
		})
	}
}
