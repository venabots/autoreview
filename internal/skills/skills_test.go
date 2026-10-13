package skills

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// bundle is a small tree shaped like the real one: two skills, a helper
// script, and two things beside them that are not skills.
func bundle() fs.FS {
	return fstest.MapFS{
		"auto-review/SKILL.md":          {Data: []byte("# auto-review\n")},
		"recheck-pr/SKILL.md":           {Data: []byte("# recheck-pr\n")},
		"recheck-pr/scripts/threads.sh": {Data: []byte("#!/bin/sh\necho threads\n")},
		"recheck-pr/evals/cases.json":   {Data: []byte("{}\n")},
		"not-a-skill/README.md":         {Data: []byte("no manifest here\n")},
		"README.md":                     {Data: []byte("the skills\n")},
	}
}

// onDisk writes files under a new directory and returns it.
func onDisk(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func staged(t *testing.T, dir string) []string {
	t.Helper()
	root := filepath.Join(dir, ".claude", "skills")
	found := []string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(root, path)
			found = append(found, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(found)
	return found
}

func TestParse(t *testing.T) {
	withSkills := onDisk(t, map[string]string{"mine/SKILL.md": "x"})
	without := onDisk(t, map[string]string{"notes/README.md": "x"})
	missing := filepath.Join(t.TempDir(), "nowhere")

	cases := []struct {
		name, value string
		want        Source
		wantErr     string
	}{
		{"nothing given is the bundle", "", Source{Kind: Bundled}, ""},
		{"the word installed stages nothing", "installed", Source{Kind: NotStaged}, ""},
		{"a directory of skills", withSkills, Source{Kind: FromDir, Dir: withSkills}, ""},
		{"a directory with no skill", without, Source{}, "holds none"},
		{"a path that is not there", missing, Source{}, "expects a directory of skills (<name>/SKILL.md) or 'installed'"},
		{"a file", filepath.Join(withSkills, "mine", "SKILL.md"), Source{}, "expects a directory of skills"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Parse(c.value)
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("Parse error = %v, want one containing %q", err, c.wantErr)
				}
				return
			}
			if err != nil || got != c.want {
				t.Errorf("Parse = %+v, %v; want %+v", got, err, c.want)
			}
		})
	}
}

func TestDescribeSaysWhichCopyRuns(t *testing.T) {
	cases := []struct {
		source Source
		want   string
	}{
		{Source{Kind: Bundled}, "bundled (1.2.3)"},
		{Source{Kind: FromDir, Dir: "/src/skills"}, "/src/skills"},
		{Source{Kind: NotStaged}, "installed"},
	}
	for _, c := range cases {
		if got := c.source.Describe("1.2.3"); got != c.want {
			t.Errorf("Describe = %q, want %q", got, c.want)
		}
	}
}

