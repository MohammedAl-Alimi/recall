package live

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Function hooks that tests replace so no real process table is needed.
// Production code always uses the real implementations below.
var (
	// runPS executes ps with the given arguments and a fixed C/UTC
	// environment and returns its stdout.
	runPS = realRunPS

	// processExists reports whether a process with pid exists (kill 0).
	processExists = realProcessExists
)

func realRunPS(args ...string) (string, error) {
	cmd := exec.Command("ps", args...)
	cmd.Env = append(minimalEnv(), "LC_ALL=C", "TZ=UTC")
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return out.String(), fmt.Errorf("ps %s: %s", strings.Join(args, " "), msg)
	}
	return out.String(), nil
}

// minimalEnv returns a small environment for child tools: PATH plus HOME.
func minimalEnv() []string {
	env := []string{"PATH=" + os.Getenv("PATH")}
	if home := os.Getenv("HOME"); home != "" {
		env = append(env, "HOME="+home)
	}
	return env
}

func realProcessExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	// EPERM means the process exists but belongs to another user.
	return errors.Is(err, syscall.EPERM)
}

// ProcessStart returns the start time of pid as printed by
// 'LC_ALL=C TZ=UTC ps -o lstart= -p PID', trimmed. The string has the shape
// "Wed Sep  2 12:51:58 2026" (time.ANSIC) and is compared byte-for-byte with
// the procStart field of the registry.
func ProcessStart(pid int) (string, error) {
	if pid <= 0 {
		return "", fmt.Errorf("invalid pid %d", pid)
	}
	out, err := runPS("-o", "lstart=", "-p", strconv.Itoa(pid))
	if err != nil {
		return "", err
	}
	s := strings.TrimSpace(out)
	if s == "" {
		return "", fmt.Errorf("pid %d: no such process", pid)
	}
	return s, nil
}

// parseLstart parses a ps lstart string in UTC. It accepts the ANSIC layout
// used by ps in the C locale. Parsing is always done in UTC regardless of
// the TZ of the current process because ProcessStart forces TZ=UTC.
func parseLstart(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, errors.New("empty lstart")
	}
	// Collapse the day padding ("Sep  2") so both layouts parse.
	t, err := time.ParseInLocation(time.ANSIC, s, time.UTC)
	if err == nil {
		return t, nil
	}
	fields := strings.Fields(s)
	t2, err2 := time.ParseInLocation("Mon Jan 2 15:04:05 2006", strings.Join(fields, " "), time.UTC)
	if err2 == nil {
		return t2, nil
	}
	return time.Time{}, err
}

// ttyArgs returns the controlling tty and argv of pid from
// 'ps -o tty=,args= -p PID'. A tty of "??" is returned as "".
func ttyArgs(pid int) (tty string, argv []string, err error) {
	out, err := runPS("-o", "tty=,args=", "-p", strconv.Itoa(pid))
	if err != nil {
		return "", nil, err
	}
	line := strings.TrimSpace(out)
	if line == "" {
		return "", nil, fmt.Errorf("pid %d: no such process", pid)
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "", nil, nil
	}
	tty = fields[0]
	if tty == "??" || tty == "?" || tty == "-" {
		tty = ""
	}
	return tty, fields[1:], nil
}

// procEntry is one row of the process table snapshot.
type procEntry struct {
	PID  int
	PPID int
	TTY  string
	Comm string
}

// procTable is a snapshot of the process table keyed by pid.
type procTable map[int]procEntry

// snapshotProcs reads the whole process table once with
// 'ps -axo pid=,ppid=,tty=,comm='. comm is last so it may contain spaces.
func snapshotProcs() (procTable, error) {
	out, err := runPS("-axo", "pid=,ppid=,tty=,comm=")
	if err != nil {
		return nil, err
	}
	return parseProcTable(out), nil
}

func parseProcTable(out string) procTable {
	tbl := procTable{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil {
			continue
		}
		tty := fields[2]
		if tty == "??" || tty == "?" || tty == "-" {
			tty = ""
		}
		tbl[pid] = procEntry{PID: pid, PPID: ppid, TTY: tty, Comm: strings.Join(fields[3:], " ")}
	}
	return tbl
}

