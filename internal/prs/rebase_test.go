package prs

import (
	"strings"
	"testing"
)

// botPR is one Dependabot PR, with the fields the cases change spliced in.
func botPR(extra string) string {
	return `[{"number":3,"title":"Bump lodash","isDraft":false,
   "updatedAt":"2026-08-04T10:00:00Z","reviewDecision":null,
   "headRefOid":"sha3","baseRefName":"main","headRefName":"dependabot/npm/lodash",
   "author":{"login":"dependabot"},
   "comments":{"nodes":[]},"reviews":{"nodes":[]}` + extra + `}]`
}

func onlyPR(t *testing.T, raw string) PR {
	t.Helper()
	listing := classify(mustNodes(t, raw), "main", Options{Me: "me", Dependabot: true})
	if len(listing.PRs) != 1 {
		t.Fatalf("the fixture has %d PRs, want 1", len(listing.PRs))
	}
	return listing.PRs[0]
}

func TestAPRConflictsOnlyWhenGitHubSaysConflicting(t *testing.T) {
	cases := []struct {
		mergeable string
		want      bool
	}{
		{`,"mergeable":"CONFLICTING"`, true},
		{`,"mergeable":"MERGEABLE"`, false},
		{`,"mergeable":"UNKNOWN"`, false},
		{``, false},
	}
	for _, c := range cases {
		if got := onlyPR(t, botPR(c.mergeable)).Conflicts; got != c.want {
			t.Errorf("mergeable %q: Conflicts is %v, want %v", c.mergeable, got, c.want)
		}
	}
}

func TestTheHeadDateIsTheNewestCommitsDate(t *testing.T) {
	pr := onlyPR(t, botPR(`,"commits":{"nodes":[
     {"commit":{"oid":"a","committedDate":"2026-08-01T10:00:00Z"}},
     {"commit":{"oid":"sha3","committedDate":"2026-08-04T10:00:00Z"}}]}`))
	if pr.HeadDate != "2026-08-04T10:00:00Z" {
		t.Errorf("HeadDate is %q, want the date of the last commit", pr.HeadDate)
	}
	if empty := onlyPR(t, botPR(`,"commits":{"nodes":[]}`)); empty.HeadDate != "" {
		t.Errorf("a PR with no commits has HeadDate %q, want empty", empty.HeadDate)
	}
}

func TestOthersCommittedSaysAPersonOtherThanTheAuthorAddedACommit(t *testing.T) {
	commit := func(login string) string {
		if login == "" {
			return `{"commit":{"oid":"x","committedDate":"2026-08-04T10:00:00Z","author":{"user":null}}}`
		}
		return `{"commit":{"oid":"x","committedDate":"2026-08-04T10:00:00Z","author":{"user":{"login":"` + login + `"}}}}`
	}
	cases := []struct {
		name    string
		commits []string
		want    bool
	}{
		{"only the bot's own commits", []string{commit("dependabot")}, false},
		{"a commit no account claims, which is how GitHub shows a bot's", []string{commit("")}, false},
		{"another bot's commit", []string{commit("dependabot-preview")}, false},
		{"a person's commit on top", []string{commit("dependabot"), commit("alice")}, true},
	}
	for _, c := range cases {
		pr := onlyPR(t, botPR(`,"commits":{"nodes":[`+strings.Join(c.commits, ",")+`]}`))
		if pr.OthersCommitted != c.want {
			t.Errorf("%s: OthersCommitted is %v, want %v", c.name, pr.OthersCommitted, c.want)
		}
	}
}

func TestTheQueryAsksWhetherAPRConflicts(t *testing.T) {
	if !strings.Contains(Query, "mergeable") {
		t.Error("the query has no mergeable: a Dependabot PR with conflicts would be reviewed, not rebased")
	}
}
