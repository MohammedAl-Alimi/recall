package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

func load(t *testing.T, p model.Paths) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(p.SettingsFile)
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatal(err)
	}
	return obj
}

func commands(obj map[string]any, ev string) []string {
	hooks, _ := obj["hooks"].(map[string]any)
	list, _ := hooks[ev].([]any)
	var out []string
	for _, e := range list {
		m, _ := e.(map[string]any)
		inner, _ := m["hooks"].([]any)
		for _, h := range inner {
			hm, _ := h.(map[string]any)
			cmd, _ := hm["command"].(string)
			out = append(out, cmd)
		}
	}
	return out
}

const existing = `{
  "model": "opus",
  "hooks": {
    "PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "rtk hook claude"}]}],
    "SessionStart": [{"hooks": [{"type": "command", "command": "echo hi"}]}]
  }
}
`

func TestInstallMergesAdditively(t *testing.T) {
	p := settingsFixture(t, existing)
	bin := "/usr/local/bin/recall"
	if err := InstallSettings(p, []string{"SessionStart", "SessionEnd", "Notification"}, bin); err != nil {
		t.Fatal(err)
	}
	obj := load(t, p)
	if obj["model"] != "opus" {
		t.Fatal("model key lost")
	}
	if got := commands(obj, "PreToolUse"); len(got) != 1 || got[0] != "rtk hook claude" {
		t.Fatalf("PreToolUse changed: %v", got)
	}
	ss := commands(obj, "SessionStart")
	if len(ss) != 2 || ss[0] != "echo hi" || ss[1] != Command(bin, "SessionStart") {
		t.Fatalf("SessionStart = %v", ss)
	}
	if got := commands(obj, "SessionEnd"); len(got) != 1 || !strings.Contains(got[0], "/recall hook SessionEnd >/dev/null 2>&1 || true") {
		t.Fatalf("SessionEnd = %v", got)
	}
	if got := commands(obj, "Notification"); len(got) != 1 {
		t.Fatalf("Notification = %v", got)
	}
	if _, err := os.Stat(p.SettingsFile + ".bak"); err != nil {
		t.Fatal("backup missing")
	}
	// Second install is a no-op: no duplicates, backup unchanged.
	before, _ := os.ReadFile(p.SettingsFile)
	if err := InstallSettings(p, nil, bin); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(p.SettingsFile)
	if string(before) != string(after) {
		t.Fatal("second install modified settings")
	}
	if got := commands(load(t, p), "SessionStart"); len(got) != 2 {
		t.Fatalf("duplicate recall hook: %v", got)
	}
}

func TestInstallCreatesSettings(t *testing.T) {
	p := settingsFixture(t, "")
	if err := InstallSettings(p, []string{"SessionStart"}, "/opt/re call/recall"); err != nil {
		t.Fatal(err)
	}
	got := commands(load(t, p), "SessionStart")
	if len(got) != 1 || !strings.HasPrefix(got[0], "'/opt/re call/recall' hook SessionStart") {
		t.Fatalf("cmd = %v", got)
	}
	st, _ := os.Stat(p.SettingsFile)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", st.Mode().Perm())
	}
	if err := InstallSettings(p, nil, ""); err == nil {
		t.Fatal("expected error for empty bin")
	}
}

func TestUninstallRemovesOnlyRecall(t *testing.T) {
	p := settingsFixture(t, existing)
	bin := "/usr/local/bin/recall"
	if err := InstallSettings(p, nil, bin); err != nil {
		t.Fatal(err)
	}
	if err := UninstallSettings(p); err != nil {
		t.Fatal(err)
	}
	obj := load(t, p)
	if got := commands(obj, "SessionStart"); len(got) != 1 || got[0] != "echo hi" {
		t.Fatalf("SessionStart after uninstall = %v", got)
	}
	if got := commands(obj, "PreToolUse"); len(got) != 1 {
		t.Fatalf("PreToolUse after uninstall = %v", got)
	}
	hooks := obj["hooks"].(map[string]any)
	for _, ev := range []string{"SessionEnd", "Notification"} {
		if _, ok := hooks[ev]; ok {
			t.Fatalf("%s should be removed entirely", ev)
		}
	}
	if obj["model"] != "opus" {
		t.Fatal("model lost")
	}
	// Uninstall on a file without recall hooks is a no-op.
	before, _ := os.ReadFile(p.SettingsFile)
	if err := UninstallSettings(p); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(p.SettingsFile)
	if string(before) != string(after) {
		t.Fatal("no-op uninstall rewrote the file")
	}
	// Missing settings file is fine too.
	if err := UninstallSettings(settingsFixture(t, "")); err != nil {
		t.Fatal(err)
	}
}

func TestUninstallDropsEmptyHooksObject(t *testing.T) {
	p := settingsFixture(t, `{"model":"opus"}`)
	if err := InstallSettings(p, nil, "/usr/local/bin/recall"); err != nil {
		t.Fatal(err)
	}
	if err := UninstallSettings(p); err != nil {
		t.Fatal(err)
	}
	obj := load(t, p)
	if _, ok := obj["hooks"]; ok {
		t.Fatalf("empty hooks object left behind: %v", obj)
	}
}

func TestInstallRefusesCorrupt(t *testing.T) {
	p := settingsFixture(t, `{"hooks": "nope"}`)
	if err := InstallSettings(p, nil, "/usr/local/bin/recall"); err == nil {
		t.Fatal("expected error when hooks is not an object")
	}
	p = settingsFixture(t, `{broken`)
	if err := InstallSettings(p, nil, "/usr/local/bin/recall"); err == nil {
		t.Fatal("expected error on corrupt json")
	}
}
