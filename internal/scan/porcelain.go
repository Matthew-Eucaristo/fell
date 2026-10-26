package scan

import (
	"strings"
)

// entry is one record of `git worktree list --porcelain -z`.
type entry struct {
	Path           string
	Head           string
	Branch         string // short name, "" when detached
	Detached       bool
	Bare           bool
	Locked         bool
	LockReason     string
	Prunable       bool
	PrunableReason string
}

// parsePorcelain parses `git worktree list --porcelain -z`: attributes end
// in NUL and records end in an extra NUL. -z keeps paths and lock reasons
// with newlines intact.
func parsePorcelain(out []byte) []entry {
	var list []entry
	var cur *entry
	for _, attr := range strings.Split(string(out), "\x00") {
		if attr == "" {
			cur = nil
			continue
		}
		key, val, _ := strings.Cut(attr, " ")
		if key == "worktree" {
			list = append(list, entry{Path: val})
			cur = &list[len(list)-1]
			continue
		}
		if cur == nil {
			continue
		}
		switch key {
		case "HEAD":
			cur.Head = val
		case "branch":
			cur.Branch = strings.TrimPrefix(val, "refs/heads/")
		case "detached":
			cur.Detached = true
		case "bare":
			cur.Bare = true
		case "locked":
			cur.Locked, cur.LockReason = true, val
		case "prunable":
			cur.Prunable, cur.PrunableReason = true, val
		}
	}
	return list
}
