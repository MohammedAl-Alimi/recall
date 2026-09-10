package launch

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MohammedAl-Alimi/recall/internal/model"
)

const sid = "0f1e2d3c-4b5a-6978-8a9b-c0d1e2f3a4b5"

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"":            "''",
		"abc":         "abc",
		"/a/b-c.d":    "/a/b-c.d",
		"a b":         "'a b'",
		"it's":        `'it'\''s'`,
		"$HOME":       "'$HOME'",
		"--resume=x":  "--resume=x",
		"Work @acme.io": "'Work @acme.io'",
		"a\"b":        `'a"b'`,
		"a\nb":        "'a\nb'",
		"back\\slash": `'back\slash'`,
		"tab\there":   "'tab\there'",
		"ü":           "'ü'",
		"*":           "'*'",
	}
	for in, want := range cases {
		if got := ShellQuote(in); got != want {
			t.Errorf("ShellQuote(%q) = %q, want %q", in, got, want)
		}
	}
	if got := ShellJoin([]string{"claude", "--resume", "x y"}); got != "claude --resume 'x y'" {
		t.Errorf("ShellJoin = %q", got)
	}
}

func closedSession(t *testing.T) *model.Session {
	t.Helper()
	dir := t.TempDir()
	return &model.Session{ID: sid, Cwd: dir, WorkCwd: dir, Title: "fix tests"}
}

