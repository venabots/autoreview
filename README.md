# autoreview

One place to review your team's PRs and handle your own.

This is version 2, a rewrite in Go. Version 1 is the Rust repo
`venabots/autoreview`, and it stays in use until this one replaces it.

## Status

Off a terminal, this build reviews: one pass over every PR that is ready,
with a summary and an exit status a script can trust. On a terminal, the
view shows both lists and starts no review yet.

- **Review** lists the open PRs in the repo and says where each one stands:
  ready, held for CI, held under another PR, seen, or approved.
- **My PRs** lists your own open PRs and says what blocks each one:
  conflicts, failing CI, requested changes, open threads.

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

- On a terminal, it opens the full-screen view and looks at the PR list every
  two minutes. Press `?` for the keys and `q` to quit.
- On a pipe, or with `--headless`, it makes one pass: it reviews every PR
  that is ready and exits 0 only when every review ran to its end.
- `--no-post` reviews and leaves the PR alone.
- `autoreview 213 240` limits the run to those PRs.
- `autoreview --help` lists the flags.

## Develop

```sh
go test -race ./...
golangci-lint run
```

[AGENTS.md](AGENTS.md) has the conventions. [docs/rules.md](docs/rules.md)
lists the rules that carry over from version 1.
