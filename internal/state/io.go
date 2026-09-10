package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/MohammedAl-Alimi/recall/internal/model"
)

// File names under RecallDir.
const (
	MetaFile     = "meta.json"
	LockFile     = "state.lock"
	EventsFile   = "events.jsonl"
	LastBootFile = "lastboot.json"
	LaunchDir    = "launch"
)

// ensureDir creates RecallDir (0700) when missing.
func ensureDir(dir string) error {
	if dir == "" {
		return errors.New("state: empty recall dir")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return nil
}

// withLock runs fn while holding an exclusive flock on RecallDir/state.lock.
func withLock(p model.Paths, fn func() error) error {
	if err := ensureDir(p.RecallDir); err != nil {
		return err
	}
	lf, err := os.OpenFile(filepath.Join(p.RecallDir, LockFile), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lf.Close()
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("state: lock: %w", err)
	}
	defer syscall.Flock(int(lf.Fd()), syscall.LOCK_UN)
	return fn()
}

// writeAtomic writes data to path via a temp file in the same directory
// followed by rename. The file is created 0600.
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := ensureDir(dir); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return err
	}
	return nil
}

func metaPath(p model.Paths) string { return filepath.Join(p.RecallDir, MetaFile) }

// LoadMeta reads RecallDir/meta.json, returning an empty Meta when missing.
func LoadMeta(p model.Paths) (*Meta, error) {
	data, err := os.ReadFile(metaPath(p))
	if err != nil {
		if os.IsNotExist(err) {
			return NewMeta(), nil
		}
		return nil, err
	}
	m := NewMeta()
	if len(strings.TrimSpace(string(data))) == 0 {
		return m, nil
	}
	if err := json.Unmarshal(data, m); err != nil {
		return nil, fmt.Errorf("state: parse %s: %w", metaPath(p), err)
	}
	if m.Labels == nil {
		m.Labels = map[string]string{}
	}
	if m.Notes == nil {
		m.Notes = map[string]string{}
	}
	if m.Tags == nil {
		m.Tags = map[string][]string{}
	}
	if m.Schema == 0 {
		m.Schema = SchemaVersion
	}
	return m, nil
}

// SaveMeta writes meta atomically under the state lock.
func SaveMeta(p model.Paths, m *Meta) error {
	if m == nil {
		return errors.New("state: nil meta")
	}
	if m.Schema == 0 {
		m.Schema = SchemaVersion
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return withLock(p, func() error {
		return writeAtomic(metaPath(p), data)
	})
}

func launchPath(p model.Paths, sid string) string {
	return filepath.Join(p.RecallDir, LaunchDir, safeName(sid)+".json")
}

// safeName strips path separators from an id so it can be used as a file name.
func safeName(s string) string {
	return strings.NewReplacer("/", "_", "\\", "_", "..", "_").Replace(s)
}

// SaveLaunch stores a launch record under RecallDir/launch/<sid>.json.
func SaveLaunch(p model.Paths, l *model.Launch) error {
	if l == nil || l.SID == "" {
		return errors.New("state: launch record needs a session id")
	}
	if l.At.IsZero() {
		l.At = time.Now()
	}
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeAtomic(launchPath(p, l.SID), data)
}

// LoadLaunch reads the launch record for sid. A missing record returns
// (nil, nil).
func LoadLaunch(p model.Paths, sid string) (*model.Launch, error) {
	if sid == "" {
		return nil, errors.New("state: empty session id")
	}
	data, err := os.ReadFile(launchPath(p, sid))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var l model.Launch
	if err := json.Unmarshal(data, &l); err != nil {
		return nil, fmt.Errorf("state: parse launch %s: %w", sid, err)
	}
	return &l, nil
}

// AppendEvent appends one JSON line to RecallDir/events.jsonl.
func AppendEvent(p model.Paths, ev map[string]any) error {
	if ev == nil {
		ev = map[string]any{}
	}
	if _, ok := ev["ts"]; !ok {
		ev["ts"] = time.Now().UTC().Format(time.RFC3339Nano)
	}
	line, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	if err := ensureDir(p.RecallDir); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(p.RecallDir, EventsFile), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(line)
	return err
}

type lastBoot struct {
	At   time.Time `json:"at"`
	SIDs []string  `json:"sids"`
}

// SnapshotLastBoot records the session ids seen at this boot.
func SnapshotLastBoot(p model.Paths, sids []string) error {
	if sids == nil {
		sids = []string{}
	}
	data, err := json.MarshalIndent(lastBoot{At: time.Now(), SIDs: sids}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return withLock(p, func() error {
		return writeAtomic(filepath.Join(p.RecallDir, LastBootFile), data)
	})
}

// LoadLastBoot returns the session ids recorded by SnapshotLastBoot. A
// missing snapshot returns an empty list.
func LoadLastBoot(p model.Paths) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(p.RecallDir, LastBootFile))
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	var lb lastBoot
	if err := json.Unmarshal(data, &lb); err != nil {
		return nil, fmt.Errorf("state: parse lastboot: %w", err)
	}
	if lb.SIDs == nil {
		lb.SIDs = []string{}
	}
	return lb.SIDs, nil
}
