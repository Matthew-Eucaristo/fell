package scan

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Matthew-Eucaristo/fell/internal/agents"
	tr "github.com/Matthew-Eucaristo/fell/internal/testrepo"
)

func TestParsePorcelain(t *testing.T) {
	out := "worktree /r\x00HEAD aaa\x00branch refs/heads/main\x00\x00" +
		"worktree /r/.claude/worktrees/x\x00HEAD bbb\x00detached\x00locked claude agent x (pid 42)\x00\x00" +
		"worktree /gone\x00HEAD ccc\x00branch refs/heads/feat/a\x00prunable gitdir file points to non-existent location\x00\x00"
	es := parsePorcelain([]byte(out))
	if len(es) != 3 {
		t.Fatalf("got %d entries", len(es))
	}
	if es[1].Branch != "" || !es[1].Detached || !es[1].Locked || es[1].LockReason != "claude agent x (pid 42)" {
		t.Errorf("entry 1 = %+v", es[1])
	}
	if es[2].Branch != "feat/a" || !es[2].Prunable {
		t.Errorf("entry 2 = %+v", es[2])
	}
}

func TestParseStatus(t *testing.T) {
	out := "M  a\x00 M b\x00?? c\x00UU d\x00R  new\x00old\x00"
	d := parseStatus([]byte(out))
	if d.Staged != 2 || d.Modified != 1 || d.Untracked != 1 || d.Conflicts != 1 {
		t.Errorf("dirty = %+v", d)
	}
}

func TestIsIgnored(t *testing.T) {
	set := map[string]bool{"node_modules/": true, ".env": true, "data/": true}
	for rel, want := range map[string]bool{"node_modules": true, ".env": true, "data/x/y.db": true, "src/.env": false, "nodes": false} {
		if got := isIgnored(set, rel, rel == "node_modules"); got != want {
			t.Errorf("isIgnored(%s) = %v", rel, got)
		}
	}
}

func TestMatchArtifactNeedsMarker(t *testing.T) {
	d := tr.Dir(t)
	tr.Write(t, d, "web/package.json", "{}")
	tr.Write(t, d, "web/node_modules/x", "")
	tr.Write(t, d, "plain/node_modules/x", "")
	tr.Write(t, d, "py/.venv/pyvenv.cfg", "")
	tr.Write(t, d, "py/venv/x", "") // no pyvenv.cfg
	tr.Write(t, d, "rs/Cargo.toml", "")
	tr.Write(t, d, "rs/target/x", "")
	for rel, want := range map[string]bool{"web/node_modules": true, "plain/node_modules": false, "py/.venv": true, "py/venv": false, "rs/target": true} {
		if got := MatchArtifact(filepath.Join(d, rel)) != nil; got != want {
			t.Errorf("MatchArtifact(%s) = %v", rel, got)
		}
	}
}

func TestPrecious(t *testing.T) {
	for name, want := range map[string]bool{".env": true, ".env.local": true, ".envrc": true, "dev.sqlite3": true, "key.pem": true, "settings.local.json": true, "README.md": false, "env.go": false} {
		if isPrecious(name) != want {
			t.Errorf("isPrecious(%s) != %v", name, want)
		}
	}
}

func TestHardLinksCountOnceAndSharedOutside(t *testing.T) {
	d := tr.Dir(t)
	tr.Write(t, d, "store/blob", strings.Repeat("x", 100000))
	os.MkdirAll(filepath.Join(d, "a"), 0o755)
	if err := os.Link(filepath.Join(d, "store", "blob"), filepath.Join(d, "a", "one")); err != nil {
		t.Skip(err)
	}
	os.Link(filepath.Join(d, "store", "blob"), filepath.Join(d, "a", "two"))
	s := DirSize(filepath.Join(d, "a"))
	if s.Allocated <= 0 || s.Shared != s.Allocated-dirBlocks(t, filepath.Join(d, "a")) {
		t.Errorf("size = %+v: the blob is linked from store/, so it must be shared", s)
	}
	all := DirSize(d)
	if all.Shared != 0 {
		t.Errorf("all links inside: shared = %d", all.Shared)
	}
}

