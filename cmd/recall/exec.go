package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/MohammedAl-Alimi/recall/internal/model"
	"github.com/MohammedAl-Alimi/recall/internal/state"
)

// runExec runs argv with inherited stdio, records the exit code as an event
// for sid, and returns the code. RECALL_SID is exported to the child so a
// claude started this way can be matched back to the kept session.
func runExec(p model.Paths, sid string, argv []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	if len(argv) == 0 {
		return exitUsage, usagef("exec: missing command after --")
	}
	start := time.Now()
	code := 0
	var runErr error
	if dryRun() {
		fmt.Fprintf(stdout, "would run for %s: %s\n", model.ShortID(sid), strings.Join(argv, " "))
	} else {
		bin, err := exec.LookPath(argv[0])
		if err != nil {
			code, runErr = 127, err
		} else {
			c := exec.Command(bin, argv[1:]...)
			c.Stdin, c.Stdout, c.Stderr = stdin, stdout, stderr
			c.Env = append(os.Environ(), "RECALL_SID="+sid)
			if err := c.Run(); err != nil {
				var ee *exec.ExitError
				if asExitError(err, &ee) {
					code = ee.ExitCode()
				} else {
					code, runErr = 1, err
				}
			}
		}
	}
	ev := map[string]any{
		"event":       "exec",
		"sid":         sid,
		"argv":        argv,
		"exit":        code,
		"started_at":  start.UTC().Format(time.RFC3339),
		"duration_ms": time.Since(start).Milliseconds(),
	}
	if runErr != nil {
		ev["error"] = runErr.Error()
	}
	if err := os.MkdirAll(p.RecallDir, 0o700); err == nil {
		_ = state.AppendEvent(p, ev)
	}
	return code, runErr
}

func newExecCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "exec <sid> -- <cmd> [args]",
		Short: "Run a command and record its exit code for a kept session",
		Long: `exec runs a command with RECALL_SID set and appends its exit code to
events.jsonl. tmux kept sessions use it as the pane command so recall can
tell a finished claude from a crashed one. The exit code is passed through.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sid := args[0]
			rest := args[1:]
			// cobra strips a leading "--" only when it is the first token
			// after the flags; strip a stray one here too.
			if len(rest) > 0 && rest[0] == "--" {
				rest = rest[1:]
			}
			code, err := runExec(paths(), sid, rest, cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr())
			if err != nil && code == exitUsage {
				return err
			}
			if err != nil {
				return &exitError{code: code, err: err}
			}
			if code != 0 {
				return &exitError{code: code}
			}
			return nil
		},
	}
	cmd.Flags().SetInterspersed(false)
	return cmd
}
