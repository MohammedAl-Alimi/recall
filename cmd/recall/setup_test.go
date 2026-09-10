package main

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MohammedAl-Alimi/recall/internal/model"
	"github.com/MohammedAl-Alimi/recall/internal/shell"
)

// The setup and uninstall commands are never executed in tests; only their
// building blocks are, against temp dirs.

func TestConfirm(t *testing.T) {
	cases := []struct {
		in   string
		yes  bool
		want bool
	}{
		{"y\n", false, true},
		{"YES\n", false, true},
		{"n\n", false, false},
		{"\n", false, false},
		{"", false, false},
		{"maybe\n", false, false},
		{"", true, true},
	}
	for _, c := range cases {
		var out bytes.Buffer
		got := confirm(bufio.NewReader(strings.NewReader(c.in)), &out, "apply?", c.yes)
		if got != c.want {
			t.Errorf("confirm(%q, yes=%v) = %v want %v", c.in, c.yes, got, c.want)
		}
		if !strings.Contains(out.String(), "apply? [y/N]") {
			t.Errorf("prompt missing: %q", out.String())
		}
	}
}

func TestRunStepsSkipsAndApplies(t *testing.T) {
	applied := map[string]int{}
	mk := func(name, desc string, fail bool) setupStep {
		return setupStep{
			Name:     name,
			Describe: func() string { return desc },
			Apply: func() error {
				applied[name]++
				if fail {
					return errors.New("boom")
				}
				return nil
			},
		}
	}
	steps := []setupStep{mk("a", "change a", false), mk("b", "", false), mk("c", "change c", false)}
	var out bytes.Buffer
	// y for a, c is declined; b is skipped without a question.
	if err := runSteps(steps, bufio.NewReader(strings.NewReader("y\nn\n")), &out, false); err != nil {
		t.Fatal(err)
	}
	if applied["a"] != 1 || applied["b"] != 0 || applied["c"] != 0 {
		t.Fatalf("applied = %v", applied)
	}
	s := out.String()
	for _, want := range []string{"[1/3] a", "will: change a", "done", "[2/3] b", "nothing to do", "[3/3] c", "skipped"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in output:\n%s", want, s)
		}
	}
	// --yes applies everything and reports failures.
	out.Reset()
	applied = map[string]int{}
	err := runSteps([]setupStep{mk("x", "change x", true), mk("y", "change y", false)}, bufio.NewReader(strings.NewReader("")), &out, true)
	if err == nil || !strings.Contains(err.Error(), "1 step(s) failed") {
		t.Fatalf("err = %v", err)
	}
	if applied["x"] != 1 || applied["y"] != 1 {
		t.Fatalf("applied = %v", applied)
	}
}

func TestWidgetStepDescribe(t *testing.T) {
	dir := t.TempDir()
	rc := filepath.Join(dir, ".zshrc")
	if got := widgetStep("", "").Describe(); got != "" {
		t.Fatalf("unknown shell should be a no-op, got %q", got)
	}
	if got := widgetStep("zsh", rc).Describe(); !strings.Contains(got, "append") || !strings.Contains(got, rc) {
		t.Fatalf("fresh rc: %q", got)
	}
	if err := os.WriteFile(rc, []byte("x\n"+shell.BeginMarker+"\n"+shell.EndMarker+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := widgetStep("zsh", rc).Describe(); !strings.Contains(got, "refresh") {
		t.Fatalf("existing block: %q", got)
	}
}

func TestHooksAndRetentionStepDescribe(t *testing.T) {
	claude, recall := t.TempDir(), t.TempDir()
	p := model.PathsFrom(claude, recall)
	got := hooksStep(p, "/usr/local/bin/recall").Describe()
	for _, want := range []string{"SessionStart", "SessionEnd", "Notification", "/usr/local/bin/recall hook", p.SettingsFile} {
		if !strings.Contains(got, want) {
			t.Errorf("hooks describe missing %q: %q", want, got)
		}
	}
	st := retentionStep(p, 365)
	if st.Name != "retention" {
		t.Fatal(st.Name)
	}
	if got := st.Describe(); !strings.Contains(got, "cleanupPeriodDays=365") {
		t.Errorf("retention describe = %q", got)
	}
}
