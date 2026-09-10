# Contributing

Thanks for looking. recall is small on purpose, so most changes are a single
package plus a test. This page covers the rules that are not obvious from the
code.

## Build and test

```sh
export CGO_ENABLED=0 GOFLAGS=-mod=mod
go build ./... && go vet ./... && go test ./...
make lint-free          # gofmt + vet
make run-dry            # recall against fixtures, never against ~/.claude
```

Go 1.25 or newer, no cgo, no Homebrew dependencies.

## Fixtures: no real data, ever

Everything under `testdata/fixtures` is written by hand to match the observed
Claude Code 2.1.x schemas. Nothing there is copied from a real transcript,
history file or registry entry. When you add a fixture:

- Make up the prompts and answers. Keep them short and obviously fake.
- Use invented paths (`/home/u/dev/proj`, `/Users/u/work/app`), never a path
  from your own machine.
- Use UUIDs that are clearly synthetic (`0f1e2d3c-4b5a-4978-8a6b-5c4d3e2f1a0b`)
  and pids under 1000.
- Never include tokens, `*.key` files, `control.key`, bridge ids from a real
  session, or anything from `~/.claude.json`.
- Match the schema exactly: envelope keys on conversational records,
  metadata records without envelopes, `history.jsonl` with epoch
  milliseconds. `docs/compatibility.md` lists the fields.

Tests copy fixtures into `t.TempDir()` and point `model.PathsFrom` at it.
No test may read or write the real `~/.claude` or `~/.recall`: set
`CLAUDE_CONFIG_DIR`, `RECALL_DIR` and `HOME` with `t.Setenv`.

## Things tests must not do

- Start `claude` interactively, or run `claude --resume`, `-c`, `--bg`,
  `attach`. Launching is tested with `RECALL_DRY_RUN=1`, which prints the
  command instead of running it.
- Create tmux sessions. Assert the generated argv instead.
- Run osascript that changes windows or tabs. Assert the script text.
- Run `recall setup`, `recall uninstall`, hook install or retention writes
  against anything but a temp dir. The `cmd/recall` tests do not run `setup`
  at all; they test its steps in isolation.

## Style

- gofmt, vet clean, no new dependencies without a reason in the PR.
- Every exported symbol in the `internal` packages is part of a contract
  used by sibling packages; add, do not rename.
- No em dashes in code, comments, docs or UI strings. Use a colon, a comma
  or a plain hyphen.
- User-facing state words are painted as text next to the color dot so the
  UI reads without color; keep that when adding states.
- Small commits with a `feat(scope):`, `fix(scope):`, `docs:` or `test:`
  prefix.

## Reporting a schema change

If Claude Code changes one of the four contracts in
`docs/compatibility.md`, open an issue with the `claude --version` output,
a synthesized line showing the new shape, and what `recall doctor` printed.
Do not attach real transcripts.
