package live

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MohammedAl-Alimi/recall/internal/model"
	"golang.org/x/sys/unix"
)

// ErrLocked is returned by Lock when another process already holds the lock.
var ErrLocked = errors.New("session is locked by another process")

// lockPath returns RecallDir/locks/<sid>. The sid is reduced to its base name
// so a crafted id can never escape the locks directory.
func lockPath(p model.Paths, sid string) (string, error) {
	if p.RecallDir == "" {
		return "", errors.New("recall dir not set")
	}
	name := filepath.Base(sid)
	if name == "" || name == "." || name == ".." || name == string(filepath.Separator) {
		return "", fmt.Errorf("invalid session id %q", sid)
	}
	return filepath.Join(p.RecallDir, "locks", name), nil
}

// Lock takes an exclusive non-blocking flock on RecallDir/locks/<sid>. The
// returned file must be kept open for the lock to hold. FD_CLOEXEC is cleared
// on the descriptor so a claude process started with exec keeps holding the
// lock for as long as it runs.
func Lock(p model.Paths, sid string) (*os.File, error) {
	path, err := lockPath(p, sid)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	fd := int(f.Fd())
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, ErrLocked
		}
		return nil, err
	}
	// f.Fd() switches the file to blocking mode and Go sets FD_CLOEXEC on
	// open; clear it so the descriptor survives exec.
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_SETFD, 0); err != nil {
		_ = unix.Flock(fd, unix.LOCK_UN)
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

// Unlock releases a lock taken with Lock and closes the file. It is safe to
// call with nil.
func Unlock(f *os.File) error {
	if f == nil {
		return nil
	}
	fd := int(f.Fd())
	unlockErr := unix.Flock(fd, unix.LOCK_UN)
	closeErr := f.Close()
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}

// IsLocked reports whether another process holds the lock for sid. It tries
// a non-blocking exclusive flock on the lock file and releases it at once.
// A missing lock file means not locked.
func IsLocked(p model.Paths, sid string) bool {
	path, err := lockPath(p, sid)
	if err != nil {
		return false
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		return false
	}
	defer f.Close()
	fd := int(f.Fd())
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN)
	}
	_ = unix.Flock(fd, unix.LOCK_UN)
	return false
}
