package launch

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

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
	// Cwd is used by PlanNew (and by Plan when sess is nil) as the directory
	// for a fresh session. Empty means the current working directory.
	Cwd string
	// Terminal selects where a new tab or window is opened: "" (auto from
	// TermProgram), "terminal" (Terminal.app), "iterm" (iTerm2) or "cmux".
	// A non-empty value implies NewTab; InPlace still wins.
	Terminal string
}

// Action is a planned, not yet executed, open operation.
type Action struct {
	// Kind is one of "focus", "attach", "resume", "new", "print".
	Kind string
	// SID is the session the action was planned for (empty for a new
	// session). Callers use it to release the session lock when a launch
	// fails or when the session was handed to another terminal tab.
	SID         string
	Cwd         string
	Argv        []string
	Script      string
	Description string
	Note        string
	// Terminal is TerminalCmux when Argv is a cmux CLI call that opens a
	// workspace; Run then starts the app if needed and runs Argv as a child
	// instead of exec'ing it in place. Empty for every other action.
	Terminal string
}

// Action kinds.
const (
	KindFocus  = "focus"
	KindAttach = "attach"
	KindResume = "resume"
	KindNew    = "new"
	KindPrint  = "print"
)

// Stdout receives dry-run and print output. Tests swap it for a buffer.
var Stdout io.Writer = os.Stdout

// Stderr receives the loss note printed right before a session is resumed,
// attached or started. Tests swap it for a buffer.
var Stderr io.Writer = os.Stderr

// DefaultMuxSocket is the tmux socket name used when a live session carries
// no MuxInfo socket. It mirrors mux.NewTmux().Socket.
const DefaultMuxSocket = "recall"

// contextWarnTokens is the context size above which a resume note warns.
const contextWarnTokens = 500_000

// Plan decides how to reopen sess.
//
// Tiers, in order:
//  1. ghost: cannot be opened.
//  2. live and kept in tmux: attach.
//  3. live in Terminal.app with a known tty: focus that tab.
//  4. live inside Cursor, VS Code, or any other host: print a hint.
//  5. closed: resume in place, or in a new tab when opts.NewTab is set.
//
// opts.Fork bypasses the live tiers because a fork never touches the
// running process. A nil sess plans a fresh session (see PlanNew).
func Plan(sess *model.Session, opts Options) (*Action, error) {
	if sess == nil {
		return PlanNew(opts)
	}
	if sess.Ghost {
		return nil, fmt.Errorf("session %s has no transcript (ghost); it cannot be opened", model.ShortID(sess.ID))
	}
	if sess.ID == "" {
		return nil, errors.New("session has no id")
	}
	if err := checkTerminal(opts.Terminal); err != nil {
		return nil, err
	}

	if sess.Live != nil && sess.Live.Alive && !opts.Fork {
		return planLive(sess, opts)
	}
	return planResume(sess, opts)
}

// PlanNew plans a brand new session in opts.Cwd (default: current dir).
func PlanNew(opts Options) (*Action, error) {
	if err := checkTerminal(opts.Terminal); err != nil {
		return nil, err
	}
	cwd := opts.Cwd
	if cwd == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("new session: %w", err)
		}
		cwd = wd
	}
	if !dirExists(cwd) {
		return nil, fmt.Errorf("new session: directory %s does not exist", cwd)
	}
	argv := []string{"claude"}
	if opts.Name != "" {
		argv = append(argv, "-n", opts.Name)
	}
	if opts.PermMode == "plan" {
		argv = append(argv, "--permission-mode", "plan")
	}
	extra, dropped := scrubBypass(opts.ExtraArgs)
	argv = append(argv, extra...)
	a := &Action{Kind: KindNew, Cwd: cwd, Argv: argv}
	if dropped {
		a.Note = "refused to pass bypassPermissions"
	}
	if opts.Keep {
		a.Argv = keptArgv("", cwd, argv)
		a.Kind = KindAttach
	}
	a.Description = "new session in " + cwd
	decorateSpawn(a, opts, cmuxTitle("", opts.Name, "", cwd))
	return a, nil
}

