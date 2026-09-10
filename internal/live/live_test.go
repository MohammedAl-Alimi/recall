package live

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MohammedAl-Alimi/recall/internal/model"
)

const testSID = "0f1e2d3c-4b5a-4c6d-8e9f-0a1b2c3d4e5f"

// testPaths builds isolated Claude and recall dirs. Nothing under the real
// ~/.claude is ever touched by these tests.
func testPaths(t *testing.T) model.Paths {
	t.Helper()
	claudeDir := filepath.Join(t.TempDir(), "claude")
	recallDir := filepath.Join(t.TempDir(), "recall")
	p := model.PathsFrom(claudeDir, recallDir)
	if err := os.MkdirAll(p.SessionsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	return p
}

// writeRegistry writes SessionsDir/<pid>.json with the given fields.
func writeRegistry(t *testing.T, p model.Paths, pid int, fields map[string]any) string {
	t.Helper()
	base := map[string]any{
		"pid":        pid,
		"sessionId":  testSID,
		"cwd":        "/tmp/project",
		"version":    "2.1.258",
		"kind":       "interactive",
		"entrypoint": "cli",
		"name":       "project-ab",
		"nameSource": "derived",
		"status":     "idle",
		"updatedAt":  1788625930691,
	}
	for k, v := range fields {
		base[k] = v
	}
	data, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(p.SessionsDir, strconv.Itoa(pid)+".json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// skipAgents is the agents hook used by every test: the real claude binary
// is never executed.
func skipAgents(context.Context) ([]byte, error) { return nil, errAgentsSkipped }

func newTestProber(p model.Paths) *Prober {
	pr := New(p)
	pr.agents = skipAgents
	return pr
}

func selfStart(t *testing.T) string {
	t.Helper()
	s, err := ProcessStart(os.Getpid())
	if err != nil {
		t.Fatalf("ProcessStart(self): %v", err)
	}
	if s == "" || strings.TrimSpace(s) != s {
		t.Fatalf("ProcessStart returned %q, want trimmed non-empty", s)
	}
	return s
}

func TestProcessStartParsesAsANSIC(t *testing.T) {
	t.Setenv("TZ", "Europe/Berlin")
	s := selfStart(t)
	got, err := parseLstart(s)
	if err != nil {
		t.Fatalf("parseLstart(%q): %v", s, err)
	}
	if got.Location() != time.UTC {
		t.Errorf("parsed location = %v, want UTC", got.Location())
	}
	// The process started recently: within the last day and not in the future.
	now := time.Now().UTC()
	if got.After(now.Add(time.Minute)) || got.Before(now.Add(-24*time.Hour)) {
		t.Errorf("parsed start %v not near now %v", got, now)
	}
}

func TestParseLstartPaddedDay(t *testing.T) {
	t.Setenv("TZ", "Europe/Berlin")
	got, err := parseLstart("Wed Sep  2 12:51:58 2026")
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, time.September, 2, 12, 51, 58, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v want %v", got, want)
	}
	if _, err := parseLstart(""); err == nil {
		t.Error("empty lstart should fail")
	}
}

func TestProbeRegistryProcStartMatch(t *testing.T) {
	t.Setenv("TZ", "Europe/Berlin")
	p := testPaths(t)
	sock := filepath.Join(t.TempDir(), "self.sock")
	if err := os.WriteFile(sock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	writeRegistry(t, p, os.Getpid(), map[string]any{
		"procStart":           selfStart(t),
		"startedAt":           1,
		"messagingSocketPath": sock,
		"bridgeSessionId":     "cse_test",
	})
	pr := newTestProber(p)
	got, err := pr.Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	lv := got[testSID]
	if lv == nil {
		t.Fatalf("session %s missing from %v", testSID, got)
	}
	if !lv.Alive {
		t.Errorf("Alive = false, want true (procStart %q)", lv.ProcStart)
	}
	if lv.Source != SourceRegistry {
		t.Errorf("Source = %q", lv.Source)
	}
	if lv.PID != os.Getpid() || lv.Name != "project-ab" || lv.NameSource != "derived" || lv.Status != "idle" || lv.Kind != "interactive" {
		t.Errorf("fields not copied: %+v", lv)
	}
	if lv.BridgeSessionID != "cse_test" {
		t.Errorf("BridgeSessionID = %q", lv.BridgeSessionID)
	}
	if len(lv.Argv) == 0 {
		t.Errorf("Argv empty")
	}
	if d, reason := pr.Degraded(); d {
		t.Errorf("unexpected degraded: %s", reason)
	}
}

func TestProbeStartedAtFallbackInNonUTCZone(t *testing.T) {
	t.Setenv("TZ", "Europe/Berlin")
	p := testPaths(t)
	sock := filepath.Join(t.TempDir(), "self.sock")
	if err := os.WriteFile(sock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	start, err := parseLstart(selfStart(t))
	if err != nil {
		t.Fatal(err)
	}
	writeRegistry(t, p, os.Getpid(), map[string]any{
		"procStart":           "Mon Jan  1 00:00:00 2001",
		"startedAt":           start.Add(3 * time.Second).UnixMilli(),
		"messagingSocketPath": sock,
	})
	pr := newTestProber(p)
	got, err := pr.Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	lv := got[testSID]
	if lv == nil || !lv.Alive {
		t.Fatalf("startedAt within 15s should be alive: %+v", lv)
	}

	// Beyond the tolerance the pid counts as reused.
	writeRegistry(t, p, os.Getpid(), map[string]any{
		"procStart":           "Mon Jan  1 00:00:00 2001",
		"startedAt":           start.Add(-2 * time.Minute).UnixMilli(),
		"messagingSocketPath": sock,
	})
	got, err = pr.Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if lv = got[testSID]; lv == nil || lv.Alive {
		t.Fatalf("startedAt 2 minutes off should not be alive: %+v", lv)
	}
}

func TestProbeRequiresSocketOrArgv(t *testing.T) {
	p := testPaths(t)
	// The test binary is not called claude and the socket path is missing.
	writeRegistry(t, p, os.Getpid(), map[string]any{
		"procStart":           selfStart(t),
		"messagingSocketPath": filepath.Join(t.TempDir(), "missing.sock"),
	})
	pr := newTestProber(p)
	got, err := pr.Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	lv := got[testSID]
	if lv == nil {
		t.Fatal("entry missing")
	}
	if lv.Alive {
		t.Errorf("Alive without socket or claude argv: %+v", lv)
	}

	// With a claude-looking argv the same process is alive.
	orig := runPS
	t.Cleanup(func() { runPS = orig })
	runPS = func(args ...string) (string, error) {
		if len(args) >= 2 && args[1] == "tty=,args=" {
			return "ttys007  claude --resume " + testSID + "\n", nil
		}
		return orig(args...)
	}
	got, err = pr.Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if lv = got[testSID]; lv == nil || !lv.Alive {
		t.Fatalf("Alive with claude argv: %+v", lv)
	}
	if lv.TTY != "ttys007" {
		t.Errorf("TTY = %q", lv.TTY)
	}
	if len(lv.Argv) < 2 || lv.Argv[0] != "claude" || lv.Argv[1] != "--resume" {
		t.Errorf("Argv = %v", lv.Argv)
	}
}

func TestProbeDeadProcess(t *testing.T) {
	p := testPaths(t)
	writeRegistry(t, p, os.Getpid(), map[string]any{"procStart": selfStart(t)})
	orig := processExists
	t.Cleanup(func() { processExists = orig })
	processExists = func(int) bool { return false }
	pr := newTestProber(p)
	got, err := pr.Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	lv := got[testSID]
	if lv == nil {
		t.Fatal("dead entries are still reported with Alive=false")
	}
	if lv.Alive {
		t.Error("dead process reported alive")
	}
	if lv.Argv != nil || lv.TTY != "" {
		t.Errorf("dead process should not be inspected further: %+v", lv)
	}
}

func TestProbeMergesAgentsJSON(t *testing.T) {
	p := testPaths(t)
	sock := filepath.Join(t.TempDir(), "self.sock")
	if err := os.WriteFile(sock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	writeRegistry(t, p, os.Getpid(), map[string]any{
		"procStart":           selfStart(t),
		"messagingSocketPath": sock,
	})
	pr := New(p)
	pr.agents = func(context.Context) ([]byte, error) {
		return []byte(`[
  {"pid": ` + strconv.Itoa(os.Getpid()) + `, "cwd": "/tmp/project", "kind": "interactive", "startedAt": 1, "sessionId": "` + testSID + `", "name": "renamed-by-agents", "status": "busy"},
  {"pid": 999999999, "cwd": "/tmp/other", "kind": "interactive", "startedAt": 1, "sessionId": "ffffffff-0000-0000-0000-000000000000", "name": "gone", "status": "idle"}
]`), nil
	}
	got, err := pr.Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	lv := got[testSID]
	if lv == nil {
		t.Fatal("merged entry missing")
	}
	if lv.Source != SourceAgentsJSON {
		t.Errorf("Source = %q, want agents-json", lv.Source)
	}
	if lv.Name != "renamed-by-agents" || lv.Status != "busy" {
		t.Errorf("agents fields should win: %+v", lv)
	}
	if !lv.Alive {
		t.Errorf("merged entry should be alive: %+v", lv)
	}
	other := got["ffffffff-0000-0000-0000-000000000000"]
	if other == nil {
		t.Fatal("agents-only entry missing")
	}
	if other.Alive || other.Source != SourceAgentsJSON {
		t.Errorf("agents-only entry with dead pid: %+v", other)
	}
	if d, reason := pr.Degraded(); d {
		t.Errorf("unexpected degraded: %s", reason)
	}
}

func TestProbeAgentsJSONWrappedShapeAndBad(t *testing.T) {
	list, err := parseAgentsJSON([]byte(`{"agents":[{"pid":1,"sessionId":"a"}]}`))
	if err != nil || len(list) != 1 || list[0].SessionID != "a" {
		t.Errorf("wrapped shape: %v %v", list, err)
	}
	if _, err := parseAgentsJSON([]byte("not json")); err == nil {
		t.Error("garbage should fail")
	}
	if _, err := parseAgentsJSON([]byte("")); err == nil {
		t.Error("empty should fail")
	}

	p := testPaths(t)
	pr := New(p)
	pr.agents = func(context.Context) ([]byte, error) { return []byte("garbage"), nil }
	if _, err := pr.Probe(context.Background()); err != nil {
		t.Fatal(err)
	}
	d, reason := pr.Degraded()
	if !d || !strings.Contains(reason, "unreadable") {
		t.Errorf("garbage agents output should degrade: %v %q", d, reason)
	}
	if pr.AgentsError() == nil {
		t.Error("AgentsError should be set")
	}
}

func TestProbeDetectsNewDaemonLock(t *testing.T) {
	p := testPaths(t)
	lock := filepath.Join(p.ClaudeDir, "daemon.lock")
	pr := New(p)
	pr.agents = func(context.Context) ([]byte, error) {
		if err := os.WriteFile(lock, []byte(`{"pid":1}`), 0o600); err != nil {
			return nil, err
		}
		return []byte("[]"), nil
	}
	if _, err := pr.Probe(context.Background()); err != nil {
		t.Fatal(err)
	}
	d, reason := pr.Degraded()
	if !d || !strings.Contains(reason, "daemon.lock") {
		t.Errorf("new daemon.lock should degrade: %v %q", d, reason)
	}

	// A lock that already existed does not count.
	pr2 := New(p)
	pr2.agents = func(context.Context) ([]byte, error) { return []byte("[]"), nil }
	if _, err := pr2.Probe(context.Background()); err != nil {
		t.Fatal(err)
	}
	if d, reason := pr2.Degraded(); d {
		t.Errorf("pre-existing daemon.lock should not degrade: %q", reason)
	}
}

func TestProbeBadRegistryDegradesAndSkipsKeys(t *testing.T) {
	p := testPaths(t)
	if err := os.WriteFile(filepath.Join(p.SessionsDir, "4242.json"), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A key file that must never be opened: unreadable on purpose.
	keyPath := filepath.Join(p.SessionsDir, "4242.deadbeef.key")
	if err := os.WriteFile(keyPath, []byte("secret"), 0o000); err != nil {
		t.Fatal(err)
	}
	// Non-numeric json files are ignored, not errors.
	if err := os.WriteFile(filepath.Join(p.SessionsDir, "notes.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	pr := newTestProber(p)
	got, err := pr.Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("got %d entries, want 0", len(got))
	}
	d, reason := pr.Degraded()
	if !d || !strings.Contains(reason, "4242.json") {
		t.Errorf("broken registry file should degrade: %v %q", d, reason)
	}
	if strings.Contains(reason, ".key") {
		t.Errorf("key files must not be read: %q", reason)
	}
}

func TestProbeMissingSessionsDir(t *testing.T) {
	p := model.PathsFrom(filepath.Join(t.TempDir(), "claude"), filepath.Join(t.TempDir(), "recall"))
	pr := newTestProber(p)
	got, err := pr.Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("got %v", got)
	}
	if d, reason := pr.Degraded(); d {
		t.Errorf("missing registry dir is not degraded: %q", reason)
	}
}

func TestProbePrefersAliveDuplicate(t *testing.T) {
	p := testPaths(t)
	sock := filepath.Join(t.TempDir(), "self.sock")
	if err := os.WriteFile(sock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// Stale entry for the same session id under a dead pid, listed first.
	writeRegistry(t, p, 1, map[string]any{"procStart": "Mon Jan  1 00:00:00 2001", "startedAt": 1})
	writeRegistry(t, p, os.Getpid(), map[string]any{"procStart": selfStart(t), "messagingSocketPath": sock, "startedAt": 2})
	pr := newTestProber(p)
	got, err := pr.Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	lv := got[testSID]
	if lv == nil || !lv.Alive || lv.PID != os.Getpid() {
		t.Fatalf("alive duplicate should win: %+v", lv)
	}
}

func TestBGKindGetsBGInfo(t *testing.T) {
	p := testPaths(t)
	if err := os.WriteFile(filepath.Join(p.ClaudeDir, "daemon.lock"), []byte(`{"pid":`+strconv.Itoa(os.Getpid())+`,"procStart":"`+selfStart(t)+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	writeRegistry(t, p, os.Getpid(), map[string]any{"procStart": selfStart(t), "kind": "bg", "status": "waiting"})
	pr := newTestProber(p)
	got, err := pr.Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	lv := got[testSID]
	if lv == nil || lv.BG == nil {
		t.Fatalf("BG info missing: %+v", lv)
	}
	if lv.BG.ShortID != model.ShortID(testSID) || !lv.BG.DaemonAlive {
		t.Errorf("BG = %+v", lv.BG)
	}
	if lv.WaitingFor != "input" {
		t.Errorf("WaitingFor = %q", lv.WaitingFor)
	}
}

func TestClassifyHost(t *testing.T) {
	cases := map[string]string{
		"/System/Applications/Utilities/Terminal.app/Contents/MacOS/Terminal": "Terminal.app",
		"/Applications/iTerm.app/Contents/MacOS/iTerm2":                       "iTerm2",
		"/Applications/Cursor.app/Contents/MacOS/Cursor":                      "Cursor",
		"/Applications/Visual Studio Code.app/Contents/MacOS/Electron":        "Code",
		"Code Helper (Plugin)": "Code",
		"tmux: server":         "tmux",
		"tmux":                 "tmux",
		"screen":               "screen",
		"-zsh":                 "",
		"login":                "",
		"claude":               "",
		"":                     "",
	}
	for in, want := range cases {
		if got := classifyHost(in); got != want {
			t.Errorf("classifyHost(%q) = %q, want %q", in, got, want)
		}
	}
}

const sampleTable = `    1     0 ??       /sbin/launchd
  702     1 ??       /System/Applications/Utilities/Terminal.app/Contents/MacOS/Terminal
76580   702 ttys033  login
76581 76580 ttys033  -zsh
16490 76581 ttys033  claude
 5000     1 ??       /Applications/Cursor.app/Contents/MacOS/Cursor
 5001  5000 ??       Cursor Helper (Plugin)
 5002  5001 ttys040  /bin/zsh
 5003  5002 ttys040  claude
 6000  6001 ??       orphan
 6001  6000 ??       cycle
`

func TestHostFromTable(t *testing.T) {
	tbl := parseProcTable(sampleTable)
	if len(tbl) != 11 {
		t.Fatalf("parsed %d rows", len(tbl))
	}
	app, tty := hostFromTable(tbl, 16490)
	if app != "Terminal.app" || tty != "ttys033" {
		t.Errorf("Terminal chain: %q %q", app, tty)
	}
	app, tty = hostFromTable(tbl, 5003)
	if app != "Cursor" || tty != "ttys040" {
		t.Errorf("Cursor chain: %q %q", app, tty)
	}
	app, tty = hostFromTable(tbl, 6000)
	if app != "" || tty != "" {
		t.Errorf("cycle: %q %q", app, tty)
	}
	if app, _ := hostFromTable(tbl, 424242); app != "" {
		t.Errorf("unknown pid: %q", app)
	}
}

func TestHostAppOfWalksPpidChain(t *testing.T) {
	orig := runPS
	t.Cleanup(func() { runPS = orig })
	parents := map[string]string{
		"16490": "76581 claude\n",
		"76581": "76580 -zsh\n",
		"76580": "  702 login\n",
		"702":   "    1 /System/Applications/Utilities/Terminal.app/Contents/MacOS/Terminal\n",
	}
	runPS = func(args ...string) (string, error) {
		if len(args) >= 4 && args[1] == "ppid=,comm=" {
			out, ok := parents[args[3]]
			if !ok {
				return "", errors.New("no such process")
			}
			return out, nil
		}
		if len(args) >= 4 && args[1] == "tty=,args=" {
			return "ttys033  claude\n", nil
		}
		return "", errors.New("unexpected ps " + strings.Join(args, " "))
	}
	app, tty := HostAppOf(16490)
	if app != "Terminal.app" || tty != "ttys033" {
		t.Errorf("HostAppOf = %q %q", app, tty)
	}
	if app, tty := HostAppOf(0); app != "" || tty != "" {
		t.Errorf("HostAppOf(0) = %q %q", app, tty)
	}
}

func TestHostAppOfSelfDoesNotPanic(t *testing.T) {
	// Real ps against the test process: any answer is fine, it must not fail.
	_, _ = HostAppOf(os.Getpid())
}

func TestArgvLooksLikeClaude(t *testing.T) {
	cases := []struct {
		argv []string
		want bool
	}{
		{[]string{"claude"}, true},
		{[]string{"/Users/x/.local/bin/claude", "--resume", "abc"}, true},
		{[]string{"node", "/opt/homebrew/lib/node_modules/@anthropic-ai/claude-code/cli.js"}, true},
		{[]string{"bun", "/x/claude"}, true},
		{[]string{"node", "/x/server.js"}, false},
		{[]string{"live.test"}, false},
		{nil, false},
	}
	for _, c := range cases {
		if got := argvLooksLikeClaude(c.argv); got != c.want {
			t.Errorf("argvLooksLikeClaude(%v) = %v", c.argv, got)
		}
	}
}

func TestProcessStartInvalid(t *testing.T) {
	if _, err := ProcessStart(0); err == nil {
		t.Error("pid 0 should fail")
	}
	if _, err := ProcessStart(2147483647); err == nil {
		t.Error("absurd pid should fail")
	}
}
