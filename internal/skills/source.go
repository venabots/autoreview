// Package skills puts a run's review skills where the reviewer finds them.
//
// A run stages its skills into a directory of its own and hands that
// directory to the reviewer. Installed skills still win: Claude Code
// resolves a name from the person's own skills directory before any
// directory a flag adds. That is the override a person wants, and it is
// silent, so the run reports the shadowing at startup. Nobody should learn
// it from a review that read the wrong instructions.
package skills

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

// Installed is the --skills value that means "stage nothing".
const Installed = "installed"

// manifest is the file that makes a directory a skill.
const manifest = "SKILL.md"

// Kind is where a run's skills come from. A run has exactly one source,
// chosen at the start, so nobody has to work out which of three copies won.
type Kind int

const (
	// Bundled is the tree the binary was built with. It is the default.
	Bundled Kind = iota
	// FromDir is a directory of <name>/SKILL.md on disk, staged in place
	// of the bundle: a fork, or a repo's own tuned reviewer.
	FromDir
	// NotStaged stages nothing. The reviewer finds whatever is installed.
	NotStaged
)

// Source is one run's skills.
type Source struct {
	Kind Kind
	// Dir is the directory of a FromDir source.
	Dir string
}

// Parse reads a --skills value: the word "installed", or a directory that
// holds at least one skill. Anything else is refused here, where the
// message can say what was expected, and not by a review that found no
// skill. An empty value is the bundle.
func Parse(value string) (Source, error) {
	switch value {
	case "":
		return Source{Kind: Bundled}, nil
	case Installed:
		return Source{Kind: NotStaged}, nil
	}
	if info, err := os.Stat(value); err != nil || !info.IsDir() {
		return Source{}, fmt.Errorf("--skills expects a directory of skills (<name>/%s) or '%s', got %q", manifest, Installed, value)
	}
	if len(namesIn(os.DirFS(value))) == 0 {
		return Source{}, fmt.Errorf("--skills expects a directory of skills (<name>/%s), but %s holds none", manifest, value)
	}
	return Source{Kind: FromDir, Dir: value}, nil
}

// Describe is one line for the run to print, so its log says which copy
// ran.
func (s Source) Describe(version string) string {
	switch s.Kind {
	case FromDir:
		return s.Dir
	case NotStaged:
		return Installed
	}
	return fmt.Sprintf("bundled (%s)", version)
}

// Names is the skills a source stages, sorted. These are the names an
// installed copy could shadow.
func (s Source) Names(bundle fs.FS) []string {
	switch s.Kind {
	case FromDir:
		return namesIn(os.DirFS(s.Dir))
	case NotStaged:
		return []string{}
	}
	return namesIn(bundle)
}

// namesIn is every child directory that holds a SKILL.md, which is how the
// agent recognises a skill. A symlink to a skill counts: a skills directory
// is often links into a checkout.
func namesIn(tree fs.FS) []string {
	entries, err := fs.ReadDir(tree, ".")
	if err != nil {
		return []string{}
	}
	names := []string{}
	for _, entry := range entries {
		if info, err := fs.Stat(tree, entry.Name()+"/"+manifest); err == nil && info.Mode().IsRegular() {
			names = append(names, entry.Name())
		}
	}
	slices.Sort(names)
	return names
}

// Shadowed is the staged names that a skill under dir shadows. A skill
// counts when its SKILL.md is there, link or not, which is how the agent
// finds it too.
func Shadowed(dir string, staged []string) []string {
	found := []string{}
	for _, name := range staged {
		if info, err := os.Stat(filepath.Join(dir, name, manifest)); err == nil && info.Mode().IsRegular() {
			found = append(found, name)
		}
	}
	return found
}
