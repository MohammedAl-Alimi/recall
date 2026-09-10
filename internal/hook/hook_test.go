package hook

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/MohammedAl-Alimi/recall/internal/archive"
	"github.com/MohammedAl-Alimi/recall/internal/model"
	"github.com/MohammedAl-Alimi/recall/internal/state"
)

const sid = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

func tempPaths(t *testing.T) model.Paths {
	t.Helper()
	return model.PathsFrom(t.TempDir(), filepath.Join(t.TempDir(), "recall"))
}

func readEvents(t *testing.T, p model.Paths) []map[string]any {
	t.Helper()
	f, err := os.Open(filepath.Join(p.RecallDir, state.EventsFile))
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	defer f.Close()
	var out []map[string]any
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var ev map[string]any
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			t.Fatal(err)
		}
		out = append(out, ev)
	}
	return out
}

// fakeChain installs a synthetic ppid chain: 100 (hook shell) -> 90 (claude) -> 80 (zsh) -> 1.
func fakeChain(t *testing.T, claudeArgs []string) {
	t.Helper()
	oldStart, oldPs := startPID, psLookup
	startPID = func() int { return 100 }
	psLookup = func(pid int) (int, []string, error) {
		switch pid {
		case 100:
			return 90, []string{"/bin/zsh", "-c", "recall hook SessionStart"}, nil
		case 90:
			return 80, claudeArgs, nil
		case 80:
			return 1, []string{"-zsh"}, nil
		}
		return 0, nil, errors.New("no such pid")
	}
	t.Cleanup(func() { startPID, psLookup = oldStart, oldPs })
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	return buf.String()
}

func TestSessionStartRecordsLaunch(t *testing.T) {
	p := tempPaths(t)
	t.Setenv("TERM_PROGRAM", "Apple_Terminal")
	t.Setenv("TMUX", "")
	fakeChain(t, []string{"claude", "--add-dir", "/tmp/x", "--permission-mode", "plan"})
	in := `{"session_id":"` + sid + `","transcript_path":"/tmp/t.jsonl","cwd":"/tmp/proj","hook_event_name":"SessionStart","source":"startup"}`
	out := captureStdout(t, func() {
		if err := Handle(p, "SessionStart", strings.NewReader(in)); err != nil {
			t.Error(err)
		}
	})
	if out != "" {
		t.Fatalf("stdout must be empty, got %q", out)
	}
	l, err := state.LoadLaunch(p, sid)
	if err != nil || l == nil {
		t.Fatalf("launch: %v %v", l, err)
	}
	if l.Argv[0] != "claude" || l.Cwd != "/tmp/proj" || l.TermProgram != "Apple_Terminal" || l.RecordedBy != "hook" || l.Mux != "" {
		t.Fatalf("launch = %+v", l)
	}
	evs := readEvents(t, p)
	if len(evs) != 1 || evs[0]["event"] != "SessionStart" || evs[0]["sid"] != sid || evs[0]["source"] != "startup" {
		t.Fatalf("events = %+v", evs)
	}
	if _, err := os.Stat(filepath.Join(p.RecallDir, LogFile)); err == nil {
		t.Fatal("no hook.log expected on success")
	}
}

func TestSessionStartNodeClaude(t *testing.T) {
	p := tempPaths(t)
	fakeChain(t, []string{"node", "/opt/homebrew/lib/node_modules/@anthropic-ai/claude-code/cli.js", "--resume", "x"})
	in := `{"session_id":"` + sid + `","cwd":"/tmp/proj","hook_event_name":"SessionStart","source":"resume"}`
	_ = Handle(p, "SessionStart", strings.NewReader(in))
	l, _ := state.LoadLaunch(p, sid)
	if l == nil || len(l.Argv) != 4 {
		t.Fatalf("launch = %+v", l)
	}
}

func TestSessionStartNoClaudeInChain(t *testing.T) {
	p := tempPaths(t)
	fakeChain(t, []string{"python3", "script.py"})
	in := `{"session_id":"` + sid + `","hook_event_name":"SessionStart"}`
	if err := Handle(p, "SessionStart", strings.NewReader(in)); err != nil {
		t.Fatal(err)
	}
	if l, _ := state.LoadLaunch(p, sid); l != nil {
		t.Fatalf("unexpected launch: %+v", l)
	}
	evs := readEvents(t, p)
	if len(evs) != 1 || evs[0]["launch"] != "claude process not found" {
		t.Fatalf("events = %+v", evs)
	}
}

