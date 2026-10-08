package prs

import (
	"slices"
	"testing"
)

func TestHiddenPRsStayHidden(t *testing.T) {
	cases := []struct {
		name string
		opts Options
		want []int
	}{
		// v1 lists these in the order GitHub gave them. Here the list is in
		// sweep order, so the SEEN PR #6 comes after the NEW ones.
		{"the defaults", Options{Me: "me"}, []int{9, 8, 6}},
		{"approved PRs too", Options{Me: "me", All: true}, []int{9, 8, 2, 6}},
		{"bots too", Options{Me: "me", Dependabot: true}, []int{9, 8, 3, 6}},
		{"both", Options{Me: "me", All: true, Dependabot: true}, []int{9, 8, 3, 2, 6}},
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

func TestEveryPRStaysInTheListWithItsReason(t *testing.T) {
	cases := []struct {
		name string
		opts Options
		want map[int]Hidden
	}{
		{
			name: "the defaults",
			opts: Options{Me: "me"},
			want: map[int]Hidden{
				9: NotHidden, 8: NotHidden, 6: NotHidden,
				5: HiddenDraft, 4: HiddenOwn, 3: HiddenBot, 2: HiddenApproved,
			},
		},
		{
			// The two flags lift their own filter and no other: the draft
			// and my own PR stay out whatever is asked for.
			name: "both flags",
			opts: Options{Me: "me", All: true, Dependabot: true},
			want: map[int]Hidden{
				9: NotHidden, 8: NotHidden, 6: NotHidden,
				5: HiddenDraft, 4: HiddenOwn, 3: NotHidden, 2: NotHidden,
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			found := classify(fixture(t), "main", c.opts)
			if len(found.PRs) != len(c.want) {
				t.Fatalf("the list has %d PRs, want all %d", len(found.PRs), len(c.want))
			}
			for _, p := range found.PRs {
				if p.Hidden != c.want[p.Number] {
					t.Errorf("#%d is %s, want %s", p.Number, p.Hidden, c.want[p.Number])
				}
			}
		})
	}
}

func TestTheFirstFilterToRemoveAPRNamesIt(t *testing.T) {
	cases := []struct {
		name string
		node node
		opts Options
		want Hidden
	}{
		{
			name: "my own approved draft is a draft",
			node: node{Number: 1, IsDraft: true, Author: actor{Login: "me"}, ReviewDecision: "APPROVED"},
			opts: Options{Me: "me"},
			want: HiddenDraft,
		},
		{
			name: "my own approved PR is mine",
			node: node{Number: 1, Author: actor{Login: "me"}, ReviewDecision: "APPROVED"},
			opts: Options{Me: "me", All: true},
			want: HiddenOwn,
		},
		{
			name: "an approved bot PR is a bot's",
			node: node{Number: 1, Author: actor{Login: "dependabot[bot]"}, ReviewDecision: "APPROVED"},
			opts: Options{Me: "me"},
			want: HiddenBot,
		},
		{
			name: "an approved bot PR with bots shown is approved",
			node: node{Number: 1, Author: actor{Login: "dependabot[bot]"}, ReviewDecision: "APPROVED"},
			opts: Options{Me: "me", Dependabot: true},
			want: HiddenApproved,
		},
		{
			// The match is anchored at the start of the login.
			name: "a login that only contains the bot name is a person",
			node: node{Number: 1, Author: actor{Login: "not-dependabot"}},
			opts: Options{Me: "me"},
			want: NotHidden,
		},
		{
			name: "changes requested is not approval",
			node: node{Number: 1, Author: actor{Login: "alice"}, ReviewDecision: "CHANGES_REQUESTED"},
			opts: Options{Me: "me"},
			want: NotHidden,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := hiddenReason(c.node, c.opts); got != c.want {
				t.Errorf("hidden = %s, want %s", got, c.want)
			}
		})
	}
}

func TestAFullPageIsKnownToBeIncompleteBeforeDraftsAreDropped(t *testing.T) {
	// The count of nodes the query returned, not the count that survived the
	// filters: a full page holding two drafts reports 48 open, and without
	// this flag it would report that as an exact total.
	first := fixture(t)[0]
	page := make([]node, queryLimit)
	for i := range page {
		page[i] = first
		page[i].Number = i + 100
		page[i].IsDraft = i < 2
	}
	found := classify(page, "main", asMe)
	if !found.Truncated {
		t.Error("a full page means there are more")
	}
	if got := openCount(found); got != queryLimit-2 {
		t.Errorf("open = %d, want %d: the drafts are still not open", got, queryLimit-2)
	}
	if got := len(found.PRs); got != queryLimit {
		t.Errorf("the list has %d PRs, want every one of the %d", got, queryLimit)
	}

	short := classify(page[:queryLimit-1], "main", asMe)
	if short.Truncated {
		t.Error("one short of a full page is the whole answer")
	}
}

func TestTheListingReportsWhatItsFiltersRemoved(t *testing.T) {
	// The number a user sees in the browser, and the number this tool will
	// act on. They differ on any repo where most PRs are your own, and both
	// come out of the one function that does the filtering.
	cases := []struct {
		name           string
		opts           Options
		wantOpen       int
		wantReviewable int
	}{
		// Six open, one draft. Yours, the bot and the approved one go.
		{"the defaults", Options{Me: "me"}, 6, 3},
		// Widening the flags moves the second number and never the first.
		{"both flags", Options{Me: "me", All: true, Dependabot: true}, 6, 5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			found := classify(fixture(t), "main", c.opts)
			if got := openCount(found); got != c.wantOpen {
				t.Errorf("open = %d, want %d", got, c.wantOpen)
			}
			if got := len(found.Reviewable()); got != c.wantReviewable {
				t.Errorf("reviewable = %d, want %d", got, c.wantReviewable)
			}
			if found.Truncated {
				t.Error("seven nodes is not a full page")
			}
		})
	}
}

