package worktree

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// isolate keeps git in the test off every real repo and every real config.
// A test run from a git hook inherits GIT_DIR, which would point each git
// call here at the repo being worked in; a developer's signing, hooks or
// default branch would change what the test sees.
func isolate(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	for _, name := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY"} {
		unsetForTest(t, name)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

func unsetForTest(t *testing.T, name string) {
	t.Helper()
	old, had := os.LookupEnv(name)
	if !had {
		return
	}
	t.Setenv(name, old)
	if err := os.Unsetenv(name); err != nil {
		t.Fatal(err)
	}
}

// mustGit runs a setup step and fails the test when it fails.
func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	base := []string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main"}
	out, err := exec.Command("git", append(base, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// fixture is a clone of a bare origin that has main and the PR's branch
// me/work, and a run directory beside them.
type fixture struct {
	base, clone, runRoot string
}

func setup(t *testing.T) fixture {
	t.Helper()
	isolate(t)
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	origin := filepath.Join(base, "origin.git")
	seed := filepath.Join(base, "seed")
	clone := filepath.Join(base, "clone")
	runRoot := filepath.Join(base, "run-test")
	for _, dir := range []string{seed, runRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustGit(t, base, "init", "--quiet", "--bare", origin)
	mustGit(t, seed, "init", "--quiet")
	mustGit(t, seed, "commit", "--quiet", "--allow-empty", "-m", "init")
	mustGit(t, seed, "remote", "add", "origin", origin)
	mustGit(t, seed, "push", "--quiet", "origin", "main")
	mustGit(t, seed, "switch", "--quiet", "-c", "me/work")
	mustGit(t, seed, "commit", "--quiet", "--allow-empty", "-m", "work")
	mustGit(t, seed, "push", "--quiet", "origin", "me/work")
	mustGit(t, base, "clone", "--quiet", origin, clone)
	return fixture{base: base, clone: clone, runRoot: runRoot}
}

func (f fixture) request(branch string) Request {
	return Request{Root: f.clone, Path: PathFor(f.runRoot, 4), PR: 4, Branch: branch}
}

func (f fixture) originHead(t *testing.T, branch string) string {
	t.Helper()
	return mustGit(t, f.clone, "rev-parse", "origin/"+branch)
}

func prepareOK(t *testing.T, r Request) Prepared {
	t.Helper()
	p, err := Prepare(context.Background(), Exec, r)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	return p
}

func prepareErr(t *testing.T, r Request) string {
	t.Helper()
	_, err := Prepare(context.Background(), Exec, r)
	if err == nil {
		t.Fatal("Prepare made a worktree it should have refused")
	}
	return err.Error()
}

func gone(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Errorf("%s is still there", path)
	}
}
