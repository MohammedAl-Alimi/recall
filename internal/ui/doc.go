// Package ui is the Bubble Tea terminal interface of recall.
//
// Screens: main list, preview pane, search, scope cycling, help, command
// palette, dialogs and multi-select. Live info refreshes every 2s and the
// transcript scan every 10s.
//
// Rules:
//   - Every state is painted as a plain-text word next to its color dot.
//   - NO_COLOR is respected.
//   - Destructive keys (x stop, D delete) require selecting twice.
//   - The UI never writes into the Claude directory.
package ui
