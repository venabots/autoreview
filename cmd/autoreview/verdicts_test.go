package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func calledWith(t *testing.T, fixtures, part string) bool {
	t.Helper()
	return slices.ContainsFunc(dashpCalls(t, fixtures), func(call string) bool { return strings.Contains(call, part) })
}

func teachVerdicts(t *testing.T, dir string) {
	t.Helper()
	for _, skill := range []string{"auto-review", "recheck-pr"} {
		write(t, filepath.Join(dir, skill), "SKILL.md", "# "+skill+"\n\n--verdicts approve-only | comment-only\n")
	}
}

// The bundled skills know the level and claude runs them. The automatic
// fallback is codex, whose own copies in the sandbox are too old: it is
// dropped and says why, as a fallback that lacks the skills is.
func TestAVerdictLevelReachesTheReviewAndDropsAFallbackThatDoesNotKnowIt(t *testing.T) {
	fixtures := sandbox(t)
	settingsFile(t, "[review]\nverdicts = \"comment-only\"\n")
	stdout, stderr, code := autoreview(t, "9")

	if code != 0 {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	copy := filepath.Join(os.Getenv("HOME"), ".agents", "skills", "auto-review", "SKILL.md")
	if want := "fallback: none (review.verdicts is comment-only, but " + copy + " does not know --verdicts"; !strings.Contains(stdout, want) {
		t.Errorf("stdout lacks %q:\n%s", want, stdout)
	}
	if !calledWith(t, fixtures, "-- /auto-review 9 --verdicts comment-only") {
		t.Errorf("the review was not told the level: %q", dashpCalls(t, fixtures))
	}
}

func TestAnAgentWhoseOwnCopyDoesNotKnowTheLevelIsRefusedBeforeItStarts(t *testing.T) {
	fixtures := sandbox(t)
	settingsFile(t, "[review]\nverdicts = \"approve-only\"\n")
	agents := filepath.Join(os.Getenv("HOME"), ".agents", "skills")

	_, stderr, code := autoreview(t, "--orchestrator", "codex", "9")
	if code != 1 || !strings.Contains(stderr, filepath.Join(agents, "auto-review", "SKILL.md")+" does not know --verdicts") {
		t.Fatalf("an old codex copy: exit %d, stderr %q", code, stderr)
	}
	if calls := dashpCalls(t, fixtures); len(calls) != 0 {
		t.Errorf("a review ran with a copy that does not know the level: %q", calls)
	}

	teachVerdicts(t, agents)
	stdout, stderr, code := autoreview(t, "--orchestrator", "codex", "9")
	if code != 0 || !calledWith(t, fixtures, "$auto-review 9 --verdicts approve-only") {
		t.Errorf("a codex copy that knows the level: exit %d, stdout %q, stderr %q, calls %q", code, stdout, stderr, dashpCalls(t, fixtures))
	}

	// An installed claude copy wins over the staged one, so it is the one
	// checked.
	shadow := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "skills")
	write(t, mkdir(t, filepath.Join(shadow, "auto-review")), "SKILL.md", "# auto-review\n")
	if _, stderr, code := autoreview(t, "9"); code != 1 || !strings.Contains(stderr, filepath.Join(shadow, "auto-review", "SKILL.md")) {
		t.Errorf("an old claude copy: exit %d, stderr %q", code, stderr)
	}
}

func mkdir(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}
