package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// sandbox makes a git checkout to run in and a GitHub to ask: the seven
// PRs the prs package is tested against, one of each kind.
func sandbox(t *testing.T) (fixtures string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	for _, name := range []string{"gh", "dash-p", "claude", "codex"} {
		if err := os.Symlink(self, filepath.Join(bin, name)); err != nil {
			t.Fatal(err)
		}
	}
	fixtures = t.TempDir()
	// The same three files scripts/sandbox.sh serves, so a person who runs
	// the sandbox sees the repo these tests see.
	for name, from := range map[string]string{
		"prs.json":  filepath.Join("..", "..", "internal", "prs", "testdata", "prs.json"),
		"mine.json": filepath.Join("testdata", "mine.json"),
		"repo.json": filepath.Join("testdata", "repo.json"),
	} {
		raw, err := os.ReadFile(from)
		if err != nil {
			t.Fatal(err)
		}
		write(t, fixtures, name, string(raw))
	}

	repo := t.TempDir()
	if out, err := exec.Command("git", "-C", repo, "init", "--quiet").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	t.Chdir(repo)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_GH_DIR", fixtures)
	t.Setenv("FAKE_GH_LOGIN", "me")
	// A panel set in the person's shell would fill the settings form and
	// seat the review panel: the tests must see the same on every machine.
	t.Setenv("PANEL_REVIEW_PANELISTS", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	// codex finds its skills under ~/.agents/skills, so the automatic
	// fallback is codex, as on a machine with both agents set up.
	for _, skill := range []string{"auto-review", "panel-review", "recheck-pr"} {
		dir := filepath.Join(home, ".agents", "skills", skill)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		write(t, dir, "SKILL.md", "# "+skill+"\n")
	}
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	t.Setenv("CODEX_HOME", "")
	t.Setenv("DASHP_BIN", "")
	t.Setenv("FAKE_DASHP_LOG", filepath.Join(fixtures, "dash-p.log"))
	for _, name := range []string{
		"AUTOREVIEW_WATCH_INTERVAL", "AUTOREVIEW_JOBS", "AUTOREVIEW_TIMEOUT", "AUTOREVIEW_MAX_BUDGET_USD",
		"AUTOREVIEW_SKILLS", "AUTOREVIEW_ORCHESTRATOR", "AUTOREVIEW_CMD", "AUTOREVIEW_FALLBACK",
		"AUTOREVIEW_BABYSIT_INTERVAL", "AUTOREVIEW_MAX_IDLE", "AUTOREVIEW_MAX_PASSES", "AUTOREVIEW_CI_WAIT",
		"XDG_CONFIG_HOME",
		"FAKE_DASHP_FAIL", "FAKE_DASHP_DECISION", "FAKE_DASHP_NO_TRAILER", "FAKE_DASHP_HANG", "FAKE_DASHP_SLEEP",
		"FAKE_GH_REVIEWS",
	} {
		t.Setenv(name, "")
	}
	t.Setenv("AUTOREVIEW_LOG_DIR", filepath.Join(fixtures, "logs"))
	t.Setenv("AUTOREVIEW_LEDGER", filepath.Join(fixtures, "ledger.jsonl"))
	// The settings of the person who runs the tests must not change them.
	t.Setenv("AUTOREVIEW_CONFIG", "off")
	return fixtures
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func autoreview(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errs strings.Builder
	code = run(args, &out, &errs)
	return out.String(), errs.String(), code
}

func TestNamedPRsLimitTheRun(t *testing.T) {
	fixtures := sandbox(t)
	stdout, _, code := autoreview(t, "6", "5", "--jobs=1")

	if code != 0 || !strings.Contains(stdout, "review list: 2 PRs\n") || !strings.Contains(stdout, "\n  #6  ready  ") || !strings.Contains(stdout, "\n  #5  ready  ") {
		t.Errorf("exit %d and stdout:\n%s\nwant only #6 and #5, both ready because they were asked for", code, stdout)
	}
	calls := dashpCalls(t, fixtures)
	if len(calls) != 2 || !strings.HasSuffix(calls[0], "-- /auto-review 5") || !strings.HasSuffix(calls[1], "-- /auto-review 6") {
		t.Errorf("reviews started: %q, want PR #5 and PR #6 and no other", calls)
	}
}

func TestFiltersWidenTheList(t *testing.T) {
	sandbox(t)
	stdout, _, _ := autoreview(t, "--all", "--dependabot")
	for _, want := range []string{"review list: 5 PRs", "#3  ", "#5  "} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout does not contain %q:\n%s", want, stdout)
		}
	}
}

func TestAskingForTheViewOffATerminalSaysSoAndPrintsPlainLines(t *testing.T) {
	sandbox(t)
	stdout, stderr, code := autoreview(t, "--tui")
	if !strings.HasPrefix(stderr, "note: --tui needs a terminal; printing plain lines\n") || code != 0 {
		t.Errorf("stderr %q and exit %d", stderr, code)
	}
	if !strings.Contains(stdout, "review list: 4 PRs") {
		t.Errorf("stdout:\n%s", stdout)
	}
}

