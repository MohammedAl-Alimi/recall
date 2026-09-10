package live

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// registryEntry is one ClaudeDir/sessions/<pid>.json file. Only *.json files
// are read; the sibling *.key files hold secrets and are never opened.
type registryEntry struct {
	PID                 int    `json:"pid"`
	SessionID           string `json:"sessionId"`
	Cwd                 string `json:"cwd"`
	StartedAt           int64  `json:"startedAt"`
	ProcStart           string `json:"procStart"`
	Version             string `json:"version"`
	Kind                string `json:"kind"`
	Entrypoint          string `json:"entrypoint"`
	MessagingSocketPath string `json:"messagingSocketPath"`
	Name                string `json:"name"`
	NameSource          string `json:"nameSource"`
	Status              string `json:"status"`
	UpdatedAt           int64  `json:"updatedAt"`
	BridgeSessionID     string `json:"bridgeSessionId"`

	// path is the file the entry was read from (for diagnostics only).
	path string
}

// agentEntry is one element of the 'claude agents --json --all' array.
type agentEntry struct {
	PID       int    `json:"pid"`
	Cwd       string `json:"cwd"`
	Kind      string `json:"kind"`
	StartedAt int64  `json:"startedAt"`
	SessionID string `json:"sessionId"`
	Name      string `json:"name"`
	Status    string `json:"status"`
}

// readRegistry reads every <pid>.json under dir. Files that do not parse or
// lack the pid or sessionId fields are counted in bad and skipped. A missing
// directory is not an error: it yields no entries.
func readRegistry(dir string) (entries []registryEntry, bad []string, err error) {
	if dir == "" {
		return nil, nil, nil
	}
	matches, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, nil, err
	}
	if _, statErr := os.Stat(dir); statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return nil, nil, nil
		}
		return nil, nil, statErr
	}
	sort.Strings(matches)
	for _, m := range matches {
		base := filepath.Base(m)
		// Only plain <pid>.json files. Anything with extra dots (for example
		// "<pid>.<hash>.key") never matches *.json, but guard the shape anyway.
		stem := strings.TrimSuffix(base, ".json")
		if _, convErr := strconv.Atoi(stem); convErr != nil {
			continue
		}
		data, readErr := os.ReadFile(m)
		if readErr != nil {
			bad = append(bad, base+": "+readErr.Error())
			continue
		}
		var e registryEntry
		if jsonErr := json.Unmarshal(data, &e); jsonErr != nil {
			bad = append(bad, base+": "+jsonErr.Error())
			continue
		}
		if e.PID <= 0 || e.SessionID == "" {
			bad = append(bad, base+": missing pid or sessionId")
			continue
		}
		e.path = m
		entries = append(entries, e)
	}
	return entries, bad, nil
}

// parseAgentsJSON decodes the output of 'claude agents --json --all'. The
// command prints a JSON array; some versions wrap it in an object with an
// "agents" key, so both shapes are accepted.
func parseAgentsJSON(data []byte) ([]agentEntry, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, errors.New("empty output")
	}
	var list []agentEntry
	if err := json.Unmarshal([]byte(trimmed), &list); err == nil {
		return list, nil
	}
	var wrapped struct {
		Agents []agentEntry `json:"agents"`
	}
	if err := json.Unmarshal([]byte(trimmed), &wrapped); err != nil {
		return nil, fmt.Errorf("agents json: %w", err)
	}
	if wrapped.Agents == nil {
		return nil, errors.New("agents json: unrecognized shape")
	}
	return wrapped.Agents, nil
}

// msToTime converts epoch milliseconds to a UTC time; zero stays zero.
func msToTime(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}
