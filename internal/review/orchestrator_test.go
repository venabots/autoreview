package review

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const sessionID = "7442b624-5cba-5d44-ae67-9c390cfe70a1"

func codex() Orchestrator {
	return Orchestrator{Backend: "codex"}
}

func TestASpecIsABackendAndAnOptionalModel(t *testing.T) {
	cases := []struct {
		raw   string
		want  Orchestrator
		label string
	}{
		{"claude", Claude(), "claude"},
		{"codex", codex(), "codex"},
		{"codex:gpt-5.5", Orchestrator{Backend: "codex", Model: "gpt-5.5"}, "codex:gpt-5.5"},
		{"claude:opus-4.8", Orchestrator{Backend: "claude", Model: "opus-4.8"}, "claude:opus-4.8"},
	}
	for _, c := range cases {
		got, err := ParseOrchestrator(c.raw)
		if err != nil {
			t.Errorf("ParseOrchestrator(%q): %v", c.raw, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseOrchestrator(%q) = %+v, want %+v", c.raw, got, c.want)
		}
		if got.Label() != c.label {
			t.Errorf("Label() = %q, want the spec as typed, %q", got.Label(), c.label)
		}
		if got.CLI() != c.want.Backend {
			t.Errorf("CLI() = %q, want the backend", got.CLI())
		}
	}
}

func TestABackendThatCannotOrchestrateIsRefused(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []string
	}{
		// opencode reviews as a panelist, but nothing here can hand it a
		// skill by name.
		{"a panelist only", "opencode", []string{`unknown orchestrator "opencode"`, "codex, claude"}},
		{"nothing at all", "", []string{`unknown orchestrator ""`}},
		{"a colon and no model", "codex:", []string{`"codex:" names no model after the colon`}},
		{"an unknown backend with a model", "gemini:pro", []string{`unknown orchestrator "gemini"`}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseOrchestrator(c.raw)
			if err == nil {
				t.Fatalf("ParseOrchestrator(%q) gave no error", c.raw)
			}
			for _, want := range c.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not say %q", err, want)
				}
			}
			if strings.HasPrefix(err.Error(), "error: ") {
				t.Errorf("error %q has the prefix that the caller adds", err)
			}
		})
	}
}

func TestEachBackendIsToldAboutASkillItsOwnWay(t *testing.T) {
	if got := Claude().SkillPrompt("auto-review", 9); got != "/auto-review 9" {
		t.Errorf("claude: SkillPrompt = %q, want a slash command", got)
	}
	if got := codex().SkillPrompt("auto-review", 9); got != "$auto-review 9" {
		t.Errorf("codex: SkillPrompt = %q, want the $name form", got)
	}
}

func TestTheReopenCommandNamesTheBackendsOwnCLI(t *testing.T) {
	cases := []struct {
		name string
		o    Orchestrator
		id   string
		want string
	}{
		{"claude", Claude(), sessionID, "claude --resume " + sessionID},
		{"codex", codex(), sessionID, "codex resume " + sessionID},
		{"no id is a legend", Claude(), "", "claude --resume <SESSION>"},
		{"no id is a legend for codex too", codex(), "", "codex resume <SESSION>"},
		// The line is pasted into a shell, and the id can come from a file.
		{"an id that is not one stays off the line", Claude(), "$(rm -rf ~)", "claude --resume <SESSION>"},
		{"so does an id with a second command", codex(), sessionID + "; reboot", "codex resume <SESSION>"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.o.ReopenCommand(c.id); got != c.want {
				t.Errorf("ReopenCommand(%q) = %q, want %q", c.id, got, c.want)
			}
		})
	}
}

func TestWhatDashpForwardsIsStatedForEachBackend(t *testing.T) {
	cases := []struct {
		o    Orchestrator
		want bool
		home string
	}{
		{Claude(), true, "~/.claude/skills"},
		{Orchestrator{Backend: "claude", Model: "opus-4.8"}, true, "~/.claude/skills"},
		// Codex gets no session flag, no budget and no system prompt. The
		// staged tree is a .claude/skills directory, so it does not find
		// that either.
		{codex(), false, "~/.agents/skills"},
		{Orchestrator{Backend: "codex", Model: "gpt-5.5"}, false, "~/.agents/skills"},
	}
	for _, c := range cases {
		got := map[string]bool{
			"SupportsSessions":      c.o.SupportsSessions(),
			"SupportsBudget":        c.o.SupportsBudget(),
			"SupportsSystemPrompt":  c.o.SupportsSystemPrompt(),
			"DiscoversStagedSkills": c.o.DiscoversStagedSkills(),
		}
		for name, value := range got {
			if value != c.want {
				t.Errorf("%s: %s = %v, want %v", c.o.Label(), name, value, c.want)
			}
		}
		if c.o.SkillsHome() != c.home {
			t.Errorf("%s: SkillsHome = %q, want %q", c.o.Label(), c.o.SkillsHome(), c.home)
		}
	}
}

