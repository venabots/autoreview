package skills

import (
	"fmt"
	"os"
	"path/filepath"
)

// Staged is the skills that Stage wrote, held in memory. Its zero value
// staged nothing.
type Staged struct {
	// Dir is the directory the reviewer is handed, or empty when nothing
	// was staged.
	Dir     string
	entries []entry
}

// Again writes the same copy under Dir again, each file whole.
//
// A run stages its skills under the temp directory, and a run that
// watches can last for days. macOS deletes what has sat in its temp
// directory for three days. The reviewer of such a run was handed a
// directory with no skill in it, so it reviewed by its own lights and
// posted nothing, and the run counted that as a review that gave no
// verdict. Staged again before each review, the copy is never older than
// the review.
//
// It writes what Stage read at the start, and never reads the source
// again. The run checked that copy before the first review, and a skills
// directory edited later, or one that lost a skill, would hand a review a
// copy nobody checked.
func (s Staged) Again() error {
	if s.Dir == "" {
		return nil
	}
	target := filepath.Join(s.Dir, filepath.FromSlash(subdir))
	if err := os.MkdirAll(target, private); err != nil {
		return fmt.Errorf("making %s: %w", target, err)
	}
	for _, e := range s.entries {
		to := filepath.Join(target, filepath.FromSlash(e.path))
		if e.dir {
			if err := os.MkdirAll(to, private); err != nil {
				return fmt.Errorf("making %s: %w", to, err)
			}
			continue
		}
		if err := writeFile(to, e.raw, e.mode); err != nil {
			return err
		}
	}
	return nil
}
