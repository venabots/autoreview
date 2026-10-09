// Package session decides which Claude Code session a review belongs to.
//
// A PR's session id is derived from the checkout and the PR, not recorded in
// a state file, so the mapping is stable across runs with nothing to keep in
// step. The checkout is in the hash on purpose: a second clone or a worktree
// of the same repo gets its own id for the same PR, so one checkout can
// never resume, and corrupt, another's session.
package session

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
)

// ID is the session id for one PR in one checkout, in the v5 UUID form that
// `claude --session-id` accepts.
//
// The derivation is pinned by golden tests. Reviews already on disk were
// filed under these ids, and a change here does not fail loudly: it quietly
// stops finding anyone's earlier review. The input still starts with
// "review-prs:", the name of a v1 program that no longer exists, for that
// reason.
//
// Hex digits 12 and 16 of the digest are replaced by literals. Digit 12 is
// the UUID version (5), and digit 16 is the variant (a).
func ID(repoRoot, owner, name string, pr int) string {
	sum := md5.Sum(fmt.Appendf(nil, "review-prs:%s:%s/%s#%d", repoRoot, owner, name, pr))
	h := hex.EncodeToString(sum[:])
	return fmt.Sprintf("%s-%s-5%s-a%s-%s", h[0:8], h[8:12], h[13:16], h[17:20], h[20:32])
}

// FreshID is a new random session id, in the v4 UUID form. It is for a
// task, which belongs to no derived session: a fix on your own PR is not a
// review, and resuming the PR's review session for it would hand the skill
// a conversation about somebody else's work.
func FreshID(random io.Reader) (string, error) {
	var b [16]byte
	if _, err := io.ReadFull(random, b[:]); err != nil {
		return "", fmt.Errorf("no randomness for a session id: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", h[0:8], h[8:12], h[12:16], h[16:20], h[20:32]), nil
}

// UUIDShaped says s is 36 characters of hex digits and hyphens. Only a
// string that passes goes onto a command line: an id read back from a file
// or from an agent's output is other people's text.
func UUIDShaped(s string) bool {
	return len(s) == 36 && strings.Trim(s, "0123456789abcdefABCDEF-") == ""
}
