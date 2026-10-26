package clean

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Matthew-Eucaristo/fell/internal/gitx"
	"github.com/Matthew-Eucaristo/fell/internal/scan"
)

// Rescue describes what retire saved next to the trashed worktree.
type Rescue struct {
	Folder          string   `json:"folder"`                   // its name in the trash
	BundleCommits   int      `json:"bundle_commits"`           // commits in commits.bundle (0 = no bundle needed)
	Patch           bool     `json:"uncommitted_patch"`        // uncommitted.patch was written
	Precious        []string `json:"precious_saved,omitempty"` // relative paths copied to precious/
	PreciousSkipped []string `json:"precious_not_copied,omitempty"`
	Untracked       int      `json:"untracked_files"` // listed in untracked.txt; the files stay in the trashed checkout

	refs *bundleRefs
}

// Limits keep the rescue folder small; anything larger is still in the
// trashed checkout and is listed as not copied.
const (
	maxPreciousFile  = 50 << 20
	maxPreciousTotal = 200 << 20
)

// bundleRefs names the commits a rescue bundle starts from: HEAD, the
// branch tip, and every tip only git's record of this worktree keeps
// (reflog entries, refs/worktree/*, refs/bisect/*).
type bundleRefs struct {
	names []string // refs/fell/head, refs/fell/branch, refs/fell/tip-N
	shas  []string
	kept  []string // tips of other branches, tags and remotes, excluded
}

