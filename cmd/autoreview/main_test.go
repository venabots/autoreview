package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain lets the test binary stand in for gh. A test links the binary
// into a directory as `gh` and puts that directory first on PATH, so the
// code under test runs a real process with a real argv and no fake script
// has to be kept in step with it.
func TestMain(m *testing.M) {
	if filepath.Base(os.Args[0]) == "gh" {
		os.Exit(fakeGh(os.Args[1:]))
	}
	os.Exit(m.Run())
}

// fakeGh answers the calls autoreview makes, from the files in
// $FAKE_GH_DIR. A call it does not know fails loudly: a new gh call needs
// an answer here before its test can pass.
func fakeGh(args []string) int {
	dir := os.Getenv("FAKE_GH_DIR")
	joined := strings.Join(args, " ")
	switch {
	case joined == "repo view --json owner,name":
		return serve(dir, "repo.json")
	case joined == "api user --jq .login":
		fmt.Println(os.Getenv("FAKE_GH_LOGIN"))
		return 0
	case strings.HasPrefix(joined, "api graphql ") && strings.Contains(joined, "search("):
		return serve(dir, "mine.json")
	case strings.HasPrefix(joined, "api graphql -F owner=acme -F name=widgets "):
		return serve(dir, "prs.json")
	}
	fmt.Fprintf(os.Stderr, "fake gh: unhandled: %s\n", firstChars(joined, 80))
	return 1
}

func serve(dir, name string) int {
	raw, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		fmt.Fprintf(os.Stderr, "fake gh: %v\n", err)
		return 1
	}
	_, _ = os.Stdout.Write(raw)
	return 0
}

func firstChars(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

const mineJSON = `{"data":{"search":{"nodes":[
  {"number":4,"title":"My own work","isDraft":false,"headRefName":"me/my-own-work",
   "headRefOid":"sha4","updatedAt":"2026-08-08T10:00:00Z","isCrossRepository":false,
   "reviewDecision":"REVIEW_REQUIRED","mergeable":"MERGEABLE",
   "latestReviews":{"nodes":[]},"reviewThreads":{"nodes":[]},
   "headCommit":{"nodes":[{"commit":{"statusCheckRollup":{"state":"SUCCESS"}}}]}}
]}}}`

// sandbox makes a git checkout to run in and a GitHub to ask: the seven
// PRs the prs package is tested against, one of each kind.
func sandbox(t *testing.T) (fixtures string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.Symlink(self, filepath.Join(bin, "gh")); err != nil {
		t.Fatal(err)
	}
	fixtures = t.TempDir()
	prs, err := os.ReadFile(filepath.Join("..", "..", "internal", "prs", "testdata", "prs.json"))
	if err != nil {
		t.Fatal(err)
	}
	write(t, fixtures, "prs.json", string(prs))
	write(t, fixtures, "mine.json", mineJSON)
	write(t, fixtures, "repo.json", `{"owner":{"login":"acme"},"name":"widgets"}`)

	repo := t.TempDir()
	if out, err := exec.Command("git", "-C", repo, "init", "--quiet").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	t.Chdir(repo)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_GH_DIR", fixtures)
	t.Setenv("FAKE_GH_LOGIN", "me")
	t.Setenv("AUTOREVIEW_WATCH_INTERVAL", "")
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

func TestOffATerminalTheRunPrintsBothListsAndExits(t *testing.T) {
	sandbox(t)
	stdout, stderr, code := autoreview(t)

	want := `acme/widgets
review list: 4 PRs
  #9  ready     @alice  alice/add-retry-logic  Add retry logic
  #8  ready     @bob    bob/fix-typo           Fix typo
  #6  seen      @carol  carol/refactor-client  Refactor client
  #5  approved  @dave   dave/approved-already  Approved already
your PRs: 1 PR
  #4  awaiting review  me/my-own-work  My own work
`
	if stdout != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", stdout, want)
	}
	if stderr != "" || code != 0 {
		t.Errorf("stderr %q and exit %d, want nothing and 0", stderr, code)
	}
}

func TestNamedPRsLimitTheRun(t *testing.T) {
	sandbox(t)
	stdout, _, code := autoreview(t, "6", "5", "--headless")

	if code != 0 || !strings.Contains(stdout, "review list: 2 PRs\n") || !strings.Contains(stdout, "\n  #6  ready  ") || !strings.Contains(stdout, "\n  #5  ready  ") {
		t.Errorf("exit %d and stdout:\n%s\nwant only #6 and #5, both ready because they were asked for", code, stdout)
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
	if stderr != "note: --tui needs a terminal; printing plain lines\n" || code != 0 {
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
