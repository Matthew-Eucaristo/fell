package scan

import (
	"errors"
	"github.com/Matthew-Eucaristo/fell/internal/pathx"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/Matthew-Eucaristo/fell/internal/agents"
	"github.com/Matthew-Eucaristo/fell/internal/gitx"
	"github.com/Matthew-Eucaristo/fell/internal/procs"
)

// Options says where to look.
type Options struct {
	Roots    []string // real absolute paths; walked MaxDepth levels deep
	Agents   bool     // also look in known agent locations and Claude Desktop's registry
	MaxDepth int      // 0 means 6
	Env      agents.Env
}

// Result is a scan. Worktrees are sorted by owner then path.
type Result struct {
	Worktrees []*Worktree
	Registry  *agents.Registry
	Procs     *procs.Table
	Warnings  []string
}

// repo is per-common-dir data shared by its worktrees.
type repo struct {
	common     string
	main       string
	name       string
	hasRemotes bool
	entries    []entry
	admins     map[string]string
}

// Scan discovers worktrees and gathers evidence for each. It does not
// measure sizes; call Measure for that.
func Scan(opts Options) (*Result, error) {
	if !gitx.Available() {
		return nil, errNoGit
	}
	if opts.MaxDepth == 0 {
		opts.MaxDepth = 6
	}
	res := &Result{Registry: &agents.Registry{}}
	f := &found{commonDirs: map[string]bool{}, orphans: map[string]bool{}}
	for _, r := range opts.Roots {
		f.walkRoot(r, opts.MaxDepth)
	}
	if opts.Agents {
		res.Registry = agents.LoadRegistry(registryPath(opts.Env))
		if !res.Registry.Supported() {
			res.Warnings = append(res.Warnings, "Claude Desktop's worktree registry could not be read ("+res.Registry.Problem+"); Claude worktrees are treated as app-tracked until fell understands it")
		}
		extra := opts.Env.Roots()
		extra = append(extra, res.Registry.Roots...)
		for _, rec := range res.Registry.Entries {
			if rec.BaseRepo != "" {
				f.inspect(rec.BaseRepo)
			}
			extra = append(extra, filepath.Dir(rec.Path))
		}
		for _, r := range extra {
			if fi, err := os.Stat(r); err == nil && fi.IsDir() {
				f.walkRoot(realOrClean(r), 4)
			}
		}
	}
	res.Procs = Snapshot()

	repos := map[string]*repo{}
	for _, c := range sortedKeys(f.commonDirs) {
		if rp := loadRepo(c); rp != nil {
			repos[c] = rp
		}
	}

	var items []*Worktree
	seen := map[string]bool{}
	for _, c := range sortedKeys(f.commonDirs) {
		rp := repos[c]
		if rp == nil {
			continue
		}
		for i, e := range rp.entries {
			if i == 0 || e.Bare {
				continue // the main worktree is never a candidate
			}
			p := realOrClean(e.Path)
			if seen[p] {
				continue
			}
			seen[p] = true
			w := &Worktree{Kind: KindWorktree, Path: p, CommonDir: rp.common, MainPath: rp.main, Repo: rp.name,
				Head: e.Head, Branch: e.Branch, Detached: e.Detached, Locked: e.Locked, LockReason: e.LockReason,
				HasRemotes: rp.hasRemotes, Admin: rp.admins[p], Unpushed: -1, OnlyHere: -1}
			if e.Prunable {
				w.Kind, w.Prunable = KindPrunable, e.PrunableReason
			}
			items = append(items, w)
		}
	}
	for _, o := range sortedKeys(f.orphans) {
		if !seen[o] {
			seen[o] = true
			items = append(items, &Worktree{Kind: KindOrphan, Path: o, Repo: filepath.Base(o), Unpushed: -1, OnlyHere: -1})
		}
	}

	paths := make([]string, len(items))
	for i, w := range items {
		paths[i] = w.Path
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.GOMAXPROCS(0))
	for _, w := range items {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			gather(w, opts.Env, res.Registry, res.Procs, paths)
		}()
	}
	wg.Wait()

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Owner.ID != items[j].Owner.ID {
			return ownerRank(items[i].Owner.ID) < ownerRank(items[j].Owner.ID)
		}
		return items[i].Path < items[j].Path
	})
	res.Worktrees = items
	return res, nil
}

