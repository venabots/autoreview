package skills

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestRestagePutsBackWhatTheTempCleanerDeleted(t *testing.T) {
	dir := t.TempDir()
	if _, err := Stage(Source{Kind: Bundled}, bundle(), dir); err != nil {
		t.Fatal(err)
	}
	want := staged(t, dir)
	root := filepath.Join(dir, ".claude", "skills")
	for _, name := range []string{"auto-review/SKILL.md", "recheck-pr/SKILL.md", "recheck-pr/scripts/threads.sh"} {
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(name))); err != nil {
			t.Fatal(err)
		}
	}

	if err := Restage(Source{Kind: Bundled}, bundle(), dir, []string{"auto-review", "recheck-pr"}); err != nil {
		t.Fatalf("Restage = %v", err)
	}
	if got := staged(t, dir); !slices.Equal(got, want) {
		t.Errorf("staged %v after Restage, want %v back", got, want)
	}
}

func TestRestageReplacesAFileAndNeverWritesIntoIt(t *testing.T) {
	source := onDisk(t, map[string]string{"mine/SKILL.md": "# first\n"})
	dir := t.TempDir()
	if _, err := Stage(Source{Kind: FromDir, Dir: source}, bundle(), dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".claude", "skills", "mine", "SKILL.md")
	// A review that started earlier has the file open.
	open, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = open.Close() }()
	if err := os.WriteFile(filepath.Join(source, "mine", "SKILL.md"), []byte("# second\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Restage(Source{Kind: FromDir, Dir: source}, bundle(), dir, []string{"mine"}); err != nil {
		t.Fatalf("Restage = %v", err)
	}
	if got, _ := io.ReadAll(open); string(got) != "# first\n" {
		t.Errorf("the reader that had the file open read %q, want the whole first copy", got)
	}
	if got, _ := os.ReadFile(path); string(got) != "# second\n" {
		t.Errorf("SKILL.md holds %q after Restage, want the second copy", got)
	}
	if got := staged(t, dir); !slices.Equal(got, []string{"mine/SKILL.md"}) {
		t.Errorf("staged %v, want SKILL.md alone: no temporary file is left beside it", got)
	}
}

func TestRestageFailsWhenTheSourceNoLongerHoldsAStagedSkill(t *testing.T) {
	source := onDisk(t, map[string]string{"mine/SKILL.md": "# mine\n", "other/SKILL.md": "# other\n"})
	dir := t.TempDir()
	if _, err := Stage(Source{Kind: FromDir, Dir: source}, bundle(), dir); err != nil {
		t.Fatal(err)
	}
	// The source lost the skill, and the cleaner took the staged copy.
	for _, path := range []string{
		filepath.Join(source, "mine", "SKILL.md"),
		filepath.Join(dir, ".claude", "skills", "mine", "SKILL.md"),
	} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}

	err := Restage(Source{Kind: FromDir, Dir: source}, bundle(), dir, []string{"mine", "other"})
	if want := source + " no longer holds mine"; err == nil || err.Error() != want {
		t.Errorf("Restage = %v, want %q", err, want)
	}
}

func TestRestageOfASourceThatStagesNothingNeedsNothing(t *testing.T) {
	dir := t.TempDir()
	if err := Restage(Source{Kind: NotStaged}, bundle(), dir, nil); err != nil {
		t.Errorf("Restage = %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("Restage wrote %d entries", len(entries))
	}
}