func TestPlanTiers(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name     string
		sess     *model.Session
		opts     Options
		wantKind string
		wantErr  bool
		check    func(t *testing.T, a *Action)
	}{
		{
			name:    "ghost cannot be opened",
			sess:    &model.Session{ID: sid, Ghost: true},
			wantErr: true,
		},
		{
			name:     "live in Terminal.app focuses by tty",
			sess:     &model.Session{ID: sid, Cwd: dir, Live: &model.Live{Alive: true, PID: 4242, HostApp: "Terminal", TTY: "ttys007"}},
			wantKind: KindFocus,
			check: func(t *testing.T, a *Action) {
				if !strings.Contains(a.Script, `tell application "Terminal"`) || !strings.Contains(a.Script, `"/dev/ttys007"`) {
					t.Errorf("focus script missing tty match:\n%s", a.Script)
				}
				if len(a.Argv) != 0 {
					t.Errorf("focus should carry no argv, got %v", a.Argv)
				}
			},
		},
		{
			name:     "live in iTerm2 focuses by tty",
			sess:     &model.Session{ID: sid, Cwd: dir, Live: &model.Live{Alive: true, PID: 1, HostApp: "iTerm2", TTY: "/dev/ttys002"}},
			wantKind: KindFocus,
			check: func(t *testing.T, a *Action) {
				if !strings.Contains(a.Script, `tell application "iTerm2"`) || !strings.Contains(a.Script, `"/dev/ttys002"`) {
					t.Errorf("iterm focus script wrong:\n%s", a.Script)
				}
			},
		},
		{
			name:     "kept session attaches through tmux",
			sess:     &model.Session{ID: sid, Cwd: dir, Live: &model.Live{Alive: true, PID: 7, HostApp: "tmux", Mux: &model.MuxInfo{Socket: "recall", SessionName: "rc-0f1e2d3c"}}},
			wantKind: KindAttach,
			check: func(t *testing.T, a *Action) {
				want := "tmux -L recall attach-session -t rc-0f1e2d3c"
				if got := strings.Join(a.Argv, " "); got != want {
					t.Errorf("attach argv = %q, want %q", got, want)
				}
				if a.Script != "" {
					t.Errorf("attach without NewTab must not carry a script")
				}
			},
		},
		{
			name:     "kept session with empty socket uses default",
			sess:     &model.Session{ID: sid, Cwd: dir, Live: &model.Live{Alive: true, Mux: &model.MuxInfo{SessionName: "rc-0f1e2d3c", Attached: true}}},
			wantKind: KindAttach,
			check: func(t *testing.T, a *Action) {
				if a.Argv[2] != DefaultMuxSocket {
					t.Errorf("socket = %q", a.Argv[2])
				}
				if !strings.Contains(a.Note, "already attached") {
					t.Errorf("note = %q", a.Note)
				}
			},
		},
		{
			name:     "live in Cursor prints a hint",
			sess:     &model.Session{ID: sid, Cwd: dir, Live: &model.Live{Alive: true, PID: 99, HostApp: "Cursor", TTY: "ttys010"}},
			wantKind: KindPrint,
			check: func(t *testing.T, a *Action) {
				if !strings.Contains(a.Description, "Cursor") || !strings.Contains(a.Description, "99") {
					t.Errorf("description = %q", a.Description)
				}
				if a.Script != "" {
					t.Errorf("print must not carry a script")
				}
				if !strings.Contains(strings.Join(a.Argv, " "), "--fork-session") {
					t.Errorf("print should suggest a fork argv, got %v", a.Argv)
				}
			},
		},
		{
			name:     "live in VS Code prints a hint",
			sess:     &model.Session{ID: sid, Cwd: dir, Live: &model.Live{Alive: true, PID: 5, HostApp: "Code"}},
			wantKind: KindPrint,
			check: func(t *testing.T, a *Action) {
				if !strings.Contains(a.Description, "Code") {
					t.Errorf("description = %q", a.Description)
				}
			},
		},
		{
			name:     "live in Terminal without tty falls back to print",
			sess:     &model.Session{ID: sid, Cwd: dir, Live: &model.Live{Alive: true, PID: 5, HostApp: "Terminal"}},
			wantKind: KindPrint,
		},
		{
			name:     "dead live record resumes",
			sess:     &model.Session{ID: sid, Cwd: dir, Live: &model.Live{Alive: false, HostApp: "Terminal", TTY: "ttys001"}},
			opts:     Options{InPlace: true},
			wantKind: KindResume,
		},
		{
			name:     "fork of a live session resumes a copy",
			sess:     &model.Session{ID: sid, Cwd: dir, Live: &model.Live{Alive: true, HostApp: "Terminal", TTY: "ttys001"}},
			opts:     Options{Fork: true, InPlace: true},
			wantKind: KindResume,
			check: func(t *testing.T, a *Action) {
				if !strings.Contains(strings.Join(a.Argv, " "), "--fork-session") {
					t.Errorf("argv = %v", a.Argv)
				}
			},
		},
		{
			name:     "closed in Apple_Terminal opens a new tab",
			sess:     &model.Session{ID: sid, Cwd: dir},
			opts:     Options{TermProgram: "Apple_Terminal"},
			wantKind: KindResume,
			check: func(t *testing.T, a *Action) {
				if a.Script == "" || !strings.Contains(a.Script, `tell application "Terminal"`) {
					t.Errorf("expected Terminal new tab script, got:\n%s", a.Script)
				}
				if !strings.Contains(a.Script, "cd "+ShellQuote(dir)+" && claude --resume "+sid) {
					t.Errorf("script lacks cd && resume:\n%s", a.Script)
				}
				if a.Cwd != dir {
					t.Errorf("cwd = %q", a.Cwd)
				}
			},
		},
		{
			name:     "closed in iTerm opens a new iTerm tab",
			sess:     &model.Session{ID: sid, Cwd: dir},
			opts:     Options{TermProgram: "iTerm.app"},
			wantKind: KindResume,
			check: func(t *testing.T, a *Action) {
				if !strings.Contains(a.Script, `tell application "iTerm2"`) {
					t.Errorf("expected iTerm script, got:\n%s", a.Script)
				}
			},
		},
		{
			name:     "closed with unknown terminal resumes in place",
			sess:     &model.Session{ID: sid, Cwd: dir},
			opts:     Options{TermProgram: "none"},
			wantKind: KindResume,
			check: func(t *testing.T, a *Action) {
				if a.Script != "" {
					t.Errorf("in-place resume must not carry a script")
				}
				if got := strings.Join(a.Argv, " "); got != "claude --resume "+sid {
					t.Errorf("argv = %q", got)
				}
			},
		},
		{
			name:     "closed with no TermProgram resumes in place",
			sess:     &model.Session{ID: sid, Cwd: dir},
			wantKind: KindResume,
			check: func(t *testing.T, a *Action) {
				if a.Script != "" {
					t.Errorf("no TermProgram must resume in place, got script:\n%s", a.Script)
				}
			},
		},
		{
			name:     "InPlace overrides Apple_Terminal default",
			sess:     &model.Session{ID: sid, Cwd: dir},
			opts:     Options{TermProgram: "Apple_Terminal", InPlace: true},
			wantKind: KindResume,
			check: func(t *testing.T, a *Action) {
				if a.Script != "" {
					t.Errorf("InPlace must suppress the tab script")
				}
			},
		},
		{
			name:     "NewTab forces a tab even without a known terminal",
			sess:     &model.Session{ID: sid, Cwd: dir},
			opts:     Options{TermProgram: "none", NewTab: true},
			wantKind: KindResume,
			check: func(t *testing.T, a *Action) {
				if a.Script == "" {
					t.Errorf("NewTab must produce a script")
				}
			},
		},
		{
			name:     "Keep wraps resume in tmux",
			sess:     &model.Session{ID: sid, Cwd: dir},
			opts:     Options{TermProgram: "none", Keep: true},
			wantKind: KindAttach,
			check: func(t *testing.T, a *Action) {
				got := strings.Join(a.Argv, " ")
				for _, want := range []string{"tmux -L recall new-session -A -s rc-0f1e2d3c -c " + dir, "RECALL_SID=" + sid, "RECALL_BYPASS=1", "claude --resume " + sid} {
					if !strings.Contains(got, want) {
						t.Errorf("kept argv %q lacks %q", got, want)
					}
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TERM_PROGRAM", "")
			a, err := Plan(tc.sess, tc.opts)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", a)
				}
				return
			}
			if err != nil {
				t.Fatalf("Plan: %v", err)
			}
			if a.Kind != tc.wantKind {
				t.Fatalf("kind = %q, want %q (%+v)", a.Kind, tc.wantKind, a)
			}
			if a.Description == "" {
				t.Errorf("description must not be empty")
			}
			if tc.check != nil {
				tc.check(t, a)
			}
		})
	}
}