func planLive(sess *model.Session, opts Options) (*Action, error) {
	lv := sess.Live
	if lv.Mux != nil && lv.Mux.SessionName != "" {
		socket := lv.Mux.Socket
		if socket == "" {
			socket = DefaultMuxSocket
		}
		a := &Action{
			Kind:        KindAttach,
			SID:         sess.ID,
			Argv:        []string{"tmux", "-L", socket, "attach-session", "-t", lv.Mux.SessionName},
			Description: fmt.Sprintf("attach to kept session %s (tmux %s)", model.ShortID(sess.ID), lv.Mux.SessionName),
		}
		if lv.Mux.Attached {
			a.Note = "already attached elsewhere; this attaches a second client"
		}
		if opts.NewTab || opts.Terminal != "" {
			decorateSpawn(a, opts, cmuxTitle(sess.ID, sess.Label, sess.Title, ""))
		}
		return a, nil
	}

	host := normalizeHost(lv.HostApp)
	switch host {
	case "Terminal":
		if lv.TTY != "" {
			return &Action{
				Kind:        KindFocus,
				SID:         sess.ID,
				Script:      FocusTerminalScript(lv.TTY),
				Description: fmt.Sprintf("focus Terminal.app tab %s (pid %d)", ttyPath(lv.TTY), lv.PID),
			}, nil
		}
	case "iTerm2":
		if lv.TTY != "" {
			return &Action{
				Kind:        KindFocus,
				SID:         sess.ID,
				Script:      FocusITermScript(lv.TTY),
				Description: fmt.Sprintf("focus iTerm2 tab %s (pid %d)", ttyPath(lv.TTY), lv.PID),
			}, nil
		}
	}

	where := host
	if where == "" {
		where = "another terminal"
	}
	desc := fmt.Sprintf("session %s is running in %s (pid %d)", model.ShortID(sess.ID), where, lv.PID)
	if lv.TTY != "" {
		desc += " on " + ttyPath(lv.TTY)
	}
	hint := "switch to that window to continue"
	switch host {
	case "Cursor", "Code":
		hint = "open the " + host + " window and its terminal panel to continue; use fork to start a copy here"
	}
	_, argv, _ := BuildResume(sess, Options{Fork: true})
	return &Action{
		Kind:        KindPrint,
		SID:         sess.ID,
		Argv:        argv,
		Description: desc + "; " + hint,
		Note:        "a fork would resume a copy: " + strings.Join(quoteAll(argv), " "),
	}, nil
}

func planResume(sess *model.Session, opts Options) (*Action, error) {
	cwd, argv, note := BuildResume(sess, opts)
	a := &Action{
		Kind:        KindResume,
		SID:         sess.ID,
		Cwd:         cwd,
		Argv:        argv,
		Note:        note,
		Description: fmt.Sprintf("resume %s in %s", model.ShortID(sess.ID), cwd),
	}
	if opts.Fork {
		a.Description = fmt.Sprintf("fork %s in %s", model.ShortID(sess.ID), cwd)
	}
	if opts.Keep {
		a.Argv = keptArgv(sess.ID, cwd, argv)
		a.Kind = KindAttach
		a.Description += " (kept in tmux)"
	}
	decorateSpawn(a, opts, cmuxTitle(sess.ID, sess.Label, sess.Title, cwd))
	return a, nil
}

// decorateSpawn attaches the osascript or cmux call for a new terminal
// tab when one was asked for. The default is an in-place exec in the
// caller's terminal, so Enter in the list and 'recall open' resume right
// here; opts.NewTab (the o key, --new-tab) or an explicit opts.Terminal
// opens a Terminal.app tab, an iTerm2 tab or a cmux workspace instead.
// opts.InPlace is accepted for explicitness and wins over both.
//
// Terminal choice: opts.Terminal when set; otherwise cmux when recall runs
// inside cmux and its CLI is found, iTerm2 when TermProgram says so, and
// Terminal.app for everything else. title names the cmux workspace.
func decorateSpawn(a *Action, opts Options, title string) {
	if opts.InPlace || (!opts.NewTab && opts.Terminal == "") {
		return
	}
	term := opts.TermProgram
	if term == "" {
		term = os.Getenv("TERM_PROGRAM")
	}
	choice := strings.ToLower(opts.Terminal)
	if choice == "" {
		switch {
		case insideCmux(opts):
			choice = TerminalCmux
		case strings.HasPrefix(term, "iTerm"):
			choice = TerminalITerm
		default:
			choice = TerminalApp
		}
	}
	if choice == TerminalCmux {
		cli, ok := CmuxAvailable()
		if ok {
			decorateCmux(a, cli, title)
			return
		}
		if opts.Terminal != "" {
			// Asked for explicitly: keep the cmux shape so the dry run
			// and the error name the missing CLI instead of opening a
			// Terminal.app tab nobody asked for.
			decorateCmux(a, "cmux", title)
			return
		}
		// Auto-detected but no CLI: fall back to the AppleScript path.
		choice = TerminalApp
		if strings.HasPrefix(term, "iTerm") {
			choice = TerminalITerm
		}
	}
	cmd := strings.Join(quoteAll(a.Argv), " ")
	switch choice {
	case TerminalITerm:
		a.Script = NewITermTabScript(a.Cwd, cmd)
		a.Description += " (new iTerm2 tab)"
	default:
		a.Script = NewTerminalTabScript(a.Cwd, cmd)
		a.Description += " (new Terminal tab)"
	}
}

