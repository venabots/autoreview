package prs

import (
	"slices"
	"testing"
)

// declaredStack is the stacked fixture in its other shape: #9's base is #8's
// branch. GitHub already keeps their diffs apart, so they share no commits,
// but #9's code only makes sense on top of #8's.
func declaredStack(t *testing.T) []node {
	t.Helper()
	return change(stackedFixture(t), 9, func(n node) node {
		n.BaseRefName = "decision"
		n.HeadRefOid = "c9"
		n.Commits = commitList{Nodes: []commitNode{{Commit: commit{Oid: "c9"}}}}
		return n
	})
}

func authoredBy(login string) func(node) node {
	return func(n node) node {
		n.Author = actor{Login: login}
		return n
	}
}

func asDraft(n node) node {
	n.IsDraft = true
	return n
}

// What the sweep reviews now. v1's select_auto printed these numbers and
// returned them; the printing belongs to a later package.
func TestTheSweepSelection(t *testing.T) {
	cases := []struct {
		name  string
		nodes func(t *testing.T) []node
		gates Gates
		want  []int
	}{
		{
			// SEEN PRs are skipped on purpose: nothing has changed since you
			// last engaged.
			name:  "NEW and UPDATED only",
			nodes: fixture,
			gates: allGates,
			want:  []int{9, 8},
		},
		{
			name: "a PR whose checks are pending is held",
			nodes: func(t *testing.T) []node {
				return withCI(fixture(t), map[int]string{9: "PENDING", 8: "SUCCESS"})
			},
			gates: allGates,
			want:  []int{8},
		},
		{
			name: "the CI gate off reviews it anyway",
			nodes: func(t *testing.T) []node {
				return withCI(fixture(t), map[int]string{9: "PENDING", 8: "SUCCESS"})
			},
			gates: ciGateOff,
			want:  []int{9, 8},
		},
		{
			// Failing is held the same way: the author is about to push.
			name: "failing checks leave nothing to review",
			nodes: func(t *testing.T) []node {
				return withCI(fixture(t), map[int]string{9: "FAILURE", 8: "ERROR"})
			},
			gates: allGates,
			want:  []int{},
		},
		{
			name: "failing checks with the CI gate off",
			nodes: func(t *testing.T) []node {
				return withCI(fixture(t), map[int]string{9: "FAILURE", 8: "ERROR"})
			},
			gates: ciGateOff,
			want:  []int{9, 8},
		},
		{
			// An unknown state is no checks, and no checks is never held.
			name: "a state outside the schema holds nothing",
			nodes: func(t *testing.T) []node {
				return withCI(fixture(t), map[int]string{9: "SOMETHING_NEW"})
			},
			gates: allGates,
			want:  []int{9, 8},
		},
		{
			name:  "the PR underneath is reviewed and the one on top is held",
			nodes: stackedFixture,
			gates: allGates,
			want:  []int{9},
		},
		{
			name:  "the stack gate off reviews both",
			nodes: stackedFixture,
			gates: stackGateOff,
			want:  []int{9, 8},
		},
		{
			name:  "a declared stack is held the same way",
			nodes: declaredStack,
			gates: allGates,
			want:  []int{8},
		},
		{
			// The overlap is worked out before a single filter runs. A
			// branch cut from your own PR carries those commits in its diff
			// whether or not this tool would ever review the PR underneath.
			name: "my own hidden PR still holds the one cut from it",
			nodes: func(t *testing.T) []node {
				return change(stackedFixture(t), 9, authoredBy("me"))
			},
			gates: allGates,
			want:  []int{},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := readyNumbers(classify(c.nodes(t), "main", asMe), c.gates)
			if !slices.Equal(got, c.want) {
				t.Errorf("ready = %v, want %v", got, c.want)
			}
		})
	}
}

