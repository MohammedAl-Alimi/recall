package hook

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MohammedAl-Alimi/recall/internal/archive"
	"github.com/MohammedAl-Alimi/recall/internal/model"
	"github.com/MohammedAl-Alimi/recall/internal/state"
)

// Events recall installs hooks for by default.
var DefaultEvents = []string{"SessionStart", "SessionEnd", "Notification"}

// LogFile is the hook error log under RecallDir.
const LogFile = "hook.log"

// maxStdin caps how much hook input is read.
const maxStdin = 4 << 20

// Payload is the subset of Claude's hook JSON recall cares about.
type Payload struct {
	SessionID        string `json:"session_id"`
	TranscriptPath   string `json:"transcript_path"`
	Cwd              string `json:"cwd"`
	HookEventName    string `json:"hook_event_name"`
	Source           string `json:"source"`
	Reason           string `json:"reason"`
	Message          string `json:"message"`
	Title            string `json:"title"`
	NotificationType string `json:"notification_type"`
	PermissionMode   string `json:"permission_mode"`
}

// Process-walk hooks, replaced in tests.
var (
	// startPID is the pid the SessionStart walk begins from.
	startPID = os.Getppid
	// psLookup returns the parent pid and the exact argv of pid. The
	// platform implementations (proc_*.go) read the NUL separated argument
	// vector, never whitespace split ps output, so a prompt such as
	// "explain the --add-dir /Users option" stays one token and can never
	// be replayed as flags.
	psLookup = procLookup
	// maxWalk bounds the ppid chain walk.
	maxWalk = 12
)

// Handle processes one Claude hook invocation. It never writes to stdout
// and always returns nil; problems are appended to RecallDir/hook.log.
func Handle(p model.Paths, event string, stdin io.Reader) error {
	if err := handle(p, event, stdin); err != nil {
		logErr(p, event, err)
	}
	return nil
}

func handle(p model.Paths, event string, stdin io.Reader) error {
	var raw []byte
	if stdin != nil {
		b, err := io.ReadAll(io.LimitReader(stdin, maxStdin))
		if err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
		raw = b
	}
	var pl Payload
	if len(strings.TrimSpace(string(raw))) > 0 {
		if err := json.Unmarshal(raw, &pl); err != nil {
			// Still record that the hook fired.
			_ = state.AppendEvent(p, map[string]any{"event": event, "error": "bad json: " + err.Error()})
			return fmt.Errorf("parse stdin: %w", err)
		}
	}
	if event == "" {
		event = pl.HookEventName
	}
	ev := map[string]any{
		"event": event,
		"sid":   pl.SessionID,
		"pid":   startPID(),
	}
	put := func(k, v string) {
		if v != "" {
			ev[k] = v
		}
	}
	put("cwd", pl.Cwd)
	put("transcript_path", pl.TranscriptPath)
	put("source", pl.Source)
	put("reason", pl.Reason)
	put("notification_type", pl.NotificationType)
	put("message", pl.Message)
	put("title", pl.Title)
	put("permission_mode", pl.PermissionMode)

	var errs []error
	switch event {
	case "SessionStart":
		if l := findLaunch(pl); l != nil {
			ev["argv"] = l.Argv
			if err := state.SaveLaunch(p, l); err != nil {
				errs = append(errs, fmt.Errorf("save launch: %w", err))
			}
		} else {
			ev["launch"] = "claude process not found"
		}
	case "SessionEnd":
		if pl.SessionID != "" && pl.TranscriptPath != "" {
			sess := &model.Session{ID: pl.SessionID, Path: pl.TranscriptPath}
			if dir, err := archive.Archive(p, sess, false); err != nil {
				errs = append(errs, fmt.Errorf("archive: %w", err))
			} else {
				ev["archive"] = dir
			}
		}
	case "Notification":
		if isPermissionPrompt(pl) {
			ev["waiting"] = true
			ev["waiting_for"] = firstNonEmpty(pl.Message, pl.Title, "permission")
		}
	}
	if err := state.AppendEvent(p, ev); err != nil {
		errs = append(errs, fmt.Errorf("append event: %w", err))
	}
	return errors.Join(errs...)
}

func isPermissionPrompt(pl Payload) bool {
	if pl.NotificationType == "permission_prompt" {
		return true
	}
	m := strings.ToLower(pl.Message + " " + pl.Title)
	return strings.Contains(m, "permission") || strings.Contains(m, "needs your")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// findLaunch walks the ppid chain from the hook's parent until it finds a
// claude process and returns a launch record built from its argv.
func findLaunch(pl Payload) *model.Launch {
	pid := startPID()
	for i := 0; i < maxWalk && pid > 1; i++ {
		ppid, args, err := psLookup(pid)
		if err != nil {
			return nil
		}
		if isClaudeArgv(args) {
			l := &model.Launch{
				SID:         pl.SessionID,
				Argv:        args,
				Cwd:         pl.Cwd,
				TermProgram: os.Getenv("TERM_PROGRAM"),
				RecordedBy:  "hook",
				At:          time.Now(),
			}
			if os.Getenv("TMUX") != "" {
				l.Mux = "tmux"
			} else if os.Getenv("STY") != "" {
				l.Mux = "screen"
			}
			return l
		}
		if ppid == pid || ppid <= 0 {
			return nil
		}
		pid = ppid
	}
	return nil
}

// isClaudeArgv reports whether args look like a claude CLI process: the
// executable (or the script run by node/bun) is named claude or is
// claude's cli.js.
func isClaudeArgv(args []string) bool {
	n := len(args)
	if n > 2 {
		n = 2
	}
	for i := 0; i < n; i++ {
		base := filepath.Base(args[i])
		if base == "claude" {
			return true
		}
		if base == "cli.js" && strings.Contains(args[i], "claude") {
			return true
		}
	}
	return false
}

func logErr(p model.Paths, event string, err error) {
	if p.RecallDir == "" {
		return
	}
	if mkErr := os.MkdirAll(p.RecallDir, 0o700); mkErr != nil {
		return
	}
	f, openErr := os.OpenFile(filepath.Join(p.RecallDir, LogFile), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if openErr != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s: %v\n", time.Now().UTC().Format(time.RFC3339), event, err)
}
