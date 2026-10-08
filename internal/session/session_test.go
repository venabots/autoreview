package session

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The ids below are what v1 filed reviews under. They are pinned because a
// change to the derivation does not fail loudly: it quietly stops finding
// anyone's earlier review.
func TestIDIsWhatV1FiledReviewsUnder(t *testing.T) {
	cases := []struct {
		root, owner, name string
		pr                int
		want              string
	}{
		{"/sandbox/repo", "acme", "widgets", 9, "7442b624-5cba-5d44-ae67-9c390cfe70a1"},
		{"/tmp/x", "octo", "repo", 123, "80e25f6a-45b7-5246-a9e2-8feda1021531"},
	}
	for _, c := range cases {
		if got := ID(c.root, c.owner, c.name, c.pr); got != c.want {
			t.Errorf("ID(%q, %q, %q, %d) = %s, want %s", c.root, c.owner, c.name, c.pr, got, c.want)
		}
	}
}

func TestIDDiffersForEachCheckoutAndEachPR(t *testing.T) {
	base := ID("/src/repo", "acme", "widgets", 9)
	others := map[string]string{
		"another checkout": ID("/src/repo.worktree", "acme", "widgets", 9),
		"another PR":       ID("/src/repo", "acme", "widgets", 10),
		"another repo":     ID("/src/repo", "acme", "gadgets", 9),
		"another owner":    ID("/src/repo", "octo", "widgets", 9),
	}
	for what, id := range others {
		if id == base {
			t.Errorf("%s has the same session id: one would resume the other's review", what)
		}
		if !UUIDShaped(id) || id[14] != '5' || id[19] != 'a' {
			t.Errorf("%s gives %s, which is not a v5 UUID", what, id)
		}
	}
	if again := ID("/src/repo", "acme", "widgets", 9); again != base {
		t.Errorf("the same PR gave %s and then %s", base, again)
	}
}

func TestIDKeepsARootWithATrailingSpaceDistinct(t *testing.T) {
	if ID("/src/repo", "a", "b", 1) == ID("/src/repo ", "a", "b", 1) {
		t.Error("a trailing space in the root does not change the id: the root was trimmed")
	}
}

func TestFreshIDIsAVersion4UUID(t *testing.T) {
	id, err := FreshID(bytes.NewReader(bytes.Repeat([]byte{0xff}, 16)))
	if err != nil {
		t.Fatalf("FreshID: %v", err)
	}
	if id != "ffffffff-ffff-4fff-bfff-ffffffffffff" {
		t.Errorf("FreshID = %s, want the version and variant bits set over all ones", id)
	}
	zero, _ := FreshID(bytes.NewReader(make([]byte, 16)))
	if zero != "00000000-0000-4000-8000-000000000000" {
		t.Errorf("FreshID = %s, want the version and variant bits set over all zeros", zero)
	}
	if !UUIDShaped(id) || !UUIDShaped(zero) {
		t.Error("a fresh id is not UUID-shaped")
	}
}

func TestFreshIDFailsWithoutRandomness(t *testing.T) {
	if _, err := FreshID(bytes.NewReader([]byte{1, 2, 3})); err == nil {
		t.Error("FreshID made an id from three bytes")
	}
}

