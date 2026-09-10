package launch

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MohammedAl-Alimi/recall/internal/model"
)

// TestMain keeps the auto-detection of a cmux host out of every test that
// does not opt in: a suite run from inside cmux must plan exactly like one
// run from Terminal.app.
func TestMain(m *testing.M) {
	os.Unsetenv("CMUX_WORKSPACE_ID")
	os.Exit(m.Run())
}

// fakeCmux installs an executable named cmux in a temp dir, puts only that
// dir on PATH and points the bundle lookup at a missing file. It returns
// the fake's path. Nothing in the launch package ever runs it.
func fakeCmux(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "cmux")
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	noBundle(t)
	return p
}

// noBundle points the bundle lookup at a path that does not exist.
func noBundle(t *testing.T) {
	t.Helper()
	old := cmuxBundleCLI
	cmuxBundleCLI = filepath.Join(t.TempDir(), "missing", "cmux")
	t.Cleanup(func() { cmuxBundleCLI = old })
}

// stubCmuxExec replaces cmuxExec with fn and restores it after the test.
func stubCmuxExec(t *testing.T, fn func(ctx context.Context, argv ...string) ([]byte, error)) {
	t.Helper()
	old := cmuxExec
	cmuxExec = fn
	t.Cleanup(func() { cmuxExec = old })
}

func TestCmuxOpenArgv(t *testing.T) {
	cases := []struct {
		name  string
		path  string
		title string
		cwd   string
		cmd   string
		want  []string
	}{
		{
			name: "all fields", path: "/opt/bin/cmux", title: "fix tests", cwd: "/Users/me/proj",
			cmd:  "cd /Users/me/proj && claude --resume abc",
			want: []string{"/opt/bin/cmux", "new-workspace", "--name", "fix tests", "--cwd", "/Users/me/proj", "--command", "cd /Users/me/proj && claude --resume abc", "--focus", "true"},
		},
		{
			name: "no title", path: "cmux", cwd: "/Users/me", cmd: "claude",
			want: []string{"cmux", "new-workspace", "--cwd", "/Users/me", "--command", "claude", "--focus", "true"},
		},
		{
			name: "no cwd", path: "cmux", title: "t", cmd: "claude",
			want: []string{"cmux", "new-workspace", "--name", "t", "--command", "claude", "--focus", "true"},
		},
		{
			name: "command kept verbatim", path: "cmux", cmd: "cd '/Users/me/my dir' && claude -n 'a b'",
			want: []string{"cmux", "new-workspace", "--command", "cd '/Users/me/my dir' && claude -n 'a b'", "--focus", "true"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CmuxOpenArgv(tc.path, tc.title, tc.cwd, tc.cmd)
			if strings.Join(got, "\x00") != strings.Join(tc.want, "\x00") {
				t.Errorf("got  %q\nwant %q", got, tc.want)
			}
		})
	}
}

func TestCmuxAvailable(t *testing.T) {
	t.Run("on PATH", func(t *testing.T) {
		want := fakeCmux(t)
		got, ok := CmuxAvailable()
		if !ok || got != want {
			t.Errorf("got %q %v, want %q true", got, ok, want)
		}
	})
	t.Run("bundle only", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		bundle := filepath.Join(t.TempDir(), "cmux")
		if err := os.WriteFile(bundle, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		old := cmuxBundleCLI
		cmuxBundleCLI = bundle
		t.Cleanup(func() { cmuxBundleCLI = old })
		got, ok := CmuxAvailable()
		if !ok || got != bundle {
			t.Errorf("got %q %v, want %q true", got, ok, bundle)
		}
	})
	t.Run("bundle not executable", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		bundle := filepath.Join(t.TempDir(), "cmux")
		if err := os.WriteFile(bundle, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		old := cmuxBundleCLI
		cmuxBundleCLI = bundle
		t.Cleanup(func() { cmuxBundleCLI = old })
		if got, ok := CmuxAvailable(); ok {
			t.Errorf("non-executable bundle file must not count, got %q", got)
		}
	})
	t.Run("nowhere", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		noBundle(t)
		if got, ok := CmuxAvailable(); ok {
			t.Errorf("expected not found, got %q", got)
		}
	})
}

