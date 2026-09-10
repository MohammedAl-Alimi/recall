package archive

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MohammedAl-Alimi/recall/internal/model"
)

func TestMirrorHistory(t *testing.T) {
	p := model.PathsFrom(t.TempDir(), filepath.Join(t.TempDir(), "recall"))
	// no history file: no-op
	if err := MirrorHistory(p); err != nil {
		t.Fatal(err)
	}
	l1 := `{"display":"first prompt","pastedContents":{},"timestamp":1700000000000,"project":"/tmp/a","sessionId":"s1"}` + "\n"
	l2 := `{"display":"second","pastedContents":{},"timestamp":1700000001000,"project":"/tmp/a","sessionId":"s2"}` + "\n"
	if err := os.WriteFile(p.HistoryFile, []byte(l1), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := MirrorHistory(p); err != nil {
		t.Fatal(err)
	}
	mirror := filepath.Join(p.RecallDir, MirrorName)
	got, _ := os.ReadFile(mirror)
	if string(got) != l1 {
		t.Fatalf("mirror after first = %q", got)
	}
	// append a partial line: must not be mirrored yet
	f, _ := os.OpenFile(p.HistoryFile, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(l2[:10])
	f.Close()
	if err := MirrorHistory(p); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(mirror)
	if string(got) != l1 {
		t.Fatalf("partial line mirrored: %q", got)
	}
	f, _ = os.OpenFile(p.HistoryFile, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(l2[10:])
	f.Close()
	if err := MirrorHistory(p); err != nil {
		t.Fatal(err)
	}
	if err := MirrorHistory(p); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(mirror)
	if string(got) != l1+l2 {
		t.Fatalf("mirror after second = %q", got)
	}
	if off := readOffset(filepath.Join(p.RecallDir, OffsetName)); off != int64(len(l1)+len(l2)) {
		t.Fatalf("offset = %d, want %d", off, len(l1)+len(l2))
	}
	// truncated history restarts from 0 and appends again
	if err := os.WriteFile(p.HistoryFile, []byte(l2), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := MirrorHistory(p); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(mirror)
	if string(got) != l1+l2+l2 {
		t.Fatalf("mirror after truncate = %q", got)
	}
}
