package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MohammedAl-Alimi/recall/internal/service"
)

// serviceEnv isolates the CLI and makes sure launchctl is never called, so
// running the test suite can never leave a background job on the machine.
// It returns the temp home, whose Library/LaunchAgents is where plists land.
func serviceEnv(t *testing.T) (home, claudeDir, recallDir string) {
	t.Helper()
	claudeDir, recallDir = isolate(t)
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(service.NoLaunchctlEnv, "1")
	return home, claudeDir, recallDir
}

// agentsDir is where 'recall service install' writes under a temp home.
func agentsDir(home string) string { return filepath.Join(home, "Library", "LaunchAgents") }

func TestServiceHelp(t *testing.T) {
	serviceEnv(t)
	code, out, errOut := execCLI(t, "", "service", "--help")
	if code != 0 {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	for _, want := range []string{"install", "uninstall", "status", "url", "bookmarkable"} {
		if !strings.Contains(out, want) {
			t.Errorf("service --help does not mention %q:\n%s", want, out)
		}
	}
	code, out, _ = execCLI(t, "", "service", "install", "--help")
	if code != 0 {
		t.Fatalf("install --help code=%d", code)
	}
	for _, want := range []string{"--serve", "--archive", "--addr", "--at", "--yes"} {
		if !strings.Contains(out, want) {
			t.Errorf("install --help does not mention %q:\n%s", want, out)
		}
	}
}

func TestServiceInstallDryRun(t *testing.T) {
	home, _, _ := serviceEnv(t) // isolate() already sets RECALL_DRY_RUN=1
	code, out, errOut := execCLI(t, "", "service", "install", "--yes")
	if code != 0 {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	for _, want := range []string{
		"DRY RUN: nothing was written",
		"dev.recall.serve.plist",
		"dev.recall.archive.plist",
		"<string>--no-open</string>",
		"<key>StartCalendarInterval</key>",
		"serve --no-open --addr 127.0.0.1:4747",
		"archive --all --quiet",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dry run output is missing %q:\n%s", want, out)
		}
	}
	if entries, err := os.ReadDir(agentsDir(home)); err == nil && len(entries) > 0 {
		t.Fatalf("dry run wrote %d file(s) into %s", len(entries), agentsDir(home))
	}
}

func TestServiceUninstallDryRun(t *testing.T) {
	serviceEnv(t)
	code, out, errOut := execCLI(t, "", "service", "uninstall", "--yes")
	if code != 0 {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	if !strings.Contains(out, "DRY RUN: nothing was stopped") {
		t.Errorf("uninstall dry run output:\n%s", out)
	}
}

func TestServiceInstallStatusUninstall(t *testing.T) {
	home, _, recallDir := serviceEnv(t)
	t.Setenv("RECALL_DRY_RUN", "")

	code, out, errOut := execCLI(t, "", "--recall-dir", recallDir, "service", "install", "--yes", "--at", "04:30")
	if code != 0 {
		t.Fatalf("install: code=%d out=%q err=%q", code, out, errOut)
	}
	if !strings.Contains(out, "install 2 service(s)? [y/N] y") {
		t.Errorf("--yes should answer the question in the transcript:\n%s", out)
	}

	servePlist := filepath.Join(agentsDir(home), "dev.recall.serve.plist")
	archivePlist := filepath.Join(agentsDir(home), "dev.recall.archive.plist")
	for _, p := range []string{servePlist, archivePlist} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s was not written: %v", p, err)
		}
	}
	body, err := os.ReadFile(archivePlist)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "<key>Hour</key>\n\t\t<integer>4</integer>") {
		t.Errorf("--at 04:30 did not reach the plist:\n%s", body)
	}
	if !strings.Contains(string(body), "<string>--recall-dir</string>\n\t\t<string>"+recallDir+"</string>") {
		t.Errorf("--recall-dir was not passed through:\n%s", body)
	}
	if _, err := os.Stat(filepath.Join(recallDir, "logs")); err != nil {
		t.Errorf("log directory not created under the recall dir: %v", err)
	}

	code, out, errOut = execCLI(t, "", "--recall-dir", recallDir, "service", "status")
	if code != 0 {
		t.Fatalf("status: code=%d err=%q", code, errOut)
	}
	if !strings.Contains(out, "SERVICE") || !strings.Contains(out, "INSTALLED") || !strings.Contains(out, "PLIST") {
		t.Errorf("status is not a table:\n%s", out)
	}
	if !strings.Contains(out, servePlist) || !strings.Contains(out, archivePlist) {
		t.Errorf("status does not list both plists:\n%s", out)
	}
	if !strings.Contains(out, service.NoLaunchctlEnv) {
		t.Errorf("status should explain that launchctl was not consulted:\n%s", out)
	}
	if !strings.Contains(out, "dashboard http://") {
		t.Errorf("status should print the dashboard URL:\n%s", out)
	}

	code, out, errOut = execCLI(t, "", "--recall-dir", recallDir, "service", "uninstall", "--yes")
	if code != 0 {
		t.Fatalf("uninstall: code=%d out=%q err=%q", code, out, errOut)
	}
	for _, p := range []string{servePlist, archivePlist} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s survived uninstall: %v", p, err)
		}
	}
	// Uninstalling again is not an error.
	if code, _, errOut := execCLI(t, "", "--recall-dir", recallDir, "service", "uninstall", "--yes"); code != 0 {
		t.Fatalf("second uninstall: code=%d err=%q", code, errOut)
	}
}