func TestEngagementAndRanking(t *testing.T) {
	type shape struct {
		number     int
		engagement Engagement
	}
	found := classify(fixture(t), "main", asMe)

	want := []shape{{9, New}, {8, New}, {6, Seen}}
	got := []shape{}
	for _, p := range found.Reviewable() {
		got = append(got, shape{p.Number, p.Engagement})
	}
	if !slices.Equal(got, want) {
		t.Errorf("reviewable = %v, want %v", got, want)
	}

	// The whole list is in the same order: NEW by recency, then SEEN.
	wantAll := []int{9, 8, 5, 4, 3, 2, 6}
	if gotAll := numbers(found.PRs); !slices.Equal(gotAll, wantAll) {
		t.Errorf("the list is %v, want %v", gotAll, wantAll)
	}
}

func TestSweepOrder(t *testing.T) {
	at := func(number int, e Engagement, updated string) PR {
		return PR{Number: number, Engagement: e, UpdatedAt: updated}
	}
	cases := []struct {
		name string
		prs  []PR
		want []int
	}{
		{
			name: "NEW, then UPDATED, then SEEN, whatever their dates",
			prs: []PR{
				at(1, Seen, "2026-08-12T10:00:00Z"),
				at(2, Updated, "2026-08-11T10:00:00Z"),
				at(3, New, "2026-08-01T10:00:00Z"),
			},
			want: []int{3, 2, 1},
		},
		{
			name: "the most recently updated first within one state",
			prs: []PR{
				at(1, New, "2026-08-01T10:00:00Z"),
				at(2, New, "2026-08-03T10:00:00Z"),
				at(3, New, "2026-08-02T10:00:00Z"),
			},
			want: []int{2, 3, 1},
		},
		{
			name: "a tie keeps the order GitHub gave",
			prs: []PR{
				at(7, New, "2026-08-01T10:00:00Z"),
				at(3, New, "2026-08-01T10:00:00Z"),
				at(5, New, "2026-08-01T10:00:00Z"),
			},
			want: []int{7, 3, 5},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			given := numbers(c.prs)
			if got := numbers(sweepOrder(c.prs)); !slices.Equal(got, c.want) {
				t.Errorf("order = %v, want %v", got, c.want)
			}
			if after := numbers(c.prs); !slices.Equal(after, given) {
				t.Errorf("the argument was reordered from %v to %v", given, after)
			}
		})
	}
}

func TestTheHeadSHAReachesThePR(t *testing.T) {
	// The push detection is only as good as this wiring: the query asks for
	// headRefOid and the PR carries it. A missing head is empty, not an
	// error.
	nodes := mustNodes(t, `[
	  {"number":9,"title":"Add retry logic","isDraft":false,
	   "updatedAt":"2026-08-10T10:00:00Z","reviewDecision":null,
	   "headRefOid":"abc123","author":{"login":"alice"},
	   "comments":{"nodes":[]},"reviews":{"nodes":[]},"commits":{"nodes":[]}},
	  {"number":8,"title":"No head","isDraft":false,
	   "updatedAt":"2026-08-09T10:00:00Z","reviewDecision":null,
	   "author":{"login":"bob"},
	   "comments":{"nodes":[]},"reviews":{"nodes":[]},"commits":{"nodes":[]}}
	]`)
	found := classify(nodes, "main", asMe)
	if got := find(t, found.PRs, 9).Head; got != "abc123" {
		t.Errorf("#9 head = %q, want %q", got, "abc123")
	}
	if got := find(t, found.PRs, 8).Head; got != "" {
		t.Errorf("#8 head = %q, want none", got)
	}
}

