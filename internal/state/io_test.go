package state

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MohammedAl-Alimi/recall/internal/model"
)

func tempPaths(t *testing.T) model.Paths {
	t.Helper()
	return model.PathsFrom(t.TempDir(), filepath.Join(t.TempDir(), "recall"))
}

func TestLoadMetaMissing(t *testing.T) {
	p := tempPaths(t)
	m, err := LoadMeta(p)
	if err != nil {
		t.Fatal(err)
	}
	if m.Schema != SchemaVersion || m.Labels == nil || m.Notes == nil || m.Tags == nil {
		t.Fatalf("empty meta not initialised: %+v", m)
	}
}

func TestSaveLoadMetaRoundTrip(t *testing.T) {
	p := tempPaths(t)
	m := NewMeta()
	m.Pin("aaa")
	m.Hide("bbb")
	m.SetLabel("aaa", "work")
	m.Notes["aaa"] = "note"
	m.Tags["aaa"] = []string{"x", "y"}
	m.TrashAdd("ccc")
	if err := SaveMeta(p, m); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(p.RecallDir, MetaFile))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("meta perms = %o, want 0600", st.Mode().Perm())
	}
	dst, err := os.Stat(p.RecallDir)
	if err != nil {
		t.Fatal(err)
	}
	if dst.Mode().Perm() != 0o700 {
		t.Fatalf("recall dir perms = %o, want 0700", dst.Mode().Perm())
	}
	got, err := LoadMeta(p)
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsPinned("aaa") || !got.IsHidden("bbb") || got.Labels["aaa"] != "work" || got.Notes["aaa"] != "note" || len(got.Tags["aaa"]) != 2 || len(got.Trash) != 1 || got.Trash[0].SID != "ccc" {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	// no temp files left behind
	entries, _ := os.ReadDir(p.RecallDir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
}

func TestLoadMetaCorrupt(t *testing.T) {
	p := tempPaths(t)
	if err := os.MkdirAll(p.RecallDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.RecallDir, MetaFile), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadMeta(p); err == nil {
		t.Fatal("expected error on corrupt meta")
	}
}

func TestSaveMetaConcurrent(t *testing.T) {
	p := tempPaths(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m := NewMeta()
			m.Pin(strings.Repeat("a", i+1))
			if err := SaveMeta(p, m); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if _, err := LoadMeta(p); err != nil {
		t.Fatalf("meta unreadable after concurrent saves: %v", err)
	}
}

func TestLaunchRoundTrip(t *testing.T) {
	p := tempPaths(t)
	got, err := LoadLaunch(p, "missing")
	if err != nil || got != nil {
		t.Fatalf("missing launch: %v %v", got, err)
	}
	l := &model.Launch{SID: "0123abcd-1", Argv: []string{"claude", "--add-dir", "/x"}, Cwd: "/tmp", TermProgram: "Apple_Terminal", RecordedBy: "hook"}
	if err := SaveLaunch(p, l); err != nil {
		t.Fatal(err)
	}
	if l.At.IsZero() {
		t.Fatal("At not stamped")
	}
	got, err = LoadLaunch(p, "0123abcd-1")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Cwd != "/tmp" || len(got.Argv) != 3 || got.TermProgram != "Apple_Terminal" {
		t.Fatalf("launch mismatch: %+v", got)
	}
	if err := SaveLaunch(p, &model.Launch{}); err == nil {
		t.Fatal("expected error for empty sid")
	}
	// path traversal in sid must stay inside the launch dir
	if err := SaveLaunch(p, &model.Launch{SID: "../evil"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(p.RecallDir, "evil.json")); err == nil {
		t.Fatal("sid escaped the launch dir")
	}
}

func TestAppendEvent(t *testing.T) {
	p := tempPaths(t)
	for i := 0; i < 3; i++ {
		if err := AppendEvent(p, map[string]any{"event": "x", "n": i}); err != nil {
			t.Fatal(err)
		}
	}
	f, err := os.Open(filepath.Join(p.RecallDir, EventsFile))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		var ev map[string]any
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			t.Fatalf("line %d not json: %v", n, err)
		}
		if _, ok := ev["ts"]; !ok {
			t.Fatal("ts missing")
		}
		if ev["event"] != "x" {
			t.Fatalf("event mismatch: %v", ev)
		}
		n++
	}
	if n != 3 {
		t.Fatalf("lines = %d, want 3", n)
	}
}

func TestLastBoot(t *testing.T) {
	p := tempPaths(t)
	got, err := LoadLastBoot(p)
	if err != nil || len(got) != 0 {
		t.Fatalf("missing lastboot: %v %v", got, err)
	}
	if err := SnapshotLastBoot(p, []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	got, err = LoadLastBoot(p)
	if err != nil || len(got) != 2 || got[0] != "a" {
		t.Fatalf("lastboot: %v %v", got, err)
	}
	if err := SnapshotLastBoot(p, nil); err != nil {
		t.Fatal(err)
	}
	got, err = LoadLastBoot(p)
	if err != nil || len(got) != 0 {
		t.Fatalf("empty lastboot: %v %v", got, err)
	}
}

func TestTrashPruneBoundary(t *testing.T) {
	m := NewMeta()
	m.Trash = []TrashEntry{{SID: "old", At: time.Now().AddDate(0, 0, -31)}, {SID: "new", At: time.Now()}}
	pruned := m.TrashPrune(30)
	if len(pruned) != 1 || pruned[0] != "old" {
		t.Fatalf("pruned = %v", pruned)
	}
}
