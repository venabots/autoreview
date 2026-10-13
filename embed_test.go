package autoreview

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestTheBinaryCarriesTheFiveReviewers(t *testing.T) {
	entries, err := fs.ReadDir(Skills(), ".")
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, entry := range entries {
		if _, err := fs.Stat(Skills(), entry.Name()+"/SKILL.md"); err != nil {
			t.Errorf("%s is bundled without a SKILL.md: an agent would not find it", entry.Name())
		}
		names = append(names, entry.Name())
	}
	want := []string{"approve-pr", "auto-post-panel-review-comments", "auto-review", "panel-review", "recheck-pr"}
	if !slices.Equal(names, want) {
		t.Errorf("the bundle holds %v, want %v", names, want)
	}
}

// The embedded tree carries no file modes, so staging gives the execute
// bit back by name: every .sh file gets it. A helper that is executable on
// disk under another name would be staged without the bit, and the skill
// that runs it would fail in the middle of a review.
func TestEveryExecutableHelperIsNamedDotSh(t *testing.T) {
	err := filepath.WalkDir("skills", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&0o111 != 0 && !strings.HasSuffix(path, ".sh") {
			t.Errorf("%s is executable and is not named .sh", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The prompts are v1's, byte for byte: a panel run in either version must
// ask the same question, or the ledger compares two different reviews.
func TestTheBinaryCarriesV1sFourPanelPrompts(t *testing.T) {
	for _, name := range []string{"review.md", "audit.md", "synthesis.md", "audit-synthesis.md"} {
		onDisk, err := os.ReadFile(filepath.Join("prompts", name))
		if err != nil {
			t.Fatal(err)
		}
		if Prompt(name) != string(onDisk) {
			t.Errorf("the embedded %s differs from prompts/%s", name, name)
		}
	}
}
