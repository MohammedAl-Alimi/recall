package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	version = "1.2.3-test"
	root := newRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestPathsFlags(t *testing.T) {
	claude := t.TempDir()
	recall := t.TempDir()
	flagClaudeDir, flagRecallDir = claude, recall
	defer func() { flagClaudeDir, flagRecallDir = "", "" }()
	p := paths()
	if p.ClaudeDir != claude || p.RecallDir != recall {
		t.Fatalf("paths() = %+v", p)
	}
	if !strings.HasPrefix(p.ProjectsDir, claude) {
		t.Fatalf("ProjectsDir = %q", p.ProjectsDir)
	}
}

func TestCommandsRegistered(t *testing.T) {
	root := newRoot()
	want := []string{"ls", "open", "new", "exec", "hook", "archive", "restore", "doctor", "setup", "uninstall", "shell", "index", "version"}
	have := map[string]bool{}
	for _, c := range root.Commands() {
		have[c.Name()] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("command %q not registered", w)
		}
	}
}