func TestTheChecksReachThePR(t *testing.T) {
	// The whole hold is only as good as this wiring: the query asks for the
	// rollup, the node reads it, and the PR carries it to the sweep.
	nodes := mustNodes(t, `[
	  {"number":9,"title":"t","isDraft":false,"updatedAt":"2026-08-10T10:00:00Z",
	   "reviewDecision":null,"author":{"login":"alice"},
	   "comments":{"nodes":[]},"reviews":{"nodes":[]},"commits":{"nodes":[]},
	   "headCommit":{"nodes":[{"commit":{"statusCheckRollup":{"state":"PENDING"}}}]}},
	  {"number":8,"title":"t","isDraft":false,"updatedAt":"2026-08-09T10:00:00Z",
	   "reviewDecision":null,"author":{"login":"bob"},
	   "comments":{"nodes":[]},"reviews":{"nodes":[]},"commits":{"nodes":[]},
	   "headCommit":{"nodes":[{"commit":{"statusCheckRollup":null}}]}},
	  {"number":7,"title":"t","isDraft":false,"updatedAt":"2026-08-08T10:00:00Z",
	   "reviewDecision":null,"author":{"login":"carol"},
	   "comments":{"nodes":[]},"reviews":{"nodes":[]},"commits":{"nodes":[]}}
	]`)
	found := classify(nodes, "main", asMe)
	want := map[int]CI{
		9: CIPending,
		8: CINone, // a null rollup is no checks
		7: CINone, // a missing field is no checks
	}
	for number, ci := range want {
		if got := find(t, found.PRs, number).CI; got != ci {
			t.Errorf("#%d checks = %s, want %s", number, got, ci)
		}
	}
}

func TestThePRCarriesWhatAViewDraws(t *testing.T) {
	nodes := mustNodes(t, `[
	  {"number":9,"title":"Add retry logic","isDraft":true,
	   "updatedAt":"2026-08-10T10:00:00Z","reviewDecision":"REVIEW_REQUIRED",
	   "headRefOid":"abc123","baseRefName":"main","headRefName":"alice/retry",
	   "isCrossRepository":true,"author":{"login":"alice"}},
	  {"number":8,"title":"From a deleted account","updatedAt":"2026-08-09T10:00:00Z",
	   "reviewDecision":"CHANGES_REQUESTED","author":null},
	  {"number":7,"title":"Bump lodash","updatedAt":"2026-08-08T10:00:00Z",
	   "reviewDecision":"APPROVED","author":{"login":"dependabot[bot]"}},
	  {"number":6,"title":"No decision","updatedAt":"2026-08-07T10:00:00Z",
	   "reviewDecision":null,"author":{"login":"bob"}}
	]`)
	found := classify(nodes, "main", Options{Me: "me", All: true, Dependabot: true})

	nine := find(t, found.PRs, 9)
	want := PR{
		Number: 9, Title: "Add retry logic", Author: "alice",
		Branch: "alice/retry", Base: "main", Head: "abc123",
		UpdatedAt: "2026-08-10T10:00:00Z", Draft: true, CrossRepo: true,
		Decision: ReviewRequired, CI: CINone, Engagement: New, Hidden: HiddenDraft,
	}
	if nine != want {
		t.Errorf("#9 = %+v, want %+v", nine, want)
	}

	decisions := map[int]Decision{9: ReviewRequired, 8: ChangesRequested, 7: Approved, 6: DecisionNone}
	for number, decision := range decisions {
		if got := find(t, found.PRs, number).Decision; got != decision {
			t.Errorf("#%d decision = %s, want %s", number, got, decision)
		}
	}

	// A deleted account is shown as GitHub shows it, and is nobody's own PR
	// and nobody's bot.
	eight := find(t, found.PRs, 8)
	if eight.Author != "ghost" || eight.Hidden != NotHidden || eight.Bot() {
		t.Errorf("#8 author = %q, hidden = %s, bot = %v; want ghost, not hidden, false",
			eight.Author, eight.Hidden, eight.Bot())
	}
	// With bots shown, Hidden no longer says which rows are a bot's.
	seven := find(t, found.PRs, 7)
	if !seven.Bot() || seven.Hidden != NotHidden {
		t.Errorf("#7 bot = %v, hidden = %s; want true, not hidden", seven.Bot(), seven.Hidden)
	}
	if nine.Bot() {
		t.Error("#9 is a person's PR")
	}
}

func TestADeletedAccountIsNotTheUser(t *testing.T) {
	// The own filter reads the login GitHub returned. A user whose login is
	// the display name "ghost" does not own every deleted account's PR.
	nodes := []node{{Number: 8, Title: "t", UpdatedAt: "2026-08-09T10:00:00Z"}}
	found := classify(nodes, "main", Options{Me: "ghost"})
	if got := found.PRs[0].Hidden; got != NotHidden {
		t.Errorf("hidden = %s, want %s", got, NotHidden)
	}
}

func TestClassifyLeavesItsArgumentAlone(t *testing.T) {
	nodes := fixture(t)
	before := make([]int, len(nodes))
	for i, n := range nodes {
		before[i] = n.Number
	}
	classify(nodes, "main", asMe)
	for i, n := range nodes {
		if n.Number != before[i] {
			t.Fatalf("the nodes were reordered: position %d was #%d and is #%d", i, before[i], n.Number)
		}
	}
}
