// Package mux wraps the tmux integration used for kept sessions.
//
// recall runs tmux on its own socket (default "recall") so it never touches
// the user's own tmux server. Kept sessions are named "rc-" + ShortID.
//
// Rules:
//   - When tmux is missing every function degrades gracefully: Available
//     reports false, List returns an empty slice.
//   - Tests must never create tmux sessions on the host; they assert the
//     generated argv only.
package mux
