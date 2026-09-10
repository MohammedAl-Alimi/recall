// Command recall is a terminal session manager for the Claude Code CLI.
//
// It catalogs every session under the Claude projects directory, shows
// title, outcome and state, and reopens a session by focusing its terminal
// tab, attaching a kept tmux session, or running 'claude --resume <uuid>'.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/debug"

	"github.com/spf13/cobra"

	"github.com/MohammedAl-Alimi/recall/internal/app"
	"github.com/MohammedAl-Alimi/recall/internal/model"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

// Exit codes. 1 is a general failure, 2 a usage or lookup error (unknown
// session, ambiguous prefix, bad arguments). 'recall exec' passes the exit
// code of the child through unchanged.
const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

var (
	flagClaudeDir string
	flagRecallDir string
)

// exitError carries a specific process exit code out of a command.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string {
	if e.err == nil {
		return fmt.Sprintf("exit %d", e.code)
	}
	return e.err.Error()
}

func (e *exitError) Unwrap() error { return e.err }

// usageErr wraps err so that main exits with exitUsage.
func usageErr(err error) error { return &exitError{code: exitUsage, err: err} }

// usagef formats a usage error.
func usagef(format string, args ...any) error {
	return usageErr(fmt.Errorf(format, args...))
}

// paths resolves the Claude and recall directories from flags and env.
func paths() model.Paths {
	p := model.DefaultPaths()
	if flagClaudeDir != "" || flagRecallDir != "" {
		claude, recall := p.ClaudeDir, p.RecallDir
		if flagClaudeDir != "" {
			claude = flagClaudeDir
		}
		if flagRecallDir != "" {
			recall = flagRecallDir
		}
		np := model.PathsFrom(claude, recall)
		if v := os.Getenv("RECALL_PROJECTS_DIR"); v != "" && flagClaudeDir == "" {
			np.ProjectsDir = v
		}
		p = np
	}
	return p
}

func newApp() (*app.App, error) {
	a, err := app.New(paths())
	if err != nil {
		return nil, err
	}
	if cwd, err := os.Getwd(); err == nil {
		a.ShellCwd = cwd
	}
	return a, nil
}

// dryRun reports whether RECALL_DRY_RUN=1 is set. In dry-run mode nothing is
// executed, focused or attached; the exact command is printed instead.
func dryRun() bool { return os.Getenv("RECALL_DRY_RUN") == "1" }

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// run executes the CLI and returns the process exit code.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	root := newRoot()
	root.SetArgs(args)
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)
	err := root.Execute()
	if err == nil {
		return exitOK
	}
	var ee *exitError
	if errors.As(err, &ee) {
		if ee.err != nil {
			fmt.Fprintln(stderr, "recall:", ee.err)
		}
		return ee.code
	}
	fmt.Fprintln(stderr, "recall:", err)
	return exitFailure
}

func newRoot() *cobra.Command {
	var (
		fromWidget bool
		query      string
	)
	root := &cobra.Command{
		Use:   "recall",
		Short: "Terminal session manager for Claude Code",
		Long: `recall catalogs every Claude Code CLI session and reopens it by focusing
its terminal tab, attaching a kept tmux session, or resuming it.

Run it without arguments for the interactive list. Every subcommand accepts
--claude-dir and --recall-dir; RECALL_DRY_RUN=1 prints commands instead of
running them.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUI(cmd, fromWidget, query)
		},
	}
	root.PersistentFlags().StringVar(&flagClaudeDir, "claude-dir", "", "Claude config directory (default $CLAUDE_CONFIG_DIR or ~/.claude)")
	root.PersistentFlags().StringVar(&flagRecallDir, "recall-dir", "", "recall state directory (default $RECALL_DIR or ~/.recall)")
	root.Flags().BoolVar(&fromWidget, "from-widget", false, "started from the shell widget (Ctrl-G); reopen the list after claude exits")
	root.Flags().StringVarP(&query, "query", "q", "", "initial search query for the list")

	root.AddCommand(
		newLsCmd(),
		newOpenCmd(),
		newNewCmd(),
		newExecCmd(),
		newHookCmd(),
		newArchiveCmd(),
		newRestoreCmd(),
		newDoctorCmd(),
		newSetupCmd(),
		newUninstallCmd(),
		newShellCmd(),
		newIndexCmd(),
		newVersionCmd(),
	)
	return root
}

func newVersionCmd() *cobra.Command {
	var short bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the recall version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			w := cmd.OutOrStdout()
			if short {
				fmt.Fprintln(w, version)
				return
			}
			fmt.Fprintf(w, "recall %s\n", version)
			fmt.Fprintf(w, "  go       %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
			if rev, dirty := vcsInfo(); rev != "" {
				fmt.Fprintf(w, "  commit   %s%s\n", rev, dirty)
			}
		},
	}
	cmd.Flags().BoolVar(&short, "short", false, "print only the version string")
	return cmd
}

// vcsInfo returns the VCS revision embedded by the Go toolchain, if any.
func vcsInfo() (rev string, dirty string) {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "", ""
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			if len(s.Value) > 12 {
				rev = s.Value[:12]
			} else {
				rev = s.Value
			}
		case "vcs.modified":
			if s.Value == "true" {
				dirty = " (modified)"
			}
		}
	}
	return rev, dirty
}
