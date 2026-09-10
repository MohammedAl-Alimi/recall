// Package launch plans and executes how a session is reopened.
//
// Plan is tiered: a live session in Terminal.app is focused via its tty, a
// kept session is attached through tmux, a live session inside Cursor or
// VS Code can only be printed with a hint, a closed session is resumed with
// 'claude --resume <id>', and a ghost cannot be opened.
//
// Rules:
//   - Run honors RECALL_DRY_RUN=1 and then only prints the planned command.
//   - osascript is used only for focusing or opening a terminal tab. Tests
//     assert the generated script text and never execute it.
//   - BuildResume never emits --permission-mode bypassPermissions.
package launch
