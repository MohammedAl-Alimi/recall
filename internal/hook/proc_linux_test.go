//go:build linux

package hook

import (
	"reflect"
	"testing"
)

func TestParseProcStatPpid(t *testing.T) {
	cases := []struct {
		stat string
		want int
		err  bool
	}{
		{"1234 (claude) S 80 1234 1234 0 -1 4194560 100 0 0 0 1 1 0 0 20 0 1 0 5 0 0", 80, false},
		{"1234 (weird (name) x) R 4321 1 1 0 -1 0", 4321, false},
		{"garbage", 0, true},
		{"1 (x)", 0, true},
	}
	for _, c := range cases {
		got, err := parseProcStatPpid([]byte(c.stat))
		if (err != nil) != c.err || got != c.want {
			t.Errorf("%q: got %d %v, want %d error %v", c.stat, got, err, c.want, c.err)
		}
	}
}

func TestParseProcCmdline(t *testing.T) {
	prompt := "explain the --add-dir /Users option"
	in := []byte("claude\x00-p\x00" + prompt + "\x00\x00--model=x y\x00")
	want := []string{"claude", "-p", prompt, "", "--model=x y"}
	if got := parseProcCmdline(in); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := parseProcCmdline(nil); got != nil {
		t.Fatalf("empty cmdline: got %q", got)
	}
}
