// Package scan discovers and parses Claude Code transcripts.
//
// It walks ProjectsDir/*/<uuid>.jsonl, skips *.orphaned-* and
// *.superseded-* files, keeps an incremental cache in RecallDir/cache.json
// keyed by path+inode+size+mtime, and parses transcripts with a streaming
// head read (capped at 8MB) plus a backward tail read (capped at 4MB).
// Ghost sessions (present in history.jsonl but without a transcript) are
// produced by Scanner.Ghosts.
//
// Rules:
//   - The Claude directory is read-only. The only file this package writes
//     is RecallDir/cache.json.
//   - Never read *.key files, control.key or tokens.
//   - Unknown record types are counted, never fatal.
//   - Title precedence: custom-title > agent-name > ai-title > FirstPrompt.
package scan
