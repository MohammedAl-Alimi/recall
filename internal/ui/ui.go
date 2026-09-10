package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/MohammedAl-Alimi/recall/internal/app"
	"github.com/MohammedAl-Alimi/recall/internal/archive"
	"github.com/MohammedAl-Alimi/recall/internal/index"
	"github.com/MohammedAl-Alimi/recall/internal/launch"
	"github.com/MohammedAl-Alimi/recall/internal/model"
	"github.com/MohammedAl-Alimi/recall/internal/state"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// Refresh cadence and confirm window.
const (
	liveRefreshEvery = 2 * time.Second
	rescanEvery      = 10 * time.Second
	confirmWindow    = 5 * time.Second
	spawnDelay       = 1 * time.Second
	// noteDelay replaces spawnDelay when the action carries a loss note
	// (dropped flags, lost background jobs, ...) so it can be read before
	// claude takes the screen.
	noteDelay       = 4 * time.Second
	statusFor       = 4 * time.Second
	sideBySideWidth = 120
	rowHeight       = 3
)

// RunOptions configures the TUI start.
type RunOptions struct {
	FromWidget   bool
	InitialQuery string
	// OnAction, when set, receives the action that still has to run after
	// the program exits (an in-place resume, attach or new session). When
	// nil, Run executes it with launch.Run itself.
	OnAction func(*launch.Action) error
}

// Run starts the Bubble Tea program and, after it exits, runs the pending
// terminal action (in-place resume, attach, new) if the user picked one.
func Run(a *app.App, opts RunOptions) error {
	act, err := RunWithAction(a, opts)
	if err != nil {
		return err
	}
	if act == nil {
		return nil
	}
	if opts.OnAction != nil {
		return opts.OnAction(act)
	}
	return launchRunFn(act)
}

// RunWithAction starts the program and returns the action that must run in
// the caller's terminal after the UI has exited, or nil.
func RunWithAction(a *app.App, opts RunOptions) (*launch.Action, error) {
	m := newModel(a, a, opts)
	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return nil, err
	}
	fm, ok := final.(*Model)
	if !ok {
		return nil, nil
	}
	if fm.fatal != nil {
		return nil, fm.fatal
	}
	return fm.pendingAction, nil
}

// Hooks that tests replace. Every side effect outside the model goes
// through one of these.
var (
	launchRunFn = launch.Run
	saveMetaFn  = state.SaveMeta
	archiveFn   = archive.Archive
	copyFn      = copyToClipboard
	stopFn      = stopSession
	dirExistsFn = dirExists
	execPathFn  = os.Executable
	// tmuxAvailableFn reports whether kept sessions (K) can be started.
	tmuxAvailableFn = tmuxAvailable
	// tickFn schedules delayed messages; tests swap it for an immediate one.
	tickFn = tea.Tick
)

// backend is the slice of *app.App the UI depends on, so tests can supply a
// fake populated with a few sessions.
type backend interface {
	Load(ctx context.Context, includeGhosts bool) error
	RefreshLive(ctx context.Context) error
	Visible(showHidden, showGhosts, showHeadless bool) []*model.Session
	Open(sess *model.Session, opts launch.Options) (*launch.Action, error)
	// ReleaseLock drops the session lock Open took, after a launch failed
	// or after the session was handed to another terminal tab.
	ReleaseLock(sid string)
	PreselectForCwd() *model.Session
	NotifyNeedsYou(prev map[string]model.State)
}

type mode int

const (
	modeList mode = iota
	modeSearch
	modePalette
	modeHelp
	modeLabel
	modeTag
	modeCwd
)

// pendingConfirm is the first press of a destructive key.
type pendingConfirm struct {
	act action
	sid string
	at  time.Time
}

// undoEntry remembers the last hide or delete so u can restore it.
type undoEntry struct {
	kind string
	sids []string
}

// cwdDialog asks where to start a session whose directory is gone.
type cwdDialog struct {
	sess    *model.Session
	opts    launch.Options
	missing string
	// original is sess.Cwd when it still exists and differs from missing.
	original string
}

// Model is the Bubble Tea model of the main screen.
type Model struct {
	app  *app.App
	be   backend
	st   styles
	opts RunOptions

	width, height int
	mode          mode

	rows    []*model.Session
	cursor  int
	offset  int
	query   string
	scope   index.Scope
	summary string

	showHidden, showGhosts, showHeadless bool
	preview                              bool
	previewScroll                        int

	selectMode bool
	marked     map[string]bool
	pending    *pendingConfirm
	undo       *undoEntry

	status      string
	statusUntil time.Time
	busy        bool
	loaded      bool
	preselected bool

	input      textinput.Model
	paletteIdx int
	dialog     *cwdDialog
	helpScroll int

	// tmuxChecked caches the result of tmuxAvailableFn for the run.
	tmuxChecked bool
	tmuxOK      bool
	tmuxVersion string

	pendingAction *launch.Action
	prevStates    map[string]model.State
	fatal         error

	now func() time.Time
}

