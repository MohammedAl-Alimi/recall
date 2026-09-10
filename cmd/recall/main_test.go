package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MohammedAl-Alimi/recall/internal/launch"
	"github.com/MohammedAl-Alimi/recall/internal/model"
)

// isolate points every path at temp dirs so no test can touch ~/.claude or
// ~/.recall, and returns the Claude and recall dirs.
func isolate(t *testing.T) (claudeDir, recallDir string) {
	t.Helper()
	claudeDir, recallDir = t.TempDir(), t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", claudeDir)
	t.Setenv("RECALL_DIR", recallDir)
	t.Setenv("RECALL_PROJECTS_DIR", "")
	t.Setenv("RECALL_DRY_RUN", "1")
	t.Setenv("HOME", t.TempDir())
	flagClaudeDir, flagRecallDir = "", ""
	t.Cleanup(func() { flagClaudeDir, flagRecallDir = "", "" })
	return claudeDir, recallDir
}

// execCLI runs the CLI with args and returns exit code, stdout and stderr.
func execCLI(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(args, strings.NewReader(stdin), &out, &errb)
	return code, out.String(), errb.String()
}

const (
	fixtureSID  = "0f1e2d3c-4b5a-4978-8a6b-5c4d3e2f1a0b"
	fixtureSID2 = "0f1e9999-1111-4222-8333-444455556666"
)

