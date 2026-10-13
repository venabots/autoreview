package prs

import (
	"strings"
	"testing"
)

func TestGraphQLErrorsDoNotReadAsAnEmptyRepo(t *testing.T) {
	// A 200 carrying errors, and a response missing the data path: both must
	// refuse, or an unattended sweep exits 0 having reviewed nothing.
	cases := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{
			name:    "errors with no data",
			raw:     `{"errors":[{"message":"rate limited"}],"data":null}`,
			wantErr: "gh api graphql returned errors:\n  rate limited",
		},
		{
			name: "errors beside data that looks whole",
			raw: `{"errors":[{"message":"first"},{"message":"second"}],
			       "data":{"repository":{"pullRequests":{"nodes":[]}}}}`,
			wantErr: "gh api graphql returned errors:\n  first\n  second",
		},
		{
			name:    "an error with no message is shown as it came",
			raw:     `{"errors":[{"type":"FORBIDDEN"}]}`,
			wantErr: "gh api graphql returned errors:\n  {\"type\":\"FORBIDDEN\"}",
		},
		{
			name:    "no data path",
			raw:     `{"data":{}}`,
			wantErr: "gh api graphql returned no pull request data",
		},
		{
			name:    "null data",
			raw:     `{"data":null}`,
			wantErr: "gh api graphql returned no pull request data",
		},
		{
			name:    "a repository GitHub would not show",
			raw:     `{"data":{"repository":null}}`,
			wantErr: "gh api graphql returned no pull request data",
		},
		{
			name:    "a pull request connection with no nodes field",
			raw:     `{"data":{"repository":{"pullRequests":{}}}}`,
			wantErr: "gh api graphql returned no pull request data",
		},
		{
			name:    "a null list",
			raw:     `{"data":{"repository":{"pullRequests":{"nodes":null}}}}`,
			wantErr: "pull request",
		},
		{
			name:    "a null entry",
			raw:     `{"data":{"repository":{"pullRequests":{"nodes":[null]}}}}`,
			wantErr: "parsing the pull request list: entry 0 has no number",
		},
		{
			name:    "an entry with no number",
			raw:     `{"data":{"repository":{"pullRequests":{"nodes":[{"number":9},{"title":"t"}]}}}}`,
			wantErr: "parsing the pull request list: entry 1 has no number",
		},
		{
			name:    "a list that is not a list",
			raw:     `{"data":{"repository":{"pullRequests":{"nodes":{"number":9}}}}}`,
			wantErr: "parsing the pull request list: ",
		},
		{
			name:    "not JSON",
			raw:     `fake gh: the PR list is unavailable`,
			wantErr: "parsing gh api graphql output: ",
		},
		{
			name:    "nothing at all",
			raw:     ``,
			wantErr: "parsing gh api graphql output: ",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			found, err := Parse([]byte(c.raw), asMe)
			if err == nil {
				t.Fatalf("Parse read %d PRs and no error, want an error", len(found.PRs))
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, c.wantErr)
			}
			if strings.HasPrefix(err.Error(), "error: ") {
				t.Errorf("error = %q: the prefix is the caller's to add", err)
			}
		})
	}
}

func TestAnEmptyListIsNoPRsAndNoError(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"an empty list", `{"data":{"repository":{"pullRequests":{"nodes":[]}}}}`},
		{"an empty errors array is not a refusal", `{"errors":[],"data":{"repository":{"pullRequests":{"nodes":[]}}}}`},
		{"null errors is not a refusal", `{"errors":null,"data":{"repository":{"pullRequests":{"nodes":[]}}}}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			found, err := Parse([]byte(c.raw), asMe)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if len(found.PRs) != 0 || len(found.Reviewable()) != 0 || found.Truncated {
				t.Errorf("listing = %+v, want empty", found)
			}
		})
	}
}

func TestParseReadsTheDefaultBranchAndUsesIt(t *testing.T) {
	// #1 merges the trunk onward, so its head branch is the trunk. Only the
	// name GitHub gives for the trunk keeps #9 from waiting on it.
	const nodes = `"pullRequests":{"nodes":[
	  {"number":1,"title":"t","updatedAt":"2026-08-10T10:00:00Z","author":{"login":"alice"},
	   "baseRefName":"develop","headRefName":"trunk"},
	  {"number":9,"title":"t","updatedAt":"2026-08-09T10:00:00Z","author":{"login":"bob"},
	   "baseRefName":"trunk","headRefName":"bob/work"}]}`
	cases := []struct {
		name       string
		raw        string
		wantBranch string
		wantHeld   bool
	}{
		{
			name:       "the trunk is named",
			raw:        `{"data":{"repository":{"defaultBranchRef":{"name":"trunk"},` + nodes + `}}}`,
			wantBranch: "trunk",
			wantHeld:   false,
		},
		{
			name:       "an empty repository has no trunk",
			raw:        `{"data":{"repository":{"defaultBranchRef":null,` + nodes + `}}}`,
			wantBranch: "",
			wantHeld:   true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			found, err := Parse([]byte(c.raw), asMe)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if found.DefaultBranch != c.wantBranch {
				t.Errorf("default branch = %q, want %q", found.DefaultBranch, c.wantBranch)
			}
			if held := find(t, found.PRs, 9).Parent != nil; held != c.wantHeld {
				t.Errorf("#9 held = %v, want %v", held, c.wantHeld)
			}
		})
	}
}