func TestCmuxEnsureRunning(t *testing.T) {
	old := cmuxPollInterval
	oldT := cmuxStartTimeout
	cmuxPollInterval = time.Millisecond
	cmuxStartTimeout = 30 * time.Millisecond
	t.Cleanup(func() { cmuxPollInterval = old; cmuxStartTimeout = oldT })

	t.Run("dry run does nothing", func(t *testing.T) {
		t.Setenv("RECALL_DRY_RUN", "1")
		t.Setenv("PATH", t.TempDir())
		noBundle(t)
		called := false
		stubCmuxExec(t, func(ctx context.Context, argv ...string) ([]byte, error) { called = true; return nil, nil })
		if err := CmuxEnsureRunning(context.Background()); err != nil {
			t.Fatal(err)
		}
		if called {
			t.Error("dry run must not run anything")
		}
	})
	t.Run("cli missing", func(t *testing.T) {
		t.Setenv("RECALL_DRY_RUN", "")
		t.Setenv("PATH", t.TempDir())
		noBundle(t)
		stubCmuxExec(t, func(ctx context.Context, argv ...string) ([]byte, error) { t.Error("must not exec"); return nil, nil })
		if err := CmuxEnsureRunning(context.Background()); err == nil || !strings.Contains(err.Error(), "not found") {
			t.Errorf("want not found error, got %v", err)
		}
	})
	t.Run("already running", func(t *testing.T) {
		t.Setenv("RECALL_DRY_RUN", "")
		cli := fakeCmux(t)
		var calls [][]string
		stubCmuxExec(t, func(ctx context.Context, argv ...string) ([]byte, error) {
			calls = append(calls, argv)
			return []byte("pong"), nil
		})
		if err := CmuxEnsureRunning(context.Background()); err != nil {
			t.Fatal(err)
		}
		if len(calls) != 1 || calls[0][0] != cli || calls[0][1] != "ping" {
			t.Errorf("expected a single ping via %s, got %q", cli, calls)
		}
	})
	t.Run("launches and polls", func(t *testing.T) {
		t.Setenv("RECALL_DRY_RUN", "")
		fakeCmux(t)
		pings := 0
		opened := false
		stubCmuxExec(t, func(ctx context.Context, argv ...string) ([]byte, error) {
			switch argv[0] {
			case "open":
				if strings.Join(argv, " ") != "open -a cmux" {
					t.Errorf("unexpected open call %q", argv)
				}
				opened = true
				return nil, nil
			}
			pings++
			if opened && pings >= 3 {
				return []byte("pong"), nil
			}
			return []byte("Error: Socket not found"), errors.New("exit status 1")
		})
		if err := CmuxEnsureRunning(context.Background()); err != nil {
			t.Fatal(err)
		}
		if !opened || pings < 3 {
			t.Errorf("opened=%v pings=%d", opened, pings)
		}
	})
	t.Run("gives up after timeout", func(t *testing.T) {
		t.Setenv("RECALL_DRY_RUN", "")
		fakeCmux(t)
		stubCmuxExec(t, func(ctx context.Context, argv ...string) ([]byte, error) {
			if argv[0] == "open" {
				return nil, nil
			}
			return []byte("Error: Socket not found"), errors.New("exit status 1")
		})
		err := CmuxEnsureRunning(context.Background())
		if err == nil || !strings.Contains(err.Error(), "Socket not found") {
			t.Errorf("want timeout error carrying the ping message, got %v", err)
		}
	})
	t.Run("open fails", func(t *testing.T) {
		t.Setenv("RECALL_DRY_RUN", "")
		fakeCmux(t)
		stubCmuxExec(t, func(ctx context.Context, argv ...string) ([]byte, error) {
			if argv[0] == "open" {
				return []byte("Unable to find application named 'cmux'"), errors.New("exit status 1")
			}
			return nil, errors.New("exit status 1")
		})
		err := CmuxEnsureRunning(context.Background())
		if err == nil || !strings.Contains(err.Error(), "Unable to find application") {
			t.Errorf("want open error, got %v", err)
		}
	})
	t.Run("context cancelled", func(t *testing.T) {
		t.Setenv("RECALL_DRY_RUN", "")
		fakeCmux(t)
		stubCmuxExec(t, func(ctx context.Context, argv ...string) ([]byte, error) {
			if argv[0] == "open" {
				return nil, nil
			}
			return nil, errors.New("exit status 1")
		})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := CmuxEnsureRunning(ctx); !errors.Is(err, context.Canceled) {
			t.Errorf("want context.Canceled, got %v", err)
		}
	})
}

