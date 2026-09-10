package hook

import (
	"errors"
	"fmt"
	"strings"

	"github.com/MohammedAl-Alimi/recall/internal/archive"
	"github.com/MohammedAl-Alimi/recall/internal/model"
)

// Command returns the shell command installed for event.
func Command(binPath, event string) string {
	return fmt.Sprintf("%s hook %s >/dev/null 2>&1 || true", shellQuote(binPath), event)
}

// isRecallCommand reports whether a hook command line belongs to recall.
func isRecallCommand(cmd string) bool {
	return strings.Contains(cmd, "/recall hook") || strings.HasPrefix(strings.TrimSpace(cmd), "recall hook")
}

// InstallSettings merges recall hooks for events into settings.json. Existing
// entries (recall's or anyone else's) are left alone; a recall entry is only
// added for an event that does not already have one.
func InstallSettings(p model.Paths, events []string, binPath string) error {
	if binPath == "" {
		return errors.New("hook: empty binary path")
	}
	if len(events) == 0 {
		events = DefaultEvents
	}
	obj, err := archive.ReadSettings(p)
	if err != nil {
		return err
	}
	hooks, _ := obj["hooks"].(map[string]any)
	if hooks == nil {
		if _, present := obj["hooks"]; present && obj["hooks"] != nil {
			return errors.New("hook: settings.json hooks is not an object")
		}
		hooks = map[string]any{}
	}
	changed := false
	for _, ev := range events {
		list, _ := hooks[ev].([]any)
		if hasRecall(list) {
			continue
		}
		list = append(list, map[string]any{
			"hooks": []any{map[string]any{"type": "command", "command": Command(binPath, ev)}},
		})
		hooks[ev] = list
		changed = true
	}
	if !changed {
		return nil
	}
	obj["hooks"] = hooks
	return archive.WriteSettings(p, obj)
}

// UninstallSettings removes recall hooks from settings.json. Only hook
// entries whose command contains "/recall hook" are removed.
func UninstallSettings(p model.Paths) error {
	obj, err := archive.ReadSettings(p)
	if err != nil {
		return err
	}
	hooks, _ := obj["hooks"].(map[string]any)
	if hooks == nil {
		return nil
	}
	changed := false
	for ev, v := range hooks {
		list, ok := v.([]any)
		if !ok {
			continue
		}
		var keep []any
		for _, entry := range list {
			pruned, wasRecall := stripRecall(entry)
			if wasRecall {
				changed = true
			}
			if pruned != nil {
				keep = append(keep, pruned)
			}
		}
		if len(keep) == 0 {
			delete(hooks, ev)
		} else {
			hooks[ev] = keep
		}
	}
	if !changed {
		return nil
	}
	if len(hooks) == 0 {
		delete(obj, "hooks")
	} else {
		obj["hooks"] = hooks
	}
	return archive.WriteSettings(p, obj)
}

// hasRecall reports whether any entry in an event list carries a recall
// command.
func hasRecall(list []any) bool {
	for _, entry := range list {
		m, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		inner, _ := m["hooks"].([]any)
		for _, h := range inner {
			if hm, ok := h.(map[string]any); ok {
				if cmd, _ := hm["command"].(string); isRecallCommand(cmd) {
					return true
				}
			}
		}
	}
	return false
}

// stripRecall removes recall commands from one matcher entry. It returns
// the entry to keep (nil when nothing is left) and whether anything was
// removed.
func stripRecall(entry any) (any, bool) {
	m, ok := entry.(map[string]any)
	if !ok {
		return entry, false
	}
	inner, ok := m["hooks"].([]any)
	if !ok {
		return entry, false
	}
	var keep []any
	removed := false
	for _, h := range inner {
		if hm, ok := h.(map[string]any); ok {
			if cmd, _ := hm["command"].(string); isRecallCommand(cmd) {
				removed = true
				continue
			}
		}
		keep = append(keep, h)
	}
	if !removed {
		return entry, false
	}
	if len(keep) == 0 {
		return nil, true
	}
	m["hooks"] = keep
	return m, true
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	safe := true
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_./=+:@%", r)) {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
