package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/MohammedAl-Alimi/recall/internal/model"
	"github.com/MohammedAl-Alimi/recall/internal/serve"
)

// Kind is one of the two services recall can install.
type Kind string

const (
	// KindServe keeps the dashboard running and reachable at a stable URL.
	KindServe Kind = "serve"
	// KindArchive runs 'recall archive --all --quiet' once a day.
	KindArchive Kind = "archive"
)

// Kinds lists every kind in the order commands should present them.
var Kinds = []Kind{KindServe, KindArchive}

// Valid reports whether k is a known kind.
func (k Kind) Valid() bool { return k == KindServe || k == KindArchive }

// String makes Kind printable.
func (k Kind) String() string { return string(k) }

// LabelPrefix is prepended to the kind to form the launchd label, so the
// serve agent is "dev.recall.serve" and the archive agent
// "dev.recall.archive".
const LabelPrefix = "dev.recall."

// NoLaunchctlEnv disables every launchctl call when set to "1". The plist
// file is still written and removed; nothing is loaded. Tests set it.
const NoLaunchctlEnv = "RECALL_SERVICE_NO_LAUNCHCTL"

// LogDirName is the directory under RecallDir that holds the service logs.
const LogDirName = "logs"

// Default schedule for the daily archive run.
const (
	DefaultHour   = 9
	DefaultMinute = 0
)

// Config describes one installable service. Zero values are not usable;
// build one with DefaultConfig and override the fields a flag changed.
type Config struct {
	// Kind selects which service this is.
	Kind Kind
	// BinPath is the absolute path of the recall binary to run.
	BinPath string
	// Label is the launchd label, also the plist file name.
	Label string
	// LaunchAgentsDir is the directory the plist is written into.
	LaunchAgentsDir string
	// LogDir holds the stdout and stderr files of the service.
	LogDir string
	// Addr is the dashboard listen address. Serve only.
	Addr string
	// Hour and Minute are the local time of the daily run. Archive only.
	Hour, Minute int
	// ClaudeDir and RecallDir are passed through as --claude-dir and
	// --recall-dir when they are set, so a service installed from an
	// isolated environment keeps pointing at it.
	ClaudeDir, RecallDir string
}

// DefaultConfig returns the configuration recall installs when no flag says
// otherwise. binPath must be the absolute path of the recall binary.
func DefaultConfig(kind Kind, binPath string) (Config, error) {
	if !kind.Valid() {
		return Config{}, fmt.Errorf("service: unknown kind %q", kind)
	}
	if strings.TrimSpace(binPath) == "" {
		return Config{}, errors.New("service: no recall binary path")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return Config{}, errors.New("service: cannot resolve the home directory")
	}
	c := Config{
		Kind:            kind,
		BinPath:         binPath,
		Label:           LabelPrefix + string(kind),
		LaunchAgentsDir: filepath.Join(home, "Library", "LaunchAgents"),
		LogDir:          filepath.Join(model.DefaultPaths().RecallDir, LogDirName),
		Addr:            serve.DefaultAddr,
		Hour:            DefaultHour,
		Minute:          DefaultMinute,
	}
	return c, nil
}

// PlistPath is the file the launchd job is written to.
func (c Config) PlistPath() string {
	return filepath.Join(c.LaunchAgentsDir, c.Label+".plist")
}

// StdoutPath and StderrPath are the log files launchd redirects into.
func (c Config) StdoutPath() string { return filepath.Join(c.LogDir, string(c.Kind)+".out.log") }
func (c Config) StderrPath() string { return filepath.Join(c.LogDir, string(c.Kind)+".err.log") }

// URL returns the dashboard URL for a serve config, given the token that is
// stored in the recall directory. It is the bookmarkable link.
func (c Config) URL(token string) string {
	addr := c.Addr
	if addr == "" {
		addr = serve.DefaultAddr
	}
	return "http://" + addr + "/?t=" + token
}

// Args returns the command line launchd runs, ProgramArguments[0] being the
// binary itself.
func (c Config) Args() ([]string, error) {
	if err := c.validate(); err != nil {
		return nil, err
	}
	var argv []string
	switch c.Kind {
	case KindServe:
		argv = []string{c.BinPath, "serve", "--no-open", "--addr", c.Addr}
	case KindArchive:
		argv = []string{c.BinPath, "archive", "--all", "--quiet"}
	}
	if c.ClaudeDir != "" {
		argv = append(argv, "--claude-dir", c.ClaudeDir)
	}
	if c.RecallDir != "" {
		argv = append(argv, "--recall-dir", c.RecallDir)
	}
	return argv, nil
}

// validate rejects a configuration that would produce a broken job.
func (c Config) validate() error {
	if !c.Kind.Valid() {
		return fmt.Errorf("service: unknown kind %q", c.Kind)
	}
	if strings.TrimSpace(c.BinPath) == "" {
		return errors.New("service: no recall binary path")
	}
	if strings.TrimSpace(c.Label) == "" {
		return errors.New("service: no label")
	}
	if strings.TrimSpace(c.LaunchAgentsDir) == "" {
		return errors.New("service: no LaunchAgents directory")
	}
	if strings.TrimSpace(c.LogDir) == "" {
		return errors.New("service: no log directory")
	}
	switch c.Kind {
	case KindServe:
		if strings.TrimSpace(c.Addr) == "" {
			return errors.New("service: serve needs a listen address")
		}
	case KindArchive:
		if c.Hour < 0 || c.Hour > 23 {
			return fmt.Errorf("service: hour %d is not between 0 and 23", c.Hour)
		}
		if c.Minute < 0 || c.Minute > 59 {
			return fmt.Errorf("service: minute %d is not between 0 and 59", c.Minute)
		}
	}
	return nil
}

// At is the daily run time as HH:MM.
func (c Config) At() string {
	return fmt.Sprintf("%02d:%02d", c.Hour, c.Minute)
}

// ParseAt reads an "HH:MM" schedule.
func ParseAt(s string) (hour, minute int, err error) {
	parts := strings.SplitN(strings.TrimSpace(s), ":", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("service: %q is not a time of day, want HH:MM", s)
	}
	hour, err = strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || hour < 0 || hour > 23 {
		return 0, 0, fmt.Errorf("service: %q is not a time of day, want HH:MM", s)
	}
	minute, err = strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("service: %q is not a time of day, want HH:MM", s)
	}
	return hour, minute, nil
}

// Supported reports whether this platform has a service implementation and,
// when it has not, why.
func Supported() (bool, string) {
	if runtime.GOOS == "darwin" {
		return true, "launchd user agents in ~/Library/LaunchAgents"
	}
	return false, "recall service manages launchd user agents, which exist on macOS only; on " + runtime.GOOS + " run the printed command from a systemd user unit instead"
}

// unsupportedError is the error Install returns off darwin. It names the
// manual alternative and the exact command to put into it.
func unsupportedError(goos string, argv []string) error {
	return fmt.Errorf("service: not supported on %s: recall installs launchd user agents, which are macOS only. "+
		"Create a systemd user unit (systemctl --user) running this command instead: %s", goos, strings.Join(argv, " "))
}

// noLaunchctl reports whether every launchctl call must be skipped.
func noLaunchctl() bool { return os.Getenv(NoLaunchctlEnv) == "1" }
