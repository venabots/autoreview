package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// panelBox is a checkout to run `autoreview panel` in: a commit on main and
// a branch one commit ahead, so `--base main` has a diff. Its PATH holds the
// fake dash-p, the three backends and git, and nothing else, so what is
// found on it is what the test put there.
type panelBox struct {
	t    *testing.T
	bin  string
	dir  string
	repo string
}

func panelSandbox(t *testing.T) panelBox {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	for _, name := range []string{"dash-p", "codex", "claude", "opencode"} {
		if err := os.Symlink(self, filepath.Join(bin, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(git, filepath.Join(bin, "git")); err != nil {
		t.Fatal(err)
	}
	b := panelBox{t: t, bin: bin, dir: t.TempDir(), repo: t.TempDir()}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	b.git("init", "--quiet", "-b", "main")
	b.git("commit", "--quiet", "--allow-empty", "-m", "init")
	b.git("checkout", "--quiet", "-b", "feature")
	write(t, b.repo, "a.rs", "fn main() {}\n")
	b.git("add", "a.rs")
	b.git("commit", "--quiet", "-m", "add a.rs")

	t.Chdir(b.repo)
	t.Setenv("PATH", bin)
	t.Setenv("FAKE_PANEL_DIR", b.dir)
	t.Setenv("AUTOREVIEW_LEDGER", filepath.Join(b.dir, "ledger.jsonl"))
	for _, name := range []string{"DASHP_BIN", "PANEL_TIMEOUT", "PANEL_SYNTH", "FAKE_PANEL_EMPTY", "FAKE_PANEL_ALWAYS_EMPTY", "FAKE_PANEL_HANG", "FAKE_PANEL_DIRTY_EXIT"} {
		t.Setenv(name, "")
	}
	return b
}

func (b panelBox) git(args ...string) {
	b.t.Helper()
	full := append([]string{"-C", b.repo, "-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)
	if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
		b.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// panel runs `autoreview panel --log-dir <box>/logs args...` after it clears
// what the last run left in the box.
func (b panelBox) panel(args ...string) (stdout, stderr string, code int) {
	b.t.Helper()
	for _, pattern := range []string{"calls", "tries-*", "stdin-*", "synthesis-prompt-path"} {
		matches, _ := filepath.Glob(filepath.Join(b.dir, pattern))
		for _, path := range matches {
			_ = os.Remove(path)
		}
	}
	full := append([]string{"panel", "--log-dir", filepath.Join(b.dir, "logs")}, args...)
	return autoreview(b.t, full...)
}

func (b panelBox) read(name string) string {
	raw, _ := os.ReadFile(filepath.Join(b.dir, name))
	return string(raw)
}

func (b panelBox) calls() string { return b.read("calls") }

// prompt is what the backend's panelist read on stdin.
func (b panelBox) prompt(backend string) string {
	matches, _ := filepath.Glob(filepath.Join(b.dir, "stdin-"+backend+"-*"))
	var all strings.Builder
	for _, path := range matches {
		raw, _ := os.ReadFile(path)
		all.Write(raw)
	}
	return all.String()
}

func (b panelBox) synthesisPrompt() string {
	raw, _ := os.ReadFile(b.read("synthesis-prompt-path"))
	return string(raw)
}

// worktrees is how many worktrees git lists for the checkout. One is the
// checkout itself.
func (b panelBox) worktrees() int {
	b.t.Helper()
	out, err := exec.Command("git", "-C", b.repo, "worktree", "list", "--porcelain").Output()
	if err != nil {
		b.t.Fatal(err)
	}
	return strings.Count(string(out), "worktree ")
}

func contains(t *testing.T, what, text string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Errorf("%s lacks %q:\n%s", what, want, text)
		}
	}
}

func lacks(t *testing.T, what, text string, unwanted ...string) {
	t.Helper()
	for _, u := range unwanted {
		if strings.Contains(text, u) {
			t.Errorf("%s holds %q:\n%s", what, u, text)
		}
	}
}
