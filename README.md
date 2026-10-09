# autoreview

One place to review your team's PRs and handle your own.

This is version 2, a rewrite in Go. Version 1 is the Rust repo
`venabots/autoreview`, and it stays in use until this one replaces it.

## Status

On a terminal it opens the full-screen view and watches: at each look it
reviews the PRs that have become ready. Off a terminal it makes one pass,
with a summary and an exit status a script can trust. `--babysit` keeps a
headless run going until nothing is left, and `--watch` keeps it going for
good.

- **Review** lists the open PRs in the repo and says where each one stands:
  ready, held for CI, held under another PR, seen, or approved.
- **My PRs** lists your own open PRs and says what blocks each one:
  conflicts, failing CI, requested changes, open threads. `f` fixes the
  selected PR once, `c` answers its review comments and `u` fixes its
  conflicts, each in a worktree of its own. `b` babysits it: it is fixed
  again each time it changes and needs work. `B` babysits all of them.
  These run the `babysit-pr`, `pr-comment-handler` and `sync-main` skills
  you have installed.

`autoreview stats` reports how each panelist model has done. It reads the
same ledger as version 1.

[docs/plan.md](docs/plan.md) has the milestones that come next.

## Run it

```sh
go build -o autoreview ./cmd/autoreview
cd some/github/checkout
/path/to/autoreview
```

You need `gh`, logged in (`gh auth login`). A review also needs `dash-p`
and `claude` (or `codex`).

- On a terminal, it opens the full-screen view and watches. A reviewed PR
  rests 30 minutes, and gets at most three reviews in a run. `R` reviews the selected PR now, `x x` stops a
  review, `r` reopens a finished one in a new terminal tab, `l` shows the
  run log, `w` stops or starts watching. A running review shows what it is
  doing. Press `?` for every key and `q` to
  quit. On exit it prints one
  line, and the full summary is in the run log.
- On a pipe, or with `--headless`, it makes one pass: it reviews every PR
  that is ready and exits 0 only when every review ran to its end.
- `--no-post` reviews and leaves the PR alone.
- `autoreview 213 240` limits the run to those PRs.
- `autoreview --help` lists the flags.

## Try it without a real review

```sh
scripts/sandbox.sh              # the view, against a fake GitHub and a fake reviewer
scripts/sandbox.sh --headless   # one plain pass
```

Nothing reaches GitHub and no agent runs. The top of the script lists the
variables that change what the fakes do. The sandbox has an origin with your
PR's branch, so the My PRs keys work there too.

## Develop

```sh
go test -race ./...
golangci-lint run
```

[AGENTS.md](AGENTS.md) has the conventions. [docs/rules.md](docs/rules.md)
lists the rules that carry over from version 1.
