package autoreview

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A merge of the base during a review moved the head, and the review gave
// no verdict on a change that nobody touched. head-moved.sh tells that merge
// from one that carries new code. These tests make real commits in a real
// repository, because the answer is what git's own merge says.

// The skills that compare a head with the reviewed one. Each installs on
// its own, so each carries a copy of the script.
var headMovedSkills = []string{"auto-review", "auto-post-panel-review-comments", "recheck-pr"}

// The fake gh answers for PR 9 of acme/widgets, whose head is $FAKE_HEAD.
const fakeHeadGh = `#!/usr/bin/env bash
printf '%s\n' "$*" >>"$FAKE_GH_LOG"
if [[ "$1 $2 $3" == "pr view 9" && -n "${FAKE_HEAD:-}" ]]; then
  printf 'https://github.com/acme/widgets/pull/9\t%s\tmain\n' "$FAKE_HEAD"
  exit 0
fi
echo "fake gh: unexpected call: $*" >&2
exit 1
`

// world is the PR's repository, a clone where its commits are made and the
// clone the review runs in.
type world struct {
	t        *testing.T
	env      []string
	builder  string
	remote   string
	reviewer string
	bin      string
}

func newWorld(t *testing.T) *world {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("head-moved.sh needs git")
	}
	root := t.TempDir()
	w := &world{
		t:        t,
		builder:  filepath.Join(root, "builder"),
		remote:   filepath.Join(root, "acme", "widgets.git"),
		reviewer: filepath.Join(root, "reviewer"),
		bin:      filepath.Join(root, "bin"),
	}
	// The machine's own git settings, a signing key say, stay out of it.
	w.env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=a", "GIT_AUTHOR_EMAIL=a@example.com",
		"GIT_COMMITTER_NAME=a", "GIT_COMMITTER_EMAIL=a@example.com",
		"FAKE_GH_LOG="+filepath.Join(root, "gh.log"),
		"PATH="+w.bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := os.MkdirAll(w.bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w.bin, "gh"), []byte(fakeHeadGh), 0o755); err != nil {
		t.Fatal(err)
	}
	w.git(root, "init", "-q", "--bare", "-b", "main", w.remote)
	w.git(root, "init", "-q", "-b", "main", w.builder)
	w.write("a.txt", "one\ntwo\nthree\n")
	w.write("b.txt", "x\n")
	w.commit("the base")
	w.git(w.builder, "push", "-q", w.remote, "main")
	w.git(root, "clone", "-q", w.remote, w.reviewer)
	w.git(w.builder, "checkout", "-q", "-b", "pr", "main")
	return w
}

