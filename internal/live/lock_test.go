package live

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/MohammedAl-Alimi/recall/internal/model"
	"golang.org/x/sys/unix"
)

func TestLockRoundTrip(t *testing.T) {
	p := model.PathsFrom(filepath.Join(t.TempDir(), "claude"), filepath.Join(t.TempDir(), "recall"))
	sid := "0123456789abcdef-0000-0000-0000-000000000000"

	if IsLocked(p, sid) {
		t.Fatal("fresh sid reported locked")
	}
	f, err := Lock(p, sid)
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	if f.Name() != filepath.Join(p.RecallDir, "locks", sid) {
		t.Errorf("lock path = %q", f.Name())
	}
	st, err := os.Stat(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("lock file mode = %o, want 600", st.Mode().Perm())
	}
	dst, err := os.Stat(filepath.Dir(f.Name()))
	if err != nil {
		t.Fatal(err)
	}
	if dst.Mode().Perm() != 0o700 {
		t.Errorf("locks dir mode = %o, want 700", dst.Mode().Perm())
	}

	// The descriptor must survive exec: FD_CLOEXEC cleared.
	flags, err := unix.FcntlInt(f.Fd(), unix.F_GETFD, 0)
	if err != nil {
		t.Fatal(err)
	}
	if flags&unix.FD_CLOEXEC != 0 {
		t.Errorf("FD_CLOEXEC still set on lock fd")
	}

	if !IsLocked(p, sid) {
		t.Error("held lock not reported by IsLocked")
	}
	if _, err := Lock(p, sid); !errors.Is(err, ErrLocked) {
		t.Errorf("second Lock err = %v, want ErrLocked", err)
	}
	if err := Unlock(f); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if IsLocked(p, sid) {
		t.Error("released lock still reported locked")
	}
	f2, err := Lock(p, sid)
	if err != nil {
		t.Fatalf("relock: %v", err)
	}
	_ = Unlock(f2)
	if err := Unlock(nil); err != nil {
		t.Errorf("Unlock(nil) = %v", err)
	}
}

func TestLockRejectsBadInput(t *testing.T) {
	if _, err := Lock(model.Paths{}, "abc"); err == nil {
		t.Error("empty recall dir should fail")
	}
	p := model.PathsFrom(filepath.Join(t.TempDir(), "claude"), filepath.Join(t.TempDir(), "recall"))
	if _, err := Lock(p, ""); err == nil {
		t.Error("empty sid should fail")
	}
	if _, err := Lock(p, ".."); err == nil {
		t.Error("dotdot sid should fail")
	}
	// A path-like sid is reduced to its base name and stays under locks/.
	f, err := Lock(p, "../../escape")
	if err != nil {
		t.Fatal(err)
	}
	defer Unlock(f)
	if filepath.Dir(f.Name()) != filepath.Join(p.RecallDir, "locks") {
		t.Errorf("lock escaped locks dir: %q", f.Name())
	}
	if IsLocked(p, "unknown-sid") {
		t.Error("unknown sid reported locked")
	}
}
