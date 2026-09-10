package archive

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/MohammedAl-Alimi/recall/internal/model"
)

func settingsFixture(t *testing.T, body string) model.Paths {
	t.Helper()
	p := model.PathsFrom(t.TempDir(), filepath.Join(t.TempDir(), "recall"))
	if body != "" {
		if err := os.WriteFile(p.SettingsFile, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func TestRetentionMissingFile(t *testing.T) {
	p := settingsFixture(t, "")
	d, set, err := Retention(p)
	if err != nil || set || d != 0 {
		t.Fatalf("missing: %d %v %v", d, set, err)
	}
}

func TestRetentionMissingKey(t *testing.T) {
	p := settingsFixture(t, `{"model":"opus"}`)
	_, set, err := Retention(p)
	if err != nil || set {
		t.Fatalf("missing key: set=%v err=%v", set, err)
	}
}

func TestRetentionSet(t *testing.T) {
	p := settingsFixture(t, `{"cleanupPeriodDays": 99, "model":"opus"}`)
	d, set, err := Retention(p)
	if err != nil || !set || d != 99 {
		t.Fatalf("set: %d %v %v", d, set, err)
	}
}

func TestRetentionBadValue(t *testing.T) {
	p := settingsFixture(t, `{"cleanupPeriodDays": "soon"}`)
	if _, _, err := Retention(p); err == nil {
		t.Fatal("expected error")
	}
	p = settingsFixture(t, `[1,2]`)
	if _, _, err := Retention(p); err == nil {
		t.Fatal("expected error for non-object")
	}
}

func TestSetRetentionMergesAndBacksUp(t *testing.T) {
	orig := `{
  "model": "opus",
  "hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "x"}]}]},
  "someFloat": 1.5,
  "bigNumber": 12345678901234567890
}
`
	p := settingsFixture(t, orig)
	if err := os.Chmod(p.SettingsFile, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := SetRetention(p, 3650); err != nil {
		t.Fatal(err)
	}
	d, set, err := Retention(p)
	if err != nil || !set || d != 3650 {
		t.Fatalf("after set: %d %v %v", d, set, err)
	}
	raw, _ := os.ReadFile(p.SettingsFile)
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatal(err)
	}
	if obj["model"] != "opus" || obj["someFloat"] != 1.5 || obj["hooks"] == nil {
		t.Fatalf("other keys lost: %v", obj)
	}
	if string(raw) == orig || !containsBytes(raw, []byte("12345678901234567890")) {
		t.Fatalf("big number not preserved verbatim:\n%s", raw)
	}
	bak, err := os.ReadFile(p.SettingsFile + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if string(bak) != orig {
		t.Fatal("backup is not the original")
	}
	st, _ := os.Stat(p.SettingsFile)
	if st.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %o, want 0640", st.Mode().Perm())
	}
	// Update again: only the one key changes.
	if err := SetRetention(p, 30); err != nil {
		t.Fatal(err)
	}
	d, _, _ = Retention(p)
	if d != 30 {
		t.Fatalf("second set = %d", d)
	}
	entries, _ := os.ReadDir(p.ClaudeDir)
	for _, e := range entries {
		if e.Name() != "settings.json" && e.Name() != "settings.json.bak" {
			t.Fatalf("unexpected file left in claude dir: %s", e.Name())
		}
	}
}

func TestSetRetentionCreatesFile(t *testing.T) {
	p := settingsFixture(t, "")
	if err := SetRetention(p, 365); err != nil {
		t.Fatal(err)
	}
	d, set, _ := Retention(p)
	if !set || d != 365 {
		t.Fatalf("created: %d %v", d, set)
	}
	if _, err := os.Stat(p.SettingsFile + ".bak"); err == nil {
		t.Fatal("no backup expected when the file did not exist")
	}
	if err := SetRetention(p, 0); err == nil {
		t.Fatal("expected error for 0 days")
	}
}

func TestSetRetentionRefusesCorrupt(t *testing.T) {
	p := settingsFixture(t, `{oops`)
	if err := SetRetention(p, 10); err == nil {
		t.Fatal("expected error on corrupt settings")
	}
	raw, _ := os.ReadFile(p.SettingsFile)
	if string(raw) != `{oops` {
		t.Fatal("corrupt file was modified")
	}
}

func containsBytes(b, sub []byte) bool {
	return len(b) >= len(sub) && (string(b) == string(sub) || indexBytes(b, sub) >= 0)
}

func indexBytes(b, sub []byte) int {
	for i := 0; i+len(sub) <= len(b); i++ {
		if string(b[i:i+len(sub)]) == string(sub) {
			return i
		}
	}
	return -1
}