// keptArgv wraps a claude argv in a tmux new-session on the recall socket.
// -A attaches when the session already exists.
func keptArgv(sid, cwd string, claudeArgv []string) []string {
	short := model.ShortID(sid)
	if short == "" {
		short = fmt.Sprintf("%d", time.Now().Unix()%100000000)
	}
	envParts := []string{"env"}
	if sid != "" {
		envParts = append(envParts, "RECALL_SID="+sid)
	}
	envParts = append(envParts, "RECALL_BYPASS=1")
	inner := strings.Join(append(envParts, quoteAll(claudeArgv)...), " ")
	argv := []string{"tmux", "-L", DefaultMuxSocket, "new-session", "-A", "-s", "rc-" + short}
	if cwd != "" {
		argv = append(argv, "-c", cwd)
	}
	return append(argv, inner)
}

// Run executes a planned action. With RECALL_DRY_RUN=1 it only prints.
func Run(a *Action) error {
	if a == nil {
		return errors.New("launch: nil action")
	}
	if os.Getenv("RECALL_DRY_RUN") == "1" {
		fmt.Fprintf(Stdout, "DRY RUN: %s cwd=%s argv=%s\n", a.Kind, a.Cwd, strings.Join(quoteAll(a.Argv), " "))
		if a.Script != "" {
			fmt.Fprintf(Stdout, "DRY RUN: script:\n%s\n", a.Script)
		}
		if a.Note != "" {
			fmt.Fprintf(Stdout, "DRY RUN: note: %s\n", a.Note)
		}
		return nil
	}
	switch a.Kind {
	case KindPrint:
		fmt.Fprintln(Stdout, a.Description)
		if a.Note != "" {
			fmt.Fprintln(Stdout, a.Note)
		}
		return nil
	case KindFocus:
		if a.Script == "" {
			return errors.New("launch: focus action without script")
		}
		return runOsascript(a.Script)
	case KindAttach, KindResume, KindNew:
		PrintNote(Stderr, a)
		if a.Terminal == TerminalCmux {
			return runCmux(a)
		}
		if a.Script != "" {
			return runOsascript(a.Script)
		}
		return execInPlace(a)
	default:
		return fmt.Errorf("launch: unknown action kind %q", a.Kind)
	}
}

// PrintNote writes the loss note of a as "recall: <note>" to w so the user
// sees what will not be replayed before claude takes over the terminal. It
// prints nothing for a nil action, an empty note or a print action, whose
// note is part of its own output.
func PrintNote(w io.Writer, a *Action) {
	if w == nil || a == nil || a.Note == "" || a.Kind == KindPrint {
		return
	}
	fmt.Fprintln(w, "recall: "+a.Note)
}

// execInPlace replaces the current process with a.Argv after chdir to a.Cwd.
func execInPlace(a *Action) error {
	if len(a.Argv) == 0 {
		return errors.New("launch: empty argv")
	}
	bin, err := exec.LookPath(a.Argv[0])
	if err != nil {
		return fmt.Errorf("launch: %s not found on PATH: %w", a.Argv[0], err)
	}
	if a.Cwd != "" {
		if err := os.Chdir(a.Cwd); err != nil {
			return fmt.Errorf("launch: chdir %s: %w", a.Cwd, err)
		}
	}
	argv := append([]string{bin}, a.Argv[1:]...)
	return syscall.Exec(bin, argv, os.Environ())
}

