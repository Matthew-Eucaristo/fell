package clean

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Matthew-Eucaristo/fell/internal/agents"
	"github.com/Matthew-Eucaristo/fell/internal/scan"
	tr "github.com/Matthew-Eucaristo/fell/internal/testrepo"
)

// fixture is a scan root with one repository, a fake home and a fake trash.
type fixture struct {
	t     *testing.T
	root  string
	repo  string
	trash string
	env   agents.Env
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	base := tr.Dir(t)
	t.Setenv("XDG_DATA_HOME", base) // Linux staging dir stays in the test
	f := &fixture{t: t, root: filepath.Join(base, "dev"), trash: filepath.Join(base, "Trash")}
	os.MkdirAll(f.root, 0o755)
	os.MkdirAll(f.trash, 0o755)
	home := filepath.Join(base, "home")
	os.MkdirAll(home, 0o755)
	f.env = agents.Env{Home: home, TmpDir: base, Getenv: func(string) string { return "" }}
	f.repo = tr.Repo(t, f.root, "app")
	old := moveFunc
	moveFunc = func(p string) error {
		dest := filepath.Join(f.trash, filepath.Base(p))
		for i := 1; ; i++ {
			if _, err := os.Lstat(dest); err != nil {
				break
			}
			dest = filepath.Join(f.trash, filepath.Base(p)+"."+strconv.Itoa(i))
		}
		return os.Rename(p, dest)
	}
	t.Cleanup(func() { moveFunc = old })
	return f
}

func (f *fixture) scan() *scan.Result {
	f.t.Helper()
	r, err := scan.Scan(scan.Options{Roots: []string{f.root}, Agents: true, Env: f.env})
	if err != nil {
		f.t.Fatal(err)
	}
	r.MeasureAll()
	return r
}

func (f *fixture) find(r *scan.Result, path string) *scan.Worktree {
	f.t.Helper()
	w := r.Find(path)
	if w == nil {
		f.t.Fatalf("worktree %s not found", path)
	}
	return w
}

func (f *fixture) opts() Options { return Options{Mode: Trash, Env: f.env} }

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

func (f *fixture) rescueDir() string {
	f.t.Helper()
	m, _ := filepath.Glob(filepath.Join(f.trash, "fell-rescue-*"))
	if len(m) != 1 {
		f.t.Fatalf("want one rescue folder in the trash, got %v", m)
	}
	return m[0]
}

func TestStripKeepsWorktreeAndTrackedFiles(t *testing.T) {
	f := newFixture(t)
	wt := tr.Worktree(t, f.repo, filepath.Join(f.root, "app-wt"), "-b", "feature")
	tr.NodeModules(t, wt)
	// A tracked directory named build next to package.json is not generated output.
	tr.Write(t, wt, "build/keep.txt", "source")
	tr.Git(t, wt, "add", "build")
	tr.Git(t, wt, "commit", "-q", "-m", "build dir")
	// A symlinked node_modules (Claude's worktree.symlinkDirectories) is never followed.
	other := filepath.Join(f.root, "app-wt2")
	other = tr.Worktree(t, f.repo, other, "-b", "two")
	os.Symlink(filepath.Join(wt, "node_modules"), filepath.Join(other, "node_modules"))

	r := f.scan()
	w := f.find(r, wt)
	if len(w.Artifacts) != 2 {
		t.Fatalf("artifacts = %d, want node_modules and build", len(w.Artifacts))
	}
	o := Strip(w, w.Fingerprint, nil, f.opts())
	if o.Status != StatusDone {
		t.Fatalf("strip: %+v", o)
	}
	if exists(filepath.Join(wt, "node_modules")) || !exists(filepath.Join(f.trash, "node_modules")) {
		t.Error("node_modules was not moved to the trash")
	}
	if !exists(filepath.Join(wt, "build", "keep.txt")) {
		t.Error("tracked build/ was removed")
	}
	if !exists(filepath.Join(wt, "package.json")) {
		t.Error("worktree damaged")
	}
	if o.Estimate <= 0 {
		t.Errorf("estimate = %d", o.Estimate)
	}
	ow := f.find(f.scan(), other)
	if len(ow.Artifacts) != 0 {
		t.Errorf("symlinked node_modules treated as an artifact: %+v", ow.Artifacts[0])
	}
}

