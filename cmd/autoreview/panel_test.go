package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAPanelReviewsABranchWithEveryBackendOnPath(t *testing.T) {
	b := panelSandbox(t)
	stdout, stderr, code := b.panel("--base", "main")

	if code != 0 {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	contains(t, "stdout", stdout,
		"# Panel review\n\n- Target: 1 commit on feature vs main\n",
		"- Panelists: codex, claude, opencode\n",
		"## codex / codex-model (exit 0)", "## opencode / opencode-model (exit 0)",
		"[LOW] a.rs:1 — nit",
		"# Synthesis\n\n### Overview\n\n**Reviewing:** synthesized\n\n---\n3 of 3 answered · outputs: `")
	// What it is doing before the first report lands. A checkout for each
	// panelist is the longest wait before any model is asked anything.
	contains(t, "stderr", stderr, "reading the repo\n", "building the diff\n",
		"materializing worktree 1 of 4\n", "materializing worktree 4 of 4\n",
		"panel: codex (codex-model) done in ")
	lacks(t, "stdout", stdout, "reading the repo", "materializing")

	// The prompt never rides in the argv: a large diff would fail the exec
	// on Linux, and macOS would never show it.
	lacks(t, "the dash-p calls", b.calls(), "Code review request")
	contains(t, "codex's prompt", b.prompt("codex"), "# Code review request", "1 commit on feature vs main", "+fn main() {}")

	// A committed target gets write and exec in a worktree of its own. The
	// synthesis reads a tree no panelist touched, read-only.
	contains(t, "the dash-p calls", b.calls(), "--dangerously-skip-permissions", "/worktree-codex", "/worktree-synthesis --perms=read-only", "--harness=claude --output-format=text --timeout=600 --cwd=")
	if n := b.worktrees(); n != 1 {
		t.Errorf("%d worktrees are listed after the run, want only the checkout", n)
	}
}

func TestAFinishedPanelIsRecordedInTheLedger(t *testing.T) {
	b := panelSandbox(t)
	if _, stderr, code := b.panel("--base", "main"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	ledger := b.read("ledger.jsonl")
	contains(t, "the ledger", ledger,
		`"source":"panel"`, `"repo":"`+filepath.Base(b.repo)+`"`,
		`"name":"codex","model":"codex-model","ok":true,"findings":1,"top":"LOW","duration_secs":`,
		`"exit_code":0`, `"reviewed":true`)
	if n := strings.Count(ledger, "\n"); n != 1 {
		t.Errorf("%d ledger lines, want one for the run", n)
	}

	t.Setenv("AUTOREVIEW_LEDGER", "off")
	b.panel("--base", "main")
	if n := strings.Count(b.read("ledger.jsonl"), "\n"); n != 1 {
		t.Errorf("a run with the ledger off was recorded")
	}
}

func TestUncommittedWorkIsReviewedReadOnlyInThePersonsTree(t *testing.T) {
	b := panelSandbox(t)
	write(t, b.repo, "a.rs", "fn main() {}\nfn other() {}\n")
	write(t, b.repo, "notes.txt", "scratch\n")

	stdout, stderr, code := b.panel("--uncommitted", "--focus", "the new function")

	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	contains(t, "stdout", stdout, "- Target: uncommitted changes on feature\n", "- Focus: the new function\n")
	contains(t, "the dash-p calls", b.calls(), "--perms=read-only")
	lacks(t, "the dash-p calls", b.calls(), "--dangerously-skip-permissions", "worktree-")
	contains(t, "codex's prompt", b.prompt("codex"), "**read-only**", "## Reviewer focus\n\nthe new function", "- `notes.txt`", "+fn other() {}")
	lacks(t, "stderr", stderr, "materializing")
	if n := b.worktrees(); n != 1 {
		t.Errorf("%d worktrees for uncommitted work", n)
	}
}

func TestAPanelistThatReturnsNothingIsTriedOnceMore(t *testing.T) {
	b := panelSandbox(t)
	t.Setenv("FAKE_PANEL_EMPTY", "codex")
	stdout, stderr, code := b.panel("--base", "main")
	if code != 0 {
		t.Errorf("exit %d", code)
	}
	contains(t, "stderr", stderr, "panel: codex produced nothing (no output (exit 1): transient blip); trying once more\n")
	contains(t, "stdout", stdout, "## codex / codex-model (exit 0)", "3 of 3 answered")
}

func TestAPanelistThatNeverAnswersIsReportedAndTheRunGoesOn(t *testing.T) {
	b := panelSandbox(t)
	t.Setenv("FAKE_PANEL_ALWAYS_EMPTY", "codex")
	stdout, _, code := b.panel("--base", "main")
	if code != 0 {
		t.Errorf("exit %d", code)
	}
	contains(t, "stdout", stdout, "## codex / unknown (exit 1)\n\nFAILED: no output (exit 1): quota exceeded\n", "## claude / claude-model", "2 of 3 answered")
	contains(t, "the synthesis prompt", b.synthesisPrompt(), "Do not count them toward consensus:\n\n- codex (unknown): no output (exit 1): quota exceeded\n")
}

func TestAReportWithABadExitIsCountedAndFlagged(t *testing.T) {
	b := panelSandbox(t)
	t.Setenv("FAKE_PANEL_DIRTY_EXIT", "codex")
	stdout, _, code := b.panel("--base", "main")
	if code != 0 {
		t.Errorf("exit %d", code)
	}
	contains(t, "stdout", stdout, "3 of 3 answered", "[LOW] a.rs:1 — nit", "## codex / codex-model (exit 3)\n\nFAILED: exited 3 after producing output\n")
	synth := b.synthesisPrompt()
	contains(t, "the synthesis prompt", synth, "did not exit cleanly. Count them, and treat a report that stops mid-sentence as truncated:\n\n- codex (codex-model): exited 3 after producing output\n")
	lacks(t, "the synthesis prompt", synth, "These panelists contributed nothing")
}

func TestAPanelWhereNobodyAnsweredExits1AndLeavesNoWorktree(t *testing.T) {
	b := panelSandbox(t)
	t.Setenv("FAKE_PANEL_ALWAYS_EMPTY", "codex claude opencode")
	stdout, stderr, code := b.panel("--base", "main")
	if code != 1 {
		t.Errorf("exit %d", code)
	}
	contains(t, "stderr", stderr, "error: no panelist returned a review; there is nothing to synthesize\n  what each one did is in ")
	lacks(t, "stdout", stdout, "# Synthesis")
	if b.read("synthesis-prompt-path") != "" {
		t.Error("a synthesis ran with nothing to synthesize")
	}
	if n := b.worktrees(); n != 1 {
		t.Errorf("%d worktrees after a failed panel", n)
	}
	contains(t, "the ledger", b.read("ledger.jsonl"), `"ok":false`, `"reviewed":false`)
}

func TestNoSynthesisStopsAfterThePanel(t *testing.T) {
	b := panelSandbox(t)
	stdout, stderr, code := b.panel("--base", "main", "--no-synthesis")
	if code != 0 {
		t.Errorf("exit %d", code)
	}
	contains(t, "stderr", stderr, "materializing worktree 3 of 3\n", "panel: --no-synthesis, stopping after 3 reports\n")
	lacks(t, "stderr", stderr, "of 4")
	lacks(t, "stdout", stdout, "# Synthesis")
	lacks(t, "the dash-p calls", b.calls(), "worktree-synthesis")
}

func TestAWedgedPanelistIsKilledAndTheRestAreReported(t *testing.T) {
	if testing.Short() {
		t.Skip("waits out the one-second timeout and the five-second grace")
	}
	b := panelSandbox(t)
	t.Setenv("FAKE_PANEL_HANG", "codex")
	stdout, _, code := b.panel("--base", "main", "--timeout", "1")
	if code != 0 {
		t.Errorf("exit %d", code)
	}
	contains(t, "stdout", stdout, "FAILED: timed out and had to be killed", "## claude / claude-model")
	if pid := b.read("hang-pid"); pid == "" || processAlive(t, pid) {
		t.Errorf("the wedged panelist (pid %q) outlived the run", pid)
	}
	if n := b.worktrees(); n != 1 {
		t.Errorf("%d worktrees after a killed panelist", n)
	}
}

func processAlive(t *testing.T, pid string) bool {
	t.Helper()
	return exec.Command("kill", "-0", pid).Run() == nil
}

func TestNothingToReviewSaysWhyAndHowToIncludeNewFiles(t *testing.T) {
	b := panelSandbox(t)
	_, stderr, code := b.panel("--staged")
	if code != 1 || stderr != "reading the repo\nbuilding the diff\nerror: nothing to review: the diff for staged changes on feature is empty\n" {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
	write(t, b.repo, "new-file.txt", "untracked\n")
	_, stderr, _ = b.panel("--staged")
	contains(t, "stderr", stderr, "1 file is not tracked by git yet, so no diff covers them: new-file.txt. To review them, add them with `git add` first\n")
	// A base target needs a commit, not a stage: the advice follows it.
	_, stderr, _ = b.panel("--base", "HEAD")
	contains(t, "stderr", stderr, "To review them, commit them first\n")
}

func TestATreeAuditNamesTheFilesAndNotADiff(t *testing.T) {
	b := panelSandbox(t)
	stdout, stderr, code := b.panel("--tree")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	contains(t, "stdout", stdout, "- Target: 1 file at ")
	contains(t, "stderr", stderr, "listing the files\n")
	prompt := b.prompt("codex")
	contains(t, "codex's prompt", prompt, "# Code audit request", "- `a.rs`")
	lacks(t, "codex's prompt", prompt, "## Diff")
	contains(t, "the dash-p calls", b.calls(), "/worktree-codex")
	contains(t, "the synthesis prompt", b.synthesisPrompt(), "Panel synthesis request: code audit")
	if n := b.worktrees(); n != 1 {
		t.Errorf("%d worktrees after an audit", n)
	}
}

func TestATreePathNarrowsTheAudit(t *testing.T) {
	b := panelSandbox(t)
	if err := os.Mkdir(filepath.Join(b.repo, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(b.repo, "sub"), "b.rs", "fn b() {}\n")
	b.git("add", "sub/b.rs")
	b.git("commit", "--quiet", "-m", "add sub/b.rs")

	stdout, _, _ := b.panel("--tree", "sub")
	contains(t, "stdout", stdout, "- Target: 1 file under sub at ")
	contains(t, "codex's prompt", b.prompt("codex"), "Only the files under `sub` are in scope.")
	lacks(t, "codex's prompt", b.prompt("codex"), "- `a.rs`")

	// A relative path is read from where the person stands.
	t.Chdir(filepath.Join(b.repo, "sub"))
	stdout, _, _ = b.panel("--tree", ".")
	contains(t, "stdout from sub/", stdout, "- Target: 1 file under sub at ")
	t.Chdir(b.repo)

	cases := []struct{ path, want string }{
		{"no-such-dir", `error: nothing to review: no file under "no-such-dir" at HEAD`},
		// An absolute path not on disk is still inside the repository.
		{filepath.Join(b.repo, "gone"), `error: nothing to review: no file under "gone" at HEAD`},
		// Pathspec magic is a name like any other.
		{":(top)sub", `error: nothing to review: no file under ":(top)sub" at HEAD`},
		{"..", `error: --tree: ".." is outside this repository`},
	}
	for _, c := range cases {
		_, stderr, code := b.panel("--tree", c.path)
		if code != 1 || !strings.Contains(stderr, c.want) {
			t.Errorf("--tree %s: exit %d, stderr %q", c.path, code, stderr)
		}
	}

	// A submodule is a gitlink with an empty directory in each worktree.
	head, err := exec.Command("git", "-C", b.repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	b.git("update-index", "--add", "--cacheinfo", "160000,"+strings.TrimSpace(string(head))+",sub/vendored")
	b.git("commit", "--quiet", "-m", "add a submodule")
	stdout, _, _ = b.panel("--tree", "sub")
	contains(t, "stdout", stdout, "- Target: 1 file under sub at ")
	lacks(t, "codex's prompt", b.prompt("codex"), "sub/vendored")
}

func TestABadBaseIsRefusedWithItsCause(t *testing.T) {
	b := panelSandbox(t)
	cases := []struct{ base, want string }{
		{"--output=/tmp/pwned", `error: --base expects a ref, and "--output=/tmp/pwned" starts with a dash` + "\n"},
		{"no-such-ref", `error: --base: no commit named "no-such-ref" in this repository` + "\n"},
	}
	for _, c := range cases {
		_, stderr, code := b.panel("--base", c.base)
		if code != 1 || !strings.HasSuffix(stderr, c.want) {
			t.Errorf("--base %s: exit %d, stderr %q", c.base, code, stderr)
		}
	}
	if b.calls() != "" {
		t.Errorf("a refused base started panelists:\n%s", b.calls())
	}
}

func TestPanelFlagsAreCheckedBeforeAnythingRuns(t *testing.T) {
	b := panelSandbox(t)
	cases := []struct {
		args     []string
		want     string
		wantHelp bool
	}{
		{[]string{"--nope"}, "error: unknown arg: --nope\n", true},
		{[]string{"--timeout", "0"}, "error: --timeout expects an integer >= 1, got \"0\"\n", false},
		{[]string{"--panelist", "gpt4"}, "error: unknown panelist backend \"gpt4\" (expected one of: codex, claude, opencode)\n", false},
		{[]string{"--synth", "claude:"}, "error: --synth \"claude:\" names no model after the colon\n", false},
	}
	for _, c := range cases {
		stdout, stderr, code := b.panel(c.args...)
		if code != 1 || stdout != "" || !strings.HasPrefix(stderr, c.want) || strings.Contains(stderr, "Usage: autoreview panel") != c.wantHelp {
			t.Errorf("%q: exit %d, stdout %q, stderr %q", c.args, code, stdout, stderr)
		}
	}
	t.Setenv("PANEL_TIMEOUT", "soon")
	if _, stderr, code := b.panel(); code != 1 || stderr != "error: --timeout expects an integer >= 1, got \"soon\"\n" {
		t.Errorf("a bad $PANEL_TIMEOUT: exit %d, stderr %q", code, stderr)
	}
}

func TestAMissingProgramIsNamedWithHowToInstallIt(t *testing.T) {
	b := panelSandbox(t)
	if err := os.Remove(filepath.Join(b.bin, "opencode")); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := b.panel("--panelist", "opencode")
	if code != 1 || stderr != "error: missing required command: opencode\n  install with: brew install opencode\n" {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
	// The synthesizer is checked before any panelist is paid for.
	_, stderr, code = b.panel("--panelist", "codex", "--synth", "opencode")
	if code != 1 || !strings.HasPrefix(stderr, "error: missing required command: opencode\n") || b.calls() != "" {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
	t.Setenv("DASHP_BIN", filepath.Join(b.bin, "no-dash-p"))
	_, stderr, _ = b.panel()
	if !strings.HasSuffix(stderr, "/no-dash-p\n  install with: brew install venabots/tap/dash-p, or set $DASHP_BIN\n") {
		t.Errorf("stderr %q", stderr)
	}
}

func TestWithNoBackendOnPathThePanelSaysWhatItLookedFor(t *testing.T) {
	b := panelSandbox(t)
	for _, name := range []string{"codex", "claude", "opencode"} {
		if err := os.Remove(filepath.Join(b.bin, name)); err != nil {
			t.Fatal(err)
		}
	}
	_, stderr, code := b.panel()
	if code != 1 || stderr != "error: no backend CLIs found on PATH (looked for codex, claude, opencode)\n" {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
}

func TestPanelIsASubcommandOnlyAsTheFirstArgument(t *testing.T) {
	stdout, _, code := autoreview(t, "panel", "--help")
	if code != 0 || !strings.HasPrefix(stdout, "autoreview panel: review one change with several models at once") {
		t.Errorf("panel --help: exit %d, stdout %q", code, stdout)
	}
	stdout, _, code = autoreview(t, "panel", "--version")
	if code != 0 || stdout != "autoreview panel dev\n" {
		t.Errorf("panel --version: exit %d, stdout %q", code, stdout)
	}
	_, stderr, code := autoreview(t, "--all", "panel")
	if code != 1 || stderr != "error: panel is a subcommand and goes first: autoreview panel\nsee autoreview --help\n" {
		t.Errorf("--all panel: exit %d, stderr %q", code, stderr)
	}
	stdout, _, _ = autoreview(t, "--help")
	if !strings.Contains(stdout, "autoreview panel [") {
		t.Error("autoreview --help does not mention the panel subcommand")
	}
}

func TestThePanelInTheSettingsIsSeatedWhenNoFlagNamesOne(t *testing.T) {
	b := panelSandbox(t)
	settingsFile(t, "[panel]\npanelists = [\"codex\", \"opencode:glm-5.2\"]\nsynth = \"opencode\"\n")

	stdout, stderr, code := b.panel("--base", "main")
	if code != 0 {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	contains(t, "stdout", stdout, "- Panelists: codex, opencode-glm-5.2\n")
	contains(t, "the dash-p calls", b.calls(), "--harness=opencode --output-format=text")
	lacks(t, "the dash-p calls", b.calls(), "--harness=claude")

	settingsFile(t, "[panel]\npanelists = [\"gemini\"]\n")
	if _, stderr, code := b.panel("--base", "main"); code != 1 || !strings.Contains(stderr, "panel.panelists in ") {
		t.Errorf("a bad panelist in the settings: exit %d, stderr %q", code, stderr)
	}
	if _, _, code := b.panel("--help"); code != 0 {
		t.Errorf("--help with a bad settings file: exit %d", code)
	}
}

func TestAnEmptySynthesizerInTheSettingsStopsThePanelAsItStopsAReview(t *testing.T) {
	b := panelSandbox(t)
	settingsFile(t, "[panel]\nsynth = \"\"\n")
	if _, stderr, code := b.panel("--base", "main"); code != 1 || !strings.Contains(stderr, "panel.synth in ") {
		t.Errorf("an empty synth: exit %d, stderr %q", code, stderr)
	}
}
