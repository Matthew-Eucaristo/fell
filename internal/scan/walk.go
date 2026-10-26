package scan

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// maxWalk bounds the source walk so a huge untracked tree cannot stall a
// scan; artifacts are skipped, not counted.
const maxWalk = 200000

// lightWalk finds artifact directories, precious files and the newest
// change, without descending into artifacts or nested worktrees.
func lightWalk(w *Worktree) {
	nested := map[string]bool{}
	for _, n := range w.Nested {
		nested[n] = true
	}
	seen := 0
	_ = filepath.WalkDir(w.Path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() && path != w.Path {
				return fs.SkipDir
			}
			return nil
		}
		if seen++; seen > maxWalk {
			w.walkTruncated = true
			return fs.SkipAll
		}
		if path == w.Path {
			return nil
		}
		rel := filepath.ToSlash(path[len(w.Path)+1:])
		if d.IsDir() {
			if d.Name() == ".git" || nested[path] {
				return fs.SkipDir
			}
			if holdsRepo(path) {
				w.Nested = append(w.Nested, path) // another repo or worktree inside
				return fs.SkipDir
			}
			if rule := MatchArtifact(path); rule != nil {
				ign := w.Kind == KindWorktree && isIgnored(w.ignored, rel, true)
				w.Artifacts = append(w.Artifacts, &Artifact{Rel: rel, Path: path, Rule: rule, Ignored: ign, Size: Size{Allocated: -1}})
				return fs.SkipDir
			}
			return nil
		}
		ign := isIgnored(w.ignored, rel, false)
		if isPrecious(d.Name()) && (ign || w.Kind == KindOrphan) {
			p := Precious{Rel: rel}
			if info, err := d.Info(); err == nil {
				p.Bytes = info.Size()
				if info.Mode()&fs.ModeSymlink != 0 {
					p.Link, _ = os.Readlink(path)
				} else {
					p.AtRisk = info.Mode().IsRegular()
				}
			}
			w.Precious = append(w.Precious, p)
		}
		if !ign && !strings.HasPrefix(rel, ".git/") {
			if info, err := d.Info(); err == nil && info.ModTime().After(w.newestChange) {
				w.newestChange = info.ModTime()
			}
		}
		return nil
	})
}

// holdsRepo reports whether dir is another repository or worktree (not a
// submodule, which belongs to this checkout).
func holdsRepo(dir string) bool {
	dotgit := filepath.Join(dir, ".git")
	fi, err := os.Lstat(dotgit)
	if err != nil {
		return false
	}
	if fi.IsDir() {
		return true
	}
	gd := readGitFile(dotgit)
	return gd != "" && filepath.Base(filepath.Dir(gd)) != "modules" && !strings.Contains(gd, string(filepath.Separator)+"modules"+string(filepath.Separator))
}
