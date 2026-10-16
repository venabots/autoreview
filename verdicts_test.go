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
