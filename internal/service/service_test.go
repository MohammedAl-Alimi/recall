package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testConfig returns a fully specified config that writes into t.TempDir,
// with launchctl disabled. No test in this package may ever reach launchd.
func testConfig(t *testing.T, kind Kind) Config {
	t.Helper()
	t.Setenv(NoLaunchctlEnv, "1")
	dir := t.TempDir()
	return Config{
		Kind:            kind,
		BinPath:         "/usr/local/bin/recall",
		Label:           LabelPrefix + string(kind),
		LaunchAgentsDir: filepath.Join(dir, "LaunchAgents"),
		LogDir:          filepath.Join(dir, "logs"),
		Addr:            "127.0.0.1:4747",
		Hour:            DefaultHour,
		Minute:          DefaultMinute,
	}
}

func TestServePlistContents(t *testing.T) {
	c := testConfig(t, KindServe)
	got, err := c.Plist()
	if err != nil {
		t.Fatalf("Plist: %v", err)
	}
	for _, want := range []string{
		`<?xml version="1.0" encoding="UTF-8"?>`,
		`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"`,
		"<key>Label</key>\n\t<string>dev.recall.serve</string>",
		"<key>ProgramArguments</key>\n\t<array>\n\t\t<string>/usr/local/bin/recall</string>\n\t\t<string>serve</string>\n\t\t<string>--no-open</string>\n\t\t<string>--addr</string>\n\t\t<string>127.0.0.1:4747</string>\n\t</array>",
		"<key>RunAtLoad</key>\n\t<true/>",
		"<key>KeepAlive</key>\n\t<dict>\n\t\t<key>SuccessfulExit</key>\n\t\t<false/>\n\t</dict>",
		"<key>StandardOutPath</key>\n\t<string>" + c.LogDir + "/serve.out.log</string>",
		"<key>StandardErrorPath</key>\n\t<string>" + c.LogDir + "/serve.err.log</string>",
		"<key>ProcessType</key>\n\t<string>Background</string>",
		"</dict>\n</plist>\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("serve plist is missing:\n%s\n\ngot:\n%s", want, got)
		}
	}
	// The dashboard must never be scheduled, and never restarted after a
	// clean stop.
	if strings.Contains(got, "StartCalendarInterval") {
		t.Error("serve plist must not carry a calendar interval")
	}
}

func TestArchivePlistContents(t *testing.T) {
	c := testConfig(t, KindArchive)
	c.Hour, c.Minute = 4, 30
	got, err := c.Plist()
	if err != nil {
		t.Fatalf("Plist: %v", err)
	}
	for _, want := range []string{
		"<key>Label</key>\n\t<string>dev.recall.archive</string>",
		"<key>ProgramArguments</key>\n\t<array>\n\t\t<string>/usr/local/bin/recall</string>\n\t\t<string>archive</string>\n\t\t<string>--all</string>\n\t\t<string>--quiet</string>\n\t</array>",
		"<key>RunAtLoad</key>\n\t<false/>",
		"<key>StartCalendarInterval</key>\n\t<dict>\n\t\t<key>Hour</key>\n\t\t<integer>4</integer>\n\t\t<key>Minute</key>\n\t\t<integer>30</integer>\n\t</dict>",
		"<key>StandardOutPath</key>\n\t<string>" + c.LogDir + "/archive.out.log</string>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("archive plist is missing:\n%s\n\ngot:\n%s", want, got)
		}
	}
	// A job that exits after doing its work must not be restarted.
	if strings.Contains(got, "KeepAlive") {
		t.Error("archive plist must not carry KeepAlive")
	}
	if strings.Contains(got, "--no-open") || strings.Contains(got, "--addr") {
		t.Error("archive plist must not carry serve flags")
	}
}

