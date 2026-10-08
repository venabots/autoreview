# autoreview

One place to review your team's PRs and handle your own.

This is version 2, a rewrite in Go. Version 1 is the Rust repo
`venabots/autoreview`, and it stays in use until this one replaces it.

## Status

This build is read-only. It shows both lists and starts no review.

- **Review** lists the open PRs in the repo and says where each one stands:
  ready, held for CI, held under another PR, seen, or approved.
- **My PRs** lists your own open PRs and says what blocks each one:
  conflicts, failing CI, requested changes, open threads.

[docs/plan.md](docs/plan.md) has the milestones that come next.

## Run it

```sh
go build -o autoreview ./cmd/autoreview
cd some/github/checkout
/path/to/autoreview
```

You need `gh`, logged in (`gh auth login`).

- On a terminal, it opens the full-screen view and looks at the PR list every
  two minutes. Press `?` for the keys and `q` to quit.
- On a pipe, it looks once and prints one line for each PR.
- `autoreview 213 240` limits the run to those PRs.
- `autoreview --help` lists the flags.

## Develop

```sh
go test -race ./...
golangci-lint run
```

[AGENTS.md](AGENTS.md) has the conventions. [docs/rules.md](docs/rules.md)
lists the rules that carry over from version 1.
