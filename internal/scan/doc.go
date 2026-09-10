// Package scan discovers and parses Claude Code transcripts.
//
// It walks ProjectsDir/*/<uuid>.jsonl, skips *.orphaned-* and
// *.superseded-* files, keeps an incremental cache in RecallDir/cache.json
// keyed by path+inode+size+mtime, and parses transcripts with a streaming
// head read (capped at 8MB) plus a backward tail read (capped at 4MB).
// Files that fit inside both caps are streamed completely, so every counter
// is exact for them; for larger files the middle is skipped apart from an
// exact line count. Appended bytes are parsed incrementally from the cached
// offset, which only ever advances to the end of a newline-terminated line.
// Ghost sessions (present in history.jsonl but without a transcript) are
// produced by Scanner.Ghosts.
//
// Heuristics worth knowing when reading Session fields:
//   - WorkCwd is the cwd most conversational records were written from.
//   - Headless means entrypoint sdk-cli, or a single promptId with no mode,
//     ai-title or custom-title record.
//   - DanglingTool is the last tool_use that never received a tool_result
//     before the transcript ended.
//   - BgJobsLost counts background Bash jobs started after the last typed
//     prompt.
//   - Interrupted is set by a trailing "[Request interrupted by user" record.
//
// Rules:
//   - The Claude directory is read-only. The only file this package writes
//     is RecallDir/cache.json.
//   - Never read *.key files, control.key or tokens.
//   - Unknown record types are counted, never fatal.
//   - Title precedence: custom-title > agent-name > ai-title > FirstPrompt.
package scan