func TestPlanUsesTermProgramEnv(t *testing.T) {
	sess := closedSession(t)
	t.Setenv("TERM_PROGRAM", "Apple_Terminal")
	a, err := Plan(sess, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if a.Script == "" {
		t.Errorf("TERM_PROGRAM=Apple_Terminal should default to a new tab")
	}
	t.Setenv("TERM_PROGRAM", "WezTerm")
	a, err = Plan(sess, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if a.Script != "" {
		t.Errorf("unknown TERM_PROGRAM should resume in place")
	}
}

func TestPlanNew(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "")
	dir := t.TempDir()
	a, err := Plan(nil, Options{Cwd: dir, Name: "spike", PermMode: "plan", ExtraArgs: []string{"--model", "opus", "--dangerously-skip-permissions"}})
	if err != nil {
		t.Fatal(err)
	}
	if a.Kind != KindNew || a.Cwd != dir {
		t.Fatalf("got %+v", a)
	}
	if got := strings.Join(a.Argv, " "); got != "claude -n spike --permission-mode plan --model opus" {
		t.Errorf("argv = %q", got)
	}
	if !strings.Contains(a.Note, "bypass") {
		t.Errorf("note should mention the refused bypass, got %q", a.Note)
	}
	if _, err := Plan(nil, Options{Cwd: filepath.Join(dir, "missing")}); err == nil {
		t.Errorf("missing cwd must fail")
	}
}

func TestBuildResumeCwdPreference(t *testing.T) {
	work := t.TempDir()
	last := t.TempDir()
	first := t.TempDir()
	gone := filepath.Join(t.TempDir(), "gone")

	cases := []struct {
		name      string
		sess      model.Session
		wantCwd   string
		wantNote  string
		noNoteSub string
	}{
		{"WorkCwd wins", model.Session{WorkCwd: work, LastCwd: last, Cwd: first}, work, "", "gone"},
		{"LastCwd when WorkCwd missing", model.Session{WorkCwd: gone, LastCwd: last, Cwd: first}, last, "is gone", ""},
		{"Cwd when others missing", model.Session{WorkCwd: gone, LastCwd: gone, Cwd: first}, first, "is gone", ""},
		{"Cwd when others empty", model.Session{Cwd: first}, first, "", "gone"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.sess.ID = sid
			cwd, _, note := BuildResume(&tc.sess, Options{})
			if cwd != tc.wantCwd {
				t.Errorf("cwd = %q, want %q", cwd, tc.wantCwd)
			}
			if tc.wantNote != "" && !strings.Contains(note, tc.wantNote) {
				t.Errorf("note %q lacks %q", note, tc.wantNote)
			}
			if tc.noNoteSub != "" && strings.Contains(note, tc.noNoteSub) {
				t.Errorf("note %q must not contain %q", note, tc.noNoteSub)
			}
		})
	}

	t.Run("home fallback", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		cwd, _, note := BuildResume(&model.Session{ID: sid, Cwd: gone}, Options{})
		if cwd != home {
			t.Errorf("cwd = %q, want %q", cwd, home)
		}
		if !strings.Contains(note, gone) {
			t.Errorf("note = %q", note)
		}
	})
}

