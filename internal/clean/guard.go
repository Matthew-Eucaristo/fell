package clean

import (
	"errors"
	"fmt"
	"github.com/Matthew-Eucaristo/fell/internal/pathx"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Matthew-Eucaristo/fell/internal/gitx"
	"github.com/Matthew-Eucaristo/fell/internal/scan"
)

// These checks run on freshly read evidence immediately before every
// removal, independently of how the target was chosen, so a stale or
// tampered plan cannot widen what is removed.

// CheckArtifact allows moving an artifact out of a worktree only if:
//   - w is a linked worktree with its checkout present (never a main checkout),
//   - the artifact lies strictly inside the checkout with no symlink on the way,
//   - its name and markers still match the catalog,
//   - git ignores it and tracks nothing inside it,
//   - it holds no .git of its own.
func CheckArtifact(w *scan.Worktree, path string) error {
	if w.Kind != scan.KindWorktree || w.Admin == "" {
		return errors.New("not a linked worktree with a checkout")
	}
	if !filepath.IsAbs(path) || !within(w.Path, path) {
		return fmt.Errorf("%s is not inside the worktree", path)
	}
	if err := realDirBelow(w.Path, path); err != nil {
		return err
	}
	if scan.MatchArtifact(path) == nil {
		return fmt.Errorf("%s is not a known rebuildable directory here", path)
	}
	for _, n := range w.Nested {
		if pathx.SameOrWithin(path, n) {
			return fmt.Errorf("%s contains the worktree or repository %s; remove it by hand", path, n)
		}
	}
	if g := findGit(path); g != "" {
		return fmt.Errorf("%s contains a repository (%s); remove it by hand", path, g)
	}
	rel := filepath.ToSlash(path[len(w.Path)+1:])
	if _, err := gitx.Read(w.Path, "check-ignore", "-q", "--", rel); err != nil {
		return fmt.Errorf("%s is not ignored by git; not treating it as generated", rel)
	}
	out, err := gitx.Read(w.Path, "ls-files", "-z", "--", rel)
	if err != nil {
		return err
	}
	if len(out) > 0 {
		f, _, _ := strings.Cut(string(out), "\x00")
		return fmt.Errorf("%s contains files tracked by git (%s); remove it by hand", rel, f)
	}
	return nil
}

// CheckRetire validates the target of a retire against git's own records.
func CheckRetire(w *scan.Worktree) error {
	home, _ := os.UserHomeDir()
	if !filepath.IsAbs(w.Path) {
		return errors.New("path must be absolute")
	}
	switch w.Kind {
	case scan.KindWorktree:
		if err := checkCheckout(w.Path, home); err != nil {
			return err
		}
		if w.MainPath != "" && pathx.SameOrWithin(w.Path, w.MainPath) {
			return errors.New("refusing: this is, or contains, the main checkout")
		}
		if within(w.Path, w.CommonDir) {
			return errors.New("refusing: the repository's git directory is inside it")
		}
		if err := checkAdmin(w); err != nil {
			return err
		}
		if got := readGitFile(filepath.Join(w.Path, ".git")); !pathx.Same(got, w.Admin) {
			return fmt.Errorf("%s/.git does not point at git's record %s; run `git worktree repair`", w.Path, w.Admin)
		}
	case scan.KindPrunable:
		if err := checkAdmin(w); err != nil {
			return err
		}
		if _, err := os.Lstat(w.Path); err == nil {
			return errors.New("the checkout exists again; rescan")
		}
	case scan.KindOrphan:
		if err := checkCheckout(w.Path, home); err != nil {
			return err
		}
		gd := readGitFile(filepath.Join(w.Path, ".git"))
		if gd == "" || filepath.Base(filepath.Dir(gd)) != "worktrees" {
			return errors.New("not a worktree checkout")
		}
		if _, err := os.Lstat(gd); err == nil {
			return errors.New("git knows this worktree again; rescan")
		}
	default:
		return errors.New("unknown item kind")
	}
	return nil
}

// checkCheckout: a real directory, not a symlink, not home or an ancestor of it.
func checkCheckout(path, home string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing symlink %s", path)
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}
	if path == "/" || filepath.Dir(path) == "/" || (home != "" && pathx.SameOrWithin(path, home)) {
		return fmt.Errorf("refusing broad path %s", path)
	}
	return nil
}

// checkAdmin: the admin dir lives in <common>/worktrees/ and names this checkout.
func checkAdmin(w *scan.Worktree) error {
	if w.Admin == "" || filepath.Dir(w.Admin) != filepath.Join(w.CommonDir, "worktrees") {
		return errors.New("git's record for this worktree is not where expected")
	}
	b, err := os.ReadFile(filepath.Join(w.Admin, "gitdir"))
	if err != nil {
		return err
	}
	if !pathx.Same(realOrClean(filepath.Dir(strings.TrimSpace(string(b)))), w.Path) {
		return errors.New("git's record points at a different checkout")
	}
	return nil
}

// realDirBelow checks that target is a real directory and that no path
// component between root and target is a symlink.
func realDirBelow(root, target string) error {
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("%s is not below %s", target, root)
	}
	p := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		p = filepath.Join(p, part)
		fi, err := os.Lstat(p)
		if err != nil {
			return err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing: %s is a symlink", p)
		}
		if !fi.IsDir() {
			return fmt.Errorf("%s is not a directory", p)
		}
	}
	return nil
}

func within(dir, path string) bool { return pathx.Within(dir, path) }

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
	return realOrClean(gd)
}

func realOrClean(p string) string { return pathx.Real(p) }

// findGit returns the first .git found below dir (never following
// symlinks), or "": installs from git sources (pip -e, composer
// --prefer-source, mix git deps) and stray clones are someone's work.
func findGit(dir string) string {
	found := ""
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Name() == ".git" {
			found = p
			return fs.SkipAll
		}
		return nil
	})
	return found
}
