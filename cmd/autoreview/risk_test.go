package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withBases gives every PR of the sandbox the base branch main, which the
// risk check reads REVIEW.md from.
func withBases(t *testing.T, fixtures string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(fixtures, "prs.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	nodes := doc["data"].(map[string]any)["repository"].(map[string]any)["pullRequests"].(map[string]any)["nodes"].([]any)
	for _, n := range nodes {
		n.(map[string]any)["baseRefName"] = "main"
	}
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	write(t, fixtures, "prs.json", string(out))
}

// onBase writes a file of the repository as its base branch has it.
func onBase(t *testing.T, fixtures, path, body string) {
	t.Helper()
	full := filepath.Join(fixtures, "base", path)
	write(t, mkdir(t, filepath.Dir(full)), filepath.Base(full), body)
}

// The script that REVIEW.md names on the base rates the PR before its
// review starts. It reads the PR's changed paths, the old path of a rename
// too. A rating above the
// ceiling takes the approval away before the reviewer runs, and the run
// says why; a rating within it leaves the review to decide. A repository
// with no REVIEW.md leaves the review to decide too.
func TestTheBasesRiskScriptSetsWhatTheReviewMayPost(t *testing.T) {
	cases := []struct {
		name    string
		rating  string
		noFile  bool
		prompt  string
		message string
	}{
		{"a migration", "HIGH", false, "-- /auto-review 9 --verdicts request-changes-only",
			"PR #9 waits for a person to approve it: bin/pr-risk rated it high, above the ceiling of medium: 3 paths, one a migration"},
		{"a docs fix", "LOW", false, "-- /auto-review 9 --max-risk medium", ""},
		{"no REVIEW.md", "", true, "-- /auto-review 9 --max-risk medium", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fixtures := sandbox(t)
			withBases(t, fixtures)
			settingsFile(t, "[review]\napprove_authors = [\"@Bob\"]\napprove_others_up_to = \"medium\"\n")
			write(t, fixtures, "files-9.tsv", "packages/db/src/migrations/0123.ts\t\napps/money.ts\tapps/old-money.ts\n")
			if !c.noFile {
				onBase(t, fixtures, "REVIEW.md", "# Review guidance\n\nRisk script: `bin/pr-risk`\n")
				onBase(t, fixtures, "bin/pr-risk", "#!/bin/sh\nn=$(wc -l | tr -d ' ')\necho "+c.rating+"\necho \"$n paths, one a migration\"\n")
			}
			stdout, stderr, code := autoreview(t, "9")
			if code != 0 || !calledWith(t, fixtures, c.prompt) {
				t.Errorf("exit %d, calls %q, want %q\nstderr: %s", code, dashpCalls(t, fixtures), c.prompt, stderr)
			}
			said := stdout + stderr
			if c.message != "" && !strings.Contains(said, c.message) || c.message == "" && strings.Contains(said, "waits for a person") {
				t.Errorf("the run said:\n%s\nwant %q", said, c.message)
			}
		})
	}
}
