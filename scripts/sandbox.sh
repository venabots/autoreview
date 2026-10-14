#!/usr/bin/env bash
# Run autoreview against a fake GitHub and a fake reviewer.
#
# Nothing reaches GitHub and no agent runs, so this is the safe way to try
# the view. The fakes are the end-to-end tests' own: the test binary, under
# the names gh, dash-p and claude. The repo they serve has seven PRs, one of
# each kind.
#
#   scripts/sandbox.sh                 the view
#   scripts/sandbox.sh --headless      one plain pass
#   scripts/sandbox.sh 6               only PR #6
#
# Variables that change what the fakes do:
#   FAKE_DASHP_SLEEP=6       seconds each review takes (default 6)
#   FAKE_DASHP_FAIL="9"      PRs whose review fails with exit 10
#   FAKE_DASHP_DECISION=...  the reviewer's own claim (default commented)
#   FAKE_GH_REVIEWS="9:APPROVED 8:CHANGES_REQUESTED"
#                            what GitHub says landed (default 9:APPROVED)
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
sandbox=${AUTOREVIEW_SANDBOX:-${TMPDIR:-/tmp}/autoreview-sandbox}
mkdir -p "$sandbox/bin" "$sandbox/fixtures" "$sandbox/repo" "$sandbox/claude"

(
  cd "$root"
  go build -o "$sandbox/bin/autoreview" ./cmd/autoreview
  go test -c -o "$sandbox/bin/fakebin" ./cmd/autoreview
)
for name in gh dash-p claude codex; do
  ln -sf "$sandbox/bin/fakebin" "$sandbox/bin/$name"
done
cp "$root/internal/prs/testdata/prs.json" "$sandbox/fixtures/prs.json"
cp "$root/cmd/autoreview/testdata/mine.json" "$sandbox/fixtures/mine.json"
cp "$root/cmd/autoreview/testdata/repo.json" "$sandbox/fixtures/repo.json"
[ -d "$sandbox/repo/.git" ] || git -C "$sandbox/repo" init --quiet

# The tasks on the My PRs tab (f, c, u, b, B) need your PR's branch on an
# origin to make a worktree from, and their skills installed.
if [ ! -d "$sandbox/origin" ]; then
  git init --quiet --bare "$sandbox/origin"
  git -C "$sandbox/repo" -c user.name=Sandbox -c user.email=sandbox@example.com \
    commit --quiet --allow-empty -m start
  git -C "$sandbox/repo" remote add origin "$sandbox/origin"
  git -C "$sandbox/repo" push --quiet origin HEAD:refs/heads/main HEAD:refs/heads/me/my-own-work
fi
for skill in babysit-pr pr-comment-handler sync-main; do
  mkdir -p "$sandbox/claude/skills/$skill"
  echo "# $skill" >"$sandbox/claude/skills/$skill/SKILL.md"
done

# A variable of yours that names a reviewer would run it in place of the
# fake one: $AUTOREVIEW_CMD replaces the reviewer and $DASHP_BIN names a real
# dash-p. Both are set aside here, so the sandbox keeps its promise whatever
# your shell exports. A run stops when $AUTOREVIEW_AUTO_CMD is set, so that
# one goes too.
#
# The settings file is the sandbox's own, unless $AUTOREVIEW_CONFIG names
# another. The first run asks for the settings and writes them there, never
# to ~/.config.
cd "$sandbox/repo"
exec env \
  -u AUTOREVIEW_CMD \
  -u AUTOREVIEW_AUTO_CMD \
  DASHP_BIN="$sandbox/bin/dash-p" \
  PATH="$sandbox/bin:$PATH" \
  CLAUDE_CONFIG_DIR="$sandbox/claude" \
  FAKE_GH_DIR="$sandbox/fixtures" \
  FAKE_GH_LOGIN=me \
  FAKE_GH_REVIEWS="${FAKE_GH_REVIEWS-9:APPROVED}" \
  FAKE_DASHP_SLEEP="${FAKE_DASHP_SLEEP-6}" \
  AUTOREVIEW_LEDGER="$sandbox/ledger.jsonl" \
  AUTOREVIEW_LOG_DIR="$sandbox/logs" \
  AUTOREVIEW_CONFIG="${AUTOREVIEW_CONFIG-$sandbox/config.toml}" \
  autoreview "$@"
