package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/venabots/autoreview/internal/session"
)

// repoRoot is the sandbox checkout as git names it, which is the path the
// session id is derived from. On macOS it differs from the working
// directory by a symlink.
func repoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSuffix(string(out), "\n")
}

func TestOffATerminalTheRunReviewsAndReports(t *testing.T) {
	fixtures := sandbox(t)
	stdout, stderr, code := autoreview(t, "9")

	want := `skills: bundled (dev)
fallback: codex
acme/widgets
review list: 1 PR
  #9  ready  @alice  alice/add-retry-logic  Add retry logic
your PRs: 1 PR
  #4  awaiting review  me/my-own-work  My own work
reviewing 1 PR
logs: <run>/pass-1

start   #9 @alice (reviewing)
done    #9 (Ns)
        not approved yet because:
        - [MEDIUM] (money, irreversible) src/pay.rs:88 — a retried checkout charges the card twice

PR  RESULT  VERDICT    RISK    FINDINGS      TIME  COST   MODEL           SESSION
#9  done    commented  MEDIUM  1 should-fix  Ns    $0.42  claude-fable-5  <session>
#9 not approved yet because:
  - [MEDIUM] (money, irreversible) src/pay.rs:88 — a retried checkout charges the card twice
panel #9: codex (gpt-5.5) 1 finding, top MEDIUM; claude (claude-opus-5.5) clean

logs: <run>/pass-1
reopen any review with: claude --resume <SESSION>
`
	if got := steady(stdout, fixtures); got != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", got, want)
	}
	if stderr != "" || code != 0 {
		t.Errorf("stderr %q and exit %d, want nothing and 0", stderr, code)
	}
}

func TestEveryReadyPRIsReviewedAndTheLimitIsNamedWhenItBinds(t *testing.T) {
	fixtures := sandbox(t)
	stdout, stderr, code := autoreview(t, "--jobs=1")

	if stderr != "" || code != 0 {
		t.Errorf("stderr %q and exit %d", stderr, code)
	}
	got := steady(stdout, fixtures)
	for _, want := range []string{
		"review list: 4 PRs\n", "reviewing 2 PRs, 1 at a time\n",
		"start   #9 @alice (reviewing)\n", "start   #8 @bob (reviewing)\n", "done    #9 (Ns)\n", "done    #8 (Ns)\n",
		"#9  done    commented", "#8  done    commented",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("stdout lacks %q:\n%s", want, got)
		}
	}
	if calls := dashpCalls(t, fixtures); len(calls) != 2 {
		t.Errorf("%d reviews started, want the two ready PRs and not the seen or the approved one", len(calls))
	}
}

func TestTheReviewerIsStartedWithTheSessionTheSkillsAndTheTrailerRequest(t *testing.T) {
	fixtures := sandbox(t)
	_, stderr, _ := autoreview(t, "9", "--budget", "2.50", "--timeout", "600", "--focus", "the ledger migration")

	calls := dashpCalls(t, fixtures)
	if len(calls) != 1 {
		t.Fatalf("reviews started: %q", calls)
	}
	// The fallback is codex, and dash-p forwards no cap to it. A review it
	// takes over runs with no limit, so the run says so up front.
	if want := "note: --budget is not enforced on a review codex takes over; dash-p forwards the cap to claude alone\n"; !strings.Contains(stderr, want) {
		t.Errorf("stderr lacks %q:\n%s", want, stderr)
	}
	root := repoRoot(t)
	for _, want := range []string{
		"-H claude --output-format json --meta-file ",
		"/pass-1/pr-9.meta.json --timeout 600 --dangerously-skip-permissions ",
		"--append-system-prompt=When your reply concludes a PR review task, end it with a fenced code block tagged autoreview",
		"/agent --session-id " + session.ID(root, "acme", "widgets", 9) + " --max-budget-usd=2.50 -- ",
		"--add-dir=" + fixtures + "/logs/run-",
		` -- /auto-review 9 --focus "the ledger migration"`,
	} {
		if !strings.Contains(calls[0], want) {
			t.Errorf("the command line lacks %q:\n%s", want, calls[0])
		}
	}
	staged, err := filepath.Glob(filepath.Join(fixtures, "logs", "run-*", "agent", ".claude", "skills", "*", "SKILL.md"))
	if err != nil || len(staged) != 5 {
		t.Errorf("staged skills: %v, %v; want the five reviewers", staged, err)
	}
	script, err := filepath.Glob(filepath.Join(fixtures, "logs", "run-*", "agent", ".claude", "skills", "recheck-pr", "scripts", "fetch_pr_threads.sh"))
	if err != nil || len(script) != 1 {
		t.Fatalf("the staged helper script: %v, %v", script, err)
	}
	if info, err := os.Stat(script[0]); err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Errorf("the staged helper script is not executable: %v, %v", info.Mode(), err)
	}
}

