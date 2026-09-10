package mux

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

const sid = "0123456789abcdef-1111-2222-3333-444444444444"

// noTmux makes lookPath fail for the duration of the test.
func noTmux(t *testing.T) {
	t.Helper()
	orig := lookPath
	t.Cleanup(func() { lookPath = orig })
	lookPath = func(string) (string, error) { return "", errors.New("executable file not found in $PATH") }
}

// fakeTmux installs a lookPath that succeeds and a runTmux that records
// argv and answers with the given function.
func fakeTmux(t *testing.T, answer func(argv []string) (string, error)) *[][]string {
	t.Helper()
	origLook, origRun := lookPath, runTmux
	t.Cleanup(func() { lookPath, runTmux = origLook, origRun })
	lookPath = func(name string) (string, error) { return "/usr/local/bin/" + name, nil }
	calls := &[][]string{}
	runTmux = func(argv []string) (string, error) {
		*calls = append(*calls, argv)
		return answer(argv)
	}
	return calls
}

func TestSessionName(t *testing.T) {
	tm := NewTmux()
	if tm.Socket != "recall" {
		t.Errorf("Socket = %q", tm.Socket)
	}
	if got := tm.SessionName(sid); got != "rc-01234567" {
		t.Errorf("SessionName = %q", got)
	}
	if got := tm.SessionName("abc"); got != "rc-abc" {
		t.Errorf("SessionName short = %q", got)
	}
}

func TestDefaultConf(t *testing.T) {
	c := DefaultConf()
	for _, want := range []string{
		`default-terminal "tmux-256color"`,
		`terminal-overrides ",*:RGB"`,
		"set -s escape-time 0",
		"set -g mouse off",
		"set -g status off",
		"set -g history-limit 50000",
		"set -g remain-on-exit on",
		`bind -n 'C-\' detach-client`,
	} {
		if !strings.Contains(c, want) {
			t.Errorf("DefaultConf missing %q", want)
		}
	}
	if strings.ContainsRune(c, 0x2014) {
		t.Error("DefaultConf contains an em dash")
	}
}

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"":                "''",
		"claude":          "claude",
		"/Users/me/proj":  "/Users/me/proj",
		"--session-id":    "--session-id",
		"my name":         "'my name'",
		"it's":            `'it'\''s'`,
		"a$b":             "'a$b'",
		"Work @act.3 /x":  "'Work @act.3 /x'",
		"semi;colon":      "'semi;colon'",
		"back`tick":       "'back`tick'",
		"dollar$(uname)":  "'dollar$(uname)'",
		"quote\"double":   `'quote"double'`,
		"new\nline":       "'new\nline'",
		"tab\there":       "'tab\there'",
		"path/with-dash_": "path/with-dash_",
	}
	for in, want := range cases {
		if got := ShellQuote(in); got != want {
			t.Errorf("ShellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClaudeShellCommand(t *testing.T) {
	got := ClaudeShellCommand(sid, "my session", []string{"--permission-mode", "plan", "--add-dir", "/tmp/a b"})
	want := "env RECALL_SID=" + sid + " RECALL_BYPASS=1 claude --session-id " + sid + " -n 'my session' --permission-mode plan --add-dir '/tmp/a b'"
	if got != want {
		t.Errorf("ClaudeShellCommand =\n  %s\nwant\n  %s", got, want)
	}
	if got := ClaudeShellCommand(sid, "", nil); strings.Contains(got, " -n ") {
		t.Errorf("empty name should omit -n: %s", got)
	}
}

func TestNewKeptCommand(t *testing.T) {
	tm := NewTmux()
	got := tm.NewKeptCommand(sid, "fix-bug", "/Users/me/proj", []string{"--model", "opus"})
	want := []string{
		"tmux", "-L", "recall",
		"new-session", "-d", "-s", "rc-01234567",
		"-c", "/Users/me/proj",
		"-x", "200", "-y", "50",
		"env RECALL_SID=" + sid + " RECALL_BYPASS=1 claude --session-id " + sid + " -n fix-bug --model opus",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("NewKeptCommand =\n  %q\nwant\n  %q", got, want)
	}

	// Custom socket and conf file, no cwd, no name.
	tm2 := &Tmux{Socket: "rc-test", Conf: "/tmp/recall/tmux.conf"}
	got = tm2.NewKeptCommand(sid, "", "", nil)
	if got[0] != "tmux" || got[1] != "-L" || got[2] != "rc-test" || got[3] != "-f" || got[4] != "/tmp/recall/tmux.conf" {
		t.Errorf("prefix = %q", got[:5])
	}
	for _, a := range got {
		if a == "-c" || a == "-n" {
			t.Errorf("unexpected %q in %q", a, got)
		}
	}
	last := got[len(got)-1]
	if !strings.HasPrefix(last, "env RECALL_SID="+sid+" RECALL_BYPASS=1 claude --session-id "+sid) {
		t.Errorf("shell command = %q", last)
	}
	if strings.Contains(last, "-n") {
		t.Errorf("empty name should omit -n: %q", last)
	}

	// Zero-value Tmux falls back to the default socket.
	var zero Tmux
	if got := zero.AttachCommand(sid); got[2] != "recall" {
		t.Errorf("zero socket = %q", got)
	}
}

func TestAttachAndKillCommands(t *testing.T) {
	tm := NewTmux()
	if got, want := tm.AttachCommand(sid), []string{"tmux", "-L", "recall", "attach-session", "-t", "=rc-01234567"}; !reflect.DeepEqual(got, want) {
		t.Errorf("AttachCommand = %q, want %q", got, want)
	}
	if got, want := tm.KillCommand(sid), []string{"tmux", "-L", "recall", "kill-session", "-t", "=rc-01234567"}; !reflect.DeepEqual(got, want) {
		t.Errorf("KillCommand = %q, want %q", got, want)
	}
}

func TestParseVersion(t *testing.T) {
	cases := []struct {
		in           string
		major, minor int
		ok           bool
		accepted     bool
	}{
		{"tmux 3.4", 3, 4, true, true},
		{"tmux 3.3a", 3, 3, true, true},
		{"tmux 3.2", 3, 2, true, true},
		{"tmux 3.1c", 3, 1, true, false},
		{"tmux 2.9a", 2, 9, true, false},
		{"tmux next-3.5", 3, 5, true, true},
		{"tmux 4.0", 4, 0, true, true},
		{"tmux master", 0, 0, false, false},
	}
	for _, c := range cases {
		major, minor, ok := parseVersion(c.in)
		if major != c.major || minor != c.minor || ok != c.ok {
			t.Errorf("parseVersion(%q) = %d %d %v", c.in, major, minor, ok)
			continue
		}
		if ok && versionOK(major, minor) != c.accepted {
			t.Errorf("versionOK(%q) = %v, want %v", c.in, !c.accepted, c.accepted)
		}
	}
}

func TestAvailable(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		noTmux(t)
		ok, v := NewTmux().Available()
		if ok || v != "" {
			t.Errorf("Available = %v %q", ok, v)
		}
	})
	t.Run("new enough", func(t *testing.T) {
		calls := fakeTmux(t, func([]string) (string, error) { return "tmux 3.4\n", nil })
		ok, v := NewTmux().Available()
		if !ok || v != "3.4" {
			t.Errorf("Available = %v %q", ok, v)
		}
		if len(*calls) != 1 || !reflect.DeepEqual((*calls)[0], []string{"tmux", "-V"}) {
			t.Errorf("calls = %q", *calls)
		}
	})
	t.Run("too old", func(t *testing.T) {
		fakeTmux(t, func([]string) (string, error) { return "tmux 3.1c\n", nil })
		ok, v := NewTmux().Available()
		if ok || v != "3.1c" {
			t.Errorf("Available = %v %q", ok, v)
		}
	})
	t.Run("broken binary", func(t *testing.T) {
		fakeTmux(t, func([]string) (string, error) { return "", errors.New("boom") })
		if ok, _ := NewTmux().Available(); ok {
			t.Error("failing tmux -V reported available")
		}
	})
}