// A cap that quietly does not apply is worse than no cap. The run says so
// once, before the first review, for the orchestrator and for the fallback
// that can take a review over.
func TestACapThatIsNotEnforcedIsSaidBeforeTheFirstReview(t *testing.T) {
	gpt := Orchestrator{Backend: "codex", Model: "gpt-5.5"}
	fallback := codex()
	cases := []struct {
		name     string
		budget   string
		o        Orchestrator
		fallback *Orchestrator
		want     []string
	}{
		{"no cap", "", gpt, &fallback, nil},
		{"claude alone", "2.50", Claude(), nil, nil},
		{"under codex", "2.50", gpt, nil, []string{
			"--budget is not enforced under codex:gpt-5.5; dash-p forwards the cap to claude alone",
		}},
		{"codex takes over", "2.50", Claude(), &fallback, []string{
			"--budget is not enforced on a review codex takes over; dash-p forwards the cap to claude alone",
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := BudgetNotes(c.budget, c.o, c.fallback); !slices.Equal(got, c.want) {
				t.Errorf("BudgetNotes = %q, want %q", got, c.want)
			}
		})
	}
}

func TestTheWordsForAFallback(t *testing.T) {
	cases := []struct {
		raw  string
		want Fallback
	}{
		{"none", Fallback{Kind: FallbackNone}},
		{"off", Fallback{Kind: FallbackNone}},
		{"auto", Fallback{Kind: FallbackAuto}},
		{"codex:gpt-5.5", Fallback{Kind: FallbackSpec, Spec: Orchestrator{Backend: "codex", Model: "gpt-5.5"}}},
		{"claude", Fallback{Kind: FallbackSpec, Spec: Claude()}},
	}
	for _, c := range cases {
		got, err := ParseFallback(c.raw)
		if err != nil || got != c.want {
			t.Errorf("ParseFallback(%q) = %+v, %v; want %+v", c.raw, got, err, c.want)
		}
	}
	if (Fallback{}).Kind != FallbackAuto {
		t.Error("a run that names no fallback does not get the automatic one")
	}
}

func TestAnUnknownFallbackIsNotCalledAnOrchestrator(t *testing.T) {
	_, err := ParseFallback("gemini")
	if err == nil || !strings.Contains(err.Error(), `unknown fallback "gemini"`) {
		t.Errorf("ParseFallback(gemini) error = %v, want it to name the fallback", err)
	}
	if _, err := ParseFallback("codex:"); err == nil || !strings.Contains(err.Error(), "names no model") {
		t.Errorf("ParseFallback(codex:) error = %v, want the missing model named", err)
	}
}

func everything(string) bool { return true }

func onlyClaude(cli string) bool { return cli == "claude" }

func TestResolveGivesTheFallbackThisRunUses(t *testing.T) {
	named := Fallback{Kind: FallbackSpec, Spec: codex()}
	cases := []struct {
		name      string
		fallback  Fallback
		primary   Orchestrator
		installed func(string) bool
		want      Orchestrator
		ok        bool
	}{
		{"auto stands codex in for claude", Fallback{}, Claude(), everything, codex(), true},
		{"auto stands claude in for codex", Fallback{}, codex(), everything, Claude(), true},
		// The stand-in runs on its own default model.
		{"auto does not carry the model over", Fallback{}, Orchestrator{Backend: "claude", Model: "opus-4.8"}, everything, codex(), true},
		{"auto is nothing when nothing else is installed", Fallback{}, Claude(), onlyClaude, Orchestrator{}, false},
		// The caller refuses the run when a named fallback is missing.
		{"a named fallback is returned whatever PATH says", named, Claude(), onlyClaude, codex(), true},
		{"none is none", Fallback{Kind: FallbackNone}, Claude(), everything, Orchestrator{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := c.fallback.Resolve(c.primary, c.installed)
			if got != c.want || ok != c.ok {
				t.Errorf("Resolve = %+v, %v; want %+v, %v", got, ok, c.want, c.ok)
			}
		})
	}
}

