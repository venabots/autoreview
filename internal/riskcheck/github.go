package riskcheck

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// maxFiles is the most files GitHub lists for one PR. A PR at the limit may
// change more, and a script that never saw them cannot rate them.
const maxFiles = 3000

var (
	// namesLine is any line that starts to name a script, and scriptLine
	// one that names it as the file says: a path in backticks, alone.
	namesLine  = regexp.MustCompile(`(?m)^Risk script:`)
	scriptLine = regexp.MustCompile("(?m)^Risk script:[ \t]*`([^`\n]+)`[ \t]*$")
	// plainPath is a path inside the repository, with no leading slash.
	plainPath = regexp.MustCompile(`^[A-Za-z0-9_.-]+(/[A-Za-z0-9_.-]+)*$`)
)

// scriptPath is the script that REVIEW.md on the base names, or false when
// the base has no REVIEW.md or the file names no script. A line that tries
// to name one and does not is an error, not "no script": read as none, a
// typo would drop the check without a word.
func (c Checker) scriptPath(ctx context.Context, base string) (string, bool, error) {
	raw, err := c.fetch(ctx, "REVIEW.md", base)
	if notFound(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("reading REVIEW.md from %s: %w", base, err)
	}
	return namedScript(string(raw))
}

func namedScript(text string) (string, bool, error) {
	switch named := len(namesLine.FindAllStringIndex(text, -1)); {
	case named == 0:
		return "", false, nil
	case named > 1:
		return "", false, errors.New("REVIEW.md names more than one risk script")
	}
	m := scriptLine.FindStringSubmatch(text)
	if m == nil || !plainPath.MatchString(m[1]) || dotSegment(m[1]) {
		return "", false, errors.New("REVIEW.md names a risk script that is not a plain path in backticks, such as `bin/pr-risk`")
	}
	return m[1], true, nil
}

func dotSegment(path string) bool {
	for _, segment := range strings.Split(path, "/") {
		if segment == "." || segment == ".." {
			return true
		}
	}
	return false
}

// fetch reads one file of the repository as the ref has it. The ref goes as
// a field, so a branch name that holds '#' or '&' cannot cut the query.
func (c Checker) fetch(ctx context.Context, path, ref string) ([]byte, error) {
	return c.GH(ctx, "api", "--method", "GET", "-H", "Accept: application/vnd.github.raw",
		fmt.Sprintf("repos/%s/%s/contents/%s", c.Repo.Owner, c.Repo.Name, path), "-f", "ref="+ref)
}

// notFound says gh failed because the file is not there.
func notFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "(HTTP 404)")
}

// changed is every path the PR changes, with the old path of a rename too:
// a file moved out of a path the script guards changes that path.
func (c Checker) changed(ctx context.Context, pr int) ([]string, error) {
	out, err := c.GH(ctx, "api", "--paginate",
		fmt.Sprintf("repos/%s/%s/pulls/%d/files?per_page=100", c.Repo.Owner, c.Repo.Name, pr),
		"--jq", `.[] | [.filename, .previous_filename // ""] | @tsv`)
	if err != nil {
		return nil, fmt.Errorf("listing the PR's files: %w", err)
	}
	var paths []string
	files := 0
	for _, row := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if row == "" {
			continue
		}
		files++
		name, previous, _ := strings.Cut(row, "\t")
		paths = append(paths, name)
		if previous != "" {
			paths = append(paths, previous)
		}
	}
	if files >= maxFiles {
		return nil, fmt.Errorf("the PR changes %d files or more, and GitHub lists no more", maxFiles)
	}
	return paths, nil
}
