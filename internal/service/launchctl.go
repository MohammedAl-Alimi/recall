package service

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// launchctlTimeout caps every launchctl call so a wedged service database
// cannot hang a command line.
const launchctlTimeout = 10 * time.Second

// runLaunchctl executes launchctl and returns its combined output. It is a
// variable so tests can replace it; the RECALL_SERVICE_NO_LAUNCHCTL guard
// means the default is never reached from a test run.
var runLaunchctl = func(args ...string) ([]byte, error) {
	cmd := exec.Command("launchctl", args...)
	done := make(chan struct{})
	var out []byte
	var err error
	go func() {
		out, err = cmd.CombinedOutput()
		close(done)
	}()
	select {
	case <-done:
		return out, err
	case <-time.After(launchctlTimeout):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		<-done
		return out, fmt.Errorf("launchctl %s timed out", strings.Join(args, " "))
	}
}

// domain is the launchd GUI domain of the current user.
func domain() string { return "gui/" + strconv.Itoa(os.Getuid()) }

// serviceTarget is the domain plus the label, what bootout and print want.
func serviceTarget(label string) string { return domain() + "/" + label }

// Install writes the plist and loads it. It returns the plist path even
// when loading failed, so the caller can tell the user what is on disk.
//
// Loading is tried with 'launchctl bootstrap' first, which is the modern
// call, and falls back to the older 'launchctl load -w' when bootstrap is
// unavailable or refuses. When RECALL_SERVICE_NO_LAUNCHCTL=1 the file is
// written and nothing is loaded.
func Install(c Config) (string, error) {
	argv, err := c.Args()
	if err != nil {
		return "", err
	}
	if ok, _ := Supported(); !ok {
		return "", unsupportedError(runtime.GOOS, argv)
	}
	body, err := c.Plist()
	if err != nil {
		return "", err
	}
	path := c.PlistPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("service: create %s: %w", filepath.Dir(path), err)
	}
	if err := os.MkdirAll(c.LogDir, 0o755); err != nil {
		return "", fmt.Errorf("service: create %s: %w", c.LogDir, err)
	}
	if err := writeFile(path, []byte(body), 0o644); err != nil {
		return "", err
	}
	if noLaunchctl() {
		return path, nil
	}
	// A job that is already loaded rejects a bootstrap, so boot it out
	// first and ignore the outcome: an absent job is exactly what we want.
	_, _ = runLaunchctl("bootout", serviceTarget(c.Label))
	if _, err := runLaunchctl("bootstrap", domain(), path); err == nil {
		return path, nil
	}
	out, err := runLaunchctl("load", "-w", path)
	if err != nil {
		return path, fmt.Errorf("service: launchctl load %s: %w%s", path, err, detailOf(out))
	}
	return path, nil
}

// Uninstall boots the job out and removes its plist. A plist that is not
// there is not an error, so calling Uninstall twice is safe.
func Uninstall(c Config) error {
	if err := c.validate(); err != nil {
		return err
	}
	path := c.PlistPath()
	ok, _ := Supported()
	if ok && !noLaunchctl() {
		if _, err := runLaunchctl("bootout", serviceTarget(c.Label)); err != nil {
			// The older call takes the file, not the label, and only works
			// while the file still exists, so it has to run before removal.
			_, _ = runLaunchctl("unload", "-w", path)
		}
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("service: remove %s: %w", path, err)
	}
	return nil
}

// Status is what recall knows about one installed service.
type Status struct {
	// Installed reports whether the plist file exists.
	Installed bool
	// PlistPath is that file, whether or not it exists.
	PlistPath string
	// Loaded reports whether launchd currently knows the job.
	Loaded bool
	// PID is the running process, 0 when the job is not running.
	PID int
	// LastExit is the exit code of the last run, 0 when unknown.
	LastExit int
	// Detail explains anything the three booleans cannot, such as
	// launchctl being unavailable.
	Detail string
}

// StatusOf inspects one service. It never fails because launchd could not
// be reached: in that case the file layer is reported and Detail says why
// the rest is missing.
//
// The name is StatusOf rather than Status because the result type already
// owns that identifier.
func StatusOf(c Config) (Status, error) {
	if err := c.validate(); err != nil {
		return Status{}, err
	}
	st := Status{PlistPath: c.PlistPath()}
	if info, err := os.Stat(st.PlistPath); err == nil && info.Mode().IsRegular() {
		st.Installed = true
	}
	if ok, why := Supported(); !ok {
		st.Detail = why
		return st, nil
	}
	if noLaunchctl() {
		st.Detail = "launchctl not consulted (" + NoLaunchctlEnv + "=1)"
		return st, nil
	}
	if out, err := runLaunchctl("print", serviceTarget(c.Label)); err == nil {
		st.Loaded = true
		st.PID, st.LastExit = parsePrint(string(out))
		return st, nil
	}
	out, err := runLaunchctl("list", c.Label)
	if err != nil {
		if st.Installed {
			st.Detail = "not loaded (launchctl does not know " + c.Label + ")"
		}
		return st, nil
	}
	st.Loaded = true
	st.PID, st.LastExit = parseList(string(out))
	return st, nil
}

// Regexes for the two launchctl output formats. 'print' uses bare keys,
// 'list' prints a quoted plist-ish dictionary.
var (
	printPID   = regexp.MustCompile(`(?m)^\s*pid\s*=\s*(\d+)`)
	printExit  = regexp.MustCompile(`(?m)^\s*last exit code\s*=\s*(\d+)`)
	listPID    = regexp.MustCompile(`"PID"\s*=\s*(\d+)`)
	listStatus = regexp.MustCompile(`"LastExitStatus"\s*=\s*(-?\d+)`)
)

// parsePrint reads pid and last exit code out of 'launchctl print'.
func parsePrint(out string) (pid, lastExit int) {
	return firstInt(printPID, out), firstInt(printExit, out)
}

// parseList reads pid and last exit status out of 'launchctl list <label>'.
func parseList(out string) (pid, lastExit int) {
	return firstInt(listPID, out), firstInt(listStatus, out)
}

func firstInt(re *regexp.Regexp, s string) int {
	m := re.FindStringSubmatch(s)
	if len(m) != 2 {
		return 0
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0
	}
	return n
}

// detailOf turns launchctl output into a suffix for an error message.
func detailOf(out []byte) string {
	s := strings.TrimSpace(string(out))
	if s == "" {
		return ""
	}
	return ": " + s
}

// writeFile writes data atomically, so a half-written plist can never be
// picked up by launchd.
func writeFile(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".recall-service-*")
	if err != nil {
		return fmt.Errorf("service: write %s: %w", path, err)
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("service: write %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("service: write %s: %w", path, err)
	}
	if err := os.Chmod(name, mode); err != nil {
		return fmt.Errorf("service: write %s: %w", path, err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("service: write %s: %w", path, err)
	}
	return nil
}