func TestAListThatCannotBeReadFailsTheRun(t *testing.T) {
	fixtures := sandbox(t)
	if err := os.Remove(filepath.Join(fixtures, "prs.json")); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := autoreview(t)

	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if !strings.HasPrefix(stderr, "error: could not read the PR list: gh api graphql: fake gh: ") {
		t.Errorf("stderr %q", stderr)
	}
	if strings.Contains(stdout, "review list") || !strings.Contains(stdout, "your PRs: 1 PR") {
		t.Errorf("stdout:\n%s\nwant your PRs and no review list", stdout)
	}
}

func TestOutsideARepoTheRunSaysSoAndExits1(t *testing.T) {
	fixtures := sandbox(t)
	if err := os.Remove(filepath.Join(fixtures, "repo.json")); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := autoreview(t)
	if code != 1 || stdout != "" || !strings.HasPrefix(stderr, "error: not a GitHub repo, or gh is not logged in: ") {
		t.Errorf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

func TestOutsideACheckoutTheRunSaysSoAndExits1(t *testing.T) {
	sandbox(t)
	t.Chdir(t.TempDir())
	t.Setenv("GIT_CEILING_DIRECTORIES", os.TempDir())
	_, stderr, code := autoreview(t)
	if code != 1 || !strings.HasPrefix(stderr, "error: not inside a git checkout") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
}

func TestAnEmptyLoginIsRefused(t *testing.T) {
	sandbox(t)
	t.Setenv("FAKE_GH_LOGIN", "")
	_, stderr, code := autoreview(t)
	if code != 1 || stderr != "error: gh api user returned an empty login\n" {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
}

func TestBadArgumentsExit1WithTheReason(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"--pick"}, "error: unknown flag: --pick\nsee autoreview --help\n"},
		{[]string{"--watch", "5"}, "error: --watch takes its value with \"=\": write --watch=5, or put PR 5 before the flag\nsee autoreview --help\n"},
		{[]string{"--tui", "--headless"}, "error: --tui and --headless cannot be used together\nsee autoreview --help\n"},
	}
	for _, c := range cases {
		stdout, stderr, code := autoreview(t, c.args...)
		if code != 1 || stdout != "" || stderr != c.want {
			t.Errorf("%v: exit %d, stdout %q, stderr %q", c.args, code, stdout, stderr)
		}
	}
}

func TestHelpAndVersion(t *testing.T) {
	stdout, _, code := autoreview(t, "--help")
	if code != 0 || !strings.HasPrefix(stdout, "autoreview: one place to review your team's PRs and handle your own.") {
		t.Errorf("--help: exit %d, stdout %q", code, stdout)
	}
	stdout, _, code = autoreview(t, "--version")
	if code != 0 || stdout != "autoreview dev\n" {
		t.Errorf("--version: exit %d, stdout %q", code, stdout)
	}
}

// The stats report needs no repo and no gh: it reads the ledger and nothing
// else. These run with no sandbox to prove it.
func TestStatsReportsFromALedger(t *testing.T) {
	ledger := filepath.Join("..", "..", "internal", "stats", "testdata", "ledger.jsonl")
	want, err := os.ReadFile(filepath.Join("..", "..", "internal", "stats", "testdata", "plain.txt"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())

	stdout, stderr, code := autoreview(t, "stats", "--ledger", ledger)

	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	// The last line names the ledger by the path it was given.
	body, _, _ := strings.Cut(stdout, "\nledger: ")
	wantBody, _, _ := strings.Cut(string(want), "\nledger: ")
	if body != wantBody {
		t.Errorf("stdout:\n%s\nwant:\n%s", body, wantBody)
	}
	if !strings.HasSuffix(stdout, "ledger: "+ledger+"\n") {
		t.Errorf("the report does not end by naming the ledger: %q", stdout[max(len(stdout)-120, 0):])
	}
}

func TestStatsReadsTheLedgerTheEnvironmentNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "ledger.jsonl")
	t.Setenv("AUTOREVIEW_LEDGER", path)

	stdout, stderr, code := autoreview(t, "stats")
	if code != 0 || stderr != "" || !strings.HasPrefix(stdout, "no reviews recorded yet in "+path) {
		t.Errorf("an empty ledger: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}

	t.Setenv("AUTOREVIEW_LEDGER", "off")
	stdout, stderr, code = autoreview(t, "stats")
	if code != 1 || stdout != "" || !strings.HasPrefix(stderr, "error: the ledger is off") {
		t.Errorf("a ledger that is off: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

func TestStatsIsASubcommandOnlyAsTheFirstArgument(t *testing.T) {
	stdout, _, code := autoreview(t, "stats", "--help")
	if code != 0 || !strings.HasPrefix(stdout, "autoreview stats: ") {
		t.Errorf("stats --help: exit %d, stdout %q", code, stdout)
	}
	_, stderr, code := autoreview(t, "--all", "stats")
	if code != 1 || stderr != "error: stats is a subcommand and goes first: autoreview stats\nsee autoreview --help\n" {
		t.Errorf("--all stats: exit %d, stderr %q", code, stderr)
	}
	_, stderr, code = autoreview(t, "stats", "--bogus")
	if code != 1 || !strings.HasPrefix(stderr, "error: unknown arg: --bogus") {
		t.Errorf("stats --bogus: exit %d, stderr %q", code, stderr)
	}
}