func TestBuildResumeArgv(t *testing.T) {
	dir := t.TempDir()
	addDir := t.TempDir()
	mcp := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(mcp, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	missingDir := filepath.Join(dir, "nope")
	missingSettings := filepath.Join(dir, "settings-gone.json")

	cases := []struct {
		name     string
		sess     model.Session
		opts     Options
		want     string
		noteHas  []string
		noteNone []string
	}{
		{
			name: "plain resume",
			sess: model.Session{Cwd: dir},
			want: "claude --resume " + sid,
		},
		{
			name: "fork and name",
			sess: model.Session{Cwd: dir},
			opts: Options{Fork: true, Name: "my fix"},
			want: "claude --resume " + sid + " --fork-session -n 'my fix'",
		},
		{
			name: "recorded plan mode is replayed",
			sess: model.Session{Cwd: dir, PermMode: "plan"},
			want: "claude --resume " + sid + " --permission-mode plan",
		},
		{
			name: "recorded acceptEdits is not replayed",
			sess: model.Session{Cwd: dir, PermMode: "acceptEdits"},
			want: "claude --resume " + sid,
		},
		{
			name:    "recorded bypassPermissions is refused",
			sess:    model.Session{Cwd: dir, PermMode: "bypassPermissions"},
			want:    "claude --resume " + sid,
			noteHas: []string{"bypassPermissions"},
		},
		{
			name: "option plan overrides recorded mode",
			sess: model.Session{Cwd: dir, PermMode: "default"},
			opts: Options{PermMode: "plan"},
			want: "claude --resume " + sid + " --permission-mode plan",
		},
		{
			name:    "option bypass is refused",
			sess:    model.Session{Cwd: dir},
			opts:    Options{PermMode: "bypassPermissions"},
			want:    "claude --resume " + sid,
			noteHas: []string{"bypassPermissions"},
		},
		{
			name: "replays allowlisted launch flags and strips the rest",
			sess: model.Session{Cwd: dir, Launch: &model.Launch{Argv: []string{
				"claude", "--session-id", "aaaa", "--add-dir", addDir, "--model", "opus",
				"--effort", "high", "--agent", "reviewer", "--mcp-config", mcp,
				"--permission-mode", "bypassPermissions", "--dangerously-skip-permissions",
				"-n", "old name", "--verbose", "fix the bug in main.go",
			}}},
			want:     "claude --resume " + sid + " --add-dir " + addDir + " --model opus --effort high --agent reviewer --mcp-config " + mcp,
			noteHas:  []string{"MCP servers reconnect"},
			noteNone: []string{"dropped"},
		},
		{
			name: "strips resume, continue and prompt from a node launcher argv",
			sess: model.Session{Cwd: dir, Launch: &model.Launch{Argv: []string{
				"node", "/usr/local/lib/node_modules/@anthropic-ai/claude-code/cli.js", "--resume", "bbbb", "-c", "--model=sonnet", "--fork-session", "hello there",
			}}},
			want: "claude --resume " + sid + " --model sonnet",
		},
		{
			name: "drops path flags whose target is missing and notes them",
			sess: model.Session{Cwd: dir, Launch: &model.Launch{Argv: []string{
				"claude", "--add-dir", missingDir, "--settings", missingSettings, "--plugin-dir", addDir, "--model", "opus",
			}}},
			want:    "claude --resume " + sid + " --plugin-dir " + addDir + " --model opus",
			noteHas: []string{"dropped --add-dir " + missingDir, "dropped --settings " + missingSettings},
		},
		{
			name: "inline JSON mcp config is replayed",
			sess: model.Session{Cwd: dir, Launch: &model.Launch{Argv: []string{"claude", "--mcp-config", `{"mcpServers":{}}`}}},
			want: "claude --resume " + sid + " --mcp-config '{\"mcpServers\":{}}'",
		},
		{
			name: "value flag at end of argv without value is ignored",
			sess: model.Session{Cwd: dir, Launch: &model.Launch{Argv: []string{"claude", "--model"}}},
			want: "claude --resume " + sid,
		},
		{
			name: "value flag followed by another flag is ignored",
			sess: model.Session{Cwd: dir, Launch: &model.Launch{Argv: []string{"claude", "--model", "--verbose", "--effort", "low"}}},
			want: "claude --resume " + sid + " --effort low",
		},
		{
			name:     "extra args appended with bypass scrubbed",
			sess:     model.Session{Cwd: dir},
			opts:     Options{ExtraArgs: []string{"--verbose", "--permission-mode", "bypassPermissions", "--allow-dangerously-skip-permissions", "--permission-mode=bypassPermissions"}},
			want:     "claude --resume " + sid + " --verbose",
			noteHas:  []string{"refused to pass bypassPermissions"},
			noteNone: []string{"dropped"},
		},
		{
			name:    "loss notes",
			sess:    model.Session{Cwd: dir, DanglingTool: "Bash", BgJobsLost: 2, ContextTokens: 612_000},
			want:    "claude --resume " + sid,
			noteHas: []string{"tool call Bash", "2 background job(s)", "612k tokens"},
		},
		{
			name:     "no loss notes when nothing is lost",
			sess:     model.Session{Cwd: dir, ContextTokens: 120_000},
			want:     "claude --resume " + sid,
			noteNone: []string{"tokens", "tool call", "background"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.sess.ID = sid
			_, argv, note := BuildResume(&tc.sess, tc.opts)
			got := ShellJoin(argv)
			if got != tc.want {
				t.Errorf("argv = %q\n     want %q", got, tc.want)
			}
			for _, a := range argv {
				if strings.Contains(a, "bypassPermissions") || strings.Contains(a, "dangerously-skip") {
					t.Errorf("argv leaks bypass: %v", argv)
				}
			}
			for _, s := range tc.noteHas {
				if !strings.Contains(note, s) {
					t.Errorf("note %q lacks %q", note, s)
				}
			}
			for _, s := range tc.noteNone {
				if strings.Contains(note, s) {
					t.Errorf("note %q must not contain %q", note, s)
				}
			}
		})
	}
}

func TestFocusTerminalScript(t *testing.T) {
	for _, tty := range []string{"ttys003", "/dev/ttys003"} {
		s := FocusTerminalScript(tty)
		for _, want := range []string{
			`tell application "Terminal"`,
			"repeat with w in windows",
			"repeat with t in tabs of w",
			`if tty of t is "/dev/ttys003" then`,
			"set selected tab of w to t",
			"set frontmost of w to true",
			"activate",
			"end tell",
		} {
			if !strings.Contains(s, want) {
				t.Errorf("FocusTerminalScript(%q) lacks %q:\n%s", tty, want, s)
			}
		}
		if strings.Contains(s, `"/dev//dev/`) {
			t.Errorf("tty doubled: %s", s)
		}
	}
	if !strings.Contains(FocusITermScript("ttys001"), `tty of s is "/dev/ttys001"`) {
		t.Errorf("iTerm focus script wrong")
	}
}

func TestNewTerminalTabScript(t *testing.T) {
	s := NewTerminalTabScript("/Users/me/Work @acme.io", `claude --resume abc -n "it's"`)
	wantLine := `"cd '/Users/me/Work @acme.io' && claude --resume abc -n \"it's\""`
	for _, want := range []string{
		`tell application "Terminal"`,
		"activate",
		"if (count of windows) is 0 then",
		"do script " + wantLine + "\n",
		`keystroke "t" using command down`,
		"do script " + wantLine + " in front window",
		"end tell",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("script lacks %q:\n%s", want, s)
		}
	}
	// backslashes are escaped for AppleScript
	s = NewTerminalTabScript("/tmp", `echo a\b`)
	if !strings.Contains(s, `"cd /tmp && echo a\\b"`) {
		t.Errorf("backslash not escaped:\n%s", s)
	}
	// no cwd means no cd
	if !strings.Contains(NewTerminalTabScript("", "ls"), `do script "ls"`) {
		t.Errorf("empty cwd should skip cd")
	}
	it := NewITermTabScript("/tmp/x y", "claude")
	for _, want := range []string{`tell application "iTerm2"`, "create tab with default profile", `write text "cd '/tmp/x y' && claude"`} {
		if !strings.Contains(it, want) {
			t.Errorf("iterm script lacks %q:\n%s", want, it)
		}
	}
}

