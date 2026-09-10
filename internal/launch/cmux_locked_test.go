package launch

import (
	"errors"
	"strings"
	"testing"
)

func TestCmuxAccessDenied(t *testing.T) {
	cases := []struct {
		name string
		msg  string
		want bool
	}{
		{"real cmux refusal", "Error: ERROR: Access denied - only processes started inside cmux can connect", true},
		{"lowercase", "access denied", true},
		{"phrase only", "only processes started inside cmux can connect", true},
		{"socket missing", "Error: Socket not found at /Users/me/.local/state/cmux/cmux.sock", false},
		{"empty", "", false},
		{"unrelated", "workspace not found", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := cmuxAccessDenied(c.msg); got != c.want {
				t.Fatalf("cmuxAccessDenied(%q) = %v, want %v", c.msg, got, c.want)
			}
		})
	}
}

func TestErrCmuxLockedExplainsTheFix(t *testing.T) {
	msg := ErrCmuxLocked.Error()
	for _, want := range []string{"socketControlMode", "CMUX_SOCKET_PASSWORD", "Settings"} {
		if !strings.Contains(msg, want) {
			t.Errorf("ErrCmuxLocked should mention %q, got: %s", want, msg)
		}
	}
	if !errors.Is(ErrCmuxLocked, ErrCmuxLocked) {
		t.Fatal("ErrCmuxLocked should match itself")
	}
}
