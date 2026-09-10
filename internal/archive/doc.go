// Package archive preserves transcripts beyond Claude Code's own cleanup
// window and manages the retention setting.
//
// Archive hard-links a transcript into RecallDir/archive/<sid>/ (falling
// back to a copy) and writes a manifest with sizes and sha256 sums. Restore
// returns a path that can be resumed from. Retention reads
// cleanupPeriodDays from settings.json; SetRetention is the single place in
// recall that modifies that file and it only ever touches that one key.
//
// Rules:
//   - Reads under the Claude directory are fine; the only write is
//     SetRetention (temp+rename, .bak kept, re-read and verified, 3 retries)
//     and it is never run by tests against the real directory.
//   - MirrorHistory only appends to RecallDir/history.mirror.jsonl.
package archive
