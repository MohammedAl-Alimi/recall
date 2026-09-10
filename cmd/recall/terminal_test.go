package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MohammedAl-Alimi/recall/internal/launch"
)

// fakeCmuxOnPath puts a fake cmux executable alone on PATH so the planned
// argv is deterministic. Nothing runs it: every test here is a dry run.
func fakeCmuxOnPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "cmux")
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	return p
}

func TestTermProgramFor(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "ghostty")
	cases := map[string]string{
		"terminal": "Apple_Terminal",
		"iterm":    "iTerm.app",
		"ITerm":    "iTerm.app",
		"cmux":     "ghostty",
		"":         "ghostty",
	}
	for in, want := range cases {
		if got := termProgramFor(in); got != want {
			t.Errorf("termProgramFor(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOpenTerminalCmuxDryRun(t *testing.T) {
	claudeDir, _ := isolate(t)
	writeFixture(t, claudeDir)
	cli := fakeCmuxOnPath(t)
	t.Setenv("CMUX_WORKSPACE_ID", "")

	code, out, errOut := execCLI(t, "", "open", fixtureSID, "--terminal", "cmux", "--dry-run")
	if code != 0 {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	if !strings.Contains(out, cli+" new-workspace") || !strings.Contains(out, "--cwd") || !strings.Contains(out, "--focus true") {
		t.Fatalf("dry run must print the cmux argv, got:\n%s", out)
	}
	if !strings.Contains(out, "claude --resume "+fixtureSID) {
		t.Fatalf("workspace command must resume the session, got:\n%s", out)
	}
	if strings.Contains(out, "osascript") {
		t.Fatalf("cmux must not produce a script, got:\n%s", out)
	}

	// -t is the short form.
	code, out2, errOut := execCLI(t, "", "open", fixtureSID, "-t", "cmux", "--dry-run")
	if code != 0 || out2 != out {
		t.Fatalf("-t cmux: code=%d err=%q\n%s\nvs\n%s", code, errOut, out2, out)
	}

	// Unknown values are usage errors.
	if code, _, errOut := execCLI(t, "", "open", fixtureSID, "--terminal", "kitty", "--dry-run"); code == 0 || !strings.Contains(errOut, "unknown terminal") {
		t.Fatalf("kitty: code=%d err=%q", code, errOut)
	}
}

func TestOpenTerminalForcesAppleScript(t *testing.T) {
	claudeDir, _ := isolate(t)
	writeFixture(t, claudeDir)
	t.Setenv("TERM_PROGRAM", "iTerm.app")
	t.Setenv("CMUX_WORKSPACE_ID", "")

	code, out, errOut := execCLI(t, "", "open", fixtureSID, "--terminal", "terminal", "--dry-run")
	if code != 0 {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	if !strings.Contains(out, `tell application "Terminal"`) || strings.Contains(out, "new-workspace") {
		t.Fatalf("--terminal terminal must plan a Terminal.app tab, got:\n%s", out)
	}
	t.Setenv("TERM_PROGRAM", "Apple_Terminal")
	code, out, errOut = execCLI(t, "", "open", fixtureSID, "--terminal", "iterm", "--dry-run")
	if code != 0 {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	if !strings.Contains(out, `tell application "iTerm2"`) {
		t.Fatalf("--terminal iterm must plan an iTerm2 tab, got:\n%s", out)
	}
}

func TestNewTerminalCmuxDryRun(t *testing.T) {
	isolate(t)
	cli := fakeCmuxOnPath(t)
	t.Setenv("CMUX_WORKSPACE_ID", "")
	dir := t.TempDir()

	code, out, errOut := execCLI(t, "", "new", "-n", "demo", "--cwd", dir, "-t", "cmux", "--", "--model", "sonnet")
	if code != 0 {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	want := "cd " + launch.ShellQuote(dir) + " && claude -n demo --model sonnet"
	if !strings.Contains(out, cli+" new-workspace --name demo --cwd "+launch.ShellQuote(dir)+" --command "+launch.ShellQuote(want)+" --focus true") {
		t.Fatalf("dry run must print the cmux argv, got:\n%s", out)
	}
	if !strings.Contains(out, "start a new claude session in "+dir) || !strings.Contains(out, "open in cmux workspace") {
		t.Fatalf("description, got:\n%s", out)
	}

	// --keep inside cmux wraps claude in tmux within the workspace.
	code, out, errOut = execCLI(t, "", "new", "--cwd", dir, "--keep", "--terminal", "cmux")
	if code != 0 {
		t.Fatalf("keep: code=%d err=%q", code, errOut)
	}
	if !strings.Contains(out, "new-workspace") || !strings.Contains(out, "tmux -L recall new-session -A") {
		t.Fatalf("keep must wrap tmux in the workspace, got:\n%s", out)
	}

	// A bypass request is still dropped and reported.
	code, out, errOut = execCLI(t, "", "new", "--cwd", dir, "-t", "cmux", "--", "--dangerously-skip-permissions")
	if code != 0 {
		t.Fatalf("bypass: code=%d err=%q", code, errOut)
	}
	if strings.Contains(out, "dangerously") || !strings.Contains(out, "note: refused to pass bypassPermissions") {
		t.Fatalf("bypass must be scrubbed and noted, got:\n%s", out)
	}
}

func TestPlanNewIn(t *testing.T) {
	isolate(t)
	fakeCmuxOnPath(t)
	dir := t.TempDir()
	act, err := planNewIn("n", dir, false, nil, "cmux")
	if err != nil {
		t.Fatal(err)
	}
	if act.Kind != "new" || act.Terminal != launch.TerminalCmux || act.Argv[1] != "new-workspace" {
		t.Fatalf("action = %+v", act)
	}
	if _, err := planNewIn("n", dir, false, nil, "nope"); err == nil {
		t.Fatal("unknown terminal must fail")
	}
}