func runOsascript(script string) error {
	cmd := exec.Command("osascript", "-")
	cmd.Stdin = strings.NewReader(script)
	cmd.Stdout = io.Discard
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("osascript: %s", msg)
		}
		return fmt.Errorf("osascript: %w", err)
	}
	return nil
}

// replayPathFlags take a path argument that must exist to be replayed.
var replayPathFlags = map[string]bool{
	"--add-dir":    true,
	"--mcp-config": true,
	"--settings":   true,
	"--plugin-dir": true,
}

// replayValueFlags take a plain value that is replayed verbatim.
var replayValueFlags = map[string]bool{
	"--model":  true,
	"--effort": true,
	"--agent":  true,
}

// skipValueFlags take a value and are never replayed.
var skipValueFlags = map[string]bool{
	"--resume": true, "-r": true,
	"--session-id":                true,
	"--permission-mode":           true,
	"-n":                          true,
	"--name":                      true,
	"--append-system-prompt":      true,
	"--system-prompt":             true,
	"--append-system-prompt-file": true,
	"--system-prompt-file":        true,
	"--allowedTools":              true,
	"--allowed-tools":             true,
	"--disallowedTools":           true,
	"--disallowed-tools":          true,
	"--tools":                     true,
	"--output-format":             true,
	"--input-format":              true,
	"--max-turns":                 true,
	"--max-budget-usd":            true,
	"--fallback-model":            true,
	"--agents":                    true,
	"--betas":                     true,
	"--worktree":                  true, "-w": true,
	"--from-pr":                true,
	"--permission-prompt-tool": true,
	"--json-schema":            true,
	"--attach":                 true,
	"--teleport":               true,
	"--remote":                 true,
	"--exec":                   true,
	"--session-name":           true,
	"--debug-file":             true,
	"--setting-sources":        true,
	"--plugin":                 true,
	"--pr":                     true,
	"--env":                    true,
}

// bypassArgs are never emitted.
var bypassArgs = map[string]bool{
	"--dangerously-skip-permissions":       true,
	"--allow-dangerously-skip-permissions": true,
}

// BuildResume returns the cwd, argv and a loss note for resuming sess.
//
// cwd preference: WorkCwd, then LastCwd, then Cwd, then $HOME; each must
// exist. argv is 'claude --resume <id>' plus fork, name and plan mode, then
// replayed launch flags whose path arguments still exist, then ExtraArgs.
// bypassPermissions is never emitted.
func BuildResume(sess *model.Session, opts Options) (cwd string, argv []string, note string) {
	var notes []string

	cwd, missing := pickCwd(sess)
	if missing != "" {
		notes = append(notes, fmt.Sprintf("directory %s is gone, using %s", missing, cwd))
	}

	argv = []string{"claude", "--resume", sess.ID}
	if opts.Fork {
		argv = append(argv, "--fork-session")
	}
	if opts.Name != "" {
		argv = append(argv, "-n", opts.Name)
	}
	mode := opts.PermMode
	if mode == "" {
		mode = sess.PermMode
	}
	switch mode {
	case "plan":
		argv = append(argv, "--permission-mode", "plan")
	case "bypassPermissions":
		notes = append(notes, "permission mode bypassPermissions is not replayed; approve permissions again")
	}

	if sess.Launch != nil && len(sess.Launch.Argv) > 0 {
		replayed, dropped, hasMCP := replayFlags(sess.Launch.Argv)
		argv = append(argv, replayed...)
		for _, d := range dropped {
			notes = append(notes, "dropped "+d)
		}
		if hasMCP {
			notes = append(notes, "MCP servers reconnect on resume")
		}
	}

	extra, droppedBypass := scrubBypass(opts.ExtraArgs)
	argv = append(argv, extra...)
	if droppedBypass {
		notes = append(notes, "refused to pass bypassPermissions")
	}

	if sess.DanglingTool != "" {
		notes = append(notes, fmt.Sprintf("interrupted tool call %s will not be completed", sess.DanglingTool))
	}
	if sess.BgJobsLost > 0 {
		notes = append(notes, fmt.Sprintf("%d background job(s) were lost", sess.BgJobsLost))
	}
	if sess.ContextTokens > contextWarnTokens {
		notes = append(notes, fmt.Sprintf("context is %s tokens; expect a slow first turn or a compaction", humanCount(sess.ContextTokens)))
	}
	return cwd, argv, strings.Join(notes, "; ")
}

