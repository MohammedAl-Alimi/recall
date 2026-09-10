package model

import (
	"path/filepath"
	"testing"
)

func TestShortID(t *testing.T) {
	cases := map[string]string{
		"":                                     "",
		"abc":                                  "abc",
		"12345678":                             "12345678",
		"0123456789abcdef-0000-0000-0000-0000": "01234567",
	}
	for in, want := range cases {
		if got := ShortID(in); got != want {
			t.Errorf("ShortID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStateWord(t *testing.T) {
	cases := map[State]string{
		StateNeedsYou:    "Needs you",
		StateLiveIdle:    "Running",
		StateLiveBusy:    "Running",
		StateKept:        "Running",
		StateBG:          "Running",
		StateForeign:     "Running",
		StateBGStale:     "Gone",
		StateGhost:       "Gone",
		StateStale:       "Expiring",
		StateClosed:      "Closed",
		StateInterrupted: "Closed",
		StateArchived:    "Closed",
		StateHeadless:    "Closed",
		State("unknown"): "Closed",
	}
	for s, want := range cases {
		if got := s.Word(); got != want {
			t.Errorf("%q.Word() = %q, want %q", s, got, want)
		}
	}
}

func TestStateHint(t *testing.T) {
	if got := StateStale.Hint(); got != "a archives before deletion" {
		t.Errorf("stale hint = %q", got)
	}
	for _, s := range []State{StateGhost, StateBGStale, StateClosed, StateNeedsYou, StateKept, StateArchived, State("unknown")} {
		if got := s.Hint(); got != "" {
			t.Errorf("%q.Hint() = %q, want empty", s, got)
		}
	}
	// Stale must never share the ghost word: its transcript is still on disk.
	if StateStale.Word() == StateGhost.Word() {
		t.Errorf("stale and ghost share the word %q", StateStale.Word())
	}
}

func TestPathsFrom(t *testing.T) {
	p := PathsFrom("/c", "/r")
	if p.ClaudeDir != "/c" || p.RecallDir != "/r" {
		t.Fatalf("unexpected dirs: %+v", p)
	}
	if p.ProjectsDir != filepath.Join("/c", "projects") {
		t.Errorf("ProjectsDir = %q", p.ProjectsDir)
	}
	if p.HistoryFile != filepath.Join("/c", "history.jsonl") {
		t.Errorf("HistoryFile = %q", p.HistoryFile)
	}
	if p.SessionsDir != filepath.Join("/c", "sessions") {
		t.Errorf("SessionsDir = %q", p.SessionsDir)
	}
	if p.DaemonDir != filepath.Join("/c", "daemon") {
		t.Errorf("DaemonDir = %q", p.DaemonDir)
	}
	if p.SettingsFile != filepath.Join("/c", "settings.json") {
		t.Errorf("SettingsFile = %q", p.SettingsFile)
	}
}

func TestDefaultPathsEnv(t *testing.T) {
	claude := t.TempDir()
	recall := t.TempDir()
	projects := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", claude)
	t.Setenv("RECALL_DIR", recall)
	t.Setenv("RECALL_PROJECTS_DIR", "")
	p := DefaultPaths()
	if p.ClaudeDir != claude {
		t.Errorf("ClaudeDir = %q, want %q", p.ClaudeDir, claude)
	}
	if p.RecallDir != recall {
		t.Errorf("RecallDir = %q, want %q", p.RecallDir, recall)
	}
	if p.ProjectsDir != filepath.Join(claude, "projects") {
		t.Errorf("ProjectsDir = %q", p.ProjectsDir)
	}
	t.Setenv("RECALL_PROJECTS_DIR", projects)
	p = DefaultPaths()
	if p.ProjectsDir != projects {
		t.Errorf("ProjectsDir override = %q, want %q", p.ProjectsDir, projects)
	}
}

func TestDefaultPathsHomeFallback(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("RECALL_DIR", "")
	t.Setenv("RECALL_PROJECTS_DIR", "")
	p := DefaultPaths()
	if filepath.Base(p.ClaudeDir) != ".claude" {
		t.Errorf("ClaudeDir = %q, want to end in .claude", p.ClaudeDir)
	}
	if filepath.Base(p.RecallDir) != ".recall" {
		t.Errorf("RecallDir = %q, want to end in .recall", p.RecallDir)
	}
}

func TestSessionShort(t *testing.T) {
	s := &Session{ID: "abcdefghijkl"}
	if s.Short() != "abcdefgh" {
		t.Errorf("Short() = %q", s.Short())
	}
	if (State("")).IsLive() || !StateKept.IsLive() {
		t.Errorf("IsLive mismatch")
	}
}
