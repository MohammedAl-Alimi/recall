package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const serveTestURL = "http://127.0.0.1:4747/?t=0123456789abcdef0123456789abcdef"

func TestBrowserArgv(t *testing.T) {
	got := browserArgv(serveTestURL)
	var want []string
	switch runtime.GOOS {
	case "darwin":
		want = []string{"open", serveTestURL}
	case "linux":
		want = []string{"xdg-open", serveTestURL}
	case "windows":
		want = []string{"cmd", "/c", "start", "", serveTestURL}
	}
	if len(got) != len(want) {
		t.Fatalf("browserArgv on %s = %v, want %v", runtime.GOOS, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("browserArgv on %s = %v, want %v", runtime.GOOS, got, want)
		}
	}
	if runtime.GOOS == "darwin" && (len(got) != 2 || got[0] != "open") {
		t.Errorf("darwin should open with 'open': %v", got)
	}
}

func TestOpenByDefault(t *testing.T) {
	// Anything that is not a file (a buffer, a pipe into another command)
	// must not trigger a browser.
	if openByDefault(&bytes.Buffer{}) {
		t.Error("openByDefault(*bytes.Buffer) = true, want false")
	}

	f, err := os.Create(filepath.Join(t.TempDir(), "out.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if openByDefault(f) {
		t.Error("openByDefault(regular file) = true, want false")
	}

	closed, err := os.Create(filepath.Join(t.TempDir(), "closed.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	if openByDefault(closed) {
		t.Error("openByDefault(closed file) = true, want false")
	}

	// A character device stands in for a terminal.
	dev, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Skipf("no %s to test the character device branch: %v", os.DevNull, err)
	}
	defer dev.Close()
	if !openByDefault(dev) {
		t.Errorf("openByDefault(%s) = false, want true", os.DevNull)
	}
}

func TestOpenBrowserDryRun(t *testing.T) {
	t.Setenv("RECALL_DRY_RUN", "1")
	// Nothing may be spawned, so an empty PATH must not change the result.
	t.Setenv("PATH", t.TempDir())

	var errOut bytes.Buffer
	if err := openBrowser(serveTestURL, &errOut); err != nil {
		t.Fatalf("openBrowser: %v", err)
	}
	line := strings.TrimSpace(errOut.String())
	if !strings.HasPrefix(line, "DRY RUN:") {
		t.Fatalf("stderr = %q, want a line starting with %q", line, "DRY RUN:")
	}
	if !strings.Contains(line, serveTestURL) {
		t.Errorf("stderr = %q, want it to contain the URL", line)
	}
	if runtime.GOOS == "darwin" && !strings.Contains(line, "open ") {
		t.Errorf("stderr = %q, want the open command", line)
	}
	if strings.Count(errOut.String(), "\n") != 1 {
		t.Errorf("stderr should be exactly one line: %q", errOut.String())
	}
}

func TestServeHelp(t *testing.T) {
	isolate(t)
	code, out, errOut := execCLI(t, "", "serve", "--help")
	if code != exitOK {
		t.Fatalf("code = %d, err = %q", code, errOut)
	}
	for _, want := range []string{"--addr", "--no-open", "--token", "loopback"} {
		if !strings.Contains(out, want) {
			t.Errorf("serve --help does not mention %q:\n%s", want, out)
		}
	}
}

func TestServeOpenAndNoOpenIsUsageError(t *testing.T) {
	isolate(t)
	code, out, errOut := execCLI(t, "", "serve", "--open", "--no-open")
	if code != exitUsage {
		t.Fatalf("code = %d, want %d (out %q err %q)", code, exitUsage, out, errOut)
	}
	if !strings.Contains(errOut, "either --open or --no-open") {
		t.Errorf("stderr = %q", errOut)
	}
	if out != "" {
		t.Errorf("stdout should stay empty, got %q", out)
	}
}

func TestServeRejectsArgsAndBadAddr(t *testing.T) {
	isolate(t)
	if code, _, _ := execCLI(t, "", "serve", "extra-arg"); code == exitOK {
		t.Error("serve with a positional argument should fail")
	}
	code, _, errOut := execCLI(t, "", "serve", "--addr", "0.0.0.0:0", "--no-open")
	if code != exitFailure {
		t.Fatalf("code = %d, want %d (err %q)", code, exitFailure, errOut)
	}
	if !strings.Contains(errOut, "refusing to bind") {
		t.Errorf("stderr = %q, want a loopback refusal", errOut)
	}
}
