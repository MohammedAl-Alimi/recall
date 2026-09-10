package state

import (
	"testing"
	"time"
)

func TestMetaHelpers(t *testing.T) {
	m := NewMeta()
	m.Pin("a")
	m.Pin("a")
	if !m.IsPinned("a") || len(m.Pinned) != 1 {
		t.Fatalf("pin: %+v", m.Pinned)
	}
	m.Unpin("a")
	if m.IsPinned("a") {
		t.Fatal("unpin failed")
	}
	m.Hide("b")
	if !m.IsHidden("b") {
		t.Fatal("hide failed")
	}
	m.Unhide("b")
	if m.IsHidden("b") {
		t.Fatal("unhide failed")
	}
	m.SetLabel("c", "work")
	if m.Labels["c"] != "work" {
		t.Fatal("label failed")
	}
	m.SetLabel("c", "")
	if _, ok := m.Labels["c"]; ok {
		t.Fatal("label clear failed")
	}
}

func TestTrash(t *testing.T) {
	m := NewMeta()
	m.TrashAdd("x")
	m.TrashAdd("x")
	if len(m.Trash) != 1 {
		t.Fatalf("trash dup: %+v", m.Trash)
	}
	m.Trash[0].At = time.Now().AddDate(0, 0, -40)
	m.TrashAdd("y")
	pruned := m.TrashPrune(30)
	if len(pruned) != 1 || pruned[0] != "x" || len(m.Trash) != 1 || m.Trash[0].SID != "y" {
		t.Fatalf("prune: pruned=%v trash=%+v", pruned, m.Trash)
	}
	if !m.TrashRestore("y") || m.TrashRestore("y") {
		t.Fatal("restore")
	}
}
