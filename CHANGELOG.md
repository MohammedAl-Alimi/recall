# Changelog

All notable changes to recall are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/).

## Unreleased

### Added

- `recall serve`: a local web dashboard. Every session is a card with its
  state, title, project and last activity, and an Open button that resumes
  it in a new terminal tab or focuses the tab it is already running in.
  Loopback only, token protected, no external scripts.
- cmux support. With the cmux app installed, a session can open as its own
  cmux workspace, from the dashboard button or with `--terminal cmux`.
- `--terminal terminal|iterm|cmux` on `recall open` and `recall new`, to
  choose where a session opens instead of following the current terminal.
- `recall service`: the dashboard and the archive as background jobs. On
  macOS `recall service install` writes two launchd user agents,
  `dev.recall.serve` and `dev.recall.archive`, each step asking y/N unless
  `--yes` is passed. `--serve` and `--archive` install one of them,
  `--addr` moves the dashboard off 127.0.0.1:4747 and `--at HH:MM` moves the
  daily run off 09:00. `recall service status` reports installed, loaded and
  the pid; `recall service uninstall` boots both out and removes their
  property lists. Other platforms refuse and print the command to put into a
  systemd user unit instead. `RECALL_DRY_RUN=1` prints the property lists and
  the launchctl calls without writing or loading anything.
- The dashboard as a login service. The serve agent runs
  `recall serve --no-open` at login and is restarted after an unclean exit.
  Because the token is stored in the recall directory, the address is stable:
  `recall service url` prints the full bookmarkable URL on one line, ready to
  paste into a browser or link from another dashboard.
- A daily archive. The archive agent runs `recall archive --all --quiet` once
  a day. It is the layer under retention and the SessionEnd hook: the hook
  only archives a session that ends cleanly, a session left open for weeks
  never ends, and a reset retention setting cannot take back what is already
  archived.
- `recall archive --quiet`: one summary line instead of one line per session,
  and no output at all when nothing changed. Archiving now skips a session
  whose archive already matches the transcript on disk, so `archive --all` is
  cheap to repeat and the daily job stays silent on a quiet day.
- Prebuilt binaries for macOS and Linux on both architectures, a Homebrew
  tap, and download links in the README.

## [0.1.0] - 2026-09-10

First release. Verified on macOS (Darwin 25.4, arm64) against Claude Code
2.1.267 with Go 1.25.1, CGO disabled.

### What works

- `recall` (no arguments): interactive list of every Claude Code session
  across all projects, with title, project, branch, last prompt and answer,
  open PRs, context size and a state word (Needs you, Running, Closed,
  Expiring, Gone). Search, preview, labels, pins, tags, fork and new session
  from the list; help overlay fits the terminal.
- `recall ls`: terminal-width aware table (TIOCGWINSZ, then `$COLUMNS`, then
  120; a pipe gets the full layout), `--json` with a stable field set,
  `--live`, `--project`, `--headless`, `--ghosts`, `--all`.
- `recall open <id>`: focus the Terminal.app tab a live session still runs in,
  attach a kept tmux session, or `claude --resume` in place with the flags the
  session was started with. `--new-tab` opens a Terminal.app or iTerm2 tab,
  `--fork`, `--keep`, `--name`, `--permission-mode`, `--dry-run` and
  `RECALL_DRY_RUN=1` print the command and the generated AppleScript instead
  of running anything. Loss notes (gone cwd, dropped `--add-dir`, lost
  background jobs, pending tool call) are shown before claude starts.
  `bypassPermissions` is never replayed.
- `recall new`: start a session in the current directory (bypass mode
  scrubbed from the recorded argv).
- `recall archive` and `recall restore`: hard-link transcripts under
  `~/.recall/archive` with a SHA-256 manifest; restore verifies the copy
  against the archive file and never deletes a transcript. `--all` and
  `--sidecars` supported.
- `recall setup`: raise `cleanupPeriodDays`, install the SessionStart and
  SessionEnd hooks and the Ctrl-G shell widget, each step skippable and each
  `settings.json` edit backed by a `.bak` copy. `recall uninstall` reverses it.
  `recall shell install|uninstall` writes the widget through symlinked rc
  files instead of replacing them.
- `recall doctor`: claude version and flag support, transcript and registry
  counts, retention, tmux, terminal, recall dir permissions, hooks, widget,
  a parser self-test on a synthesized transcript and a no-network statement.
- `recall exec`: record the exit code of a command for a kept session.
- Streaming transcript parser with an incremental cache under `~/.recall`
  (head and tail windows for large files, unknown record types counted and
  never fatal, zero parse errors on 53 real transcripts). Ghost sessions
  (history only, transcript gone) are rebuilt from `history.jsonl` and its
  mirror.
- SessionStart hook records the exact NUL-separated claude argv per platform
  instead of a whitespace split of `ps` output.
- Session locks: a new-tab resume locks the session inside the tab and the
  lock is released when the widget child exits.
- Stale sessions inside two days of `cleanupPeriodDays` read as Expiring;
  Gone is reserved for ghosts and stale background sessions.
- Never touches `~/.claude` outside the two confirmed `setup` steps, never
  reads `*.key` files or tokens, makes no network connections.

### Release verification (0.1.0)

- `CGO_ENABLED=0 go build ./...`, `go vet ./...`, `gofmt -l .` clean.
- `go test ./... -count=1`: 391 tests (255 top-level, 136 subtests) in 15
  packages, 0 failures, 0 skips. All tests use temporary Claude and recall
  directories.
- Read-only smoke run on a real install with `RECALL_DRY_RUN=1`: doctor,
  `ls`, `ls --json` (52 sessions: 10 live_idle, 1 live_busy, 3 interrupted,
  16 closed, 22 stale; 53 with `--headless`; 59 ghosts), `open` dry-runs for
  a closed and a live session, a fresh empty `RECALL_DIR`. `ls --json` takes
  about 0.48 s cold (no cache) and 0.28 s warm. No em dashes in the output.
  Nothing under `~/.claude` was written by recall during the run.

### Known gaps

- Kept sessions (`K`, `--keep`, `recall exec`) need tmux; the verification
  machine has no tmux, so the attach path is covered by unit tests only.
- Tab focus and `--new-tab` are verified through dry-run AppleScript text,
  not by driving Terminal.app or iTerm2. iTerm2 is untested on a real install.
- Linux builds compile and pass the unit tests, but the focus and new-tab
  tiers are macOS only: focus needs a Terminal.app or iTerm2 tty and
  `--new-tab` always generates AppleScript. On Linux use the in-place resume
  or a kept tmux session.
- `setup`, `uninstall`, hook installation and retention writes were exercised
  only against temporary directories, never the real `~/.claude`.
- Kept sessions survive a closed tab, not a reboot. Background shell jobs,
  pending permission prompts and terminal scrollback of plain sessions are
  not recoverable; recall only reports what was lost.
- Ghosts depend on `~/.claude/history.jsonl`; if Claude Code ever prunes it,
  only the recall mirror remains.
- Headless (`-p`, SDK) sessions are hidden unless `--headless` is passed.
- Retention stays at Claude's 30-day default until `recall setup --retention`
  is run; `doctor` warns about it.
- macOS notifications for Needs you sessions rely on `osascript` and are
  not tested end to end.
- No Homebrew tap is published yet; `Formula/recall.rb` is a template for
  a future tap.