// Messages.
type (
	loadedMsg        struct{ err error }
	liveMsg          struct{ err error }
	tickLiveMsg      struct{}
	tickScanMsg      struct{}
	clearStatusMsg   struct{ at time.Time }
	expirePendingMsg struct{ at time.Time }
	runActionMsg     struct{ act *launch.Action }
	actionDoneMsg    struct {
		err  error
		text string
	}
	archivedMsg struct {
		sid  string
		path string
		err  error
	}
)

// newModel builds a model. be is normally a but tests pass a fake.
func newModel(a *app.App, be backend, opts RunOptions) *Model {
	ti := textinput.New()
	ti.CharLimit = 200
	ti.Prompt = ""
	m := &Model{
		app:        a,
		be:         be,
		st:         newStyles(!noColor()),
		opts:       opts,
		width:      100,
		height:     30,
		scope:      index.ScopeAll,
		marked:     map[string]bool{},
		input:      ti,
		prevStates: map[string]model.State{},
		now:        time.Now,
		query:      opts.InitialQuery,
	}
	return m
}

// Init starts the first scan and the refresh timers.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.loadCmd(), tickLive(), tickScan())
}

func tickLive() tea.Cmd {
	return tickFn(liveRefreshEvery, func(time.Time) tea.Msg { return tickLiveMsg{} })
}

func tickScan() tea.Cmd {
	return tickFn(rescanEvery, func(time.Time) tea.Msg { return tickScanMsg{} })
}

func (m *Model) loadCmd() tea.Cmd {
	if m.busy {
		return nil
	}
	m.busy = true
	be, ghosts := m.be, m.showGhosts
	return func() tea.Msg {
		return loadedMsg{err: be.Load(context.Background(), ghosts)}
	}
}

func (m *Model) liveCmd() tea.Cmd {
	if m.busy {
		return nil
	}
	m.busy = true
	be := m.be
	return func() tea.Msg {
		return liveMsg{err: be.RefreshLive(context.Background())}
	}
}

// setStatus shows text in the status bar for a while.
func (m *Model) setStatus(text string) tea.Cmd {
	m.status = text
	at := m.now().Add(statusFor)
	m.statusUntil = at
	return tickFn(statusFor, func(time.Time) tea.Msg { return clearStatusMsg{at: at} })
}

// refreshRows rebuilds the visible list from the backend.
func (m *Model) refreshRows() {
	var curSID string
	if s := m.selected(); s != nil {
		curSID = s.ID
	}
	list := m.be.Visible(m.showHidden, m.showGhosts, m.showHeadless)
	list = index.Filter(list, m.query, m.scope, m.shellCwd())
	index.Sort(list)
	m.rows = list
	m.summary = index.Summary(list)

	m.cursor = 0
	if curSID != "" {
		for i, s := range list {
			if s.ID == curSID {
				m.cursor = i
				break
			}
		}
	}
	if !m.preselected && m.loaded {
		m.preselected = true
		if p := m.be.PreselectForCwd(); p != nil {
			for i, s := range list {
				if s.ID == p.ID {
					m.cursor = i
					break
				}
			}
		}
	}
	m.clampCursor()

	// Notify on transitions into needs_you and remember the new states.
	if len(m.prevStates) > 0 {
		m.be.NotifyNeedsYou(m.prevStates)
	}
	next := make(map[string]model.State, len(list))
	for _, s := range m.be.Visible(true, true, true) {
		next[s.ID] = s.State
	}
	m.prevStates = next
}

func (m *Model) shellCwd() string {
	if m.app != nil {
		return m.app.ShellCwd
	}
	return ""
}

func (m *Model) selected() *model.Session {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil
	}
	return m.rows[m.cursor]
}

// targets returns the sessions an action applies to: the marked rows in
// select mode, else the selected row.
func (m *Model) targets() []*model.Session {
	if m.selectMode && len(m.marked) > 0 {
		var out []*model.Session
		for _, s := range m.rows {
			if m.marked[s.ID] {
				out = append(out, s)
			}
		}
		return out
	}
	if s := m.selected(); s != nil {
		return []*model.Session{s}
	}
	return nil
}

func (m *Model) pageSize() int {
	h := m.listHeight() / rowHeight
	if h < 1 {
		h = 1
	}
	return h
}

