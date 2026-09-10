package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/MohammedAl-Alimi/recall/internal/archive"
	"github.com/MohammedAl-Alimi/recall/internal/index"
	"github.com/MohammedAl-Alimi/recall/internal/launch"
	"github.com/MohammedAl-Alimi/recall/internal/live"
	"github.com/MohammedAl-Alimi/recall/internal/model"
	"github.com/MohammedAl-Alimi/recall/internal/mux"
	"github.com/MohammedAl-Alimi/recall/internal/notify"
	"github.com/MohammedAl-Alimi/recall/internal/scan"
	"github.com/MohammedAl-Alimi/recall/internal/state"
)

// preselectWindow is how far back PreselectForCwd looks.
const preselectWindow = 7 * 24 * time.Hour

// waitingWindow is how long a permission_prompt hook event keeps a live
// session in needs_you when the registry itself does not say "waiting".
const waitingWindow = 15 * time.Minute

// eventsTail bounds how much of events.jsonl is read per refresh.
const eventsTail = 256 * 1024

// now is swapped in tests.
var now = time.Now

// deps holds every call into a sibling package so tests can substitute
// deterministic fakes. New wires the real implementations.
type deps struct {
	scan        func(ctx context.Context) ([]*model.Session, error)
	ghosts      func(known map[string]bool) ([]*model.Session, error)
	parse       func(path string) (*model.Session, error)
	probe       func(ctx context.Context) (map[string]*model.Live, error)
	degraded    func() (bool, string)
	muxList     func() ([]model.MuxInfo, error)
	loadMeta    func(p model.Paths) (*state.Meta, error)
	retention   func(p model.Paths) (int, bool, error)
	hasArchive  func(p model.Paths, sid string) (string, bool)
	lock        func(p model.Paths, sid string) (*os.File, error)
	plan        func(sess *model.Session, opts launch.Options) (*launch.Action, error)
	saveLaunch  func(p model.Paths, l *model.Launch) error
	appendEvent func(p model.Paths, ev map[string]any) error
	notify      func(title, body string) error
}

// App is the shared application state.
type App struct {
	Paths   model.Paths
	Scanner *scan.Scanner
	Prober  *live.Prober
	Tmux    *mux.Tmux
	Meta    *state.Meta

	Sessions []*model.Session
	LiveByID map[string]*model.Live

	Degraded       bool
	DegradedReason string
	RetentionDays  int
	RetentionSet   bool
	ShellCwd       string

	// Warnings collects non-fatal problems met during Load and RefreshLive
	// (unreadable meta, failed launch record, ...). Reset on every Load.
	Warnings []string

	// Panes is the tmux pane list from the last refresh.
	Panes []model.MuxInfo

	// LoadedAt is when Load last completed.
	LoadedAt time.Time

	d     deps
	prev  map[string]model.State
	locks map[string]*os.File
}

// New builds an App for p without loading anything.
func New(p model.Paths) (*App, error) {
	a := &App{
		Paths:    p,
		Scanner:  scan.New(p),
		Prober:   live.New(p),
		Tmux:     mux.NewTmux(),
		Meta:     state.NewMeta(),
		LiveByID: map[string]*model.Live{},
		locks:    map[string]*os.File{},
	}
	if wd, err := os.Getwd(); err == nil {
		a.ShellCwd = wd
	}
	a.d = deps{
		scan:        a.Scanner.Scan,
		ghosts:      a.Scanner.Ghosts,
		parse:       scan.ParseTranscript,
		probe:       a.Prober.Probe,
		degraded:    a.Prober.Degraded,
		muxList:     a.Tmux.List,
		loadMeta:    state.LoadMeta,
		retention:   archive.Retention,
		hasArchive:  archive.HasArchive,
		lock:        live.Lock,
		plan:        launch.Plan,
		saveLaunch:  state.SaveLaunch,
		appendEvent: state.AppendEvent,
		notify:      notify.Send,
	}
	return a, nil
}