func TestListWithoutTmux(t *testing.T) {
	noTmux(t)
	list, err := NewTmux().List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if list == nil || len(list) != 0 {
		t.Errorf("List = %#v, want empty non-nil slice", list)
	}
}

func TestListNoServer(t *testing.T) {
	fakeTmux(t, func([]string) (string, error) {
		return "", errors.New("tmux -L recall list-panes: no server running on /private/tmp/tmux-501/recall")
	})
	list, err := NewTmux().List()
	if err != nil || len(list) != 0 {
		t.Errorf("List = %v %v", list, err)
	}
}

func TestListParsesPanes(t *testing.T) {
	calls := fakeTmux(t, func([]string) (string, error) {
		return "rc-01234567\t1\t4242\nrc-89abcdef\t0\t4343\n\nbroken line\n", nil
	})
	tm := NewTmux()
	list, err := tm.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("List = %+v", list)
	}
	if list[0].SessionName != "rc-01234567" || !list[0].Attached || list[0].PanePID != 4242 || list[0].Socket != "recall" {
		t.Errorf("first = %+v", list[0])
	}
	if list[1].SessionName != "rc-89abcdef" || list[1].Attached || list[1].PanePID != 4343 {
		t.Errorf("second = %+v", list[1])
	}
	want := []string{"tmux", "-L", "recall", "list-panes", "-a", "-F", listFormat}
	if len(*calls) != 1 || !reflect.DeepEqual((*calls)[0], want) {
		t.Errorf("argv = %q, want %q", *calls, want)
	}

	found, err := tm.Find(sid)
	if err != nil || found == nil || found.PanePID != 4242 {
		t.Errorf("Find = %+v %v", found, err)
	}
	missing, err := tm.Find("ffffffff-0000")
	if err != nil || missing != nil {
		t.Errorf("Find missing = %+v %v", missing, err)
	}
}

func TestKill(t *testing.T) {
	t.Run("missing tmux", func(t *testing.T) {
		noTmux(t)
		if err := NewTmux().Kill(sid); err == nil {
			t.Error("Kill without tmux should fail")
		}
	})
	t.Run("ok", func(t *testing.T) {
		calls := fakeTmux(t, func([]string) (string, error) { return "", nil })
		if err := NewTmux().Kill(sid); err != nil {
			t.Fatal(err)
		}
		want := []string{"tmux", "-L", "recall", "kill-session", "-t", "=rc-01234567"}
		if len(*calls) != 1 || !reflect.DeepEqual((*calls)[0], want) {
			t.Errorf("argv = %q", *calls)
		}
	})
	t.Run("already gone", func(t *testing.T) {
		fakeTmux(t, func([]string) (string, error) { return "", errors.New("can't find session: =rc-01234567") })
		if err := NewTmux().Kill(sid); err != nil {
			t.Errorf("missing session should not be an error: %v", err)
		}
	})
	t.Run("other error", func(t *testing.T) {
		fakeTmux(t, func([]string) (string, error) { return "", errors.New("permission denied") })
		if err := NewTmux().Kill(sid); err == nil {
			t.Error("unexpected tmux error should surface")
		}
	})
}
