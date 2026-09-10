package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/MohammedAl-Alimi/recall/internal/archive"
	"github.com/MohammedAl-Alimi/recall/internal/hook"
	"github.com/MohammedAl-Alimi/recall/internal/model"
	"github.com/MohammedAl-Alimi/recall/internal/shell"
)

// defaultRetentionDays is what setup proposes for cleanupPeriodDays when the
// user has not set one: ten years, which is effectively never. Claude's own
// default is 30 days. 'recall doctor' suggests the same number.
const defaultRetentionDays = 3650

// claudeDefaultRetention is the value Claude Code uses when the key is unset.
const claudeDefaultRetention = 30

// hookEvents are the Claude hook events recall subscribes to.
var hookEvents = []string{"SessionStart", "SessionEnd", "Notification"}

// setupStep is one skippable unit of work in 'recall setup'.
type setupStep struct {
	Name string
	// Describe returns what the step will change, or "" when nothing needs
	// doing.
	Describe func() string
	Apply    func() error
}

// confirm asks "question [y/N] " on w and reads one line from r. With yes it
// returns true without asking. EOF or anything but y/yes counts as no.
func confirm(r *bufio.Reader, w io.Writer, question string, yes bool) bool {
	if yes {
		fmt.Fprintf(w, "%s [y/N] y\n", question)
		return true
	}
	fmt.Fprintf(w, "%s [y/N] ", question)
	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		fmt.Fprintln(w)
		return false
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	return false
}

// retentionStep describes the change to cleanupPeriodDays.
func retentionStep(p model.Paths, days int) setupStep {
	return setupStep{
		Name: "retention",
		Describe: func() string {
			cur, set, err := archive.Retention(p)
			switch {
			case err != nil:
				return fmt.Sprintf("set cleanupPeriodDays=%d in %s (current value unreadable: %v)", days, p.SettingsFile, err)
			case !set:
				return fmt.Sprintf("set cleanupPeriodDays=%d in %s (currently unset, so Claude deletes transcripts after %d days)", days, p.SettingsFile, claudeDefaultRetention)
			case cur == days:
				return ""
			default:
				return fmt.Sprintf("change cleanupPeriodDays from %d to %d in %s", cur, days, p.SettingsFile)
			}
		},
		Apply: func() error { return archive.SetRetention(p, days) },
	}
}

// widgetStep describes installing the Ctrl-G widget.
func widgetStep(sh, rc string) setupStep {
	return setupStep{
		Name: "widget",
		Describe: func() string {
			if sh == "" || rc == "" {
				return ""
			}
			if data, err := os.ReadFile(rc); err == nil && strings.Contains(string(data), shell.BeginMarker) {
				return fmt.Sprintf("refresh the recall block (%s ... %s) in %s", shell.BeginMarker, shell.EndMarker, rc)
			}
			return fmt.Sprintf("append a %s block to %s binding Ctrl-G to recall (%s)", shell.BeginMarker, rc, sh)
		},
		Apply: func() error { return shell.Install(sh, rc) },
	}
}

// hooksStep describes installing the Claude hooks.
func hooksStep(p model.Paths, bin string) setupStep {
	return setupStep{
		Name: "hooks",
		Describe: func() string {
			return fmt.Sprintf("add %s hooks running '%s hook <event>' to %s (a .bak copy is kept)", strings.Join(hookEvents, ", "), bin, p.SettingsFile)
		},
		Apply: func() error { return hook.InstallSettings(p, hookEvents, bin) },
	}
}

// runSteps prints, confirms and applies each step in order. Steps whose
// Describe returns "" are reported as already done and skipped.
func runSteps(steps []setupStep, r *bufio.Reader, w io.Writer, yes bool) error {
	failed := 0
	for i, st := range steps {
		fmt.Fprintf(w, "\n[%d/%d] %s\n", i+1, len(steps), st.Name)
		desc := st.Describe()
		if desc == "" {
			fmt.Fprintln(w, "  nothing to do")
			continue
		}
		fmt.Fprintln(w, "  will:", desc)
		if !confirm(r, w, "  apply?", yes) {
			fmt.Fprintln(w, "  skipped")
			continue
		}
		if err := st.Apply(); err != nil {
			failed++
			fmt.Fprintln(w, "  failed:", err)
			continue
		}
		fmt.Fprintln(w, "  done")
	}
	if failed > 0 {
		return fmt.Errorf("setup: %d step(s) failed", failed)
	}
	return nil
}