// Load scans transcripts, ghosts, metadata and live processes, then derives
// every session state.
func (a *App) Load(ctx context.Context, includeGhosts bool) error {
	a.Warnings = nil

	sessions, err := a.d.scan(ctx)
	if err != nil {
		return fmt.Errorf("scan: %w", err)
	}
	known := make(map[string]bool, len(sessions))
	for _, s := range sessions {
		if s != nil && s.ID != "" {
			known[s.ID] = true
		}
	}

	sessions = append(sessions, a.archivedOnly(known)...)
	for _, s := range sessions {
		if s == nil || s.Archived {
			continue
		}
		if dir, ok := a.d.hasArchive(a.Paths, s.ID); ok {
			s.Archived = true
			s.ArchivePath = dir
		}
	}

	if includeGhosts {
		ghosts, gerr := a.d.ghosts(known)
		if gerr != nil {
			a.warn("ghosts: %v", gerr)
		}
		for _, g := range ghosts {
			if g == nil || known[g.ID] {
				continue
			}
			g.Ghost = true
			known[g.ID] = true
			sessions = append(sessions, g)
		}
	}

	if m, merr := a.d.loadMeta(a.Paths); merr != nil {
		a.warn("meta: %v", merr)
		if a.Meta == nil {
			a.Meta = state.NewMeta()
		}
	} else if m != nil {
		a.Meta = m
	}

	days, set, rerr := a.d.retention(a.Paths)
	if rerr != nil {
		a.warn("retention: %v", rerr)
		days, set = 0, false
	}
	a.RetentionDays, a.RetentionSet = days, set

	compact := sessions[:0]
	for _, s := range sessions {
		if s != nil && s.ID != "" {
			compact = append(compact, s)
		}
	}
	a.Sessions = compact
	a.applyMeta()

	if err := a.RefreshLive(ctx); err != nil {
		return err
	}
	a.LoadedAt = now()
	return nil
}

// archivedOnly returns sessions that exist only under RecallDir/archive.
// Their Path is left empty so index.Derive reports them as archived; the
// transcript to resume from is ArchivePath.
func (a *App) archivedOnly(known map[string]bool) []*model.Session {
	root := filepath.Join(a.Paths.RecallDir, "archive")
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []*model.Session
	for _, e := range entries {
		if !e.IsDir() || known[e.Name()] {
			continue
		}
		tp := filepath.Join(root, e.Name(), "transcript.jsonl")
		if _, err := os.Stat(tp); err != nil {
			continue
		}
		s, err := a.d.parse(tp)
		if err != nil || s == nil {
			if err != nil {
				a.warn("archive %s: %v", model.ShortID(e.Name()), err)
			}
			continue
		}
		if s.ID == "" {
			s.ID = e.Name()
		}
		s.Archived = true
		s.ArchivePath = tp
		s.Path = ""
		known[s.ID] = true
		out = append(out, s)
	}
	return out
}

// applyMeta copies labels, pins, hidden flags, notes and tags onto sessions.
func (a *App) applyMeta() {
	if a.Meta == nil {
		return
	}
	trashed := map[string]bool{}
	for _, t := range a.Meta.Trash {
		trashed[t.SID] = true
	}
	for _, s := range a.Sessions {
		s.Label = a.Meta.Labels[s.ID]
		s.Pinned = a.Meta.IsPinned(s.ID)
		s.Hidden = a.Meta.IsHidden(s.ID) || trashed[s.ID]
		s.Notes = a.Meta.Notes[s.ID]
		if tags := a.Meta.Tags[s.ID]; len(tags) > 0 {
			s.Tags = append([]string(nil), tags...)
		} else {
			s.Tags = nil
		}
	}
}

// ApplyMeta re-applies a.Meta onto the loaded sessions. Call it after
// editing Meta in place (pin, label, hide) so the list reflects the change
// without a rescan.
func (a *App) ApplyMeta() { a.applyMeta() }

