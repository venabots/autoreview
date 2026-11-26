package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withBases gives every PR of the sandbox a base branch, which the risk
// check needs: main, or for PR #9 the base given.
func withBases(t *testing.T, fixtures, base9 string) {
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
		node := n.(map[string]any)
		node["baseRefName"] = "main"
		if node["number"] == 9.0 {
			node["baseRefName"] = base9
		}
	}
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	write(t, fixtures, "prs.json", string(out))
}

// onRef writes a file of the repository as one ref has it. The risk check
// reads at fakeTrustedCommit, the default branch's commit.
func onRef(t *testing.T, fixtures, ref, path, body string) {
	t.Helper()
	full := filepath.Join(fixtures, "at", ref, path)
	write(t, mkdir(t, filepath.Dir(full)), filepath.Base(full), body)
}

// riskScript prints the rating, then how many paths it read, whether it
// holds a GitHub token, and the commit it was read at.
func riskScript(rating string) string {
	return "#!/bin/sh\nn=$(wc -l | tr -d ' ')\necho " + rating +
		"\necho \"$n paths, one a migration, token ${GH_TOKEN:-none}, read at $AUTOREVIEW_TRUSTED_REF\"\n"
}

// The script that REVIEW.md names on the default branch rates the PR before
// its review starts, whatever branch the PR is based on: a branch below a
// stack can be pushed by anyone who can push, and its copy must not decide.
// The script reads the PR's changed paths, the old path of a rename too, and
// none of the run's tokens: only the one named for it reaches it, and that
// one reaches nothing else, the review included. A rating
// above the ceiling takes the approval away before the reviewer runs, and
// the run says why; a rating within it leaves the review to decide. A
// repository with no REVIEW.md leaves the review to decide too.
func TestTheDefaultBranchsRiskScriptSetsWhatTheReviewMayPost(t *testing.T) {
	held := "PR #9 waits for a person to approve it: bin/pr-risk rated it high, above the ceiling of medium: 3 paths, one a migration, "
	cases := []struct {
		name    string
		base    string
		rating  string
		noFile  bool
		token   string
		prompt  string
		message string
	}{
		{"a migration", "main", "HIGH", false, "", "-- /auto-review 9 --verdicts request-changes-only",
			held + "token none, read at " + fakeTrustedCommit},
		{"a token named for the script", "main", "HIGH", false, "github_pat_read_only", "-- /auto-review 9 --verdicts request-changes-only",
			held + "token github_pat_read_only, read at " + fakeTrustedCommit},
		{"a stacked PR whose base says LOW", "bob/fix-typo", "HIGH", false, "", "-- /auto-review 9 --verdicts request-changes-only",
			held + "token none, read at " + fakeTrustedCommit},
		{"a docs fix", "main", "LOW", false, "", "-- /auto-review 9 --max-risk medium", ""},
		{"no REVIEW.md", "main", "", true, "", "-- /auto-review 9 --max-risk medium", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fixtures := sandbox(t)
			withBases(t, fixtures, c.base)
			settingsFile(t, "[review]\napprove_authors = [\"@Bob\"]\napprove_others_up_to = \"medium\"\n\n[prs]\nstacked = true\n")
			t.Setenv("GH_TOKEN", "ghp_approving")
			t.Setenv("AUTOREVIEW_RISK_GH_TOKEN", c.token)
			reviewEnv := filepath.Join(t.TempDir(), "env")
			t.Setenv("FAKE_DASHP_ENV", reviewEnv)
			write(t, fixtures, "files-9.tsv", "packages/db/src/migrations/0123.ts\t\napps/money.ts\tapps/old-money.ts\n")
			// The PR's base names a script that passes everything. It is
			// never the one that runs.
			onRef(t, fixtures, c.base, "REVIEW.md", "Risk script: `bin/always-low`\n")
			onRef(t, fixtures, c.base, "bin/always-low", riskScript("LOW"))
			if !c.noFile {
				onRef(t, fixtures, fakeTrustedCommit, "REVIEW.md", "# Review guidance\n\nRisk script: `bin/pr-risk`\n")
				onRef(t, fixtures, fakeTrustedCommit, "bin/pr-risk", riskScript(c.rating))
			}
			stdout, stderr, code := autoreview(t, "9")
			if code != 0 || !calledWith(t, fixtures, c.prompt) {
				t.Errorf("exit %d, calls %q, want %q\nstderr: %s", code, dashpCalls(t, fixtures), c.prompt, stderr)
			}
			said := stdout + stderr
			if c.message != "" && !strings.Contains(said, c.message) || c.message == "" && strings.Contains(said, "waits for a person") {
				t.Errorf("the run said:\n%s\nwant %q", said, c.message)
			}
			if strings.Contains(said, "ghp_approving") {
				t.Errorf("the script saw the run's own token:\n%s", said)
			}
			env, err := os.ReadFile(reviewEnv)
			if err != nil || !strings.Contains(string(env), "PATH=") {
				t.Fatalf("the review did not say what it was started with: %v", err)
			}
			if holdsRiskToken(string(env), c.token) {
				t.Errorf("the review was started with the risk script's token")
			}
		})
	}
}

// The risk script's token is for the risk script alone. A panelist runs a
// PR's code, so the PR's author could read whatever it holds: a panel that
// a person starts with the token set starts every panelist without it.
func TestNoPanelistIsStartedWithTheRiskScriptsToken(t *testing.T) {
	b := panelSandbox(t)
	t.Setenv("FAKE_PANEL_ENV", "1")
	t.Setenv("AUTOREVIEW_RISK_GH_TOKEN", "github_pat_read_only")
	if _, stderr, code := b.panel("--base", "main"); code != 0 {
		t.Fatalf("exit %d\nstderr:\n%s", code, stderr)
	}
	envs := b.environments()
	if len(envs) != 4 {
		t.Fatalf("%d panelists said what they were started with, want 3 and the synthesis", len(envs))
	}
	for i, env := range envs {
		if holdsRiskToken(env, "github_pat_read_only") {
			t.Errorf("panelist %d of %d was started with the risk script's token", i+1, len(envs))
		}
	}
}

// holdsRiskToken tells whether env, a child's environment, holds the risk
// script's variable or token, a value that can be "". It says no more than
// that: the rest of the environment can hold the credentials of whoever
// runs the tests, which a failure must not print.
func holdsRiskToken(env, token string) bool {
	return strings.Contains("\n"+env, "\nAUTOREVIEW_RISK_GH_TOKEN=") || token != "" && strings.Contains(env, token)
}