func TestSessionEndArchives(t *testing.T) {
	p := tempPaths(t)
	proj := filepath.Join(p.ProjectsDir, "-tmp-proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	tp := filepath.Join(proj, sid+".jsonl")
	if err := os.WriteFile(tp, []byte(`{"type":"user","message":{"role":"user","content":"hi"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	in := `{"session_id":"` + sid + `","transcript_path":"` + tp + `","cwd":"/tmp/proj","hook_event_name":"SessionEnd","reason":"exit"}`
	if err := Handle(p, "SessionEnd", strings.NewReader(in)); err != nil {
		t.Fatal(err)
	}
	ap, ok := archive.HasArchive(p, sid)
	if !ok {
		t.Fatal("transcript not archived")
	}
	a, _ := os.Stat(ap)
	b, _ := os.Stat(tp)
	if !os.SameFile(a, b) {
		t.Fatal("archive is not a hard link")
	}
	if _, err := os.Stat(filepath.Join(archive.Dir(p, sid), archive.SidecarDirName)); err == nil {
		t.Fatal("SessionEnd must not copy sidecars")
	}
	evs := readEvents(t, p)
	if len(evs) != 1 || evs[0]["reason"] != "exit" || evs[0]["archive"] == nil {
		t.Fatalf("events = %+v", evs)
	}
}

func TestSessionEndMissingTranscriptLogsAndExitsZero(t *testing.T) {
	p := tempPaths(t)
	in := `{"session_id":"` + sid + `","transcript_path":"/nonexistent/x.jsonl","hook_event_name":"SessionEnd"}`
	if err := Handle(p, "SessionEnd", strings.NewReader(in)); err != nil {
		t.Fatalf("Handle must return nil, got %v", err)
	}
	log, err := os.ReadFile(filepath.Join(p.RecallDir, LogFile))
	if err != nil || !strings.Contains(string(log), "archive") {
		t.Fatalf("hook.log = %q %v", log, err)
	}
	if evs := readEvents(t, p); len(evs) != 1 {
		t.Fatalf("event still expected: %+v", evs)
	}
}

func TestNotificationPermissionPrompt(t *testing.T) {
	p := tempPaths(t)
	in := `{"session_id":"` + sid + `","hook_event_name":"Notification","notification_type":"permission_prompt","message":"Claude needs your permission to use Bash","title":"Claude Code"}`
	_ = Handle(p, "Notification", strings.NewReader(in))
	in2 := `{"session_id":"` + sid + `","hook_event_name":"Notification","notification_type":"idle_prompt","message":"waiting for input"}`
	_ = Handle(p, "Notification", strings.NewReader(in2))
	evs := readEvents(t, p)
	if len(evs) != 2 {
		t.Fatalf("events = %+v", evs)
	}
	if evs[0]["waiting"] != true || evs[0]["waiting_for"] != "Claude needs your permission to use Bash" {
		t.Fatalf("permission event = %+v", evs[0])
	}
	if _, ok := evs[1]["waiting"]; ok {
		t.Fatalf("idle event marked waiting: %+v", evs[1])
	}
}

func TestBadJSONAndEmptyStdin(t *testing.T) {
	p := tempPaths(t)
	if err := Handle(p, "SessionEnd", strings.NewReader("{nope")); err != nil {
		t.Fatal(err)
	}
	if err := Handle(p, "SessionEnd", nil); err != nil {
		t.Fatal(err)
	}
	if err := Handle(p, "", strings.NewReader(`{"hook_event_name":"Stop"}`)); err != nil {
		t.Fatal(err)
	}
	evs := readEvents(t, p)
	if len(evs) != 3 || evs[2]["event"] != "Stop" {
		t.Fatalf("events = %+v", evs)
	}
	if _, err := os.Stat(filepath.Join(p.RecallDir, LogFile)); err != nil {
		t.Fatal("bad json should be logged")
	}
}

func TestIsClaudeArgv(t *testing.T) {
	cases := map[bool][][]string{
		true:  {{"claude"}, {"/usr/local/bin/claude", "--resume", "x"}, {"node", "/x/claude-code/cli.js"}},
		false: {{"-zsh"}, {"node", "/x/other/cli.js"}, {"recall", "hook", "SessionStart"}, {}},
	}
	for want, list := range cases {
		for _, args := range list {
			if got := isClaudeArgv(args); got != want {
				t.Errorf("isClaudeArgv(%v) = %v", args, got)
			}
		}
	}
}

func TestProcLookupReal(t *testing.T) {
	ppid, args, err := psLookup(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if ppid != os.Getppid() || len(args) == 0 {
		t.Fatalf("ppid=%d args=%v", ppid, args)
	}
	if _, _, err := psLookup(0x7fffffff); err == nil {
		t.Fatal("expected error for bogus pid")
	}
}

// TestProcLookupKeepsQuotedArgs spawns a child whose argv holds a prompt
// with flag-like text inside and checks that the lookup returns the exact
// vector: one token per argument, no whitespace splitting.
func TestProcLookupKeepsQuotedArgs(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh not available")
	}
	prompt := "explain the --add-dir /Users option  with  double spaces"
	cmd := exec.Command(sh, "-c", "sleep 30", "sh", prompt, "", "--model=x y")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })
	ppid, args, err := psLookup(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if ppid != os.Getpid() {
		t.Fatalf("ppid = %d, want %d", ppid, os.Getpid())
	}
	want := []string{sh, "-c", "sleep 30", "sh", prompt, "", "--model=x y"}
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		// The ps fallback deliberately returns argv[0] only.
		want = want[:1]
	}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %q, want %q", args, want)
	}
}

// TestSessionStartPromptIsNotSplit records a launch whose prompt contains
// flag-like text and checks it is saved as a single argv element.
func TestSessionStartPromptIsNotSplit(t *testing.T) {
	p := tempPaths(t)
	prompt := "explain the --add-dir /Users option"
	fakeChain(t, []string{"claude", "-p", prompt, "--append-system-prompt", "be terse --model x"})
	in := `{"session_id":"` + sid + `","cwd":"/tmp/proj","hook_event_name":"SessionStart"}`
	_ = Handle(p, "SessionStart", strings.NewReader(in))
	l, err := state.LoadLaunch(p, sid)
	if err != nil || l == nil {
		t.Fatalf("launch: %v %v", l, err)
	}
	want := []string{"claude", "-p", prompt, "--append-system-prompt", "be terse --model x"}
	if !reflect.DeepEqual(l.Argv, want) {
		t.Fatalf("argv = %q, want %q", l.Argv, want)
	}
	for _, a := range l.Argv {
		if a == "--add-dir" || a == "/Users" || a == "--model" {
			t.Fatalf("prompt text leaked as a flag token: %q", l.Argv)
		}
	}
}