// RefreshLive re-probes live processes and re-derives states.
func (a *App) RefreshLive(ctx context.Context) error {
	liveByID, err := a.d.probe(ctx)
	if err != nil {
		a.Degraded = true
		a.DegradedReason = err.Error()
		if liveByID == nil {
			liveByID = a.LiveByID
		}
	} else if a.d.degraded != nil {
		a.Degraded, a.DegradedReason = a.d.degraded()
	} else {
		a.Degraded, a.DegradedReason = false, ""
	}
	if liveByID == nil {
		liveByID = map[string]*model.Live{}
	}
	a.LiveByID = liveByID

	panes, merr := a.d.muxList()
	if merr != nil {
		a.warn("tmux: %v", merr)
	}
	a.Panes = panes

	a.applyWaitingEvents()
	a.derive()
	return nil
}

// derive recomputes every session state, remembering the previous states
// for NotifyNeedsYou.
func (a *App) derive() {
	prev := make(map[string]model.State, len(a.Sessions))
	for _, s := range a.Sessions {
		if s.State != "" {
			prev[s.ID] = s.State
		}
	}
	if len(prev) > 0 || a.prev == nil {
		a.prev = prev
	}
	days := a.RetentionDays
	if !a.RetentionSet {
		days = index.DefaultRetentionDays
	}
	for _, s := range a.Sessions {
		lv := a.LiveByID[s.ID]
		s.Live = lv
		s.State = index.Derive(s, lv, a.Panes, days)
	}
}

// States returns a snapshot of the current state of every session, suitable
// to pass to NotifyNeedsYou after a later refresh.
func (a *App) States() map[string]model.State {
	out := make(map[string]model.State, len(a.Sessions))
	for _, s := range a.Sessions {
		out[s.ID] = s.State
	}
	return out
}

// applyWaitingEvents marks live sessions as waiting when a recent
// permission_prompt hook event is the latest event for that session.
func (a *App) applyWaitingEvents() {
	if len(a.LiveByID) == 0 {
		return
	}
	waiting := recentWaiting(filepath.Join(a.Paths.RecallDir, "events.jsonl"), now())
	for sid, lv := range a.LiveByID {
		if lv == nil || !lv.Alive || lv.WaitingFor != "" {
			continue
		}
		if _, ok := waiting[sid]; ok {
			lv.WaitingFor = "permission"
		}
	}
}

// recentWaiting scans the tail of events.jsonl and returns the sessions
// whose latest event within waitingWindow is a permission prompt.
func recentWaiting(path string, at time.Time) map[string]time.Time {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.Size() > eventsTail {
		if _, err := f.Seek(st.Size()-eventsTail, io.SeekStart); err == nil {
			r := bufio.NewReader(f)
			_, _ = r.ReadString('\n')
			return parseWaiting(r, at)
		}
	}
	return parseWaiting(bufio.NewReader(f), at)
}

func parseWaiting(r io.Reader, at time.Time) map[string]time.Time {
	out := map[string]time.Time{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	cutoff := at.Add(-waitingWindow)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev map[string]any
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		sid := firstString(ev, "session_id", "sid", "sessionId")
		if sid == "" {
			continue
		}
		ts := eventTime(ev)
		if !ts.IsZero() && ts.Before(cutoff) {
			delete(out, sid)
			continue
		}
		name := strings.ToLower(firstString(ev, "hook_event_name", "event", "name", "type"))
		kind := strings.ToLower(firstString(ev, "notification_type", "notificationType", "reason", "source"))
		switch {
		case strings.Contains(kind, "permission") || strings.Contains(name, "permission") ||
			(name == "notification" && strings.Contains(strings.ToLower(string(line)), "permission_prompt")) ||
			name == "waiting":
			if ts.IsZero() {
				ts = at
			}
			out[sid] = ts
		case name != "":
			// Any later event for the session means the prompt was answered.
			delete(out, sid)
		}
	}
	return out
}