func TestDryRunChangesNothing(t *testing.T) {
	f := newFixture(t)
	wt := tr.Worktree(t, f.repo, filepath.Join(f.root, "dry"), "--detach")
	tr.NodeModules(t, wt)
	tr.Git(t, wt, "commit", "-q", "--allow-empty", "-m", "only here")
	w := f.find(f.scan(), wt)
	opts := f.opts()
	opts.Mode, opts.Force = DryRun, true
	if o := Strip(w, w.Fingerprint, nil, opts); o.Status != StatusDryRun || o.Estimate <= 0 {
		t.Fatalf("strip dry run: %+v", o)
	}
	o := Retire(w, w.Fingerprint, opts)
	if o.Status != StatusDryRun || o.Rescue == nil || o.Rescue.BundleCommits != 1 {
		t.Fatalf("retire dry run: %+v %+v", o, o.Rescue)
	}
	if !exists(filepath.Join(wt, "node_modules")) || !exists(wt) {
		t.Error("dry run removed something")
	}
	if entries, _ := os.ReadDir(f.trash); len(entries) != 0 {
		t.Errorf("dry run wrote to the trash: %v", entries)
	}
}

func TestRetireCleanWorktree(t *testing.T) {
	f := newFixture(t)
	wt := tr.Worktree(t, f.repo, filepath.Join(f.root, "clean"), "-b", "clean")
	tr.NodeModules(t, wt)
	tr.Write(t, wt, ".env", "SECRET=1\n")
	r := f.scan()
	w := f.find(r, wt)
	if w.Safety.Level != scan.Caution || !w.Safety.Has(scan.ReasonPrecious) {
		t.Fatalf("safety = %+v", w.Safety)
	}
	o := Retire(w, w.Fingerprint, f.opts())
	if o.Status != StatusDone || !o.Pruned {
		t.Fatalf("retire: %+v", o)
	}
	if exists(wt) || !exists(filepath.Join(f.trash, "clean", "node_modules")) {
		t.Error("checkout not moved to the trash whole")
	}
	if out := tr.Git(t, f.repo, "worktree", "list"); strings.Contains(out, "clean") {
		t.Errorf("git still lists it:\n%s", out)
	}
	if tr.Git(t, f.repo, "branch", "--list", "clean") == "" {
		t.Error("branch was deleted")
	}
	rescue := f.rescueDir()
	b, err := os.ReadFile(filepath.Join(rescue, "precious", ".env"))
	if err != nil || string(b) != "SECRET=1\n" {
		t.Errorf("precious .env not copied: %q %v", b, err)
	}
	for _, name := range []string{"manifest.json", "RESTORE.md"} {
		if !exists(filepath.Join(rescue, name)) {
			t.Errorf("%s missing", name)
		}
	}
	if exists(filepath.Join(rescue, "commits.bundle")) {
		t.Error("bundle written although every commit is on a branch")
	}
}

