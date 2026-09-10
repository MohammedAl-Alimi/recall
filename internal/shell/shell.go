package shell

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// BeginMarker and EndMarker delimit the recall block in an rc file.
const (
	BeginMarker = "# >>> recall >>>"
	EndMarker   = "# <<< recall <<<"
)

// Supported shells.
var Supported = []string{"zsh", "bash", "fish"}

const zshWidget = `# recall: Ctrl-G opens the Claude Code session picker (managed by 'recall shell')
_recall_widget() {
  zle -I
  recall --from-widget </dev/tty >/dev/tty
  zle reset-prompt
}
zle -N _recall_widget
bindkey '^G' _recall_widget
if ! whence rcl >/dev/null 2>&1; then
  alias rcl='recall'
fi`

const bashWidget = `# recall: Ctrl-G opens the Claude Code session picker (managed by 'recall shell')
if [ -n "$BASH_VERSION" ] && [[ $- == *i* ]]; then
  bind -x '"\C-g": "recall --from-widget </dev/tty >/dev/tty"'
  if ! type rcl >/dev/null 2>&1; then
    alias rcl='recall'
  fi
fi`

const fishWidget = `# recall: Ctrl-G opens the Claude Code session picker (managed by 'recall shell')
function _recall_widget
    recall --from-widget </dev/tty >/dev/tty
    commandline -f repaint
end
bind \cg _recall_widget
if not type -q rcl
    alias rcl='recall'
end`

// Widget returns the rc snippet for shell ("zsh", "bash" or "fish"),
// including the begin and end markers. Unknown shells yield "".
func Widget(shell string) string {
	var body string
	switch normalize(shell) {
	case "zsh":
		body = zshWidget
	case "bash":
		body = bashWidget
	case "fish":
		body = fishWidget
	default:
		return ""
	}
	return BeginMarker + "\n" + body + "\n" + EndMarker + "\n"
}

// Install writes the widget into rcPath. An existing recall block is
// replaced in place; otherwise the block is appended. The rc file is
// created when missing.
func Install(shell, rcPath string) error {
	snippet := Widget(shell)
	if snippet == "" {
		return fmt.Errorf("shell: unsupported shell %q (supported: %s)", shell, strings.Join(Supported, ", "))
	}
	if rcPath == "" {
		return errors.New("shell: empty rc path")
	}
	content, mode, err := readRC(rcPath)
	if err != nil {
		return err
	}
	var out string
	if start, end, ok := findBlock(content); ok {
		out = content[:start] + snippet + content[end:]
	} else {
		out = content
		if out != "" && !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		if out != "" {
			out += "\n"
		}
		out += snippet
	}
	if out == content {
		return nil
	}
	return writeRC(rcPath, out, mode)
}

// Uninstall removes the widget block from rcPath. A missing file or a file
// without the block is left untouched.
func Uninstall(rcPath string) error {
	if rcPath == "" {
		return errors.New("shell: empty rc path")
	}
	content, mode, err := readRC(rcPath)
	if err != nil {
		return err
	}
	start, end, ok := findBlock(content)
	if !ok {
		return nil
	}
	// Drop one blank line that Install added before the block.
	if start > 0 && strings.HasSuffix(content[:start], "\n\n") {
		start--
	}
	out := content[:start] + content[end:]
	return writeRC(rcPath, out, mode)
}

// Installed reports whether rcPath contains a recall block.
func Installed(rcPath string) bool {
	content, _, err := readRC(rcPath)
	if err != nil {
		return false
	}
	_, _, ok := findBlock(content)
	return ok
}

// Detect returns the user's shell and its rc file from $SHELL. Unknown
// shells return empty strings.
func Detect() (shell string, rcPath string) {
	shell = normalize(os.Getenv("SHELL"))
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "."
	}
	switch shell {
	case "zsh":
		dir := os.Getenv("ZDOTDIR")
		if dir == "" {
			dir = home
		}
		return shell, filepath.Join(dir, ".zshrc")
	case "bash":
		rc := filepath.Join(home, ".bashrc")
		profile := filepath.Join(home, ".bash_profile")
		if _, err := os.Stat(rc); err != nil {
			if _, err := os.Stat(profile); err == nil {
				return shell, profile
			}
		}
		return shell, rc
	case "fish":
		cfg := os.Getenv("XDG_CONFIG_HOME")
		if cfg == "" {
			cfg = filepath.Join(home, ".config")
		}
		return shell, filepath.Join(cfg, "fish", "config.fish")
	}
	return "", ""
}

func normalize(shell string) string {
	s := strings.ToLower(filepath.Base(strings.TrimSpace(shell)))
	s = strings.TrimPrefix(s, "-")
	switch s {
	case "zsh", "bash", "fish":
		return s
	}
	return s
}

// findBlock returns the byte range [start, end) of the marker block, where
// end sits after the trailing newline of the end marker.
func findBlock(content string) (int, int, bool) {
	start := strings.Index(content, BeginMarker)
	if start < 0 {
		return 0, 0, false
	}
	// Markers are only honoured at the start of a line.
	if start > 0 && content[start-1] != '\n' {
		return 0, 0, false
	}
	rel := strings.Index(content[start:], EndMarker)
	if rel < 0 {
		return 0, 0, false
	}
	end := start + rel + len(EndMarker)
	if end < len(content) && content[end] == '\n' {
		end++
	}
	return start, end, true
}

func readRC(path string) (string, os.FileMode, error) {
	st, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", 0o644, nil
		}
		return "", 0, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	return string(data), st.Mode().Perm(), nil
}

func writeRC(path, content string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	fail := func(e error) error {
		tmp.Close()
		_ = os.Remove(name)
		return e
	}
	if _, err := tmp.WriteString(content); err != nil {
		return fail(err)
	}
	if err := tmp.Chmod(mode); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}
