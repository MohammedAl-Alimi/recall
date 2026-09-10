package launch

import "testing"

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"":            "''",
		"abc":         "abc",
		"/a/b-c.d":    "/a/b-c.d",
		"a b":         "'a b'",
		"it's":        `'it'\''s'`,
		"$HOME":       "'$HOME'",
		"--resume=x":  "--resume=x",
		"Work @act.3": "'Work @act.3'",
	}
	for in, want := range cases {
		if got := ShellQuote(in); got != want {
			t.Errorf("ShellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}
