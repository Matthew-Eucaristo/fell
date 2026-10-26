//go:build !unix

package scan

import "io/fs"

func diskUsage(info fs.FileInfo) (int64, fileID, uint64) {
	return info.Size(), fileID{}, 1
}
