// Package state persists recall's own metadata under RecallDir: pins,
// hidden sessions, labels, notes, tags, the trash list, launch records,
// the events log and the last-boot snapshot.
//
// Rules:
//   - Everything here lives under RecallDir. Nothing is written into the
//     Claude directory.
//   - Meta is written with temp+rename under an flock on RecallDir/state.lock
//     and mode 0600.
//   - events.jsonl is opened with O_APPEND so concurrent hooks never
//     interleave.
package state