func TestRetireDetachedCommitsNeedForceAndAreBundled(t *testing.T) {
	f := newFixture(t)
	wt := tr.Worktree(t, f.repo, filepath.Join(f.root, "agent"), "--detach")
	tr.Write(t, wt, "feature.txt", "work\n")
	tr.Git(t, wt, "add", ".")
	tr.Git(t, wt, "commit", "-q", "-m", "agent work")
	head := tr.Git(t, wt, "rev-parse", "HEAD")

	w := f.find(f.scan(), wt)
	if w.OnlyHere != 1 || w.Unpushed != 1 || w.Safety.Level != scan.Protected {
		t.Fatalf("only_here=%d unpushed=%d level=%s", w.OnlyHere, w.Unpushed, w.Safety.Level)
	}
	if o := Retire(w, w.Fingerprint, f.opts()); o.Status != StatusRefused || !strings.Contains(o.Error, scan.ReasonOnlyHere) {
		t.Fatalf("retire without --force: %+v", o)
	}
	if !exists(wt) {
		t.Fatal("refused retire removed the worktree")
	}
	opts := f.opts()
	opts.Force = true
	o := Retire(w, w.Fingerprint, opts)
	if o.Status != StatusDone || o.Rescue.BundleCommits != 1 {
		t.Fatalf("retire --force: %+v", o)
	}
	// The commit is unreachable in the repo now, but the bundle restores it.
	bundle := filepath.Join(f.rescueDir(), "commits.bundle")
	tr.Git(t, f.repo, "fetch", "-q", bundle, "refs/fell/head:refs/heads/rescued")
	if got := tr.Git(t, f.repo, "rev-parse", "rescued"); got != head {
		t.Errorf("bundle restored %s, want %s", got, head)
	}
}

func TestRetireDirtySavesPatch(t *testing.T) {
	f := newFixture(t)
	wt := tr.Worktree(t, f.repo, filepath.Join(f.root, "dirty"), "-b", "dirty")
	tr.Write(t, wt, "README.md", "changed\n")
	tr.Write(t, wt, "new.txt", "untracked\n")
	w := f.find(f.scan(), wt)
	if w.Dirty.Modified != 1 || w.Dirty.Untracked != 1 {
		t.Fatalf("dirty = %+v", w.Dirty)
	}
	if o := Retire(w, w.Fingerprint, f.opts()); o.Status != StatusRefused {
		t.Fatalf("dirty retire without --force: %+v", o)
	}
	opts := f.opts()
	opts.Force = true
	if o := Retire(w, w.Fingerprint, opts); o.Status != StatusDone {
		t.Fatalf("retire --force: %+v", o)
	}
	patch, err := os.ReadFile(filepath.Join(f.rescueDir(), "uncommitted.patch"))
	if err != nil || !strings.Contains(string(patch), "+changed") {
		t.Errorf("patch = %q %v", patch, err)
	}
	if b, _ := os.ReadFile(filepath.Join(f.rescueDir(), "untracked.txt")); !strings.Contains(string(b), "new.txt") {
		t.Errorf("untracked.txt = %q", b)
	}
	if !exists(filepath.Join(f.trash, "dirty", "new.txt")) {
		t.Error("untracked file not in the trashed checkout")
	}
}

func TestStaleFingerprintIsRefused(t *testing.T) {
	f := newFixture(t)
	wt := tr.Worktree(t, f.repo, filepath.Join(f.root, "stale"), "-b", "stale")
	tr.NodeModules(t, wt)
	w := f.find(f.scan(), wt)
	tr.Write(t, wt, "README.md", "edited after the scan\n")
	opts := f.opts()
	opts.Force = true
	for _, o := range []*Outcome{Retire(w, w.Fingerprint, opts), Strip(w, w.Fingerprint, nil, opts)} {
		if o.Status != StatusRefused || !strings.Contains(o.Error, "fingerprint") {
			t.Errorf("%s: %+v", o.Action, o)
		}
	}
	if !exists(filepath.Join(wt, "node_modules")) {
		t.Error("stale plan removed something")
	}
}

func TestLockedNeedsUnlockAndForeignLockIsKept(t *testing.T) {
	f := newFixture(t)
	wt := tr.Worktree(t, f.repo, filepath.Join(f.root, "locked"), "-b", "locked")
	tr.Git(t, f.repo, "worktree", "lock", "--reason", "on a USB disk", wt)
	w := f.find(f.scan(), wt)
	if !w.Locked || w.OwnerLive {
		t.Fatalf("locked=%v live=%v", w.Locked, w.OwnerLive)
	}
	opts := f.opts()
	opts.Force = true
	if o := Retire(w, w.Fingerprint, opts); o.Status != StatusRefused || !strings.Contains(o.Error, "--unlock") {
		t.Fatalf("retire without --unlock: %+v", o)
	}
	opts.Unlock = true
	if o := Retire(w, w.Fingerprint, opts); o.Status != StatusDone || !o.Pruned {
		t.Fatalf("retire --unlock: %+v", o)
	}
}

