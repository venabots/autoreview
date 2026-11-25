package autoreview

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/venabots/autoreview/internal/review"
)

// A run trusts the bundled copies of the skills that give a verdict to
// know the option that limits it, and checks only installed copies. A
// bundled copy that lost the option would post the verdict the person
// turned off.
func TestTheBundledSkillsThatGiveAVerdictKnowTheVerdictsOption(t *testing.T) {
	for _, skill := range []string{"auto-review", "recheck-pr"} {
		raw, err := fs.ReadFile(Skills(), skill+"/SKILL.md")
		if err != nil {
			t.Fatal(err)
		}
		for _, level := range []string{review.VerdictsFlag + " approve-only", review.VerdictsFlag + " comment-only"} {
			if !strings.Contains(string(raw), level) {
				t.Errorf("the bundled %s does not say what %s does", skill, level)
			}
		}
	}
}

// The bundled copies are trusted to know the ceiling, as they are trusted
// to know the level. A copy that read a missing Risk, or a word like
// LOWISH, as low would approve the PR whose rating it lost, so each one
// says a missing or garbled Risk is above every ceiling, and gives the body
// that names the risk.
func TestTheBundledSkillsThatGiveAVerdictKnowTheRiskCeiling(t *testing.T) {
	for _, skill := range []string{"auto-review", "recheck-pr"} {
		raw, err := fs.ReadFile(Skills(), skill+"/SKILL.md")
		if err != nil {
			t.Fatal(err)
		}
		text := strings.Join(strings.Fields(string(raw)), " ")
		for _, phrase := range []string{
			review.MaxRiskFlag + " low",
			review.MaxRiskFlag + " medium",
			"above every ceiling",
			"`LOWISH`, `LOW-ish`, `low/medium`",
			"`LGTM. This PR is <risk> risk, so a person approves it.`",
			"Risk within the ceiling",
		} {
			if !strings.Contains(text, phrase) {
				t.Errorf("the bundled %s does not say %q", skill, phrase)
			}
		}
	}
}