func firstString(ev map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := ev[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func eventTime(ev map[string]any) time.Time {
	for _, k := range []string{"at", "ts", "timestamp", "time"} {
		switch v := ev[k].(type) {
		case string:
			if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
				return t
			}
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				return t
			}
		case float64:
			if v > 1e12 {
				return time.UnixMilli(int64(v))
			}
			if v > 0 {
				return time.Unix(int64(v), 0)
			}
		}
	}
	return time.Time{}
}

// Visible returns the sessions to display given the toggles, sorted.
func (a *App) Visible(showHidden, showGhosts, showHeadless bool) []*model.Session {
	out := make([]*model.Session, 0, len(a.Sessions))
	for _, s := range a.Sessions {
		if s.Hidden && !showHidden {
			continue
		}
		if s.Ghost && !showGhosts {
			continue
		}
		if s.Headless && !showHeadless {
			continue
		}
		out = append(out, s)
	}
	index.Sort(out)
	return out
}

// Find resolves ref to a loaded session: exact id, then exact label
// (case-insensitive), then a unique id prefix.
func (a *App) Find(ref string) (*model.Session, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, errors.New("empty session reference")
	}
	for _, s := range a.Sessions {
		if s.ID == ref {
			return s, nil
		}
	}
	for _, s := range a.Sessions {
		if s.Label != "" && strings.EqualFold(s.Label, ref) {
			return s, nil
		}
	}
	var hits []*model.Session
	lower := strings.ToLower(ref)
	for _, s := range a.Sessions {
		if strings.HasPrefix(strings.ToLower(s.ID), lower) {
			hits = append(hits, s)
		}
	}
	switch len(hits) {
	case 0:
		return nil, fmt.Errorf("no session matches %q", ref)
	case 1:
		return hits[0], nil
	}
	var ids []string
	for _, h := range hits {
		ids = append(ids, model.ShortID(h.ID))
	}
	sort.Strings(ids)
	return nil, fmt.Errorf("%q is ambiguous: %s", ref, strings.Join(ids, ", "))
}

// Open locks, plans and records the launch of sess.
//
// Resume actions take the session lock first; when another process holds it
// the error reads "already open (pid N)". Forks never lock the parent
// session because claude creates a new id for them.
func (a *App) Open(sess *model.Session, opts launch.Options) (*launch.Action, error) {
	if sess == nil {
		return nil, errors.New("no session selected")
	}
	if sess.Ghost {
		return nil, fmt.Errorf("%s has no transcript left to resume", model.ShortID(sess.ID))
	}
	if opts.TermProgram == "" {
		opts.TermProgram = os.Getenv("TERM_PROGRAM")
	}
	if opts.DryRun || os.Getenv("RECALL_DRY_RUN") == "1" {
		opts.DryRun = true
	}

	action, err := a.d.plan(sess, opts)
	if err != nil {
		return nil, err
	}
	if action == nil {
		return nil, errors.New("launch plan returned nothing")
	}

	if action.Kind == "resume" && !opts.Fork && !opts.DryRun {
		if err := a.acquire(sess); err != nil {
			return nil, err
		}
	}

	if !opts.DryRun && len(action.Argv) > 0 && (action.Kind == "resume" || action.Kind == "new" || action.Kind == "attach") {
		muxName := ""
		if opts.Keep {
			muxName = "tmux"
		}
		l := &model.Launch{
			SID:         sess.ID,
			Argv:        append([]string(nil), action.Argv...),
			Cwd:         action.Cwd,
			TermProgram: opts.TermProgram,
			Mux:         muxName,
			RecordedBy:  "recall",
			At:          now(),
		}
		if err := a.d.saveLaunch(a.Paths, l); err != nil {
			a.warn("record launch: %v", err)
		} else {
			sess.Launch = l
		}
	}
	if a.d.appendEvent != nil {
		_ = a.d.appendEvent(a.Paths, map[string]any{
			"event":      "open",
			"session_id": sess.ID,
			"kind":       action.Kind,
			"at":         now().UTC().Format(time.RFC3339),
			"dry_run":    opts.DryRun,
		})
	}
	return action, nil
}

