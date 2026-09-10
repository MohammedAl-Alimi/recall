package mux

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/MohammedAl-Alimi/recall/internal/model"
)

// DefaultSocket is the tmux -L socket name recall uses.
const DefaultSocket = "recall"

// minVersion is the oldest tmux release recall accepts (new-session -e and
// modern format strings).
const (
	minMajor = 3
	minMinor = 2
)

// Hooks that tests replace so no tmux binary is ever needed or executed.
var (
	// lookPath resolves the tmux binary. Production uses exec.LookPath.
	lookPath = exec.LookPath

	// runTmux executes tmux with the given argv (argv[0] is "tmux") and
	// returns its stdout. Production runs the real binary.
	runTmux = realRunTmux
)

func realRunTmux(argv []string) (string, error) {
	if len(argv) == 0 {
		return "", errors.New("empty argv")
	}
	bin, err := lookPath(argv[0])
	if err != nil {
		return "", err
	}
	cmd := exec.Command(bin, argv[1:]...)
	cmd.Env = os.Environ()
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return out.String(), fmt.Errorf("%s: %s", strings.Join(argv, " "), msg)
	}
	return out.String(), nil
}

// Tmux drives tmux on a private socket.
type Tmux struct {
	// Socket is the tmux -L socket name. Default "recall".
	Socket string

	// Conf is an optional tmux.conf path passed with -f so the user's own
	// ~/.tmux.conf is never loaded on the recall socket. Empty means tmux
	// reads its defaults.
	Conf string
}

// NewTmux returns a Tmux using the default socket.
func NewTmux() *Tmux {
	return &Tmux{Socket: DefaultSocket}
}

func (t *Tmux) socket() string {
	if t == nil || t.Socket == "" {
		return DefaultSocket
	}
	return t.Socket
}

// base returns the leading argv shared by every tmux invocation:
// tmux -L <socket> [-f <conf>].
func (t *Tmux) base() []string {
	argv := []string{"tmux", "-L", t.socket()}
	if t != nil && t.Conf != "" {
		argv = append(argv, "-f", t.Conf)
	}
	return argv
}

var versionRe = regexp.MustCompile(`(\d+)\.(\d+)`)

// parseVersion extracts major and minor from 'tmux -V' output such as
// "tmux 3.4", "tmux 3.3a" or "tmux next-3.5".
func parseVersion(out string) (major, minor int, ok bool) {
	m := versionRe.FindStringSubmatch(out)
	if m == nil {
		return 0, 0, false
	}
	major, _ = strconv.Atoi(m[1])
	minor, _ = strconv.Atoi(m[2])
	return major, minor, true
}

// versionOK reports whether major.minor is at least the minimum.
func versionOK(major, minor int) bool {
	if major != minMajor {
		return major > minMajor
	}
	return minor >= minMinor
}

// Available reports whether a tmux binary >= 3.2 is on PATH and its version.
// The version string is returned even when it is too old so the caller can
// explain why kept sessions are unavailable.
func (t *Tmux) Available() (bool, string) {
	if _, err := lookPath("tmux"); err != nil {
		return false, ""
	}
	out, err := runTmux([]string{"tmux", "-V"})
	if err != nil {
		return false, ""
	}
	version := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(out), "tmux "))
	major, minor, ok := parseVersion(version)
	if !ok {
		return false, version
	}
	return versionOK(major, minor), version
}

// SessionName returns the tmux session name for a session id.
func (t *Tmux) SessionName(sid string) string {
	return "rc-" + model.ShortID(sid)
}

