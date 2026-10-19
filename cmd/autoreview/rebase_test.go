package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// staleDependabot rewrites the fixture's Dependabot PR #3 so that it
// conflicts with main.
func staleDependabot(t *testing.T, fixtures string) {
	t.Helper()
	path := filepath.Join(fixtures, "prs.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Data struct {
			Repository struct {
				DefaultBranchRef any `json:"defaultBranchRef"`
				PullRequests     struct {
					Nodes []map[string]any `json:"nodes"`
				} `json:"pullRequests"`
			} `json:"repository"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for _, n := range doc.Data.Repository.PullRequests.Nodes {
		if n["number"] == float64(3) {
			n["mergeable"], n["baseRefName"] = "CONFLICTING", "main"
		}
	}
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	write(t, fixtures, "prs.json", string(out))
}

func posted(t *testing.T, fixtures string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(fixtures, "posted.log"))
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestAStaleDependabotPRGetsARebaseCommentAndNoReview(t *testing.T) {
	fixtures := sandbox(t)
	staleDependabot(t, fixtures)
	stdout, stderr, code := autoreview(t, "--dependabot", "--jobs=2")

	if code != 0 {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "note: asked Dependabot to rebase PR #3: it conflicts with main\n") {
		t.Errorf("stderr does not say the comment was posted:\n%s", stderr)
	}
	if !strings.Contains(stdout, "#3  asked Dependabot to rebase") {
		t.Errorf("the review list does not say #3 waits on Dependabot:\n%s", stdout)
	}
	if got := posted(t, fixtures); got != "api --method POST repos/acme/widgets/issues/3/comments -f body=@dependabot rebase --silent\n" {
		t.Errorf("posted %q, want one rebase comment on #3", got)
	}
	for _, call := range dashpCalls(t, fixtures) {
		if strings.HasSuffix(call, "/auto-review 3") {
			t.Errorf("PR #3 was reviewed: %s", call)
		}
	}
}

func TestARebaseCommentIsNotPostedTwiceForOneHead(t *testing.T) {
	fixtures := sandbox(t)
	staleDependabot(t, fixtures)
	write(t, fixtures, "rebase-asks.tsv", "me\t2026-08-05T10:00:00Z\n")
	_, stderr, _ := autoreview(t, "--dependabot")

	if !strings.Contains(stderr, "note: PR #3 waits for Dependabot: you asked it to rebase already\n") {
		t.Errorf("stderr:\n%s", stderr)
	}
	if got := posted(t, fixtures); got != "" {
		t.Errorf("posted %q, want nothing: you asked about this head already", got)
	}
}

func TestNoPostSaysTheRebaseCommentItWouldPost(t *testing.T) {
	fixtures := sandbox(t)
	staleDependabot(t, fixtures)
	_, stderr, _ := autoreview(t, "--dependabot", "--no-post")

	if !strings.Contains(stderr, "note: would ask Dependabot to rebase PR #3: it conflicts with main (--no-post)\n") {
		t.Errorf("stderr:\n%s", stderr)
	}
	if got := posted(t, fixtures); got != "" {
		t.Errorf("posted %q under --no-post", got)
	}
}
