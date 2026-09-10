package mux

import (
	"strings"
	"testing"
)

func TestSessionName(t *testing.T) {
	tm := NewTmux()
	if tm.Socket != "recall" {
		t.Errorf("Socket = %q", tm.Socket)
	}
	if got := tm.SessionName("0123456789abcdef"); got != "rc-01234567" {
		t.Errorf("SessionName = %q", got)
	}
}

func TestDefaultConf(t *testing.T) {
	c := DefaultConf()
	for _, want := range []string{"tmux-256color", "RGB", "escape-time 0", "mouse off", "status off", "history-limit 50000", "remain-on-exit on", `bind -n 'C-\' detach`} {
		if !strings.Contains(c, want) {
			t.Errorf("DefaultConf missing %q", want)
		}
	}
}
