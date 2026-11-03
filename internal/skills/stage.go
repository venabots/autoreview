package skills

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// subdir is where an agent looks for skills inside a directory it is
// handed.
const subdir = ".claude/skills"

// executable is the mode a staged script gets.
const executable = 0o755

// Stage reads the source's skills into memory and writes them under dir
// as <dir>/.claude/skills/<name>/.... It returns what it wrote, so that the
// run can write the same copy again before each review (Staged.Again).
// With NotStaged it writes nothing, and the reviewer is handed no
// directory.
func Stage(s Source, bundle fs.FS, dir string) (Staged, error) {
	tree := bundle
	switch s.Kind {
	case NotStaged:
		return Staged{}, nil
	case FromDir:
		tree = os.DirFS(s.Dir)
	}
	staged := Staged{Dir: dir}
	for _, name := range namesIn(tree) {
		entries, err := read(tree, name)
		if err != nil {
			return Staged{}, err
		}
		staged.entries = append(staged.entries, entries...)
	}
	return staged, staged.Again()
}

// AddDirFlag is the flag that hands the staged directory to the reviewer.
// It is one token: dash-p forwards an unknown flag only in that form.
func AddDirFlag(dir string) string {
	return "--add-dir=" + dir
}

// entry is one staged file or directory, held in memory. Its path is
// slash-separated and under the skills directory.
type entry struct {
	path string
	dir  bool
	raw  []byte
	mode fs.FileMode
}

// private is the mode of each staged directory.
const private = 0o700

// read reads one directory of the tree, the directory first. Only the
// skill directories are read, so the staged set is exactly what Names
// reports: a .git or a log directory next to the skills is never copied
// along. A link is followed and not recreated: the agent needs the files.
// A .git inside a skill is left out too: a skill installed by git clone
// carries its history, which the agent has no use for and which can
// outweigh the skill many times over.
func read(tree fs.FS, from string) ([]entry, error) {
	children, err := fs.ReadDir(tree, from)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", from, err)
	}
	entries := []entry{{path: from, dir: true}}
	for _, child := range children {
		if child.Name() == ".git" {
			continue
		}
		source := from + "/" + child.Name()
		// Stat follows a link. The child's own type would report the link.
		info, err := fs.Stat(tree, source)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", source, err)
		}
		if info.IsDir() {
			below, err := read(tree, source)
			if err != nil {
				return nil, err
			}
			entries = append(entries, below...)
			continue
		}
		raw, err := fs.ReadFile(tree, source)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", source, err)
		}
		entries = append(entries, entry{path: source, raw: raw, mode: modeFor(source, info.Mode())})
	}
	return entries, nil
}

// modeFor is the mode a staged file gets. A script keeps the execute bit it
// has on disk. The embedded tree carries no modes at all, and the skills
// run their helper scripts directly, so every .sh file gets the bit
// whatever the source says. A test holds the repo to that spelling: an
// executable helper under skills/ must be a .sh.
func modeFor(name string, mode fs.FileMode) fs.FileMode {
	if strings.HasSuffix(name, ".sh") || mode&0o111 != 0 {
		return executable
	}
	return 0o644
}

// writeFile writes one file whole, under a temporary name, and renames it
// into place. A run stages again before each review, while another review
// may be reading the skills or running a helper script. Written in place,
// the file would be empty or half written for a moment, and a shell reads
// its script while it runs. A rename swaps in the whole file at once, and
// a reader that has the old one open keeps reading the old one.
func writeFile(to string, raw []byte, mode fs.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(to), "."+filepath.Base(to)+".*")
	if err != nil {
		return fmt.Errorf("writing %s: %w", to, err)
	}
	if err := fill(tmp, raw, mode); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("writing %s: %w", to, err)
	}
	if err := os.Rename(tmp.Name(), to); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("writing %s: %w", to, err)
	}
	return nil
}

// fill writes the content, closes the file, and then sets the mode. A mode
// given when a file is made is cut by the umask, and a reviewer under a
// strict umask would find a script it cannot run.
func fill(f *os.File, raw []byte, mode fs.FileMode) error {
	_, err := f.Write(raw)
	return errors.Join(err, f.Close(), os.Chmod(f.Name(), mode))
}

// ShadowNote is what to say when skills under any of dirs shadow the ones
// a run staged, or empty when none does.
func ShadowNote(dirs, staged []string) string {
	found := []string{}
	for _, dir := range dirs {
		if names := Shadowed(dir, staged); len(names) > 0 {
			found = append(found, strings.Join(names, ", ")+" under "+dir)
		}
	}
	if len(found) == 0 {
		return ""
	}
	return strings.Join(found, " and ") + " shadow the staged copies; the installed skills run"
}
