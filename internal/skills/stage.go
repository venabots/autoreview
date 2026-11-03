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

// Stage writes the source's skills under dir as
// <dir>/.claude/skills/<name>/... and reports whether it staged any. With
// NotStaged it writes nothing, and the reviewer is handed no directory.
func Stage(s Source, bundle fs.FS, dir string) (bool, error) {
	target := filepath.Join(dir, filepath.FromSlash(subdir))
	switch s.Kind {
	case NotStaged:
		return false, nil
	case FromDir:
		return true, copySkills(os.DirFS(s.Dir), target)
	}
	return true, copySkills(bundle, target)
}

// AddDirFlag is the flag that hands the staged directory to the reviewer.
// It is one token: dash-p forwards an unknown flag only in that form.
func AddDirFlag(dir string) string {
	return "--add-dir=" + dir
}

// copySkills copies the skill directories and nothing beside them, so the
// staged set is exactly what Names reports. A .git or a log directory next
// to the skills is never copied along.
func copySkills(tree fs.FS, target string) error {
	if err := os.MkdirAll(target, private); err != nil {
		return fmt.Errorf("making %s: %w", target, err)
	}
	for _, name := range namesIn(tree) {
		if err := copyTree(tree, name, filepath.Join(target, name)); err != nil {
			return err
		}
	}
	return nil
}

// private is the mode of each staged directory.
const private = 0o700

// copyTree copies one directory. A link is followed and not recreated: the
// agent needs the files. A .git is left out: a skill installed by git clone
// carries its history, which the agent has no use for and which can
// outweigh the skill many times over.
func copyTree(tree fs.FS, from, to string) error {
	if err := os.MkdirAll(to, private); err != nil {
		return fmt.Errorf("making %s: %w", to, err)
	}
	entries, err := fs.ReadDir(tree, from)
	if err != nil {
		return fmt.Errorf("reading %s: %w", from, err)
	}
	for _, entry := range entries {
		if entry.Name() == ".git" {
			continue
		}
		source := from + "/" + entry.Name()
		dest := filepath.Join(to, entry.Name())
		// Stat follows a link. The entry's own type would report the link.
		info, err := fs.Stat(tree, source)
		if err != nil {
			return fmt.Errorf("reading %s: %w", source, err)
		}
		if info.IsDir() {
			err = copyTree(tree, source, dest)
		} else {
			err = copyFile(tree, source, dest, modeFor(source, info.Mode()))
		}
		if err != nil {
			return err
		}
	}
	return nil
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

// copyFile writes one file whole, under a temporary name, and renames it
// into place. A run stages again before each review, while another review
// may be reading the skills or running a helper script. Written in place,
// the file would be empty or half written for a moment, and a shell reads
// its script while it runs. A rename swaps in the whole file at once, and
// a reader that has the old one open keeps reading the old one.
func copyFile(tree fs.FS, from, to string, mode fs.FileMode) error {
	raw, err := fs.ReadFile(tree, from)
	if err != nil {
		return fmt.Errorf("reading %s: %w", from, err)
	}
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