func TestUUIDShaped(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"7442b624-5cba-5d44-ae67-9c390cfe70a1", true},
		{"7442B624-5CBA-5D44-AE67-9C390CFE70A1", true},
		{"", false},
		{"7442b624-5cba-5d44-ae67-9c390cfe70a", false},
		{"7442b624-5cba-5d44-ae67-9c390cfe70a11", false},
		{"7442b624-5cba-5d44-ae67-9c390cfe70g1", false},
		{"7442b624 5cba 5d44 ae67 9c390cfe70a1", false},
		{"--resume 7442b624-5cba-5d44-ae67-9c3", false},
		{"$(rm -rf ~)-5cba-5d44-ae67-9c390cfe7", false},
	}
	for _, c := range cases {
		if got := UUIDShaped(c.in); got != c.want {
			t.Errorf("UUIDShaped(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestConfigDir(t *testing.T) {
	env := func(vars map[string]string) func(string) string {
		return func(name string) string { return vars[name] }
	}
	if got := ConfigDir(env(map[string]string{"CLAUDE_CONFIG_DIR": "/cfg", "HOME": "/home/me"})); got != "/cfg" {
		t.Errorf("ConfigDir = %q, want the variable to win", got)
	}
	if got := ConfigDir(env(map[string]string{"HOME": "/home/me"})); got != "/home/me/.claude" {
		t.Errorf("ConfigDir = %q, want ~/.claude", got)
	}
	if got := ConfigDir(env(map[string]string{"CLAUDE_CONFIG_DIR": "", "HOME": "/home/me"})); got != "/home/me/.claude" {
		t.Errorf("ConfigDir = %q, want an empty variable to count as unset", got)
	}
}

func storeWith(t *testing.T, files ...string) Store {
	t.Helper()
	config := t.TempDir()
	for _, file := range files {
		path := filepath.Join(config, "projects", file)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return StoreIn(config)
}

func TestTranscriptIsFoundInAnyProjectDirectory(t *testing.T) {
	const id = "7442b624-5cba-5d44-ae67-9c390cfe70a1"
	store := storeWith(t, "-src-other/unrelated.jsonl", "-some-escaped-cwd/"+id+".jsonl")

	path, ok := store.Transcript(id)
	if !ok || !strings.HasSuffix(path, filepath.Join("-some-escaped-cwd", id+".jsonl")) {
		t.Errorf("Transcript = %q, %v; want the file under whatever directory holds it", path, ok)
	}
	if !store.Exists(id) {
		t.Error("Exists is false for a session with a transcript")
	}
}

func TestTranscriptIsNotFound(t *testing.T) {
	const id = "7442b624-5cba-5d44-ae67-9c390cfe70a1"
	cases := []struct {
		name  string
		store Store
		id    string
	}{
		{"no such session", storeWith(t, "-proj/other.jsonl"), id},
		{"an empty id", storeWith(t, "-proj/.jsonl"), ""},
		{"no projects directory", StoreIn(t.TempDir()), id},
		{"a directory with the transcript's name", storeWith(t, "-proj/"+id+".jsonl/x"), id},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if path, ok := c.store.Transcript(c.id); ok {
				t.Errorf("Transcript found %q", path)
			}
			if c.store.Exists(c.id) {
				t.Error("Exists is true")
			}
		})
	}
}

func TestPlanFor(t *testing.T) {
	const id = "7442b624-5cba-5d44-ae67-9c390cfe70a1"
	cases := []struct {
		name                  string
		exists, inUse, resume bool
		want                  Plan
	}{
		{"a first review pins the id", false, false, false, Plan{ID: id, Flag: FlagPin}},
		{"a first review pins the id when resume is asked for too", false, false, true, Plan{ID: id, Flag: FlagPin}},
		{"an earlier session is not reused without being asked", true, false, false, Plan{ID: id, Flag: FlagNone}},
		{"an earlier session is resumed when asked", true, false, true, Plan{ID: id, Flag: FlagResume}},
		{"a session open elsewhere is left alone", true, true, true, Plan{
			ID: id, Flag: FlagNone,
			Note: "PR #9 has a review session open in another tab or process; reviewing fresh",
		}},
		{"a session open elsewhere does not matter without resume", true, true, false, Plan{ID: id, Flag: FlagNone}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			probe := Probe{
				Exists: func(string) bool { return c.exists },
				InUse:  func(string) bool { return c.inUse },
			}
			got := PlanFor(id, 9, c.resume, probe)
			if got != c.want {
				t.Errorf("PlanFor = %+v, want %+v", got, c.want)
			}
			if got.Resumes() != (c.want.Flag == FlagResume) {
				t.Errorf("Resumes() = %v", got.Resumes())
			}
		})
	}
}

func TestPlanForDoesNotAskTheProcessTableUnlessItMustKnow(t *testing.T) {
	asked := false
	probe := Probe{
		Exists: func(string) bool { return false },
		InUse:  func(string) bool { asked = true; return false },
	}
	PlanFor("id", 9, true, probe)
	if asked {
		t.Error("PlanFor ran the in-use check for a session that does not exist")
	}
}

func TestInUseSeesAProcessWithTheIDOnItsCommandLine(t *testing.T) {
	if _, err := exec.LookPath("pgrep"); err != nil {
		t.Skip("pgrep is not installed")
	}
	const id = "0d0d0d0d-1111-4222-8333-444455556666"
	if InUse(context.Background(), id) {
		t.Fatal("InUse is true before any process carries the id")
	}
	if InUse(context.Background(), "") {
		t.Error("InUse is true for an empty id, which would match every process")
	}

	// The id is the shell's $0, an argument of its own. The script is a
	// constant, and ends in a second command so the shell stays alive and
	// does not replace itself with sleep, which would drop the id.
	holder := exec.Command("sh", "-c", "sleep 30; exit 0", id)
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = holder.Process.Kill()
		_ = holder.Wait()
	}()
	if !InUse(context.Background(), id) {
		t.Error("InUse is false while a process has the id on its command line")
	}
}
