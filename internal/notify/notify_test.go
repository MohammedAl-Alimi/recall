package notify

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestScript(t *testing.T) {
	cases := []struct{ title, body, want string }{
		{"recall", "Needs you: fix tests", `display notification "Needs you: fix tests" with title "recall"`},
		{"", "plain", `display notification "plain"`},
		{`say "hi"`, `back\slash`, `display notification "back\\slash" with title "say \"hi\""`},
	}
	for _, tc := range cases {
		if got := Script(tc.title, tc.body); got != tc.want {
			t.Errorf("Script(%q, %q) = %q, want %q", tc.title, tc.body, got, tc.want)
		}
	}
}

func TestSendDryRun(t *testing.T) {
	t.Setenv("RECALL_DRY_RUN", "1")
	var buf bytes.Buffer
	old := Stdout
	Stdout = &buf
	t.Cleanup(func() { Stdout = old })
	called := false
	oldRunner := runner
	runner = func(string) error { called = true; return nil }
	t.Cleanup(func() { runner = oldRunner })

	if err := Send("recall", "Needs you"); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Errorf("dry run must not execute osascript")
	}
	if got := buf.String(); got != "DRY RUN: notify title=\"recall\" body=\"Needs you\"\n" {
		t.Errorf("output = %q", got)
	}
}

func TestSendUsesRunner(t *testing.T) {
	t.Setenv("RECALL_DRY_RUN", "")
	var got string
	oldRunner := runner
	runner = func(s string) error { got = s; return nil }
	t.Cleanup(func() { runner = oldRunner })

	if err := Send("  recall ", "session abc needs you\n"); err != nil {
		t.Fatal(err)
	}
	if want := `display notification "session abc needs you" with title "recall"`; got != want {
		t.Errorf("script = %q, want %q", got, want)
	}

	runner = func(string) error { return errors.New("boom") }
	if err := Send("t", "b"); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("runner error must propagate, got %v", err)
	}
	if err := Send("", " "); err == nil {
		t.Errorf("empty notification must fail")
	}
}