func dirBlocks(t *testing.T, dir string) int64 {
	fi, err := os.Lstat(dir)
	if err != nil {
		t.Fatal(err)
	}
	n, _, _ := diskUsage(fi)
	return n
}

func env(home string) agents.Env {
	return agents.Env{Home: home, TmpDir: home, Getenv: func(string) string { return "" }}
}

func TestAttributionAndTracking(t *testing.T) {
	home := tr.Dir(t)
	root := filepath.Join(home, "dev")
	repo := tr.Repo(t, root, "app")
	claude := tr.Worktree(t, repo, filepath.Join(repo, ".claude", "worktrees", "agent-aaaa"), "--detach")
	desktop := tr.Worktree(t, repo, filepath.Join(repo, ".claude", "worktrees", "brave-hopper-123456"), "-b", "claude/brave")
	codex := tr.Worktree(t, repo, filepath.Join(home, ".codex", "worktrees", "ab12", "app"), "--detach")
	conductor := tr.Worktree(t, repo, filepath.Join(home, "conductor", "workspaces", "app", "lisbon"), "-b", "lisbon")
	manual := tr.Worktree(t, repo, filepath.Join(root, "app-hotfix"), "-b", "hotfix")
	os.WriteFile(filepath.Join(repo, ".git", "worktrees", "ab12", "codex-thread.json"), []byte(`{"version":1}`), 0o644)

	reg := filepath.Join(home, "registry.json")
	os.WriteFile(reg, []byte(`{"schemaVersion":2,"worktrees":{"brave":{"name":"brave","path":"`+desktop+`","baseRepo":"`+repo+`","leasedBy":null}}}`), 0o644)
	e := env(home)
	r, err := scanWithRegistry(t, root, e, reg)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{claude: "claude-code", desktop: "claude-desktop", codex: "codex", conductor: "conductor", manual: "git"}
	for p, id := range want {
		w := r.Find(p)
		if w == nil {
			t.Errorf("%s not found", p)
			continue
		}
		if w.Owner.ID != id {
			t.Errorf("%s owner = %s, want %s", filepath.Base(p), w.Owner.ID, id)
		}
		tracked := id == "claude-desktop" || id == "codex" || id == "conductor"
		if w.Tracked != tracked {
			t.Errorf("%s tracked = %v", filepath.Base(p), w.Tracked)
		}
		if tracked && w.Decide(ActionRetire).Possible() {
			t.Errorf("%s: retire of an app-tracked worktree is possible", filepath.Base(p))
		}
		if tracked && !w.OwnerLive && !w.Decide(ActionStrip).Allowed {
			t.Errorf("%s: strip should stay available", filepath.Base(p))
		}
	}
}

func TestUnreadableRegistryFailsClosed(t *testing.T) {
	home := tr.Dir(t)
	root := filepath.Join(home, "dev")
	repo := tr.Repo(t, root, "app")
	wt := tr.Worktree(t, repo, filepath.Join(repo, ".claude", "worktrees", "agent-bbbb"), "--detach")
	for name, content := range map[string]string{"corrupt": `{"schemaVersion":2,"worktrees":{`, "future": `{"schemaVersion":3,"worktrees":{}}`} {
		reg := filepath.Join(home, name+".json")
		os.WriteFile(reg, []byte(content), 0o644)
		r, err := scanWithRegistry(t, root, env(home), reg)
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Warnings) == 0 {
			t.Errorf("%s registry: no warning", name)
		}
		if w := r.Find(wt); w == nil || !w.Tracked || w.Decide(ActionRetire).Possible() {
			t.Errorf("%s registry: Claude worktree not protected", name)
		}
	}
}

