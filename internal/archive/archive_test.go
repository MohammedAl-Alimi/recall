package archive

import (
	"crypto/sha256"
	"errors"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MohammedAl-Alimi/recall/internal/model"
)

const sid = "11111111-2222-3333-4444-555555555555"

// fixture builds a temp Claude dir with one synthesized transcript and
// sidecar dirs, plus a temp recall dir.
func fixture(t *testing.T) (model.Paths, *model.Session) {
	t.Helper()
	claude := t.TempDir()
	p := model.PathsFrom(claude, filepath.Join(t.TempDir(), "recall"))
	proj := filepath.Join(p.ProjectsDir, "-Users-tester-proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(proj, sid+".jsonl")
	lines := []string{
		`{"type":"user","uuid":"u1","sessionId":"` + sid + `","cwd":"/Users/tester/proj","message":{"role":"user","content":"hello"}}`,
		`{"type":"assistant","uuid":"a1","sessionId":"` + sid + `","message":{"role":"assistant","content":[{"type":"text","text":"hi"}]}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// sidecars
	sub := filepath.Join(proj, sid, "subagents")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "agent-1.jsonl"), []byte(`{"type":"user"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fh := filepath.Join(claude, "file-history", sid)
	if err := os.MkdirAll(fh, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fh, "abc@v1"), []byte("old content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := &model.Session{ID: sid, Path: path, Cwd: "/Users/tester/proj", WorkCwd: "/Users/tester/proj"}
	return p, sess
}

func sha(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func TestArchiveHardLinkAndManifest(t *testing.T) {
	p, sess := fixture(t)
	dir, err := Archive(p, sess, false)
	if err != nil {
		t.Fatal(err)
	}
	if dir != Dir(p, sid) {
		t.Fatalf("dir = %s", dir)
	}
	dst := filepath.Join(dir, TranscriptName)
	same, err := sameFile(sess.Path, dst)
	if err != nil || !same {
		t.Fatalf("expected hard link: same=%v err=%v", same, err)
	}
	m, err := ReadManifest(p, sid)
	if err != nil || m == nil {
		t.Fatalf("manifest: %v %v", m, err)
	}
	if !m.Linked || m.SID != sid || m.CompletedAt.IsZero() || len(m.Files) != 1 {
		t.Fatalf("manifest fields: %+v", m)
	}
	if m.Files[0].Path != TranscriptName || m.Files[0].SHA256 != sha(t, sess.Path) || m.Files[0].Size == 0 {
		t.Fatalf("manifest file: %+v", m.Files[0])
	}
	// raw json keys per contract
	raw, _ := os.ReadFile(filepath.Join(dir, ManifestName))
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"files", "completedAt"} {
		if _, ok := generic[k]; !ok {
			t.Fatalf("manifest missing key %s", k)
		}
	}
	got, ok := HasArchive(p, sid)
	if !ok || got != dst {
		t.Fatalf("HasArchive = %q %v", got, ok)
	}
	if _, ok := HasArchive(p, "nope"); ok {
		t.Fatal("HasArchive false positive")
	}
	// idempotent
	if _, err := Archive(p, sess, false); err != nil {
		t.Fatal(err)
	}
}

func TestArchiveCopyFallback(t *testing.T) {
	p, sess := fixture(t)
	old := linkFn
	linkFn = func(string, string) error { return errors.New("EXDEV") }
	defer func() { linkFn = old }()
	dir := Dir(p, sid)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dir, TranscriptName)
	if err := os.WriteFile(stale, []byte("stale\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Archive(p, sess, false); err != nil {
		t.Fatal(err)
	}
	if sha(t, stale) != sha(t, sess.Path) {
		t.Fatal("stale archive was not replaced")
	}
	if same, _ := sameFile(stale, sess.Path); same {
		t.Fatal("expected a copy, got a link")
	}
	m, _ := ReadManifest(p, sid)
	if m.Linked {
		t.Fatal("manifest claims a link")
	}
	// direct copy path
	dst := filepath.Join(t.TempDir(), "copy.jsonl")
	if err := copyFile(sess.Path, dst); err != nil {
		t.Fatal(err)
	}
	if sha(t, dst) != sha(t, sess.Path) {
		t.Fatal("copy mismatch")
	}
	st, _ := os.Stat(dst)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("copy perms = %o", st.Mode().Perm())
	}
}

func TestArchiveWithSidecars(t *testing.T) {
	p, sess := fixture(t)
	dir, err := Archive(p, sess, true)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(dir, SidecarDirName, "session", "subagents", "agent-1.jsonl"),
		filepath.Join(dir, SidecarDirName, "file-history", "abc@v1"),
	}
	for _, w := range want {
		if _, err := os.Stat(w); err != nil {
			t.Fatalf("sidecar missing: %s", w)
		}
	}
	m, _ := ReadManifest(p, sid)
	if len(m.Files) != 3 {
		t.Fatalf("manifest files = %d: %+v", len(m.Files), m.Files)
	}
	// Detached sidecar copy after a bare archive merges into the manifest.
	p2, sess2 := fixture(t)
	if _, err := Archive(p2, sess2, false); err != nil {
		t.Fatal(err)
	}
	if _, err := CopySidecars(p2, sess2); err != nil {
		t.Fatal(err)
	}
	m2, _ := ReadManifest(p2, sid)
	if len(m2.Files) != 3 || m2.Files[len(m2.Files)-1].Path != TranscriptName {
		t.Fatalf("merged manifest = %+v", m2.Files)
	}
}

func TestArchiveErrors(t *testing.T) {
	p, sess := fixture(t)
	if _, err := Archive(p, &model.Session{}, false); err == nil {
		t.Fatal("expected error for empty session")
	}
	sess.Path = filepath.Join(t.TempDir(), "missing.jsonl")
	if _, err := Archive(p, sess, false); err == nil {
		t.Fatal("expected error for missing transcript")
	}
}

func TestRestoreOriginalPresent(t *testing.T) {
	p, sess := fixture(t)
	if _, err := Archive(p, sess, false); err != nil {
		t.Fatal(err)
	}
	got, err := Restore(p, sess)
	if err != nil || got != sess.Path {
		t.Fatalf("restore = %q %v", got, err)
	}
}

func TestRestoreCopiesBack(t *testing.T) {
	p, sess := fixture(t)
	if _, err := Archive(p, sess, false); err != nil {
		t.Fatal(err)
	}
	want := sha(t, sess.Path)
	if err := os.Remove(sess.Path); err != nil {
		t.Fatal(err)
	}
	got, err := Restore(p, sess)
	if err != nil {
		t.Fatal(err)
	}
	if got != sess.Path {
		t.Fatalf("restore path = %s", got)
	}
	if sha(t, got) != want {
		t.Fatal("restored content mismatch")
	}
}

func TestRestoreRefusesWhenOrphanedExists(t *testing.T) {
	p, sess := fixture(t)
	if _, err := Archive(p, sess, false); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(p.ProjectsDir, "-Users-other", sid+".jsonl.orphaned-20260101")
	if err := os.MkdirAll(filepath.Dir(other), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(sess.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(p, sess); err == nil || !strings.Contains(err.Error(), "orphaned") {
		t.Fatalf("expected refusal, got %v", err)
	}
}

func TestRestoreNoArchive(t *testing.T) {
	p, sess := fixture(t)
	if err := os.Remove(sess.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(p, sess); err == nil {
		t.Fatal("expected error without archive")
	}
}

func TestRestoreDerivesPathFromCwd(t *testing.T) {
	p, sess := fixture(t)
	if _, err := Archive(p, sess, false); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(sess.Path); err != nil {
		t.Fatal(err)
	}
	sess.Path = ""
	got, err := Restore(p, sess)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(p.ProjectsDir, "-Users-tester-proj", sid+".jsonl")
	if got != want {
		t.Fatalf("derived path = %s, want %s", got, want)
	}
}

func TestEncodeProjectDir(t *testing.T) {
	if got := encodeProjectDir("/Users/a b/x.y_z"); got != "-Users-a-b-x-y-z" {
		t.Fatalf("encode = %s", got)
	}
}
