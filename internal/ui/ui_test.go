package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/MohammedAl-Alimi/recall/internal/app"
	"github.com/MohammedAl-Alimi/recall/internal/launch"
	"github.com/MohammedAl-Alimi/recall/internal/model"
	"github.com/MohammedAl-Alimi/recall/internal/state"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var ansiRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

// fakeBackend stands in for *app.App with three synthesized sessions.
type fakeBackend struct {
	sessions   []*model.Session
	loads      int
	refreshes  int
	opened     []*model.Session
	openedOpts []launch.Options
	openErr    error
	notified   int
	released   []string
}

func (f *fakeBackend) Load(ctx context.Context, includeGhosts bool) error {
	f.loads++
	return nil
}

func (f *fakeBackend) RefreshLive(ctx context.Context) error {
	f.refreshes++
	return nil
}

func (f *fakeBackend) Visible(showHidden, showGhosts, showHeadless bool) []*model.Session {
	var out []*model.Session
	for _, s := range f.sessions {
		if s.Hidden && !showHidden {
			continue
		}
		if s.Ghost && !showGhosts {
			continue
		}
		out = append(out, s)
	}
	return out
}

func (f *fakeBackend) Open(sess *model.Session, opts launch.Options) (*launch.Action, error) {
	f.opened = append(f.opened, sess)
	f.openedOpts = append(f.openedOpts, opts)
	if f.openErr != nil {
		return nil, f.openErr
	}
	argv := []string{"claude", "--resume", sess.ID}
	if opts.Fork {
		argv = append(argv, "--fork-session")
	}
	return &launch.Action{Kind: "resume", Cwd: sessionDir(sess), Argv: argv, Description: "resume " + sess.Short()}, nil
}

func (f *fakeBackend) ReleaseLock(sid string) { f.released = append(f.released, sid) }

func (f *fakeBackend) PreselectForCwd() *model.Session { return nil }

func (f *fakeBackend) NotifyNeedsYou(prev map[string]model.State) { f.notified++ }

var fixedNow = time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

func threeSessions(t *testing.T) []*model.Session {
	t.Helper()
	dir := t.TempDir()
	proj := filepath.Join(dir, "alpha-project")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	return []*model.Session{
		{
			ID: "aaaaaaaa-1111-4222-8333-444444444444", Title: "Fix the login race", WorkCwd: proj, Cwd: proj,
			Branch: "fix/login", LastPrompt: "why does the login hang", LastAssistant: "The mutex is taken twice.",
			LastActive: fixedNow.Add(-3 * time.Minute), State: model.StateNeedsYou,
			Live: &model.Live{PID: 4242, Alive: true, Status: "waiting", HostApp: "Terminal.app", TTY: "ttys003"},
			PRs:  []model.Link{{URL: "https://github.com/x/y/pull/12", Number: 12}},
			Path: filepath.Join(dir, "a.jsonl"),
		},
		{
			ID: "bbbbbbbb-1111-4222-8333-444444444444", Title: "Add calendar view", WorkCwd: proj, Cwd: proj,
			Branch: "feat/calendar", IsWorktree: true, WorktreeBranch: "feat/calendar", Cwds: []string{proj, dir},
			Lineage: "continuation", LastPrompt: "render the hour grid",
			LastActive: fixedNow.Add(-2 * time.Hour), State: model.StateLiveIdle,
			Live: &model.Live{PID: 4343, Alive: true, Status: "idle"},
			Path: filepath.Join(dir, "b.jsonl"),
		},
		{
			ID: "cccccccc-1111-4222-8333-444444444444", Title: "Write the README", WorkCwd: filepath.Join(dir, "gone-project"),
			Cwd: proj, Branch: "main", FirstPrompt: "draft a README for recall",
			LastActive: fixedNow.Add(-3 * 24 * time.Hour), State: model.StateClosed,
			Path: filepath.Join(dir, "c.jsonl"),
		},
	}
}