func TestLiveAgentLockBlocksEverything(t *testing.T) {
	f := newFixture(t)
	wt := tr.Worktree(t, f.repo, filepath.Join(f.root, ".claude", "worktrees", "agent-a1234567"), "-b", "worktree-agent")
	tr.NodeModules(t, wt)
	reason := "claude agent a1234567 (pid " + strconv.Itoa(os.Getpid()) + ")"
	tr.Git(t, f.repo, "worktree", "lock", "--reason", reason, wt)
	w := f.find(f.scan(), wt)
	if w.Owner.ID != agents.ClaudeCode.ID || !w.OwnerLive || w.Safety.Level != scan.Live {
		t.Fatalf("owner=%s live=%v level=%s", w.Owner.ID, w.OwnerLive, w.Safety.Level)
	}
	opts := f.opts()
	opts.Force, opts.Unlock = true, true
	if o := Strip(w, w.Fingerprint, nil, opts); o.Status != StatusRefused {
		t.Errorf("strip on a live worktree: %+v", o)
	}
	if o := Retire(w, w.Fingerprint, opts); o.Status != StatusRefused {
		t.Errorf("retire on a live worktree: %+v", o)
	}
	if !exists(filepath.Join(wt, "node_modules")) {
		t.Error("live worktree touched")
	}
}

func TestPruneOnlyTouchesOwnEntry(t *testing.T) {
	f := newFixture(t)
	a := tr.Worktree(t, f.repo, filepath.Join(f.root, "a"), "-b", "a")
	b := tr.Worktree(t, f.repo, filepath.Join(f.root, "b"), "--detach")
	tr.Git(t, b, "commit", "-q", "--allow-empty", "-m", "b only")
	os.RemoveAll(b) // b's checkout vanished; git still has its HEAD

	r := f.scan()
	wb := f.find(r, b)
	if wb.Kind != scan.KindPrunable || wb.OnlyHere != 1 {
		t.Fatalf("b: kind=%s only_here=%d", wb.Kind, wb.OnlyHere)
	}
	wa := f.find(r, a)
	o := Retire(wa, wa.Fingerprint, f.opts())
	if o.Status != StatusDone || o.Pruned || !strings.Contains(o.PruneNote, filepath.Base(b)) {
		t.Fatalf("retire a: %+v", o)
	}
	if !exists(filepath.Join(f.repo, ".git", "worktrees", "b")) {
		t.Fatal("b's record was pruned without consent")
	}
	// Retiring the prunable entry bundles its commit, then prunes both.
	wb = f.find(f.scan(), b)
	opts := f.opts()
	opts.Force = true
	o = Retire(wb, wb.Fingerprint, opts)
	if o.Status != StatusDone || o.Rescue.BundleCommits != 1 {
		t.Fatalf("retire prunable: %+v", o)
	}
	if o.Pruned {
		// a's stale entry is still there, so b is not pruned alone either.
		t.Log("pruned")
	}
}

func TestRetirePrunableAlone(t *testing.T) {
	f := newFixture(t)
	b := tr.Worktree(t, f.repo, filepath.Join(f.root, "gone"), "--detach")
	tr.Git(t, b, "commit", "-q", "--allow-empty", "-m", "only here")
	os.RemoveAll(b)
	w := f.find(f.scan(), b)
	opts := f.opts()
	opts.Force = true
	o := Retire(w, w.Fingerprint, opts)
	if o.Status != StatusDone || !o.Pruned || o.Rescue.BundleCommits != 1 {
		t.Fatalf("retire prunable: %+v", o)
	}
	if exists(filepath.Join(f.repo, ".git", "worktrees", "gone")) {
		t.Error("record not pruned")
	}
}

