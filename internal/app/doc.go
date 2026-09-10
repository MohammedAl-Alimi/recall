// Package app wires scanner, prober, tmux, metadata and state derivation
// into one App used by both the TUI and the CLI commands.
//
// Rules:
//   - Load and RefreshLive only read from the Claude directory.
//   - Open takes the session lock before resuming and records the launch.
package app
