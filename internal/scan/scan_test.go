package scan

import "testing"

func TestEncodeProjectDir(t *testing.T) {
	cases := map[string]string{
		"/Users/me/dev/recall":    "-Users-me-dev-recall",
		"/tmp/a b.c":              "-tmp-a-b-c",
		"C:\\work":                "C--work",
		"":                        "",
		"/Users/me/Work @act.3 /": "-Users-me-Work--act-3--",
	}
	for in, want := range cases {
		if got := EncodeProjectDir(in); got != want {
			t.Errorf("EncodeProjectDir(%q) = %q, want %q", in, got, want)
		}
	}
}
