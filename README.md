<div align="center">

# recall

**Every Claude Code terminal session, listed, searchable, and one keypress away. Even after you closed the window.**

[![CI](https://github.com/MohammedAl-Alimi/recall/actions/workflows/ci.yml/badge.svg)](https://github.com/MohammedAl-Alimi/recall/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/MohammedAl-Alimi/recall)](https://github.com/MohammedAl-Alimi/recall/releases/latest)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue)](LICENSE)
[![Go 1.25](https://img.shields.io/badge/go-1.25-00ADD8)](go.mod)
[![Platform](https://img.shields.io/badge/platform-macOS%20%7C%20Linux-lightgrey)](#install)

</div>

```text
$ recall ls
ID        STATE      AGE         PROJECT    BRANCH           TITLE
0f1e2d3c  Needs you  2m          harbor     fix/auth-loop    OAuth refresh loop on token expiry
7c2e91aa  Needs you  9m          quill      main             Import legacy posts from the old CMS
1a2b3c4d  Running    now         atlas-cli  feat/scan        Scanner incremental cache
5e6f7a8b  Running    14m         lantern    main             api: rate limit per token
2b3c4d5e  Closed     3h          tally      main             Fix flaky calendar popover test
9c0d1e2f  Closed     6h          harbor     main             Add Postgres migration for invoices
3c4d5e6f  Closed     2d          orbit      docs/quickstart  Rewrite the getting started guide
8b9c0d1e  Expiring   28d         meadow     main             Migrate config loader to TOML
4d5e6f70  Gone       2026-07-30  lantern                     Kickoff notes for the billing rewrite
```

## Why

Claude Code writes a transcript for every session, then deletes it after 30
days. `claude --resume` only lists the sessions of the directory you are in,
and only if you remember which directory that was.

So you keep tabs open. Closing one feels unsafe, because the session where
the fix was half done might never come back. Twelve tabs later the laptop
reboots and they are gone anyway.

And nothing tells you which of those twelve sessions is waiting for you. One
of them stopped on a permission prompt an hour ago. You find out when you
click through them.

recall fixes all three. It reads the files Claude Code already writes, keeps
them, lists them, and reopens the one you pick.

## What it does

- **See every session.** One list across all projects, sorted by what needs
  you first, then pinned, then most recent.
- **Know what it was about.** Title, project, branch, last prompt, last
  answer, open PRs, context size, and a plain state word: Needs you,
  Running, Closed, Expiring, Gone.
- **Open with one keypress.** Enter focuses the tab a live session is still
  in, attaches the tmux session that kept it alive, or runs
  `claude --resume` in the right directory with the flags it was started
  with. Whatever loses the least.
- **Needs-you routing.** A session blocked on a permission prompt sorts to
  the top, can raise a macOS notification, and after it exits the pending
  tool call is still shown.
- **Keeps your history.** `recall setup` raises Claude's retention and
  installs a hook that archives each session under `~/.recall` when it
  ends. A session from last spring still resumes.
- **Dashboard.** `recall serve` shows the same list in your browser, local
  only, with Open buttons on every card.
- **Terminal.app, iTerm2 or cmux.** Pick where a session opens with
  `--terminal`. With cmux installed, each session gets its own workspace.

## Install

Homebrew:

```sh
brew tap MohammedAl-Alimi/tap
brew install recall
```

Direct download. Each archive holds the `recall` binary; unpack it and put
it on your `PATH`:

| Platform | Download |
| --- | --- |
| macOS Apple Silicon | [recall_0.1.0_darwin_arm64.tar.gz](https://github.com/MohammedAl-Alimi/recall/releases/latest/download/recall_0.1.0_darwin_arm64.tar.gz) |
| macOS Intel | [recall_0.1.0_darwin_amd64.tar.gz](https://github.com/MohammedAl-Alimi/recall/releases/latest/download/recall_0.1.0_darwin_amd64.tar.gz) |
| Linux x86_64 | [recall_0.1.0_linux_amd64.tar.gz](https://github.com/MohammedAl-Alimi/recall/releases/latest/download/recall_0.1.0_linux_amd64.tar.gz) |
| Linux arm64 | [recall_0.1.0_linux_arm64.tar.gz](https://github.com/MohammedAl-Alimi/recall/releases/latest/download/recall_0.1.0_linux_arm64.tar.gz) |

All releases: [github.com/MohammedAl-Alimi/recall/releases](https://github.com/MohammedAl-Alimi/recall/releases).

Go 1.25 or newer:

```sh
go install github.com/MohammedAl-Alimi/recall/cmd/recall@latest
```

recall targets macOS with Claude Code 2.1.223 or newer. The list, search,
archive, dashboard and resume paths work on Linux too. Tab focusing and new
tabs use AppleScript and are macOS only.

### Quick start

```sh
recall doctor      # what works on this machine, what is missing
recall setup       # retention, Ctrl-G widget, hooks: each step asks y/N
recall             # the list
```

Once the widget is installed, press **Ctrl-G** in any shell. The list opens,
you pick a session, claude runs, and when it exits you are back in the list.

## How opening works

recall picks one action per session and tells you what will be lost before
it runs.

| Tier | When | What happens |
| --- | --- | --- |
| Focus | Live in a Terminal.app tab | AppleScript brings that tab to the front. Nothing is lost. |
| Attach | Live inside a kept tmux session | `tmux attach`. Scrollback and process survive. |
| Resume | Closed | `cd <cwd> && claude --resume <id>` with the recorded flags. In place by default, or in a new tab or workspace. |

A live session in Cursor or VS Code cannot be driven; recall tells you where
it is. Ghosts (transcript deleted) cannot be opened; their prompts are shown
instead. `bypassPermissions` is never replayed.

The interactive list looks like this:

```text
 recall  5 sessions · 2 live · 1 waiting                              scope: all · ghosts

> ● Needs you  Login page for the dashboard                ascension   main             2m
              Waiting for permission: Edit src/app/login.tsx                         PR#42

  ● Running    Scanner incremental cache                   recall      feat/scan       now
              Running go test ./internal/scan ...                                       wt

  · Closed     Fix flaky calendar popover test             lcc         main             3h
              Fixed the race by cancelling the previous timer.

  · Closed     ETF savings plan rebalance                  sparplan    main             2d
              Here is the drift table for September.                                  fork

  ○ Gone       Kitchen forecast kickoff notes              biergarten                  12d
              summarise the kickoff notes for the kitchen forecast


 0f1e2d3c  /Users/me/dev/ascension
 Enter open · Space preview · / search · n new · f fork · ? keys
```

### Command reference

```sh
recall                          # interactive list
recall ls                       # plain table
recall ls --json | jq '.[0]'    # stable machine-readable rows
recall ls --live                # only sessions with a running claude
recall open 0f1e2d3c            # id prefix, resumes in this terminal
recall open api                 # by label
recall open api --new-tab       # in a new tab of the current terminal app
recall open api --terminal cmux # in a cmux workspace (terminal, iterm or cmux)
recall open api --fork          # branch off instead of continuing
recall open api --keep          # inside a tmux session that survives the tab
recall new -n scanner --keep    # new session here, kept alive
recall new --terminal iterm     # new session in a new iTerm2 tab
recall serve                    # the web dashboard
recall archive --all            # archive every transcript now
recall restore 0f1e2d3c         # bring an archived transcript back
recall doctor --json            # environment checks as JSON
```

`--terminal` takes `terminal` (Terminal.app), `iterm` (iTerm2) or `cmux` and
implies `--new-tab`. Without it, `--new-tab` picks the app you are running
in. Every command accepts `--claude-dir` and `--recall-dir`.
`RECALL_DRY_RUN=1` prints what would run instead of running it.

## What stays, honestly

recall cannot bring back what was never written to disk. This table is the
whole truth about what survives a closed tab, a reboot, or Claude's retention.

| Thing | Where it lives | Who deletes it | With recall |
| --- | --- | --- | --- |
| Transcript (prompts, answers, tool calls) | `~/.claude/projects/<cwd>/<id>.jsonl` | Claude, after `cleanupPeriodDays` (default 30) | Retention raised by `setup`; hard-linked into `~/.recall/archive` by the SessionEnd hook or `recall archive`; resumable from the archive |
| Context window | Rebuilt from the transcript on `--resume` | Same as the transcript | Survives with the transcript; compactions are counted and shown |
| The typed prompts | `~/.claude/history.jsonl` | Never pruned by Claude today | Mirrored to `~/.recall/history.mirror.jsonl`; sessions whose transcript is gone still show as ghosts with their prompts |
| Tool results and file snapshots | `~/.claude/projects/.../tool-results`, `file-history` | Claude, with the transcript | Copied only with `recall archive --sidecars`; not needed to resume |
| A running claude process | Memory, plus `~/.claude/sessions/<pid>.json` | Gone when the tab closes or the Mac reboots | Kept sessions (`K`, `--keep`) run inside tmux on a private socket and survive the tab, not a reboot |
| Terminal scrollback | The terminal | Gone with the tab | Kept sessions hold 50000 lines in tmux; plain sessions rely on the transcript |
| Background shell jobs started by Claude | The claude process | Lost on exit | recall counts them and warns before resuming (`BgJobsLost`) |
| A permission prompt waiting for you | The claude process | Lost on exit | Surfaced as **Needs you** while live; after exit the dangling tool call is shown so you know what was pending |
| Launch flags (`--add-dir`, `--model`, `--mcp-config`, ...) | Nowhere | Gone at exit | Recorded by the SessionStart hook and replayed on resume when the paths still exist |
| Labels, pins, notes, tags | `~/.recall/meta.json` | Only you | Yours |

Things recall never does: it never resumes with `bypassPermissions`, never
sends anything over the network, never reads `*.key` files or tokens, and
never writes into `~/.claude` except the two explicit `setup` steps you
confirm (retention and hooks in `settings.json`, with a `.bak` copy).

## Dashboard

```sh
recall serve
```

This starts a local web view and prints its address:

```text
http://127.0.0.1:4747/?t=<token>
```

Every session is a card: a state pill (Needs you, Running, Closed, Gone),
the title, project, branch, last activity, the last prompt or answer, and
badges for PRs, pins, labels and forks. Each card has two buttons:

- **Open** runs `claude --resume <id>` in a new Terminal.app tab. If the
  session is already running in a tab, that tab is focused instead.
- **Open in cmux** creates a cmux workspace running the resume command. The
  button only appears when the cmux app is installed.

Search, filter chips, pin, label, hide, fork and copy resume command all
work from the page. Cards refresh every few seconds. A banner warns when
retention is still at Claude's 30-day default.

The dashboard is local only:

- It binds to `127.0.0.1` and is never reachable from the network.
- The token in the URL is generated per start and must be sent with every
  request. The page passes it as the `X-Recall-Token` header.
- Cross-origin requests are refused and the page loads no external scripts.

See [docs/dashboard.md](docs/dashboard.md) for the API and the limits.

## Keys

| Key | Action |
| --- | --- |
| `Enter` | open here (focus, attach or resume) |
| `Space` | preview (mark in select mode) |
| `/` | search |
| `Tab` | cycle scope |
| `?` | help |
| `:` | command palette |
| `n` | new session here |
| `N` | new session in row's dir |
| `f` | fork session |
| `K` | open and keep (tmux) |
| `o` | open in new tab |
| `r` | rename label |
| `p` | pin / unpin |
| `H` | hide / unhide |
| `h` | show hidden |
| `t` | tag |
| `a` | archive now |
| `y` | copy resume command |
| `c` | copy claude.ai link |
| `x` | stop (press twice) |
| `D` | remove from list (press twice) |
| `S` | setup |
| `R` | refresh |
| `g` | toggle ghosts |
| `V` | multi-select mode |
| `u` | undo last hide/remove |
| `q / Esc` | quit |

`j` / `k` and the arrow keys move, `PgUp` / `PgDn` page, `Home` / `End` jump.
`Tab` cycles the scopes all, project, live, kept, pinned and ghosts. Search
understands `state:kept`, `branch:main`, `since:7d` and `project:api`. A
label set with `r` is shown instead of the title and works as a reference in
`recall open`. This table is generated from the UI's action table and checked
by `TestReadmeKeyTableMatchesActionTable`, as is the mockup above.

Every state is painted as a plain word next to its dot, so the list reads
without color. `NO_COLOR` is respected.

## How it finds sessions

No daemon, no database, no account. recall reads, and only reads, the files
Claude Code writes:

- `~/.claude/projects/<encoded-cwd>/<uuid>.jsonl`: one transcript per session.
  The head is streamed for the first prompt and metadata; the tail is read
  backwards for the title, last prompt, cost and permission mode. A cache
  keyed by inode, size and mtime makes rescans incremental.
- `~/.claude/history.jsonl`: every prompt you typed. Sessions found here but
  not on disk are shown as ghosts.
- `~/.claude/sessions/<pid>.json` plus `claude agents --json --all`: which
  claude processes are alive right now, and whether they are idle, busy or
  waiting for a permission.
- The tmux socket `recall` for kept sessions.
- The hooks installed by `setup`, which record launch flags on SessionStart
  and archive the transcript on SessionEnd.

What recall writes, and where:

- `~/.recall/` (mode 0700): the scan cache, `meta.json` for labels, pins,
  notes and tags, `launch/<id>.json` for recorded flags, `archive/` for
  hard-linked transcripts, `locks/` for resume locks, and the history mirror.
- `~/.claude/settings.json`: only during `recall setup`, only the
  `cleanupPeriodDays` and `hooks` keys, only after you confirm, with a
  `.bak` copy. `recall uninstall` reverses it.

States, in the order they sort:

| Dot | Word | Meaning |
| --- | --- | --- |
| `●` red | Needs you | Live and waiting for a permission or an answer |
| `●` green | Running | Live: idle, busy, kept in tmux, or a background job |
| `·` | Closed | Transcript on disk, no process; resume with Enter |
| `◔` amber | Expiring | Transcript still on disk and resumable, but within 2 days of `cleanupPeriodDays`; press `a` to archive before Claude deletes it |
| `○` | Gone | Transcript deleted (ghost) or a background session whose daemon died |

## Privacy and security

- Read-only on `~/.claude`. The only writes are the two `setup` steps you
  confirm, and each keeps a `.bak`.
- No network calls, no telemetry, no analytics. The dashboard binds to
  loopback only.
- Never reads `*.key`, `control.key` or tokens.
- Never resumes with `bypassPermissions`. Inline JSON passed to `--settings`
  or `--mcp-config` is never replayed, because it can carry a permission
  mode or secrets.
- `~/.recall` is created with mode 0700; archives are hard links of files you
  already own.
- Test fixtures are synthesized by hand and contain no real transcript data.

## Compatibility

recall depends on four things Claude Code does today: the transcript format,
`history.jsonl`, the liveness registry, and the resume flags. None of them is
a documented API. [docs/compatibility.md](docs/compatibility.md) lists each
contract, how recall notices a change, and what still works when it breaks.
`recall doctor` prints which contracts hold on your machine.

Supported: Claude Code 2.1.223 and newer on macOS. Linux builds pass the
same tests; the focus and new-tab tiers are macOS only.

## Docs

- [docs/design.md](docs/design.md): architecture, data sources, states.
- [docs/dashboard.md](docs/dashboard.md): the web dashboard, its API and
  security model.
- [docs/compatibility.md](docs/compatibility.md): the four Claude Code
  contracts recall depends on.
- [CHANGELOG.md](CHANGELOG.md): what changed in each release.

## Contributing

Issues and pull requests are welcome at
[github.com/MohammedAl-Alimi/recall](https://github.com/MohammedAl-Alimi/recall).
Read [CONTRIBUTING.md](CONTRIBUTING.md) first: it covers fixtures, tests and
the no-real-data rule. `go test ./...` must stay green and every fixture
must be synthesized by hand.

## License

MIT
