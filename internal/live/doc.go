// Package live detects running claude processes and maps them to sessions.
//
// Primary source is 'claude agents --json --all'. Fallback and merge source
// is the registry under ClaudeDir/sessions/<pid>.json. A process counts as
// alive only when it exists, its start time matches the recorded procStart
// (or startedAt within 15s), and either its messaging socket exists or its
// argv starts with claude.
//
// Rules:
//   - ClaudeDir is read-only. After running 'claude agents --json' the prober
//     checks whether a new daemon.lock appeared under ClaudeDir and, if so,
//     marks itself degraded so the caller can report it.
//   - Never read *.key files, control.key or tokens.
//   - Locks live under RecallDir/locks/<sid> and are flock based. The lock FD
//     is left inheritable so an exec'ed claude keeps holding it.
package live