func (m *Model) clampCursor() {
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	page := m.pageSize()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+page {
		m.offset = m.cursor - page + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m *Model) move(delta int) {
	m.cursor += delta
	m.clampCursor()
	m.previewScroll = 0
}

// Update handles every message.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.Width = max(10, m.width-10)
		m.clampCursor()
		return m, nil
	case loadedMsg:
		m.busy = false
		m.loaded = true
		m.refreshRows()
		if msg.err != nil {
			return m, m.setStatus("scan error: " + msg.err.Error())
		}
		return m, nil
	case liveMsg:
		m.busy = false
		m.refreshRows()
		if msg.err != nil {
			return m, m.setStatus("liveness error: " + msg.err.Error())
		}
		return m, nil
	case tickLiveMsg:
		return m, tea.Batch(m.liveCmd(), tickLive())
	case tickScanMsg:
		return m, tea.Batch(m.loadCmd(), tickScan())
	case clearStatusMsg:
		if msg.at.Equal(m.statusUntil) {
			m.status = ""
		}
		return m, nil
	case expirePendingMsg:
		if m.pending != nil && m.pending.at.Equal(msg.at) {
			m.pending = nil
		}
		return m, nil
	case runActionMsg:
		return m.runAction(msg.act)
	case actionDoneMsg:
		if msg.err != nil {
			return m, m.setStatus("failed: " + msg.err.Error())
		}
		if msg.text != "" {
			return m, m.setStatus(msg.text)
		}
		return m, nil
	case archivedMsg:
		if msg.err != nil {
			return m, m.setStatus("archive failed: " + msg.err.Error())
		}
		for _, s := range m.rows {
			if s.ID == msg.sid {
				s.Archived = true
				s.ArchivePath = msg.path
			}
		}
		return m, m.setStatus("archived " + model.ShortID(msg.sid) + " to " + msg.path)
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// handleKey dispatches on the current mode.
func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeHelp:
		return m.handleHelpKey(msg)
	case modeSearch:
		return m.handleSearchKey(msg)
	case modePalette:
		return m.handlePaletteKey(msg)
	case modeLabel, modeTag:
		return m.handleInputKey(msg)
	case modeCwd:
		return m.handleCwdKey(msg)
	}
	return m.handleListKey(msg)
}

func (m *Model) handleListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	act := actionFor(msg)
	if m.preview {
		// Preview scrolling uses Ctrl+n/Ctrl+p only, so every letter key
		// (including K for keep) keeps its documented action.
		switch msg.String() {
		case "ctrl+n":
			m.previewScroll++
			return m, nil
		case "ctrl+p":
			if m.previewScroll > 0 {
				m.previewScroll--
			}
			return m, nil
		}
	}
	return m.perform(act)
}

// handleHelpKey scrolls the help overlay when it overflows and closes it on
// any other key.
func (m *Model) handleHelpKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	maxScroll := m.helpMaxScroll()
	if maxScroll > 0 {
		switch msg.String() {
		case "j", "down":
			m.helpScroll = min(maxScroll, m.helpScroll+1)
			return m, nil
		case "k", "up":
			m.helpScroll = max(0, m.helpScroll-1)
			return m, nil
		case "pgdown", "ctrl+d":
			m.helpScroll = min(maxScroll, m.helpScroll+m.helpRows())
			return m, nil
		case "pgup", "ctrl+u":
			m.helpScroll = max(0, m.helpScroll-m.helpRows())
			return m, nil
		case "home":
			m.helpScroll = 0
			return m, nil
		case "end":
			m.helpScroll = maxScroll
			return m, nil
		}
	}
	m.mode = modeList
	m.helpScroll = 0
	return m, nil
}

// perform executes an action from a key or the palette.
func (m *Model) perform(act action) (tea.Model, tea.Cmd) {
	// Any non-destructive key cancels a pending confirm.
	if m.pending != nil && !act.destructive() {
		m.pending = nil
	}
	switch act {
	case actNone:
		return m, nil
	case actQuit:
		if m.selectMode {
			m.selectMode = false
			m.marked = map[string]bool{}
			return m, nil
		}
		if m.preview && m.width < sideBySideWidth {
			m.preview = false
			return m, nil
		}
		if m.query != "" && m.mode == modeList {
			m.query = ""
			m.refreshRows()
			return m, nil
		}
		return m, tea.Quit
	case actUp:
		m.move(-1)
	case actDown:
		m.move(1)
	case actPageUp:
		m.move(-m.pageSize())
	case actPageDown:
		m.move(m.pageSize())
	case actHome:
		m.move(-len(m.rows))
	case actEnd:
		m.move(len(m.rows))
	case actPreview:
		if m.selectMode {
			if s := m.selected(); s != nil {
				if m.marked[s.ID] {
					delete(m.marked, s.ID)
				} else {
					m.marked[s.ID] = true
				}
				m.move(1)
			}
			return m, nil
		}
		m.preview = !m.preview
		m.previewScroll = 0
	case actSearch:
		m.mode = modeSearch
		m.input.Prompt = "/ "
		m.input.Placeholder = "title, prompt, branch, id, state:kept, since:7d, project:name"
		m.input.SetValue(m.query)
		m.input.CursorEnd()
		return m, m.input.Focus()
	case actScope:
		return m, m.cycleScopeCmd()
	case actHelp:
		m.mode = modeHelp
		m.helpScroll = 0
	case actPalette:
		m.mode = modePalette
		m.paletteIdx = 0
		m.input.Prompt = ": "
		m.input.Placeholder = "command"
		m.input.SetValue("")
		return m, m.input.Focus()
	case actSelectMode:
		m.selectMode = !m.selectMode
		if !m.selectMode {
			m.marked = map[string]bool{}
		}
	case actShowHidden:
		m.showHidden = !m.showHidden
		m.refreshRows()
		if m.showHidden {
			return m, m.setStatus("showing hidden sessions")
		}
		return m, m.setStatus("hidden sessions folded away")
	case actGhosts:
		m.showGhosts = !m.showGhosts
		return m, m.loadCmd()
	case actRefresh:
		return m, tea.Batch(m.loadCmd(), m.setStatus("refreshing"))
	case actUndo:
		return m, m.doUndo()
	case actSetup:
		return m.doSetup()
	case actNewHere:
		return m.newSession(m.shellCwd())
	case actRenameReal:
		return m, m.setStatus("real rename needs a claude that supports it; use r for a recall label")
	}
	if act.needsSession() {
		sess := m.selected()
		if sess == nil {
			return m, m.setStatus("no session selected")
		}
		return m.performOn(act, sess)
	}
	return m, nil
}

