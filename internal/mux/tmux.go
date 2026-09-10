package mux

import (
	"errors"

	"github.com/MohammedAl-Alimi/recall/internal/model"
)

// Tmux drives tmux on a private socket.
type Tmux struct {
	// Socket is the tmux -L socket name. Default "recall".
	Socket string
}

// NewTmux returns a Tmux using the default socket.
func NewTmux() *Tmux {
	return &Tmux{Socket: "recall"}
}

// Available reports whether a tmux binary >= 3.2 is on PATH and its version.
func (t *Tmux) Available() (bool, string) {
	return false, ""
}

// SessionName returns the tmux session name for a session id.
func (t *Tmux) SessionName(sid string) string {
	return "rc-" + model.ShortID(sid)
}

// NewKeptCommand returns the argv that creates a detached tmux session
// running claude with RECALL_SID and RECALL_BYPASS set.
func (t *Tmux) NewKeptCommand(sid, name, cwd string, claudeArgs []string) []string {
	return nil
}

// AttachCommand returns the argv that attaches to the kept session for sid.
func (t *Tmux) AttachCommand(sid string) []string {
	return nil
}

// List returns every pane on the recall socket. It returns an empty slice
// when tmux is missing.
func (t *Tmux) List() ([]model.MuxInfo, error) {
	return []model.MuxInfo{}, nil
}

// Kill terminates the kept session for sid.
func (t *Tmux) Kill(sid string) error {
	return errors.New("not implemented: mux.Tmux.Kill")
}

// DefaultConf returns the tmux.conf used on the recall socket.
func DefaultConf() string {
	return `set -g default-terminal "tmux-256color"
set -ga terminal-overrides ",*:RGB"
set -s escape-time 0
set -g mouse off
set -g status off
set -g history-limit 50000
set -g remain-on-exit on
bind -n 'C-\' detach-client
`
}
