package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/MohammedAl-Alimi/recall/internal/serve"
	"github.com/MohammedAl-Alimi/recall/internal/service"
)

// newServiceCmd builds 'recall service': the dashboard as a login item and
// a daily archive run, both as launchd user agents.
func newServiceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Run the dashboard and the daily archive in the background",
		Long: `service installs recall as a background service so the dashboard is always
running at one bookmarkable URL and every session is archived once a day.

Two jobs are installed, and either can be left out:

  serve     the dashboard, started at login and restarted after a crash,
            always reachable at the URL 'recall service url' prints
  archive   'recall archive --all --quiet' once a day, so a session cannot
            be lost even if the retention setting is changed back

On macOS these are launchd user agents in ~/Library/LaunchAgents. Other
platforms print the command to put into a systemd user unit instead.

RECALL_DRY_RUN=1 prints the property list and the launchctl calls without
writing or loading anything.`,
	}
	cmd.AddCommand(
		newServiceInstallCmd(),
		newServiceUninstallCmd(),
		newServiceStatusCmd(),
		newServiceURLCmd(),
	)
	return cmd
}

// serviceKinds turns the --serve/--archive flags into the list of services
// to act on. Neither flag means both, which is what the user almost always
// wants.
func serviceKinds(wantServe, wantArchive bool) []service.Kind {
	if !wantServe && !wantArchive {
		return service.Kinds
	}
	var out []service.Kind
	if wantServe {
		out = append(out, service.KindServe)
	}
	if wantArchive {
		out = append(out, service.KindArchive)
	}
	return out
}

// serviceConfig builds the configuration for one kind from the global
// directory flags and the command's own flags.
func serviceConfig(kind service.Kind) (service.Config, error) {
	c, err := service.DefaultConfig(kind, binPath())
	if err != nil {
		return service.Config{}, err
	}
	// Logs belong next to the state they describe, so an isolated
	// --recall-dir keeps its own logs.
	c.LogDir = filepath.Join(paths().RecallDir, service.LogDirName)
	// A service started by launchd inherits none of this shell's
	// environment, so a directory chosen with a flag has to travel with the
	// job as a flag of its own.
	c.ClaudeDir, c.RecallDir = flagClaudeDir, flagRecallDir
	return c, nil
}

// describeInstall prints what installing c would write, run and load.
func describeInstall(w io.Writer, c service.Config) error {
	argv, err := c.Args()
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "\n%s\n", c.Kind)
	fmt.Fprintf(w, "  write  %s\n", c.PlistPath())
	fmt.Fprintf(w, "  run    %s\n", strings.Join(argv, " "))
	switch c.Kind {
	case service.KindServe:
		fmt.Fprintf(w, "  when   at login, and again whenever it exits unexpectedly\n")
	case service.KindArchive:
		fmt.Fprintf(w, "  when   every day at %s\n", c.At())
	}
	fmt.Fprintf(w, "  logs   %s\n", c.StdoutPath())
	if ok, _ := service.Supported(); ok {
		fmt.Fprintf(w, "  load   launchctl bootstrap gui/%d %s\n", os.Getuid(), c.PlistPath())
	}
	return nil
}

