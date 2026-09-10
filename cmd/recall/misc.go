package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/MohammedAl-Alimi/recall/internal/archive"
	"github.com/MohammedAl-Alimi/recall/internal/doctor"
	"github.com/MohammedAl-Alimi/recall/internal/hook"
	"github.com/MohammedAl-Alimi/recall/internal/model"
	"github.com/MohammedAl-Alimi/recall/internal/shell"
)

func newHookCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "hook <event>",
		Short:  "Claude Code hook entry point (reads JSON on stdin)",
		Args:   cobra.ExactArgs(1),
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Hooks must never break a Claude session: always exit 0 and keep
			// stdout empty, because Claude parses stdout of a hook.
			if err := hook.Handle(paths(), args[0], cmd.InOrStdin()); err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "recall hook:", err)
			}
			return nil
		},
	}
}

func newArchiveCmd() *cobra.Command {
	var (
		all      bool
		sidecars bool
	)
	cmd := &cobra.Command{
		Use:   "archive <sid>|--all",
		Short: "Archive a transcript under the recall directory",
		Long: `archive hard-links (or copies) a transcript into ~/.recall/archive/<sid>/ so
it outlives Claude's cleanupPeriodDays deletion. With --sidecars the
tool-results and file-history directories are copied too.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !all && len(args) != 1 {
				return usagef("archive: pass a session id or --all")
			}
			if all && len(args) != 0 {
				return usagef("archive: --all takes no session id")
			}
			a, err := loadedApp(false)
			if err != nil {
				return err
			}
			var targets []*model.Session
			if all {
				for _, s := range a.Sessions {
					if s.Ghost || s.Path == "" {
						continue
					}
					targets = append(targets, s)
				}
			} else {
				s, err := findSession(a.Sessions, args[0])
				if err != nil {
					return err
				}
				if s.Ghost || s.Path == "" {
					return usagef("archive: %s has no transcript to archive", s.Short())
				}
				targets = []*model.Session{s}
			}
			out := cmd.OutOrStdout()
			failed := 0
			for _, s := range targets {
				dir, err := archive.Archive(a.Paths, s, sidecars)
				if err != nil {
					failed++
					fmt.Fprintf(cmd.ErrOrStderr(), "recall: archive %s: %v\n", s.Short(), err)
					continue
				}
				fmt.Fprintf(out, "%s  %s\n", s.Short(), dir)
			}
			if failed > 0 {
				return fmt.Errorf("archive: %d of %d failed", failed, len(targets))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "archive every session with a transcript")
	cmd.Flags().BoolVar(&sidecars, "sidecars", false, "also copy tool-results and file-history directories")
	return cmd
}

func newRestoreCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restore <sid>",
		Short: "Restore an archived transcript so it can be resumed",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadedApp(true)
			if err != nil {
				return err
			}
			s, err := findSession(a.Sessions, args[0])
			if err != nil {
				// A restore target may be gone from the list entirely; try the
				// archive directly with the id as given.
				if _, ok := archive.HasArchive(a.Paths, args[0]); !ok {
					return err
				}
				s = &model.Session{ID: args[0]}
			}
			path, err := archive.Restore(a.Paths, s)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), path)
			return nil
		},
	}
}

// writeChecks prints doctor checks as an aligned table.
func writeChecks(w io.Writer, checks []doctor.Check) (failed int) {
	for _, c := range checks {
		mark := "ok  "
		switch c.Status {
		case "warn":
			mark = "warn"
		case "fail":
			mark = "FAIL"
			failed++
		}
		fmt.Fprintf(w, "%s  %-28s %s\n", mark, c.Name, strings.ReplaceAll(c.Detail, "\n", "\n      "+strings.Repeat(" ", 28)))
	}
	return failed
}

func newDoctorCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check the environment",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			checks, err := doctor.Run(paths())
			if err != nil {
				return err
			}
			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				if checks == nil {
					checks = []doctor.Check{}
				}
				return enc.Encode(checks)
			}
			if failed := writeChecks(cmd.OutOrStdout(), checks); failed > 0 {
				return fmt.Errorf("doctor: %d check(s) failed", failed)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print checks as JSON")
	return cmd
}

func newShellCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "shell",
		Short: "Manage the shell widget (Ctrl-G opens recall)",
	}
	var (
		shellName string
		rcPath    string
	)
	resolve := func() (string, string, error) {
		sh, rc := shell.Detect()
		if shellName != "" {
			sh = shellName
		}
		if rcPath != "" {
			rc = rcPath
		}
		switch sh {
		case "zsh", "bash", "fish":
		case "":
			return "", "", usagef("could not detect the shell; pass --shell zsh|bash|fish")
		default:
			return "", "", usagef("unsupported shell %q (zsh, bash or fish)", sh)
		}
		return sh, rc, nil
	}
	install := &cobra.Command{
		Use:   "install",
		Short: "Install the Ctrl-G widget into the shell rc file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sh, rc, err := resolve()
			if err != nil {
				return err
			}
			if rc == "" {
				return usagef("no rc file known for %s; pass --rc", sh)
			}
			if err := shell.Install(sh, rc); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "installed recall widget for %s into %s (open a new shell, then press Ctrl-G)\n", sh, rc)
			return nil
		},
	}
	uninstall := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the widget from the shell rc file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, rc, err := resolve()
			if err != nil {
				return err
			}
			if err := shell.Uninstall(rc); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed recall widget from %s\n", rc)
			return nil
		},
	}
	print := &cobra.Command{
		Use:   "print",
		Short: "Print the widget snippet",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sh, _, err := resolve()
			if err != nil {
				return err
			}
			w := shell.Widget(sh)
			if w == "" {
				return errors.New("no widget available for " + sh)
			}
			fmt.Fprint(cmd.OutOrStdout(), w)
			if !strings.HasSuffix(w, "\n") {
				fmt.Fprintln(cmd.OutOrStdout())
			}
			return nil
		},
	}
	cmd.PersistentFlags().StringVar(&shellName, "shell", "", "shell name (zsh, bash, fish); default: detected from $SHELL")
	cmd.PersistentFlags().StringVar(&rcPath, "rc", "", "rc file to edit (default: detected)")
	cmd.AddCommand(install, uninstall, print)
	return cmd
}

// newIndexCmd reserves the 'index' name for the full-text session index.
// The command is hidden until the index exists, so it does not appear in
// --help, and it fails clearly instead of pretending to have worked.
func newIndexCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "index",
		Short:  "Rebuild the full-text session index",
		Args:   cobra.NoArgs,
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("index: the full-text session index is not available in this version")
		},
	}
}

// binPath returns the absolute path of the running recall binary, used when
// writing hook commands into settings.json.
func binPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "recall"
	}
	return exe
}