func TestRunDryRun(t *testing.T) {
	t.Setenv("RECALL_DRY_RUN", "1")
	var buf bytes.Buffer
	old := Stdout
	Stdout = &buf
	t.Cleanup(func() { Stdout = old })

	a := &Action{Kind: KindResume, Cwd: "/tmp/p q", Argv: []string{"claude", "--resume", sid, "-n", "a b"}, Script: "tell application \"Terminal\"\nend tell", Note: "2 background job(s) were lost"}
	if err := Run(a); err != nil {
		t.Fatalf("Run: %v", err)
	}
	out := buf.String()
	first := strings.SplitN(out, "\n", 2)[0]
	want := "DRY RUN: resume cwd=/tmp/p q argv=claude --resume " + sid + " -n 'a b'"
	if first != want {
		t.Errorf("first line = %q, want %q", first, want)
	}
	if !strings.Contains(out, "DRY RUN: script:\ntell application") {
		t.Errorf("script not echoed: %q", out)
	}
	if !strings.Contains(out, "DRY RUN: note: 2 background") {
		t.Errorf("note not echoed: %q", out)
	}

	buf.Reset()
	if err := Run(&Action{Kind: KindFocus, Script: "x"}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(buf.String(), "DRY RUN: focus cwd= argv=\n") {
		t.Errorf("focus dry run = %q", buf.String())
	}
}

func TestRunPrintAndErrors(t *testing.T) {
	t.Setenv("RECALL_DRY_RUN", "")
	var buf bytes.Buffer
	old := Stdout
	Stdout = &buf
	t.Cleanup(func() { Stdout = old })

	if err := Run(&Action{Kind: KindPrint, Description: "running in Cursor", Note: "fork: claude --resume x"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "running in Cursor\nfork: claude --resume x\n") {
		t.Errorf("print output = %q", buf.String())
	}
	if err := Run(nil); err == nil {
		t.Errorf("nil action must fail")
	}
	if err := Run(&Action{Kind: "bogus"}); err == nil {
		t.Errorf("unknown kind must fail")
	}
	if err := Run(&Action{Kind: KindFocus}); err == nil {
		t.Errorf("focus without script must fail")
	}
	if err := Run(&Action{Kind: KindResume}); err == nil {
		t.Errorf("empty argv must fail")
	}
	// A binary that cannot be found fails before any exec or chdir.
	t.Setenv("PATH", t.TempDir())
	if err := Run(&Action{Kind: KindResume, Cwd: t.TempDir(), Argv: []string{"claude", "--resume", sid}}); err == nil {
		t.Errorf("missing claude on PATH must fail")
	}
}

func TestNormalizeHost(t *testing.T) {
	cases := map[string]string{
		"Terminal":                  "Terminal",
		"Terminal.app":              "Terminal",
		"Apple_Terminal":            "Terminal",
		"iTerm2":                    "iTerm2",
		"iTerm.app":                 "iTerm2",
		"Cursor":                    "Cursor",
		"Code":                      "Code",
		"Visual Studio Code":        "Code",
		"":                          "",
		"/Applications/WezTerm.app": "WezTerm.app",
	}
	for in, want := range cases {
		if got := normalizeHost(in); got != want {
			t.Errorf("normalizeHost(%q) = %q, want %q", in, got, want)
		}
	}
}
