package shell

import "errors"

// BeginMarker and EndMarker delimit the recall block in an rc file.
const (
	BeginMarker = "# >>> recall >>>"
	EndMarker   = "# <<< recall <<<"
)

// Widget returns the rc snippet for shell ("zsh", "bash" or "fish").
func Widget(shell string) string {
	return ""
}

// Install writes the widget into rcPath.
func Install(shell, rcPath string) error {
	return errors.New("not implemented: shell.Install")
}

// Uninstall removes the widget block from rcPath.
func Uninstall(rcPath string) error {
	return errors.New("not implemented: shell.Uninstall")
}

// Detect returns the user's shell and its rc file.
func Detect() (shell string, rcPath string) {
	return "", ""
}