func TestAFailedReviewFailsTheRun(t *testing.T) {
	fixtures := sandbox(t)
	t.Setenv("FAKE_DASHP_FAIL", "9")
	stdout, stderr, code := autoreview(t, "--jobs=1")

	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if stderr != "error: 1 of 2 reviews failed\n" {
		t.Errorf("stderr %q", stderr)
	}
	got := steady(stdout, fixtures)
	for _, want := range []string{
		"FAILED  #9 (exit 10, Ns): You've hit your session limit · resets 12pm\n",
		"done    #8 (Ns)\n",
		"#9  failed (exit 10)  nothing posted  -",
		"error #9: You've hit your session limit · resets 12pm\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("stdout lacks %q:\n%s", want, got)
		}
	}
}

func TestTheVerdictIsReadBackFromGitHub(t *testing.T) {
	fixtures := sandbox(t)
	t.Setenv("FAKE_GH_REVIEWS", "9:APPROVED")
	stdout, _, code := autoreview(t, "9")

	got := steady(stdout, fixtures)
	if code != 0 || !strings.Contains(got, "#9  done    approved  MEDIUM") {
		t.Errorf("exit %d and stdout:\n%s\nwant GitHub's approval, not the reviewer's own \"commented\"", code, got)
	}
	if strings.Contains(got, "not approved yet because") {
		t.Errorf("an approved PR lists reasons it is not approved:\n%s", got)
	}
}

func TestAnApprovalThatGitHubDoesNotShowIsNotReported(t *testing.T) {
	fixtures := sandbox(t)
	t.Setenv("FAKE_DASHP_DECISION", "approved")
	stdout, stderr, _ := autoreview(t, "9")

	if !strings.Contains(steady(stdout, fixtures), "#9  done    nothing posted  MEDIUM") {
		t.Errorf("stdout:\n%s\nwant no verdict: the reviewer says it approved and GitHub shows nothing", stdout)
	}
	if !strings.Contains(stderr, `note: PR #9: the reviewer reported "approved", but GitHub shows no review from you`) {
		t.Errorf("stderr %q", stderr)
	}
}

func TestNoPostRunsTheSkillThatPostsNothing(t *testing.T) {
	fixtures := sandbox(t)
	t.Setenv("FAKE_GH_REVIEWS", "9:APPROVED")
	stdout, stderr, code := autoreview(t, "9", "--no-post")

	calls := dashpCalls(t, fixtures)
	if len(calls) != 1 || !strings.HasSuffix(calls[0], "-- /panel-review 9") {
		t.Errorf("reviews started: %q", calls)
	}
	got := steady(stdout, fixtures)
	if code != 0 || stderr != "" || !strings.Contains(got, "#9  done    nothing posted  MEDIUM") {
		t.Errorf("exit %d, stderr %q, stdout:\n%s", code, stderr, got)
	}
	if !strings.HasSuffix(got, "nothing was posted to any PR; the reviews are in <run>/pass-1\n") {
		t.Errorf("stdout does not end by saying where the reviews are:\n%s", got)
	}
	review, err := filepath.Glob(filepath.Join(fixtures, "logs", "run-*", "pass-1", "pr-9.review.md"))
	if err != nil || len(review) != 1 {
		t.Fatalf("the review file: %v, %v", review, err)
	}
	text, _ := os.ReadFile(review[0])
	if !strings.HasPrefix(string(text), "Reviewed PR 9.\n") || strings.Contains(string(text), "autoreview") {
		t.Errorf("the review file holds %q, want the review without its trailer", text)
	}
}