// performOn runs an action that needs the selected session.
func (m *Model) performOn(act action, sess *model.Session) (tea.Model, tea.Cmd) {
	switch act {
	case actOpen:
		// Enter resumes right here: the TUI quits and claude takes over this
		// terminal. Live tiers (focus, attach) are unaffected. A new tab is
		// reserved for o.
		o := m.baseOptions()
		o.InPlace = true
		return m.open(sess, o)
	case actNewTab:
		o := m.baseOptions()
		o.NewTab = true
		return m.open(sess, o)
	case actFork:
		o := m.baseOptions()
		o.Fork = true
		return m.open(sess, o)
	case actKeep:
		if !m.tmuxAvailable() {
			return m, m.setStatus(m.tmuxMissingText())
		}
		o := m.baseOptions()
		o.Keep = true
		return m.open(sess, o)
	case actNewInDir:
		return m.newSession(sessionDir(sess))
	case actLabel:
		m.mode = modeLabel
		m.input.Prompt = "label: "
		m.input.Placeholder = "short name, empty clears"
		m.input.SetValue(sess.Label)
		m.input.CursorEnd()
		return m, m.input.Focus()
	case actTag:
		m.mode = modeTag
		m.input.Prompt = "tags: "
		m.input.Placeholder = "space separated"
		m.input.SetValue(strings.Join(sess.Tags, " "))
		m.input.CursorEnd()
		return m, m.input.Focus()
	case actPin:
		return m, m.togglePin(sess)
	case actHide:
		return m, m.toggleHide(m.targets())
	case actArchive:
		return m, m.archive(sess)
	case actCopyResume:
		cwd, argv, _ := launch.BuildResume(sess, m.baseOptions())
		if len(argv) == 0 {
			argv = []string{"claude", "--resume", sess.ID}
		}
		text := resumeCommandText(cwd, argv)
		return m, m.copy(text, "resume command")
	case actCopyLink:
		link := claudeLink(sess)
		if link == "" {
			return m, m.setStatus("no claude.ai link recorded for " + sess.Short())
		}
		return m, m.copy(link, "claude.ai link")
	case actStop, actDelete:
		return m, m.confirmTwice(act, sess)
	}
	return m, nil
}

func (m *Model) baseOptions() launch.Options {
	return launch.Options{
		DryRun:      os.Getenv("RECALL_DRY_RUN") == "1",
		TermProgram: os.Getenv("TERM_PROGRAM"),
	}
}

func (m *Model) cycleScope() {
	for i, s := range index.Scopes {
		if s == m.scope {
			m.scope = index.Scopes[(i+1)%len(index.Scopes)]
			return
		}
	}
	m.scope = index.ScopeAll
}

// cycleScopeCmd moves to the next scope and refreshes. Entering the ghosts
// scope loads ghosts when they are not shown yet, so the list is never
// empty just because g was not pressed.
func (m *Model) cycleScopeCmd() tea.Cmd {
	m.cycleScope()
	if m.scope == index.ScopeGhosts && !m.showGhosts {
		m.showGhosts = true
		m.refreshRows()
		return tea.Batch(m.loadCmd(), m.setStatus("loading ghosts"))
	}
	m.refreshRows()
	return nil
}

// tmuxAvailable reports (once per run) whether kept sessions can start.
func (m *Model) tmuxAvailable() bool {
	if !m.tmuxChecked {
		m.tmuxChecked = true
		m.tmuxOK, m.tmuxVersion = tmuxAvailableFn(m.app)
	}
	return m.tmuxOK
}

