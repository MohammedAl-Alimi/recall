// Package model holds the shared data types of recall: sessions, live
// process info, launch records, and the Paths that locate the Claude Code
// config directory and the recall state directory.
//
// Rules:
//   - This package contains types only, plus tiny helpers (DefaultPaths,
//     PathsFrom, ShortID, State.Word). No I/O, no parsing.
//   - Nothing in recall ever writes into the Claude config directory
//     (~/.claude) except the explicit setup/uninstall commands, which live in
//     other packages. Paths.ClaudeDir is treated as read-only input.
//   - Secrets under the Claude directory (*.key, control.key, tokens) are
//     never read or printed by any package.
package model
