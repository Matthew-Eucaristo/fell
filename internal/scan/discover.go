package scan

import (
	"github.com/Matthew-Eucaristo/fell/internal/agents"
	"github.com/Matthew-Eucaristo/fell/internal/pathx"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// skipDirs are never descended into while looking for repositories: they
// are large and never hold worktrees.
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, ".venv": true, "venv": true, "__pycache__": true,
	".next": true, ".nuxt": true, ".turbo": true, ".gradle": true, "Pods": true,
	".dart_tool": true, "DerivedData": true, ".build": true, "target": true,
	".cache": true, ".Trash": true, "Library": true,
}

// found collects what the filesystem walk saw.
type found struct {
	commonDirs map[string]bool // real paths of git common dirs
	orphans    map[string]bool // dirs whose .git file points at a missing worktree admin dir
}

// walkRoot looks for repositories and worktrees under root, to maxDepth
// directory levels.
func (f *found) walkRoot(root string, maxDepth int) {
	root = filepath.Clean(root)
	base := strings.Count(root, string(filepath.Separator))
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() && path != root {
				return fs.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		name := d.Name()
		if path != root && (skipDirs[name] || agents.SkipDir(name)) {
			return fs.SkipDir
		}
		if strings.Count(path, string(filepath.Separator))-base > maxDepth {
			return fs.SkipDir
		}
		f.inspect(path)
		return nil
	})
}

// inspect records dir if it is a repository or a worktree checkout.
func (f *found) inspect(dir string) {
	dotgit := filepath.Join(dir, ".git")
	fi, err := os.Lstat(dotgit)
	if err != nil {
		return
	}
	switch {
	case fi.IsDir():
		if c, err := filepath.EvalSymlinks(dotgit); err == nil {
			f.commonDirs[c] = true
		}
	case fi.Mode().IsRegular():
		admin := readGitFile(dotgit)
		if admin == "" || !isWorktreeAdmin(admin) {
			return // submodule or something else
		}
		if common := commonDirOf(admin); common != "" {
			f.commonDirs[common] = true
			return
		}
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			f.orphans[real] = true
		}
	}
}

// readGitFile returns the absolute gitdir a ".git" file points to.
func readGitFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil || len(b) > 4096 {
		return ""
	}
	line, _, _ := strings.Cut(string(b), "\n")
	gd, ok := strings.CutPrefix(strings.TrimSpace(line), "gitdir:")
	if !ok {
		return ""
	}
	gd = strings.TrimSpace(gd)
	if !filepath.IsAbs(gd) {
		gd = filepath.Join(filepath.Dir(path), gd)
	}
	return filepath.Clean(gd)
}

// isWorktreeAdmin reports whether gitdir looks like <common>/worktrees/<id>
// (as opposed to <common>/modules/<name> for submodules).
func isWorktreeAdmin(gitdir string) bool {
	return filepath.Base(filepath.Dir(gitdir)) == "worktrees"
}

// commonDirOf resolves a worktree admin dir to its common dir, or "" if the
// admin dir is gone.
func commonDirOf(admin string) string {
	b, err := os.ReadFile(filepath.Join(admin, "commondir"))
	if err != nil {
		return ""
	}
	c := strings.TrimSpace(string(b))
	if !filepath.IsAbs(c) {
		c = filepath.Join(admin, c)
	}
	real, err := filepath.EvalSymlinks(c)
	if err != nil {
		return ""
	}
	return real
}

// adminDirs maps each worktree's checkout path (from <admin>/gitdir, which
// names <checkout>/.git) to its admin dir, for every entry of a common dir.
func adminDirs(common string) map[string]string {
	m := map[string]string{}
	entries, err := os.ReadDir(filepath.Join(common, "worktrees"))
	if err != nil {
		return m
	}
	for _, e := range entries {
		admin := filepath.Join(common, "worktrees", e.Name())
		b, err := os.ReadFile(filepath.Join(admin, "gitdir"))
		if err != nil {
			continue
		}
		checkout := filepath.Dir(filepath.Clean(strings.TrimSpace(string(b))))
		m[pathx.Real(checkout)] = admin
	}
	return m
}

func realOrClean(p string) string { return pathx.Real(p) }

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
