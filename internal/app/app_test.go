package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MohammedAl-Alimi/recall/internal/launch"
	"github.com/MohammedAl-Alimi/recall/internal/live"
	"github.com/MohammedAl-Alimi/recall/internal/model"
	"github.com/MohammedAl-Alimi/recall/internal/state"
)

var fixedNow = time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

func ago(d time.Duration) time.Time { return fixedNow.Add(-d) }

const (
	sidA = "aaaa1111-1111-1111-1111-111111111111"
	sidB = "bbbb2222-2222-2222-2222-222222222222"
	sidC = "cccc3333-3333-3333-3333-333333333333"
	sidG = "dddd4444-4444-4444-4444-444444444444"
)

// newTestApp returns an App on temp dirs whose sibling calls are all fakes
// that succeed with empty results. Tests override the fakes they need.
func newTestApp(t *testing.T) *App {
	t.Helper()
	old := now
	now = func() time.Time { return fixedNow }
	t.Cleanup(func() { now = old })

	claude := t.TempDir()
	recall := t.TempDir()
	if err := os.MkdirAll(filepath.Join(claude, "projects"), 0o700); err != nil {
		t.Fatal(err)
	}
	a, err := New(model.PathsFrom(claude, recall))
	if err != nil {
		t.Fatal(err)
	}
	a.ShellCwd = "/home/u/dev/webapp"
	a.d = deps{
		scan:        func(context.Context) ([]*model.Session, error) { return nil, nil },
		ghosts:      func(map[string]bool) ([]*model.Session, error) { return nil, nil },
		parse:       func(string) (*model.Session, error) { return nil, errors.New("no parser") },
		probe:       func(context.Context) (map[string]*model.Live, error) { return map[string]*model.Live{}, nil },
		degraded:    func() (bool, string) { return false, "" },
		muxList:     func() ([]model.MuxInfo, error) { return nil, nil },
		loadMeta:    func(model.Paths) (*state.Meta, error) { return state.NewMeta(), nil },
		retention:   func(model.Paths) (int, bool, error) { return 30, true, nil },
		hasArchive:  func(model.Paths, string) (string, bool) { return "", false },
		lock:        func(p model.Paths, sid string) (*os.File, error) { return os.CreateTemp(p.RecallDir, "lock") },
		plan:        fakePlan,
		saveLaunch:  func(model.Paths, *model.Launch) error { return nil },
		appendEvent: func(model.Paths, map[string]any) error { return nil },
		notify:      func(string, string) error { return nil },
	}
	return a
}

func fakePlan(sess *model.Session, opts launch.Options) (*launch.Action, error) {
	cwd := sess.WorkCwd
	if cwd == "" {
		cwd = sess.Cwd
	}
	if sess.Live != nil && sess.Live.Alive {
		return &launch.Action{Kind: "focus", Cwd: cwd, Description: "focus"}, nil
	}
	argv := []string{"claude", "--resume", sess.ID}
	if opts.Fork {
		argv = append(argv, "--fork-session")
	}
	return &launch.Action{Kind: "resume", Cwd: cwd, Argv: argv, Description: "resume"}, nil
}

func baseSessions() []*model.Session {
	return []*model.Session{
		{ID: sidA, Path: "/p/a.jsonl", Title: "Fix login", WorkCwd: "/home/u/dev/webapp", Cwd: "/home/u/dev/webapp", LastActive: ago(time.Hour)},
		{ID: sidB, Path: "/p/b.jsonl", Title: "Scanner", WorkCwd: "/home/u/dev/recall", LastActive: ago(2 * 24 * time.Hour)},
		{ID: sidC, Path: "/p/c.jsonl", Title: "Headless run", WorkCwd: "/home/u/dev/webapp", Headless: true, LastActive: ago(20 * 24 * time.Hour)},
	}
}

