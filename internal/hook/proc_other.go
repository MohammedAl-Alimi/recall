//go:build !darwin && !linux

package hook

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// procLookup falls back to ps on platforms without an exact argv source.
// ps prints the arguments joined by spaces, so quoting is lost and a prompt
// containing something like "--add-dir /x" would come back as separate
// tokens. To keep such text from ever being replayed as flags, the fallback
// returns only argv[0]: the process is still recognised as claude and the
// launch is recorded, but no flags are carried over.
func procLookup(pid int) (int, []string, error) {
	out, err := exec.Command("ps", "-ww", "-o", "ppid=,args=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, nil, err
	}
	line := strings.TrimSpace(string(out))
	if line == "" {
		return 0, nil, fmt.Errorf("pid %d not found", pid)
	}
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0, nil, fmt.Errorf("unexpected ps output %q", line)
	}
	ppid, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, nil, err
	}
	return ppid, fields[1:2], nil
}
