//go:build unix

package scan

import (
	"os"
	"syscall"
)

// inodeOf returns the inode number of a file, or 0 when unavailable.
func inodeOf(fi os.FileInfo) uint64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && st != nil {
		return uint64(st.Ino)
	}
	return 0
}
