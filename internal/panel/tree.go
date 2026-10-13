package panel

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/venabots/autoreview/internal/sanitize"
	"github.com/venabots/autoreview/internal/words"
)

// resolveTree is the committed files at HEAD, or those under one path.
//
// HEAD and not the working tree, so every panelist gets a worktree and may
// run the tests: an audit that can run the suite reports a failing test as
// evidence, not a guess. The label names the commit, so a person with
// uncommitted edits can see they were not part of it.
func resolveTree(g, here gitIn, path string) (Resolved, error) {
	head, found, err := g.ok("rev-parse", "--verify", "--quiet", "HEAD^{commit}")
	if err != nil {
		return Resolved{}, err
	}
	if !found {
		return Resolved{}, errors.New("--tree: this repository has no commits yet")
	}
	sha := strings.TrimSpace(head)
	scope := ""
	if path != "" {
		if scope, err = scopeFor(g.dir, here, path); err != nil {
			return Resolved{}, err
		}
	}
	files, err := treeFiles(g, sha, scope)
	if err != nil {
		return Resolved{}, err
	}
	if len(files) == 0 {
		if scope != "" {
			return Resolved{}, fmt.Errorf("nothing to review: no file under %q at HEAD", sanitize.Line(scope))
		}
		return Resolved{}, errors.New("nothing to review: HEAD has no files")
	}
	under := ""
	if scope != "" {
		under = " under " + sanitize.Line(scope)
	}
	return Resolved{
		Label:    fmt.Sprintf("%s%s at %s on %s", words.Count(len(files), "file"), under, sha[:min(len(sha), 7)], branch(g)),
		Subject:  Subject{Audit: true, Files: files, Scope: scope},
		Isolated: true,
		SHA:      sha,
	}, nil
}

// treeFiles lists the files of one commit. The SHA and not HEAD: a commit
// that landed between two calls would otherwise list one commit's files
// and pin the worktrees to another. Literal pathspecs, so a directory named
// like git's pathspec magic, such as `:(top)src`, names that directory.
func treeFiles(g gitIn, sha, scope string) ([]string, error) {
	args := []string{"--literal-pathspecs", "ls-tree", "-r", "-z", "--full-tree", sha}
	if scope != "" {
		args = append(args, "--", scope)
	}
	raw, err := g.out(args...)
	if err != nil {
		return nil, err
	}
	return blobPaths(raw), nil
}

// blobPaths is the file paths in `ls-tree -r -z` output, without
// submodules. A submodule is a gitlink whose directory is empty in every
// worktree, so a panelist told to read it has nothing to open.
func blobPaths(raw string) []string {
	var out []string
	for entry := range strings.SplitSeq(raw, "\x00") {
		meta, path, ok := strings.Cut(entry, "\t")
		if !ok {
			continue
		}
		if kind := strings.Fields(meta); len(kind) > 1 && kind[1] == "blob" {
			out = append(out, path)
		}
	}
	return out
}

// scopeFor is the path the person named, as a path from the repository
// root. A relative path is read from where the person stands, so `--tree .`
// inside `src/` means `src/` and not the whole repository.
func scopeFor(root string, here gitIn, path string) (string, error) {
	if filepath.IsAbs(path) {
		inside, ok := within(realPath(path), root)
		if !ok {
			return "", fmt.Errorf("--tree: %q is outside this repository", path)
		}
		return scopeWithin("", inside, path)
	}
	// Checked: an empty prefix from a failed call would read the path from
	// the root and audit the wrong directory without a word.
	prefix, err := here.out("rev-parse", "--show-prefix")
	if err != nil {
		return "", err
	}
	return scopeWithin(strings.TrimSpace(prefix), path, path)
}

// within is path relative to root, and false when it is not under root.
func within(path, root string) (string, bool) {
	if path == root {
		return "", true
	}
	if rest, ok := strings.CutPrefix(path, strings.TrimSuffix(root, "/")+"/"); ok {
		return rest, true
	}
	return "", false
}

// realPath is the path with its longest existing ancestor made canonical.
// git reports the root at its real path, and on macOS a path under /var is
// really under /private/var. The path names something at HEAD, which need
// not exist on disk, so the part that does not exist is kept as typed.
func realPath(path string) string {
	clean := filepath.Clean(path)
	for ancestor := clean; ; ancestor = filepath.Dir(ancestor) {
		if real, err := filepath.EvalSymlinks(ancestor); err == nil {
			rest, _ := filepath.Rel(ancestor, clean)
			return filepath.Join(real, rest)
		}
		if ancestor == filepath.Dir(ancestor) {
			return clean
		}
	}
}

// scopeWithin joins a relative path onto the directory the person stands
// in, both relative to the root, and resolves `.` and `..` by hand. Lexical
// on purpose: the path names something at HEAD, which need not exist on
// disk. "" is the root itself, which is the whole repository. typed is the
// path as the person wrote it, for the error.
func scopeWithin(prefix, path, typed string) (string, error) {
	var parts []string
	for part := range strings.SplitSeq(prefix+"/"+path, "/") {
		switch part {
		case "", ".":
		case "..":
			if len(parts) == 0 {
				return "", fmt.Errorf("--tree: %q is outside this repository", typed)
			}
			parts = parts[:len(parts)-1]
		default:
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, "/"), nil
}
