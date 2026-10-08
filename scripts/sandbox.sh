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

cd "$sandbox/repo"
exec env \
  PATH="$sandbox/bin:$PATH" \
  CLAUDE_CONFIG_DIR="$sandbox/claude" \
  FAKE_GH_DIR="$sandbox/fixtures" \
  FAKE_GH_LOGIN=me \
  FAKE_GH_REVIEWS="${FAKE_GH_REVIEWS-9:APPROVED}" \
  FAKE_DASHP_SLEEP="${FAKE_DASHP_SLEEP-6}" \
  AUTOREVIEW_LEDGER="$sandbox/ledger.jsonl" \
  AUTOREVIEW_LOG_DIR="$sandbox/logs" \
  autoreview "$@"
