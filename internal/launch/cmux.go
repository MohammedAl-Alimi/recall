package launch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/MohammedAl-Alimi/recall/internal/model"
)

// cmux backend: opens a session in a new cmux workspace through the cmux
// socket CLI. cmux (https://cmux.dev) is a Ghostty based terminal whose
// workspaces are created with 'cmux new-workspace'. The CLI talks to the
// running app over a Unix socket, so the app must be up before a workspace
// can be opened; CmuxEnsureRunning takes care of that.

// Options.Terminal values.
const (
	// TerminalApp forces a Terminal.app tab.
	TerminalApp = "terminal"
	// TerminalITerm forces an iTerm2 tab.
	TerminalITerm = "iterm"
	// TerminalCmux forces a cmux workspace.
	TerminalCmux = "cmux"
)

// Terminals lists the accepted Options.Terminal values for help text.
var Terminals = []string{TerminalApp, TerminalITerm, TerminalCmux}

// CmuxBundleCLI is where the cmux app bundle ships its CLI. It is checked
// after PATH because the app does not install a symlink by default.
const CmuxBundleCLI = "/Applications/cmux.app/Contents/Resources/bin/cmux"

// cmuxBundleCLI is the bundle path consulted by CmuxAvailable. Tests point
// it at a temp path so the real bundle never influences a result.
var cmuxBundleCLI = CmuxBundleCLI

// cmux start-up polling: 40 x 250ms = 10s.
var (
	cmuxPollInterval = 250 * time.Millisecond
	cmuxStartTimeout = 10 * time.Second
)

// cmuxExec runs argv[0] with argv[1:] and returns combined output. Tests
// replace it so no real process is ever started.
var cmuxExec = func(ctx context.Context, argv ...string) ([]byte, error) {
	return exec.CommandContext(ctx, argv[0], argv[1:]...).CombinedOutput()
}

// checkTerminal validates an Options.Terminal value.
func checkTerminal(term string) error {
	switch strings.ToLower(term) {
	case "", TerminalApp, TerminalITerm, TerminalCmux:
		return nil
	}
	return fmt.Errorf("unknown terminal %q (use %s)", term, strings.Join(Terminals, ", "))
}

// CmuxAvailable reports whether the cmux CLI can be found on this machine
// and returns its path: 'cmux' on PATH first, then the app bundle copy.
func CmuxAvailable() (string, bool) {
	if p, err := exec.LookPath("cmux"); err == nil {
		return p, true
	}
	if st, err := os.Stat(cmuxBundleCLI); err == nil && st.Mode().IsRegular() && st.Mode()&0o111 != 0 {
		return cmuxBundleCLI, true
	}
	return "", false
}

// insideCmux reports whether recall itself runs inside a cmux workspace.
// cmux does not set a distinctive TERM_PROGRAM (it reports Ghostty's), but
// its shell integration exports CMUX_WORKSPACE_ID into every surface. An
// explicit opts.TermProgram of "cmux" counts as well.
func insideCmux(opts Options) bool {
	if strings.EqualFold(opts.TermProgram, TerminalCmux) {
		return true
	}
	return os.Getenv("CMUX_WORKSPACE_ID") != ""
}

// CmuxOpenArgv builds the CLI command that opens a focused cmux workspace
// named title in cwd running shellCommand. Empty title and cwd are left
// out so cmux applies its defaults.
func CmuxOpenArgv(cmuxPath, title, cwd, shellCommand string) []string {
	argv := []string{cmuxPath, "new-workspace"}
	if title != "" {
		argv = append(argv, "--name", title)
	}
	if cwd != "" {
		argv = append(argv, "--cwd", cwd)
	}
	argv = append(argv, "--command", shellCommand, "--focus", "true")
	return argv
}

// cmuxPing reports whether the cmux app answers on its socket.
func cmuxPing(ctx context.Context, cli string) error {
	out, err := cmuxExec(ctx, cli, "ping")
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("cmux ping: %s", msg)
		}
		return fmt.Errorf("cmux ping: %w", err)
	}
	return nil
}

// CmuxEnsureRunning makes sure the cmux app is up and answering on its
// socket. When 'cmux ping' fails it launches the app with 'open -a cmux'
// and polls ping every 250ms for up to 10s. With RECALL_DRY_RUN=1 it does
// nothing and returns nil.
func CmuxEnsureRunning(ctx context.Context) error {
	if os.Getenv("RECALL_DRY_RUN") == "1" {
		return nil
	}
	cli, ok := CmuxAvailable()
	if !ok {
		return errors.New("cmux CLI not found on PATH or in " + CmuxBundleCLI)
	}
	if cmuxPing(ctx, cli) == nil {
		return nil
	}
	if out, err := cmuxExec(ctx, "open", "-a", "cmux"); err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("launch cmux: %s", msg)
		}
		return fmt.Errorf("launch cmux: %w", err)
	}
	deadline := time.Now().Add(cmuxStartTimeout)
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for cmux: %w", ctx.Err())
		case <-time.After(cmuxPollInterval):
		}
		if err := cmuxPing(ctx, cli); err == nil {
			return nil
		} else if time.Now().After(deadline) {
			return fmt.Errorf("cmux did not answer within %s: %w", cmuxStartTimeout, err)
		}
	}
}

// decorateCmux turns a resume, new or attach action into a cmux workspace
// open: the claude (or tmux) argv becomes the workspace command, prefixed
// by a cd into the action's cwd, and Argv becomes the cmux CLI call. Script
// stays empty; Run executes Argv as a child after CmuxEnsureRunning.
func decorateCmux(a *Action, cli, title string) {
	shell := shellLine(a.Cwd, strings.Join(quoteAll(a.Argv), " "))
	a.Argv = CmuxOpenArgv(cli, title, a.Cwd, shell)
	a.Terminal = TerminalCmux
	a.Description += " (open in cmux workspace)"
}

// runCmux executes a cmux workspace action: it starts the app when needed
// and then runs the cmux CLI as a child process. The CLI returns as soon as
// the workspace exists; claude keeps running inside cmux.
func runCmux(a *Action) error {
	if len(a.Argv) == 0 {
		return errors.New("launch: empty cmux argv")
	}
	ctx, cancel := context.WithTimeout(context.Background(), cmuxStartTimeout+5*time.Second)
	defer cancel()
	if err := CmuxEnsureRunning(ctx); err != nil {
		return fmt.Errorf("launch: %w", err)
	}
	cmd := exec.CommandContext(ctx, a.Argv[0], a.Argv[1:]...)
	cmd.Stdout = io.Discard
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("cmux: %s", msg)
		}
		return fmt.Errorf("cmux: %w", err)
	}
	return nil
}

// cmuxTitle picks a workspace name for a session: its label, else its
// title, else "claude <short id>"; a fresh session is named after its
// directory. Long titles are cut so the sidebar stays readable.
func cmuxTitle(sid, label, title, cwd string) string {
	name := strings.TrimSpace(label)
	if name == "" {
		name = strings.TrimSpace(title)
	}
	if name == "" {
		if sid != "" {
			name = "claude " + model.ShortID(sid)
		} else {
			name = "claude " + filepath.Base(cwd)
		}
	}
	const max = 48
	if r := []rune(name); len(r) > max {
		name = string(r[:max-3]) + "..."
	}
	return name
}
