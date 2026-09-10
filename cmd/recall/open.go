package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/MohammedAl-Alimi/recall/internal/app"
	"github.com/MohammedAl-Alimi/recall/internal/launch"
	"github.com/MohammedAl-Alimi/recall/internal/model"
)

// findSession resolves ref against the loaded sessions. Resolution order:
// exact id, exact label (must be unique), id prefix (must be unique). Prefix
// matching is case-insensitive because session ids are lower-case hex.
func findSession(sessions []*model.Session, ref string) (*model.Session, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, usagef("empty session reference")
	}
	var byLabel, byPrefix []*model.Session
	lower := strings.ToLower(ref)
	for _, s := range sessions {
		if s.ID == ref {
			return s, nil
		}
		if s.Label != "" && s.Label == ref {
			byLabel = append(byLabel, s)
		}
		if strings.HasPrefix(strings.ToLower(s.ID), lower) {
			byPrefix = append(byPrefix, s)
		}
	}
	switch len(byLabel) {
	case 0:
	case 1:
		return byLabel[0], nil
	default:
		return nil, usagef("%d sessions carry the label %q: %s", len(byLabel), ref, describe(byLabel))
	}
	switch len(byPrefix) {
	case 0:
		return nil, usagef("no session matches %q", ref)
	case 1:
		return byPrefix[0], nil
	default:
		return nil, usagef("%d sessions match %q, be more specific: %s", len(byPrefix), ref, describe(byPrefix))
	}
}

func describe(list []*model.Session) string {
	parts := make([]string, 0, len(list))
	for i, s := range list {
		if i == 5 {
			parts = append(parts, "...")
			break
		}
		parts = append(parts, s.Short()+" ("+truncate(s.Title, 30)+")")
	}
	return strings.Join(parts, ", ")
}

// loadedApp builds the App and loads every session.
func loadedApp(includeGhosts bool) (*app.App, error) {
	a, err := newApp()
	if err != nil {
		return nil, err
	}
	if err := a.Load(context.Background(), includeGhosts); err != nil {
		return nil, err
	}
	return a, nil
}

// commandLine renders an action as the shell command a user could paste.
func commandLine(act *launch.Action) string {
	var b strings.Builder
	if act.Cwd != "" {
		b.WriteString("cd " + launch.ShellQuote(act.Cwd) + " && ")
	}
	quoted := make([]string, 0, len(act.Argv))
	for _, arg := range act.Argv {
		quoted = append(quoted, launch.ShellQuote(arg))
	}
	b.WriteString(strings.Join(quoted, " "))
	return b.String()
}

// printAction prints what an action would do, used for --dry-run and
// RECALL_DRY_RUN=1 without executing anything.
func printAction(w io.Writer, act *launch.Action) {
	if act.Description != "" {
		fmt.Fprintln(w, act.Description)
	}
	if len(act.Argv) > 0 {
		fmt.Fprintln(w, commandLine(act))
	}
	if act.Script != "" {
		fmt.Fprintln(w, "osascript:")
		fmt.Fprintln(w, act.Script)
	}
	if act.Note != "" {
		fmt.Fprintln(w, "note:", act.Note)
	}
}

// runChild runs act.Argv in act.Cwd as a child process with inherited stdio
// and returns its exit code. Used by the widget loop, where recall must
// regain control after claude exits.
func runChild(act *launch.Action, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	if len(act.Argv) == 0 {
		return 0, fmt.Errorf("nothing to run")
	}
	bin, err := exec.LookPath(act.Argv[0])
	if err != nil {
		return 127, err
	}
	c := exec.Command(bin, act.Argv[1:]...)
	c.Dir = act.Cwd
	c.Stdin, c.Stdout, c.Stderr = stdin, stdout, stderr
	c.Env = os.Environ()
	if err := c.Run(); err != nil {
		var ee *exec.ExitError
		if ok := asExitError(err, &ee); ok {
			return ee.ExitCode(), nil
		}
		return 1, err
	}
	return 0, nil
}

