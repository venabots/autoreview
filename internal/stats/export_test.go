package stats

import "testing"

func TestCohortSpellsAModelAsTheStatsTableDoes(t *testing.T) {
	model := "xai/grok-4.7"
	backend, spelled := Cohort("opencode-xai-grok-4.7", &model)
	if backend != "opencode" || spelled != "grok-4.7" {
		t.Errorf("Cohort = %q, %q, want opencode, grok-4.7", backend, spelled)
	}
}

func TestParseSinceReadsTheSameSpansAsStats(t *testing.T) {
	got, err := ParseSince("1d", 200_000)
	if err != nil || got != 200_000-daySecs {
		t.Errorf("ParseSince(1d) = %d, %v, want %d", got, err, 200_000-daySecs)
	}
}
