package live

import (
	"context"
	"errors"
	"os"

	"github.com/MohammedAl-Alimi/recall/internal/model"
	"golang.org/x/sys/unix"
)

// Ensure x/sys stays in go.mod; flock and CLOEXEC handling use it.
var _ = unix.LOCK_EX

// Prober discovers live claude processes.
type Prober struct {
	Paths model.Paths

	degraded bool
	reason   string
}

// New returns a Prober for the given paths.
func New(p model.Paths) *Prober {
	return &Prober{Paths: p}
}

// Probe returns live process info keyed by session id.
func (p *Prober) Probe(ctx context.Context) (map[string]*model.Live, error) {
	return nil, errors.New("not implemented: live.Prober.Probe")
}

// Degraded reports whether the last probe ran in degraded mode and why.
func (p *Prober) Degraded() (bool, string) {
	return p.degraded, p.reason
}

// Lock takes an exclusive non-blocking flock on RecallDir/locks/<sid>. The
// returned file must be kept open for the lock to hold; its FD is inheritable.
func Lock(p model.Paths, sid string) (*os.File, error) {
	return nil, errors.New("not implemented: live.Lock")
}

// IsLocked reports whether another process holds the lock for sid.
func IsLocked(p model.Paths, sid string) bool {
	return false
}

// ProcessStart returns the start time of pid as printed by
// 'LC_ALL=C TZ=UTC ps -o lstart= -p PID', trimmed.
func ProcessStart(pid int) (string, error) {
	return "", errors.New("not implemented: live.ProcessStart")
}

// HostAppOf walks the parent chain of pid and returns the hosting terminal
// application (Terminal.app, iTerm2, Cursor, Code, tmux, screen) and the tty.
func HostAppOf(pid int) (app string, tty string) {
	return "", ""
}