// tmuxMissingText explains why K is unavailable.
func (m *Model) tmuxMissingText() string {
	if m.tmuxVersion != "" {
		return "K needs tmux 3.2 or newer (found " + m.tmuxVersion + "); brew install tmux"
	}
	return "K needs tmux 3.2 or newer; brew install tmux"
}

// actionDisabled reports whether an action is unavailable on this machine.
func (m *Model) actionDisabled(a action) bool {
	return a == actKeep && !m.tmuxAvailable()
}

// tmuxAvailable is the production tmuxAvailableFn.
func tmuxAvailable(a *app.App) (bool, string) {
	if a == nil || a.Tmux == nil {
		return false, ""
	}
	return a.Tmux.Available()
}

// open plans the launch through the backend, shows the exact command for a
// second and then runs it (or quits and hands it back).
func (m *Model) open(sess *model.Session, opts launch.Options) (tea.Model, tea.Cmd) {
	if sess.Ghost {
		return m, m.setStatus("ghost session: no transcript to resume; y copies the prompts")
	}
	if sess.State == "" || !sess.State.IsLive() {
		dir := sessionDir(sess)
		if dir != "" && !dirExistsFn(dir) {
			d := &cwdDialog{sess: sess, opts: opts, missing: dir}
			if sess.Cwd != "" && sess.Cwd != dir && dirExistsFn(sess.Cwd) {
				d.original = sess.Cwd
			}
			m.dialog = d
			m.mode = modeCwd
			return m, nil
		}
	}
	act, err := m.be.Open(sess, opts)
	if err != nil {
		return m, m.setStatus("cannot open: " + err.Error())
	}
	if act == nil {
		return m, m.setStatus("nothing to do")
	}
	return m.spawn(act)
}

// spawn shows the command in the status bar for spawnDelay, then runs it.
// When the action carries a loss note (dropped flags, lost background jobs,
// a missing directory) the note leads the status line and the wait grows to
// noteDelay so it can be read before claude takes the screen.
func (m *Model) spawn(act *launch.Action) (tea.Model, tea.Cmd) {
	m.pendingAction = act
	text := spawnStatusText(act)
	m.status = text
	m.statusUntil = m.now().Add(time.Hour)
	if act.Kind == "print" {
		m.pendingAction = nil
		msg := act.Description
		if act.Note != "" {
			msg += " (" + act.Note + ")"
		}
		return m, m.setStatus(msg)
	}
	delay := spawnDelay
	if act.Note != "" {
		delay = noteDelay
	}
	return m, tickFn(delay, func(time.Time) tea.Msg { return runActionMsg{act: act} })
}

// spawnStatusText is the status shown while an action waits to run: the
// exact command, led by the loss note when there is one so the warning is
// never cut off by a long command line.
func spawnStatusText(act *launch.Action) string {
	text := actionCommandText(act)
	if act == nil || act.Note == "" || act.Kind == "print" {
		return text
	}
	return "note: " + act.Note + "  " + text
}

// runAction either quits so the caller can exec in the terminal, or runs
// the action in the background (osascript focus, new tab) and stays open.
//
// A resume that runs through osascript (new tab) holds the session lock in
// this process while claude lives in another tab, and a failed osascript
// (no Accessibility permission for the terminal) would keep it forever, so
// the lock is released either way once the script has run.
func (m *Model) runAction(act *launch.Action) (tea.Model, tea.Cmd) {
	if needsTerminal(act) {
		return m, tea.Quit
	}
	m.pendingAction = nil
	be := m.be
	return m, func() tea.Msg {
		err := launchRunFn(act)
		if act.SID != "" && act.Script != "" && act.Kind != "focus" && be != nil {
			be.ReleaseLock(act.SID)
		}
		if err != nil && act.Script != "" && act.Kind != "focus" {
			return actionDoneMsg{err: fmt.Errorf("%w; grant %s accessibility in System Settings, or press Enter to resume here", err, scriptHost(act))}
		}
		text := ""
		if err == nil {
			text = act.Description
			if text == "" {
				text = "done: " + act.Kind
			}
		}
		return actionDoneMsg{err: err, text: text}
	}
}

// scriptHost names the terminal app an osascript action drives, for hints.
func scriptHost(act *launch.Action) string {
	if act != nil && strings.Contains(act.Script, `"iTerm2"`) {
		return "iTerm2"
	}
	return "Terminal"
}

// needsTerminal reports whether an action must take over the terminal after
// the UI exits.
func needsTerminal(act *launch.Action) bool {
	if act == nil {
		return false
	}
	if act.Script != "" {
		return false
	}
	switch act.Kind {
	case "resume", "attach", "new":
		return true
	}
	return false
}

// newSession plans a brand new claude session in dir.
func (m *Model) newSession(dir string) (tea.Model, tea.Cmd) {
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = home
	}
	if !dirExistsFn(dir) {
		return m, m.setStatus("directory missing: " + dir)
	}
	act := &launch.Action{
		Kind:        "new",
		Cwd:         dir,
		Argv:        []string{"claude"},
		Description: "new session in " + dir,
	}
	return m.spawn(act)
}

