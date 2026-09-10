// Package notify sends desktop notifications through osascript.
//
// Rules:
//   - Send honors RECALL_DRY_RUN=1 and then only prints the notification.
//   - Failures are non-fatal for callers; the UI never blocks on a
//     notification.
package notify
