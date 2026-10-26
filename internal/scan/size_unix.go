//go:build unix

package scan

import (
	"io/fs"
	"syscall"
)

// diskUsage returns the allocated size of a file, its identity and its link
// count.
func diskUsage(info fs.FileInfo) (n int64, id fileID, nlink uint64) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return info.Size(), fileID{}, 1
	}
	return int64(st.Blocks) * 512, fileID{uint64(st.Dev), uint64(st.Ino)}, uint64(st.Nlink)
}