func newTestModel(t *testing.T) (*Model, *fakeBackend) {
	t.Helper()
	t.Setenv("NO_COLOR", "")
	t.Setenv("RECALL_DRY_RUN", "1")
	tmp := t.TempDir()
	a, err := app.New(model.PathsFrom(filepath.Join(tmp, "claude"), filepath.Join(tmp, "recall")))
	if err != nil {
		t.Fatal(err)
	}
	a.RetentionSet = true
	fb := &fakeBackend{sessions: threeSessions(t)}
	m := newModel(a, fb, RunOptions{})
	m.now = func() time.Time { return fixedNow }
	m.width, m.height = 100, 30
	saveMetaFn = func(model.Paths, *state.Meta) error { return nil }
	copyFn = func(string) error { return nil }
	dirExistsFn = dirExists
	tmuxAvailableFn = func(*app.App) (bool, string) { return true, "3.4" }
	tickFn = func(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd {
		return func() tea.Msg { return fn(fixedNow.Add(d)) }
	}
	t.Cleanup(func() {
		saveMetaFn = state.SaveMeta
		copyFn = copyToClipboard
		dirExistsFn = dirExists
		tmuxAvailableFn = tmuxAvailable
		tickFn = tea.Tick
	})
	m.Update(loadedMsg{})
	return m, fb
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "pgdown":
		return tea.KeyMsg{Type: tea.KeyPgDown}
	case "home":
		return tea.KeyMsg{Type: tea.KeyHome}
	case "end":
		return tea.KeyMsg{Type: tea.KeyEnd}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func press(m *Model, keys ...string) tea.Cmd {
	var last tea.Cmd
	for _, k := range keys {
		_, last = m.Update(key(k))
	}
	return last
}

// drain runs a command tree and returns the messages it produced without
// waiting on ticks.
func drain(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	switch v := msg.(type) {
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range v {
			out = append(out, drain(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func TestRenderRowWide(t *testing.T) {
	st := newStyles(false)
	list := threeSessions(t)
	got := renderRow(st, list[1], rowOpts{width: 100, now: fixedNow})
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("want two lines, got %d: %q", len(lines), got)
	}
	l1, l2 := lines[0], lines[1]
	for _, want := range []string{"Running", "Add calendar view", "alpha-project", "feat/calendar", "2h"} {
		if !strings.Contains(l1, want) {
			t.Errorf("line1 missing %q: %q", want, l1)
		}
	}
	for _, want := range []string{"render the hour grid", "wt", "+1 dirs", "fork"} {
		if !strings.Contains(l2, want) {
			t.Errorf("line2 missing %q: %q", want, l2)
		}
	}
	if w := len([]rune(l1)); w > 100 {
		t.Errorf("line1 wider than 100: %d", w)
	}
	if strings.Contains(got, "\x1b[") {
		t.Errorf("no-color row carries escape codes: %q", got)
	}
}

func TestRenderRowNarrowCollapses(t *testing.T) {
	st := newStyles(false)
	list := threeSessions(t)
	got := renderRow(st, list[0], rowOpts{width: 60, now: fixedNow, selected: true})
	lines := strings.Split(got, "\n")
	if strings.Contains(lines[0], "fix/login") {
		t.Errorf("narrow line1 should drop the branch column: %q", lines[0])
	}
	if !strings.Contains(lines[0], "Needs you") || !strings.Contains(lines[0], "3m") {
		t.Errorf("narrow line1 must keep state word and age: %q", lines[0])
	}
	if !strings.Contains(lines[1], "alpha-project") || !strings.Contains(lines[1], "PR#12") {
		t.Errorf("narrow line2 should carry project and badges: %q", lines[1])
	}
	if !strings.HasPrefix(lines[0], "> ") {
		t.Errorf("selected row should carry a cursor: %q", lines[0])
	}
	for _, l := range lines {
		if len([]rune(l)) > 60 {
			t.Errorf("line wider than 60: %q", l)
		}
	}
}

func TestRenderRowLabelPinAndSelect(t *testing.T) {
	st := newStyles(false)
	s := threeSessions(t)[2]
	s.Label = "readme"
	s.Pinned = true
	got := renderRow(st, s, rowOpts{width: 100, now: fixedNow, selectMode: true, marked: true, pending: "D"})
	l1 := strings.Split(got, "\n")[0]
	for _, want := range []string{"readme", "★", "[x]", "!", "Closed", "3d"} {
		if !strings.Contains(l1, want) {
			t.Errorf("missing %q in %q", want, l1)
		}
	}
	if !strings.Contains(got, "draft a README") {
		t.Errorf("second line should fall back to first prompt: %q", got)
	}
}

func TestStateColouring(t *testing.T) {
	st := newStyles(true)
	cases := map[model.State]string{
		model.StateNeedsYou:    string(colorRed),
		model.StateLiveIdle:    string(colorGreen),
		model.StateLiveBusy:    string(colorGreen),
		model.StateKept:        string(colorGreen),
		model.StateClosed:      string(colorDim),
		model.StateInterrupted: string(colorDim),
		model.StateGhost:       string(colorDimmer),
		model.StateStale:       string(colorYellow),
	}
	for state, want := range cases {
		fg := st.forState(state).GetForeground()
		if got := colorString(fg); got != want {
			t.Errorf("%s: foreground %q, want %q", state, got, want)
		}
		if w := state.Word(); w == "" {
			t.Errorf("%s has no word", state)
		}
	}
	plain := newStyles(false)
	for state := range cases {
		if colorString(plain.forState(state).GetForeground()) != "" {
			t.Errorf("NO_COLOR palette still colours %s", state)
		}
	}
	// Every state still paints its plain-text word.
	for state := range cases {
		row := renderRow(plain, &model.Session{ID: "deadbeef-0000", Title: "x", State: state, LastActive: fixedNow}, rowOpts{width: 90, now: fixedNow})
		if !strings.Contains(row, state.Word()) {
			t.Errorf("%s row lacks the state word: %q", state, row)
		}
	}
}

// A stale session still resumes; it must not read as Gone, and the row has
// to say how to keep it.
func TestStaleRowIsExpiringNotGone(t *testing.T) {
	plain := newStyles(false)
	sess := &model.Session{ID: "deadbeef-0000", Title: "old but resumable", Path: "/t", State: model.StateStale, LastActive: fixedNow.Add(-29 * 24 * time.Hour)}
	row := renderRow(plain, sess, rowOpts{width: 100, now: fixedNow})
	if strings.Contains(row, "Gone") {
		t.Errorf("stale row reads as Gone: %q", row)
	}
	if !strings.Contains(row, "◔ Expiring") {
		t.Errorf("stale row lacks the Expiring dot and word: %q", row)
	}
	if !strings.Contains(row, "a archives before deletion") {
		t.Errorf("stale row lacks the archive hint: %q", row)
	}
	ghost := renderRow(plain, &model.Session{ID: "deadbeef-0001", Title: "x", Ghost: true, State: model.StateGhost, LastActive: fixedNow}, rowOpts{width: 100, now: fixedNow})
	if !strings.Contains(ghost, "○ Gone") || strings.Contains(ghost, "a archives") {
		t.Errorf("ghost row changed: %q", ghost)
	}
	if stateDot(model.StateStale) == stateDot(model.StateGhost) || stateDot(model.StateStale) == stateDot(model.StateClosed) {
		t.Errorf("stale dot %q is not distinct", stateDot(model.StateStale))
	}
}

func colorString(c lipgloss.TerminalColor) string {
	if s, ok := c.(lipgloss.Color); ok {
		return string(s)
	}
	return ""
}

func TestNoColorEnv(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if !noColor() {
		t.Fatal("NO_COLOR=1 should disable colour")
	}
	m := newModel(nil, &fakeBackend{}, RunOptions{})
	if m.st.color {
		t.Fatal("model built with NO_COLOR must use the plain palette")
	}
	m.width, m.height = 100, 24
	if out := m.View(); strings.Contains(out, "\x1b[") {
		t.Errorf("NO_COLOR view carries escape codes")
	}
}

func TestKeyToAction(t *testing.T) {
	cases := map[string]action{
		"enter": actOpen, " ": actPreview, "/": actSearch, "tab": actScope, "?": actHelp, ":": actPalette,
		"n": actNewHere, "N": actNewInDir, "f": actFork, "K": actKeep, "O": actNone, "o": actNewTab,
		"r": actLabel, "p": actPin, "H": actHide, "h": actShowHidden, "t": actTag, "a": actArchive,
		"y": actCopyResume, "c": actCopyLink, "x": actStop, "D": actDelete, "S": actSetup, "R": actRefresh,
		"g": actGhosts, "q": actQuit, "esc": actQuit, "j": actDown, "k": actUp, "up": actUp, "down": actDown,
		"pgdown": actPageDown, "home": actHome, "end": actEnd, "V": actSelectMode, "u": actUndo, "z": actNone,
	}
	for k, want := range cases {
		if got := actionFor(key(k)); got != want {
			t.Errorf("key %q: got %d want %d", k, got, want)
		}
	}
	for _, i := range actionTable {
		if i.key == "" && !i.disabled {
			t.Errorf("action %d has no key and is enabled", i.act)
		}
	}
	if !actStop.destructive() || !actDelete.destructive() || actHide.destructive() {
		t.Error("destructive set is wrong")
	}
}

func TestLoadNavigationAndView(t *testing.T) {
	m, fb := newTestModel(t)
	if fb.loads != 0 {
		t.Fatalf("loadedMsg should not load again")
	}
	if len(m.rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(m.rows))
	}
	press(m, "j", "j", "j")
	if m.cursor != 2 {
		t.Errorf("cursor should clamp at 2, got %d", m.cursor)
	}
	press(m, "k")
	if m.cursor != 1 {
		t.Errorf("cursor = %d, want 1", m.cursor)
	}
	press(m, "home")
	if m.cursor != 0 {
		t.Errorf("home: cursor = %d", m.cursor)
	}
	press(m, "end")
	if m.cursor != 2 {
		t.Errorf("end: cursor = %d", m.cursor)
	}
	out := ansiRE.ReplaceAllString(m.View(), "")
	for _, want := range []string{"recall", "sessions", keyBar, "Needs you", "Running", "Closed", "Fix the login race"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q", want)
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if len([]rune(line)) > 100 {
			t.Errorf("view line wider than terminal: %q", line)
		}
	}
	// Init schedules the first scan plus both timers.
	var ticks int
	for _, msg := range drain(m.Init()) {
		switch msg.(type) {
		case tickLiveMsg, tickScanMsg:
			ticks++
		}
	}
	if fb.loads != 1 || ticks != 2 {
		t.Errorf("Init should load once and arm two timers, loads=%d ticks=%d", fb.loads, ticks)
	}
}

func TestTimersRefreshAndRescan(t *testing.T) {
	m, fb := newTestModel(t)
	_, cmd := m.Update(tickLiveMsg{})
	var sawTick bool
	for _, msg := range drain(cmd) {
		switch msg.(type) {
		case liveMsg:
			m.Update(msg)
		case tickLiveMsg:
			sawTick = true
		}
	}
	if fb.refreshes != 1 || !sawTick {
		t.Errorf("tickLive should probe once and reschedule, refreshes=%d tick=%v", fb.refreshes, sawTick)
	}
	if m.busy {
		t.Error("busy flag should clear after liveMsg")
	}
	_, cmd = m.Update(tickScanMsg{})
	sawTick = false
	for _, msg := range drain(cmd) {
		switch msg.(type) {
		case loadedMsg:
			m.Update(msg)
		case tickScanMsg:
			sawTick = true
		}
	}
	if fb.loads != 1 || !sawTick {
		t.Errorf("tickScan should load once and reschedule, loads=%d tick=%v", fb.loads, sawTick)
	}
	// While a load is in flight a second tick does not start another.
	m.busy = true
	_, cmd = m.Update(tickScanMsg{})
	for _, msg := range drain(cmd) {
		if _, ok := msg.(loadedMsg); ok {
			t.Error("busy model must not start a second load")
		}
	}
}

func TestOpenShowsCommandThenQuits(t *testing.T) {
	m, fb := newTestModel(t)
	_, cmd := m.Update(key("enter"))
	if len(fb.opened) != 1 || fb.opened[0].ID != m.rows[0].ID {
		t.Fatalf("Enter should open the selected session, opened=%d", len(fb.opened))
	}
	if !strings.HasPrefix(m.status, "$ cd ") || !strings.Contains(m.status, "claude --resume "+m.rows[0].ID) {
		t.Errorf("status should show the exact command, got %q", m.status)
	}
	if m.pendingAction == nil || m.pendingAction.Kind != "resume" {
		t.Fatal("pending action not recorded")
	}
	if cmd == nil {
		t.Fatal("expected a delayed run command")
	}
	// Deliver the delayed message directly instead of waiting a second.
	_, quit := m.Update(runActionMsg{act: m.pendingAction})
	if quit == nil {
		t.Fatal("in-place resume should quit")
	}
	if _, ok := quit().(tea.QuitMsg); !ok {
		t.Errorf("expected tea.QuitMsg, got %T", quit())
	}
	if m.pendingAction == nil {
		t.Error("pending action must survive quit so Run can execute it")
	}
}

func TestOpenVariantsPassOptions(t *testing.T) {
	m, fb := newTestModel(t)
	press(m, "f")
	press(m, "K")
	press(m, "o")
	if len(fb.openedOpts) != 3 {
		t.Fatalf("want 3 opens, got %d", len(fb.openedOpts))
	}
	if !fb.openedOpts[0].Fork || !fb.openedOpts[1].Keep || !fb.openedOpts[2].NewTab {
		t.Errorf("options not threaded: %+v", fb.openedOpts)
	}
	if !fb.openedOpts[0].DryRun {
		t.Error("RECALL_DRY_RUN=1 should set DryRun")
	}
	fb.openErr = errors.New("locked by another recall")
	press(m, "enter")
	if !strings.Contains(m.status, "locked by another recall") {
		t.Errorf("open error should reach the status bar: %q", m.status)
	}
}

// Enter resumes in this terminal (the TUI quits, claude is exec'ed) while o
// asks for a new tab, so the two keys never do the same thing.
func TestEnterResumesInPlaceAndOOpensTab(t *testing.T) {
	m, fb := newTestModel(t)
	t.Setenv("TERM_PROGRAM", "Apple_Terminal")
	press(m, "enter")
	press(m, "o")
	if len(fb.openedOpts) != 2 {
		t.Fatalf("want 2 opens, got %d", len(fb.openedOpts))
	}
	enter, tab := fb.openedOpts[0], fb.openedOpts[1]
	if !enter.InPlace || enter.NewTab {
		t.Errorf("Enter must ask for an in-place resume, got %+v", enter)
	}
	if !tab.NewTab || tab.InPlace {
		t.Errorf("o must ask for a new tab, got %+v", tab)
	}
	// The real planner honours InPlace even in Terminal.app.
	sess := m.rows[2]
	act, err := launch.Plan(sess, enter)
	if err != nil {
		t.Fatal(err)
	}
	if act.Script != "" || !needsTerminal(act) {
		t.Errorf("Enter in Terminal.app must plan an in-place resume, got script:\n%s", act.Script)
	}
	act, err = launch.Plan(sess, tab)
	if err != nil {
		t.Fatal(err)
	}
	if act.Script == "" || needsTerminal(act) {
		t.Errorf("o in Terminal.app must plan a new tab script")
	}
}

// The loss note is part of the status shown before the action runs and
// buys a longer look.
func TestSpawnShowsLossNoteLonger(t *testing.T) {
	m, _ := newTestModel(t)
	var delays []time.Duration
	tickFn = func(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd {
		delays = append(delays, d)
		return func() tea.Msg { return fn(fixedNow.Add(d)) }
	}
	plain := &launch.Action{Kind: "resume", SID: "s1", Cwd: "/tmp", Argv: []string{"claude", "--resume", "s1"}}
	m.spawn(plain)
	if m.status != "$ cd /tmp && claude --resume s1" {
		t.Errorf("status without note = %q", m.status)
	}
	noted := &launch.Action{Kind: "resume", SID: "s1", Cwd: "/tmp", Argv: []string{"claude", "--resume", "s1"}, Note: "2 background job(s) were lost; dropped --add-dir /gone (path missing)"}
	m.spawn(noted)
	if !strings.HasPrefix(m.status, "note: 2 background job(s) were lost; dropped --add-dir /gone (path missing)") || !strings.Contains(m.status, "$ cd /tmp && claude --resume s1") {
		t.Errorf("status with note = %q", m.status)
	}
	if len(delays) != 2 || delays[0] != spawnDelay || delays[1] != noteDelay {
		t.Errorf("delays = %v, want [%v %v]", delays, spawnDelay, noteDelay)
	}
	if noteDelay <= spawnDelay {
		t.Errorf("noteDelay %v must be longer than spawnDelay %v", noteDelay, spawnDelay)
	}
}

// A new-tab resume whose osascript fails must give the session lock back
// and tell the user how to get out of it; a successful hand-off releases it
// too because the other tab's claude cannot inherit it.
func TestNewTabFailureReleasesLockWithHint(t *testing.T) {
	m, fb := newTestModel(t)
	launchRunFn = func(a *launch.Action) error {
		return errors.New("osascript: System Events got an error: osascript is not allowed to send keystrokes")
	}
	t.Cleanup(func() { launchRunFn = launch.Run })
	act := &launch.Action{Kind: "resume", SID: "sid-1", Cwd: "/tmp", Argv: []string{"claude", "--resume", "sid-1"}, Script: "tell application \"Terminal\"", Description: "resume sid-1 (new Terminal tab)"}
	_, cmd := m.runAction(act)
	if cmd == nil {
		t.Fatal("script action should run inside the program")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); ok {
		t.Fatal("script action must not quit the UI")
	}
	if len(fb.released) != 1 || fb.released[0] != "sid-1" {
		t.Errorf("lock not released after failure: %v", fb.released)
	}
	m.Update(msg)
	for _, want := range []string{"not allowed to send keystrokes", "grant Terminal accessibility", "press Enter to resume here"} {
		if !strings.Contains(m.status, want) {
			t.Errorf("status %q lacks %q", m.status, want)
		}
	}

	fb.released = nil
	launchRunFn = func(a *launch.Action) error { return nil }
	it := &launch.Action{Kind: "resume", SID: "sid-2", Script: "tell application \"iTerm2\"", Description: "resume sid-2 (new iTerm2 tab)"}
	_, cmd = m.runAction(it)
	m.Update(cmd())
	if len(fb.released) != 1 || fb.released[0] != "sid-2" {
		t.Errorf("lock not released after a successful hand-off: %v", fb.released)
	}
	if m.status != "resume sid-2 (new iTerm2 tab)" {
		t.Errorf("status = %q", m.status)
	}
	if scriptHost(it) != "iTerm2" || scriptHost(act) != "Terminal" {
		t.Errorf("scriptHost wrong")
	}

	// Focus actions never touch a lock.
	fb.released = nil
	launchRunFn = func(a *launch.Action) error { return errors.New("no window") }
	_, cmd = m.runAction(&launch.Action{Kind: "focus", SID: "sid-3", Script: "x"})
	m.Update(cmd())
	if len(fb.released) != 0 {
		t.Errorf("focus must not release a lock: %v", fb.released)
	}
	if strings.Contains(m.status, "accessibility") {
		t.Errorf("focus failure must not carry the tab hint: %q", m.status)
	}
}

func TestFocusActionRunsInsideProgram(t *testing.T) {
	m, _ := newTestModel(t)
	ran := 0
	launchRunFn = func(a *launch.Action) error { ran++; return nil }
	t.Cleanup(func() { launchRunFn = launch.Run })
	act := &launch.Action{Kind: "focus", Script: "tell application \"Terminal\"", Description: "focused tab"}
	_, cmd := m.runAction(act)
	if cmd == nil {
		t.Fatal("focus should return a run command")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); ok {
		t.Fatal("focus must not quit the UI")
	}
	if ran != 1 {
		t.Errorf("launch.Run called %d times", ran)
	}
	m.Update(msg)
	if m.status != "focused tab" {
		t.Errorf("status = %q", m.status)
	}
	if needsTerminal(&launch.Action{Kind: "resume", Script: "osascript"}) {
		t.Error("new-tab resume runs via osascript and must not need the terminal")
	}
	if !needsTerminal(&launch.Action{Kind: "attach"}) {
		t.Error("attach needs the terminal")
	}
}

func TestConfirmTwiceDeleteAndUndo(t *testing.T) {
	m, fb := newTestModel(t)
	target := m.rows[0]
	press(m, "D")
	if m.pending == nil || m.pending.sid != target.ID {
		t.Fatal("first D should arm the confirm")
	}
	if target.Hidden {
		t.Fatal("first D must not delete")
	}
	if !strings.Contains(m.status, "press D again") {
		t.Errorf("status = %q", m.status)
	}
	// A different key cancels the pending confirm.
	press(m, "j")
	if m.pending != nil {
		t.Fatal("moving should cancel the confirm")
	}
	press(m, "k", "D", "D")
	if !target.Hidden || !m.app.Meta.IsHidden(target.ID) {
		t.Fatal("second D should trash and hide the session")
	}
	if len(m.app.Meta.Trash) != 1 || m.app.Meta.Trash[0].SID != target.ID {
		t.Fatalf("trash = %+v", m.app.Meta.Trash)
	}
	if len(m.rows) != 2 {
		t.Errorf("rows after delete = %d", len(m.rows))
	}
	press(m, "u")
	if target.Hidden || m.app.Meta.IsHidden(target.ID) || len(m.app.Meta.Trash) != 0 {
		t.Fatal("u should restore the deleted session")
	}
	if len(m.rows) != 3 {
		t.Errorf("rows after undo = %d", len(m.rows))
	}
	_ = fb
}

func TestConfirmExpires(t *testing.T) {
	m, _ := newTestModel(t)
	press(m, "x")
	if m.pending == nil {
		t.Fatal("x should arm")
	}
	first := m.pending.at
	m.now = func() time.Time { return fixedNow.Add(6 * time.Second) }
	press(m, "x")
	if m.pending == nil || m.pending.at.Equal(first) {
		t.Fatal("a late second press should re-arm, not fire")
	}
	m.Update(expirePendingMsg{at: m.pending.at})
	if m.pending != nil {
		t.Fatal("expire message should clear the pending confirm")
	}
}

func TestHidePinLabelTagPersist(t *testing.T) {
	m, _ := newTestModel(t)
	saves := 0
	saveMetaFn = func(model.Paths, *state.Meta) error { saves++; return nil }
	s := m.rows[0]

	cmd := press(m, "p")
	drain(cmd)
	if !s.Pinned || !m.app.Meta.IsPinned(s.ID) {
		t.Fatal("p should pin")
	}
	drain(press(m, "p"))
	if s.Pinned || m.app.Meta.IsPinned(s.ID) {
		t.Fatal("p again should unpin")
	}

	press(m, "r")
	if m.mode != modeLabel {
		t.Fatal("r should open the label prompt")
	}
	press(m, "b", "u", "g")
	drain(press(m, "enter"))
	if s.Label != "bug" || m.app.Meta.Labels[s.ID] != "bug" {
		t.Errorf("label = %q meta=%q", s.Label, m.app.Meta.Labels[s.ID])
	}

	press(m, "t")
	if m.mode != modeTag {
		t.Fatal("t should open the tag prompt")
	}
	press(m, "a", " ", "b")
	drain(press(m, "enter"))
	if strings.Join(s.Tags, ",") != "a,b" {
		t.Errorf("tags = %v", s.Tags)
	}

	drain(press(m, "H"))
	if !s.Hidden || len(m.rows) != 2 {
		t.Fatal("H should hide the row")
	}
	press(m, "h")
	if len(m.rows) != 3 {
		t.Fatal("h should show hidden rows")
	}
	if saves < 5 {
		t.Errorf("metadata saved %d times, want at least 5", saves)
	}
}

func TestSearchAndScope(t *testing.T) {
	m, _ := newTestModel(t)
	press(m, "/")
	if m.mode != modeSearch {
		t.Fatal("/ should enter search")
	}
	press(m, "l", "o", "g")
	if m.query != "log" {
		t.Errorf("query = %q", m.query)
	}
	press(m, "enter")
	if m.mode != modeList || m.query != "log" {
		t.Errorf("enter should keep the query and return to the list, mode=%d query=%q", m.mode, m.query)
	}
	press(m, "/")
	press(m, "esc")
	if m.query != "" {
		t.Errorf("esc should clear the query, got %q", m.query)
	}
	press(m, "tab")
	if m.scope != "project" {
		t.Errorf("tab should cycle to project, got %s", m.scope)
	}
	for i := 0; i < 5; i++ {
		press(m, "tab")
	}
	if m.scope != "all" {
		t.Errorf("scope should wrap to all, got %s", m.scope)
	}
	m2 := newModel(m.app, m.be, RunOptions{InitialQuery: "readme"})
	if m2.query != "readme" {
		t.Error("InitialQuery should seed the search")
	}
}

func TestHelpPaletteAndPreview(t *testing.T) {
	m, fb := newTestModel(t)
	press(m, "?")
	if m.mode != modeHelp {
		t.Fatal("? should open help")
	}
	out := ansiRE.ReplaceAllString(m.View(), "")
	for _, want := range []string{"open here", "copy claude.ai link", "remove from list (press twice)", "real rename"} {
		if !strings.Contains(out, want) {
			t.Errorf("help missing %q", want)
		}
	}
	press(m, "q")
	if m.mode != modeList {
		t.Fatal("any key should close help")
	}

	press(m, ":")
	if m.mode != modePalette {
		t.Fatal(": should open the palette")
	}
	press(m, "f", "o", "r", "k")
	items := paletteItems(m.input.Value())
	if len(items) != 1 || items[0].act != actFork {
		t.Fatalf("palette filter: %+v", items)
	}
	press(m, "enter")
	if len(fb.opened) != 1 || !fb.openedOpts[0].Fork {
		t.Error("palette should run the fork action")
	}
	if m.mode != modeList {
		t.Error("palette should close after running")
	}

	press(m, " ")
	if !m.preview {
		t.Fatal("space should toggle the preview")
	}
	m.width = 140
	out = ansiRE.ReplaceAllString(m.View(), "")
	if !strings.Contains(out, "pid 4242") || !strings.Contains(out, "Fix the login race") {
		t.Errorf("side-by-side preview should show the selected session details")
	}
	m.width = 90
	out = ansiRE.ReplaceAllString(m.View(), "")
	if !strings.Contains(out, "Terminal.app") {
		t.Errorf("full-screen preview should show live details")
	}
	if strings.Contains(out, "Add calendar view") {
		t.Errorf("full-screen preview should replace the list")
	}
	press(m, " ")
	if m.preview {
		t.Fatal("space again should close the preview")
	}
}

func TestMultiSelect(t *testing.T) {
	m, _ := newTestModel(t)
	press(m, "V")
	if !m.selectMode {
		t.Fatal("V enters select mode")
	}
	press(m, " ", " ")
	if len(m.marked) != 2 {
		t.Fatalf("marked = %d", len(m.marked))
	}
	out := ansiRE.ReplaceAllString(m.View(), "")
	if strings.Count(out, "[x]") != 2 || !strings.Contains(out, "select: 2 marked") {
		t.Errorf("view should show two checked rows")
	}
	drain(press(m, "H"))
	if len(m.rows) != 1 {
		t.Errorf("H should hide both marked rows, rows=%d", len(m.rows))
	}
	if m.undo == nil || len(m.undo.sids) != 2 {
		t.Fatal("undo should remember both")
	}
	drain(press(m, "u"))
	if len(m.rows) != 3 {
		t.Errorf("undo should restore both, rows=%d", len(m.rows))
	}
	press(m, "esc")
	if m.selectMode {
		t.Error("esc leaves select mode")
	}
}

func TestCwdMissingDialog(t *testing.T) {
	m, fb := newTestModel(t)
	press(m, "end")
	sess := m.selected()
	if sess.ID[:1] != "c" {
		t.Fatalf("expected the closed session, got %s", sess.ID)
	}
	press(m, "enter")
	if m.mode != modeCwd || m.dialog == nil {
		t.Fatal("missing WorkCwd should open the dialog")
	}
	if m.dialog.original != sess.Cwd {
		t.Errorf("original should fall back to the existing Cwd, got %q", m.dialog.original)
	}
	out := ansiRE.ReplaceAllString(m.View(), "")
	if !strings.Contains(out, "directory missing") || !strings.Contains(out, "home directory") {
		t.Errorf("dialog text missing: %q", out)
	}
	press(m, "c")
	if m.mode != modeList || len(fb.opened) != 0 {
		t.Fatal("c should cancel")
	}
	press(m, "enter", "o")
	if len(fb.opened) != 1 || fb.opened[0].WorkCwd != sess.Cwd {
		t.Fatalf("o should open in the original dir, got %+v", fb.opened)
	}
	press(m, "enter", "h")
	home, _ := os.UserHomeDir()
	if len(fb.opened) != 2 || fb.opened[1].WorkCwd != home {
		t.Fatalf("h should open in home, got %q", fb.opened[1].WorkCwd)
	}
	if sess.WorkCwd == home {
		t.Error("the real session must not be rewritten")
	}
}

func TestCopyResumeAndLink(t *testing.T) {
	m, _ := newTestModel(t)
	var copied []string
	copyFn = func(s string) error { copied = append(copied, s); return nil }
	press(m, "y")
	if len(copied) != 1 || !strings.Contains(copied[0], "claude --resume "+m.rows[0].ID) || !strings.HasPrefix(copied[0], "cd ") {
		t.Errorf("resume command = %v", copied)
	}
	press(m, "c")
	if len(copied) != 1 || !strings.Contains(m.status, "no claude.ai link") {
		t.Errorf("c without a link should explain, status=%q copied=%v", m.status, copied)
	}
	m.rows[0].BridgeURL = "https://claude.ai/code/session_abc"
	press(m, "c")
	if len(copied) != 2 || copied[1] != "https://claude.ai/code/session_abc" {
		t.Errorf("link copy = %v", copied)
	}
	copyFn = func(string) error { return errors.New("no clipboard") }
	press(m, "y")
	if !strings.Contains(m.status, "cd ") {
		t.Errorf("without a clipboard the command should be shown, status=%q", m.status)
	}
}

func TestNewSessionHereAndInDir(t *testing.T) {
	m, _ := newTestModel(t)
	m.app.ShellCwd = t.TempDir()
	press(m, "n")
	if m.pendingAction == nil || m.pendingAction.Kind != "new" || m.pendingAction.Cwd != m.app.ShellCwd {
		t.Fatalf("n should plan a new session in the shell cwd, got %+v", m.pendingAction)
	}
	m.pendingAction = nil
	press(m, "j", "N")
	if m.pendingAction == nil || m.pendingAction.Cwd != m.rows[1].WorkCwd {
		t.Fatalf("N should plan a new session in the row dir, got %+v", m.pendingAction)
	}
	if !strings.HasPrefix(m.status, "$ cd ") {
		t.Errorf("status = %q", m.status)
	}
}

func TestArchiveGoesThroughHook(t *testing.T) {
	m, _ := newTestModel(t)
	archiveFn = func(p model.Paths, s *model.Session, sidecars bool) (string, error) {
		return "/tmp/archive/" + s.ID, nil
	}
	t.Cleanup(func() { archiveFn = archiveDefault })
	cmd := press(m, "a")
	for _, msg := range drain(cmd) {
		m.Update(msg)
	}
	if !m.rows[0].Archived || !strings.Contains(m.status, "archived") {
		t.Errorf("archive result not applied: archived=%v status=%q", m.rows[0].Archived, m.status)
	}
}

func TestBannersAndStatus(t *testing.T) {
	m, _ := newTestModel(t)
	m.app.RetentionSet = false
	m.app.Degraded = true
	m.app.DegradedReason = "agents json unavailable"
	out := ansiRE.ReplaceAllString(m.View(), "")
	if !strings.Contains(out, "Retention is not set") || !strings.Contains(out, "agents json unavailable") {
		t.Errorf("banners missing from header")
	}
	cmd := m.setStatus("hello")
	if m.status != "hello" {
		t.Fatal("status not set")
	}
	m.Update(clearStatusMsg{at: m.statusUntil})
	if m.status != "" {
		t.Error("status should clear on its own timer")
	}
	_ = cmd
}

func TestGhostCannotOpen(t *testing.T) {
	m, fb := newTestModel(t)
	fb.sessions = append(fb.sessions, &model.Session{ID: "dddddddd-0000", Title: "ghost", Ghost: true, State: model.StateGhost, GhostPrompts: []string{"hello"}})
	press(m, "g")
	m.Update(loadedMsg{})
	if !m.showGhosts || len(m.rows) != 4 {
		t.Fatalf("g should include ghosts, rows=%d", len(m.rows))
	}
	press(m, "end", "enter")
	if len(fb.opened) != 0 || !strings.Contains(m.status, "ghost") {
		t.Errorf("ghosts must not open, status=%q", m.status)
	}
}

func TestHumanAgeAndHelpers(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{{30 * time.Second, "now"}, {5 * time.Minute, "5m"}, {3 * time.Hour, "3h"}, {30 * time.Hour, "yesterday"}, {5 * 24 * time.Hour, "5d"}}
	for _, c := range cases {
		if got := humanAge(fixedNow.Add(-c.d), fixedNow); got != c.want {
			t.Errorf("%v: got %q want %q", c.d, got, c.want)
		}
	}
	if got := truncate("abcdef", 4); got != "abc…" {
		t.Errorf("truncate = %q", got)
	}
	if got := oneLine("a\n  b\tc"); got != "a b c" {
		t.Errorf("oneLine = %q", got)
	}
	if got := resumeCommandText("/tmp/my dir", []string{"claude", "--resume", "x"}); got != "cd '/tmp/my dir' && claude --resume x" {
		t.Errorf("resumeCommandText = %q", got)
	}
}

// archiveDefault restores the production archive hook after tests.
var archiveDefault = archiveFn

// viewLines returns the plain-text lines of the current view.
func viewLines(m *Model) []string {
	return strings.Split(ansiRE.ReplaceAllString(m.View(), ""), "\n")
}

// The help overlay must fit the terminal: the title and the first entries
// are visible even at 80x24, and nothing spills past the height or width.
func TestHelpOverlayFitsSmallTerminals(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 40}, {60, 16}} {
		m, _ := newTestModel(t)
		m.width, m.height = size[0], size[1]
		press(m, "?")
		lines := viewLines(m)
		if len(lines) > m.height {
			t.Errorf("%dx%d: help view is %d lines tall", m.width, m.height, len(lines))
		}
		for _, l := range lines {
			if lipgloss.Width(l) > m.width {
				t.Errorf("%dx%d: help line wider than terminal: %q", m.width, m.height, l)
			}
		}
		out := strings.Join(lines, "\n")
		for _, want := range []string{"keys", "open here", "search", "any"} {
			if !strings.Contains(out, want) {
				t.Errorf("%dx%d: help overlay does not show %q:\n%s", m.width, m.height, want, out)
			}
		}
		if strings.Contains(out, "page up") {
			t.Errorf("%dx%d: navigation rows should fold into one line", m.width, m.height)
		}
		if !strings.Contains(out, "PgUp/PgDn") {
			t.Errorf("%dx%d: folded navigation line missing", m.width, m.height)
		}
		press(m, "q")
		if m.mode != modeList {
			t.Errorf("%dx%d: q should close help", m.width, m.height)
		}
	}
}

// When the table still overflows, j and k scroll it and the footer says so.
func TestHelpOverlayScrolls(t *testing.T) {
	m, _ := newTestModel(t)
	m.width, m.height = 60, 16
	press(m, "?")
	if m.helpMaxScroll() == 0 {
		t.Skip("no overflow at 60x16")
	}
	out := strings.Join(viewLines(m), "\n")
	if !strings.Contains(out, "j/k") {
		t.Errorf("overflowing help should advertise j/k:\n%s", out)
	}
	if !strings.Contains(out, "open here") {
		t.Errorf("first entry must be visible before scrolling")
	}
	press(m, "j")
	if m.mode != modeHelp || m.helpScroll != 1 {
		t.Fatalf("j should scroll the help, mode=%d scroll=%d", m.mode, m.helpScroll)
	}
	press(m, "pgdown")
	if m.helpScroll != 1+m.helpRows() {
		t.Errorf("PgDn should move one page, scroll=%d rows=%d", m.helpScroll, m.helpRows())
	}
	press(m, "end")
	if m.helpScroll != m.helpMaxScroll() {
		t.Errorf("End should reach the end, scroll=%d max=%d", m.helpScroll, m.helpMaxScroll())
	}
	out = strings.Join(viewLines(m), "\n")
	if !strings.Contains(out, "quit") {
		t.Errorf("last entries should be visible after scrolling:\n%s", out)
	}
	press(m, "k")
	if m.helpScroll != m.helpMaxScroll()-1 {
		t.Errorf("k should scroll up, scroll=%d", m.helpScroll)
	}
	press(m, "esc")
	if m.mode != modeList || m.helpScroll != 0 {
		t.Errorf("esc should close and reset, mode=%d scroll=%d", m.mode, m.helpScroll)
	}
}

// In select mode D describes the marked set, refuses an unmarked cursor
// row and removes exactly the marked rows.
func TestSelectModeDeleteNamesMarkedRows(t *testing.T) {
	m, _ := newTestModel(t)
	a, b, c := m.rows[0], m.rows[1], m.rows[2]
	press(m, "V", " ", " ")
	if !m.marked[a.ID] || !m.marked[b.ID] || m.cursor != 2 {
		t.Fatalf("Space twice should mark the first two rows, marked=%v cursor=%d", m.marked, m.cursor)
	}
	press(m, "D")
	if m.pending != nil {
		t.Fatal("D on an unmarked cursor row must not arm")
	}
	if !strings.Contains(m.status, "not marked") || !strings.Contains(m.status, c.Short()) {
		t.Errorf("status should say the cursor row is unmarked: %q", m.status)
	}
	press(m, "D")
	if a.Hidden || b.Hidden || c.Hidden {
		t.Fatal("nothing may be removed while the cursor row is unmarked")
	}
	press(m, "k", "D")
	if m.pending == nil {
		t.Fatal("D on a marked row should arm")
	}
	for _, want := range []string{"2 marked sessions", "transcripts are kept", "remove"} {
		if !strings.Contains(m.status, want) {
			t.Errorf("confirm text lacks %q: %q", want, m.status)
		}
	}
	out := ansiRE.ReplaceAllString(m.View(), "")
	if strings.Count(out, "!") < 2 {
		t.Errorf("both marked rows should carry the pending marker:\n%s", out)
	}
	drain(press(m, "D"))
	if !a.Hidden || !b.Hidden || c.Hidden {
		t.Errorf("second D should remove the marked rows only: a=%v b=%v c=%v", a.Hidden, b.Hidden, c.Hidden)
	}
	if !strings.Contains(m.status, "removed 2") || !strings.Contains(m.status, "transcripts kept") {
		t.Errorf("status = %q", m.status)
	}
	if info := infoFor(actDelete); !strings.Contains(info.label, "remove from list") {
		t.Errorf("delete label should say remove from list, got %q", info.label)
	}
}

// H toggles: a hidden or trashed row shown through h comes back with H.
func TestHideToggleRestoresHiddenAndTrashed(t *testing.T) {
	m, _ := newTestModel(t)
	s := m.rows[0]
	drain(press(m, "H"))
	if !s.Hidden || len(m.rows) != 2 {
		t.Fatal("H should hide")
	}
	press(m, "h")
	if len(m.rows) != 3 {
		t.Fatal("h should show hidden rows")
	}
	press(m, "home")
	for m.selected() != s {
		press(m, "j")
	}
	drain(press(m, "H"))
	if s.Hidden || m.app.Meta.IsHidden(s.ID) {
		t.Fatal("H on a hidden row should unhide it")
	}
	if !strings.Contains(m.status, "unhid 1") {
		t.Errorf("status = %q", m.status)
	}
	drain(press(m, "u"))
	if !s.Hidden || !m.app.Meta.IsHidden(s.ID) {
		t.Fatal("u should undo the unhide")
	}
	// A trashed row (D twice) comes back the same way, with its trash
	// entry cleared.
	drain(press(m, "H"))
	press(m, "D")
	drain(press(m, "D"))
	if !s.Hidden || len(m.app.Meta.Trash) != 1 {
		t.Fatalf("D twice should trash: hidden=%v trash=%d", s.Hidden, len(m.app.Meta.Trash))
	}
	if !m.showHidden {
		press(m, "h")
	}
	for m.selected() != s {
		press(m, "j")
	}
	drain(press(m, "H"))
	if s.Hidden || m.app.Meta.IsHidden(s.ID) || len(m.app.Meta.Trash) != 0 {
		t.Errorf("H should restore a trashed row: hidden=%v metaHidden=%v trash=%d", s.Hidden, m.app.Meta.IsHidden(s.ID), len(m.app.Meta.Trash))
	}
}

// O used to promise a new window while opening a tab; the binding is gone.
func TestNoNewWindowBinding(t *testing.T) {
	m, fb := newTestModel(t)
	press(m, "O")
	if len(fb.opened) != 0 {
		t.Error("O must not open anything")
	}
	for _, i := range actionTable {
		if strings.Contains(i.label, "window") {
			t.Errorf("action table still lists a window action: %+v", i)
		}
	}
	if _, ok := keyToAction["O"]; ok {
		t.Error("O is still bound")
	}
}

// Cycling Tab into the ghosts scope loads ghosts instead of showing an
// empty list.
func TestGhostScopeLoadsGhosts(t *testing.T) {
	m, fb := newTestModel(t)
	fb.sessions = append(fb.sessions, &model.Session{ID: "dddddddd-0000", Title: "ghost", Ghost: true, State: model.StateGhost, GhostPrompts: []string{"hello"}})
	var cmd tea.Cmd
	for m.scope != "ghosts" {
		cmd = press(m, "tab")
	}
	if !m.showGhosts {
		t.Fatal("entering the ghosts scope should switch ghosts on")
	}
	loads := fb.loads
	for _, msg := range drain(cmd) {
		m.Update(msg)
	}
	if fb.loads != loads+1 {
		t.Errorf("entering the ghosts scope should reload, loads %d -> %d", loads, fb.loads)
	}
	if len(m.rows) != 1 || !m.rows[0].Ghost {
		t.Errorf("ghost scope rows = %v", rowTitles(m))
	}
	// Turning ghosts off while in the scope explains itself.
	m.showGhosts = false
	m.refreshRows()
	out := ansiRE.ReplaceAllString(m.View(), "")
	if !strings.Contains(out, "no ghosts loaded, press g") {
		t.Errorf("empty ghosts scope should hint at g:\n%s", out)
	}
	// Tab from search mode does the same.
	m2, fb2 := newTestModel(t)
	press(m2, "/")
	for m2.scope != "ghosts" {
		cmd = press(m2, "tab")
	}
	if !m2.showGhosts || fb2.loads != 0 {
		t.Errorf("search Tab into ghosts: showGhosts=%v loads=%d", m2.showGhosts, fb2.loads)
	}
	for _, msg := range drain(cmd) {
		m2.Update(msg)
	}
	if fb2.loads != 1 {
		t.Errorf("search Tab into ghosts should reload, loads=%d", fb2.loads)
	}
}

// The key bar follows the mode, and K keeps its meaning in the preview.
func TestKeyBarFollowsMode(t *testing.T) {
	m, fb := newTestModel(t)
	bar := func() string {
		lines := viewLines(m)
		return lines[len(lines)-1]
	}
	if !strings.Contains(bar(), "Enter open") {
		t.Errorf("list bar = %q", bar())
	}
	press(m, "/")
	if b := bar(); !strings.Contains(b, "Enter apply") || !strings.Contains(b, "Esc clear") || strings.Contains(b, "Space preview") {
		t.Errorf("search bar = %q", b)
	}
	press(m, "esc")
	m.width = 80
	press(m, " ")
	if b := bar(); !strings.Contains(b, "Ctrl+n/Ctrl+p scroll") || !strings.Contains(b, "Space close") {
		t.Errorf("full-screen preview bar = %q", b)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	if m.previewScroll != 2 {
		t.Errorf("ctrl+n should scroll the preview, got %d", m.previewScroll)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	if m.previewScroll != 1 {
		t.Errorf("ctrl+p should scroll back, got %d", m.previewScroll)
	}
	press(m, "K")
	if len(fb.openedOpts) != 1 || !fb.openedOpts[0].Keep {
		t.Errorf("K in the preview must still open and keep, opens=%d", len(fb.openedOpts))
	}
	if m.previewScroll != 1 {
		t.Errorf("K must not scroll the preview, scroll=%d", m.previewScroll)
	}
	press(m, " ")
	press(m, "end", "enter")
	if m.mode != modeCwd {
		t.Fatal("expected the missing directory dialog")
	}
	if b := bar(); !strings.Contains(b, "o original") || !strings.Contains(b, "h home") || !strings.Contains(b, "c cancel") {
		t.Errorf("cwd dialog bar = %q", b)
	}
	out := strings.Join(viewLines(m), "\n")
	if !strings.Contains(out, "file paths from it will") {
		t.Errorf("cwd dialog should warn about paths:\n%s", out)
	}
	press(m, "c")
	press(m, "r")
	if b := bar(); !strings.Contains(b, "Enter save") {
		t.Errorf("label bar = %q", b)
	}
	press(m, "esc")
	press(m, "?")
	if b := bar(); !strings.Contains(b, "closes") {
		t.Errorf("help bar = %q", b)
	}
}

// The retention banner survives an 80-column terminal in full.
func TestRetentionBannerFitsNarrow(t *testing.T) {
	m, _ := newTestModel(t)
	m.app.RetentionSet = false
	m.width, m.height = 80, 24
	out := strings.Join(viewLines(m), "\n")
	if !strings.Contains(out, "S sets it.") {
		t.Errorf("banner lost its call to action at 80 columns:\n%s", out)
	}
	m.width = 50
	out = strings.Join(viewLines(m), "\n")
	if !strings.Contains(out, "S sets it.") {
		t.Errorf("banner should wrap at 50 columns:\n%s", out)
	}
	for _, l := range viewLines(m) {
		if lipgloss.Width(l) > 50 {
			t.Errorf("line wider than 50: %q", l)
		}
	}
	if len(viewLines(m)) > m.height {
		t.Errorf("wrapped banner pushed the view past the height")
	}
}

// Project and branch columns share their widths across the page so titles
// and ages line up, the title keeps at least 24 cells, and no line
// overflows.
func TestRowColumnsAlignAcrossPage(t *testing.T) {
	m, fb := newTestModel(t)
	long := &model.Session{
		ID: "dddddddd-1111-4222-8333-444444444444", Title: "A very long title that should keep at least twenty four cells",
		WorkCwd: "/tmp/a-very-long-project-name-here", Branch: "feature/some-really-long-branch-name",
		LastActive: fixedNow.Add(-5 * time.Hour), State: model.StateClosed, Path: "/tmp/d.jsonl",
	}
	fb.sessions = append(fb.sessions, long)
	for _, width := range []int{80, 100, 140} {
		m.width, m.height = width, 30
		m.refreshRows()
		lines := viewLines(m)
		var ageCols []int
		for _, l := range lines {
			if lipgloss.Width(l) > width {
				t.Errorf("%d cols: line overflows: %q", width, l)
			}
			for _, age := range []string{"3m", "2h", "3d", "5h"} {
				if strings.HasSuffix(strings.TrimRight(l, " "), age) {
					ageCols = append(ageCols, lipgloss.Width(strings.TrimRight(l, " ")))
				}
			}
		}
		if len(ageCols) != 4 {
			t.Fatalf("%d cols: expected 4 age cells, got %v", width, ageCols)
		}
		for _, c := range ageCols[1:] {
			if c != ageCols[0] {
				t.Errorf("%d cols: age column not aligned: %v", width, ageCols)
			}
		}
		// The long title keeps at least minTitleW cells.
		found := false
		for _, l := range lines {
			if strings.Contains(l, "A very long title that s") {
				found = true
			}
		}
		if !found {
			t.Errorf("%d cols: long title squeezed below %d cells:\n%s", width, minTitleW, strings.Join(lines, "\n"))
		}
	}
	// Columns pad so the project cells start at the same column.
	st := newStyles(false)
	cols := pageColumns([]*model.Session{m.rows[0], long}, 140)
	r1 := renderRow(st, m.rows[0], rowOpts{width: 140, now: fixedNow, cols: cols})
	r2 := renderRow(st, long, rowOpts{width: 140, now: fixedNow, cols: cols})
	col := func(row, cell string) int {
		i := strings.Index(row, cell)
		if i < 0 {
			return -1
		}
		return lipgloss.Width(row[:i])
	}
	if i, j := col(r1, "alpha-project"), col(r2, "a-very-long-project-n"); i != j || i < 0 {
		t.Errorf("project column differs between rows: %d vs %d\n%s\n%s", i, j, r1, r2)
	}
}

// K refuses to launch without tmux and explains how to get it.
func TestKeepNeedsTmux(t *testing.T) {
	m, fb := newTestModel(t)
	tmuxAvailableFn = func(*app.App) (bool, string) { return false, "" }
	press(m, "K")
	if len(fb.opened) != 0 {
		t.Fatal("K without tmux must not open")
	}
	if !strings.Contains(m.status, "tmux 3.2") || !strings.Contains(m.status, "brew install tmux") {
		t.Errorf("status = %q", m.status)
	}
	press(m, "?")
	out := strings.Join(viewLines(m), "\n")
	if !strings.Contains(out, "needs tmux") {
		t.Errorf("help should mark K as needing tmux:\n%s", out)
	}
	press(m, "esc")
	press(m, ":")
	press(m, "k", "e", "e", "p")
	out = strings.Join(viewLines(m), "\n")
	if !strings.Contains(out, "needs tmux") {
		t.Errorf("palette should mark keep as disabled:\n%s", out)
	}
	press(m, "enter")
	if len(fb.opened) != 0 || !strings.Contains(m.status, "tmux") {
		t.Errorf("palette keep must not open, status=%q", m.status)
	}
	// An old tmux is named in the message.
	m2, _ := newTestModel(t)
	m2.tmuxChecked = false
	tmuxAvailableFn = func(*app.App) (bool, string) { return false, "2.9" }
	press(m2, "K")
	if !strings.Contains(m2.status, "found 2.9") {
		t.Errorf("status = %q", m2.status)
	}
}
