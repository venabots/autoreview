package stats

import "testing"

func TestNamesAndModelsFoldToOneSpelling(t *testing.T) {
	cases := []struct {
		name  string
		model *string
		want  key
	}{
		// v1's golden cases.
		{"codex", ptr("gpt-5.6-sol"), key{"codex", "gpt-5.6-sol"}},
		{"codex-gpt-5.6-sol", ptr("gpt-5.6-sol"), key{"codex", "gpt-5.6-sol"}},
		{"codex-gpt-5.6-sol", ptr("codex-gpt-5.6-sol"), key{"codex", "gpt-5.6-sol"}},
		{"codex-gpt-5.6-sol", nil, key{"codex", "gpt-5.6-sol"}},
		{"opencode-xai-grok-4.6", ptr("xai/grok-4.6"), key{"opencode", "grok-4.6"}},
		{"opencode", ptr("zai-coding-plan/glm-5.3"), key{"opencode", "glm-5.3"}},
		{"opencode-ollama-grok-4.6", ptr("ollama/grok-4.6"), key{"opencode", "ollama/grok-4.6"}},
		{"claude-fable", ptr("claude-fable-5-1"), key{"claude", "claude-fable-5.1"}},
		{"claude-fable", ptr("claude-fable-5.1"), key{"claude", "claude-fable-5.1"}},
		// A dated snapshot folds onto the short id and does not split off.
		{"claude", ptr("claude-haiku-4-5-20251001"), key{"claude", "claude-haiku-4.5"}},
		{"claude", ptr("claude-haiku-4-5"), key{"claude", "claude-haiku-4.5"}},
		{"claude", ptr("claude-haiku-4.5"), key{"claude", "claude-haiku-4.5"}},
		{"claude", ptr("Claude-Opus-5"), key{"claude", "claude-opus-5"}},
		{"claude-fable", ptr("fable"), key{"claude", "fable"}},
		{"claude", ptr("unknown"), key{"claude", "unknown"}},
		{"codex", nil, key{"codex", "unknown"}},
		{"gemini", ptr("gemini-3"), key{"gemini", "gemini-3"}},
		// The edges of the same rules.
		{"  Codex  ", ptr("  GPT-5.5 "), key{"codex", "gpt-5.5"}},
		{"claude", ptr("?"), key{"claude", "unknown"}},
		{"claude", ptr(""), key{"claude", "unknown"}},
		{"claude-opus", ptr("UNKNOWN"), key{"claude", "opus"}},
		{"gemini-pro", nil, key{"gemini", "pro"}},
		{"codex-", nil, key{"codex", ""}},
		{"", nil, key{"", "unknown"}},
		{"opencode", ptr("a/ollama/grok-4.6"), key{"opencode", "grok-4.6"}},
		{"opencode", ptr("openrouter/xai/grok-4-6"), key{"opencode", "grok-4.6"}},
		{"claude", ptr("claude-opus-20251001"), key{"claude", "claude-opus"}},
		{"claude", ptr("opus-20251001"), key{"claude", "opus-20251001"}},
		{"claude", ptr("claude-3-5-sonnet"), key{"claude", "claude-3-5-sonnet"}},
		{"claude", ptr("claude-opus-4-100"), key{"claude", "claude-opus-4-100"}},
		{"claude", ptr("opus-4-7"), key{"claude", "opus-4.7"}},
		{"claude", ptr("4-7"), key{"claude", "4-7"}},
		{"claude", ptr("claude-opus-5[1m]"), key{"claude", "claude-opus-5[1m]"}},
		// Only A to Z fold. A capital outside ASCII stays as it is.
		{"claude", ptr("ÉCLAIR-1"), key{"claude", "Éclair-1"}},
	}
	for _, c := range cases {
		label := c.name + " with no model"
		if c.model != nil {
			label = c.name + " reporting " + *c.model
		}
		t.Run(label, func(t *testing.T) {
			if got := canonical(c.name, c.model); got != c.want {
				t.Errorf("canonical(%q) = %+v, want %+v", c.name, got, c.want)
			}
		})
	}
}

func TestTheBackendIsTheKnownOneTheNameStartsWith(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"codex", "codex"},
		{"codex-gpt-5.6-sol", "codex"},
		{"Claude-Fable", "claude"},
		{" opencode-glm ", "opencode"},
		{"opencoder", "opencoder"},
		{"codexx-1", "codexx"},
		{"gemini-pro-3", "gemini"},
		{"coderabbitai", "coderabbitai"},
		{"", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := backendOf(c.name); got != c.want {
				t.Errorf("backendOf(%q) = %q, want %q", c.name, got, c.want)
			}
		})
	}
}

func TestKeysOrderByBackendThenModel(t *testing.T) {
	cases := []struct {
		name string
		a, b key
		want int
	}{
		{"the backend decides first", key{"claude", "z"}, key{"codex", "a"}, -1},
		{"then the model", key{"codex", "gpt-5.5"}, key{"codex", "gpt-5.6"}, -1},
		{"the same key", key{"codex", "gpt-5.5"}, key{"codex", "gpt-5.5"}, 0},
		{"the other way round", key{"opencode", "a"}, key{"codex", "z"}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.a.compare(c.b); got != c.want {
				t.Errorf("compare = %d, want %d", got, c.want)
			}
		})
	}
}
