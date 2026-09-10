// Command recall is a terminal session manager for the Claude Code CLI.
//
// It catalogs every session under the Claude projects directory, shows
// title, outcome and state, and reopens a session by focusing its terminal
// tab, attaching a kept tmux session, or running 'claude --resume <uuid>'.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/MohammedAl-Alimi/recall/internal/app"
	"github.com/MohammedAl-Alimi/recall/internal/archive"
	"github.com/MohammedAl-Alimi/recall/internal/doctor"
	"github.com/MohammedAl-Alimi/recall/internal/hook"
	"github.com/MohammedAl-Alimi/recall/internal/launch"
	"github.com/MohammedAl-Alimi/recall/internal/model"
	"github.com/MohammedAl-Alimi/recall/internal/shell"
	"github.com/MohammedAl-Alimi/recall/internal/ui"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

var (
	flagClaudeDir string
	flagRecallDir string
)

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

func main() {
	root := newRoot()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "recall:", err)
		os.Exit(1)
	}
}

func newRoot() *cobra.Command {
	var fromWidget bool
	root := &cobra.Command{
		Use:           "recall",
		Short:         "Terminal session manager for Claude Code",
		Long:          "recall catalogs every Claude Code CLI session and reopens it by focusing its terminal tab, attaching a kept tmux session, or resuming it.",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newApp()
			if err != nil {
				return err
			}
			return ui.Run(a, ui.RunOptions{FromWidget: fromWidget})
		},
	}
	root.PersistentFlags().StringVar(&flagClaudeDir, "claude-dir", "", "Claude config directory (default $CLAUDE_CONFIG_DIR or ~/.claude)")
	root.PersistentFlags().StringVar(&flagRecallDir, "recall-dir", "", "recall state directory (default $RECALL_DIR or ~/.recall)")
	root.Flags().BoolVar(&fromWidget, "from-widget", false, "started from the shell widget (Ctrl-G)")

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

func newLsCmd() *cobra.Command {
	var (
		asJSON, all, onlyLive, ghosts, headless bool
		project                                 string
	)
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List sessions",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newApp()
			if err != nil {
				return err
			}
			if err := a.Load(context.Background(), ghosts); err != nil {
				return err
			}
			list := a.Visible(all, ghosts, headless)
			if project != "" {
				var out []*model.Session
				for _, s := range list {
					if strings.Contains(s.WorkCwd, project) || strings.Contains(s.Cwd, project) {
						out = append(out, s)
					}
				}
				list = out
			}
			if onlyLive {
				var out []*model.Session
				for _, s := range list {
					if s.State.IsLive() {
						out = append(out, s)
					}
				}
				list = out
			}
			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(list)
			}
			for _, s := range list {
				fmt.Printf("%s  %-9s  %s\n", s.Short(), s.State.Word(), s.Title)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	cmd.Flags().BoolVar(&all, "all", false, "include hidden sessions")
	cmd.Flags().BoolVar(&onlyLive, "live", false, "only live sessions")
	cmd.Flags().BoolVar(&ghosts, "ghosts", false, "include ghost sessions")
	cmd.Flags().BoolVar(&headless, "headless", false, "include headless sessions")
	cmd.Flags().StringVar(&project, "project", "", "filter by project path substring")
	return cmd
}

func findSession(a *app.App, ref string) (*model.Session, error) {
	var matches []*model.Session
	for _, s := range a.Sessions {
		if s.ID == ref || s.Label == ref {
			return s, nil
		}
		if strings.HasPrefix(s.ID, ref) {
			matches = append(matches, s)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("no session matches %q", ref)
	case 1:
		return matches[0], nil
	default:
		return nil, fmt.Errorf("%d sessions match %q, be more specific", len(matches), ref)
	}
}

func newOpenCmd() *cobra.Command {
	var opts launch.Options
	cmd := &cobra.Command{
		Use:   "open <sid|prefix|label>",
		Short: "Reopen a session (focus, attach or resume)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newApp()
			if err != nil {
				return err
			}
			if err := a.Load(context.Background(), false); err != nil {
				return err
			}
			sess, err := findSession(a, args[0])
			if err != nil {
				return err
			}
			opts.TermProgram = os.Getenv("TERM_PROGRAM")
			if os.Getenv("RECALL_DRY_RUN") == "1" {
				opts.DryRun = true
			}
			act, err := a.Open(sess, opts)
			if err != nil {
				return err
			}
			return launch.Run(act)
		},
	}
	cmd.Flags().BoolVar(&opts.NewTab, "new-tab", false, "open in a new terminal tab")
	cmd.Flags().BoolVar(&opts.Keep, "keep", false, "run inside a kept tmux session")
	cmd.Flags().BoolVar(&opts.Fork, "fork", false, "fork the session instead of resuming it")
	cmd.Flags().BoolVar(&opts.InPlace, "in-place", false, "replace the current shell with claude")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "print the command instead of running it")
	return cmd
}

