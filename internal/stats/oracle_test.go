package stats

import (
	"bytes"
	"cmp"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/venabots/autoreview/internal/ledger"
)

// defaultV1 is where Homebrew installs v1.
const defaultV1 = "/opt/homebrew/bin/autoreview"

// TestTheReportsMatchV1OnARealLedger is skipped unless
// $AUTOREVIEW_STATS_ORACLE names a ledger file. It runs v1 and this package
// on that file with the same flags and compares what they print, all but
// the time the JSON report was made. $AUTOREVIEW_V1 names the v1 binary when
// it is not Homebrew's.
//
// Point it at a copy of the ledger: v1 still appends to the real one, and a
// record added between the two runs would be a difference that is not a
// bug. A failure names the line that differs and prints neither side,
// because the report of a real ledger names real bots and models.
func TestTheReportsMatchV1OnARealLedger(t *testing.T) {
	path := os.Getenv("AUTOREVIEW_STATS_ORACLE")
	if path == "" {
		t.Skip("set AUTOREVIEW_STATS_ORACLE to a copy of a ledger to run this")
	}
	v1 := cmp.Or(os.Getenv("AUTOREVIEW_V1"), defaultV1)
	repo := someRepo(t, path)
	cases := []struct {
		name string
		args []string
	}{
		{"no flags", nil},
		{"--json", []string{"--json"}},
		{"--since 7d", []string{"--since", "7d"}},
		{"--since 30d --json", []string{"--since", "30d", "--json"}},
		{"--since 2w", []string{"--since=2w"}},
		{"--repo with part of a repo's name", []string{"--repo", repo}},
		{"--repo with part of a repo's name, --json", []string{"--repo", repo, "--json"}},
		{"--repo and --since 30d", []string{"--since", "30d", "--repo=" + strings.ToUpper(repo)}},
		{"--repo that matches nothing", []string{"--repo", "no-such-repo-anywhere"}},
		{"--repo that matches nothing, --json", []string{"--repo", "no-such-repo-anywhere", "--json"}},
		{"--since a date in the future", []string{"--since", "2999-01-01"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			args := append([]string{"--ledger", path}, c.args...)
			want, err := exec.Command(v1, append([]string{"stats"}, args...)...).Output()
			if err != nil {
				t.Fatalf("v1 failed: %v", err)
			}
			got := runStats(os.Getenv, time.Now(), false, args...)
			if got.status != 0 || got.stderr != "" {
				t.Fatalf("status %d with %d bytes on stderr, want 0 and nothing", got.status, len(got.stderr))
			}
			ours, theirs := withoutClock(got.stdout), withoutClock(string(want))
			if ours != theirs {
				t.Errorf("the report differs from v1's at line %d (%d lines here, %d in v1's)",
					firstDifferentLine(ours, theirs), strings.Count(ours, "\n"), strings.Count(theirs, "\n"))
			}
			if len(want) == 0 {
				t.Error("v1 printed nothing: the case compares nothing")
			}
			t.Logf("%d lines, the same as v1's", bytes.Count(want, []byte("\n")))
		})
	}
}

// someRepo is part of the name of a repo in the ledger, so that --repo has
// something to match. It is the part after the owner.
func someRepo(t *testing.T, path string) string {
	t.Helper()
	records, err := ledger.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range records {
		if r.Repo == nil {
			continue
		}
		if _, name, ok := strings.Cut(*r.Repo, "/"); ok && name != "" {
			return name
		}
	}
	t.Fatal("no record in the ledger names a repo")
	return ""
}

func firstDifferentLine(a, b string) int {
	ours, theirs := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := range min(len(ours), len(theirs)) {
		if ours[i] != theirs[i] {
			return i + 1
		}
	}
	return min(len(ours), len(theirs)) + 1
}
