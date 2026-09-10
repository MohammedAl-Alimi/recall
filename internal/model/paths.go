package model

import (
	"os"
	"path/filepath"
)

// Paths locates the Claude Code config tree (read-only for recall) and the
// recall state directory (owned by recall).
type Paths struct {
	// ClaudeDir is ~/.claude or $CLAUDE_CONFIG_DIR. Read-only.
	ClaudeDir string
	// ProjectsDir is ClaudeDir/projects (or $RECALL_PROJECTS_DIR). Read-only.
	ProjectsDir string
	// RecallDir is ~/.recall or $RECALL_DIR. Owned by recall.
	RecallDir string
	// HistoryFile is ClaudeDir/history.jsonl. Read-only.
	HistoryFile string
	// SessionsDir is ClaudeDir/sessions, the live process registry. Read-only.
	SessionsDir string
	// DaemonDir is ClaudeDir/daemon. Read-only; recall only checks for
	// daemon.lock appearing after 'claude agents --json'.
	DaemonDir string
	// SettingsFile is ClaudeDir/settings.json. Written only by explicit
	// setup/uninstall commands.
	SettingsFile string
}

// DefaultPaths resolves paths from the environment:
//
//	CLAUDE_CONFIG_DIR   overrides ~/.claude
//	RECALL_PROJECTS_DIR overrides ClaudeDir/projects
//	RECALL_DIR          overrides ~/.recall
func DefaultPaths() Paths {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "."
	}
	claudeDir := os.Getenv("CLAUDE_CONFIG_DIR")
	if claudeDir == "" {
		claudeDir = filepath.Join(home, ".claude")
	}
	recallDir := os.Getenv("RECALL_DIR")
	if recallDir == "" {
		recallDir = filepath.Join(home, ".recall")
	}
	p := PathsFrom(claudeDir, recallDir)
	if v := os.Getenv("RECALL_PROJECTS_DIR"); v != "" {
		p.ProjectsDir = v
	}
	return p
}

// PathsFrom builds Paths from an explicit Claude dir and recall dir.
func PathsFrom(claudeDir, recallDir string) Paths {
	return Paths{
		ClaudeDir:    claudeDir,
		ProjectsDir:  filepath.Join(claudeDir, "projects"),
		RecallDir:    recallDir,
		HistoryFile:  filepath.Join(claudeDir, "history.jsonl"),
		SessionsDir:  filepath.Join(claudeDir, "sessions"),
		DaemonDir:    filepath.Join(claudeDir, "daemon"),
		SettingsFile: filepath.Join(claudeDir, "settings.json"),
	}
}
