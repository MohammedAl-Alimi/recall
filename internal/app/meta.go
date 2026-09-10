package app

import (
	"sort"

	"github.com/MohammedAl-Alimi/recall/internal/state"
)

// EditMeta runs fn on the live Meta under the meta lock. Every in-memory
// edit (pin, hide, label, tags, trash) must go through it so a concurrent
// Load or SaveMeta never observes a half-written map.
func (a *App) EditMeta(fn func(m *state.Meta)) {
	a.metaMu.Lock()
	defer a.metaMu.Unlock()
	if a.Meta == nil {
		a.Meta = state.NewMeta()
	}
	fn(a.Meta)
}

// MetaSnapshot returns a deep copy of the live Meta taken under the meta
// lock. Hand the copy to anything that runs off the update goroutine, such
// as a save.
func (a *App) MetaSnapshot() *state.Meta {
	a.metaMu.Lock()
	defer a.metaMu.Unlock()
	if a.Meta == nil {
		return state.NewMeta()
	}
	return cloneMeta(a.Meta)
}

// SaveMeta writes a snapshot of the live Meta to disk. The snapshot is taken
// under the lock, the write happens without it, so callers may run SaveMeta
// from a background goroutine while the UI keeps editing.
func (a *App) SaveMeta() error {
	snap := a.MetaSnapshot()
	if err := a.d.saveMeta(a.Paths, snap); err != nil {
		return err
	}
	// The snapshot is now what the disk holds; remember it so the next Load
	// does not mistake our own write for an external change.
	a.metaMu.Lock()
	a.metaDisk = snap
	a.metaMu.Unlock()
	return nil
}

// cloneMeta returns a deep copy of m.
func cloneMeta(m *state.Meta) *state.Meta {
	if m == nil {
		return nil
	}
	out := state.NewMeta()
	out.Schema = m.Schema
	out.Pinned = append([]string(nil), m.Pinned...)
	out.Hidden = append([]string(nil), m.Hidden...)
	for k, v := range m.Labels {
		out.Labels[k] = v
	}
	for k, v := range m.Notes {
		out.Notes[k] = v
	}
	for k, v := range m.Tags {
		out.Tags[k] = append([]string(nil), v...)
	}
	out.Trash = append([]state.TrashEntry(nil), m.Trash...)
	return out
}

// installMeta merges the freshly read disk copy into the live Meta. On the
// first load the disk copy simply becomes the live object. Afterwards only
// the keys that changed on disk since the previous read (edits made by
// another recall process) are copied over, so unsaved in-memory edits
// survive a rescan. Must be called with metaMu held.
func (a *App) installMeta(disk *state.Meta) {
	if disk == nil {
		return
	}
	if a.Meta == nil || a.metaDisk == nil {
		a.Meta = disk
	} else {
		mergeMeta(a.Meta, a.metaDisk, disk)
	}
	a.metaDisk = cloneMeta(disk)
}

// mergeMeta applies to live every difference between oldDisk and newDisk
// (a three-way merge where live wins on untouched keys).
func mergeMeta(live, oldDisk, newDisk *state.Meta) {
	if live.Labels == nil {
		live.Labels = map[string]string{}
	}
	if live.Notes == nil {
		live.Notes = map[string]string{}
	}
	if live.Tags == nil {
		live.Tags = map[string][]string{}
	}
	mergeStringMap(live.Labels, oldDisk.Labels, newDisk.Labels)
	mergeStringMap(live.Notes, oldDisk.Notes, newDisk.Notes)
	for _, k := range keysOf(oldDisk.Tags, newDisk.Tags) {
		o, n := oldDisk.Tags[k], newDisk.Tags[k]
		if equalStrings(o, n) {
			continue
		}
		if len(n) == 0 {
			delete(live.Tags, k)
		} else {
			live.Tags[k] = append([]string(nil), n...)
		}
	}
	live.Pinned = mergeSet(live.Pinned, oldDisk.Pinned, newDisk.Pinned)
	live.Hidden = mergeSet(live.Hidden, oldDisk.Hidden, newDisk.Hidden)
	live.Trash = mergeTrash(live.Trash, oldDisk.Trash, newDisk.Trash)
	if newDisk.Schema > live.Schema {
		live.Schema = newDisk.Schema
	}
}

func mergeStringMap(live, oldDisk, newDisk map[string]string) {
	for _, k := range keysOf(oldDisk, newDisk) {
		o, oOK := oldDisk[k]
		n, nOK := newDisk[k]
		if oOK == nOK && o == n {
			continue
		}
		if !nOK {
			delete(live, k)
		} else {
			live[k] = n
		}
	}
}

// mergeSet returns live with every membership change between oldDisk and
// newDisk applied, keeping live's order for untouched entries.
func mergeSet(live, oldDisk, newDisk []string) []string {
	oldSet := toSet(oldDisk)
	newSet := toSet(newDisk)
	out := make([]string, 0, len(live)+len(newDisk))
	seen := map[string]bool{}
	for _, s := range live {
		if oldSet[s] && !newSet[s] {
			continue // removed externally
		}
		if !seen[s] {
			out = append(out, s)
			seen[s] = true
		}
	}
	for _, s := range newDisk {
		if !oldSet[s] && !seen[s] {
			out = append(out, s) // added externally
			seen[s] = true
		}
	}
	return out
}

func mergeTrash(live, oldDisk, newDisk []state.TrashEntry) []state.TrashEntry {
	oldSet := map[string]bool{}
	for _, e := range oldDisk {
		oldSet[e.SID] = true
	}
	newBy := map[string]state.TrashEntry{}
	for _, e := range newDisk {
		newBy[e.SID] = e
	}
	out := make([]state.TrashEntry, 0, len(live)+len(newDisk))
	seen := map[string]bool{}
	for _, e := range live {
		if _, still := newBy[e.SID]; oldSet[e.SID] && !still {
			continue
		}
		if !seen[e.SID] {
			out = append(out, e)
			seen[e.SID] = true
		}
	}
	for _, e := range newDisk {
		if !oldSet[e.SID] && !seen[e.SID] {
			out = append(out, e)
			seen[e.SID] = true
		}
	}
	return out
}

func toSet(list []string) map[string]bool {
	s := make(map[string]bool, len(list))
	for _, v := range list {
		s[v] = true
	}
	return s
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// keysOf returns the sorted union of the keys of both maps.
func keysOf[V any](a, b map[string]V) []string {
	set := map[string]bool{}
	for k := range a {
		set[k] = true
	}
	for k := range b {
		set[k] = true
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