func TestNamesAreTheSkillsASourceStages(t *testing.T) {
	dir := onDisk(t, map[string]string{"zeta/SKILL.md": "x", "alpha/SKILL.md": "x", "notes/README.md": "x"})
	cases := []struct {
		name   string
		source Source
		want   []string
	}{
		{"the bundle", Source{Kind: Bundled}, []string{"auto-review", "recheck-pr"}},
		{"a directory, sorted", Source{Kind: FromDir, Dir: dir}, []string{"alpha", "zeta"}},
		{"nothing staged", Source{Kind: NotStaged}, []string{}},
	}
	for _, c := range cases {
		if got := c.source.Names(bundle()); !slices.Equal(got, c.want) {
			t.Errorf("%s: Names = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestStageWritesTheBundleWhereTheAgentLooks(t *testing.T) {
	dir := t.TempDir()
	ok, err := Stage(Source{Kind: Bundled}, bundle(), dir)
	if err != nil || !ok {
		t.Fatalf("Stage = %v, %v", ok, err)
	}

	want := []string{"auto-review/SKILL.md", "recheck-pr/SKILL.md", "recheck-pr/evals/cases.json", "recheck-pr/scripts/threads.sh"}
	if got := staged(t, dir); !slices.Equal(got, want) {
		t.Errorf("staged %v, want %v: the skills and nothing beside them", got, want)
	}
	script := filepath.Join(dir, ".claude", "skills", "recheck-pr", "scripts", "threads.sh")
	if info, err := os.Stat(script); err != nil || info.Mode().Perm() != 0o755 {
		t.Errorf("the helper script has mode %v, %v; want 755: the embedded tree carries no modes", info.Mode().Perm(), err)
	}
	manifestFile := filepath.Join(dir, ".claude", "skills", "auto-review", "SKILL.md")
	if info, err := os.Stat(manifestFile); err != nil || info.Mode().Perm() != 0o644 {
		t.Errorf("SKILL.md has mode %v, %v; want 644", info.Mode().Perm(), err)
	}
	if got, _ := os.ReadFile(manifestFile); string(got) != "# auto-review\n" {
		t.Errorf("SKILL.md holds %q", got)
	}
}

func TestStageCopiesADirectoryInPlaceOfTheBundle(t *testing.T) {
	source := onDisk(t, map[string]string{
		"mine/SKILL.md":       "# mine\n",
		"mine/run":            "#!/bin/sh\n",
		"mine/.git/HEAD":      "ref: refs/heads/main\n",
		"notes/README.md":     "not a skill\n",
		"real/tuned/SKILL.md": "# tuned\n",
	})
	if err := os.Chmod(filepath.Join(source, "mine", "run"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(source, "real", "tuned"), filepath.Join(source, "linked")); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	ok, err := Stage(Source{Kind: FromDir, Dir: source}, bundle(), dir)
	if err != nil || !ok {
		t.Fatalf("Stage = %v, %v", ok, err)
	}

	want := []string{"linked/SKILL.md", "mine/SKILL.md", "mine/run"}
	if got := staged(t, dir); !slices.Equal(got, want) {
		t.Errorf("staged %v, want %v: a link is followed, and .git and the bundle are left out", got, want)
	}
	run := filepath.Join(dir, ".claude", "skills", "mine", "run")
	if info, err := os.Stat(run); err != nil || info.Mode().Perm() != 0o755 {
		t.Errorf("a script that is executable on disk was staged with mode %v, %v", info.Mode().Perm(), err)
	}
	linked := filepath.Join(dir, ".claude", "skills", "linked")
	if info, err := os.Lstat(linked); err != nil || !info.IsDir() {
		t.Errorf("the linked skill was staged as %v, %v; want a real directory", info.Mode(), err)
	}
}

func TestStageWritesNothingWhenTheReviewerUsesWhatIsInstalled(t *testing.T) {
	dir := t.TempDir()
	ok, err := Stage(Source{Kind: NotStaged}, bundle(), dir)
	if err != nil || ok {
		t.Fatalf("Stage = %v, %v; want nothing staged", ok, err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("Stage wrote %d entries", len(entries))
	}
}

func TestStageSaysWhereItCouldNotWrite(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Stage(Source{Kind: Bundled}, bundle(), file); err == nil || !strings.Contains(err.Error(), "making") {
		t.Errorf("Stage under a file: %v", err)
	}
}

func TestAddDirFlagIsOneToken(t *testing.T) {
	if got := AddDirFlag("/logs/run 1/agent"); got != "--add-dir=/logs/run 1/agent" {
		t.Errorf("AddDirFlag = %q", got)
	}
}

func TestAnInstalledSkillShadowsAStagedOne(t *testing.T) {
	user := onDisk(t, map[string]string{"auto-review/SKILL.md": "x", "other/SKILL.md": "x", "recheck-pr/README.md": "x"})
	repo := onDisk(t, map[string]string{"recheck-pr/SKILL.md": "x", "panel-review/SKILL.md": "x"})
	empty := t.TempDir()
	names := []string{"auto-review", "panel-review", "recheck-pr"}

	if got := Shadowed(user, names); !slices.Equal(got, []string{"auto-review"}) {
		t.Errorf("Shadowed = %v, want only the staged name that has a SKILL.md there", got)
	}
	if got := Shadowed(empty, names); len(got) != 0 {
		t.Errorf("Shadowed in an empty directory = %v", got)
	}

	want := "auto-review under " + user + " and panel-review, recheck-pr under " + repo + " shadow the staged copies; the installed skills run"
	if got := ShadowNote([]string{user, empty, repo}, names); got != want {
		t.Errorf("ShadowNote = %q, want %q", got, want)
	}
	if got := ShadowNote([]string{empty}, names); got != "" {
		t.Errorf("ShadowNote with nothing shadowed = %q", got)
	}
	if got := ShadowNote([]string{user, repo}, nil); got != "" {
		t.Errorf("ShadowNote with nothing staged = %q", got)
	}
}