// parentOf returns the ppid and comm of pid from a single ps call.
func parentOf(pid int) (ppid int, comm string, err error) {
	out, err := runPS("-o", "ppid=,comm=", "-p", strconv.Itoa(pid))
	if err != nil {
		return 0, "", err
	}
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) < 2 {
		return 0, "", fmt.Errorf("pid %d: no such process", pid)
	}
	ppid, err = strconv.Atoi(fields[0])
	if err != nil {
		return 0, "", err
	}
	return ppid, strings.Join(fields[1:], " "), nil
}

// classifyHost maps a process comm (basename or full path) to a host
// application name. It returns "" when the process is not a known host.
func classifyHost(comm string) string {
	c := strings.TrimSpace(comm)
	if c == "" {
		return ""
	}
	lower := strings.ToLower(c)
	base := strings.ToLower(filepath.Base(c))
	switch {
	case strings.Contains(lower, "terminal.app/") || base == "terminal":
		return "Terminal.app"
	case strings.Contains(lower, "iterm"):
		return "iTerm2"
	case strings.Contains(lower, "cursor.app/") || base == "cursor" || strings.HasPrefix(base, "cursor helper"):
		return "Cursor"
	case strings.Contains(lower, "visual studio code") || base == "code" || strings.HasPrefix(base, "code helper") || strings.Contains(lower, "code.app/") || strings.Contains(lower, "code - insiders"):
		return "Code"
	case base == "tmux" || strings.HasPrefix(base, "tmux:") || strings.HasPrefix(base, "tmux "):
		return "tmux"
	case base == "screen" || strings.HasPrefix(base, "screen-") || strings.HasPrefix(base, "screen "):
		return "screen"
	case strings.Contains(lower, "warp.app/") || base == "warp" || base == "warpterminal":
		return "Warp"
	case strings.Contains(lower, "ghostty"):
		return "Ghostty"
	case strings.Contains(lower, "wezterm"):
		return "WezTerm"
	case strings.Contains(lower, "alacritty"):
		return "Alacritty"
	case strings.Contains(lower, "kitty"):
		return "kitty"
	case strings.Contains(lower, "hyper.app/"):
		return "Hyper"
	}
	return ""
}

// maxChainDepth bounds the ppid walk so a cyclic or huge table never loops.
const maxChainDepth = 32

// hostFromTable walks the ppid chain of pid inside a snapshot and returns the
// first known host application and the tty of pid.
func hostFromTable(tbl procTable, pid int) (app string, tty string) {
	if e, ok := tbl[pid]; ok {
		tty = e.TTY
	}
	cur := pid
	for i := 0; i < maxChainDepth; i++ {
		e, ok := tbl[cur]
		if !ok {
			break
		}
		if a := classifyHost(e.Comm); a != "" {
			return a, tty
		}
		if e.PPID <= 1 || e.PPID == cur {
			break
		}
		cur = e.PPID
	}
	return "", tty
}

// HostAppOf walks the parent chain of pid with repeated
// 'ps -o ppid=,comm= -p' calls and returns the hosting terminal application
// (Terminal.app, iTerm2, Cursor, Code, tmux, screen) and the tty of pid.
func HostAppOf(pid int) (app string, tty string) {
	if pid <= 0 {
		return "", ""
	}
	tty, _, _ = ttyArgs(pid)
	cur := pid
	for i := 0; i < maxChainDepth; i++ {
		ppid, comm, err := parentOf(cur)
		if err != nil {
			break
		}
		if a := classifyHost(comm); a != "" {
			return a, tty
		}
		if ppid <= 1 || ppid == cur {
			break
		}
		cur = ppid
	}
	return "", tty
}

// argvLooksLikeClaude reports whether an argv belongs to a claude process:
// the first token's basename starts with "claude", or the first token is a
// JS runtime and the script path contains "claude".
func argvLooksLikeClaude(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	first := strings.ToLower(filepath.Base(argv[0]))
	if strings.HasPrefix(first, "claude") {
		return true
	}
	switch first {
	case "node", "bun", "deno":
		if len(argv) > 1 && strings.Contains(strings.ToLower(argv[1]), "claude") {
			return true
		}
	}
	return false
}