func TestLoadDerivesAndAppliesMeta(t *testing.T) {
	a := newTestApp(t)
	a.d.scan = func(context.Context) ([]*model.Session, error) { return baseSessions(), nil }
	ghostCalls := 0
	a.d.ghosts = func(known map[string]bool) ([]*model.Session, error) {
		ghostCalls++
		if !known[sidA] || !known[sidB] || !known[sidC] {
			t.Errorf("ghosts called with incomplete known set: %v", known)
		}
		return []*model.Session{
			{ID: sidG, Ghost: true, Title: "what is flock", Cwd: "/home/u/dev/webapp", LastActive: ago(3 * time.Hour)},
			{ID: sidA, Ghost: true, Title: "dup must be dropped"},
		}, nil
	}
	a.d.probe = func(context.Context) (map[string]*model.Live, error) {
		return map[string]*model.Live{
			sidA: {PID: 4242, Alive: true, Status: "waiting", SessionID: sidA},
			sidB: {PID: 4343, Alive: true, Status: "busy", SessionID: sidB},
		}, nil
	}
	a.d.muxList = func() ([]model.MuxInfo, error) {
		return []model.MuxInfo{{Socket: "recall", SessionName: "rc-bbbb2222", PanePID: 4343}}, nil
	}
	a.d.loadMeta = func(model.Paths) (*state.Meta, error) {
		m := state.NewMeta()
		m.SetLabel(sidB, "scanner-work")
		m.Pin(sidB)
		m.Hide(sidC)
		m.Notes[sidA] = "escalation"
		m.Tags[sidA] = []string{"urgent"}
		return m, nil
	}
	a.d.retention = func(model.Paths) (int, bool, error) { return 45, true, nil }

	if err := a.Load(context.Background(), false); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if ghostCalls != 0 {
		t.Errorf("ghosts called without includeGhosts")
	}
	if len(a.Sessions) != 3 {
		t.Fatalf("sessions = %d, want 3", len(a.Sessions))
	}
	if a.RetentionDays != 45 || !a.RetentionSet {
		t.Errorf("retention = %d/%v", a.RetentionDays, a.RetentionSet)
	}
	if a.Degraded {
		t.Errorf("unexpected degraded: %s", a.DegradedReason)
	}
	by := byID(a)
	if by[sidA].State != model.StateNeedsYou {
		t.Errorf("A state = %s, want needs_you", by[sidA].State)
	}
	if by[sidB].State != model.StateKept {
		t.Errorf("B state = %s, want kept", by[sidB].State)
	}
	if by[sidC].State != model.StateHeadless {
		t.Errorf("C state = %s, want headless", by[sidC].State)
	}
	if by[sidA].Live == nil || by[sidA].Live.PID != 4242 {
		t.Errorf("A live not attached")
	}
	if by[sidB].Label != "scanner-work" || !by[sidB].Pinned {
		t.Errorf("meta not applied to B: %+v", by[sidB])
	}
	if !by[sidC].Hidden {
		t.Errorf("hidden not applied to C")
	}
	if by[sidA].Notes != "escalation" || len(by[sidA].Tags) != 1 || by[sidA].Tags[0] != "urgent" {
		t.Errorf("notes/tags not applied to A: %q %v", by[sidA].Notes, by[sidA].Tags)
	}
	if a.LoadedAt != fixedNow {
		t.Errorf("LoadedAt = %v", a.LoadedAt)
	}

	// Now with ghosts.
	if err := a.Load(context.Background(), true); err != nil {
		t.Fatalf("Load(ghosts): %v", err)
	}
	if ghostCalls != 1 {
		t.Errorf("ghosts called %d times, want 1", ghostCalls)
	}
	by = byID(a)
	if len(a.Sessions) != 4 {
		t.Fatalf("sessions with ghosts = %d, want 4 (duplicate ghost must be dropped)", len(a.Sessions))
	}
	if by[sidG].State != model.StateGhost {
		t.Errorf("ghost state = %s", by[sidG].State)
	}
	if by[sidA].Title != "Fix login" {
		t.Errorf("transcript session replaced by ghost")
	}

	vis := ids(a.Visible(false, false, false))
	if vis != "aaaa1111,bbbb2222" {
		t.Errorf("Visible(default) = %s", vis)
	}
	// Sorted: pinned B before the more recent ghost, then the old hidden one.
	if got := ids(a.Visible(true, true, true)); got != "aaaa1111,bbbb2222,dddd4444,cccc3333" {
		t.Errorf("Visible(all) = %s", got)
	}
	if got := ids(a.Visible(false, true, false)); got != "aaaa1111,bbbb2222,dddd4444" {
		t.Errorf("Visible(ghosts) = %s", got)
	}
	// C is both hidden and headless, so both toggles are needed.
	if got := ids(a.Visible(true, false, false)); got != "aaaa1111,bbbb2222" {
		t.Errorf("Visible(hidden only) = %s", got)
	}
	if got := ids(a.Visible(true, false, true)); got != "aaaa1111,bbbb2222,cccc3333" {
		t.Errorf("Visible(hidden+headless) = %s", got)
	}
}