func TestOrphanNeedsForce(t *testing.T) {
	f := newFixture(t)
	wt := tr.Worktree(t, f.repo, filepath.Join(f.root, "orphan"), "-b", "orphan")
	tr.Write(t, wt, ".env", "K=V\n")
	tr.Git(t, f.repo, "worktree", "prune") // no-op: checkout exists
	os.RemoveAll(filepath.Join(f.repo, ".git", "worktrees", "orphan"))
	w := f.find(f.scan(), wt)
	if w.Kind != scan.KindOrphan || w.Safety.Level != scan.Protected {
		t.Fatalf("kind=%s level=%s", w.Kind, w.Safety.Level)
	}
	if o := Retire(w, w.Fingerprint, f.opts()); o.Status != StatusRefused {
		t.Fatalf("orphan retire without --force: %+v", o)
	}
	opts := f.opts()
	opts.Force = true
	if o := Retire(w, w.Fingerprint, opts); o.Status != StatusDone || exists(wt) {
		t.Fatalf("orphan retire --force: %+v", o)
	}
	if !exists(filepath.Join(f.rescueDir(), "precious", ".env")) {
		t.Error(".env not saved")
	}
}

func TestNestedRepoBlocksRetire(t *testing.T) {
	f := newFixture(t)
	wt := tr.Worktree(t, f.repo, filepath.Join(f.root, "outer"), "-b", "outer")
	tr.Git(t, wt, "init", "-q", "vendor-clone")
	w := f.find(f.scan(), wt)
	opts := f.opts()
	opts.Force = true
	if o := Retire(w, w.Fingerprint, opts); o.Status != StatusRefused || !strings.Contains(o.Error, scan.ReasonNested) {
		t.Fatalf("retire with a nested repo: %+v", o)
	}
}

func TestMainCheckoutIsNeverAnItem(t *testing.T) {
	f := newFixture(t)
	tr.Worktree(t, f.repo, filepath.Join(f.root, "x"), "-b", "x")
	for _, w := range f.scan().Worktrees {
		if w.Path == f.repo {
			t.Fatal("main checkout listed")
		}
	}
}

func TestCheckRetireRefusesMismatchedRecord(t *testing.T) {
	f := newFixture(t)
	wt := tr.Worktree(t, f.repo, filepath.Join(f.root, "moved"), "-b", "moved")
	w := f.find(f.scan(), wt)
	// Point the checkout's .git somewhere else, as a broken manual move would.
	os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: /nonexistent/worktrees/x\n"), 0o644)
	if err := CheckRetire(w); err == nil {
		t.Error("mismatched .git accepted")
	}
}

// Review finding: a commit only in the worktree's reflog (after a reset)
// is counted and bundled, and nothing is written into the user's refs.
func TestReflogOnlyCommitIsBundled(t *testing.T) {
	f := newFixture(t)
	wt := tr.Worktree(t, f.repo, filepath.Join(f.root, "reflog"), "--detach")
	tr.Git(t, wt, "commit", "-q", "--allow-empty", "-m", "will be reset away")
	lost := tr.Git(t, wt, "rev-parse", "HEAD")
	tr.Git(t, wt, "reset", "-q", "--hard", "HEAD~1")
	w := f.find(f.scan(), wt)
	if w.OnlyHere != 1 {
		t.Fatalf("only_here = %d, want 1 (the reflog-only commit)", w.OnlyHere)
	}
	refsBefore := tr.Git(t, f.repo, "for-each-ref")
	opts := f.opts()
	opts.Force = true
	o := Retire(w, w.Fingerprint, opts)
	if o.Status != StatusDone || o.Rescue.BundleCommits != 1 {
		t.Fatalf("retire: %+v", o)
	}
	if after := tr.Git(t, f.repo, "for-each-ref"); after != refsBefore {
		t.Errorf("retire changed the repository's refs:\n%s\n---\n%s", refsBefore, after)
	}
	tr.Git(t, f.repo, "fetch", "-q", filepath.Join(f.rescueDir(), "commits.bundle"), "refs/fell/*:refs/heads/rescued/*")
	if !strings.Contains(tr.Git(t, f.repo, "for-each-ref", "--format=%(objectname)", "refs/heads/rescued"), lost) {
		t.Error("reflog-only commit not in the bundle")
	}
}