func planBundle(w *scan.Worktree) (*bundleRefs, int, error) {
	tips, err := scan.Tips(w.Admin)
	if err != nil {
		return nil, 0, err
	}
	b := &bundleRefs{}
	seen := map[string]bool{}
	addTip := func(name, sha string) {
		if !seen[sha] {
			seen[sha] = true
			b.names, b.shas = append(b.names, name), append(b.shas, sha)
		}
	}
	addTip("refs/fell/head", tips[0])
	if w.Branch != "" {
		out, err := gitx.ReadGitDir(w.CommonDir, "rev-parse", "--verify", "-q", "refs/heads/"+w.Branch)
		if err == nil {
			addTip("refs/fell/branch", strings.TrimSpace(string(out)))
		}
	}
	for i, t := range tips[1:] {
		addTip(fmt.Sprintf("refs/fell/tip-%d", i+1), t)
	}
	if b.kept, err = scan.KeptBy(w.CommonDir, w.Branch); err != nil {
		return nil, 0, err
	}
	out, err := gitx.ReadGitDirStdin(w.Admin, scan.RevList(b.shas, b.kept), "rev-list", "--count", "--stdin")
	if err != nil {
		return nil, 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	return b, n, err
}

// writeBundle bundles the planned commits without writing a single ref into
// the user's repository: a throwaway bare repository in fell's staging dir
// borrows the objects read-only (objects/info/alternates) and holds the refs.
func writeBundle(w *scan.Worktree, b *bundleRefs, scratch, file string) error {
	repo := filepath.Join(scratch, "bundle-repo.git")
	if _, err := gitx.Write("", "init", "-q", "--bare", repo); err != nil {
		return err
	}
	alt := filepath.Join(w.CommonDir, "objects") + "\n"
	if err := os.WriteFile(filepath.Join(repo, "objects", "info", "alternates"), []byte(alt), 0o600); err != nil {
		return err
	}
	for i, name := range b.names {
		if _, err := gitx.WriteGitDir(repo, "update-ref", name, b.shas[i]); err != nil {
			return err
		}
	}
	in := strings.Join(b.names, "\n") + "\n" + scan.RevList(nil, b.kept)
	if _, err := gitx.WriteGitDirStdin(repo, in, "bundle", "create", "-q", file, "--stdin"); err != nil {
		return err
	}
	if _, err := gitx.ReadGitDir(repo, "bundle", "verify", "-q", file); err != nil {
		return fmt.Errorf("bundle did not verify: %w", err)
	}
	return nil
}

// planRescue works out what retire will save, without writing anything. An
// error means fell cannot tell which commits would be lost; retire stops.
func planRescue(w *scan.Worktree) (*Rescue, error) {
	r := &Rescue{Folder: rescueName(w)}
	if w.Admin != "" && w.Head != "" {
		b, n, err := planBundle(w)
		if err != nil {
			return nil, fmt.Errorf("cannot work out which commits to save: %w", err)
		}
		r.BundleCommits, r.refs = n, b
	}
	if w.Kind == scan.KindWorktree {
		r.Patch = w.Dirty.Staged+w.Dirty.Modified+w.Dirty.Conflicts > 0
		r.Untracked = w.Dirty.Untracked
	}
	var total int64
	for _, p := range w.Precious {
		if !p.AtRisk {
			continue
		}
		if p.Bytes > maxPreciousFile || total+p.Bytes > maxPreciousTotal {
			r.PreciousSkipped = append(r.PreciousSkipped, p.Rel)
			continue
		}
		total += p.Bytes
		r.Precious = append(r.Precious, p.Rel)
	}
	return r, nil
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func rescueName(w *scan.Worktree) string {
	clean := func(s string) string { return strings.Trim(unsafeName.ReplaceAllString(s, "-"), "-") }
	return fmt.Sprintf("fell-rescue-%s-%s-%s", clean(w.Repo), clean(filepath.Base(w.Path)), time.Now().Format("20060102-150405"))
}

// writeRescue assembles the rescue folder in a staging directory and
// returns its path. On error nothing of the user's has changed.
func writeRescue(w *scan.Worktree, plan *Rescue) (string, *Rescue, error) {
	parent, err := os.MkdirTemp(stagingParent(), ".fell-staging-")
	if err != nil {
		return "", nil, err
	}
	dir := filepath.Join(parent, plan.Folder)
	fail := func(err error) (string, *Rescue, error) {
		os.RemoveAll(parent) // only fell's own staging copies
		return "", nil, err
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		return fail(err)
	}
	r := *plan
	r.Precious, r.PreciousSkipped = nil, append([]string(nil), plan.PreciousSkipped...)

	if r.BundleCommits > 0 {
		if err := writeBundle(w, r.refs, parent, filepath.Join(dir, "commits.bundle")); err != nil {
			return fail(err)
		}
		os.RemoveAll(filepath.Join(parent, "bundle-repo.git")) // fell's own throwaway refs
	}
	if r.Patch {
		out, err := gitx.Read(w.Path, "diff", "HEAD", "--binary")
		if err != nil {
			return fail(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "uncommitted.patch"), out, 0o600); err != nil {
			return fail(err)
		}
	}
	if r.Untracked > 0 {
		out, err := gitx.Read(w.Path, "ls-files", "--others", "--exclude-standard", "-z")
		if err == nil {
			list := strings.ReplaceAll(strings.TrimRight(string(out), "\x00"), "\x00", "\n") + "\n"
			if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte(list), 0o600); err != nil {
				return fail(err)
			}
		}
	}
	for _, rel := range plan.Precious {
		if err := copyRegular(filepath.Join(w.Path, filepath.FromSlash(rel)), filepath.Join(dir, "precious", filepath.FromSlash(rel))); err != nil {
			r.PreciousSkipped = append(r.PreciousSkipped, rel+" ("+err.Error()+")")
			continue
		}
		r.Precious = append(r.Precious, rel)
	}
	if err := writeManifest(dir, w, &r); err != nil {
		return fail(err)
	}
	return dir, &r, nil
}

// cleanupStaging removes the now-empty staging parent after its only
// child moved to the trash. os.Remove refuses non-empty directories.
func cleanupStaging(staged string) {
	os.Remove(filepath.Dir(staged))
}

// copyRegular copies one regular file, never following symlinks.
func copyRegular(src, dst string) error {
	fi, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("not a regular file")
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

func writeManifest(dir string, w *scan.Worktree, r *Rescue) error {
	m := map[string]any{
		"tool":           "fell",
		"schema_version": 1,
		"created_at":     time.Now().UTC().Format(time.RFC3339),
		"kind":           w.Kind,
		"worktree":       w.Path,
		"main_checkout":  w.MainPath,
		"git_common_dir": w.CommonDir,
		"owner":          w.Owner.ID,
		"head":           w.Head,
		"branch":         w.Branch,
		"detached":       w.Detached,
		"rescue":         r,
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "RESTORE.md"), []byte(restoreDoc(w, r)), 0o600)
}

func restoreDoc(w *scan.Worktree, r *Rescue) string {
	var b strings.Builder
	name := filepath.Base(w.Path)
	repo := w.MainPath
	if repo == "" {
		repo = w.CommonDir
	}
	fmt.Fprintf(&b, "# Restoring %s\n\n", name)
	fmt.Fprintf(&b, "fell retired the worktree `%s` of `%s`", w.Path, repo)
	if w.Branch != "" {
		fmt.Fprintf(&b, " (branch `%s`, kept)", w.Branch)
	} else if w.Head != "" {
		fmt.Fprintf(&b, " (detached at `%s`)", w.Head)
	}
	b.WriteString(".\n\n")
	if w.Kind != scan.KindPrunable {
		fmt.Fprintf(&b, "- The whole checkout, including untracked and ignored files, is in the %s as `%s`.\n", TrashName, name)
		b.WriteString("  If git's record was not pruned, putting it back makes it a working worktree again.\n")
	}
	if r.BundleCommits > 0 {
		safe := unsafeName.ReplaceAllString(name, "-")
		fmt.Fprintf(&b, "- `commits.bundle` holds %d commit(s) that no remote, tag or other branch had (from HEAD, the branch, reflog entries and per-worktree refs). Recover them with:\n\n", r.BundleCommits)
		fmt.Fprintf(&b, "      git -C %q fetch \"$PWD/commits.bundle\" 'refs/fell/*:refs/heads/fell-rescue/%s/*'\n", repo, safe)
		fmt.Fprintf(&b, "      git -C %q worktree add <new-path> fell-rescue/%s/head\n\n", repo, safe)
		b.WriteString("  (run the fetch from inside this folder)\n")
	} else if w.Head != "" {
		fmt.Fprintf(&b, "- Every commit was already on a remote, tag or branch; HEAD was `%s`.\n", w.Head)
	}
	if r.Patch {
		b.WriteString("- `uncommitted.patch` has the uncommitted changes to tracked files: `git apply --index uncommitted.patch` in a checkout of the same commit.\n")
	}
	if r.Untracked > 0 {
		b.WriteString("- `untracked.txt` lists untracked files; they are in the trashed checkout.\n")
	}
	if len(r.Precious) > 0 {
		b.WriteString("- `precious/` has copies of git-ignored secrets and local data: " + strings.Join(r.Precious, ", ") + ".\n")
	}
	if len(r.PreciousSkipped) > 0 {
		b.WriteString("- Not copied (still in the trashed checkout): " + strings.Join(r.PreciousSkipped, ", ") + ".\n")
	}
	b.WriteString("\nEmptying the " + TrashName + " deletes all of this for good.\n")
	return b.String()
}