func ownerRank(id string) int {
	for i, o := range agents.Owners {
		if o.ID == id {
			return i
		}
	}
	return len(agents.Owners)
}

// loadRepo lists a common dir's worktrees.
func loadRepo(common string) *repo {
	out, err := gitx.ReadGitDir(common, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil
	}
	entries := parsePorcelain(out)
	if len(entries) == 0 {
		return nil
	}
	rp := &repo{common: common, entries: entries, admins: adminDirs(common)}
	if !entries[0].Bare {
		rp.main = realOrClean(entries[0].Path)
		rp.name = filepath.Base(rp.main)
	} else {
		rp.name = strings.TrimSuffix(filepath.Base(common), ".git")
		if rp.name == "" || rp.name == "." {
			rp.name = filepath.Base(filepath.Dir(common))
		}
	}
	if out, err := gitx.ReadGitDir(common, "for-each-ref", "--count=1", "--format=x", "refs/remotes"); err == nil {
		rp.hasRemotes = len(strings.TrimSpace(string(out))) > 0
	}
	return rp
}

var errChanged = errors.New("no longer exists in the form it was scanned; rescan")

// Find returns the worktree with this ID or path (absolute or ~-relative).
func (r *Result) Find(ref string) *Worktree {
	for _, w := range r.Worktrees {
		if w.ID == ref {
			return w
		}
	}
	p := ref
	if home, err := os.UserHomeDir(); err == nil {
		if rest, ok := strings.CutPrefix(p, "~/"); ok {
			p = filepath.Join(home, rest)
		}
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = realOrClean(abs)
		for _, w := range r.Worktrees {
			if pathx.Same(w.Path, p) {
				return w
			}
		}
	}
	return nil
}

// Owners lists owner IDs present, in display order.
func (r *Result) Owners() []string {
	var ids []string
	for _, w := range r.Worktrees {
		if !slices.Contains(ids, w.Owner.ID) {
			ids = append(ids, w.Owner.ID)
		}
	}
	return ids
}

// Snapshot reads the process table for liveness; tests swap it so results
// do not depend on which apps run on the test machine.
var Snapshot = procs.Snapshot

// registryPath is swapped in tests.
var registryPath = func(e agents.Env) string { return e.DesktopRegistryPath() }

// Refresh re-reads one item from disk with a fresh process snapshot and
// registry, for re-validation right before acting on it.
func Refresh(old *Worktree, env agents.Env) (*Worktree, error) {
	reg := agents.LoadRegistry(registryPath(env))
	pt := Snapshot()
	var w *Worktree
	switch old.Kind {
	case KindOrphan:
		admin := readGitFile(filepath.Join(old.Path, ".git"))
		if admin == "" || !isWorktreeAdmin(admin) || exists(admin) {
			return nil, errChanged
		}
		w = &Worktree{Kind: KindOrphan, Path: old.Path, Repo: old.Repo, Unpushed: -1, OnlyHere: -1}
	default:
		rp := loadRepo(old.CommonDir)
		if rp == nil {
			return nil, errChanged
		}
		for i, e := range rp.entries {
			if i == 0 || e.Bare || !pathx.Same(realOrClean(e.Path), old.Path) {
				continue
			}
			w = &Worktree{Kind: KindWorktree, Path: old.Path, CommonDir: rp.common, MainPath: rp.main, Repo: rp.name,
				Head: e.Head, Branch: e.Branch, Detached: e.Detached, Locked: e.Locked, LockReason: e.LockReason,
				HasRemotes: rp.hasRemotes, Admin: rp.admins[old.Path], Unpushed: -1, OnlyHere: -1}
			if e.Prunable {
				w.Kind, w.Prunable = KindPrunable, e.PrunableReason
			}
		}
		if w == nil {
			return nil, errChanged
		}
	}
	var all []string
	if rp := loadRepo(w.CommonDir); rp != nil {
		for _, e := range rp.entries {
			all = append(all, realOrClean(e.Path))
		}
	}
	gather(w, env, reg, pt, all)
	return w, nil
}
