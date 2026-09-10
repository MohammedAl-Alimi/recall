//go:build !unix

package scan

import "os"

// inodeOf is unavailable on this platform; size and mtime still key the cache.
func inodeOf(fi os.FileInfo) uint64 { return 0 }