func asExitError(err error, target **exec.ExitError) bool {
	ee, ok := err.(*exec.ExitError)
	if ok {
		*target = ee
	}
	return ok
}

// execInPlace replaces the current process with act.Argv after chdir.
func execInPlace(act *launch.Action) error {
	if len(act.Argv) == 0 {
		return fmt.Errorf("nothing to exec")
	}
	bin, err := exec.LookPath(act.Argv[0])
	if err != nil {
		return err
	}
	if act.Cwd != "" {
		if err := os.Chdir(act.Cwd); err != nil {
			return err
		}
	}
	return syscall.Exec(bin, act.Argv, os.Environ())
}

// runAction executes a planned action. Actions that carry a script or that
// only focus or print are delegated to launch.Run; a plain resume/new/attach
// argv is exec'ed in place so the shell ends up running claude directly.
// The loss note (dropped flags, lost background jobs, a missing directory)
// is printed to stderr before claude takes over; launch.Run prints it on
// its own path.
func runAction(act *launch.Action, w io.Writer) error {
	if dryRun() {
		printAction(w, act)
		return nil
	}
	switch act.Kind {
	case "resume", "new", "attach":
		if act.Script == "" && len(act.Argv) > 0 {
			launch.PrintNote(os.Stderr, act)
			return execInPlace(act)
		}
	}
	return launch.Run(act)
}

// runHolding runs act for an App that may hold session lock files. The App
// (and the *os.File in its lock map) is otherwise unreachable once Open
// returned, and the os.File finalizer would close the descriptor if a GC
// ran between Open and exec. KeepAlive pins it until the process has been
// replaced or the child has exited.
func runHolding(a *app.App, act *launch.Action, w io.Writer) error {
	err := runAction(act, w)
	runtime.KeepAlive(a)
	return err
}

// runChildHolding is runChild with the same lock-pinning guarantee as
// runHolding, for the widget loop where claude runs as a child process and
// inherits the lock descriptor.
func runChildHolding(a *app.App, act *launch.Action, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	code, err := runChild(act, stdin, stdout, stderr)
	runtime.KeepAlive(a)
	return code, err
}

func newOpenCmd() *cobra.Command {
	var (
		opts     launch.Options
		name     string
		permMode string
	)
	cmd := &cobra.Command{
		Use:   "open <sid|prefix|label>",
		Short: "Reopen a session (focus, attach or resume)",
		Long: `open reopens one session. A live session in Terminal.app is focused, a kept
tmux session is attached, and a closed session is resumed with
'claude --resume'. The argument is a full session id, a unique id prefix,
or a label set with 'r' in the list.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadedApp(false)
			if err != nil {
				return err
			}
			sess, err := findSession(a.Sessions, args[0])
			if err != nil {
				return err
			}
			opts.TermProgram = os.Getenv("TERM_PROGRAM")
			opts.Name = name
			opts.PermMode = permMode
			if dryRun() {
				opts.DryRun = true
			}
			act, err := a.Open(sess, opts)
			if err != nil {
				return err
			}
			if opts.DryRun {
				printAction(cmd.OutOrStdout(), act)
				return nil
			}
			return runHolding(a, act, cmd.OutOrStdout())
		},
	}
	cmd.Flags().BoolVar(&opts.NewTab, "new-tab", false, "open in a new Terminal.app or iTerm2 tab instead of this terminal")
	cmd.Flags().BoolVar(&opts.Keep, "keep", false, "run inside a kept tmux session that survives the tab")
	cmd.Flags().BoolVar(&opts.Fork, "fork", false, "fork the session (--fork-session) instead of resuming it")
	cmd.Flags().BoolVar(&opts.InPlace, "in-place", false, "replace the current shell with claude (the default; wins over --new-tab)")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "print the command instead of running it")
	cmd.Flags().StringVarP(&name, "name", "n", "", "name for the resumed session (claude -n)")
	cmd.Flags().StringVar(&permMode, "permission-mode", "", "permission mode for the resumed session")
	return cmd
}