// writeFixture synthesizes a minimal Claude config tree with two transcripts
// in one project. Nothing here comes from a real machine.
func writeFixture(t *testing.T, claudeDir string) string {
	t.Helper()
	cwd := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	enc := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, cwd)
	dir := filepath.Join(claudeDir, "projects", enc)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	rec := func(sid, typ, uuid, parent, ts, content string) string {
		m := map[string]any{
			"parentUuid": nil, "isSidechain": false, "type": typ, "uuid": uuid,
			"timestamp": ts, "cwd": cwd, "sessionId": sid, "version": "2.1.230",
			"gitBranch": "main", "entrypoint": "cli", "promptId": "p-" + uuid,
		}
		if parent != "" {
			m["parentUuid"] = parent
		}
		if typ == "user" {
			m["message"] = map[string]any{"role": "user", "content": content}
		} else {
			m["message"] = map[string]any{
				"role": "assistant", "model": "claude-test",
				"content": []map[string]any{{"type": "text", "text": content}},
				"usage":   map[string]any{"input_tokens": 100, "cache_read_input_tokens": 50, "cache_creation_input_tokens": 0, "output_tokens": 20},
			}
		}
		b, _ := json.Marshal(m)
		return string(b)
	}
	lines := []string{
		rec(fixtureSID, "user", "u1", "", "2026-09-01T10:00:00Z", "Add a login page to the app"),
		rec(fixtureSID, "assistant", "a1", "u1", "2026-09-01T10:00:05Z", "Done, the login page is in place."),
		`{"type":"ai-title","aiTitle":"Login page","sessionId":"` + fixtureSID + `"}`,
	}
	if err := os.WriteFile(filepath.Join(dir, fixtureSID+".jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines2 := []string{
		rec(fixtureSID2, "user", "u2", "", "2026-09-02T10:00:00Z", "Fix the flaky test in the scanner"),
		rec(fixtureSID2, "assistant", "a2", "u2", "2026-09-02T10:00:05Z", "The test is fixed."),
		`{"type":"custom-title","customTitle":"Scanner test fix","sessionId":"` + fixtureSID2 + `"}`,
	}
	if err := os.WriteFile(filepath.Join(dir, fixtureSID2+".jsonl"), []byte(strings.Join(lines2, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hist := fmt.Sprintf(`{"display":"Add a login page to the app","pastedContents":{},"timestamp":1756720800000,"project":%q,"sessionId":%q}`+"\n", cwd, fixtureSID)
	if err := os.WriteFile(filepath.Join(claudeDir, "history.jsonl"), []byte(hist), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(`{"cleanupPeriodDays": 365}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return cwd
}

func TestVersionCommand(t *testing.T) {
	isolate(t)
	version = "1.2.3-test"
	code, out, _ := execCLI(t, "", "version")
	if code != 0 || !strings.HasPrefix(out, "recall 1.2.3-test\n") {
		t.Fatalf("code=%d out=%q", code, out)
	}
	code, out, _ = execCLI(t, "", "version", "--short")
	if code != 0 || out != "1.2.3-test\n" {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestPathsFlags(t *testing.T) {
	isolate(t)
	claude := t.TempDir()
	recall := t.TempDir()
	flagClaudeDir, flagRecallDir = claude, recall
	p := paths()
	if p.ClaudeDir != claude || p.RecallDir != recall {
		t.Fatalf("paths() = %+v", p)
	}
	if !strings.HasPrefix(p.ProjectsDir, claude) {
		t.Fatalf("ProjectsDir = %q", p.ProjectsDir)
	}
	// RECALL_PROJECTS_DIR only applies when --claude-dir is not given.
	t.Setenv("RECALL_PROJECTS_DIR", "/nowhere/projects")
	if p := paths(); p.ProjectsDir == "/nowhere/projects" {
		t.Fatalf("--claude-dir must win over RECALL_PROJECTS_DIR, got %q", p.ProjectsDir)
	}
	flagClaudeDir = ""
	if p := paths(); p.ProjectsDir != "/nowhere/projects" {
		t.Fatalf("RECALL_PROJECTS_DIR ignored: %q", p.ProjectsDir)
	}
}

func TestCommandsRegistered(t *testing.T) {
	root := newRoot()
	want := []string{"ls", "open", "new", "exec", "hook", "archive", "restore", "doctor", "setup", "uninstall", "shell", "index", "version"}
	have := map[string]bool{}
	for _, c := range root.Commands() {
		have[c.Name()] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("command %q not registered", w)
		}
	}
	shellCmd, _, err := root.Find([]string{"shell", "print"})
	if err != nil || shellCmd.Name() != "print" {
		t.Fatalf("shell print not found: %v", err)
	}
}

func TestExitCodes(t *testing.T) {
	isolate(t)
	if code, _, errOut := execCLI(t, "", "no-such-command"); code != exitFailure || !strings.Contains(errOut, "recall:") {
		t.Fatalf("unknown command: code=%d err=%q", code, errOut)
	}
	if code, _, _ := execCLI(t, "", "archive"); code != exitUsage {
		t.Fatalf("archive without args should exit %d, got %d", exitUsage, code)
	}
	if code, out, _ := execCLI(t, "", "index"); code != 0 || out != "not yet\n" {
		t.Fatalf("index: code=%d out=%q", code, out)
	}
}

func TestHookAlwaysExitsZeroWithEmptyStdout(t *testing.T) {
	isolate(t)
	code, out, _ := execCLI(t, `{"session_id":"x","hook_event_name":"SessionStart"}`, "hook", "SessionStart")
	if code != 0 {
		t.Fatalf("hook exit code = %d", code)
	}
	if out != "" {
		t.Fatalf("hook printed to stdout: %q", out)
	}
}

func TestFindSession(t *testing.T) {
	sessions := []*model.Session{
		{ID: "aaaa1111-0000-4000-8000-000000000001", Title: "one", Label: "api"},
		{ID: "aaaa2222-0000-4000-8000-000000000002", Title: "two", Label: "web"},
		{ID: "bbbb3333-0000-4000-8000-000000000003", Title: "three", Label: "web"},
	}
	cases := []struct {
		ref     string
		want    string
		wantErr bool
	}{
		{"aaaa1111-0000-4000-8000-000000000001", "one", false},
		{"api", "one", false},
		{"bbbb", "three", false},
		{"BBBB3333", "three", false},
		{"aaaa", "", true},
		{"web", "", true},
		{"zzzz", "", true},
		{"", "", true},
	}
	for _, c := range cases {
		s, err := findSession(sessions, c.ref)
		if c.wantErr {
			if err == nil {
				t.Errorf("%q: want error, got %v", c.ref, s.Title)
				continue
			}
			var ee *exitError
			if !errors.As(err, &ee) || ee.code != exitUsage {
				t.Errorf("%q: lookup error should carry exit code %d: %v", c.ref, exitUsage, err)
			}
			continue
		}
		if err != nil || s.Title != c.want {
			t.Errorf("%q: got %v, %v; want %s", c.ref, s, err, c.want)
		}
	}
}

func TestLsRowStableFields(t *testing.T) {
	s := &model.Session{
		ID: "abcd1234-0000-4000-8000-000000000000", Title: "T", TitleSource: "ai-title", Label: "L",
		State: model.StateNeedsYou, WorkCwd: "/home/u/dev/proj", Cwd: "/home/u", Branch: "main",
		LastActive: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC), Turns: 3, ContextTokens: 1200,
		PRs:  []model.Link{{URL: "https://github.com/x/y/pull/7", Number: 7}},
		Live: &model.Live{PID: 42, Status: "waiting", HostApp: "Terminal.app", TTY: "ttys003"},
	}
	var buf bytes.Buffer
	if err := writeLsJSON(&buf, []*model.Session{s, {ID: "x"}}); err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	want := []string{"id", "title", "titleSource", "label", "state", "stateWord", "project", "workCwd", "cwd", "branch", "lastActive", "createdAt", "turns", "contextTokens", "prs", "live", "interrupted", "headless", "ghost", "archived"}
	for _, row := range rows {
		for _, k := range want {
			if _, ok := row[k]; !ok {
				t.Errorf("missing key %q in %v", k, row)
			}
		}
		if len(row) != len(want) {
			t.Errorf("unexpected keys: got %d want %d", len(row), len(want))
		}
	}
	if rows[0]["project"] != "proj" || rows[0]["stateWord"] != "Needs you" || rows[0]["state"] != "needs_you" {
		t.Errorf("row0 = %v", rows[0])
	}
	live := rows[0]["live"].(map[string]any)
	if live["pid"].(float64) != 42 || live["hostApp"] != "Terminal.app" {
		t.Errorf("live = %v", live)
	}
	if rows[1]["live"] != nil {
		t.Errorf("absent live must be null, got %v", rows[1]["live"])
	}
	if prs, ok := rows[1]["prs"].([]any); !ok || len(prs) != 0 {
		t.Errorf("absent prs must be [], got %v", rows[1]["prs"])
	}
	// An empty list must encode as [] and never null.
	buf.Reset()
	if err := writeLsJSON(&buf, nil); err != nil || strings.TrimSpace(buf.String()) != "[]" {
		t.Fatalf("empty list = %q (%v)", buf.String(), err)
	}
}

func TestLsTableAndFilters(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	list := []*model.Session{
		{ID: "11111111-0000-4000-8000-000000000000", Title: "alpha", WorkCwd: "/w/alpha", Branch: "main", State: model.StateLiveIdle, LastActive: now.Add(-90 * time.Second)},
		{ID: "22222222-0000-4000-8000-000000000000", Title: "beta", Label: "b", Cwd: "/w/beta", State: model.StateClosed, LastActive: now.Add(-49 * time.Hour)},
	}
	var buf bytes.Buffer
	writeLsTable(&buf, list, now)
	out := buf.String()
	if !strings.Contains(out, "11111111") || !strings.Contains(out, "Running") || !strings.Contains(out, "b: beta") || !strings.Contains(out, "2d") {
		t.Fatalf("table:\n%s", out)
	}
	if got := filterProject(list, "beta"); len(got) != 1 || got[0].Title != "beta" {
		t.Fatalf("filterProject = %v", got)
	}
	if got := filterLive(list); len(got) != 1 || got[0].Title != "alpha" {
		t.Fatalf("filterLive = %v", got)
	}
	ages := map[time.Duration]string{30 * time.Second: "now", 5 * time.Minute: "5m", 3 * time.Hour: "3h", 72 * time.Hour: "3d"}
	for d, want := range ages {
		if got := humanAge(now, now.Add(-d)); got != want {
			t.Errorf("humanAge(%v) = %q want %q", d, got, want)
		}
	}
	if got := humanAge(now, time.Time{}); got != "?" {
		t.Errorf("zero time age = %q", got)
	}
	if got := truncate("abcdefghij", 5); got != "abcd…" {
		t.Errorf("truncate = %q", got)
	}
}

func TestNewSessionIDAndArgv(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		id := newSessionID()
		if len(id) != 36 || id[14] != '4' || !strings.ContainsRune("89ab", rune(id[19])) {
			t.Fatalf("bad uuid %q", id)
		}
		if seen[id] {
			t.Fatalf("duplicate uuid %q", id)
		}
		seen[id] = true
	}
	argv := newClaudeArgv("api", []string{"--model", "opus"})
	if strings.Join(argv, " ") != "claude -n api --model opus" {
		t.Fatalf("argv = %v", argv)
	}
	if argv := newClaudeArgv("", nil); strings.Join(argv, " ") != "claude" {
		t.Fatalf("argv = %v", argv)
	}
}

func TestPlanNewWithoutKeep(t *testing.T) {
	isolate(t)
	acts, err := planNew(nil, newSessionID(), "n", "/tmp/x", false, []string{"--verbose"})
	if err != nil || len(acts) != 1 {
		t.Fatalf("planNew = %v, %v", acts, err)
	}
	if acts[0].Kind != "new" || acts[0].Cwd != "/tmp/x" || strings.Join(acts[0].Argv, " ") != "claude -n n --verbose" {
		t.Fatalf("action = %+v", acts[0])
	}
	if got := commandLine(acts[0]); got != "cd /tmp/x && claude -n n --verbose" {
		t.Fatalf("commandLine = %q", got)
	}
}

func TestNewDryRun(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	code, out, errOut := execCLI(t, "", "new", "-n", "demo", "--cwd", dir, "--", "--model", "sonnet")
	if code != 0 {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	if !strings.Contains(out, "claude -n demo --model sonnet") || !strings.Contains(out, "cd "+launch.ShellQuote(dir)) {
		t.Fatalf("out=%q", out)
	}
	if code, _, errOut := execCLI(t, "", "new", "--cwd", filepath.Join(dir, "missing")); code != exitUsage || !strings.Contains(errOut, "missing") {
		t.Fatalf("missing cwd: code=%d err=%q", code, errOut)
	}
}

func TestResolveCwd(t *testing.T) {
	dir := t.TempDir()
	if got, err := resolveCwd(dir); err != nil || got != dir {
		t.Fatalf("resolveCwd(%q) = %q, %v", dir, got, err)
	}
	f := filepath.Join(dir, "file")
	if err := os.WriteFile(f, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveCwd(f); err == nil {
		t.Fatal("file accepted as cwd")
	}
}

func TestPrintAction(t *testing.T) {
	var buf bytes.Buffer
	printAction(&buf, &launch.Action{Kind: "resume", Cwd: "/a b", Argv: []string{"claude", "--resume", "id", "-n", "my name"}, Script: "tell application \"Terminal\"", Note: "1 bg job lost", Description: "resume in place"})
	out := buf.String()
	for _, want := range []string{"resume in place\n", "cd '/a b' && claude --resume id -n 'my name'\n", "osascript:\ntell application", "note: 1 bg job lost"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %q", want, out)
		}
	}
}

func TestExecRecordsExitCode(t *testing.T) {
	_, recallDir := isolate(t)
	t.Setenv("RECALL_DRY_RUN", "")
	code, _, _ := execCLI(t, "", "exec", fixtureSID, "--", "sh", "-c", "exit 3")
	if code != 3 {
		t.Fatalf("exit code passthrough: got %d want 3", code)
	}
	code, out, _ := execCLI(t, "", "exec", fixtureSID, "--", "sh", "-c", "echo hi-$RECALL_SID")
	if code != 0 || !strings.Contains(out, "hi-"+fixtureSID) {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if code, _, errOut := execCLI(t, "", "exec", fixtureSID); code != exitUsage || !strings.Contains(errOut, "missing command") {
		t.Fatalf("missing cmd: code=%d err=%q", code, errOut)
	}
	if code, _, _ := execCLI(t, "", "exec", fixtureSID, "--", "definitely-not-a-binary-xyz"); code != 127 {
		t.Fatalf("missing binary should exit 127, got %d", code)
	}
	data, err := os.ReadFile(filepath.Join(recallDir, "events.jsonl"))
	if err != nil {
		t.Fatalf("events.jsonl not written: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 3 {
		t.Fatalf("want at least 3 events, got %d: %s", len(lines), data)
	}
	var ev map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &ev); err != nil {
		t.Fatal(err)
	}
	if ev["event"] != "exec" || ev["sid"] != fixtureSID || ev["exit"].(float64) != 3 {
		t.Fatalf("event = %v", ev)
	}
}

func TestExecDryRun(t *testing.T) {
	isolate(t)
	code, out, _ := execCLI(t, "", "exec", fixtureSID, "--", "true")
	if code != 0 || !strings.Contains(out, "would run for "+model.ShortID(fixtureSID)+": true") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestLoopsAfter(t *testing.T) {
	act := &launch.Action{Kind: "resume", Argv: []string{"claude", "--resume", "x"}}
	if !loopsAfter(act) {
		t.Error("resume in place should loop")
	}
	if loopsAfter(&launch.Action{Kind: "resume", Argv: act.Argv, Script: "tell app"}) {
		t.Error("new-tab script must not loop")
	}
	if loopsAfter(&launch.Action{Kind: "focus", Script: "tell app"}) || loopsAfter(&launch.Action{Kind: "print"}) || loopsAfter(nil) {
		t.Error("focus/print/nil must not loop")
	}
	if !loopsAfter(&launch.Action{Kind: "attach", Argv: []string{"tmux"}}) {
		t.Error("attach should loop")
	}
}

func TestRunChildExitCode(t *testing.T) {
	var out bytes.Buffer
	code, err := runChild(&launch.Action{Argv: []string{"sh", "-c", "pwd; exit 4"}, Cwd: os.TempDir()}, strings.NewReader(""), &out, &out)
	if err != nil || code != 4 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if !strings.Contains(out.String(), strings.TrimSuffix(os.TempDir(), "/")) {
		t.Fatalf("child did not run in cwd: %q", out.String())
	}
	if code, err := runChild(&launch.Action{Argv: []string{"definitely-not-a-binary-xyz"}}, nil, &out, &out); code != 127 || err == nil {
		t.Fatalf("missing binary: code=%d err=%v", code, err)
	}
	if _, err := runChild(&launch.Action{}, nil, &out, &out); err == nil {
		t.Fatal("empty argv must error")
	}
}

func TestRunActionDryRunNeverExecs(t *testing.T) {
	isolate(t)
	var buf bytes.Buffer
	act := &launch.Action{Kind: "resume", Cwd: "/", Argv: []string{"claude", "--resume", fixtureSID}}
	if err := runAction(act, &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "claude --resume "+fixtureSID) {
		t.Fatalf("dry run output = %q", buf.String())
	}
}