func (m *Model) doSetup() (tea.Model, tea.Cmd) {
	if os.Getenv("RECALL_DRY_RUN") == "1" {
		return m, m.setStatus("dry run: would run recall setup")
	}
	exe, err := execPathFn()
	if err != nil {
		return m, m.setStatus("cannot locate recall binary: " + err.Error())
	}
	c := exec.Command(exe, "setup")
	return m, tea.ExecProcess(c, func(err error) tea.Msg {
		if err != nil {
			return actionDoneMsg{err: err}
		}
		return tickScanMsg{}
	})
}

// confirmTwice arms or fires a destructive action.
func (m *Model) confirmTwice(act action, sess *model.Session) tea.Cmd {
	now := m.now()
	key := infoFor(act).key
	what := sess.Short()
	if act == actDelete && m.selectMode && len(m.marked) > 0 {
		// The second press removes every marked row, so the prompt must
		// describe the marked set and the cursor row must be part of it.
		if !m.marked[sess.ID] {
			m.pending = nil
			return m.setStatus(fmt.Sprintf("%s is not marked: Space marks it, or press V to leave select mode", sess.Short()))
		}
		what = fmt.Sprintf("%d marked sessions", len(m.targets()))
	}
	if m.pending != nil && m.pending.act == act && m.pending.sid == sess.ID && now.Sub(m.pending.at) <= confirmWindow {
		m.pending = nil
		if act == actStop {
			return m.stop(sess)
		}
		return m.trash(m.targets())
	}
	m.pending = &pendingConfirm{act: act, sid: sess.ID, at: now}
	text := fmt.Sprintf("press %s again within 5s to stop %s", key, what)
	if act == actDelete {
		text = fmt.Sprintf("press %s again within 5s to remove %s from the list (transcripts are kept)", key, what)
	}
	at := now
	return tea.Batch(
		m.setStatus(text),
		tickFn(confirmWindow, func(time.Time) tea.Msg { return expirePendingMsg{at: at} }),
	)
}

func (m *Model) stop(sess *model.Session) tea.Cmd {
	a := m.app
	return func() tea.Msg {
		if err := stopFn(a, sess); err != nil {
			return actionDoneMsg{err: err}
		}
		return actionDoneMsg{text: "stopped " + sess.Short()}
	}
}

// trash soft-deletes: the transcript is never touched, the session is
// hidden and remembered in the trash list so u can bring it back.
func (m *Model) trash(list []*model.Session) tea.Cmd {
	if len(list) == 0 || m.app == nil || m.app.Meta == nil {
		return nil
	}
	var sids []string
	for _, s := range list {
		m.app.Meta.TrashAdd(s.ID)
		m.app.Meta.Hide(s.ID)
		s.Hidden = true
		sids = append(sids, s.ID)
	}
	m.undo = &undoEntry{kind: "delete", sids: sids}
	m.marked = map[string]bool{}
	m.refreshRows()
	return tea.Batch(m.saveMeta(), m.setStatus(fmt.Sprintf("removed %d from the list, transcripts kept (u to undo)", len(sids))))
}

// toggleHide hides the targets, or unhides them when every target is
// already hidden (or trashed), so H works both ways after a restart.
func (m *Model) toggleHide(list []*model.Session) tea.Cmd {
	if len(list) == 0 {
		return nil
	}
	allHidden := true
	for _, s := range list {
		if !s.Hidden {
			allHidden = false
			break
		}
	}
	if allHidden {
		return m.unhide(list)
	}
	return m.hide(list)
}

// unhide clears the hidden flag and the trash entry of every session.
func (m *Model) unhide(list []*model.Session) tea.Cmd {
	if len(list) == 0 || m.app == nil || m.app.Meta == nil {
		return nil
	}
	var sids []string
	for _, s := range list {
		m.app.Meta.Unhide(s.ID)
		m.app.Meta.TrashRestore(s.ID)
		s.Hidden = false
		sids = append(sids, s.ID)
	}
	m.undo = &undoEntry{kind: "unhide", sids: sids}
	m.marked = map[string]bool{}
	m.refreshRows()
	return tea.Batch(m.saveMeta(), m.setStatus(fmt.Sprintf("unhid %d (u to undo)", len(sids))))
}

func (m *Model) hide(list []*model.Session) tea.Cmd {
	if len(list) == 0 || m.app == nil || m.app.Meta == nil {
		return nil
	}
	var sids []string
	for _, s := range list {
		m.app.Meta.Hide(s.ID)
		s.Hidden = true
		sids = append(sids, s.ID)
	}
	m.undo = &undoEntry{kind: "hide", sids: sids}
	m.marked = map[string]bool{}
	m.refreshRows()
	return tea.Batch(m.saveMeta(), m.setStatus(fmt.Sprintf("hid %d (u to undo)", len(sids))))
}