// acquire takes the session lock for sess and keeps it open so an exec'ed
// claude inherits it.
func (a *App) acquire(sess *model.Session) error {
	if a.locks == nil {
		a.locks = map[string]*os.File{}
	}
	if _, held := a.locks[sess.ID]; held {
		return fmt.Errorf("already open (pid %d)", os.Getpid())
	}
	f, err := a.d.lock(a.Paths, sess.ID)
	if err != nil {
		return fmt.Errorf("already open (pid %s): %w", a.lockHolder(sess, f), err)
	}
	a.locks[sess.ID] = f
	return nil
}

// lockHolder guesses who holds the lock: the live process if known, else a
// pid written into the lock file, else "unknown".
func (a *App) lockHolder(sess *model.Session, f *os.File) string {
	if lv := a.LiveByID[sess.ID]; lv != nil && lv.PID > 0 {
		return strconv.Itoa(lv.PID)
	}
	if sess.Live != nil && sess.Live.PID > 0 {
		return strconv.Itoa(sess.Live.PID)
	}
	if f != nil {
		f.Close()
	}
	b, err := os.ReadFile(filepath.Join(a.Paths.RecallDir, "locks", sess.ID))
	if err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && n > 0 {
			return strconv.Itoa(n)
		}
	}
	return "unknown"
}

// ReleaseLock drops the lock Open took for sid, for example after a launch
// failed or after the session was handed to another terminal tab.
func (a *App) ReleaseLock(sid string) {
	if f, ok := a.locks[sid]; ok {
		f.Close()
		delete(a.locks, sid)
	}
}

// HoldsLock reports whether this App holds the lock for sid.
func (a *App) HoldsLock(sid string) bool {
	_, ok := a.locks[sid]
	return ok
}

// PreselectForCwd returns the single session active in the last 7 days whose
// working directory equals ShellCwd, or nil.
func (a *App) PreselectForCwd() *model.Session {
	cwd := filepath.Clean(a.ShellCwd)
	if cwd == "" || cwd == "." {
		return nil
	}
	cutoff := now().Add(-preselectWindow)
	var hit *model.Session
	for _, s := range a.Sessions {
		if s.Ghost || s.Hidden || s.LastActive.Before(cutoff) || s.LastActive.IsZero() {
			continue
		}
		if !sameDir(s.WorkCwd, cwd) && !sameDir(s.Cwd, cwd) {
			continue
		}
		if hit != nil {
			return nil
		}
		hit = s
	}
	return hit
}

func sameDir(d, cwd string) bool {
	return d != "" && filepath.Clean(d) == cwd
}

// NotifyNeedsYou sends a notification for every session that transitioned
// into needs_you since prev. When prev is nil the states recorded by the
// previous derive are used.
func (a *App) NotifyNeedsYou(prev map[string]model.State) {
	if prev == nil {
		prev = a.prev
	}
	if prev == nil {
		return
	}
	for _, s := range a.Sessions {
		if s.State != model.StateNeedsYou {
			continue
		}
		was, seen := prev[s.ID]
		if !seen || was == model.StateNeedsYou {
			continue
		}
		title := s.Label
		if title == "" {
			title = s.Title
		}
		if title == "" {
			title = model.ShortID(s.ID)
		}
		body := title
		if p := projectName(s); p != "" {
			body = title + " (" + p + ")"
		}
		if err := a.d.notify("Needs you", body); err != nil {
			a.warn("notify: %v", err)
		}
	}
}

func projectName(s *model.Session) string {
	for _, d := range []string{s.WorkCwd, s.Cwd, s.LastCwd} {
		if d != "" {
			return filepath.Base(d)
		}
	}
	return ""
}

func (a *App) warn(format string, args ...any) {
	a.Warnings = append(a.Warnings, fmt.Sprintf(format, args...))
}
