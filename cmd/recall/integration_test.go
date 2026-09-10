package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These tests run the real command pipeline (scan, index, app, launch)
// against a synthesized Claude directory in a temp dir. RECALL_DRY_RUN=1 is
// set by isolate, so nothing is ever executed, focused or attached.

func TestLsJSONAgainstFixture(t *testing.T) {
	claudeDir, _ := isolate(t)
	cwd := writeFixture(t, claudeDir)
	code, out, errOut := execCLI(t, "", "ls", "--json")
	if code != 0 {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("bad json: %v\n%s", err, out)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 sessions, got %d: %s", len(rows), out)
	}
	byID := map[string]map[string]any{}
	for _, r := range rows {
		byID[r["id"].(string)] = r
	}
	r1 := byID[fixtureSID]
	if r1 == nil {
		t.Fatalf("session %s missing: %s", fixtureSID, out)
	}
	if r1["title"] != "Login page" || r1["titleSource"] != "ai-title" {
		t.Errorf("title = %v (%v)", r1["title"], r1["titleSource"])
	}
	if r1["project"] != "proj" || r1["workCwd"] != cwd || r1["branch"] != "main" {
		t.Errorf("project/workCwd/branch = %v / %v / %v", r1["project"], r1["workCwd"], r1["branch"])
	}
	if r1["state"] != "closed" || r1["stateWord"] != "Closed" || r1["live"] != nil {
		t.Errorf("state = %v %v live=%v", r1["state"], r1["stateWord"], r1["live"])
	}
	if r1["turns"].(float64) < 1 {
		t.Errorf("turns = %v", r1["turns"])
	}
	r2 := byID[fixtureSID2]
	if r2 == nil || r2["title"] != "Scanner test fix" || r2["titleSource"] != "custom-title" {
		t.Errorf("session 2 = %v", r2)
	}
}