func newServiceInstallCmd() *cobra.Command {
	var (
		wantServe   bool
		wantArchive bool
		addr        string
		at          string
		yes         bool
	)
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install the background services (both by default)",
		Long: `install writes one launchd property list per service and loads it.

Nothing is written before the exact plan is printed and confirmed. Pass
--yes to skip the question, which is what a scripted setup wants.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			kinds := serviceKinds(wantServe, wantArchive)
			hour, minute := service.DefaultHour, service.DefaultMinute
			if at != "" {
				var err error
				hour, minute, err = service.ParseAt(at)
				if err != nil {
					return usageErr(err)
				}
			}
			configs := make([]service.Config, 0, len(kinds))
			for _, k := range kinds {
				c, err := serviceConfig(k)
				if err != nil {
					return err
				}
				if addr != "" {
					c.Addr = addr
				}
				c.Hour, c.Minute = hour, minute
				configs = append(configs, c)
			}

			out := cmd.OutOrStdout()
			if ok, why := service.Supported(); !ok {
				fmt.Fprintln(out, "recall service is not available here:", why)
			}
			fmt.Fprintln(out, "recall service install")
			for _, c := range configs {
				if err := describeInstall(out, c); err != nil {
					return err
				}
			}

			if dryRun() {
				for _, c := range configs {
					body, err := c.Plist()
					if err != nil {
						return err
					}
					fmt.Fprintf(out, "\nDRY RUN: %s would contain:\n%s", c.PlistPath(), body)
				}
				fmt.Fprintln(out, "\nDRY RUN: nothing was written and nothing was loaded")
				return nil
			}

			fmt.Fprintln(out)
			r := bufio.NewReader(cmd.InOrStdin())
			if !confirm(r, out, fmt.Sprintf("install %d service(s)?", len(configs)), yes) {
				fmt.Fprintln(out, "nothing was installed")
				return nil
			}
			for _, c := range configs {
				path, err := service.Install(c)
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "installed %s  %s\n", c.Kind, path)
			}
			if hasKind(kinds, service.KindServe) {
				url, err := dashboardURL(addr)
				if err == nil {
					fmt.Fprintln(out, "\ndashboard", url)
					fmt.Fprintln(out, "(bookmark that URL; the token does not change)")
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&wantServe, "serve", false, "install the dashboard service only")
	cmd.Flags().BoolVar(&wantArchive, "archive", false, "install the daily archive service only")
	cmd.Flags().StringVar(&addr, "addr", "", "dashboard listen address (default "+serve.DefaultAddr+")")
	cmd.Flags().StringVar(&at, "at", "", "time of the daily archive run as HH:MM (default 09:00)")
	cmd.Flags().BoolVar(&yes, "yes", false, "do not ask for confirmation")
	return cmd
}

func newServiceUninstallCmd() *cobra.Command {
	var (
		wantServe   bool
		wantArchive bool
		yes         bool
	)
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Stop the background services and remove their property lists",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			kinds := serviceKinds(wantServe, wantArchive)
			out := cmd.OutOrStdout()
			configs := make([]service.Config, 0, len(kinds))
			for _, k := range kinds {
				c, err := serviceConfig(k)
				if err != nil {
					return err
				}
				configs = append(configs, c)
			}
			fmt.Fprintln(out, "recall service uninstall")
			for _, c := range configs {
				fmt.Fprintf(out, "\n%s\n", c.Kind)
				if ok, _ := service.Supported(); ok {
					fmt.Fprintf(out, "  stop   launchctl bootout gui/%d/%s\n", os.Getuid(), c.Label)
				}
				fmt.Fprintf(out, "  remove %s\n", c.PlistPath())
			}
			if dryRun() {
				fmt.Fprintln(out, "\nDRY RUN: nothing was stopped and nothing was removed")
				return nil
			}
			fmt.Fprintln(out)
			r := bufio.NewReader(cmd.InOrStdin())
			if !confirm(r, out, fmt.Sprintf("uninstall %d service(s)?", len(configs)), yes) {
				fmt.Fprintln(out, "nothing was uninstalled")
				return nil
			}
			for _, c := range configs {
				if err := service.Uninstall(c); err != nil {
					return err
				}
				fmt.Fprintf(out, "uninstalled %s\n", c.Kind)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&wantServe, "serve", false, "uninstall the dashboard service only")
	cmd.Flags().BoolVar(&wantArchive, "archive", false, "uninstall the daily archive service only")
	cmd.Flags().BoolVar(&yes, "yes", false, "do not ask for confirmation")
	return cmd
}

func newServiceStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show whether the background services are installed and running",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			if ok, why := service.Supported(); !ok {
				fmt.Fprintln(out, "note:", why)
			}
			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "SERVICE\tINSTALLED\tLOADED\tPID\tPLIST")
			var details []string
			for _, k := range service.Kinds {
				c, err := serviceConfig(k)
				if err != nil {
					return err
				}
				st, err := service.StatusOf(c)
				if err != nil {
					return err
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", k, yesNo(st.Installed), yesNo(st.Loaded), pidText(st.PID), st.PlistPath)
				if st.Detail != "" {
					details = append(details, fmt.Sprintf("%s: %s", k, st.Detail))
				}
			}
			if err := tw.Flush(); err != nil {
				return err
			}
			for _, d := range details {
				fmt.Fprintln(out, d)
			}
			if url, err := dashboardURL(""); err == nil {
				fmt.Fprintln(out, "\ndashboard", url)
			}
			return nil
		},
	}
}

func newServiceURLCmd() *cobra.Command {
	var addr string
	cmd := &cobra.Command{
		Use:   "url",
		Short: "Print the bookmarkable dashboard URL on one line",
		Long: `url prints the dashboard URL, token included, and nothing else, so it can
be piped into a browser or pasted into another dashboard as a link.

The token is stored in the recall directory and does not change, so the URL
stays valid across restarts. The address is read from the installed serve
service when there is one.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			url, err := dashboardURL(addr)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), url)
			return nil
		},
	}
	cmd.Flags().StringVar(&addr, "addr", "", "dashboard listen address (default: the installed service, else "+serve.DefaultAddr+")")
	return cmd
}

// dashboardURL builds the stable dashboard URL. addr wins when given,
// otherwise the address of the installed serve service is used, otherwise
// the default. The token is the one stored in the recall directory, which
// is created on first use and then never changes.
func dashboardURL(addr string) (string, error) {
	c, err := serviceConfig(service.KindServe)
	if err != nil {
		return "", err
	}
	if addr == "" {
		if a, ok := service.InstalledAddr(c); ok {
			addr = a
		}
	}
	a, err := newApp()
	if err != nil {
		return "", err
	}
	s := serve.New(a, serve.Options{Addr: addr})
	c.Addr = s.Addr()
	return c.URL(s.Token()), nil
}

func hasKind(kinds []service.Kind, k service.Kind) bool {
	for _, have := range kinds {
		if have == k {
			return true
		}
	}
	return false
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func pidText(pid int) string {
	if pid <= 0 {
		return "-"
	}
	return fmt.Sprint(pid)
}
