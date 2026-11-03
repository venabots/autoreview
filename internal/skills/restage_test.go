package skills

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestStagingAgainPutsBackWhatTheTempCleanerDeleted(t *testing.T) {
	dir := t.TempDir()
	out, err := Stage(Source{Kind: Bundled}, bundle(), dir)
	if err != nil {
		t.Fatal(err)
	}
	want := staged(t, dir)
	root := filepath.Join(dir, ".claude", "skills")
	for _, name := range []string{"auto-review/SKILL.md", "recheck-pr/SKILL.md", "recheck-pr/scripts"} {
		if err := os.RemoveAll(filepath.Join(root, filepath.FromSlash(name))); err != nil {
			t.Fatal(err)
		}
	}

	if err := out.Again(); err != nil {
		t.Fatalf("Again = %v", err)
	}
	if got := staged(t, dir); !slices.Equal(got, want) {
		t.Errorf("staged %v after Again, want %v back", got, want)
	}
	info, err := os.Stat(filepath.Join(root, "recheck-pr", "scripts", "threads.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("the helper script came back with mode %v, want 755", info.Mode().Perm())
	}
}

func TestStagingAgainReplacesAFileAndNeverWritesIntoIt(t *testing.T) {
	dir := t.TempDir()
	out, err := Stage(Source{Kind: Bundled}, bundle(), dir)
	if err != nil {
		t.Fatal(err)
	}
	want := staged(t, dir)
	path := filepath.Join(dir, ".claude", "skills", "auto-review", "SKILL.md")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// A review that started earlier has the file open.
	open, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = open.Close() }()

	if err := out.Again(); err != nil {
		t.Fatalf("Again = %v", err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(before, after) {
		t.Errorf("Again wrote into the file a review has open, want a new file renamed over it")
	}
	if got, _ := io.ReadAll(open); string(got) != "# auto-review\n" {
		t.Errorf("the reader that had the file open read %q, want the whole file", got)
	}
	if got := staged(t, dir); !slices.Equal(got, want) {
		t.Errorf("staged %v, want %v: no temporary file is left beside them", got, want)
	}
}

func TestStagingAgainWritesTheCopyFromTheStartAndNeverReadsTheSource(t *testing.T) {
	source := onDisk(t, map[string]string{"mine/SKILL.md": "# mine\n", "other/SKILL.md": "# other\n"})
	dir := t.TempDir()
	out, err := Stage(Source{Kind: FromDir, Dir: source}, bundle(), dir)
	if err != nil {
		t.Fatal(err)
	}
	// Since the start, someone edited one skill and deleted the other, and
	// the cleaner took the staged copies.
	if err := os.WriteFile(filepath.Join(source, "mine", "SKILL.md"), []byte("# edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(source, "other"), filepath.Join(dir, ".claude")} {
		if err := os.RemoveAll(path); err != nil {
			t.Fatal(err)
		}
	}

	if err := out.Again(); err != nil {
		t.Fatalf("Again = %v", err)
	}
	root := filepath.Join(dir, ".claude", "skills")
	for name, want := range map[string]string{"mine/SKILL.md": "# mine\n", "other/SKILL.md": "# other\n"} {
		if got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name))); string(got) != want {
			t.Errorf("%s holds %q, %v after Again; want %q, the copy the run checked at the start", name, got, err, want)
		}
	}
}

func TestStagingAgainSaysWhereItCouldNotWrite(t *testing.T) {
	dir := t.TempDir()
	out, err := Stage(Source{Kind: Bundled}, bundle(), dir)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, ".claude", "skills")
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := out.Again(); err == nil || !strings.Contains(err.Error(), "making "+root) {
		t.Errorf("Again over a file = %v, want an error that names %s", err, root)
	}
}

func TestStagingNothingWritesNothingAgain(t *testing.T) {
	dir := t.TempDir()
	out, err := Stage(Source{Kind: NotStaged}, bundle(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := out.Again(); err != nil {
		t.Errorf("Again = %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("Again wrote %d entries", len(entries))
	}
}
