//go:build darwin

package hook

import (
	"encoding/binary"
	"fmt"

	"golang.org/x/sys/unix"
)

// procLookup returns the parent pid and the exact argv of pid using sysctl.
// kern.procargs2 hands back the NUL separated argument vector the process
// was exec'd with, so quoted arguments (prompts, --append-system-prompt
// text) stay single tokens instead of being split on whitespace the way
// 'ps -o args' output would be.
func procLookup(pid int) (int, []string, error) {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return 0, nil, fmt.Errorf("kern.proc.pid %d: %w", pid, err)
	}
	if kp.Proc.P_pid == 0 && pid != 0 {
		return 0, nil, fmt.Errorf("pid %d not found", pid)
	}
	raw, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return 0, nil, fmt.Errorf("kern.procargs2 %d: %w", pid, err)
	}
	args, err := parseProcArgs2(raw)
	if err != nil {
		return 0, nil, fmt.Errorf("kern.procargs2 %d: %w", pid, err)
	}
	return int(kp.Eproc.Ppid), args, nil
}

// parseProcArgs2 decodes a kern.procargs2 buffer: a native endian int32
// argc, the NUL terminated executable path, zero or more NUL padding
// bytes, then argc NUL terminated arguments followed by the environment.
func parseProcArgs2(raw []byte) ([]string, error) {
	if len(raw) < 4 {
		return nil, fmt.Errorf("short buffer (%d bytes)", len(raw))
	}
	argc := int(int32(binary.NativeEndian.Uint32(raw[:4])))
	if argc < 0 {
		return nil, fmt.Errorf("negative argc %d", argc)
	}
	rest := raw[4:]
	// Skip the executable path.
	i := 0
	for i < len(rest) && rest[i] != 0 {
		i++
	}
	// Skip the padding after it.
	for i < len(rest) && rest[i] == 0 {
		i++
	}
	rest = rest[i:]
	args := make([]string, 0, argc)
	for len(args) < argc {
		j := 0
		for j < len(rest) && rest[j] != 0 {
			j++
		}
		if j == len(rest) {
			// Buffer ended without a terminator: keep what is there.
			if j > 0 {
				args = append(args, string(rest))
			}
			break
		}
		args = append(args, string(rest[:j]))
		rest = rest[j+1:]
	}
	if len(args) != argc {
		return nil, fmt.Errorf("expected %d arguments, decoded %d", argc, len(args))
	}
	return args, nil
}
