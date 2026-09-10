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
      sessions/<pid>.json                    synthesized live registry entries
      settings.json                          minimal settings

Tests must always copy fixtures into `t.TempDir()` before use and never read
or write the real `~/.claude`.
