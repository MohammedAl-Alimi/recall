// Package hook implements the 'recall hook <event>' entry point that Claude
// Code calls through its hooks configuration, and the settings.json
// install/uninstall used by setup.
//
// Rules:
//   - Handle always exits 0 and prints nothing on stdout so a bug in recall
//     can never break a Claude session.
//   - Handle only appends to events.jsonl, saves launch records and archives
//     transcripts by hard link. It never writes into the Claude directory.
//   - SessionStart records the claude process argv exactly as it was exec'd
//     (kern.procargs2 on macOS, /proc/<pid>/cmdline on Linux), never from
//     whitespace split ps output, so a prompt containing flag-like text is
//     one token and cannot be replayed as flags on resume.
//   - InstallSettings and UninstallSettings modify settings.json additively
//     with a .bak copy. They are never run in tests against a real directory.
package hook
