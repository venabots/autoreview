package panel

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestATreePathIsReadFromWhereThePersonStands(t *testing.T) {
	cases := []struct{ prefix, path, want string }{
		{"", "src", "src"},
		{"", "src/panel/", "src/panel"},
		{"src/", "panel", "src/panel"},
		{"src/", ".", "src"},
		{"src/panel/", "../bin", "src/bin"},
		{"", "./src//panel", "src/panel"},
		// The root itself is the whole repository, which has no scope.
		{"", ".", ""},
		{"src/", "..", ""},
	}
	for _, c := range cases {
		got, err := scopeWithin(c.prefix, c.path, c.path)
		if err != nil || got != c.want {
			t.Errorf("scopeWithin(%q, %q) = %q, %v; want %q", c.prefix, c.path, got, err, c.want)
		}
	}
}

func TestATreePathOutsideTheRepositoryIsRefused(t *testing.T) {
	_, err := scopeWithin("", "..", "..")
	if err == nil || err.Error() != `--tree: ".." is outside this repository` {
		t.Errorf("error %v", err)
	}
	if _, err := scopeWithin("src/", "../../x", "../../x"); err == nil {
		t.Error("../../x from src/ was let through")
	}
}

func TestAnAbsolutePathIsMatchedAgainstTheRootByWholeDirectories(t *testing.T) {
	cases := []struct {
		path, root, want string
		ok               bool
	}{
		{"/repo/src", "/repo", "src", true},
		{"/repo", "/repo", "", true},
		{"/repository/src", "/repo", "", false},
		{"/elsewhere", "/repo", "", false},
	}
	for _, c := range cases {
		got, ok := within(c.path, c.root)
		if got != c.want || ok != c.ok {
			t.Errorf("within(%q, %q) = %q, %v", c.path, c.root, got, ok)
		}
	}
}

func TestOnlyFilesAreListedNotSubmodules(t *testing.T) {
	// A submodule is a gitlink, and its worktree directory is empty, so no
	// panelist could read it.
	raw := "100644 blob aaa\tsrc/a.rs\x00160000 commit bbb\tvendor/lib\x00100755 blob ccc\tbin/run\x00"
	if got := blobPaths(raw); !slices.Equal(got, []string{"src/a.rs", "bin/run"}) {
		t.Errorf("blobPaths = %q", got)
	}
	// A name may hold a tab; only the first one ends the metadata.
	if got := blobPaths("100644 blob aaa\ta\tb.rs\x00"); !slices.Equal(got, []string{"a\tb.rs"}) {
		t.Errorf("blobPaths = %q", got)
	}
}

func TestAnUntrackedNameThatIsOnlySpaceIsKept(t *testing.T) {
	if got := nulFields("a\x00 \x00\x00b\x00"); !slices.Equal(got, []string{"a", " ", "b"}) {
		t.Errorf("nulFields = %q", got)
	}
}

func TestALongListSaysHowManyItLeftOut(t *testing.T) {
	files := make([]string, 12)
	for i := range files {
		files[i] = string(rune('a' + i))
	}
	if got := summarize(files); got != "a, b, c, d, e, f, g, h, i, j, and 2 more" {
		t.Errorf("summarize = %q", got)
	}
	if got := summarize([]string{"new\x1b[2J.txt"}); got != "new[2J.txt" {
		t.Errorf("a name reaches the terminal as %q", got)
	}
}

// fakeGit answers git calls from a table keyed by the joined argv. A call
// it has no answer for fails, as git would for a command it cannot run.
type fakeGit map[string]string

func (f fakeGit) run(_ context.Context, _ string, args ...string) ([]byte, error) {
	out, ok := f[strings.Join(args, " ")]
	if !ok {
		return nil, errors.New("fatal: no answer for git " + strings.Join(args, " "))
	}
	return []byte(out), nil
}

func TestAnEmptyDiffSaysWhatWouldHaveBeenInIt(t *testing.T) {
	base := fakeGit{
		"rev-parse --abbrev-ref HEAD": "feat\n",
		"diff HEAD":                   "",
		"diff --cached":               "",
		"rev-parse --verify --quiet main^{commit}": "abc\n",
		"diff main...HEAD":                         "",
		"rev-parse HEAD":                           "abc\n",
		"rev-list --count main..HEAD":              "0\n",
		"ls-files --others --exclude-standard -z":  "",
	}
	withNew := fakeGit{}
	for k, v := range base {
		withNew[k] = v
	}
	withNew["ls-files --others --exclude-standard -z"] = "new.txt\x00"
	cases := []struct {
		name   string
		git    fakeGit
		target Target
		want   string
	}{
		{"nothing at all", base, Target{Kind: Staged}, "nothing to review: the diff for staged changes on feat is empty"},
		{"an untracked file, staged", withNew, Target{Kind: Staged},
			"nothing to review: the diff for staged changes on feat is empty. 1 file is not tracked by git yet, so no diff covers them: new.txt. To review them, add them with `git add` first"},
		{"an untracked file, uncommitted", withNew, Target{Kind: Uncommitted}, "add them with `git add` first"},
		{"an untracked file against a base", withNew, Target{Kind: Base, Ref: "main"}, "To review them, commit them first"},
	}
	for _, c := range cases {
		_, err := ResolveTarget(context.Background(), c.git.run, c.target, "/repo", "/repo")
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %v, want %q", c.name, err, c.want)
		}
	}
}