func (m *Model) doUndo() tea.Cmd {
	if m.undo == nil || m.app == nil || m.app.Meta == nil {
		return m.setStatus("nothing to undo")
	}
	u := m.undo
	m.undo = nil
	all := m.be.Visible(true, true, true)
	for _, sid := range u.sids {
		if u.kind == "unhide" {
			m.app.Meta.Hide(sid)
		} else {
			m.app.Meta.Unhide(sid)
		}
		if u.kind == "delete" {
			m.app.Meta.TrashRestore(sid)
		}
		for _, s := range all {
			if s.ID == sid {
				s.Hidden = u.kind == "unhide"
			}
		}
	}
	m.refreshRows()
	if u.kind == "unhide" {
		return tea.Batch(m.saveMeta(), m.setStatus(fmt.Sprintf("hid %d again", len(u.sids))))
	}
	return tea.Batch(m.saveMeta(), m.setStatus(fmt.Sprintf("restored %d", len(u.sids))))
}

func (m *Model) togglePin(sess *model.Session) tea.Cmd {
	if m.app == nil || m.app.Meta == nil {
		return nil
	}
	if sess.Pinned {
		m.app.Meta.Unpin(sess.ID)
		sess.Pinned = false
	} else {
		m.app.Meta.Pin(sess.ID)
		sess.Pinned = true
	}
	m.refreshRows()
	word := "pinned"
	if !sess.Pinned {
		word = "unpinned"
	}
	return tea.Batch(m.saveMeta(), m.setStatus(word+" "+sess.Short()))
}

func (m *Model) archive(sess *model.Session) tea.Cmd {
	if sess.Ghost || sess.Path == "" {
		return m.setStatus("nothing to archive for " + sess.Short())
	}
	p := m.app.Paths
	return func() tea.Msg {
		path, err := archiveFn(p, sess, false)
		return archivedMsg{sid: sess.ID, path: path, err: err}
	}
}

func (m *Model) copy(text, what string) tea.Cmd {
	if err := copyFn(text); err != nil {
		return m.setStatus(what + ": " + text)
	}
	return m.setStatus("copied " + what + ": " + truncate(text, 60))
}

func (m *Model) saveMeta() tea.Cmd {
	if m.app == nil || m.app.Meta == nil {
		return nil
	}
	p, meta := m.app.Paths, m.app.Meta
	return func() tea.Msg {
		if err := saveMetaFn(p, meta); err != nil {
			return actionDoneMsg{err: errors.New("saving metadata: " + err.Error())}
		}
		return nil
	}
}

// Search input.
func (m *Model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.mode = modeList
		m.input.Blur()
		return m, nil
	case "esc", "ctrl+c":
		m.mode = modeList
		m.input.Blur()
		m.query = ""
		m.refreshRows()
		return m, nil
	case "tab":
		return m, m.cycleScopeCmd()
	case "up":
		m.move(-1)
		return m, nil
	case "down":
		m.move(1)
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if v := m.input.Value(); v != m.query {
		m.query = v
		m.refreshRows()
	}
	return m, cmd
}

// Label and tag inputs.
func (m *Model) handleInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.mode = modeList
		m.input.Blur()
		return m, nil
	case "enter":
		sess := m.selected()
		v := strings.TrimSpace(m.input.Value())
		which := m.mode
		m.mode = modeList
		m.input.Blur()
		if sess == nil || m.app == nil || m.app.Meta == nil {
			return m, nil
		}
		if which == modeLabel {
			m.app.Meta.SetLabel(sess.ID, v)
			sess.Label = v
			return m, tea.Batch(m.saveMeta(), m.setStatus("label set for "+sess.Short()))
		}
		tags := strings.Fields(v)
		if m.app.Meta.Tags == nil {
			m.app.Meta.Tags = map[string][]string{}
		}
		if len(tags) == 0 {
			delete(m.app.Meta.Tags, sess.ID)
		} else {
			m.app.Meta.Tags[sess.ID] = tags
		}
		sess.Tags = tags
		return m, tea.Batch(m.saveMeta(), m.setStatus("tags set for "+sess.Short()))
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// Command palette.
func (m *Model) handlePaletteKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := paletteItems(m.input.Value())
	switch msg.String() {
	case "esc", "ctrl+c":
		m.mode = modeList
		m.input.Blur()
		return m, nil
	case "up", "ctrl+p":
		if m.paletteIdx > 0 {
			m.paletteIdx--
		}
		return m, nil
	case "down", "ctrl+n", "tab":
		if m.paletteIdx < len(items)-1 {
			m.paletteIdx++
		}
		return m, nil
	case "enter":
		m.mode = modeList
		m.input.Blur()
		if m.paletteIdx >= len(items) {
			return m, nil
		}
		it := items[m.paletteIdx]
		if it.disabled {
			return m, m.setStatus(it.label + ": not available")
		}
		if m.actionDisabled(it.act) {
			return m, m.setStatus(m.tmuxMissingText())
		}
		return m.perform(it.act)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if n := len(paletteItems(m.input.Value())); m.paletteIdx >= n {
		m.paletteIdx = max(0, n-1)
	}
	return m, cmd
}