// pickCwd returns the first existing directory in preference order and the
// first preferred directory that was missing (empty when none).
func pickCwd(sess *model.Session) (cwd string, missing string) {
	for _, c := range []string{sess.WorkCwd, sess.LastCwd, sess.Cwd} {
		if c == "" {
			continue
		}
		if dirExists(c) {
			return c, missing
		}
		if missing == "" {
			missing = c
		}
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "/"
	}
	return home, missing
}

// replayFlags extracts the allowlisted flags from a recorded argv. It drops
// path flags whose argument no longer exists, drops inline JSON given to
// --settings or --mcp-config, and reports whether an MCP config was
// involved. Each dropped entry is a short reason without the value when the
// value was inline JSON, which may carry permissions or secrets.
func replayFlags(orig []string) (out []string, dropped []string, hasMCP bool) {
	args := orig
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		// argv[0] is the binary (claude, node .../cli.js, ...).
		args = args[1:]
		// A node launcher carries the script as the next positional.
		if len(args) > 0 && strings.HasSuffix(args[0], ".js") {
			args = args[1:]
		}
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		flag, val, hasVal := strings.Cut(arg, "=")
		if !strings.HasPrefix(arg, "-") {
			continue // positional prompt, skipped
		}
		if !hasVal {
			flag = arg
		}
		switch {
		case replayPathFlags[flag]:
			if !hasVal {
				if i+1 >= len(args) {
					continue
				}
				i++
				val = args[i]
			}
			if flag == "--mcp-config" {
				hasMCP = true
			}
			if isInlineJSON(val) {
				dropped = append(dropped, flag+" (inline JSON is not replayed)")
				continue
			}
			if pathArgExists(flag, val) {
				out = append(out, flag, val)
			} else {
				dropped = append(dropped, flag+" "+val+" (path missing)")
			}
		case replayValueFlags[flag]:
			if !hasVal {
				if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
					continue
				}
				i++
				val = args[i]
			}
			out = append(out, flag, val)
		case skipValueFlags[flag]:
			if !hasVal && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
			}
		default:
			// boolean or unknown flag: not replayed
		}
	}
	return out, dropped, hasMCP
}

