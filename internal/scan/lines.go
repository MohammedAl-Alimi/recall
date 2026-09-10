package scan

import (
	"bufio"
	"bytes"
	"io"
	"os"
)

// lineBufSize is the bufio buffer used for forward reads. Lines longer than
// this are assembled in a growing buffer, so no line length limit applies.
const lineBufSize = 1 << 20

// forwardLines calls fn for every newline-terminated line found in f between
// offset start and start+limit. A trailing line without a newline is never
// consumed, so the returned offset always points just past the last complete
// line. Iteration stops early when fn returns false.
func forwardLines(f *os.File, start, limit int64, fn func(line []byte) bool) (int64, error) {
	if limit <= 0 {
		return start, nil
	}
	r := bufio.NewReaderSize(io.NewSectionReader(f, start, limit), lineBufSize)
	pos := start
	var long []byte
	for {
		chunk, err := r.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			long = append(long, chunk...)
			continue
		}
		if err == io.EOF {
			// Partial last line (or clean end): do not consume it.
			return pos, nil
		}
		if err != nil {
			return pos, err
		}
		var line []byte
		if long != nil {
			long = append(long, chunk...)
			line = long
			long = nil
		} else {
			line = chunk
		}
		pos += int64(len(line))
		if !fn(trimLine(line)) {
			return pos, nil
		}
	}
}

// countNewlines returns the number of '\n' bytes in f between start and end.
func countNewlines(f *os.File, start, end int64) (int, error) {
	if end <= start {
		return 0, nil
	}
	r := io.NewSectionReader(f, start, end-start)
	buf := make([]byte, lineBufSize)
	n := 0
	for {
		k, err := r.Read(buf)
		n += bytes.Count(buf[:k], []byte{'\n'})
		if err == io.EOF {
			return n, nil
		}
		if err != nil {
			return n, err
		}
	}
}

// tailLines reads the region [start, end) of f into memory and returns the
// offset of the first complete line inside it plus the complete lines found.
// When start is not known to be a line boundary (aligned false), everything
// up to and including the first newline is dropped. A trailing partial line
// is dropped too; consumed reports the offset just past the last complete
// line.
func tailLines(f *os.File, start, end int64, aligned bool) (lineStart int64, lines [][]byte, consumed int64, err error) {
	if end <= start {
		return start, nil, start, nil
	}
	buf := make([]byte, end-start)
	n, rerr := f.ReadAt(buf, start)
	if rerr != nil && rerr != io.EOF {
		return start, nil, start, rerr
	}
	buf = buf[:n]
	lineStart = start
	if !aligned {
		i := bytes.IndexByte(buf, '\n')
		if i < 0 {
			return end, nil, end, nil
		}
		buf = buf[i+1:]
		lineStart = start + int64(i+1)
	}
	consumed = lineStart
	for len(buf) > 0 {
		i := bytes.IndexByte(buf, '\n')
		if i < 0 {
			break
		}
		lines = append(lines, trimLine(buf[:i+1]))
		consumed += int64(i + 1)
		buf = buf[i+1:]
	}
	return lineStart, lines, consumed, nil
}

// trimLine strips the trailing newline and carriage return of a line.
func trimLine(b []byte) []byte {
	if n := len(b); n > 0 && b[n-1] == '\n' {
		b = b[:n-1]
	}
	if n := len(b); n > 0 && b[n-1] == '\r' {
		b = b[:n-1]
	}
	return b
}
