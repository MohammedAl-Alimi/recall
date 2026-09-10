package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/MohammedAl-Alimi/recall/internal/launch"
	"github.com/MohammedAl-Alimi/recall/internal/ui"
)

// loopsAfter reports whether the widget loop should reopen the list once
// the action finished. Only actions that run claude in this terminal do;
// focusing another tab, opening a new tab or printing a hint ends the loop.
func loopsAfter(act *launch.Action) bool {
	if act == nil || act.Script != "" || len(act.Argv) == 0 {
		return false
	}
	switch act.Kind {
	case "resume", "new", "attach":
		return true
	}
	return false
}

// runUI shows the list and executes the chosen action once the TUI has
// released the terminal. With fromWidget the action runs as a child process
// and the list reopens when claude exits, so Ctrl-G behaves like a
// persistent launcher.
func runUI(cmd *cobra.Command, fromWidget bool, query string) error {
	opts := ui.RunOptions{FromWidget: fromWidget, InitialQuery: query}
	for {
		a, err := newApp()
		if err != nil {
			return err
		}
		act, err := ui.RunWithAction(a, opts)
		if err != nil {
			return err
		}
		if act == nil {
			return nil
		}
		out := cmd.OutOrStdout()
		if dryRun() {
			printAction(out, act)
			return nil
		}
		if fromWidget && loopsAfter(act) {
			if act.Note != "" {
				fmt.Fprintln(cmd.ErrOrStderr(), "recall:", act.Note)
			}
			if _, err := runChildHolding(a, act, cmd.InOrStdin(), out, cmd.ErrOrStderr()); err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "recall:", err)
			}
			// claude has exited: drop the session lock this process still
			// holds, or the next pick of the same session is refused as
			// already open.
			a.ReleaseLocks()
			opts.InitialQuery = ""
			continue
		}
		return runHolding(a, act, out)
	}
}