func (w *world) git(dir string, args ...string) string {
	w.t.Helper()
	out, err := w.try(dir, args...)
	if err != nil {
		w.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

func (w *world) try(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = w.env
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func (w *world) write(name, content string) {
	w.t.Helper()
	if err := os.WriteFile(filepath.Join(w.builder, name), []byte(content), 0o644); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) commit(msg string) string {
	w.t.Helper()
	w.git(w.builder, "add", "-A")
	w.git(w.builder, "commit", "-q", "-m", msg)
	return w.git(w.builder, "rev-parse", "HEAD")
}

// reviewed makes the PR's change, the commit the panel read.
func (w *world) reviewed() string {
	w.t.Helper()
	w.write("a.txt", "ONE\ntwo\nthree\n")
	return w.commit("the reviewed change")
}

// onBase moves the base on by one commit and comes back to where it was.
func (w *world) onBase(name, content string) {
	w.t.Helper()
	back := w.git(w.builder, "rev-parse", "HEAD")
	w.git(w.builder, "checkout", "-q", "main")
	w.write(name, content)
	w.commit("a change on the base")
	w.git(w.builder, "checkout", "-q", back)
}

func (w *world) merge(ref string) string {
	w.t.Helper()
	w.git(w.builder, "merge", "-q", "--no-ff", "--no-edit", ref)
	return w.git(w.builder, "rev-parse", "HEAD")
}

// mergeAs merges ref and commits the tree with name set to content: the
// resolution of a conflict, or an edit folded into the merge.
func (w *world) mergeAs(ref, name, content string) string {
	w.t.Helper()
	_, _ = w.try(w.builder, "merge", "-q", "--no-ff", "--no-commit", ref)
	w.write(name, content)
	w.git(w.builder, "add", "-A")
	w.git(w.builder, "commit", "-q", "--no-edit")
	return w.git(w.builder, "rev-parse", "HEAD")
}

// headMoved publishes the base and the head and runs the script in the
// reviewer's clone.
func (w *world) headMoved(head string, args ...string) (out, errOut string, code int) {
	w.t.Helper()
	w.git(w.builder, "push", "-q", "-f", w.remote, "main:refs/heads/main", head+":refs/pull/9/head")
	cmd := exec.Command("bash", append([]string{filepath.Join(w.wd(), "skills", "auto-review", "scripts", "head-moved.sh")}, args...)...)
	cmd.Dir = w.reviewer
	cmd.Env = append(w.env, "FAKE_HEAD="+head)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		code = exit.ExitCode()
	} else if err != nil {
		w.t.Fatal(err)
	}
	return stdout.String(), stderr.String(), code
}

func (w *world) wd() string {
	w.t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		w.t.Fatal(err)
	}
	return wd
}

func TestOnlyACleanMergeOfTheBaseLeavesTheReviewedChangeInPlace(t *testing.T) {
	cases := []struct {
		name string
		// build makes the commits and returns the reviewed commit and the head.
		build func(w *world) (reviewed, head string)
		want  string
	}{
		{"a head that is the reviewed commit is the same", func(w *world) (string, string) {
			r := w.reviewed()
			return r, r
		}, "same"},
		{"a clean merge of the base is a base merge", func(w *world) (string, string) {
			r := w.reviewed()
			w.onBase("b.txt", "y\n")
			return r, w.merge("main")
		}, "base-merge"},
		{"two clean merges of the base are a base merge", func(w *world) (string, string) {
			r := w.reviewed()
			w.onBase("b.txt", "y\n")
			w.merge("main")
			w.onBase("c.txt", "z\n")
			return r, w.merge("main")
		}, "base-merge"},
		{"a merge whose conflict was resolved by hand has moved", func(w *world) (string, string) {
			r := w.reviewed()
			w.onBase("a.txt", "uno\ntwo\nthree\n")
			return r, w.mergeAs("main", "a.txt", "UNO\ntwo\nthree\n")
		}, "moved"},
		{"a clean merge with an edit folded in has moved", func(w *world) (string, string) {
			r := w.reviewed()
			w.onBase("b.txt", "y\n")
			return r, w.mergeAs("main", "a.txt", "ONE\nTWO\nthree\n")
		}, "moved"},
		{"a new commit has moved", func(w *world) (string, string) {
			r := w.reviewed()
			w.write("a.txt", "ONE\nTWO\nthree\n")
			return r, w.commit("more")
		}, "moved"},
		{"a commit after a clean merge of the base has moved", func(w *world) (string, string) {
			r := w.reviewed()
			w.onBase("b.txt", "y\n")
			w.merge("main")
			w.write("a.txt", "ONE\nTWO\nthree\n")
			return r, w.commit("more")
		}, "moved"},
		{"a merge of a branch outside the base has moved", func(w *world) (string, string) {
			r := w.reviewed()
			w.git(w.builder, "checkout", "-q", "-b", "side", "main")
			w.write("c.txt", "z\n")
			w.commit("a side branch")
			w.git(w.builder, "checkout", "-q", "pr")
			return r, w.merge("side")
		}, "moved"},
		{"a head force-pushed past the reviewed commit has moved", func(w *world) (string, string) {
			r := w.reviewed()
			w.git(w.builder, "checkout", "-q", "-b", "redo", "main")
			w.write("a.txt", "Un\ntwo\nthree\n")
			return r, w.commit("another change")
		}, "moved"},
		{"a merge that puts the base first has moved", func(w *world) (string, string) {
			r := w.reviewed()
			w.onBase("b.txt", "y\n")
			w.git(w.builder, "checkout", "-q", "--detach", "main")
			return r, w.merge("pr")
		}, "moved"},
		{"a head rebuilt below the reviewed commit has moved", func(w *world) (string, string) {
			r := w.reviewed()
			w.git(w.builder, "checkout", "-q", "main")
			w.merge("pr")
			w.git(w.builder, "checkout", "-q", "--detach", "main~1")
			return r, w.merge("main")
		}, "moved"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t)
			reviewed, head := c.build(w)
			out, errOut, code := w.headMoved(head, "9", reviewed)
			if code != 0 || out != c.want+"\n" {
				t.Errorf("exit %d, %q, want %q\n%s", code, out, c.want, errOut)
			}
		})
	}
}

func TestHeadMovedReadsTheReviewedCommitByItsShortSha(t *testing.T) {
	w := newWorld(t)
	r := w.reviewed()
	w.onBase("b.txt", "y\n")
	head := w.merge("main")
	if out, errOut, code := w.headMoved(head, "9", r[:7]); code != 0 || out != "base-merge\n" {
		t.Errorf("exit %d, %q\n%s", code, out, errOut)
	}
}

func TestHeadMovedSaysWhenItCannotTell(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no arguments", nil, "usage:"},
		{"a ref that would read as an option", []string{"--repo=x", "abcdef1"}, "not a PR number or URL"},
		{"a reviewed commit that is not a sha", []string{"9", "HEAD"}, "not a commit sha"},
		{"a PR that gh cannot find", []string{"404", "abcdef1"}, "could not resolve PR '404'"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t)
			out, errOut, code := w.headMoved(w.reviewed(), c.args...)
			if code != 1 || out != "" || !strings.Contains(errOut, c.want) {
				t.Errorf("exit %d, stdout %q, stderr %q, want %q", code, out, errOut, c.want)
			}
		})
	}
}

func TestEverySkillThatComparesHeadsRunsTheSameHeadMovedScript(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("skills", headMovedSkills[0], "scripts", "head-moved.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, skill := range headMovedSkills {
		got, err := os.ReadFile(filepath.Join("skills", skill, "scripts", "head-moved.sh"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s carries a head-moved.sh that differs from %s's", skill, headMovedSkills[0])
		}
		doc, err := os.ReadFile(filepath.Join("skills", skill, "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(doc), "scripts/head-moved.sh") {
			t.Errorf("%s's SKILL.md does not say to run scripts/head-moved.sh", skill)
		}
	}
}