func TestPlanTerminalChoice(t *testing.T) {
	cli := fakeCmux(t)
	dir := filepath.Join(t.TempDir(), "my proj")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	sess := &model.Session{ID: sid, Cwd: dir, WorkCwd: dir, Title: "fix tests", BgJobsLost: 2}
	resume := "cd " + ShellQuote(dir) + " && claude --resume " + sid

	cases := []struct {
		name    string
		sess    *model.Session
		opts    Options
		wantErr string
		check   func(t *testing.T, a *Action)
	}{
		{
			name: "cmux resume",
			sess: sess,
			opts: Options{Terminal: "cmux", TermProgram: "Apple_Terminal"},
			check: func(t *testing.T, a *Action) {
				if a.Kind != KindResume || a.Terminal != TerminalCmux || a.Script != "" {
					t.Fatalf("got kind=%q terminal=%q script=%q", a.Kind, a.Terminal, a.Script)
				}
				want := []string{cli, "new-workspace", "--name", "fix tests", "--cwd", dir, "--command", resume, "--focus", "true"}
				if strings.Join(a.Argv, "\x00") != strings.Join(want, "\x00") {
					t.Errorf("argv\n got %q\nwant %q", a.Argv, want)
				}
				if !strings.Contains(resume, "cd '"+dir+"' && claude --resume "+sid) {
					t.Errorf("command must cd into the quoted cwd, got %q", resume)
				}
				if !strings.Contains(a.Description, "open in cmux workspace") {
					t.Errorf("description %q", a.Description)
				}
				if !strings.Contains(a.Note, "2 background job(s) were lost") {
					t.Errorf("loss note must survive, got %q", a.Note)
				}
				if a.SID != sid || a.Cwd != dir {
					t.Errorf("sid=%q cwd=%q", a.SID, a.Cwd)
				}
			},
		},
		{
			name: "cmux is case insensitive and implies new tab",
			sess: sess,
			opts: Options{Terminal: "CMUX"},
			check: func(t *testing.T, a *Action) {
				if a.Terminal != TerminalCmux || a.Argv[1] != "new-workspace" {
					t.Errorf("got %+v", a)
				}
			},
		},
		{
			name: "in place wins over cmux",
			sess: sess,
			opts: Options{Terminal: "cmux", InPlace: true},
			check: func(t *testing.T, a *Action) {
				if a.Terminal != "" || a.Script != "" || a.Argv[0] != "claude" {
					t.Errorf("got %+v", a)
				}
			},
		},
		{
			name: "cmux with keep wraps tmux",
			sess: sess,
			opts: Options{Terminal: "cmux", Keep: true},
			check: func(t *testing.T, a *Action) {
				if a.Kind != KindAttach || a.Terminal != TerminalCmux {
					t.Fatalf("got %+v", a)
				}
				cmd := a.Argv[len(a.Argv)-3]
				if !strings.HasPrefix(cmd, "cd "+ShellQuote(dir)+" && tmux -L recall new-session -A -s rc-") {
					t.Errorf("command %q", cmd)
				}
			},
		},
		{
			name: "cmux fork",
			sess: sess,
			opts: Options{Terminal: "cmux", Fork: true},
			check: func(t *testing.T, a *Action) {
				cmd := a.Argv[len(a.Argv)-3]
				if !strings.Contains(cmd, "claude --resume "+sid+" --fork-session") {
					t.Errorf("command %q", cmd)
				}
			},
		},
		{
			name: "label names the workspace",
			sess: &model.Session{ID: sid, Cwd: dir, Title: "fix tests", Label: "api"},
			opts: Options{Terminal: "cmux"},
			check: func(t *testing.T, a *Action) {
				if a.Argv[3] != "api" {
					t.Errorf("name %q", a.Argv[3])
				}
			},
		},
		{
			name: "short id names an untitled session",
			sess: &model.Session{ID: sid, Cwd: dir},
			opts: Options{Terminal: "cmux"},
			check: func(t *testing.T, a *Action) {
				if a.Argv[3] != "claude "+model.ShortID(sid) {
					t.Errorf("name %q", a.Argv[3])
				}
			},
		},
		{
			name: "terminal forces Terminal.app over iTerm env",
			sess: sess,
			opts: Options{Terminal: "terminal", TermProgram: "iTerm.app"},
			check: func(t *testing.T, a *Action) {
				if a.Terminal != "" || !strings.Contains(a.Script, `tell application "Terminal"`) {
					t.Errorf("got terminal=%q script:\n%s", a.Terminal, a.Script)
				}
				if a.Argv[0] != "claude" {
					t.Errorf("argv must stay the claude command, got %q", a.Argv)
				}
			},
		},
		{
			name: "iterm forces iTerm2 over Terminal env",
			sess: sess,
			opts: Options{Terminal: "iterm", TermProgram: "Apple_Terminal"},
			check: func(t *testing.T, a *Action) {
				if !strings.Contains(a.Script, `tell application "iTerm2"`) {
					t.Errorf("script:\n%s", a.Script)
				}
			},
		},
		{
			name: "terminal forces Terminal.app over cmux env",
			sess: sess,
			opts: Options{Terminal: "terminal", TermProgram: "cmux"},
			check: func(t *testing.T, a *Action) {
				if a.Terminal != "" || !strings.Contains(a.Script, `tell application "Terminal"`) {
					t.Errorf("got terminal=%q script:\n%s", a.Terminal, a.Script)
				}
			},
		},
		{
			name: "auto picks cmux from TermProgram with new tab",
			sess: sess,
			opts: Options{NewTab: true, TermProgram: "cmux"},
			check: func(t *testing.T, a *Action) {
				if a.Terminal != TerminalCmux {
					t.Errorf("got %+v", a)
				}
			},
		},
		{
			name: "auto without new tab resumes in place",
			sess: sess,
			opts: Options{TermProgram: "cmux"},
			check: func(t *testing.T, a *Action) {
				if a.Terminal != "" || a.Script != "" || a.Argv[0] != "claude" {
					t.Errorf("got %+v", a)
				}
			},
		},
		{
			name:    "unknown terminal",
			sess:    sess,
			opts:    Options{Terminal: "wezterm"},
			wantErr: `unknown terminal "wezterm"`,
		},
		{
			name: "new session in cmux",
			sess: nil,
			opts: Options{Terminal: "cmux", Cwd: dir, Name: "spike", PermMode: "plan"},
			check: func(t *testing.T, a *Action) {
				if a.Kind != KindNew || a.Terminal != TerminalCmux {
					t.Fatalf("got %+v", a)
				}
				if a.Argv[3] != "spike" {
					t.Errorf("name %q", a.Argv[3])
				}
				cmd := a.Argv[len(a.Argv)-3]
				if cmd != "cd "+ShellQuote(dir)+" && claude -n spike --permission-mode plan" {
					t.Errorf("command %q", cmd)
				}
			},
		},
		{
			name: "new session named after directory",
			sess: nil,
			opts: Options{Terminal: "cmux", Cwd: dir},
			check: func(t *testing.T, a *Action) {
				if a.Argv[3] != "claude my proj" {
					t.Errorf("name %q", a.Argv[3])
				}
			},
		},
		{
			name:    "new session unknown terminal",
			sess:    nil,
			opts:    Options{Terminal: "kitty", Cwd: dir},
			wantErr: `unknown terminal "kitty"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, err := Plan(tc.sess, tc.opts)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("want error %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			tc.check(t, a)
		})
	}
}

func TestPlanCmuxLiveAttach(t *testing.T) {
	fakeCmux(t)
	sess := closedSession(t)
	sess.Live = &model.Live{Alive: true, PID: 42, Mux: &model.MuxInfo{SessionName: "rc-abc", Socket: "recall"}}
	a, err := Plan(sess, Options{Terminal: "cmux"})
	if err != nil {
		t.Fatal(err)
	}
	if a.Kind != KindAttach || a.Terminal != TerminalCmux {
		t.Fatalf("got %+v", a)
	}
	cmd := a.Argv[len(a.Argv)-3]
	if cmd != "tmux -L recall attach-session -t rc-abc" {
		t.Errorf("command %q", cmd)
	}
	// Without --terminal or --new-tab the attach stays in place.
	a, err = Plan(sess, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if a.Terminal != "" || a.Argv[0] != "tmux" {
		t.Errorf("got %+v", a)
	}
}

func TestPlanCmuxAutoDetectEnv(t *testing.T) {
	sess := closedSession(t)
	t.Setenv("CMUX_WORKSPACE_ID", "ws-1")
	t.Setenv("TERM_PROGRAM", "ghostty")

	t.Run("cli present", func(t *testing.T) {
		cli := fakeCmux(t)
		a, err := Plan(sess, Options{NewTab: true})
		if err != nil {
			t.Fatal(err)
		}
		if a.Terminal != TerminalCmux || a.Argv[0] != cli {
			t.Errorf("inside cmux with a CLI, --new-tab should open a workspace, got %+v", a)
		}
	})
	t.Run("cli missing falls back to Terminal.app", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		noBundle(t)
		a, err := Plan(sess, Options{NewTab: true})
		if err != nil {
			t.Fatal(err)
		}
		if a.Terminal != "" || !strings.Contains(a.Script, `tell application "Terminal"`) {
			t.Errorf("got terminal=%q script:\n%s", a.Terminal, a.Script)
		}
	})
	t.Run("explicit cmux without cli keeps the cmux shape", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		noBundle(t)
		a, err := Plan(sess, Options{Terminal: "cmux"})
		if err != nil {
			t.Fatal(err)
		}
		if a.Terminal != TerminalCmux || a.Argv[0] != "cmux" || a.Script != "" {
			t.Errorf("got %+v", a)
		}
	})
	t.Run("in place ignores the host", func(t *testing.T) {
		fakeCmux(t)
		a, err := Plan(sess, Options{})
		if err != nil {
			t.Fatal(err)
		}
		if a.Terminal != "" || a.Argv[0] != "claude" {
			t.Errorf("got %+v", a)
		}
	})
}

func TestRunCmuxDryRun(t *testing.T) {
	t.Setenv("RECALL_DRY_RUN", "1")
	cli := fakeCmux(t)
	stubCmuxExec(t, func(ctx context.Context, argv ...string) ([]byte, error) { t.Error("must not exec"); return nil, nil })
	sess := closedSession(t)
	a, err := Plan(sess, Options{Terminal: "cmux"})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	old := Stdout
	Stdout = &out
	t.Cleanup(func() { Stdout = old })
	if err := Run(a); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "DRY RUN: resume") || !strings.Contains(got, cli+" new-workspace") || !strings.Contains(got, "--command") {
		t.Errorf("dry run output:\n%s", got)
	}
}

func TestRunCmuxCLIMissing(t *testing.T) {
	t.Setenv("RECALL_DRY_RUN", "")
	t.Setenv("PATH", t.TempDir())
	noBundle(t)
	stubCmuxExec(t, func(ctx context.Context, argv ...string) ([]byte, error) { t.Error("must not exec"); return nil, nil })
	var errOut bytes.Buffer
	old := Stderr
	Stderr = &errOut
	t.Cleanup(func() { Stderr = old })
	a := &Action{Kind: KindResume, Terminal: TerminalCmux, Argv: []string{"cmux", "new-workspace"}, Note: "1 background job(s) were lost"}
	err := Run(a)
	if err == nil || !strings.Contains(err.Error(), "cmux CLI not found") {
		t.Errorf("want CLI missing error, got %v", err)
	}
	if !strings.Contains(errOut.String(), "recall: 1 background job(s) were lost") {
		t.Errorf("note must be printed before the cmux attempt, got %q", errOut.String())
	}
}

func TestCmuxTitle(t *testing.T) {
	long := strings.Repeat("x", 60)
	cases := []struct {
		sid, label, title, cwd, want string
	}{
		{sid, "api", "fix tests", "/Users/me/p", "api"},
		{sid, "", "fix tests", "/Users/me/p", "fix tests"},
		{sid, "", "", "/Users/me/p", "claude " + model.ShortID(sid)},
		{"", "", "", "/Users/me/p", "claude p"},
		{"", "spike", "", "/Users/me/p", "spike"},
		{sid, "", long, "", strings.Repeat("x", 45) + "..."},
	}
	for _, tc := range cases {
		if got := cmuxTitle(tc.sid, tc.label, tc.title, tc.cwd); got != tc.want {
			t.Errorf("cmuxTitle(%q,%q,%q,%q) = %q, want %q", tc.sid, tc.label, tc.title, tc.cwd, got, tc.want)
		}
	}
}

func TestNormalizeHostCmux(t *testing.T) {
	for _, in := range []string{"cmux", "cmux.app", "CMUX"} {
		if got := normalizeHost(in); got != "cmux" {
			t.Errorf("normalizeHost(%q) = %q", in, got)
		}
	}
}