func TestContinueResumesTheEarlierSession(t *testing.T) {
	fixtures := sandbox(t)
	root := repoRoot(t)
	id := session.ID(root, "acme", "widgets", 9)
	project := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "projects", "-some-project")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, project, id+".jsonl", "")

	stdout, _, _ := autoreview(t, "9", "--continue")

	calls := dashpCalls(t, fixtures)
	if len(calls) != 1 || !strings.Contains(calls[0], "--resume "+id) || !strings.HasSuffix(calls[0], "-- /recheck-pr 9") {
		t.Errorf("reviews started: %q, want the earlier session resumed for a re-check", calls)
	}
	if !strings.Contains(stdout, "start   #9 @alice (rechecking)\n") {
		t.Errorf("stdout:\n%s", stdout)
	}

	autoreview(t, "9")
	calls = dashpCalls(t, fixtures)
	if strings.Contains(calls[1], "--resume") || strings.Contains(calls[1], "--session-id") || !strings.HasSuffix(calls[1], "-- /auto-review 9") {
		t.Errorf("without --continue the run started %q, want a fresh review and no session flag: the id is taken", calls[1])
	}
}

func TestEachReviewThatReportedOnItselfGoesInTheLedger(t *testing.T) {
	fixtures := sandbox(t)
	t.Setenv("FAKE_DASHP_NO_TRAILER", "8")
	t.Setenv("FAKE_GH_REVIEWS", "9:CHANGES_REQUESTED")
	_, _, code := autoreview(t, "--jobs=1")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}

	raw, err := os.ReadFile(filepath.Join(fixtures, "ledger.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("the ledger holds %d lines, want one: PR #8 reported no trailer", len(lines))
	}
	for _, want := range []string{
		`"source":"autoreview"`, `"repo":"acme/widgets"`, `"pr":9`, `"decision":"changes-requested"`, `"risk":"MEDIUM"`,
		`"counts":{"must_fix":0,"should_fix":1,"polish":0}`, `"cost_usd":0.42`, `"driver_model":"claude-fable-5"`,
		`"panel":[{"name":"codex","model":"gpt-5.5","ok":true,"findings":1,"top":"MEDIUM"},{"name":"claude","model":"claude-opus-5.5","ok":true,"findings":0}]`,
		`"reviewed":true`,
	} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("the record lacks %s:\n%s", want, lines[0])
		}
	}
	if !strings.Contains(lines[0], `"findings":[{`) {
		t.Errorf("the record holds no finding read from the review text:\n%s", lines[0])
	}

	stdout, _, code := autoreview(t, "stats")
	if code != 0 || !strings.Contains(stdout, "1 review on ") || !strings.Contains(stdout, "gpt-5.5") {
		t.Errorf("stats after the run: exit %d\n%s", code, stdout)
	}
}

func TestALedgerThatIsOffRecordsNothing(t *testing.T) {
	fixtures := sandbox(t)
	t.Setenv("AUTOREVIEW_LEDGER", "off")
	if _, stderr, code := autoreview(t, "9"); code != 0 || stderr != "" {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(fixtures, "ledger.jsonl")); err == nil {
		t.Error("a ledger was written")
	}
}

func TestNothingReadyIsNotAnError(t *testing.T) {
	fixtures := sandbox(t)
	stdout, stderr, code := autoreview(t, "4")
	if code != 0 || stderr != "" || !strings.HasSuffix(stdout, "review list: 0 PRs\nyour PRs: 1 PR\n  #4  awaiting review  me/my-own-work  My own work\nno PR is ready for review\n") {
		t.Errorf("exit %d, stderr %q, stdout:\n%s", code, stderr, stdout)
	}
	if calls := dashpCalls(t, fixtures); len(calls) != 0 {
		t.Errorf("reviews started: %q", calls)
	}
}

func TestAnOverrideReplacesTheReviewer(t *testing.T) {
	fixtures := sandbox(t)
	t.Setenv("AUTOREVIEW_CMD", `echo "session=$REVIEW_PRS_SESSION_ID resume=$REVIEW_PRS_SESSION_RESUME reviewed"`)
	stdout, _, code := autoreview(t, "9")

	if code != 0 || !strings.HasPrefix(stdout, "skills: not used by $AUTOREVIEW_CMD\n") {
		t.Errorf("exit %d, stdout:\n%s", code, stdout)
	}
	if calls := dashpCalls(t, fixtures); len(calls) != 0 {
		t.Errorf("dash-p was started under an override: %q", calls)
	}
	review, _ := filepath.Glob(filepath.Join(fixtures, "logs", "run-*", "pass-1", "pr-9.review.md"))
	if len(review) != 1 {
		t.Fatalf("the review file: %v", review)
	}
	text, _ := os.ReadFile(review[0])
	if got := steady(string(text), fixtures); got != "session=<session> resume=0 reviewed 9\n" {
		t.Errorf("the override's review is %q, want the PR number appended and the session in its environment", got)
	}
}

