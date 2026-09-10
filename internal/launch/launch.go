package launch

import (
	"errors"
	"strings"

	"github.com/MohammedAl-Alimi/recall/internal/model"
)

// Options controls how a session is opened.
type Options struct {
	NewTab      bool
	Keep        bool
	Fork        bool
	InPlace     bool
	DryRun      bool
	Name        string
	PermMode    string
	ExtraArgs   []string
	TermProgram string
}

// Action is a planned, not yet executed, open operation.
type Action struct {
	// Kind is one of "focus", "attach", "resume", "new", "print".
	Kind        string
	Cwd         string
	Argv        []string
	Script      string
	Description string
	Note        string
}

// Plan decides how to reopen sess.
func Plan(sess *model.Session, opts Options) (*Action, error) {
	return nil, errors.New("not implemented: launch.Plan")
}

// Run executes a planned action. With RECALL_DRY_RUN=1 it only prints.
func Run(a *Action) error {
	return errors.New("not implemented: launch.Run")
}

// BuildResume returns the cwd, argv and a loss note for resuming sess.
func BuildResume(sess *model.Session, opts Options) (cwd string, argv []string, note string) {
	return "", nil, ""
}

// FocusTerminalScript returns AppleScript that focuses the Terminal.app tab
// whose tty matches.
func FocusTerminalScript(tty string) string {
	return ""
}

// NewTerminalTabScript returns AppleScript that opens a new Terminal.app tab
// in cwd and runs shellCommand.
func NewTerminalTabScript(cwd, shellCommand string) string {
	return ""
}

// ShellQuote quotes s for POSIX sh using single quotes.
func ShellQuote(s string) string {
	if s == "" {
		return "''"
	}
	safe := true
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_./:=@+,", r)) {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
