package task

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/venabots/autoreview/internal/review"
)

func TestEachKindNamesItsSkillItsVerbAndItsFiles(t *testing.T) {
	cases := []struct {
		kind                    Kind
		skill, doing, tag, noun string
	}{
		{Fix, "babysit-pr", "fixing", "fix", "fix"},
		{Conflicts, "sync-main", "fixing conflicts", "conflicts", "conflict fix"},
		{Review, "", "reviewing", "", "review"},
	}
	for _, c := range cases {
		if c.kind.Skill() != c.skill || c.kind.Doing() != c.doing || c.kind.Tag() != c.tag || c.kind.Noun() != c.noun {
			t.Errorf("kind %d: %q, %q, %q, %q", c.kind, c.kind.Skill(), c.kind.Doing(), c.kind.Tag(), c.kind.Noun())
		}
	}
}

func TestSkillRoots(t *testing.T) {
	claude := SkillRoots(review.Claude(), "/home/me", "", "/home/me/.claude", "/src/repo")
	if !slices.Equal(claude, []string{"/home/me/.claude/skills", "/src/repo/.claude/skills"}) {
		t.Errorf("claude roots = %v", claude)
	}
	codex := SkillRoots(review.Orchestrator{Backend: "codex"}, "/home/me", "", "/home/me/.claude", "/src/repo")
	if !slices.Contains(codex, "/home/me/.agents/skills") {
		t.Errorf("codex roots = %v", codex)
	}
}

func TestNotInstalledSaysWhereTheSkillWasLookedFor(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "babysit-pr"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "babysit-pr", "SKILL.md"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if why := NotInstalled(Fix, []string{root}, review.Claude()); why != "" {
		t.Errorf("an installed skill was refused: %q", why)
	}
	why := NotInstalled(Conflicts, []string{root}, review.Claude())
	if why != "sync-main is not installed where claude looks (~/.claude/skills)" {
		t.Errorf("NotInstalled = %q", why)
	}
}

func TestReadbackSaysWhetherTheHeadMoved(t *testing.T) {
	var asked []string
	answer := func(head string, err error) func(context.Context, ...string) ([]byte, error) {
		return func(_ context.Context, args ...string) ([]byte, error) {
			asked = args
			return []byte(head + "\n"), err
		}
	}
	cases := []struct {
		head string
		err  error
		want string
		ok   bool
	}{
		{"sha2", nil, "pushed", true},
		{"sha1", nil, "nothing pushed", true},
		{"", nil, "nothing pushed", true},
		{"", errors.New("timed out"), "unknown", false},
	}
	for _, c := range cases {
		got, ok := Readback(context.Background(), answer(c.head, c.err), "acme/widgets", 4, "sha1")
		if got != c.want || ok != c.ok {
			t.Errorf("head %q, err %v: Readback = %q, %v; want %q, %v", c.head, c.err, got, ok, c.want, c.ok)
		}
	}
	if got := strings.Join(asked, " "); got != "pr view 4 --repo acme/widgets --json headRefOid --jq .headRefOid" {
		t.Errorf("gh was asked %q", got)
	}
}