func TestSkillsInstalledStagesNothing(t *testing.T) {
	fixtures := sandbox(t)
	stdout, _, _ := autoreview(t, "9", "--skills", "installed")
	if !strings.HasPrefix(stdout, "skills: installed\n") {
		t.Errorf("stdout:\n%s", stdout)
	}
	if calls := dashpCalls(t, fixtures); len(calls) != 1 || strings.Contains(calls[0], "--add-dir") {
		t.Errorf("reviews started: %q, want no staged directory", calls)
	}
}

func TestAnInstalledSkillThatShadowsAStagedOneIsNamed(t *testing.T) {
	sandbox(t)
	installed := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "skills", "auto-review")
	if err := os.MkdirAll(installed, 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, installed, "SKILL.md", "# mine\n")
	_, stderr, code := autoreview(t, "9")
	want := "note: auto-review under " + filepath.Dir(installed) + " shadow the staged copies; the installed skills run\n"
	if code != 0 || stderr != want {
		t.Errorf("exit %d, stderr %q, want %q", code, stderr, want)
	}
}

func TestAMissingProgramIsNamedBeforeAnyReview(t *testing.T) {
	cases := []struct {
		name, remove, want string
		args               []string
	}{
		{"dash-p", "dash-p", "error: dash-p is not installed (brew install venabots/tap/dash-p), and $DASHP_BIN names no other\n", nil},
		{"the orchestrator", "claude", "error: claude is not installed, and it runs the reviews (--orchestrator chooses another)\n", nil},
		{"codex's skills", "", "error: codex has no auto-review, panel-review, recheck-pr skill; install the review skills under ~/.agents/skills\n", []string{"--orchestrator", "codex"}},
		{"a skills directory", "", `error: --skills expects a directory of skills (<name>/SKILL.md) or 'installed', got "/no/such/dir"` + "\n", []string{"--skills", "/no/such/dir"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fixtures := sandbox(t)
			// A home with no codex skills in it.
			t.Setenv("HOME", t.TempDir())
			bin := strings.Split(os.Getenv("PATH"), string(os.PathListSeparator))[0]
			if c.remove != "" {
				if err := os.Remove(filepath.Join(bin, c.remove)); err != nil {
					t.Fatal(err)
				}
				t.Setenv("PATH", bin+string(os.PathListSeparator)+"/usr/bin"+string(os.PathListSeparator)+"/bin")
			}
			_, stderr, code := autoreview(t, c.args...)
			if code != 1 || stderr != c.want {
				t.Errorf("exit %d, stderr %q, want %q", code, stderr, c.want)
			}
			if calls := dashpCalls(t, fixtures); len(calls) != 0 {
				t.Errorf("reviews started: %q", calls)
			}
		})
	}
}

