// Package repo finds out which repository a run is about and who is running
// it. Every later step depends on these four values, so each one is checked
// here once and not once per PR.
package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/venabots/autoreview/internal/gh"
)

// Context is the repository a run reviews and the person running it.
type Context struct {
	Owner string
	Name  string
	// Root is the checkout's top directory. Reviews run in it, and it is
	// hashed into every session id, so it is kept byte for byte.
	Root string
	// Me is the GitHub login the reviews are posted as.
	Me string
}

// Slug is the repository as GitHub writes it.
func (c Context) Slug() string {
	return c.Owner + "/" + c.Name
}

// Git runs one git command in the current directory and returns its stdout.
type Git func(ctx context.Context, args ...string) ([]byte, error)

// Load asks gh and git for the context. It fails with a message a person
// can act on: not a GitHub repo, not a checkout, or not logged in.
func Load(ctx context.Context, run gh.Runner, git Git) (Context, error) {
	view, err := run(ctx, "repo", "view", "--json", "owner,name")
	if err != nil {
		return Context{}, fmt.Errorf("not a GitHub repo, or gh is not logged in: %w", err)
	}
	owner, name, err := parseRepoView(view)
	if err != nil {
		return Context{}, err
	}

	// gh can answer from $GH_REPO outside any checkout. An empty root would
	// hash into every session id and fail every review one PR at a time.
	toplevel, err := git(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return Context{}, fmt.Errorf("not inside a git checkout: %w", err)
	}
	root, err := RootFrom(toplevel)
	if err != nil {
		return Context{}, err
	}

	login, err := run(ctx, "api", "user", "--jq", ".login")
	if err != nil {
		return Context{}, fmt.Errorf("could not read your GitHub login: %w", err)
	}
	me, err := loginFrom(login)
	if err != nil {
		return Context{}, err
	}

	return Context{Owner: owner, Name: name, Root: root, Me: me}, nil
}

type repoView struct {
	Owner struct {
		Login string `json:"login"`
	} `json:"owner"`
	Name string `json:"name"`
}

func parseRepoView(raw []byte) (owner, name string, err error) {
	var v repoView
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", "", fmt.Errorf("gh repo view gave no repository: %w", err)
	}
	if v.Owner.Login == "" || v.Name == "" {
		return "", "", errors.New("gh repo view gave no owner or name")
	}
	return v.Owner.Login, v.Name, nil
}

// RootFrom is the repo root out of `git rev-parse --show-toplevel`.
//
// It removes exactly the one newline git adds. A directory name may end in
// a space, or in a newline of its own, and trimming either would name a
// directory nobody has. A path that is not valid UTF-8 is refused: it is
// sent to a shell and hashed into session ids, so it has to be exact.
func RootFrom(stdout []byte) (string, error) {
	if !utf8.Valid(stdout) {
		return "", errors.New("the repo path is not valid UTF-8")
	}
	root := strings.TrimSuffix(string(stdout), "\n")
	if root == "" {
		return "", errors.New("git rev-parse --show-toplevel printed nothing")
	}
	return root, nil
}

// loginFrom refuses an empty login. gh can exit 0 and still print nothing,
// and an empty login would mislabel every PR as somebody else's.
func loginFrom(stdout []byte) (string, error) {
	me := strings.TrimSpace(string(stdout))
	if me == "" {
		return "", errors.New("gh api user returned an empty login")
	}
	return me, nil
}