func TestPlistPassesThroughDirectories(t *testing.T) {
	c := testConfig(t, KindServe)
	c.ClaudeDir, c.RecallDir = "/Users/me/alt-claude", "/Users/me/alt-recall"
	argv, err := c.Args()
	if err != nil {
		t.Fatalf("Args: %v", err)
	}
	want := []string{"/usr/local/bin/recall", "serve", "--no-open", "--addr", "127.0.0.1:4747", "--claude-dir", "/Users/me/alt-claude", "--recall-dir", "/Users/me/alt-recall"}
	if strings.Join(argv, " ") != strings.Join(want, " ") {
		t.Fatalf("Args = %q, want %q", argv, want)
	}
	// An empty directory must not turn into an empty flag value.
	c.ClaudeDir, c.RecallDir = "", ""
	argv, _ = c.Args()
	if len(argv) != 5 {
		t.Fatalf("unset directories should add no flags, got %q", argv)
	}
}

func TestPlistEscapesXML(t *testing.T) {
	c := testConfig(t, KindServe)
	c.BinPath = "/Users/me/Dev & Ops/<recall>"
	c.LogDir = "/Users/me/Dev & Ops/logs"
	got, err := c.Plist()
	if err != nil {
		t.Fatalf("Plist: %v", err)
	}
	if !strings.Contains(got, "<string>/Users/me/Dev &amp; Ops/&lt;recall&gt;</string>") {
		t.Errorf("binary path was not escaped:\n%s", got)
	}
	if !strings.Contains(got, "<string>/Users/me/Dev &amp; Ops/logs/serve.out.log</string>") {
		t.Errorf("log path was not escaped:\n%s", got)
	}
	// A bare ampersand anywhere would make the file unparsable.
	for _, line := range strings.Split(got, "\n") {
		rest := line
		for {
			i := strings.Index(rest, "&")
			if i < 0 {
				break
			}
			rest = rest[i+1:]
			if !strings.HasPrefix(rest, "amp;") && !strings.HasPrefix(rest, "lt;") && !strings.HasPrefix(rest, "gt;") {
				t.Fatalf("unescaped ampersand in %q", line)
			}
		}
	}
}

func TestPaths(t *testing.T) {
	c := testConfig(t, KindArchive)
	if want := filepath.Join(c.LaunchAgentsDir, "dev.recall.archive.plist"); c.PlistPath() != want {
		t.Errorf("PlistPath = %q, want %q", c.PlistPath(), want)
	}
	if want := filepath.Join(c.LogDir, "archive.out.log"); c.StdoutPath() != want {
		t.Errorf("StdoutPath = %q, want %q", c.StdoutPath(), want)
	}
	if want := filepath.Join(c.LogDir, "archive.err.log"); c.StderrPath() != want {
		t.Errorf("StderrPath = %q, want %q", c.StderrPath(), want)
	}
	if got := c.At(); got != "09:00" {
		t.Errorf("At = %q, want 09:00", got)
	}
}

func TestDefaultConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("RECALL_DIR", filepath.Join(home, ".recall"))
	c, err := DefaultConfig(KindServe, "/opt/recall")
	if err != nil {
		t.Fatalf("DefaultConfig: %v", err)
	}
	if c.Label != "dev.recall.serve" {
		t.Errorf("Label = %q", c.Label)
	}
	if c.LaunchAgentsDir != filepath.Join(home, "Library", "LaunchAgents") {
		t.Errorf("LaunchAgentsDir = %q", c.LaunchAgentsDir)
	}
	if c.LogDir != filepath.Join(home, ".recall", "logs") {
		t.Errorf("LogDir = %q", c.LogDir)
	}
	if c.Addr != "127.0.0.1:4747" {
		t.Errorf("Addr = %q", c.Addr)
	}
	if c.Hour != 9 || c.Minute != 0 {
		t.Errorf("schedule = %d:%d, want 9:0", c.Hour, c.Minute)
	}
	if _, err := DefaultConfig(Kind("nope"), "/opt/recall"); err == nil {
		t.Error("an unknown kind must be rejected")
	}
	if _, err := DefaultConfig(KindServe, ""); err == nil {
		t.Error("an empty binary path must be rejected")
	}
}

