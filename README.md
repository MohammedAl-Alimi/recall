# recall

**Every Claude Code session you ever ran, one keypress away.**

recall is a terminal session manager for the Claude Code CLI. It reads the
transcripts Claude already writes to disk, shows every session with a title,
its last outcome and whether it is still running, and reopens the one you pick
in the way that loses the least: focus the tab it is still in, attach the tmux
session that kept it alive, or `claude --resume` it right here.

```
recall   53 sessions · 13 live · 1 waiting                 [all]  Tab scope  / search  ? help

● Needs you  Login page for the dashboard           ascension   main          2m
             waiting for permission: Edit src/app/login.tsx                PR #42
● Running    Scanner incremental cache              recall      feat/scan    now
             Running go test ./internal/scan ...                              wt
○ Closed     Fix flaky calendar popover test        lcc         main          3h
             Fixed the race by cancelling the previous timer.
○ Closed     ETF savings plan rebalance             sparplan    main          2d
             Here is the drift table for September.                         fork
◌ Gone       Kitchen forecast kickoff notes         biergarten                12d
             transcript deleted by retention, prompts kept from history

Enter open   n new here   K keep   f fork   r label   p pin   a archive   q quit
```

## Why

Claude Code keeps a transcript of every session, then deletes it after 30
days. It also has no list: `claude --resume` shows the sessions of the current
directory, and only if you remember which directory that was. Tabs get closed,
laptops get rebooted, and the session where the fix was half done is gone.

recall fixes three things:

1. **Nothing gets deleted behind your back.** `recall setup` raises Claude's
   `cleanupPeriodDays` (ten years by default, or any number you choose) and archives transcripts under
   `~/.recall` so a session can be resumed months later.
2. **One list, every project.** Title, project, branch, last prompt, last
   answer, open PRs, whether it is live, waiting for you, or long gone.
3. **The reopen tier that loses the least.** Still running in a Terminal.app
   tab? recall focuses that tab. Running inside a kept tmux session? recall
   attaches. Closed? recall resumes it, in a new tab or in place, with the
   flags it was started with. Sessions that need your permission bubble to
   the top and can raise a macOS notification.

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

## Install

Homebrew:

```sh
brew tap MohammedAl-Alimi/tap
brew install recall
```

Go 1.25 or newer:

```sh
go install github.com/MohammedAl-Alimi/recall/cmd/recall@latest
```

From source:

```sh
git clone https://github.com/MohammedAl-Alimi/recall
cd recall && make build && ./recall version
```

recall targets macOS with Claude Code 2.1.223 or newer. The list, search,
archive and resume paths work on Linux too; tab focusing and new tabs use
AppleScript and are macOS only.

## Quick start

```sh
recall doctor      # what works on this machine, what is missing
recall setup       # retention, Ctrl-G widget, hooks: each step asks y/N
recall             # the list
```

Or from any shell, once the widget is installed: press **Ctrl-G**. The list
opens, you pick a session, claude runs, and when it exits you are back in the
list.

Common commands:

```sh
recall ls                       # plain table
recall ls --json | jq '.[0]'    # stable machine-readable rows
recall ls --live                # only sessions with a running claude
recall open 0f1e2d3c            # id prefix
recall open api --new-tab       # by label, in a new terminal tab
recall open api --fork          # branch off instead of continuing
recall new -n scanner --keep    # new session that survives the tab
recall archive --all            # archive every transcript now
recall restore 0f1e2d3c         # bring an archived transcript back
```

Every command accepts `--claude-dir` and `--recall-dir`. `RECALL_DRY_RUN=1`
prints what would run instead of running it.

## Keys

| Key | Action |
| --- | --- |
| `Enter` | Open: focus, attach or resume, whichever loses the least |
| `o` / `O` | Open in a new tab / new window |
| `n` / `N` | New session here / in a chosen directory |
| `f` | Fork the selected session |
| `K` | Keep: run inside tmux so it survives the tab |
| `Space` | Preview pane (or mark, in select mode `V`) |
| `/` | Search (supports `state:kept`, `branch:main`, `since:7d`, `project:api`) |
| `Tab` | Cycle scope: all, project, live, kept, pinned, ghosts |
| `r` | Label the session (shown instead of the title, usable in `recall open`) |
| `p` / `H` / `h` | Pin / hide / show hidden |
| `t` | Tag |
| `a` | Archive now |
| `y` / `c` | Copy the resume command / the claude.ai link |
| `x` / `D` | Stop / delete (press twice) |
| `u` | Undo the last change |
| `g` | Toggle ghosts |
| `S` / `R` | Setup / refresh |
| `?` / `:` | Help / command palette |
| `q`, `Esc` | Quit |

Every state is painted as a plain word next to its dot, so the list reads
without color. `NO_COLOR` is respected.

## How it finds sessions

recall reads, and only reads, the files Claude Code writes:

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

States, in the order they sort:

| Dot | Word | Meaning |
| --- | --- | --- |
| `●` amber | Needs you | Live and waiting for a permission or an answer |
| `●` green | Running | Live: idle, busy, kept in tmux, or a background job |
| `○` | Closed | Transcript on disk, no process; resume with Enter |
| `◌` | Gone | Transcript deleted (ghost) or past retention with no archive |

## Privacy

- Read-only on `~/.claude`. The only writes are the two `setup` steps you
  confirm, and each keeps a `.bak`.
- No network calls, no telemetry, no analytics.
- Never reads `*.key`, `control.key` or tokens.
- `~/.recall` is created with mode 0700; archives are hard links of files you
  already own.
- Test fixtures are synthesized by hand and contain no real transcript data.

## Docs

- [docs/design.md](docs/design.md): architecture, data sources, states.
- [docs/compatibility.md](docs/compatibility.md): the four Claude Code
  contracts recall depends on and what happens when each one changes.
- [CONTRIBUTING.md](CONTRIBUTING.md): fixtures, tests, the no-real-data rule.

## License

MIT
