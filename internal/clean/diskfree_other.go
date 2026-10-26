//go:build !unix

package clean

func freeBytes(string) int64 { return -1 }
