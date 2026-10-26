package scan

import (
	"io/fs"
	"path/filepath"
)

// Size is disk usage measured like `du -s`: allocated blocks, symlinks never
// followed, each hard-linked inode counted once. Shared is the part of
// Allocated that removing the measured paths would not free: hard-linked
// files with links outside (a pnpm store, another worktree) and, on APFS,
// blocks shared with clones or snapshots. Exclusive is therefore a lower
// bound on macOS (two clones inside the same tree both count as shared) and
// an upper bound on filesystems with reflinks fell cannot see; freed space
// after a removal is measured, never assumed.
type Size struct {
	Allocated int64 `json:"allocated"`
	Shared    int64 `json:"shared"`
	Files     int64 `json:"files"`
}

// Exclusive is what removing the measured paths can free.
func (s Size) Exclusive() int64 { return s.Allocated - s.Shared }

// Add sums two sizes.
func (s Size) Add(o Size) Size {
	return Size{s.Allocated + o.Allocated, s.Shared + o.Shared, s.Files + o.Files}
}

type fileID struct{ dev, ino uint64 }

type link struct {
	nlink, seen uint64
	bytes       int64
	bucket      *Size
}

// sizer measures several trees that belong together (one worktree and its
// artifacts), so a file hard-linked between them counts once and only links
// that leave the whole set count as shared.
type sizer struct {
	links map[fileID]*link
	dev   uint64
	init  bool
}

func newSizer() *sizer { return &sizer{links: map[fileID]*link{}} }

// add accounts one entry into bucket. It returns false for directories on
// another filesystem, which callers skip.
func (z *sizer) add(path string, info fs.FileInfo, bucket *Size) bool {
	n, id, nlink := diskUsage(info)
	if !z.init {
		z.dev, z.init = id.dev, true
	}
	if info.IsDir() && id.dev != z.dev && id.dev != 0 {
		return false
	}
	if !info.IsDir() && nlink > 1 {
		if l := z.links[id]; l != nil {
			l.seen++
			return true
		}
		z.links[id] = &link{nlink: nlink, seen: 1, bytes: n, bucket: bucket}
	}
	bucket.Allocated += n
	if !info.IsDir() {
		bucket.Files++
	}
	if info.Mode().IsRegular() && nlink <= 1 && n > 0 {
		if private, ok := privateSize(path); ok && private < n {
			bucket.Shared += n - private
		}
	}
	return true
}

// walk measures everything under path into bucket and reports whether a
// .git (another repository or worktree) lies below it.
func (z *sizer) walk(path string, bucket *Size) (repo bool) {
	_ = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entries are skipped, not fatal
		}
		if d.Name() == ".git" && p != path {
			repo = true
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if !z.add(p, info, bucket) && d.IsDir() {
			return fs.SkipDir
		}
		return nil
	})
	return repo
}

// finish moves bytes of hard-linked files with links outside the measured
// set into their bucket's Shared.
func (z *sizer) finish() {
	for _, l := range z.links {
		if l.seen < l.nlink {
			l.bucket.Shared += l.bytes
		}
	}
}

// DirSize measures one tree on its own.
func DirSize(path string) Size {
	z := newSizer()
	var s Size
	z.walk(path, &s)
	z.finish()
	return s
}
