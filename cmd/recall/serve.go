package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/MohammedAl-Alimi/recall/internal/launch"
	"github.com/MohammedAl-Alimi/recall/internal/serve"
)

// newServeCmd builds the 'recall serve' command: a local web dashboard that
// lists every session and opens one in a terminal with a single click.
func newServeCmd() *cobra.Command {
	var (
		addr    string
		open    bool
		noOpen  bool
		token   string
		verbose bool
	)
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Open the session dashboard in a browser",
		Long: `serve starts a small web server on the loopback address and prints its
URL. The page lists every session with its title, project and state, and
opens one in a new terminal tab (or a cmux workspace) with one click.

The server binds to 127.0.0.1 only and every request carries a token that
is stored in the recall directory, so nothing on the network can reach it.
Press Ctrl-C to stop.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("open") && cmd.Flags().Changed("no-open") {
				return usagef("pass either --open or --no-open, not both")
			}
			a, err := newApp()
			if err != nil {
				return err
			}
			wantOpen := openByDefault(cmd.OutOrStdout())
			if cmd.Flags().Changed("open") {
				wantOpen = open
			}
			if noOpen {
				wantOpen = false
			}

			s := serve.New(a, serve.Options{Addr: addr, Token: token, OpenBrowser: wantOpen})
			url := s.URL()

			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "recall dashboard")
			fmt.Fprintln(out, "  ", url)
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, "Local only. Press Ctrl-C to stop.")
			if verbose {
				fmt.Fprintln(out, "token:", s.Token())
			}

			if wantOpen {
				if err := openBrowser(url, cmd.ErrOrStderr()); err != nil {
					fmt.Fprintln(cmd.ErrOrStderr(), "recall: could not open the browser:", err)
				}
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			if err := s.ListenAndServe(ctx); err != nil {
				return err
			}
			fmt.Fprintln(out, "dashboard stopped")
			return nil
		},
	}
	cmd.Flags().StringVar(&addr, "addr", serve.DefaultAddr, "listen address (loopback only)")
	cmd.Flags().BoolVar(&open, "open", true, "open the dashboard in the default browser")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "do not open a browser")
	cmd.Flags().StringVar(&token, "token", "", "use this access token instead of the stored one")
	cmd.Flags().BoolVar(&verbose, "print-token", false, "print the access token on its own line")
	return cmd
}

// openByDefault reports whether a browser should be opened without being
// asked. Only an interactive terminal gets one, so piping the URL into
// another command stays quiet.
func openByDefault(out io.Writer) bool {
	f, ok := out.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// openBrowser asks the desktop to open url. In dry-run mode it prints the
// command instead, so tests and scripted runs never spawn a window.
func openBrowser(url string, errOut io.Writer) error {
	argv := browserArgv(url)
	if len(argv) == 0 {
		fmt.Fprintln(errOut, "recall: open this URL yourself:", url)
		return nil
	}
	if dryRun() {
		fmt.Fprintf(errOut, "DRY RUN: %s\n", launch.ShellJoin(argv))
		return nil
	}
	return exec.Command(argv[0], argv[1:]...).Start()
}

// browserArgv returns the platform command that opens a URL, or nil when
// the platform has none.
func browserArgv(url string) []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"open", url}
	case "linux":
		return []string{"xdg-open", url}
	case "windows":
		return []string{"cmd", "/c", "start", "", url}
	}
	return nil
}