// ShellQuote quotes s for a POSIX shell using single quotes.
func ShellQuote(s string) string {
	if s == "" {
		return "''"
	}
	safe := true
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.', r == '/', r == ':', r == '@', r == '%', r == '+', r == '=', r == ',':
		default:
			safe = false
		}
		if !safe {
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ClaudeShellCommand returns the shell command that runs claude inside the
// kept session: env RECALL_SID=<sid> RECALL_BYPASS=1 claude --session-id
// <sid> [-n <name>] <claudeArgs...>. RECALL_BYPASS tells the recall shell
// widget and hooks that this claude was started by recall itself.
func ClaudeShellCommand(sid, name string, claudeArgs []string) string {
	parts := []string{"env", "RECALL_SID=" + ShellQuote(sid), "RECALL_BYPASS=1", "claude", "--session-id", ShellQuote(sid)}
	if name != "" {
		parts = append(parts, "-n", ShellQuote(name))
	}
	for _, a := range claudeArgs {
		parts = append(parts, ShellQuote(a))
	}
	return strings.Join(parts, " ")
}

// NewKeptCommand returns the argv that creates a detached tmux session
// running claude with RECALL_SID and RECALL_BYPASS set.
func (t *Tmux) NewKeptCommand(sid, name, cwd string, claudeArgs []string) []string {
	argv := append(t.base(), "new-session", "-d", "-s", t.SessionName(sid))
	if cwd != "" {
		argv = append(argv, "-c", cwd)
	}
	argv = append(argv, "-x", "200", "-y", "50")
	argv = append(argv, ClaudeShellCommand(sid, name, claudeArgs))
	return argv
}

// AttachCommand returns the argv that attaches to the kept session for sid.
func (t *Tmux) AttachCommand(sid string) []string {
	return append(t.base(), "attach-session", "-t", "="+t.SessionName(sid))
}

// listFormat is the list-panes format: session name, attached flag, pane pid.
const listFormat = "#{session_name}\t#{session_attached}\t#{pane_pid}"

// List returns every pane on the recall socket. It returns an empty slice
// when tmux is missing or no server is running on the socket.
func (t *Tmux) List() ([]model.MuxInfo, error) {
	if _, err := lookPath("tmux"); err != nil {
		return []model.MuxInfo{}, nil
	}
	out, err := runTmux(append(t.base(), "list-panes", "-a", "-F", listFormat))
	if err != nil {
		// No server on the socket is the normal idle state, not an error.
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "no server running") || strings.Contains(msg, "no such file") || strings.Contains(msg, "error connecting") {
			return []model.MuxInfo{}, nil
		}
		return []model.MuxInfo{}, err
	}
	return parseList(t.socket(), out), nil
}

// parseList decodes list-panes output produced with listFormat.
func parseList(socket, out string) []model.MuxInfo {
	list := []model.MuxInfo{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 3 {
			continue
		}
		attached, _ := strconv.Atoi(strings.TrimSpace(fields[1]))
		pid, _ := strconv.Atoi(strings.TrimSpace(fields[2]))
		list = append(list, model.MuxInfo{
			Socket:      socket,
			SessionName: fields[0],
			Attached:    attached > 0,
			PanePID:     pid,
		})
	}
	return list
}

// Find returns the MuxInfo of the kept session for sid, if any.
func (t *Tmux) Find(sid string) (*model.MuxInfo, error) {
	list, err := t.List()
	if err != nil {
		return nil, err
	}
	name := t.SessionName(sid)
	for i := range list {
		if list[i].SessionName == name {
			return &list[i], nil
		}
	}
	return nil, nil
}

// KillCommand returns the argv that kills the kept session for sid.
func (t *Tmux) KillCommand(sid string) []string {
	return append(t.base(), "kill-session", "-t", "="+t.SessionName(sid))
}

// Kill terminates the kept session for sid. Killing a session that does not
// exist is not an error.
func (t *Tmux) Kill(sid string) error {
	if _, err := lookPath("tmux"); err != nil {
		return fmt.Errorf("tmux not available: %w", err)
	}
	_, err := runTmux(t.KillCommand(sid))
	if err != nil {
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "can't find session") || strings.Contains(msg, "no server running") || strings.Contains(msg, "no such file") {
			return nil
		}
		return err
	}
	return nil
}

// DefaultConf returns the tmux.conf used on the recall socket.
func DefaultConf() string {
	return `# recall tmux configuration (private socket, never the user's server)
set -g default-terminal "tmux-256color"
set -ga terminal-overrides ",*:RGB"
set -s escape-time 0
set -g mouse off
set -g status off
set -g history-limit 50000
set -g remain-on-exit on
bind -n 'C-\' detach-client
`
}
