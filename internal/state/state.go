package state

import (
	"errors"
	"time"

	"github.com/MohammedAl-Alimi/recall/internal/model"
)

// SchemaVersion is the current Meta schema.
const SchemaVersion = 1

// TrashEntry records a soft-deleted session.
type TrashEntry struct {
	SID string    `json:"sid"`
	At  time.Time `json:"at"`
}

// Meta is recall's per-user metadata.
type Meta struct {
	Schema int                 `json:"schema"`
	Pinned []string            `json:"pinned,omitempty"`
	Hidden []string            `json:"hidden,omitempty"`
	Labels map[string]string   `json:"labels,omitempty"`
	Notes  map[string]string   `json:"notes,omitempty"`
	Tags   map[string][]string `json:"tags,omitempty"`
	Trash  []TrashEntry        `json:"trash,omitempty"`
}

// NewMeta returns an empty Meta with maps initialised.
func NewMeta() *Meta {
	return &Meta{
		Schema: SchemaVersion,
		Labels: map[string]string{},
		Notes:  map[string]string{},
		Tags:   map[string][]string{},
	}
}

// LoadMeta reads RecallDir/meta.json, returning an empty Meta when missing.
func LoadMeta(p model.Paths) (*Meta, error) {
	return nil, errors.New("not implemented: state.LoadMeta")
}

// SaveMeta writes meta atomically under the state lock.
func SaveMeta(p model.Paths, m *Meta) error {
	return errors.New("not implemented: state.SaveMeta")
}

// IsPinned reports whether sid is pinned.
func (m *Meta) IsPinned(sid string) bool { return contains(m.Pinned, sid) }

// IsHidden reports whether sid is hidden.
func (m *Meta) IsHidden(sid string) bool { return contains(m.Hidden, sid) }

// SetLabel sets or clears (empty label) the label for sid.
func (m *Meta) SetLabel(sid, label string) {
	if m.Labels == nil {
		m.Labels = map[string]string{}
	}
	if label == "" {
		delete(m.Labels, sid)
		return
	}
	m.Labels[sid] = label
}

// Pin marks sid as pinned.
func (m *Meta) Pin(sid string) { m.Pinned = add(m.Pinned, sid) }

// Unpin removes the pin from sid.
func (m *Meta) Unpin(sid string) { m.Pinned = remove(m.Pinned, sid) }

// Hide marks sid as hidden.
func (m *Meta) Hide(sid string) { m.Hidden = add(m.Hidden, sid) }

// Unhide removes sid from the hidden list.
func (m *Meta) Unhide(sid string) { m.Hidden = remove(m.Hidden, sid) }

// TrashAdd records sid as trashed now.
func (m *Meta) TrashAdd(sid string) {
	m.TrashRestore(sid)
	m.Trash = append(m.Trash, TrashEntry{SID: sid, At: time.Now()})
}

// TrashRestore removes sid from the trash and reports whether it was there.
func (m *Meta) TrashRestore(sid string) bool {
	out := m.Trash[:0]
	found := false
	for _, e := range m.Trash {
		if e.SID == sid {
			found = true
			continue
		}
		out = append(out, e)
	}
	m.Trash = out
	return found
}

// TrashPrune drops trash entries older than days and returns the pruned ids.
func (m *Meta) TrashPrune(days int) []string {
	cutoff := time.Now().AddDate(0, 0, -days)
	var pruned []string
	out := m.Trash[:0]
	for _, e := range m.Trash {
		if e.At.Before(cutoff) {
			pruned = append(pruned, e.SID)
			continue
		}
		out = append(out, e)
	}
	m.Trash = out
	return pruned
}

// SaveLaunch stores a launch record under RecallDir/launch/<sid>.json.
func SaveLaunch(p model.Paths, l *model.Launch) error {
	return errors.New("not implemented: state.SaveLaunch")
}

// LoadLaunch reads the launch record for sid.
func LoadLaunch(p model.Paths, sid string) (*model.Launch, error) {
	return nil, errors.New("not implemented: state.LoadLaunch")
}

// AppendEvent appends one JSON line to RecallDir/events.jsonl.
func AppendEvent(p model.Paths, ev map[string]any) error {
	return errors.New("not implemented: state.AppendEvent")
}

// SnapshotLastBoot records the session ids seen at this boot.
func SnapshotLastBoot(p model.Paths, sids []string) error {
	return errors.New("not implemented: state.SnapshotLastBoot")
}

// LoadLastBoot returns the session ids recorded by SnapshotLastBoot.
func LoadLastBoot(p model.Paths) ([]string, error) {
	return nil, errors.New("not implemented: state.LoadLastBoot")
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func add(list []string, s string) []string {
	if contains(list, s) {
		return list
	}
	return append(list, s)
}

func remove(list []string, s string) []string {
	out := list[:0]
	for _, v := range list {
		if v != s {
			out = append(out, v)
		}
	}
	return out
}
