package riskcheck

import (
	"context"
	"errors"
	"maps"
	"os"
	"strings"
	"testing"

	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/repo"
)

const (
	trustedCommit = "0123456789abcdef0123456789abcdef01234567"
	contentsAt    = "repos/acme/widgets/contents/"
	filesAt       = "repos/acme/widgets/pulls/9/files?per_page=100"
	// lookup is the key of the call that asks for the default branch.
	lookup = "default branch"
	// files is the key of the call that lists the PR's changed files.
	files = "files"
)

const names = "# Review guidance\n\nRisk script: `bin/pr-risk`\n"

// onMain is GitHub's answer for the default branch: main, at trustedCommit.
var onMain = ok(`{"data":{"repository":{"defaultBranchRef":{"name":"main","target":{"oid":"` + trustedCommit + `"}}}}}`)

type answer struct {
	out string
	err error
}

func ok(out string) answer { return answer{out: out} }

func fails(msg string) answer { return answer{err: errors.New(msg)} }

// at is the key of a file as one ref has it, such as "REVIEW.md@main".
func at(path, ref string) string { return path + "@" + ref }

// github answers each call by what it asks for: a file by its path and the
// ref it is read at, the default branch by lookup, and the PR's files by
// files. A call with no answer is a 404, as GitHub gives for a file that is
// not there.
type github map[string]answer

func (g github) run(_ context.Context, args ...string) ([]byte, error) {
	a, found := g[callOf(args)]
	if !found {
		return nil, errors.New("gh api: gh: Not Found (HTTP 404)")
	}
	return []byte(a.out), a.err
}

// with is a copy of g that also answers key, so a case never changes the
// repository another case starts from.
func (g github) with(key string, a answer) github {
	next := maps.Clone(g)
	next[key] = a
	return next
}

func callOf(args []string) string {
	var path, ref string
	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "query="):
			return lookup
		case arg == filesAt:
			return files
		case strings.HasPrefix(arg, contentsAt):
			path = strings.TrimPrefix(arg, contentsAt)
		case strings.HasPrefix(arg, "ref="):
			ref = strings.TrimPrefix(arg, "ref=")
		}
	}
	return at(path, ref)
}

// scripted is a repository whose default branch names bin/pr-risk, with a
// PR that changes one path.
func scripted() github {
	return github{
		lookup:                           onMain,
		at("REVIEW.md", trustedCommit):   ok(names),
		at("bin/pr-risk", trustedCommit): ok("#!/bin/sh\necho default branch copy\n"),
		files:                            ok("a.ts\t\n"),
	}
}

// ran is one run of the fake script: what it was handed, the script's
// text, and what its gh settings directory held then, or -1 when there was
// none.
type ran struct {
	Script
	body     string
	ghConfig int
}

// script is a fake run of the script: it keeps what it was handed and
// prints out, or fails with err.
type script struct {
	got []ran
	out string
	err error
}

func (s *script) run(_ context.Context, sc Script) ([]byte, error) {
	body, _ := os.ReadFile(sc.Path)
	entries := -1
	if dir, found := variable(sc.Env, "GH_CONFIG_DIR"); found {
		if listed, err := os.ReadDir(dir); err == nil {
			entries = len(listed)
		}
	}
	s.got = append(s.got, ran{Script: sc, body: string(body), ghConfig: entries})
	return []byte(s.out), s.err
}

// variable is the value env gives name, as a program would read it.
func variable(env []string, name string) (string, bool) {
	value, found := "", false
	for _, entry := range env {
		if n, v, cut := strings.Cut(entry, "="); cut && n == name {
			value, found = v, true
		}
	}
	return value, found
}

func pr9() prs.PR { return prs.PR{Number: 9, Base: "main", Head: "abc123"} }

func checker(t *testing.T, g github, s *script) Checker {
	return Checker{GH: g.run, Repo: repo.Context{Owner: "acme", Name: "widgets", Root: "/repo"}, Dir: t.TempDir(), Run: s.run}
}
