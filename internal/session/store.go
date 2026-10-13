package session

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
)

// ConfigDir is Claude Code's configuration directory: $CLAUDE_CONFIG_DIR,
// else ~/.claude. Sessions and the person's own skills live under it.
func ConfigDir(env func(string) string) string {
	if dir := env("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir
	}
	return filepath.Join(env("HOME"), ".claude")
}

// Store is where Claude Code keeps its session transcripts.
type Store struct {
	// Projects is the directory with one subdirectory for each project.
	Projects string
}

// StoreIn is the session store under a configuration directory.
func StoreIn(configDir string) Store {
	return Store{Projects: filepath.Join(configDir, "projects")}
}

// Transcript is the transcript file for a session, wherever Claude Code put
// it.
//
// Sessions live under a directory named for the escaped working directory.
// That escaping is not documented and has changed before, so this looks in
// every project directory and does not rebuild the name. Looking wider than
// the repo is safe, because the id already encodes the checkout.
func (s Store) Transcript(id string) (string, bool) {
	if id == "" {
		return "", false
	}
	projects, err := os.ReadDir(s.Projects)
	if err != nil {
		return "", false
	}
	for _, project := range projects {
		path := filepath.Join(s.Projects, project.Name(), id+".jsonl")
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return path, true
		}
	}
	return "", false
}

// Exists says a session with this id is already on disk.
func (s Store) Exists(id string) bool {
	_, ok := s.Transcript(id)
	return ok
}

// InUse says another process still holds this session open.
//
// Claude Code treats an id as taken once its transcript exists, so it does
// not stop a second process from reopening a live session, and two agents
// would then write one transcript. pgrep matches the id on any command
// line. A false match costs one fresh review, so the check fails safe.
func InUse(ctx context.Context, id string) bool {
	if id == "" {
		return false
	}
	return exec.CommandContext(ctx, "pgrep", "-f", "--", id).Run() == nil
}