func TestServiceInstallOnlyOne(t *testing.T) {
	home, _, recallDir := serviceEnv(t)
	t.Setenv("RECALL_DRY_RUN", "")
	code, _, errOut := execCLI(t, "", "--recall-dir", recallDir, "service", "install", "--archive", "--yes")
	if code != 0 {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(agentsDir(home), "dev.recall.archive.plist")); err != nil {
		t.Fatalf("archive plist missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(agentsDir(home), "dev.recall.serve.plist")); !os.IsNotExist(err) {
		t.Fatalf("--archive must not install the dashboard: %v", err)
	}
}

func TestServiceInstallWithoutYesAsks(t *testing.T) {
	home, _, recallDir := serviceEnv(t)
	t.Setenv("RECALL_DRY_RUN", "")
	code, out, errOut := execCLI(t, "n\n", "--recall-dir", recallDir, "service", "install")
	if code != 0 {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	if !strings.Contains(out, "nothing was installed") {
		t.Errorf("answering no should install nothing:\n%s", out)
	}
	if entries, err := os.ReadDir(agentsDir(home)); err == nil && len(entries) > 0 {
		t.Fatalf("a declined install wrote %d file(s)", len(entries))
	}
}

func TestServiceInstallRejectsBadTime(t *testing.T) {
	serviceEnv(t)
	if code, _, errOut := execCLI(t, "", "service", "install", "--at", "25:00", "--yes"); code != exitUsage {
		t.Fatalf("code=%d err=%q, want a usage error", code, errOut)
	}
}

func TestServiceURLIsOneLine(t *testing.T) {
	_, _, recallDir := serviceEnv(t)
	code, out, errOut := execCLI(t, "", "--recall-dir", recallDir, "service", "url")
	if code != 0 {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	if strings.Count(out, "\n") != 1 {
		t.Fatalf("url must print exactly one line, got %q", out)
	}
	line := strings.TrimSpace(out)
	if !strings.HasPrefix(line, "http://127.0.0.1:4747/?t=") {
		t.Fatalf("unexpected URL %q", line)
	}
	token, err := os.ReadFile(filepath.Join(recallDir, "serve.token"))
	if err != nil {
		t.Fatalf("the token should have been stored: %v", err)
	}
	if !strings.Contains(line, strings.TrimSpace(string(token))) {
		t.Fatalf("URL %q does not carry the stored token %q", line, token)
	}
	// The URL is stable: a second call returns the same one.
	_, again, _ := execCLI(t, "", "--recall-dir", recallDir, "service", "url")
	if strings.TrimSpace(again) != line {
		t.Fatalf("URL changed between calls: %q then %q", line, again)
	}
}

func TestServiceURLFollowsInstalledAddr(t *testing.T) {
	_, _, recallDir := serviceEnv(t)
	t.Setenv("RECALL_DRY_RUN", "")
	if code, _, errOut := execCLI(t, "", "--recall-dir", recallDir, "service", "install", "--serve", "--addr", "127.0.0.1:5151", "--yes"); code != 0 {
		t.Fatalf("install: code=%d err=%q", code, errOut)
	}
	code, out, errOut := execCLI(t, "", "--recall-dir", recallDir, "service", "url")
	if code != 0 {
		t.Fatalf("url: code=%d err=%q", code, errOut)
	}
	if !strings.HasPrefix(strings.TrimSpace(out), "http://127.0.0.1:5151/?t=") {
		t.Fatalf("url should follow the installed address, got %q", out)
	}
}

func TestArchiveQuiet(t *testing.T) {
	claudeDir, _ := isolate(t)
	writeFixture(t, claudeDir)

	code, out, errOut := execCLI(t, "", "archive", "--all", "--quiet")
	if code != 0 {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	line := strings.TrimSpace(out)
	if strings.Count(out, "\n") != 1 || !strings.HasPrefix(line, "archived ") {
		t.Fatalf("quiet run should print one summary line, got %q", out)
	}
	if !strings.Contains(line, "0 already archived") || !strings.Contains(line, "0 failed") {
		t.Fatalf("summary = %q", line)
	}

	// Nothing changed since, so the second run is silent and archives
	// nothing: that is what makes the daily job cheap.
	code, out, errOut = execCLI(t, "", "archive", "--all", "--quiet")
	if code != 0 {
		t.Fatalf("second run: code=%d err=%q", code, errOut)
	}
	if out != "" {
		t.Fatalf("a run that changes nothing should print nothing, got %q", out)
	}

	// Without --quiet the already archived sessions are still reported, one
	// line each.
	code, out, errOut = execCLI(t, "", "archive", "--all")
	if code != 0 {
		t.Fatalf("loud run: code=%d err=%q", code, errOut)
	}
	if !strings.Contains(out, "(already archived)") {
		t.Fatalf("loud run should say what it skipped, got %q", out)
	}
}