func TestLeasedDesktopWorktreeIsLive(t *testing.T) {
	home := tr.Dir(t)
	root := filepath.Join(home, "dev")
	repo := tr.Repo(t, root, "app")
	wt := tr.Worktree(t, repo, filepath.Join(repo, ".claude", "worktrees", "calm-turing-abcdef"), "--detach")
	reg := filepath.Join(home, "registry.json")
	os.WriteFile(reg, []byte(`{"schemaVersion":2,"worktrees":{"x":{"path":"`+wt+`","leasedBy":"session-1"}}}`), 0o644)
	r, _ := scanWithRegistry(t, root, env(home), reg)
	w := r.Find(wt)
	if w == nil || !w.OwnerLive || w.Safety.Level != Live || w.Decide(ActionStrip).Possible() {
		t.Fatalf("leased worktree not live: %+v", w)
	}
}

func TestKeepMarkerBlocksRetire(t *testing.T) {
	home := tr.Dir(t)
	root := filepath.Join(home, "dev")
	repo := tr.Repo(t, root, "app")
	wt := tr.Worktree(t, repo, filepath.Join(root, "keep"), "-b", "keep")
	tr.Write(t, wt, ".worktree-keep", "")
	r, _ := scanWithRegistry(t, root, env(home), filepath.Join(home, "none.json"))
	if d := r.Find(wt).Decide(ActionRetire); d.Possible() {
		t.Errorf("retire with .worktree-keep possible: %+v", d)
	}
}

func TestFingerprintStableUntilStateChanges(t *testing.T) {
	home := tr.Dir(t)
	root := filepath.Join(home, "dev")
	repo := tr.Repo(t, root, "app")
	wt := tr.Worktree(t, repo, filepath.Join(root, "fp"), "-b", "fp")
	tr.NodeModules(t, wt)
	scanFP := func() string {
		r, _ := scanWithRegistry(t, root, env(home), filepath.Join(home, "none.json"))
		return r.Find(wt).Fingerprint
	}
	a := scanFP()
	tr.Write(t, wt, "node_modules/more/x.js", "build output changes size only")
	if b := scanFP(); a != b {
		t.Error("fingerprint changed when only artifacts changed")
	}
	tr.Git(t, wt, "commit", "-q", "--allow-empty", "-m", "new")
	if c := scanFP(); c == a {
		t.Error("fingerprint unchanged after a new commit")
	}
}

// scanWithRegistry scans root with Claude Desktop's registry at a test path.
func scanWithRegistry(t *testing.T, root string, e agents.Env, reg string) (*Result, error) {
	t.Helper()
	old := registryPath
	registryPath = func(agents.Env) string { return reg }
	defer func() { registryPath = old }()
	return Scan(Options{Roots: []string{root}, Agents: true, Env: e})
}

func TestClonesCountAsShared(t *testing.T) {
	d := tr.Dir(t)
	tr.Write(t, d, "store/blob", strings.Repeat("y", 400000))
	os.MkdirAll(filepath.Join(d, "nm"), 0o755)
	if err := exec.Command("cp", "-c", filepath.Join(d, "store", "blob"), filepath.Join(d, "nm", "blob")).Run(); err != nil {
		t.Skip("no clonefile support:", err)
	}
	if _, ok := privateSize(filepath.Join(d, "nm", "blob")); !ok {
		t.Skip("filesystem does not report private size")
	}
	s := DirSize(filepath.Join(d, "nm"))
	if s.Shared < 400000 {
		t.Errorf("clone not reported as shared: %+v", s)
	}
}

// Review finding: unknown evidence fails closed.
func TestUnknownCommitsAreProtected(t *testing.T) {
	w := &Worktree{Kind: KindWorktree, Head: "abc", HasRemotes: true, Unpushed: 0, OnlyHere: -1, CwdKnown: true}
	w.Classify()
	if !w.Safety.Has(ReasonUnknown) || w.Decide(ActionRetire).Allowed {
		t.Errorf("unknown only_here allowed retire: %+v", w.Safety)
	}
}