func TestTheFirstLineSaysWhyThereIsNoFallback(t *testing.T) {
	resolved := codex()
	cases := []struct {
		name     string
		fallback Fallback
		primary  Orchestrator
		resolved *Orchestrator
		want     string
	}{
		{"a fallback is named", Fallback{}, Claude(), &resolved, "codex"},
		// Turned off on purpose reads as a choice.
		{"turned off", Fallback{Kind: FallbackNone}, Claude(), nil, "none"},
		// Not turned off, and nothing to fall back to: the missing CLI is
		// named, because to install it is the fix.
		{"codex is missing", Fallback{}, Claude(), nil, "none (codex is not installed)"},
		{"claude is missing", Fallback{}, codex(), nil, "none (claude is not installed)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.fallback.Describe(c.primary, c.resolved); got != c.want {
				t.Errorf("Describe = %q, want %q", got, c.want)
			}
		})
	}
}

func TestOnlyCodexHasSkillRootsToCheck(t *testing.T) {
	cases := []struct {
		name                     string
		backend, home, codexHome string
		want                     []string
	}{
		// The staged directory serves claude, so there is nothing to look
		// for and nothing to refuse over.
		{"claude", "claude", "/home/x", "", nil},
		{"codex", "codex", "/home/x", "", []string{"/home/x/.codex/skills", "/home/x/.agents/skills"}},
		{"CODEX_HOME replaces the default and ~/.agents still counts", "codex", "/home/x", "/elsewhere", []string{"/elsewhere/skills", "/home/x/.agents/skills"}},
		{"CODEX_HOME alone", "codex", "", "/elsewhere", []string{"/elsewhere/skills"}},
		// No home is no roots, and not a path of "/skills".
		{"no home", "codex", "", "", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SkillRoots(c.backend, c.home, c.codexHome); !slices.Equal(got, c.want) {
				t.Errorf("SkillRoots = %v, want %v", got, c.want)
			}
		})
	}
}

func TestASkillIsThereWhenItsSkillFileIs(t *testing.T) {
	root := filepath.Join(t.TempDir(), "agents")
	elsewhere := t.TempDir()
	write := func(path string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(root, "auto-review", "SKILL.md"))
	// A directory with no SKILL.md is not a skill.
	if err := os.MkdirAll(filepath.Join(root, "panel-review"), 0o700); err != nil {
		t.Fatal(err)
	}
	// A directory that has the file's name is not a skill either.
	if err := os.MkdirAll(filepath.Join(root, "broken", "SKILL.md"), 0o700); err != nil {
		t.Fatal(err)
	}
	// A symlink to a skill counts: the agents follow it too.
	write(filepath.Join(elsewhere, "linked", "SKILL.md"))
	if err := os.Symlink(filepath.Join(elsewhere, "linked"), filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name   string
		roots  []string
		wanted []string
		want   []string
	}{
		{"the entry skills", []string{root}, EntrySkills, []string{"panel-review", "recheck-pr"}},
		{"a skill that is there", []string{root}, []string{"auto-review"}, nil},
		{"a skill behind a symlink", []string{root}, []string{"linked"}, nil},
		{"a directory named SKILL.md", []string{root}, []string{"broken"}, []string{"broken"}},
		{"any root is enough", []string{elsewhere, root}, []string{"auto-review", "linked"}, nil},
		// Nowhere to look means every skill is missing, which is the refusal.
		{"no roots", nil, EntrySkills, EntrySkills},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			roots, wanted := slices.Clone(c.roots), slices.Clone(c.wanted)
			if got := MissingSkills(c.roots, c.wanted); !slices.Equal(got, c.want) {
				t.Errorf("MissingSkills = %v, want %v", got, c.want)
			}
			if !slices.Equal(c.roots, roots) || !slices.Equal(c.wanted, wanted) {
				t.Error("MissingSkills changed an argument")
			}
		})
	}
}

func TestTheEntrySkillsAreTheOnesAPromptNames(t *testing.T) {
	want := []string{"auto-review", "panel-review", "recheck-pr"}
	if !slices.Equal(EntrySkills, want) {
		t.Errorf("EntrySkills = %v, want %v", EntrySkills, want)
	}
	requests := []Request{
		request(),
		with(func(r *Request) { r.NoPost = true }),
		with(func(r *Request) { r.Session.Flag = resume }),
	}
	for _, r := range requests {
		if !slices.Contains(EntrySkills, Skill(r)) {
			t.Errorf("a review runs %s, which the check before a run does not look for", Skill(r))
		}
	}
}