func newNewCmd() *cobra.Command {
	var (
		name, cwd string
		keep      bool
	)
	cmd := &cobra.Command{
		Use:   "new [-n name] [--keep] [--cwd dir] [-- claude args]",
		Short: "Start a new session here",
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("not implemented: recall new")
		},
	}
	cmd.Flags().StringVarP(&name, "name", "n", "", "session name")
	cmd.Flags().BoolVar(&keep, "keep", false, "run inside a kept tmux session")
	cmd.Flags().StringVar(&cwd, "cwd", "", "working directory")
	return cmd
}

func newExecCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "exec <sid> -- <cmd>",
		Short: "Run a command and record its exit code for a kept session",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("not implemented: recall exec")
		},
	}
}

func newHookCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "hook <event>",
		Short:  "Claude Code hook entry point (reads JSON on stdin)",
		Args:   cobra.ExactArgs(1),
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Hooks must never break a Claude session: always exit 0, stdout stays empty.
			if err := hook.Handle(paths(), args[0], os.Stdin); err != nil {
				fmt.Fprintln(os.Stderr, "recall hook:", err)
			}
			return nil
		},
	}
}

func newArchiveCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "archive <sid>|--all",
		Short: "Archive a transcript under the recall directory",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !all && len(args) != 1 {
				return errors.New("archive: pass a session id or --all")
			}
			a, err := newApp()
			if err != nil {
				return err
			}
			if err := a.Load(context.Background(), false); err != nil {
				return err
			}
			var targets []*model.Session
			if all {
				targets = a.Sessions
			} else {
				s, err := findSession(a, args[0])
				if err != nil {
					return err
				}
				targets = []*model.Session{s}
			}
			for _, s := range targets {
				dir, err := archive.Archive(a.Paths, s, false)
				if err != nil {
					return err
				}
				fmt.Println(dir)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "archive every session")
	return cmd
}

func newRestoreCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restore <sid>",
		Short: "Restore an archived transcript so it can be resumed",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newApp()
			if err != nil {
				return err
			}
			if err := a.Load(context.Background(), false); err != nil {
				return err
			}
			s, err := findSession(a, args[0])
			if err != nil {
				return err
			}
			path, err := archive.Restore(a.Paths, s)
			if err != nil {
				return err
			}
			fmt.Println(path)
			return nil
		},
	}
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check the environment",
		RunE: func(cmd *cobra.Command, args []string) error {
			checks, err := doctor.Run(paths())
			if err != nil {
				return err
			}
			failed := false
			for _, c := range checks {
				fmt.Printf("%-4s %-28s %s\n", c.Status, c.Name, c.Detail)
				if c.Status == "fail" {
					failed = true
				}
			}
			if failed {
				return errors.New("doctor: some checks failed")
			}
			return nil
		},
	}
}

func newSetupCmd() *cobra.Command {
	var (
		retention          int
		widget, hooks, yes bool
	)
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Configure retention, shell widget and hooks (each step skippable)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("not implemented: recall setup")
		},
	}
	cmd.Flags().IntVar(&retention, "retention", 0, "set cleanupPeriodDays in settings.json")
	cmd.Flags().BoolVar(&widget, "widget", false, "install the shell widget")
	cmd.Flags().BoolVar(&hooks, "hooks", false, "install Claude hooks")
	cmd.Flags().BoolVar(&yes, "yes", false, "run non-interactively, accepting defaults")
	return cmd
}

func newUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove hooks and shell widget installed by setup",
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("not implemented: recall uninstall")
		},
	}
}

func newShellCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "shell",
		Short: "Manage the shell widget",
	}
	var shellName string
	install := &cobra.Command{
		Use:   "install",
		Short: "Install the Ctrl-G widget into the shell rc file",
		RunE: func(cmd *cobra.Command, args []string) error {
			sh, rc := shell.Detect()
			if shellName != "" {
				sh = shellName
			}
			return shell.Install(sh, rc)
		},
	}
	uninstall := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the widget from the shell rc file",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, rc := shell.Detect()
			return shell.Uninstall(rc)
		},
	}
	print := &cobra.Command{
		Use:   "print",
		Short: "Print the widget snippet",
		RunE: func(cmd *cobra.Command, args []string) error {
			sh, _ := shell.Detect()
			if shellName != "" {
				sh = shellName
			}
			fmt.Print(shell.Widget(sh))
			return nil
		},
	}
	cmd.PersistentFlags().StringVar(&shellName, "shell", "", "shell name (zsh, bash, fish)")
	cmd.AddCommand(install, uninstall, print)
	return cmd
}

func newIndexCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "index",
		Short: "Rebuild the session index (placeholder)",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("not yet")
			return nil
		},
	}
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the recall version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("recall", version)
		},
	}
}