func TestLsTableAndProjectFilterAgainstFixture(t *testing.T) {
	claudeDir, _ := isolate(t)
	writeFixture(t, claudeDir)
	code, out, errOut := execCLI(t, "", "ls")
	if code != 0 {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	if !strings.Contains(out, "ID") || !strings.Contains(out, "Login page") || !strings.Contains(out, "Closed") {
		t.Fatalf("table:\n%s", out)
	}
	code, out, _ = execCLI(t, "", "ls", "--project", "no-such-project-zzz")
	if code != 0 || strings.TrimSpace(out) != "no sessions" {
		t.Fatalf("filtered: code=%d out=%q", code, out)
	}
	code, out, _ = execCLI(t, "", "ls", "--live", "--json")
	if code != 0 || strings.TrimSpace(out) != "[]" {
		t.Fatalf("--live with no processes: code=%d out=%q", code, out)
	}
}

func TestOpenDryRunResumes(t *testing.T) {
	claudeDir, _ := isolate(t)
	cwd := writeFixture(t, claudeDir)
	t.Setenv("TERM_PROGRAM", "")
	code, out, errOut := execCLI(t, "", "open", "0f1e2d3c", "--dry-run", "--in-place")
	if code != 0 {
		t.Fatalf("code=%d err=%q out=%q", code, errOut, out)
	}
	if !strings.Contains(out, "--resume "+fixtureSID) {
		t.Fatalf("expected a resume command, got %q", out)
	}
	if !strings.Contains(out, cwd) {
		t.Fatalf("expected cd into %s, got %q", cwd, out)
	}
	if strings.Contains(out, "bypassPermissions") {
		t.Fatalf("must never resume with bypassPermissions: %q", out)
	}
	code, out, _ = execCLI(t, "", "open", "0f1e2d3c", "--dry-run", "--fork", "--in-place")
	if code != 0 || !strings.Contains(out, "--fork-session") {
		t.Fatalf("fork: code=%d out=%q", code, out)
	}
	if code, _, errOut := execCLI(t, "", "open", "0f1e", "--dry-run"); code != exitUsage || !strings.Contains(errOut, "2 sessions match") {
		t.Fatalf("ambiguous prefix: code=%d err=%q", code, errOut)
	}
	if code, _, errOut := execCLI(t, "", "open", "ffff", "--dry-run"); code != exitUsage || !strings.Contains(errOut, "no session matches") {
		t.Fatalf("unknown: code=%d err=%q", code, errOut)
	}
}

func TestArchiveAndRestoreAgainstFixture(t *testing.T) {
	claudeDir, recallDir := isolate(t)
	writeFixture(t, claudeDir)
	code, out, errOut := execCLI(t, "", "archive", "0f1e2d3c")
	if code != 0 {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	if !strings.Contains(out, fixtureSID[:8]) || !strings.Contains(out, recallDir) {
		t.Fatalf("archive out=%q", out)
	}
	if _, err := os.Stat(filepath.Join(recallDir, "archive", fixtureSID, "transcript.jsonl")); err != nil {
		t.Fatalf("archived transcript missing: %v", err)
	}
	code, out, errOut = execCLI(t, "", "archive", "--all")
	if code != 0 || strings.Count(out, "\n") != 2 {
		t.Fatalf("archive --all: code=%d out=%q err=%q", code, out, errOut)
	}
	if code, _, _ := execCLI(t, "", "archive", "--all", "extra"); code != exitUsage {
		t.Fatalf("--all with an id should be a usage error, got %d", code)
	}
	code, out, errOut = execCLI(t, "", "restore", "0f1e2d3c")
	if code != 0 || !strings.HasSuffix(strings.TrimSpace(out), ".jsonl") {
		t.Fatalf("restore: code=%d out=%q err=%q", code, out, errOut)
	}
}

func TestDoctorRunsAgainstTempDir(t *testing.T) {
	claudeDir, _ := isolate(t)
	writeFixture(t, claudeDir)
	// doctor may report failures (no claude on PATH in CI); only the output
	// shape is asserted, and the exit code must be 0 or 1.
	code, out, errOut := execCLI(t, "", "doctor")
	if code != 0 && code != exitFailure {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	if !strings.Contains(out, "ok") && !strings.Contains(out, "warn") && !strings.Contains(out, "FAIL") {
		t.Fatalf("no checks printed:\n%s%s", out, errOut)
	}
	code, out, _ = execCLI(t, "", "doctor", "--json")
	var checks []map[string]any
	if err := json.Unmarshal([]byte(out), &checks); err != nil || len(checks) == 0 {
		t.Fatalf("doctor --json: code=%d err=%v out=%q", code, err, out)
	}
	for _, c := range checks {
		if _, ok := c["Name"]; !ok {
			t.Fatalf("check without Name: %v", c)
		}
	}
}

func TestShellPrint(t *testing.T) {
	isolate(t)
	for _, sh := range []string{"zsh", "bash", "fish"} {
		code, out, errOut := execCLI(t, "", "shell", "print", "--shell", sh)
		if code != 0 {
			t.Fatalf("%s: code=%d err=%q", sh, code, errOut)
		}
		if !strings.Contains(out, "# >>> recall >>>") || !strings.Contains(out, "--from-widget") {
			t.Fatalf("%s widget = %q", sh, out)
		}
	}
	if code, _, _ := execCLI(t, "", "shell", "print", "--shell", "csh"); code != exitUsage {
		t.Fatalf("unsupported shell should be a usage error, got %d", code)
	}
}

func TestShellInstallUninstallTempRc(t *testing.T) {
	isolate(t)
	rc := filepath.Join(t.TempDir(), ".zshrc")
	if err := os.WriteFile(rc, []byte("export FOO=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := execCLI(t, "", "shell", "install", "--shell", "zsh", "--rc", rc); code != 0 {
		t.Fatalf("install: code=%d err=%q", code, errOut)
	}
	data, _ := os.ReadFile(rc)
	if !strings.Contains(string(data), "# >>> recall >>>") || !strings.HasPrefix(string(data), "export FOO=1\n") {
		t.Fatalf("rc after install:\n%s", data)
	}
	if code, _, errOut := execCLI(t, "", "shell", "uninstall", "--shell", "zsh", "--rc", rc); code != 0 {
		t.Fatalf("uninstall: code=%d err=%q", code, errOut)
	}
	data, _ = os.ReadFile(rc)
	if strings.Contains(string(data), "recall") || !strings.Contains(string(data), "export FOO=1") {
		t.Fatalf("rc after uninstall:\n%s", data)
	}
}