// paletteItems filters the action table for the palette.
func paletteItems(filter string) []actionInfo {
	f := strings.ToLower(strings.TrimSpace(filter))
	var out []actionInfo
	for _, i := range actionTable {
		if !i.palette {
			continue
		}
		if f == "" || strings.Contains(strings.ToLower(i.label), f) || strings.EqualFold(i.key, f) {
			out = append(out, i)
		}
	}
	return out
}

// Missing directory dialog.
func (m *Model) handleCwdKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	d := m.dialog
	if d == nil {
		m.mode = modeList
		return m, nil
	}
	switch msg.String() {
	case "esc", "c", "q", "ctrl+c":
		m.mode = modeList
		m.dialog = nil
		return m, m.setStatus("cancelled")
	case "o", "1":
		if d.original == "" {
			return m, nil
		}
		return m.openIn(d, d.original)
	case "h", "2":
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return m, m.setStatus("home directory unknown")
		}
		return m.openIn(d, home)
	}
	return m, nil
}

// openIn resumes the dialog's session from dir by rewriting the working
// directories on a copy of the session.
func (m *Model) openIn(d *cwdDialog, dir string) (tea.Model, tea.Cmd) {
	m.mode = modeList
	m.dialog = nil
	cp := *d.sess
	cp.WorkCwd, cp.LastCwd, cp.Cwd = dir, dir, dir
	act, err := m.be.Open(&cp, d.opts)
	if err != nil {
		return m, m.setStatus("cannot open: " + err.Error())
	}
	if act == nil {
		return m, m.setStatus("nothing to do")
	}
	return m.spawn(act)
}

// Helpers.

func sessionDir(sess *model.Session) string {
	for _, c := range []string{sess.WorkCwd, sess.LastCwd, sess.Cwd} {
		if c != "" {
			return c
		}
	}
	return ""
}

func dirExists(dir string) bool {
	fi, err := os.Stat(dir)
	return err == nil && fi.IsDir()
}

// actionCommandText is the exact command shown before an action runs.
func actionCommandText(act *launch.Action) string {
	if act == nil {
		return ""
	}
	if len(act.Argv) > 0 {
		return "$ " + resumeCommandText(act.Cwd, act.Argv)
	}
	if act.Script != "" {
		return "$ osascript (" + act.Kind + ")"
	}
	if act.Description != "" {
		return act.Description
	}
	return act.Kind
}

// resumeCommandText renders "cd <dir> && <argv>" with shell quoting.
func resumeCommandText(cwd string, argv []string) string {
	q := make([]string, len(argv))
	for i, a := range argv {
		q[i] = launch.ShellQuote(a)
	}
	cmd := strings.Join(q, " ")
	if cwd != "" {
		return "cd " + launch.ShellQuote(cwd) + " && " + cmd
	}
	return cmd
}

// claudeLink returns the claude.ai URL for a session when one is known.
func claudeLink(sess *model.Session) string {
	if sess.BridgeURL != "" {
		return sess.BridgeURL
	}
	if sess.Live != nil && sess.Live.BridgeSessionID != "" {
		return "https://claude.ai/code/" + sess.Live.BridgeSessionID
	}
	return ""
}

// copyToClipboard uses pbcopy on macOS and xclip elsewhere.
func copyToClipboard(text string) error {
	if os.Getenv("RECALL_DRY_RUN") == "1" {
		return nil
	}
	var c *exec.Cmd
	if _, err := exec.LookPath("pbcopy"); err == nil {
		c = exec.Command("pbcopy")
	} else if _, err := exec.LookPath("xclip"); err == nil {
		c = exec.Command("xclip", "-selection", "clipboard")
	} else {
		return errors.New("no clipboard tool")
	}
	c.Stdin = strings.NewReader(text)
	return c.Run()
}

// stopSession kills a kept tmux session or sends SIGTERM to the process.
func stopSession(a *app.App, sess *model.Session) error {
	if os.Getenv("RECALL_DRY_RUN") == "1" {
		return nil
	}
	if sess.Live == nil || !sess.Live.Alive {
		return errors.New("session is not running")
	}
	if sess.Live.Mux != nil && a != nil && a.Tmux != nil {
		return a.Tmux.Kill(sess.ID)
	}
	if sess.Live.PID <= 0 {
		return errors.New("no pid recorded")
	}
	return syscall.Kill(sess.Live.PID, syscall.SIGTERM)
}

// displayPath shortens a home-relative path for the header.
func displayPath(p string) string {
	home, err := os.UserHomeDir()
	if err == nil && home != "" && strings.HasPrefix(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return filepath.Clean(p)
}
