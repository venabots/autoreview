package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEachReviewFindsItsSkillsAfterTheTempCleanerDeletedThem(t *testing.T) {
	fixtures := sandbox(t)
	found := filepath.Join(fixtures, "skills-found.log")
	// Each review deletes the staged skills once it has started, so the
	// next one finds them only if the run puts them back.
	t.Setenv("FAKE_DASHP_PURGE_SKILLS", found)

	stdout, stderr, code := autoreview(t, "6", "5", "--jobs=1")
	if code != 0 {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	raw, err := os.ReadFile(found)
	if err != nil {
		t.Fatal(err)
	}
	if want := "#5 found 5\n#6 found 5\n"; string(raw) != want {
		t.Errorf("the reviews found:\n%s\nwant each of them handed the five skills:\n%s", raw, want)
	}
}