func newSetupCmd() *cobra.Command {
	var (
		retention          int
		widget, hooks, yes bool
	)
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Configure retention, shell widget and hooks (each step skippable)",
		Long: `setup walks through three changes and asks before each one:

  retention  set cleanupPeriodDays in settings.json so Claude stops deleting
             transcripts after 30 days (default proposal: 3650, ten years)
  widget     add a Ctrl-G binding to your shell rc file
  hooks      add SessionStart/SessionEnd/Notification hooks to settings.json
             so recall can record launch flags and archive on exit

Pass --retention N, --widget or --hooks to run only those steps; --yes
answers every question with y.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p := paths()
			if err := os.MkdirAll(p.RecallDir, 0o700); err != nil {
				return fmt.Errorf("create %s: %w", p.RecallDir, err)
			}
			selective := cmd.Flags().Changed("retention") || widget || hooks
			days := retention
			if days <= 0 {
				days = defaultRetentionDays
			}
			if cmd.Flags().Changed("retention") && retention <= 0 {
				return usagef("--retention must be a positive number of days")
			}
			var steps []setupStep
			if !selective || cmd.Flags().Changed("retention") {
				steps = append(steps, retentionStep(p, days))
			}
			if !selective || widget {
				sh, rc := shell.Detect()
				steps = append(steps, widgetStep(sh, rc))
			}
			if !selective || hooks {
				steps = append(steps, hooksStep(p, binPath()))
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "recall setup\n  claude dir: %s\n  recall dir: %s\n", p.ClaudeDir, p.RecallDir)
			r := bufio.NewReader(cmd.InOrStdin())
			if err := runSteps(steps, r, w, yes); err != nil {
				return err
			}
			fmt.Fprintln(w, "\nsetup complete. Run 'recall doctor' to verify.")
			return nil
		},
	}
	cmd.Flags().IntVar(&retention, "retention", 0, "set cleanupPeriodDays in settings.json to N days")
	cmd.Flags().BoolVar(&widget, "widget", false, "install the shell widget")
	cmd.Flags().BoolVar(&hooks, "hooks", false, "install Claude hooks")
	cmd.Flags().BoolVar(&yes, "yes", false, "run non-interactively, answering y to every step")
	return cmd
}

func newUninstallCmd() *cobra.Command {
	var yes, purge bool
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove hooks and shell widget installed by setup",
		Long: `uninstall removes the recall hooks from settings.json and the recall block
from your shell rc file. cleanupPeriodDays is left as it is. The recall
directory (archives, labels, launch records) is kept unless --purge.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p := paths()
			sh, rc := shell.Detect()
			steps := []setupStep{
				{
					Name:     "hooks",
					Describe: func() string { return "remove recall hooks from " + p.SettingsFile },
					Apply:    func() error { return hook.UninstallSettings(p) },
				},
				{
					Name: "widget",
					Describe: func() string {
						if rc == "" {
							return ""
						}
						data, err := os.ReadFile(rc)
						if err != nil || !strings.Contains(string(data), shell.BeginMarker) {
							return ""
						}
						return fmt.Sprintf("remove the recall block from %s (%s)", rc, sh)
					},
					Apply: func() error { return shell.Uninstall(rc) },
				},
			}
			if purge {
				steps = append(steps, setupStep{
					Name:     "purge",
					Describe: func() string { return "delete " + p.RecallDir + " including archived transcripts" },
					Apply:    func() error { return os.RemoveAll(p.RecallDir) },
				})
			}
			w := cmd.OutOrStdout()
			fmt.Fprintln(w, "recall uninstall")
			r := bufio.NewReader(cmd.InOrStdin())
			return runSteps(steps, r, w, yes)
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "run non-interactively, answering y to every step")
	cmd.Flags().BoolVar(&purge, "purge", false, "also delete the recall directory")
	return cmd
}
