// Package doctor runs environment checks and detects which claude CLI
// features are available.
//
// Rules:
//   - Only 'claude --version' and 'claude --help' are executed. No
//     interactive claude session is ever started.
//   - Checks are read-only. Nothing is written anywhere.
package doctor