// isInlineJSON reports whether a --settings or --mcp-config value is a JSON
// document rather than a path. Inline settings can switch the permission
// mode to bypassPermissions and inline MCP config can carry env secrets, so
// neither is ever put back on a command line.
func isInlineJSON(val string) bool {
	t := strings.TrimSpace(val)
	return strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[")
}

// pathArgExists validates a path flag argument.
func pathArgExists(flag, val string) bool {
	if flag == "--add-dir" {
		// --add-dir accepts several comma separated dirs in some versions.
		for _, d := range strings.Split(val, ",") {
			if !dirExists(strings.TrimSpace(d)) {
				return false
			}
		}
		return true
	}
	_, err := os.Stat(val)
	return err == nil
}

// scrubBypass removes any bypassPermissions request from extra args.
func scrubBypass(extra []string) (out []string, dropped bool) {
	for i := 0; i < len(extra); i++ {
		a := extra[i]
		if bypassArgs[a] {
			dropped = true
			continue
		}
		if a == "--permission-mode" && i+1 < len(extra) && extra[i+1] == "bypassPermissions" {
			dropped = true
			i++
			continue
		}
		if a == "--permission-mode=bypassPermissions" {
			dropped = true
			continue
		}
		out = append(out, a)
	}
	return out, dropped
}

// FocusTerminalScript returns AppleScript that focuses the Terminal.app tab
// whose tty matches.
func FocusTerminalScript(tty string) string {
	dev := ttyPath(tty)
	return `tell application "Terminal"
	set found to false
	repeat with w in windows
		if found then exit repeat
		repeat with t in tabs of w
			if tty of t is ` + appleString(dev) + ` then
				set selected tab of w to t
				set frontmost of w to true
				set found to true
				exit repeat
			end if
		end repeat
	end repeat
	if found then activate
	return found
end tell
`
}

// FocusITermScript returns AppleScript that focuses the iTerm2 session whose
// tty matches.
func FocusITermScript(tty string) string {
	dev := ttyPath(tty)
	return `tell application "iTerm2"
	set found to false
	repeat with w in windows
		if found then exit repeat
		repeat with t in tabs of w
			if found then exit repeat
			repeat with s in sessions of t
				if tty of s is ` + appleString(dev) + ` then
					select t
					select s
					set index of w to 1
					set found to true
					exit repeat
				end if
			end repeat
		end repeat
	end repeat
	if found then activate
	return found
end tell
`
}

// NewTerminalTabScript returns AppleScript that opens a new Terminal.app tab
// in cwd and runs shellCommand.
//
// Terminal.app has no scripting verb for a new tab, so when a window exists
// the script remembers the tab count of the front window, sends Command-T
// through System Events and polls until that count grows. Only then does it
// run the command in the front window, which is now the new tab. When the
// keystroke was refused (no Accessibility permission) or no tab appeared in
// time, the script falls back to a plain 'do script', which opens a new
// window, so the command line is never typed into the tab that runs recall.
// Without any window a plain 'do script' opens a new one straight away.
func NewTerminalTabScript(cwd, shellCommand string) string {
	line := appleString(shellLine(cwd, shellCommand))
	return `tell application "Terminal"
	activate
	if (count of windows) is 0 then
		do script ` + line + `
	else
		set tabsBefore to count of tabs of front window
		try
			tell application "System Events" to keystroke "t" using command down
		end try
		set tabOpened to false
		repeat ` + terminalTabPolls + ` times
			delay ` + terminalTabPollDelay + `
			if (count of tabs of front window) > tabsBefore then
				set tabOpened to true
				exit repeat
			end if
		end repeat
		if tabOpened then
			do script ` + line + ` in front window
		else
			do script ` + line + `
		end if
	end if
end tell
`
}

// terminalTabPolls and terminalTabPollDelay bound how long the Terminal.app
// tab script waits for Command-T to take effect (20 x 0.1s = 2s).
const (
	terminalTabPolls     = "20"
	terminalTabPollDelay = "0.1"
)

// NewITermTabScript returns AppleScript that opens a new iTerm2 tab in cwd
// and runs shellCommand.
func NewITermTabScript(cwd, shellCommand string) string {
	line := appleString(shellLine(cwd, shellCommand))
	return `tell application "iTerm2"
	activate
	if (count of windows) is 0 then
		create window with default profile
	else
		tell current window to create tab with default profile
	end if
	tell current session of current window to write text ` + line + `
end tell
`
}

// shellLine builds 'cd <cwd> && <command>' with cwd quoted.
func shellLine(cwd, command string) string {
	if cwd == "" {
		return command
	}
	if command == "" {
		return "cd " + ShellQuote(cwd)
	}
	return "cd " + ShellQuote(cwd) + " && " + command
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

// ShellJoin quotes every element and joins with spaces.
func ShellJoin(argv []string) string {
	return strings.Join(quoteAll(argv), " ")
}

func quoteAll(argv []string) []string {
	out := make([]string, len(argv))
	for i, a := range argv {
		out[i] = ShellQuote(a)
	}
	return out
}

// appleString quotes s as an AppleScript string literal.
func appleString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// ttyPath normalizes "ttys003" and "/dev/ttys003" to the device path.
func ttyPath(tty string) string {
	tty = strings.TrimSpace(tty)
	if tty == "" {
		return ""
	}
	if strings.HasPrefix(tty, "/dev/") {
		return tty
	}
	return "/dev/" + tty
}

// normalizeHost maps the many spellings of a host app to a canonical name.
func normalizeHost(app string) string {
	a := strings.TrimSpace(app)
	switch strings.ToLower(strings.TrimSuffix(a, ".app")) {
	case "terminal", "apple_terminal":
		return "Terminal"
	case "iterm", "iterm2":
		return "iTerm2"
	case "cmux":
		return "cmux"
	case "cursor":
		return "Cursor"
	case "code", "vscode", "visual studio code", "code - insiders", "code-insiders":
		return "Code"
	case "":
		return ""
	}
	return filepath.Base(a)
}

func dirExists(p string) bool {
	if p == "" {
		return false
	}
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func humanCount(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%dk", n/1_000)
	}
	return fmt.Sprintf("%d", n)
}
