# recall design

recall is a read-mostly index over the files Claude Code already writes, plus
a small launcher that picks the least lossy way to get back into a session.
This document is the short version of how it is put together.

## Architecture

```
                 ~/.claude (read-only)                      ~/.recall (owned)
   +---------------------------------------------+     +----------------------+
   | projects/<cwd>/<id>.jsonl   transcripts     |     | cache.json           |
   | history.jsonl               typed prompts   |     | meta.json  labels..  |
   | sessions/<pid>.json         live registry   |     | launch/<id>.json     |
   | settings.json               retention,hooks |     | archive/<id>/...     |
   | daemon/                     bg daemons      |     | events.jsonl         |
   +----------------------+----------------------+     | locks/<id>  (flock)  |
                          |                            +----------+-----------+
                          v                                       ^
   +------------+   +-----------+   +-----------+   +---------+   |
   |   scan     |   |   live    |   |   mux     |   |  state  |---+
   | transcripts|   | processes |   |  tmux     |   | meta,   |
   | + ghosts   |   | + regisry |   |  socket   |   | launch  |
   +-----+------+   +-----+-----+   +-----+-----+   +----+----+
         |                |               |              |
         +--------+-------+-------+-------+------+-------+
                  v                       v
             +---------+            +-----------+
             |  index  | Derive     |    app    |  Load / RefreshLive / Open
             | Filter  | Sort       | (glue)    |
             +----+----+            +-----+-----+
                  |                       |
        +---------+---------+   +---------+---------+
        v                   v   v                   v
   +---------+         +---------+            +-----------+
   |   ui    |  TUI    | cmd/    |  cobra     |  launch   | focus / attach /
   | bubble  |         | recall  |            |  + notify | resume / new
   +---------+         +---------+            +-----------+
                                                     |
                                   +-----------------+---------------+
                                   v                 v               v
                              osascript        tmux attach     claude --resume
                            (focus, new tab)  (kept session)   (exec in place)
```

Hooks (`recall hook <event>`) and the shell widget (`Ctrl-G`) are the two
entry points that are not started by the user directly. Both call back into
the same packages: `hook` records launches and archives on exit, the widget
runs `recall --from-widget`, which loops back into the list when claude exits.

## Data sources

| Source | Read by | Gives |
| --- | --- | --- |
| `projects/*/<uuid>.jsonl` | `scan.ParseTranscript` | title (custom-title > agent-name > ai-title > first prompt), prompts, cwd list, branch, versions, turns, compactions, permission mode, cost, context tokens, PR and artifact links, dangling tool call, files changed, headless and lineage heuristics |
| `history.jsonl` | `scan.Ghosts` | sessions whose transcript is gone (ghosts) with their prompts |
| `claude agents --json --all` | `live.Prober` | pid, cwd, kind, status, name per live session |
| `sessions/<pid>.json` | `live.Prober` | registry fallback and merge: procStart, socket path, bridge id |
| `ps` | `live` | liveness proof (pid exists, start time matches), host app and tty from the ppid chain |
| tmux socket `recall` | `mux.Tmux.List` | kept sessions, attached or not |
| `settings.json` | `archive.Retention` | `cleanupPeriodDays` |
| `~/.recall/meta.json` | `state` | labels, pins, hidden, notes, tags, trash |
| `~/.recall/launch/<id>.json` | `state` | argv and cwd recorded by the SessionStart hook |

Scanning is incremental: `cache.json` stores parsed sessions keyed by path,
inode, size and mtime. A grown transcript is re-read from its last offset; an
untouched one is not opened at all. A transcript up to 12 MB is streamed in
full. A larger one is parsed from its head (streamed until the id, first cwd
and first prompt are known, at most 8 MB) plus its last 4 MB; the middle is
only line-counted, so `Lines` stays exact while turns, compactions, files
changed and the cwd counts behind `WorkCwd` cover the head and the tail
only. That is the price of a 300 MB transcript costing about the same as a
small one.

## States