func TestReadinessIsEngagementAndThenChecks(t *testing.T) {
	nodes := withCI(fixture(t), map[int]string{9: "PENDING", 6: "PENDING"})
	found := classify(nodes, "main", asMe)
	nine := find(t, found.PRs, 9)
	six := find(t, found.PRs, 6)

	cases := []struct {
		name string
		got  bool
		want bool
	}{
		{"#9 is actionable", nine.Actionable(), true},
		{"#9 is ready with every gate on", nine.Ready(allGates), false},
		{"#9 is ready with the CI gate off", nine.Ready(ciGateOff), true},
		{"#9 is held with every gate on", nine.Held(allGates), true},
		{"#9 is held with the CI gate off", nine.Held(ciGateOff), false},
		// SEEN is not ready whatever its checks say, and not held either:
		// the hold is for PRs the sweep would otherwise review.
		{"#6 is actionable", six.Actionable(), false},
		{"#6 is ready with every gate on", six.Ready(allGates), false},
		{"#6 is ready with the CI gate off", six.Ready(ciGateOff), false},
		{"#6 is held", six.Held(allGates), false},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestActionableIsNewOrUpdated(t *testing.T) {
	cases := []struct {
		engagement Engagement
		want       bool
	}{
		{New, true},
		{Updated, true},
		{Seen, false},
	}
	for _, c := range cases {
		if got := (PR{Engagement: c.engagement}).Actionable(); got != c.want {
			t.Errorf("%s actionable = %v, want %v", c.engagement, got, c.want)
		}
	}
}

func TestWhatTheStackTestNeedsReachesThePR(t *testing.T) {
	// The hold is only as good as this wiring: the query asks for the fields,
	// the stack reads them, and the PR carries the answer to the sweep and
	// the view.
	found := classify(stackedFixture(t), "main", asMe)
	eight := find(t, found.PRs, 8)
	nine := find(t, found.PRs, 9)

	// #9's two commits, the second of which is its tip.
	want := &Parent{Number: 9, Evidence: CarriesTip, Shared: 2}
	if !sameParent(eight.Parent, want) {
		t.Errorf("#8 sits on %s, want %s", describe(eight.Parent), describe(want))
	}
	if nine.Parent != nil {
		t.Errorf("#9 sits on %s; the one underneath is nobody's descendant", describe(nine.Parent))
	}
	if eight.Base != "main" || eight.Branch != "decision" {
		t.Errorf("#8 base = %q, branch = %q; want main, decision", eight.Base, eight.Branch)
	}
}

func TestADeclaredStackIsHeldTheSameWay(t *testing.T) {
	found := classify(declaredStack(t), "main", asMe)
	nine := find(t, found.PRs, 9)
	eight := find(t, found.PRs, 8)

	want := &Parent{Number: 8, Evidence: Declared}
	if !sameParent(nine.Parent, want) {
		t.Errorf("#9 sits on %s, want %s", describe(nine.Parent), describe(want))
	}
	if eight.Parent != nil {
		t.Errorf("#8 sits on %s; the one underneath is reviewed", describe(eight.Parent))
	}
}

func TestAPRTheListHidesStillHoldsTheOneCutFromIt(t *testing.T) {
	// A stack worked out from the visible half would miss exactly these.
	cases := []struct {
		name       string
		hide       func(node) node
		wantHidden Hidden
	}{
		{"my own PR", authoredBy("me"), HiddenOwn},
		{"a draft", asDraft, HiddenDraft},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			found := classify(change(stackedFixture(t), 9, c.hide), "main", asMe)
			if got := find(t, found.PRs, 9).Hidden; got != c.wantHidden {
				t.Fatalf("#9 is %s, want %s", got, c.wantHidden)
			}
			reviewable := found.Reviewable()
			if len(reviewable) != 1 {
				t.Fatalf("reviewable = %v, want only #8", numbers(reviewable))
			}
			if on := reviewable[0].Parent; on == nil || on.Number != 9 {
				t.Errorf("#8 sits on %s, want #9", describe(on))
			}
		})
	}
}

func TestAStackedPRIsNeverReportedAsHeldForItsChecks(t *testing.T) {
	// The two holds end differently. Checks settle by themselves, so a loop
	// waits for them; a stack moves when a person merges something, so a loop
	// that counted this as a CI hold would sit waiting on a person for as
	// long as it ran.
	nodes := withCI(stackedFixture(t), map[int]string{8: "PENDING"})
	eight := find(t, classify(nodes, "main", asMe).PRs, 8)
	plain := find(t, classify(stackedFixture(t), "main", asMe).PRs, 8)

	cases := []struct {
		name string
		got  bool
		want bool
	}{
		{"stacked with every gate on", plain.Stacked(allGates), true},
		{"ready with every gate on: not reviewed", plain.Ready(allGates), false},
		{"held with every gate on: not waited on either", plain.Held(allGates), false},
		{"held with pending checks: still the stack's hold", eight.Held(allGates), false},
		// With the gate off it is an ordinary PR again, on both counts.
		{"ready with the stack gate off", plain.Ready(stackGateOff), true},
		{"stacked with the stack gate off", plain.Stacked(stackGateOff), false},
		{"held for its pending checks with the stack gate off", eight.Held(stackGateOff), true},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestAQuietPRIsNotDescribedAsStacked(t *testing.T) {
	// SEEN PRs are left alone for their own reason. Naming one as held for
	// the PR underneath it would invite someone to merge that PR expecting a
	// review that was never coming.
	nodes := change(stackedFixture(t), 8, commentBy("me", "2026-08-11T10:00:00Z"))
	eight := find(t, classify(nodes, "main", asMe).PRs, 8)

	if eight.Engagement != Seen {
		t.Fatalf("#8 is %s, want %s", eight.Engagement, Seen)
	}
	if eight.Parent == nil {
		t.Error("the overlap is still true")
	}
	if eight.Stacked(allGates) {
		t.Error("the overlap is not why the sweep skips it")
	}
}
