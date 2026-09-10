//go:build linux

package hook

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// procLookup returns the parent pid and the exact argv of pid from procfs.
// /proc/<pid>/cmdline is NUL separated, so quoted arguments stay single
// tokens instead of being split on whitespace the way 'ps -o args' output
// would be.
func procLookup(pid int) (int, []string, error) {
	dir := filepath.Join("/proc", strconv.Itoa(pid))
	stat, err := os.ReadFile(filepath.Join(dir, "stat"))
	if err != nil {
		return 0, nil, fmt.Errorf("pid %d: %w", pid, err)
	}
	ppid, err := parseProcStatPpid(stat)
	if err != nil {
		return 0, nil, fmt.Errorf("pid %d stat: %w", pid, err)
	}
	cmd, err := os.ReadFile(filepath.Join(dir, "cmdline"))
	if err != nil {
		return 0, nil, fmt.Errorf("pid %d: %w", pid, err)
	}
	return ppid, parseProcCmdline(cmd), nil
}

// parseProcStatPpid extracts the parent pid from a /proc/<pid>/stat line.
// The comm field is parenthesised and may itself contain spaces and
// parentheses, so fields are counted from the last ')'.
func parseProcStatPpid(stat []byte) (int, error) {
	end := bytes.LastIndexByte(stat, ')')
	if end < 0 {
		return 0, fmt.Errorf("unexpected stat %q", stat)
	}
	fields := strings.Fields(string(stat[end+1:]))
	// fields[0] is the state, fields[1] the ppid.
	if len(fields) < 2 {
		return 0, fmt.Errorf("unexpected stat %q", stat)
	}
	return strconv.Atoi(fields[1])
}

// parseProcCmdline splits a NUL separated cmdline. Empty arguments in the
// middle are preserved; only the terminator after the last argument is
// dropped.
func parseProcCmdline(cmd []byte) []string {
	cmd = bytes.TrimSuffix(cmd, []byte{0})
	if len(cmd) == 0 {
		return nil
	}
	parts := bytes.Split(cmd, []byte{0})
	args := make([]string, len(parts))
	for i, p := range parts {
		args[i] = string(p)
	}
	return args
}