`index.Derive` folds transcript, live and tmux facts into one state per
session. The plain word is always painted next to the dot.

| State | Word | Derived from |
| --- | --- | --- |
| `needs_you` | Needs you | live and status `waiting`, or a permission_prompt notification event |
| `live_idle`, `live_busy` | Running | live process, status idle or busy |
| `kept` | Running | live inside the recall tmux socket |
| `bg` | Running | `claude --bg` session whose daemon is alive |
| `foreign` | Running | live process recall did not start and cannot focus (Cursor, VS Code) |
| `bg_stale` | Gone | bg session whose daemon is dead |
| `closed` | Closed | transcript on disk, no process |
| `interrupted` | Closed | closed after an Esc interruption: a trailing `[Request interrupted by user` record. A tool_use with no tool_result at the end does not change the state; it is kept as `DanglingTool` and shown in the preview and in the loss note before a resume |
| `headless` | Closed | `-p` or SDK session, hidden by default |
| `archived` | Closed | transcript gone from projects, copy in `~/.recall/archive` |
| `stale` | Expiring | transcript still on disk, last active within 2 days of `cleanupPeriodDays`, not archived; the row hints `a archives before deletion` |
| `ghost` | Gone | only in history.jsonl |

Sort order: needs_you first, then pinned, then last active descending.

## Open tiers

`launch.Plan` picks one action per session and returns it with a note about
what will be lost, so the UI can show it before doing anything.

1. **focus**: live in a Terminal.app tab with a known tty: AppleScript selects
   that tab. Nothing is lost.
2. **attach**: live inside the recall tmux socket: `tmux attach`. Scrollback
   and process survive.
3. **print**: live in Cursor, VS Code or another host recall cannot drive: the
   hint tells you where it is.
4. **resume**: closed: `cd <work cwd> && claude --resume <id>` with recorded
   flags replayed when their paths still exist. In place by default (Enter
   in the list, `recall open`); a new Terminal.app or iTerm2 tab with `o` or
   `--new-tab`. Inline JSON given to `--settings` or `--mcp-config` is never
   replayed because it can carry a permission mode or secrets. The loss note
   (gone directory, dropped flags, lost background jobs, dangling tool call)
   is shown in the status bar before the action runs and printed as
   `recall: <note>` to stderr right before claude starts. `--fork` adds
   `--fork-session`. `bypassPermissions` is never replayed.
5. **error**: ghosts cannot be opened; their prompts are shown instead.

Before a resume recall takes a non-blocking `flock` on
`~/.recall/locks/<id>` and leaves the descriptor inheritable, so a second
`recall open` of the same session sees the lock held by the running claude.

## Spikes

Things that were verified against Claude Code 2.1.x before the design was
frozen, in the order they were tried:

1. Transcript records carry `sessionId`, `cwd`, `gitBranch`, `entrypoint` and
   `promptId` on every envelope; metadata records (`ai-title`,
   `custom-title`, `permission-mode`, `cost-state`, `pr-link`) have no
   envelope and can appear anywhere in the file.
2. `claude agents --json --all` lists every live process with a stable
   `sessionId`; it can spawn a daemon lock file, which recall detects and
   reports as degraded liveness.
3. `sessions/<pid>.json` survives a crash, so pid existence alone is not
   proof of life: `ps -o lstart=` must match `procStart`.
4. `claude --resume <id>` works from any directory when the transcript exists
   under the encoded project dir of that session's cwd, so archives can be
   restored by copying back.
5. `cleanupPeriodDays` is honoured at claude startup, not by a daemon, so
   raising it before the next start keeps everything that still exists.
6. tmux 3.2 on a private socket with `remain-on-exit on` keeps the pane after
   claude exits, so a crashed kept session can still be read.
7. AppleScript can select a Terminal.app tab by `tty of tabs`; iTerm needs
   its own dictionary and is a follow-up.
8. Hooks receive `session_id`, `transcript_path` and `cwd` on stdin and must
   print nothing on stdout; `recall hook` always exits 0.
