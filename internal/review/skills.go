package review

import (
	"os"
	"path/filepath"
	"slices"
)

// EntrySkills are the skills a review cannot start without: the entry point
// that each prompt names. They call the others, so a run that finds these
// has found the set.
var EntrySkills = []string{"auto-review", "panel-review", "recheck-pr"}

// SkillRoots is where a backend looks for skills, in the order it reads
// them. It is empty for a backend that the staged directory already
// serves, which needs no check.
//
// Codex reads `$CODEX_HOME/skills`, or `~/.codex/skills` without the
// variable, and then `~/.agents/skills`. It does not read a directory added
// at run time, so these are the only places a codex review can find the
// review skills. An empty home gives no root: "/skills" is not a path
// anybody meant.
func SkillRoots(backend, home, codexHome string) []string {
	if backend != "codex" {
		return nil
	}
	var roots []string
	switch {
	case codexHome != "":
		roots = append(roots, filepath.Join(codexHome, "skills"))
	case home != "":
		roots = append(roots, filepath.Join(home, ".codex", "skills"))
	}
	if home != "" {
		roots = append(roots, filepath.Join(home, ".agents", "skills"))
	}
	return roots
}

// MissingSkills is the skills in wanted that no root holds. A codex review
// whose skill is not installed still runs and still costs money, and it
// reviews nothing, so the run refuses before it starts.
//
// A skill is there when its `SKILL.md` is a file, through a symlink or not.
// That is the test the agents make.
func MissingSkills(roots, wanted []string) []string {
	var missing []string
	for _, name := range wanted {
		held := slices.ContainsFunc(roots, func(root string) bool {
			return isFile(filepath.Join(root, name, "SKILL.md"))
		})
		if !held {
			missing = append(missing, name)
		}
	}
	return missing
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
