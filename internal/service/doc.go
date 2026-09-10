// Package service installs recall as a background service managed by the
// operating system: the dashboard as a login item that is always reachable
// at a stable URL, and a daily job that archives every session that is not
// archived yet.
//
// macOS is the only platform with a real implementation. A launchd user
// agent is written into ~/Library/LaunchAgents and loaded with launchctl.
// Supported reports what the current platform can do, and Install refuses
// with a clear message everywhere else instead of pretending to have
// worked.
//
// The seam for other platforms is Config plus the three verbs Install,
// Uninstall and Status. Everything that is platform specific lives behind
// them: Plist builds the launchd XML, and the launchctl calls are the only
// place that shells out.
//
// Safety rule: when the environment variable RECALL_SERVICE_NO_LAUNCHCTL is
// set to 1, nothing is ever loaded, booted out or queried. Install still
// writes the plist file and Uninstall still removes it, so the file layer
// can be exercised in full, but no launchctl process is started and no
// background job appears on the machine running the tests. Every test in
// this repository sets that variable.
package service
