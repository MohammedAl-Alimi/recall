package ui

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MohammedAl-Alimi/recall/internal/app"
	"github.com/MohammedAl-Alimi/recall/internal/launch"
	"github.com/MohammedAl-Alimi/recall/internal/model"
	"github.com/MohammedAl-Alimi/recall/internal/state"
	tea "github.com/charmbracelet/bubbletea"
)

// copyFixtures copies testdata/fixtures/claude into a fresh temp dir so the
// real scanner, prober and meta store run against synthesized data only.
func copyFixtures(t *testing.T) string {
	t.Helper()
	src := filepath.Join("..", "..", "testdata", "fixtures", "claude")
	dst := filepath.Join(t.TempDir(), "claude")
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o700)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(out, b, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

// TestRealAppEndToEnd drives the model with a real *app.App over the
// fixtures: load, render, search, ghosts toggle and a dry-run open.
func TestRealAppEndToEnd(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("RECALL_DRY_RUN", "1")
	t.Setenv("RECALL_NO_AGENTS_JSON", "1")
	t.Setenv("TERM_PROGRAM", "Apple_Terminal")
	claude := copyFixtures(t)
	recall := filepath.Join(t.TempDir(), "recall")
	a, err := app.New(model.PathsFrom(claude, recall))
	if err != nil {
		t.Fatal(err)
	}
	a.ShellCwd = "/Users/alice/dev/app"
	m := newModel(a, a, RunOptions{})
	m.width, m.height = 120, 40
	tickFn = func(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd {
		return func() tea.Msg { return fn(time.Now().Add(d)) }
	}
	t.Cleanup(func() { tickFn = tea.Tick })

	// Init loads through the real backend.
	for _, msg := range drain(m.Init()) {
		if _, ok := msg.(loadedMsg); ok {
			m.Update(msg)
		}
	}
	if m.fatal != nil {
		t.Fatalf("fatal after load: %v", m.fatal)
	}
	if len(a.Sessions) == 0 {
		t.Fatal("real app loaded no sessions from fixtures")
	}
	if len(m.rows) == 0 {
		t.Fatal("no rows rendered from real app")
	}
	out := ansiRE.ReplaceAllString(m.View(), "")
	for _, want := range []string{"sessions", "Config migration (renamed by hand)", "Retry helper for http client", "cache-fixer"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "\u2014") {
		t.Error("view contains an em dash")
	}
	for _, line := range strings.Split(out, "\n") {
		if len([]rune(line)) > 120 {
			t.Errorf("view line wider than terminal: %q", line)
		}
	}

	// Search narrows through index.Filter.
	press(m, "/")
	for _, r := range "retry" {
		m.Update(key(string(r)))
	}
	press(m, "enter")
	if len(m.rows) != 1 || !strings.Contains(m.rows[0].Title, "Retry") {
		t.Errorf("search rows = %d (%+v)", len(m.rows), rowTitles(m))
	}
	press(m, "/")
	press(m, "esc")
	press(m, "esc")
	m.query = ""
	m.refreshRows()

	// Ghost toggle brings history-only sessions in via Scanner.Ghosts.
	before := len(m.rows)
	for _, msg := range drain(press(m, "g")) {
		m.Update(msg)
	}
	ghosts := 0
	for _, r := range m.rows {
		if r.Ghost {
			ghosts++
		}
	}
	if ghosts == 0 || len(m.rows) <= before {
		t.Errorf("ghost toggle: rows %d -> %d, ghosts %d", before, len(m.rows), ghosts)
	}

	// Meta persists through the real state package.
	for _, msg := range drain(press(m, "g")) {
		m.Update(msg)
	}
	m.cursor = 0
	sid := m.rows[0].ID
	for _, msg := range drain(press(m, "p")) {
		m.Update(msg)
	}
	meta, err := state.LoadMeta(a.Paths)
	if err != nil {
		t.Fatal(err)
	}
	if !meta.IsPinned(sid) {
		t.Errorf("pin did not persist for %s", sid)
	}

	// Dry-run open of a closed session plans a resume without executing.
	m.cursor = 0
	var closed *model.Session
	for i, r := range m.rows {
		if r.State == model.StateClosed || r.State == model.StateStale || r.State == model.StateInterrupted {
			m.cursor = i
			closed = r
			break
		}
	}
	if closed == nil {
		t.Fatalf("no closed session among %v", rowTitles(m))
	}
	act, err := a.Open(closed, launch.Options{DryRun: true})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if act.Kind != "resume" || len(act.Argv) < 3 || act.Argv[1] != "--resume" || act.Argv[2] != closed.ID {
		t.Errorf("action = %+v", act)
	}
	if err := a.Load(context.Background(), false); err != nil {
		t.Fatalf("second Load: %v", err)
	}
	if _, err := os.Stat(filepath.Join(recall, "cache.json")); err != nil {
		t.Errorf("scanner cache not written: %v", err)
	}
}

func rowTitles(m *Model) []string {
	var out []string
	for _, r := range m.rows {
		out = append(out, r.Title)
	}
	return out
}
