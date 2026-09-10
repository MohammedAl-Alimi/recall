# Fixtures

Every file under this directory is synthesized by hand to match the observed
Claude Code 2.1.x transcript, history and registry schemas. Nothing here is
copied from a real transcript, and no fixture may contain real prompts,
tokens, keys, paths from a real machine, or any other secret.

Layout mirrors a Claude config directory so tests can point
`--claude-dir` at `testdata/fixtures/claude`:

    claude/
      projects/<encoded-cwd>/<uuid>.jsonl   synthesized transcripts
      history.jsonl                          synthesized prompt history
      sessions/<pid>.json                    synthesized live registry entry
      settings.json                          minimal settings

Tests must always copy fixtures into `t.TempDir()` before use and never read
or write the real `~/.claude`.

## Transcripts (projects/-Users-alice-dev-app)

| File prefix | Scenario |
|-------------|----------|
| `11111111` | Normal session: ai-title, two CLI versions, branch change, compaction, Edit/Write tool uses, pr-link, frame-link, bridge-session, remote_session_change attachment, cost-state, one unknown record type and one non-JSON line |
| `22222222` | custom-title present together with agent-name and ai-title (custom wins), plan permission mode, claude-desktop entrypoint |
| `33333333` | agent-name beats ai-title; work moves into a `.claude/worktrees` checkout |
| `44444444` | First typed prompt only appears after a 200 KB tool_result line |
| `55555555` | Resumed session whose first conversational record is a compact summary (lineage continuation) |
| `66666666` | Closed mid tool call: trailing tool_use without tool_result, plus a background Bash job |
| `77777777` | Headless `claude -p` run: single promptId, no mode or ai-title records |
| `88888888` | Envelope-less transcript: metadata records only |
| `99999999` | 2.1.168 style records: no entrypoint or promptId, legacy summary record, trailing interrupt |
| `aaaaaaaa.jsonl.orphaned-*`, `bbbbbbbb.superseded-*.jsonl` | Must be skipped by the scanner |
| `subagents/agent-*.jsonl`, `notes.txt` | Nested or non-transcript files that must be ignored |

## history.jsonl

Two entries for sessions that have transcripts, one ghost with three prompts
(one slash command), one slash-only ghost that must be dropped, one ghost in
another project, one entry without a session id and one malformed line.
