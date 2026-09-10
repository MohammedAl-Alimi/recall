package doctor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MohammedAl-Alimi/recall/internal/hook"
	"github.com/MohammedAl-Alimi/recall/internal/model"
	"github.com/MohammedAl-Alimi/recall/internal/shell"
)

// sampleHelp is synthesized to match the layout of 'claude --help' in the
// 2.1.x line. It is not a copy of real output.
const sampleHelp = `Usage: claude [options] [command] [prompt]

Claude Code - starts an interactive session by default

Options:
  --bg, --background                    Start the session in the background and
                                        print its id. Use ` + "`claude attach`" + ` to
                                        take; ` + "`claude agents`" + ` lists them. With
                                        --resume <session-id>, continues that
  --fork-session                        When resuming, create a new session ID
                                        (use with --resume or --continue)
  -n, --name <name>                     Set a display name for this session
  -r, --resume [value]                  Resume a conversation by session ID, or
                                        open interactive picker
  --session-id <uuid>                   Use a specific session ID for the
                                        conversation
  -v, --version                         Output the version number

Commands:
  agents [options]                      Manage background agents
  attach <id>                           Open a background session in this
                                        terminal
  doctor                                Check the health of your installation
  help [command]                        display help for command
`

const oldHelp = `Usage: claude [options] [command] [prompt]

Options:
  -r, --resume [sessionId]              Resume a conversation
  -v, --version                         Output the version number

Commands:
  doctor                                Check the health
`

func fakeClaude(t *testing.T, version, help string, fail bool) {
	t.Helper()
	old := runClaude
	runClaude = func(args ...string) (string, error) {
		if fail {
			return "", errors.New("exec: claude: not found")
		}
		switch args[0] {
		case "--version":
			return version + " (Claude Code)\n", nil
		case "--help":
			return help, nil
		}
		return "", errors.New("unexpected args")
	}
	t.Cleanup(func() { runClaude = old })
}

func TestDetectFeatures(t *testing.T) {
	fakeClaude(t, "2.1.267", sampleHelp, false)
	f, err := DetectFeatures()
	if err != nil {
		t.Fatal(err)
	}
	want := Features{Version: "2.1.267", Resume: true, ForkSession: true, SessionID: true, Name: true, BG: true, AgentsJSON: true, Attach: true}
	if f != want {
		t.Fatalf("features = %+v, want %+v", f, want)
	}
}

func TestDetectFeaturesOld(t *testing.T) {
	fakeClaude(t, "1.0.80", oldHelp, false)
	f, err := DetectFeatures()
	if err != nil {
		t.Fatal(err)
	}
	if f.Version != "1.0.80" || !f.Resume || f.ForkSession || f.SessionID || f.Name || f.BG || f.AgentsJSON || f.Attach {
		t.Fatalf("features = %+v", f)
	}
}