func TestABaseIsCheckedBeforeGitSeesIt(t *testing.T) {
	git := fakeGit{"rev-parse --abbrev-ref HEAD": "feat\n"}
	_, err := ResolveTarget(context.Background(), git.run, Target{Kind: Base, Ref: "--output=/tmp/pwned"}, "/repo", "/repo")
	if err == nil || err.Error() != `--base expects a ref, and "--output=/tmp/pwned" starts with a dash` {
		t.Errorf("error %v", err)
	}
	_, err = ResolveTarget(context.Background(), git.run, Target{Kind: Base, Ref: "nope"}, "/repo", "/repo")
	if err == nil || err.Error() != `--base: no commit named "nope" in this repository` {
		t.Errorf("error %v", err)
	}
}

func TestABaseTargetIsIsolatedAndCountsItsCommits(t *testing.T) {
	git := fakeGit{
		"rev-parse --abbrev-ref HEAD":              "feat\n",
		"rev-parse --verify --quiet main^{commit}": "abc\n",
		"diff main...HEAD":                         "diff --git a b\n",
		"rev-parse HEAD":                           "0123456789\n",
		"rev-list --count main..HEAD":              "3\n",
	}
	r, err := ResolveTarget(context.Background(), git.run, Target{Kind: Base, Ref: "main"}, "/repo", "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if r.Label != "3 commits on feat vs main" || !r.Isolated || r.SHA != "0123456789" || len(r.Untracked) != 0 {
		t.Errorf("resolved %+v", r)
	}
}

func TestAnUncommittedTargetReadsTheTreeAndNamesUntrackedFiles(t *testing.T) {
	git := fakeGit{
		"rev-parse --abbrev-ref HEAD":             "main\n",
		"diff HEAD":                               "diff --git a b\n",
		"ls-files --others --exclude-standard -z": "src/new.rs\x00",
	}
	r, err := ResolveTarget(context.Background(), git.run, Target{Kind: Uncommitted}, "/repo", "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if r.Label != "uncommitted changes on main" || r.Isolated || r.SHA != "" || !slices.Equal(r.Untracked, []string{"src/new.rs"}) {
		t.Errorf("resolved %+v", r)
	}
}

func TestAFailedUntrackedListStopsTheRun(t *testing.T) {
	// A review that quietly leaves out every new file is worse than one
	// that does not start.
	git := fakeGit{"rev-parse --abbrev-ref HEAD": "main\n", "diff HEAD": "diff\n"}
	_, err := ResolveTarget(context.Background(), git.run, Target{Kind: Uncommitted}, "/repo", "/repo")
	if err == nil || !strings.HasPrefix(err.Error(), "git ls-files --others --exclude-standard -z failed: ") {
		t.Errorf("error %v", err)
	}
}

func TestATreeAuditNamesTheCommitAndTheScope(t *testing.T) {
	git := fakeGit{
		"rev-parse --abbrev-ref HEAD":                                           "main\n",
		"rev-parse --verify --quiet HEAD^{commit}":                              "abcdef0123\n",
		"rev-parse --show-prefix":                                               "src/\n",
		"--literal-pathspecs ls-tree -r -z --full-tree abcdef0123 -- src/panel": "100644 blob x\tsrc/panel/a.go\x00",
		"--literal-pathspecs ls-tree -r -z --full-tree abcdef0123":              "",
	}
	r, err := ResolveTarget(context.Background(), git.run, Target{Kind: Tree, Path: "panel"}, "/repo", "/repo/src")
	if err != nil {
		t.Fatal(err)
	}
	if r.Label != "1 file under src/panel at abcdef0 on main" || !r.Subject.Audit || r.Subject.Scope != "src/panel" || r.SHA != "abcdef0123" {
		t.Errorf("resolved %+v", r)
	}
	_, err = ResolveTarget(context.Background(), git.run, Target{Kind: Tree}, "/repo", "/repo")
	if err == nil || err.Error() != "nothing to review: HEAD has no files" {
		t.Errorf("an empty HEAD: %v", err)
	}
	_, err = ResolveTarget(context.Background(), fakeGit{}.run, Target{Kind: Tree}, "/repo", "/repo")
	if err == nil || err.Error() != "--tree: this repository has no commits yet" {
		t.Errorf("no commits: %v", err)
	}
}

func TestAnEndedContextIsNotReportedAsAMissingCommit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ResolveTarget(ctx, fakeGit{}.run, Target{Kind: Base, Ref: "main"}, "/repo", "/repo")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error %v, want the context's", err)
	}
}