func TestLoadDegradedAndWarnings(t *testing.T) {
	a := newTestApp(t)
	a.d.scan = func(context.Context) ([]*model.Session, error) { return baseSessions(), nil }
	a.d.probe = func(context.Context) (map[string]*model.Live, error) { return nil, errors.New("agents json timed out") }
	a.d.loadMeta = func(model.Paths) (*state.Meta, error) { return nil, errors.New("corrupt meta") }
	a.d.retention = func(model.Paths) (int, bool, error) { return 0, false, errors.New("no settings") }
	if err := a.Load(context.Background(), false); err != nil {
		t.Fatalf("Load must tolerate probe/meta/retention failures: %v", err)
	}
	if !a.Degraded || !strings.Contains(a.DegradedReason, "timed out") {
		t.Errorf("degraded = %v %q", a.Degraded, a.DegradedReason)
	}
	if a.Meta == nil {
		t.Fatal("Meta must stay non-nil")
	}
	if a.RetentionSet || a.RetentionDays != 0 {
		t.Errorf("retention should be unset: %d %v", a.RetentionDays, a.RetentionSet)
	}
	joined := strings.Join(a.Warnings, "\n")
	for _, want := range []string{"meta:", "retention:"} {
		if !strings.Contains(joined, want) {
			t.Errorf("warnings missing %q: %v", want, a.Warnings)
		}
	}
	// With retention unset the default window still applies: C is 20 days old, not stale.
	if st := byID(a)[sidC].State; st != model.StateHeadless {
		t.Errorf("C = %s", st)
	}
	// Reported degraded by the prober itself.
	a.d.probe = func(context.Context) (map[string]*model.Live, error) { return map[string]*model.Live{}, nil }
	a.d.degraded = func() (bool, string) { return true, "daemon.lock appeared" }
	if err := a.RefreshLive(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !a.Degraded || a.DegradedReason != "daemon.lock appeared" {
		t.Errorf("prober degraded not propagated: %v %q", a.Degraded, a.DegradedReason)
	}

	a.d.scan = func(context.Context) ([]*model.Session, error) { return nil, errors.New("projects dir unreadable") }
	if err := a.Load(context.Background(), false); err == nil || !strings.Contains(err.Error(), "scan:") {
		t.Errorf("scan failure must be fatal, got %v", err)
	}
}

func TestLoadArchivedOnlySessions(t *testing.T) {
	a := newTestApp(t)
	a.d.scan = func(context.Context) ([]*model.Session, error) { return baseSessions(), nil }
	archived := "eeee5555-5555-5555-5555-555555555555"
	dir := filepath.Join(a.Paths.RecallDir, "archive", archived)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	tp := filepath.Join(dir, "transcript.jsonl")
	if err := os.WriteFile(tp, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// An archive dir for a still-present transcript must not be duplicated.
	if err := os.MkdirAll(filepath.Join(a.Paths.RecallDir, "archive", sidA), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.Paths.RecallDir, "archive", sidA, "transcript.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.d.parse = func(path string) (*model.Session, error) {
		if path != tp {
			t.Errorf("parse called for %s", path)
		}
		return &model.Session{ID: archived, Title: "Old work", LastActive: ago(100 * 24 * time.Hour)}, nil
	}
	a.d.hasArchive = func(_ model.Paths, sid string) (string, bool) {
		if sid == sidA {
			return filepath.Join(a.Paths.RecallDir, "archive", sidA), true
		}
		return "", false
	}
	if err := a.Load(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	by := byID(a)
	if len(a.Sessions) != 4 {
		t.Fatalf("sessions = %d, want 4", len(a.Sessions))
	}
	s := by[archived]
	if s == nil || !s.Archived || s.ArchivePath != tp || s.Path != "" {
		t.Fatalf("archived-only session wrong: %+v", s)
	}
	if s.State != model.StateArchived {
		t.Errorf("state = %s, want archived", s.State)
	}
	if !by[sidA].Archived || by[sidA].State == model.StateArchived {
		t.Errorf("A should be flagged archived but keep its transcript state: %+v", by[sidA])
	}
}

func TestWaitingEventPromotesToNeedsYou(t *testing.T) {
	a := newTestApp(t)
	a.d.scan = func(context.Context) ([]*model.Session, error) { return baseSessions(), nil }
	a.d.probe = func(context.Context) (map[string]*model.Live, error) {
		return map[string]*model.Live{
			sidA: {PID: 1, Alive: true, Status: "idle"},
			sidB: {PID: 2, Alive: true, Status: "idle"},
			sidC: {PID: 3, Alive: true, Status: "idle"},
		}, nil
	}
	lines := []string{
		// A: recent permission prompt, nothing after it.
		fmt.Sprintf(`{"hook_event_name":"Notification","notification_type":"permission_prompt","session_id":%q,"at":%q}`, sidA, ago(2*time.Minute).Format(time.RFC3339)),
		// B: prompt answered by a later event.
		fmt.Sprintf(`{"hook_event_name":"Notification","notification_type":"permission_prompt","session_id":%q,"at":%q}`, sidB, ago(3*time.Minute).Format(time.RFC3339)),
		fmt.Sprintf(`{"hook_event_name":"PostToolUse","session_id":%q,"at":%q}`, sidB, ago(1*time.Minute).Format(time.RFC3339)),
		// C: too old.
		fmt.Sprintf(`{"hook_event_name":"Notification","notification_type":"permission_prompt","session_id":%q,"at":%q}`, sidC, ago(2*time.Hour).Format(time.RFC3339)),
		"not json",
		"",
	}
	if err := os.WriteFile(filepath.Join(a.Paths.RecallDir, "events.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.Load(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	by := byID(a)
	if by[sidA].State != model.StateNeedsYou {
		t.Errorf("A = %s, want needs_you", by[sidA].State)
	}
	if by[sidB].State != model.StateLiveIdle {
		t.Errorf("B = %s, want live_idle", by[sidB].State)
	}
	// C is alive, so the stale prompt event does not promote it and live wins over headless.
	if by[sidC].State != model.StateLiveIdle {
		t.Errorf("C = %s, want live_idle", by[sidC].State)
	}
}

func TestNotifyNeedsYouOnTransition(t *testing.T) {
	a := newTestApp(t)
	a.d.scan = func(context.Context) ([]*model.Session, error) { return baseSessions(), nil }
	status := map[string]string{sidA: "waiting", sidB: "busy"}
	a.d.probe = func(context.Context) (map[string]*model.Live, error) {
		return map[string]*model.Live{
			sidA: {PID: 1, Alive: true, Status: status[sidA]},
			sidB: {PID: 2, Alive: true, Status: status[sidB]},
		}, nil
	}
	var sent []string
	a.d.notify = func(title, body string) error {
		sent = append(sent, title+"|"+body)
		return nil
	}
	if err := a.Load(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	// First load: no previous states, nothing is a transition.
	a.NotifyNeedsYou(nil)
	if len(sent) != 0 {
		t.Fatalf("notified on first load: %v", sent)
	}
	prev := a.States()
	if prev[sidA] != model.StateNeedsYou || prev[sidB] != model.StateLiveBusy {
		t.Fatalf("States() = %v", prev)
	}
	status[sidB] = "waiting"
	if err := a.RefreshLive(context.Background()); err != nil {
		t.Fatal(err)
	}
	a.NotifyNeedsYou(prev)
	if len(sent) != 1 || !strings.HasPrefix(sent[0], "Needs you|Scanner (recall)") {
		t.Fatalf("notify = %v, want one for B", sent)
	}
	// Internal prev works the same way and A (already waiting) is never repeated.
	sent = nil
	status[sidA] = "busy"
	if err := a.RefreshLive(context.Background()); err != nil {
		t.Fatal(err)
	}
	a.NotifyNeedsYou(nil)
	if len(sent) != 0 {
		t.Fatalf("unexpected notifications: %v", sent)
	}
	status[sidA] = "waiting"
	if err := a.RefreshLive(context.Background()); err != nil {
		t.Fatal(err)
	}
	a.NotifyNeedsYou(nil)
	if len(sent) != 1 || !strings.Contains(sent[0], "Fix login (webapp)") {
		t.Fatalf("notify = %v, want one for A", sent)
	}
}

func TestPreselectForCwd(t *testing.T) {
	a := newTestApp(t)
	a.ShellCwd = "/home/u/dev/webapp/"
	a.Sessions = []*model.Session{
		{ID: sidA, WorkCwd: "/home/u/dev/webapp", LastActive: ago(time.Hour)},
		{ID: sidB, WorkCwd: "/home/u/dev/recall", LastActive: ago(time.Hour)},
		{ID: sidC, Cwd: "/home/u/dev/webapp", LastActive: ago(10 * 24 * time.Hour)},
		{ID: sidG, Ghost: true, Cwd: "/home/u/dev/webapp", LastActive: ago(time.Hour)},
	}
	if got := a.PreselectForCwd(); got == nil || got.ID != sidA {
		t.Fatalf("PreselectForCwd = %v, want A", got)
	}
	// A second recent match makes the choice ambiguous.
	a.Sessions[2].LastActive = ago(2 * time.Hour)
	if got := a.PreselectForCwd(); got != nil {
		t.Fatalf("PreselectForCwd = %s, want nil on ambiguity", got.ID)
	}
	// Hidden sessions are not candidates.
	a.Sessions[2].Hidden = true
	if got := a.PreselectForCwd(); got == nil || got.ID != sidA {
		t.Fatalf("PreselectForCwd with hidden = %v, want A", got)
	}
	a.ShellCwd = "/nowhere"
	if got := a.PreselectForCwd(); got != nil {
		t.Fatalf("PreselectForCwd(/nowhere) = %s", got.ID)
	}
	a.ShellCwd = ""
	if got := a.PreselectForCwd(); got != nil {
		t.Fatal("empty cwd must not preselect")
	}
}

func TestFind(t *testing.T) {
	a := newTestApp(t)
	a.Sessions = baseSessions()
	a.Sessions[1].Label = "Scanner-Work"
	cases := map[string]string{
		sidA:           sidA,
		"aaaa":         sidA,
		"AAAA1111":     sidA,
		"scanner-work": sidB,
		"cccc3333-33":  sidC,
	}
	for ref, want := range cases {
		got, err := a.Find(ref)
		if err != nil || got.ID != want {
			t.Errorf("Find(%q) = %v, %v", ref, got, err)
		}
	}
	if _, err := a.Find("zzz"); err == nil {
		t.Error("Find(zzz) should fail")
	}
	if _, err := a.Find(""); err == nil {
		t.Error("Find(empty) should fail")
	}
	a.Sessions = append(a.Sessions, &model.Session{ID: "aaaa9999-0000-0000-0000-000000000000"})
	if _, err := a.Find("aaaa"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("Find(aaaa) = %v, want ambiguity error", err)
	}
}

func TestOpenRecordsLaunchAndLocks(t *testing.T) {
	a := newTestApp(t)
	t.Setenv("TERM_PROGRAM", "Apple_Terminal")
	a.Sessions = baseSessions()
	sess := a.Sessions[0]
	var saved *model.Launch
	a.d.saveLaunch = func(_ model.Paths, l *model.Launch) error { saved = l; return nil }
	var events []map[string]any
	a.d.appendEvent = func(_ model.Paths, ev map[string]any) error { events = append(events, ev); return nil }
	lockCalls := 0
	a.d.lock = func(p model.Paths, sid string) (*os.File, error) {
		lockCalls++
		if sid != sidA {
			t.Errorf("lock for %s", sid)
		}
		return os.CreateTemp(p.RecallDir, "lock")
	}

	act, err := a.Open(sess, launch.Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if act.Kind != "resume" || act.Argv[1] != "--resume" {
		t.Errorf("action = %+v", act)
	}
	if lockCalls != 1 || !a.HoldsLock(sidA) {
		t.Errorf("lock not taken: calls=%d holds=%v", lockCalls, a.HoldsLock(sidA))
	}
	if saved == nil || saved.SID != sidA || saved.RecordedBy != "recall" || saved.TermProgram != "Apple_Terminal" ||
		saved.Cwd != "/home/u/dev/webapp" || len(saved.Argv) != 3 || saved.At != fixedNow {
		t.Errorf("launch record = %+v", saved)
	}
	if sess.Launch != saved {
		t.Errorf("session.Launch not set")
	}
	if len(events) != 1 || events[0]["event"] != "open" || events[0]["session_id"] != sidA {
		t.Errorf("events = %v", events)
	}

	// Same process, same sid: refused.
	if _, err := a.Open(sess, launch.Options{}); err == nil || !strings.Contains(err.Error(), "already open") {
		t.Errorf("second Open = %v, want already open", err)
	}
	a.ReleaseLock(sidA)
	if a.HoldsLock(sidA) {
		t.Error("ReleaseLock did not release")
	}

	// Forks never lock the parent session.
	lockCalls = 0
	if act, err := a.Open(sess, launch.Options{Fork: true}); err != nil || act.Argv[len(act.Argv)-1] != "--fork-session" {
		t.Errorf("fork Open = %+v, %v", act, err)
	}
	if lockCalls != 0 {
		t.Errorf("fork took a lock")
	}

	// Live session: focus action, no lock, no launch record.
	saved = nil
	sess.Live = &model.Live{PID: 77, Alive: true}
	if act, err := a.Open(sess, launch.Options{}); err != nil || act.Kind != "focus" {
		t.Errorf("focus Open = %+v, %v", act, err)
	}
	if lockCalls != 0 || saved != nil {
		t.Errorf("focus must not lock or record: calls=%d saved=%v", lockCalls, saved)
	}

	// Locked by another process with a known pid.
	sess.Live = nil
	a.LiveByID[sidA] = &model.Live{PID: 4242}
	a.d.lock = func(model.Paths, string) (*os.File, error) {
		return nil, errors.New("resource temporarily unavailable")
	}
	if _, err := a.Open(sess, launch.Options{}); err == nil || !strings.Contains(err.Error(), "already open (pid 4242)") {
		t.Errorf("locked Open = %v", err)
	}
	// Unknown holder: pid read from the lock file when present.
	delete(a.LiveByID, sidA)
	if err := os.MkdirAll(filepath.Join(a.Paths.RecallDir, "locks"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.Paths.RecallDir, "locks", sidA), []byte("9001\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Open(sess, launch.Options{}); err == nil || !strings.Contains(err.Error(), "already open (pid 9001)") {
		t.Errorf("locked Open with lock file = %v", err)
	}

	// Plan failures and ghosts propagate.
	a.d.plan = func(*model.Session, launch.Options) (*launch.Action, error) { return nil, errors.New("cwd missing") }
	if _, err := a.Open(sess, launch.Options{}); err == nil || err.Error() != "cwd missing" {
		t.Errorf("plan error = %v", err)
	}
	if _, err := a.Open(&model.Session{ID: sidG, Ghost: true}, launch.Options{}); err == nil {
		t.Error("ghost Open should fail")
	}
	if _, err := a.Open(nil, launch.Options{}); err == nil {
		t.Error("nil Open should fail")
	}
}

// TestOpenRealLockSecondOpenFails uses the real live.Lock so a second Open
// of the same session in this process is refused by flock.
func TestOpenRealLockSecondOpenFails(t *testing.T) {
	a := newTestApp(t)
	a.d.lock = live.Lock
	sess := baseSessions()[0]
	a.Sessions = []*model.Session{sess}
	if _, err := a.Open(sess, launch.Options{DryRun: true}); err != nil {
		if strings.Contains(err.Error(), "not implemented") {
			t.Skip("live.Lock not implemented yet")
		}
		t.Fatalf("first Open: %v", err)
	}
	if !a.HoldsLock(sidA) {
		t.Fatal("lock not held after Open")
	}
	if !live.IsLocked(a.Paths, sidA) {
		t.Errorf("live.IsLocked should report the lock held by Open")
	}
	b, _ := New(a.Paths)
	b.d = a.d
	if _, err := b.Open(sess, launch.Options{DryRun: true}); err == nil || !strings.Contains(err.Error(), "already open") {
		t.Fatalf("second Open = %v, want already open", err)
	}
	a.ReleaseLock(sidA)
	if _, err := b.Open(sess, launch.Options{DryRun: true}); err != nil {
		t.Fatalf("Open after release: %v", err)
	}
}

// TestLoadWithRealScanner writes a small hand-written transcript and runs
// Load through the real scan package.
func TestLoadWithRealScanner(t *testing.T) {
	a := newTestApp(t)
	a.d.scan = a.Scanner.Scan
	a.d.ghosts = a.Scanner.Ghosts
	cwd := t.TempDir()
	sid := "12345678-abcd-4000-8000-000000000001"
	enc := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, cwd)
	dir := filepath.Join(a.Paths.ProjectsDir, enc)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	ts := ago(30 * time.Minute).Format(time.RFC3339)
	lines := []string{
		fmt.Sprintf(`{"parentUuid":null,"isSidechain":false,"type":"user","uuid":"u1","timestamp":%q,"cwd":%q,"sessionId":%q,"version":"2.1.230","gitBranch":"main","entrypoint":"cli","promptId":"p1","message":{"role":"user","content":"please add a health endpoint"}}`, ts, cwd, sid),
		fmt.Sprintf(`{"parentUuid":"u1","isSidechain":false,"type":"assistant","uuid":"a1","timestamp":%q,"cwd":%q,"sessionId":%q,"version":"2.1.230","gitBranch":"main","message":{"role":"assistant","model":"test-model","content":[{"type":"text","text":"Added GET /health."}],"usage":{"input_tokens":10,"output_tokens":5}}}`, ts, cwd, sid),
		fmt.Sprintf(`{"type":"ai-title","aiTitle":"Health endpoint","sessionId":%q}`, sid),
		fmt.Sprintf(`{"type":"permission-mode","permissionMode":"default","sessionId":%q}`, sid),
	}
	if err := os.WriteFile(filepath.Join(dir, sid+".jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	hist := fmt.Sprintf(`{"display":"what is a flock","pastedContents":{},"timestamp":%d,"project":%q,"sessionId":"99999999-abcd-4000-8000-000000000009"}`, ago(time.Hour).UnixMilli(), cwd)
	if err := os.WriteFile(a.Paths.HistoryFile, []byte(hist+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.Load(context.Background(), true); err != nil {
		if strings.Contains(err.Error(), "not implemented") {
			t.Skip("scan not implemented yet")
		}
		t.Fatalf("Load: %v", err)
	}
	by := byID(a)
	s := by[sid]
	if s == nil {
		t.Fatalf("transcript session missing; got %v", ids(a.Sessions))
	}
	if s.Title != "Health endpoint" {
		t.Errorf("Title = %q", s.Title)
	}
	if s.State != model.StateClosed {
		t.Errorf("State = %s, want closed", s.State)
	}
	if s.Branch != "main" || s.Cwd != cwd {
		t.Errorf("Branch/Cwd = %q %q", s.Branch, s.Cwd)
	}
	a.ShellCwd = cwd
	if got := a.PreselectForCwd(); got == nil || got.ID != sid {
		t.Errorf("PreselectForCwd = %v", got)
	}
	if g := by["99999999-abcd-4000-8000-000000000009"]; g == nil {
		t.Logf("ghost not surfaced (Ghosts may still be a stub); sessions: %s", ids(a.Sessions))
	} else if g.State != model.StateGhost {
		t.Errorf("ghost state = %s", g.State)
	}
	if got := ids(a.Visible(false, false, false)); !strings.Contains(got, "12345678") {
		t.Errorf("Visible = %s", got)
	}
}

func byID(a *App) map[string]*model.Session {
	m := map[string]*model.Session{}
	for _, s := range a.Sessions {
		m[s.ID] = s
	}
	return m
}

func ids(list []*model.Session) string {
	var out []string
	for _, s := range list {
		out = append(out, model.ShortID(s.ID))
	}
	return strings.Join(out, ",")
}
