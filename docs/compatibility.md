# Compatibility

recall depends on four things Claude Code does today. None of them is a
documented API. This page lists each contract, how recall uses it, how it
notices a change, and what still works when it breaks.

Supported: Claude Code 2.1.223 and newer on macOS. `recall doctor` prints
which contracts hold on the current machine.

## 1. Transcript files

**Contract.** Each session is one JSONL file at
`~/.claude/projects/<encoded cwd>/<uuid>.jsonl` where the encoded cwd
replaces every byte outside `[A-Za-z0-9]` with `-`. Conversational records
have `type` (`user`, `assistant`, `attachment`, `system`), `uuid`,
`parentUuid`, `timestamp`, `cwd`, `sessionId`, `version`, `gitBranch` and
optional `entrypoint`, `promptId`, `isMeta`, `isCompactSummary`,
`sessionKind`. Metadata records without an envelope carry `ai-title`,
`custom-title`, `agent-name`, `last-prompt`, `permission-mode`,
`cost-state`, `pr-link`, `frame-link`, `bridge-session`, `queue-operation`,
`file-history-snapshot` and the legacy `summary`. `system` records with
subtype `compact_boundary` mark a compaction. Files named
`*.orphaned-*` and `*.superseded-*` are leftovers and are skipped.

**Used for.** Everything in the list: title, prompts, cwd, branch, turns,
tokens, cost, PR links, interrupted and headless heuristics.

**Detection.** Unknown record types are counted, never fatal; the count is
visible in `ls --json` as `parse_errors`. A transcript with no recognisable
records still yields a session with its id, path and mtime.

**Fallback.** Title falls back from custom-title to agent-name to ai-title
to the first prompt to the file name. Missing envelopes degrade one field at
a time. If the projects directory moves, `RECALL_PROJECTS_DIR` or
`--claude-dir` points recall at it.

## 2. history.jsonl

**Contract.** One JSON object per line with `display` (the typed prompt),
`pastedContents`, `timestamp` (epoch milliseconds), `project` (absolute cwd)
and `sessionId`.

**Used for.** Ghost sessions (prompts without a transcript) and the cwd of
a session whose transcript lost its envelope. Mirrored by offset into
`~/.recall/history.mirror.jsonl` so recall keeps its own copy.

**Detection.** Lines that fail to parse are skipped. Sessions whose prompts
are all slash commands are dropped.

**Fallback.** Without history there are no ghosts; everything else works.

## 3. Liveness: `claude agents --json` and the sessions registry

**Contract.** `claude agents --json --all` prints a JSON array of
`{pid, cwd, kind, startedAt, sessionId, name, status}`. Separately,
`~/.claude/sessions/<pid>.json` holds `pid`, `sessionId`, `cwd`, `startedAt`
(ms), `procStart` (ctime string, UTC), `version`, `kind`, `entrypoint`,
`messagingSocketPath`, `name`, `nameSource`, `status` (`idle`, `busy`,
`waiting`), `updatedAt`, `bridgeSessionId`.

**Used for.** The Running and Needs you states, the pid to focus, the
socket path that proves the process is claude.

**Detection.** A process counts as alive only when the pid exists, `ps -o
lstart=` matches `procStart` (or `startedAt` is within 15 s of it) and the
socket path exists or argv starts with `claude`. If `claude agents` fails,
recall marks liveness degraded and shows a banner. If a new
`~/.claude/daemon.lock` appears right after the call, that is recorded too.

**Fallback.** Registry files alone. If those are missing as well, every
session is shown as Closed and Enter resumes it; resuming a session that is
secretly still running is caught by the flock on `~/.recall/locks/<id>`.

## 4. Resume, fork, session id and name flags

**Contract.** `claude --resume <uuid>`, `--fork-session`,
`--session-id <uuid>`, `-n <name>`, `--permission-mode <mode>`,
`--add-dir`, `--mcp-config`, `--settings`, `--plugin-dir`, `--model`,
`--effort`, `--agent`. `claude --bg` and `claude attach` for background
sessions.

**Used for.** Every reopen. Recorded launch flags are replayed only when
their path arguments still exist.

**Detection.** `doctor.DetectFeatures` parses `claude --version` and
`claude --help` once and reports which flags are present. The UI disables
what is missing (real rename needs `-n`; fork needs `--fork-session`).

**Fallback.** Plain `claude --resume <id>` is the floor. If even that is
missing, recall prints the transcript path so you can copy it.

## Things recall relies on outside Claude

| Dependency | Needed for | Without it |
| --- | --- | --- |
| tmux >= 3.2 | kept sessions (`K`, `--keep`) | `K` is disabled, everything else works |
| osascript, Terminal.app or iTerm | focus a tab, open in a new tab | resume runs in place |
| `ps` with `-o lstart=` | liveness proof | all sessions shown as Closed |
| a POSIX shell (zsh, bash, fish) | Ctrl-G widget | `recall` still works as a command |

## Settings recall writes

Only `recall setup` and `recall uninstall` write into `~/.claude`, each step
after a y/N prompt (or `--yes`):

- `settings.json` key `cleanupPeriodDays`: read, merge, write to a temp file,
  rename, re-read to verify, three retries, `.bak` kept. No other key is
  touched.
- `settings.json` key `hooks`: recall entries for `SessionStart`,
  `SessionEnd` and `Notification` are merged additively and removed by
  `uninstall`; other hooks are left alone.

Everything else recall stores lives under `~/.recall` (mode 0700).