func TestInstallWritesFileAndSkipsLaunchctl(t *testing.T) {
	c := testConfig(t, KindServe)
	// Any launchctl call at all is a test failure: this machine must not
	// gain a background job.
	restore := stubLaunchctl(t)
	defer restore()

	path, err := Install(c)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if path != c.PlistPath() {
		t.Fatalf("Install returned %q, want %q", path, c.PlistPath())
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("plist not written: %v", err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("plist mode = %v, want 0644", info.Mode().Perm())
	}
	if _, err := os.Stat(c.LogDir); err != nil {
		t.Errorf("log directory not created: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := c.Plist()
	if string(body) != want {
		t.Errorf("written plist differs from Plist()")
	}
	// Installing twice must overwrite rather than fail.
	if _, err := Install(c); err != nil {
		t.Fatalf("second Install: %v", err)
	}
}

func TestUninstallRemovesAndIsIdempotent(t *testing.T) {
	c := testConfig(t, KindArchive)
	restore := stubLaunchctl(t)
	defer restore()

	if _, err := Install(c); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if err := Uninstall(c); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if _, err := os.Stat(c.PlistPath()); !os.IsNotExist(err) {
		t.Fatalf("plist still there: %v", err)
	}
	// Removing a plist that is not there is not an error.
	if err := Uninstall(c); err != nil {
		t.Fatalf("second Uninstall: %v", err)
	}
}

func TestStatusDegradesWithoutLaunchctl(t *testing.T) {
	c := testConfig(t, KindServe)
	restore := stubLaunchctl(t)
	defer restore()

	st, err := StatusOf(c)
	if err != nil {
		t.Fatalf("StatusOf: %v", err)
	}
	if st.Installed {
		t.Error("nothing is installed yet")
	}
	if st.PlistPath != c.PlistPath() {
		t.Errorf("PlistPath = %q", st.PlistPath)
	}
	if _, err := Install(c); err != nil {
		t.Fatal(err)
	}
	st, err = StatusOf(c)
	if err != nil {
		t.Fatalf("StatusOf: %v", err)
	}
	if !st.Installed {
		t.Error("Installed should be true after Install")
	}
	if st.Loaded || st.PID != 0 {
		t.Errorf("launchctl must not be consulted: %+v", st)
	}
	if !strings.Contains(st.Detail, NoLaunchctlEnv) {
		t.Errorf("Detail should explain the skip, got %q", st.Detail)
	}
}

// stubLaunchctl replaces the launchctl runner with one that fails the test
// if it is ever called, and returns a function restoring the original.
func stubLaunchctl(t *testing.T) func() {
	t.Helper()
	prev := runLaunchctl
	runLaunchctl = func(args ...string) ([]byte, error) {
		t.Errorf("launchctl must not run under %s=1, got: %v", NoLaunchctlEnv, args)
		return nil, nil
	}
	return func() { runLaunchctl = prev }
}

func TestUnsupportedErrorText(t *testing.T) {
	err := unsupportedError("linux", []string{"/usr/local/bin/recall", "serve", "--no-open"})
	msg := err.Error()
	for _, want := range []string{"not supported on linux", "systemd", "/usr/local/bin/recall serve --no-open"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error text is missing %q: %s", want, msg)
		}
	}
}

func TestSupported(t *testing.T) {
	ok, why := Supported()
	if why == "" {
		t.Error("Supported must always explain itself")
	}
	// This suite runs on darwin in CI and on the developer machine; the
	// reason string must name the mechanism either way.
	if ok && !strings.Contains(why, "launchd") {
		t.Errorf("reason = %q", why)
	}
	if !ok && !strings.Contains(why, "macOS") {
		t.Errorf("reason = %q", why)
	}
}

func TestParseAt(t *testing.T) {
	for _, tc := range []struct {
		in           string
		hour, minute int
		wantErr      bool
	}{
		{in: "09:00", hour: 9},
		{in: "0:5", hour: 0, minute: 5},
		{in: "23:59", hour: 23, minute: 59},
		{in: " 4 : 30 ", hour: 4, minute: 30},
		{in: "24:00", wantErr: true},
		{in: "9", wantErr: true},
		{in: "09:60", wantErr: true},
		{in: "nine:zero", wantErr: true},
		{in: "", wantErr: true},
	} {
		h, m, err := ParseAt(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseAt(%q) should fail", tc.in)
			}
			continue
		}
		if err != nil || h != tc.hour || m != tc.minute {
			t.Errorf("ParseAt(%q) = %d, %d, %v", tc.in, h, m, err)
		}
	}
}

func TestValidateRejectsBadConfigs(t *testing.T) {
	base := testConfig(t, KindArchive)
	bad := base
	bad.Hour = 24
	if _, err := bad.Plist(); err == nil {
		t.Error("hour 24 must be rejected")
	}
	bad = base
	bad.Minute = -1
	if _, err := bad.Plist(); err == nil {
		t.Error("minute -1 must be rejected")
	}
	bad = testConfig(t, KindServe)
	bad.Addr = ""
	if _, err := bad.Plist(); err == nil {
		t.Error("an empty serve address must be rejected")
	}
	bad = base
	bad.Kind = Kind("cron")
	if _, err := bad.Plist(); err == nil {
		t.Error("an unknown kind must be rejected")
	}
}

func TestInstalledAddr(t *testing.T) {
	c := testConfig(t, KindServe)
	c.Addr = "127.0.0.1:9999"
	restore := stubLaunchctl(t)
	defer restore()
	if _, ok := InstalledAddr(c); ok {
		t.Error("nothing is installed yet")
	}
	if _, err := Install(c); err != nil {
		t.Fatal(err)
	}
	got, ok := InstalledAddr(c)
	if !ok || got != "127.0.0.1:9999" {
		t.Fatalf("InstalledAddr = %q, %v", got, ok)
	}
	// The archive plist carries no address at all.
	a := testConfig(t, KindArchive)
	a.LaunchAgentsDir = c.LaunchAgentsDir
	if _, err := Install(a); err != nil {
		t.Fatal(err)
	}
	if got, ok := InstalledAddr(a); ok {
		t.Errorf("archive plist should have no address, got %q", got)
	}
}

func TestParseLaunchctlOutput(t *testing.T) {
	print := `gui/501/dev.recall.serve = {
	active count = 1
	path = /Users/me/Library/LaunchAgents/dev.recall.serve.plist
	state = running

	pid = 4242
	last exit code = 0
}`
	if pid, exit := parsePrint(print); pid != 4242 || exit != 0 {
		t.Errorf("parsePrint = %d, %d", pid, exit)
	}
	list := `{
	"Label" = "dev.recall.archive";
	"LastExitStatus" = 0;
	"PID" = 777;
};`
	if pid, exit := parseList(list); pid != 777 || exit != 0 {
		t.Errorf("parseList = %d, %d", pid, exit)
	}
	// A job that is loaded but not running has no pid line at all.
	if pid, _ := parsePrint("gui/501/dev.recall.archive = {\n\tstate = not running\n}"); pid != 0 {
		t.Errorf("pid should be 0 when the job is not running, got %d", pid)
	}
}

func TestURL(t *testing.T) {
	c := testConfig(t, KindServe)
	if got := c.URL("deadbeef"); got != "http://127.0.0.1:4747/?t=deadbeef" {
		t.Errorf("URL = %q", got)
	}
	c.Addr = ""
	if got := c.URL("x"); !strings.HasPrefix(got, "http://127.0.0.1:4747/") {
		t.Errorf("an empty address should fall back to the default, got %q", got)
	}
}
