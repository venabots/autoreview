package skills

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Restage stages the source again over dir, and fails when any of names,
// the skills staged at the start, is not there afterwards.
//
// A run stages its skills once, under the temp directory, and a run that
// watches can last for days. macOS deletes what has sat in its temp
// directory for three days. The reviewer of such a run was handed a
// directory with no skill in it, so it reviewed by its own lights and
// posted nothing, and the run counted that as a review that gave no
// verdict. Staged again before each review, the copy is never older than
// the review. A source that lost a skill since the start leaves the same
// hole, so that fails here, where the review can say why.
func Restage(s Source, bundle fs.FS, dir string, names []string) error {
	if _, err := Stage(s, bundle, dir); err != nil {
		return err
	}
	staged := namesIn(os.DirFS(filepath.Join(dir, filepath.FromSlash(subdir))))
	missing := slices.DeleteFunc(slices.Clone(names), func(name string) bool {
		return slices.Contains(staged, name)
	})
	if len(missing) == 0 {
		return nil
	}
	from := "the bundle"
	if s.Kind == FromDir {
		from = s.Dir
	}
	return fmt.Errorf("%s no longer holds %s", from, strings.Join(missing, ", "))
}