// Review finding: submodule repositories live in git's record of a linked
// worktree, which prune deletes, so retire is blocked.
func TestSubmodulesBlockRetire(t *testing.T) {
	f := newFixture(t)
	sub := tr.Repo(t, f.root, "lib")
	wt := tr.Worktree(t, f.repo, filepath.Join(f.root, "with-sub"), "-b", "with-sub")
	tr.Git(t, wt, "-c", "protocol.file.allow=always", "submodule", "add", "-q", sub, "lib")
	tr.Git(t, wt, "commit", "-q", "-m", "add submodule")
	w := f.find(f.scan(), wt)
	if !w.Submodules {
		t.Fatal("submodules not detected")
	}
	opts := f.opts()
	opts.Force, opts.Unlock = true, true
	if o := Retire(w, w.Fingerprint, opts); o.Status != StatusRefused || !strings.Contains(o.Error, scan.ReasonSubmodules) {
		t.Fatalf("retire with submodules: %+v", o)
	}
}

// Review finding: a repository inside an artifact (an editable install, a
// stray clone) is never moved by strip, even with --permanent.
func TestStripRefusesArtifactHoldingARepo(t *testing.T) {
	f := newFixture(t)
	wt := tr.Worktree(t, f.repo, filepath.Join(f.root, "host"), "-b", "host")
	tr.NodeModules(t, wt)
	inner := filepath.Join(wt, "node_modules", "editable")
	os.MkdirAll(inner, 0o755)
	tr.Git(t, inner, "init", "-q")
	tr.Write(t, inner, "work.txt", "uncommitted work")
	w := f.find(f.scan(), wt)
	if a := w.Artifacts[0]; a.Eligible() {
		t.Errorf("artifact holding a repo is eligible: %+v", a)
	}
	opts := f.opts()
	opts.Mode, opts.Force = Permanent, true
	Strip(w, w.Fingerprint, nil, opts)
	if !exists(filepath.Join(inner, "work.txt")) {
		t.Fatal("strip removed a repository inside node_modules")
	}
}

// Review finding: a managed app's worktree stays app-tracked when its
// checkout is missing, so retire (and its prune) stays blocked.
func TestPrunableManagedWorktreeStaysTracked(t *testing.T) {
	f := newFixture(t)
	wt := tr.Worktree(t, f.repo, filepath.Join(f.env.Home, "conductor", "workspaces", "app", "lisbon"), "-b", "lisbon")
	os.RemoveAll(wt)
	w := f.find(f.scan(), wt)
	if w.Kind != scan.KindPrunable || !w.Tracked {
		t.Fatalf("kind=%s tracked=%v", w.Kind, w.Tracked)
	}
	opts := f.opts()
	opts.Force, opts.Unlock = true, true
	if o := Retire(w, w.Fingerprint, opts); o.Status != StatusRefused {
		t.Fatalf("retire of a tracked prunable entry: %+v", o)
	}
	if !exists(filepath.Join(f.repo, ".git", "worktrees", "lisbon")) {
		t.Error("record pruned")
	}
}

// Review finding: if the checkout cannot move, the lock and git's record
// are left alone.
func TestFailedMoveKeepsLockAndRecord(t *testing.T) {
	f := newFixture(t)
	wt := tr.Worktree(t, f.repo, filepath.Join(f.root, "stuck"), "-b", "stuck")
	tr.Git(t, f.repo, "worktree", "lock", "--reason", "external disk", wt)
	w := f.find(f.scan(), wt)
	ok := moveFunc
	moveFunc = func(p string) error {
		if p == wt {
			return os.ErrPermission
		}
		return ok(p)
	}
	opts := f.opts()
	opts.Unlock = true
	if o := Retire(w, w.Fingerprint, opts); o.Status != StatusFailed {
		t.Fatalf("retire: %+v", o)
	}
	if !exists(filepath.Join(f.repo, ".git", "worktrees", "stuck", "locked")) {
		t.Error("lock removed although the worktree stayed")
	}
}
