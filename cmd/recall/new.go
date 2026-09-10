package main

import (
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/MohammedAl-Alimi/recall/internal/launch"
	"github.com/MohammedAl-Alimi/recall/internal/model"
	"github.com/MohammedAl-Alimi/recall/internal/mux"
	"github.com/MohammedAl-Alimi/recall/internal/state"
)

// newSessionID returns a random UUID v4 in the canonical lower-case form,
// the same shape claude uses for session ids.
func newSessionID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// rand.Read never fails on supported platforms; fall back to time.
		n := uint64(time.Now().UnixNano())
		for i := range b {
			b[i] = byte(n >> (8 * (i % 8)))
		}
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// newClaudeArgv builds the argv for a fresh session: claude [-n name] [args].
func newClaudeArgv(name string, extra []string) []string {
	argv := []string{"claude"}
	if name != "" {
		argv = append(argv, "-n", name)
	}
	return append(argv, extra...)
}

// resolveCwd returns an absolute, existing working directory.
func resolveCwd(dir string) (string, error) {
	if dir == "" {
		return os.Getwd()
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return "", usagef("cwd %s: %v", abs, err)
	}
	if !st.IsDir() {
		return "", usagef("cwd %s is not a directory", abs)
	}
	return abs, nil
}

// planNew returns the actions needed to start a fresh session. Without
// --keep there is a single in-place action; with --keep the first action
// creates the detached tmux session and the second attaches to it.
func planNew(t *mux.Tmux, sid, name, cwd string, keep bool, extra []string) ([]*launch.Action, error) {
	if !keep {
		return []*launch.Action{{
			Kind:        "new",
			Cwd:         cwd,
			Argv:        newClaudeArgv(name, extra),
			Description: "start a new claude session in " + cwd,
		}}, nil
	}
	ok, ver := t.Available()
	if !ok && !dryRun() {
		return nil, fmt.Errorf("--keep needs tmux >= 3.2 on PATH (found %q); run 'recall doctor'", ver)
	}
	create := t.NewKeptCommand(sid, name, cwd, extra)
	attach := t.AttachCommand(sid)
	if len(create) == 0 || len(attach) == 0 {
		return nil, fmt.Errorf("tmux command builder returned nothing")
	}
	return []*launch.Action{
		{Kind: "new", Cwd: cwd, Argv: create, Description: "create kept tmux session " + t.SessionName(sid)},
		{Kind: "attach", Cwd: cwd, Argv: attach, Description: "attach to " + t.SessionName(sid) + " (Ctrl-\\ detaches)"},
	}, nil
}

func newNewCmd() *cobra.Command {
	var (
		name, cwd string
		keep      bool
	)
	cmd := &cobra.Command{
		Use:   "new [-n name] [--keep] [--cwd dir] [-- claude args]",
		Short: "Start a new session here",
		Long: `new starts a fresh claude session in the current directory (or --cwd).
Arguments after -- are passed to claude unchanged. With --keep the session
runs inside a tmux session on recall's private socket so it survives the
terminal tab; detach with Ctrl-\ and reattach from the list.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := resolveCwd(cwd)
			if err != nil {
				return err
			}
			sid := newSessionID()
			actions, err := planNew(mux.NewTmux(), sid, name, dir, keep, args)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if dryRun() {
				for _, act := range actions {
					printAction(out, act)
				}
				return nil
			}
			if keep {
				p := paths()
				if err := os.MkdirAll(p.RecallDir, 0o700); err == nil {
					_ = state.SaveLaunch(p, &model.Launch{
						SID: sid, Argv: actions[0].Argv, Cwd: dir,
						TermProgram: os.Getenv("TERM_PROGRAM"), Mux: "tmux",
						RecordedBy: "recall new --keep", At: time.Now(),
					})
					_ = state.AppendEvent(p, map[string]any{
						"event": "new", "sid": sid, "cwd": dir, "keep": true, "at": time.Now().UTC().Format(time.RFC3339),
					})
				}
				create := exec.Command(actions[0].Argv[0], actions[0].Argv[1:]...)
				create.Dir = dir
				create.Stdout, create.Stderr = cmd.OutOrStdout(), cmd.ErrOrStderr()
				if err := create.Run(); err != nil {
					return fmt.Errorf("tmux new-session: %w", err)
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "recall: kept session %s started, attaching (Ctrl-\\ detaches)\n", model.ShortID(sid))
				return execInPlace(actions[1])
			}
			return execInPlace(actions[0])
		},
	}
	cmd.Flags().StringVarP(&name, "name", "n", "", "session name (claude -n)")
	cmd.Flags().BoolVar(&keep, "keep", false, "run inside a kept tmux session")
	cmd.Flags().StringVar(&cwd, "cwd", "", "working directory (default: current)")
	return cmd
}