func TestTheRunKeepsItsOwnLog(t *testing.T) {
	fixtures := sandbox(t)
	stdout, _, _ := autoreview(t, "9")
	logs, err := filepath.Glob(filepath.Join(fixtures, "logs", "run-*", "autoreview.log"))
	if err != nil || len(logs) != 1 {
		t.Fatalf("the run log: %v, %v", logs, err)
	}
	raw, err := os.ReadFile(logs[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != stdout {
		t.Errorf("the run log holds:\n%s\nwant what the run printed:\n%s", raw, stdout)
	}
}

func TestAnInterruptStopsTheReviewAndExits130(t *testing.T) {
	fixtures := sandbox(t)
	pidFile := filepath.Join(fixtures, "review.pid")
	t.Setenv("FAKE_DASHP_HANG", pidFile)

	type outcome struct {
		stdout, stderr string
		code           int
	}
	done := make(chan outcome, 1)
	go func() {
		stdout, stderr, code := autoreview(t, "9")
		done <- outcome{stdout, stderr, code}
	}()

	var reviewer int
	for range 500 {
		if raw, err := os.ReadFile(pidFile); err == nil && len(raw) > 0 {
			reviewer, _ = strconv.Atoi(string(raw))
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if reviewer == 0 {
		t.Fatal("the review never started")
	}
	began := time.Now()
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	got := <-done

	if got.code != 130 {
		t.Errorf("exit %d, want 130", got.code)
	}
	if took := time.Since(began); took > 10*time.Second {
		t.Errorf("the run took %v to stop", took)
	}
	if !strings.Contains(got.stderr, "\ninterrupted; stopping running reviews\n") || !strings.Contains(got.stderr, "error: 1 of 1 review failed\n") {
		t.Errorf("stderr %q", got.stderr)
	}
	out := steady(got.stdout, fixtures)
	if !strings.Contains(out, "FAILED  #9 (stopped, Ns)\n") || !strings.Contains(out, "#9  failed (stopped)  nothing posted") {
		t.Errorf("stdout:\n%s", out)
	}
	for range 100 {
		if err := syscall.Kill(reviewer, 0); err != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("the reviewer, pid %d, outlived the run", reviewer)
}

func TestAReviewWhoseAgentFailsIsRetriedUnderTheOther(t *testing.T) {
	fixtures := sandbox(t)
	t.Setenv("FAKE_DASHP_FAIL", "9:claude")
	stdout, stderr, code := autoreview(t, "9")

	if code != 0 || stderr != "" {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
	got := steady(stdout, fixtures)
	for _, want := range []string{
		"RETRY   #9 (claude exit 10: You've hit your session limit · resets 12pm · retrying with codex)\n",
		"start   #9 @alice (reviewing with codex)\n",
		"PR #9: claude failed (exit 10: You've hit your session limit · resets 12pm); reviewed with codex instead\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("stdout lacks %q:\n%s", want, got)
		}
	}
	calls := dashpCalls(t, fixtures)
	if len(calls) != 2 || !strings.HasPrefix(calls[0], "-H claude ") || !strings.HasPrefix(calls[1], "-H codex ") {
		t.Fatalf("reviews started: %d, want claude then codex", len(calls))
	}
	if !strings.Contains(calls[1], "-- $auto-review 9\\n\\nWhen your reply concludes a PR review task, end it with a fenced code block tagged autoreview") {
		t.Errorf("the codex retry does not carry the skill and then the trailer request in its prompt:\n%s", calls[1])
	}
	if logs, _ := filepath.Glob(filepath.Join(fixtures, "logs", "run-*", "pass-1", "pr-9.claude.log")); len(logs) != 1 {
		t.Errorf("the failed attempt's log was not set aside: %v", logs)
	}
}

func TestFallbackNoneLeavesAFailedReviewFailed(t *testing.T) {
	fixtures := sandbox(t)
	t.Setenv("FAKE_DASHP_FAIL", "9:claude")
	stdout, _, code := autoreview(t, "9", "--fallback", "none")
	if code != 1 || !strings.HasPrefix(stdout, "skills: bundled (dev)\nfallback: none\n") || len(dashpCalls(t, fixtures)) != 1 {
		t.Errorf("exit %d, %d reviews, stdout:\n%s", code, len(dashpCalls(t, fixtures)), stdout)
	}
}

func TestAnAutomaticFallbackWithoutItsSkillsIsDroppedAndSaysWhy(t *testing.T) {
	sandbox(t)
	t.Setenv("HOME", t.TempDir())
	stdout, _, code := autoreview(t, "9")
	if code != 0 || !strings.Contains(stdout, "fallback: none (codex has no auto-review, panel-review, recheck-pr skill; install the review skills under ~/.agents/skills)\n") {
		t.Errorf("exit %d, stdout:\n%s", code, stdout)
	}
}

func TestAReviewMarksItsPRWithAReactionWhileItRuns(t *testing.T) {
	fixtures := sandbox(t)
	t.Setenv("FAKE_DASHP_FAIL", "8")
	autoreview(t, "9", "8", "--fallback=none")
	calls := reactionCalls(t, fixtures)
	for _, pr := range []string{"9", "8"} {
		on := "api repos/acme/widgets/issues/" + pr + "/reactions --method POST -f content=eyes --jq .id"
		off := "api repos/acme/widgets/issues/" + pr + "/reactions/77 --method DELETE"
		if !slices.Contains(calls, on) || !slices.Contains(calls, off) {
			t.Errorf("PR #%s: reaction calls %q, want it on and then off, a failed review too", pr, calls)
		}
	}
}

func TestNoPostLeavesNoReaction(t *testing.T) {
	fixtures := sandbox(t)
	autoreview(t, "9", "--no-post")
	if calls := reactionCalls(t, fixtures); len(calls) != 0 {
		t.Errorf("--no-post touched the PR: %q", calls)
	}
}