func TestDetectFeaturesMissing(t *testing.T) {
	fakeClaude(t, "", "", true)
	if _, err := DetectFeatures(); err == nil {
		t.Fatal("expected error")
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"2.1.267", "2.1.223", 1},
		{"2.1.223", "2.1.223", 0},
		{"2.1.100", "2.1.223", -1},
		{"2.0.999", "2.1.0", -1},
		{"3.0.0", "2.9.9", 1},
		{"2.1.267 (Claude Code)", "2.1.223", 1},
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("Compare(%s,%s) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func byName(checks []Check) map[string]Check {
	m := map[string]Check{}
	for _, c := range checks {
		m[c.Name] = c
	}
	return m
}

func TestRunEmptyEnvironment(t *testing.T) {
	fakeClaude(t, "2.1.267", sampleHelp, false)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/zsh")
	t.Setenv("ZDOTDIR", "")
	t.Setenv("TERM_PROGRAM", "")
	oldLook := lookPath
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	t.Cleanup(func() { lookPath = oldLook })

	p := model.PathsFrom(filepath.Join(home, "claude"), filepath.Join(home, "recall"))
	checks, err := Run(p)
	if err != nil {
		t.Fatal(err)
	}
	m := byName(checks)
	if m["claude"].Status != "ok" || !strings.Contains(m["claude"].Detail, "2.1.267") {
		t.Fatalf("claude: %+v", m["claude"])
	}
	if m["projects"].Status != "fail" {
		t.Fatalf("projects: %+v", m["projects"])
	}
	if m["registry"].Status != "warn" {
		t.Fatalf("registry: %+v", m["registry"])
	}
	if m["retention"].Status != "warn" || !strings.Contains(m["retention"].Detail, "30 days") {
		t.Fatalf("retention: %+v", m["retention"])
	}
	if m["tmux"].Status != "warn" {
		t.Fatalf("tmux: %+v", m["tmux"])
	}
	if m["terminal"].Status != "warn" {
		t.Fatalf("terminal: %+v", m["terminal"])
	}
	if m["recall-dir"].Status != "warn" {
		t.Fatalf("recall-dir: %+v", m["recall-dir"])
	}
	if m["hooks"].Status != "warn" {
		t.Fatalf("hooks: %+v", m["hooks"])
	}
	if m["widget"].Status != "warn" {
		t.Fatalf("widget: %+v", m["widget"])
	}
	if m["network"].Status != "ok" {
		t.Fatalf("network: %+v", m["network"])
	}
	if !Failed(checks) {
		t.Fatal("Failed should be true with a failing check")
	}
	for _, c := range checks {
		if c.Status != "ok" && c.Status != "warn" && c.Status != "fail" {
			t.Fatalf("bad status %q on %s", c.Status, c.Name)
		}
	}
	// Doctor must not create anything.
	if _, err := os.Stat(p.RecallDir); err == nil {
		t.Fatal("doctor created the recall dir")
	}
	if _, err := os.Stat(p.ClaudeDir); err == nil {
		t.Fatal("doctor created the claude dir")
	}
}

func TestRunHealthyEnvironment(t *testing.T) {
	fakeClaude(t, "2.1.267", sampleHelp, false)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/zsh")
	t.Setenv("ZDOTDIR", "")
	t.Setenv("TERM_PROGRAM", "Apple_Terminal")
	oldLook := lookPath
	lookPath = func(string) (string, error) { return "/opt/homebrew/bin/tmux", nil }
	t.Cleanup(func() { lookPath = oldLook })

	p := model.PathsFrom(filepath.Join(home, "claude"), filepath.Join(home, "recall"))
	proj := filepath.Join(p.ProjectsDir, "-Users-tester-a")
	for _, d := range []string{proj, filepath.Join(p.ProjectsDir, "-Users-tester-b"), p.SessionsDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range []string{"11111111-1.jsonl", "22222222-2.jsonl", "33333333-3.jsonl.orphaned-1"} {
		if err := os.WriteFile(filepath.Join(proj, n), []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(p.SessionsDir, "123.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.SessionsDir, "123.abc.key"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.SettingsFile, []byte(`{"cleanupPeriodDays": 3650}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := hook.InstallSettings(p, nil, "/usr/local/bin/recall"); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(p.RecallDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := shell.Install("zsh", filepath.Join(home, ".zshrc")); err != nil {
		t.Fatal(err)
	}

	checks, err := Run(p)
	if err != nil {
		t.Fatal(err)
	}
	m := byName(checks)
	for _, name := range []string{"claude", "projects", "registry", "retention", "tmux", "terminal", "recall-dir", "hooks", "widget", "network"} {
		if m[name].Status != "ok" {
			t.Errorf("%s: %+v", name, m[name])
		}
	}
	if !strings.Contains(m["projects"].Detail, "2 transcripts in 1 projects") {
		t.Fatalf("projects detail: %s", m["projects"].Detail)
	}
	if !strings.Contains(m["registry"].Detail, "1 registry entries") {
		t.Fatalf("registry detail: %s", m["registry"].Detail)
	}
	if Failed(checks) {
		t.Fatal("no failure expected")
	}
	if out := Format(checks); !strings.Contains(out, "ok    claude") {
		t.Fatalf("format:\n%s", out)
	}
}

func TestRunWarnsOnOldClaudeAndPerms(t *testing.T) {
	fakeClaude(t, "2.1.100", sampleHelp, false)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/tcsh")
	p := model.PathsFrom(filepath.Join(home, "claude"), filepath.Join(home, "recall"))
	if err := os.MkdirAll(p.RecallDir, 0o755); err != nil {
		t.Fatal(err)
	}
	checks, _ := Run(p)
	m := byName(checks)
	if m["claude"].Status != "warn" || !strings.Contains(m["claude"].Detail, MinClaudeVersion) {
		t.Fatalf("claude: %+v", m["claude"])
	}
	if m["recall-dir"].Status != "warn" || !strings.Contains(m["recall-dir"].Detail, "0755") {
		t.Fatalf("recall-dir: %+v", m["recall-dir"])
	}
	if m["widget"].Status != "warn" || !strings.Contains(m["widget"].Detail, "SHELL") {
		t.Fatalf("widget: %+v", m["widget"])
	}
}

func TestRunClaudeMissingIsFail(t *testing.T) {
	fakeClaude(t, "", "", true)
	p := model.PathsFrom(t.TempDir(), filepath.Join(t.TempDir(), "recall"))
	checks, _ := Run(p)
	if c := byName(checks)["claude"]; c.Status != "fail" {
		t.Fatalf("claude: %+v", c)
	}
}

func TestHooksPartial(t *testing.T) {
	p := model.PathsFrom(t.TempDir(), filepath.Join(t.TempDir(), "recall"))
	if err := hook.InstallSettings(p, []string{"SessionStart"}, "/usr/local/bin/recall"); err != nil {
		t.Fatal(err)
	}
	installed, missing, err := HooksInstalled(p)
	if err != nil || len(installed) != 1 || len(missing) != 2 {
		t.Fatalf("installed=%v missing=%v err=%v", installed, missing, err)
	}
	if c := checkHooks(p); c.Status != "warn" || !strings.Contains(c.Detail, "missing SessionEnd, Notification") {
		t.Fatalf("hooks: %+v", c)
	}
}

func TestParserSelfTest(t *testing.T) {
	c := checkParser()
	if c.Status != "ok" {
		t.Fatalf("parser self-test: %+v", c)
	}
	if !strings.Contains(c.Detail, "parsed") {
		t.Errorf("detail = %q", c.Detail)
	}
}
