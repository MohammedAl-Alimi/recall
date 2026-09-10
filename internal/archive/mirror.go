package archive

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/MohammedAl-Alimi/recall/internal/model"
)

// MirrorHistory appends new history.jsonl lines to
// RecallDir/history.mirror.jsonl. The byte offset already mirrored is kept in
// RecallDir/history.offset; only complete lines are copied. When history.jsonl
// shrinks (rotated or rewritten) the mirror restarts from offset 0.
func MirrorHistory(p model.Paths) error {
	src, err := os.Open(p.HistoryFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer src.Close()
	st, err := src.Stat()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(p.RecallDir, 0o700); err != nil {
		return err
	}
	offPath := filepath.Join(p.RecallDir, OffsetName)
	offset := readOffset(offPath)
	if offset > st.Size() {
		offset = 0
	}
	if offset == st.Size() {
		return nil
	}
	if _, err := src.Seek(offset, io.SeekStart); err != nil {
		return err
	}
	data, err := io.ReadAll(src)
	if err != nil {
		return err
	}
	cut := bytes.LastIndexByte(data, '\n')
	if cut < 0 {
		return nil
	}
	data = data[:cut+1]
	dst, err := os.OpenFile(filepath.Join(p.RecallDir, MirrorName), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err := dst.Write(data); err != nil {
		dst.Close()
		return err
	}
	if err := dst.Close(); err != nil {
		return err
	}
	return writeAtomic(offPath, []byte(strconv.FormatInt(offset+int64(len(data)), 10)+"\n"), 0o600)
}

func readOffset(path string) int64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}
