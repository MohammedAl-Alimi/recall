//go:build darwin

package hook

import (
	"encoding/binary"
	"reflect"
	"testing"
)

func procArgs2Buf(argc int32, exe string, pad int, args []string, env []string) []byte {
	buf := binary.NativeEndian.AppendUint32(nil, uint32(argc))
	buf = append(buf, exe...)
	buf = append(buf, 0)
	for i := 0; i < pad; i++ {
		buf = append(buf, 0)
	}
	for _, a := range args {
		buf = append(buf, a...)
		buf = append(buf, 0)
	}
	for _, e := range env {
		buf = append(buf, e...)
		buf = append(buf, 0)
	}
	return buf
}

func TestParseProcArgs2(t *testing.T) {
	prompt := "explain the --add-dir /Users option"
	args := []string{"claude", "-p", prompt, "", "--model=x y"}
	cases := []struct {
		name string
		buf  []byte
		want []string
		err  bool
	}{
		{"padded", procArgs2Buf(5, "/usr/local/bin/claude", 7, args, []string{"HOME=/tmp"}), args, false},
		{"unpadded", procArgs2Buf(5, "/usr/local/bin/claude", 0, args, nil), args, false},
		{"no env", procArgs2Buf(1, "/bin/x", 3, []string{"x"}, nil), []string{"x"}, false},
		{"zero argc", procArgs2Buf(0, "/bin/x", 3, nil, []string{"A=b"}), []string{}, false},
		{"short", []byte{1, 0}, nil, true},
		{"negative argc", procArgs2Buf(-1, "/bin/x", 0, nil, nil), nil, true},
		{"truncated", procArgs2Buf(3, "/bin/x", 1, []string{"x"}, nil), nil, true},
	}
	for _, c := range cases {
		got, err := parseProcArgs2(c.buf)
		if (err != nil) != c.err {
			t.Errorf("%s: err = %v, want error %v", c.name, err, c.err)
			continue
		}
		if !c.err && !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
