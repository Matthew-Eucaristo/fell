package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Matthew-Eucaristo/fell/internal/agents"
	"github.com/Matthew-Eucaristo/fell/internal/clean"
	"github.com/Matthew-Eucaristo/fell/internal/scan"
	tr "github.com/Matthew-Eucaristo/fell/internal/testrepo"
)

// fixture: one repo with a Claude Code agent worktree holding an unpushed
// commit, a Cursor-managed worktree and a clean manual one.
func fixture(t *testing.T) (*Model, map[string]string) {
	t.Helper()
	base := tr.Dir(t)
	home := filepath.Join(base, "home")
	root := filepath.Join(home, "dev")
	os.MkdirAll(root, 0o755)
	env := agents.Env{Home: home, TmpDir: base, Getenv: func(string) string { return "" }}
	repo := tr.Repo(t, root, "shop")
	paths := map[string]string{}
	paths["agent"] = tr.Worktree(t, repo, filepath.Join(repo, ".claude", "worktrees", "agent-a1b2c3d4"), "--detach")
	tr.Git(t, paths["agent"], "commit", "-q", "--allow-empty", "-m", "agent work")
	tr.NodeModules(t, paths["agent"])
	paths["cursor"] = tr.Worktree(t, repo, filepath.Join(home, ".cursor", "worktrees", "shop", "x7k2"), "--detach")
	tr.NodeModules(t, paths["cursor"])
	paths["manual"] = tr.Worktree(t, repo, filepath.Join(root, "shop-fix"), "-b", "fix")

	opts := scan.Options{Roots: []string{root}, Agents: true, Env: env}
	res, err := scan.Scan(opts)
	if err != nil {
		t.Fatal(err)
	}
	res.MeasureAll()
	m := New(opts, clean.Options{Mode: clean.DryRun, Env: env})
	m.Update(tea.WindowSizeMsg{Width: 130, Height: 34})
	m.res = res
	m.cursorKey = firstItem(m.rows())
	return m, paths
}

func screen(m *Model) string { return ansi.Strip(m.render()) }

func TestRenderGroupsAndEvidence(t *testing.T) {
	m, _ := fixture(t)
	out := screen(m)
	if os.Getenv("FELL_PRINT") != "" {
		t.Log("\n" + out)
	}
	for _, want := range []string{"Claude Code", "Cursor", "git (manual)", "shop/agent-a1b2c3d4", "only-here:1", "app-tracked", "dry run"} {
		if !strings.Contains(out, want) {
			t.Errorf("screen lacks %q", want)
		}
	}
	for _, l := range strings.Split(out, "\n") {
		if ansi.StringWidth(l) > 130 {
			t.Errorf("line wider than the terminal: %q", l)
		}
	}
}

func TestRetireConfirmExplainsRefusals(t *testing.T) {
	m, paths := fixture(t)
	for _, k := range []string{"a", "r"} {
		m.key(k)
	}
	if m.confirm == nil {
		t.Fatal("no confirmation")
	}
	out := screen(m)
	if os.Getenv("FELL_PRINT") != "" {
		t.Log("\n" + out)
	}
	if !strings.Contains(out, "press f to force") || !strings.Contains(out, "run /delete-worktree in Cursor") {
		t.Errorf("confirmation does not explain refusals:\n%s", out)
	}
	run, refuse := m.plan()
	if len(run) != 1 || run[0].Path != paths["manual"] || len(refuse) != 2 {
		t.Fatalf("run=%d refuse=%d", len(run), len(refuse))
	}
	m.key("f") // force covers commits-only-here, never app tracking
	run, refuse = m.plan()
	if len(run) != 2 || len(refuse) != 1 || refuse[0].Path != paths["cursor"] {
		t.Fatalf("with force: run=%d refuse=%d", len(run), len(refuse))
	}
	m.key("n")
	if m.confirm != nil {
		t.Error("any other key should cancel")
	}
}

func TestDryRunActionsChangeNothing(t *testing.T) {
	m, paths := fixture(t)
	m.key("a")
	m.key("s")
	cmd := m.key("y")
	for cmd != nil {
		msg := cmd()
		_, cmd = m.Update(msg)
	}
	if len(m.outcomes) == 0 {
		t.Fatal("no outcomes")
	}
	for _, o := range m.outcomes {
		if o.Status != clean.StatusDryRun {
			t.Errorf("%s: %s %s", o.Path, o.Status, o.Error)
		}
	}
	for _, p := range []string{paths["agent"], paths["manual"]} {
		if _, err := os.Stat(p); err != nil {
			t.Error(err)
		}
	}
	if _, err := os.Stat(filepath.Join(paths["agent"], "node_modules")); err != nil {
		t.Error("dry run removed node_modules")
	}
	if !strings.Contains(screen(m), "Would move") {
		t.Error("summary does not report the dry run")
	}
}

func TestGroupByRepoAndSort(t *testing.T) {
	m, _ := fixture(t)
	m.key("tab")
	rows := m.rows()
	if rows[0].w != nil || rows[0].title != "shop" {
		t.Fatalf("first row = %+v", rows[0])
	}
	m.key("o")
	m.key("o")
	if m.sort != byName {
		t.Errorf("sort = %v", m.sort)
	}
}

// TestPrintDemoScreen renders the TUI for the demo workspace
// (source demo/setup.sh first), for the README. Skipped otherwise.
func TestPrintDemoScreen(t *testing.T) {
	home := os.Getenv("FELL_DEMO_HOME")
	if home == "" {
		t.Skip("set FELL_DEMO_HOME")
	}
	env := agents.Env{Home: home, TmpDir: os.TempDir(), Getenv: func(string) string { return "" }}
	opts := scan.Options{Roots: []string{filepath.Join(home, "dev")}, Agents: true, Env: env}
	res, err := scan.Scan(opts)
	if err != nil {
		t.Fatal(err)
	}
	res.MeasureAll()
	m := New(opts, clean.Options{Mode: clean.Trash, Env: env})
	m.Update(tea.WindowSizeMsg{Width: 118, Height: 26})
	m.res, m.freeDisk = res, 212_400_000_000
	rows := m.rows()
	m.cursorKey = rows[1].key()
	m.selected[rows[2].w.ID] = true
	t.Log("\n" + strings.ReplaceAll(screen(m), home, "~"))
}

func TestFineBar(t *testing.T) {
	for _, c := range []struct {
		n, total int64
		want     string
	}{{100, 100, "████"}, {50, 100, "██"}, {1, 100, "▏"}, {55, 100, "██▏"}, {0, 100, ""}} {
		if got := fine(c.n, c.total, 4); got != c.want {
			t.Errorf("fine(%d/%d) = %q, want %q", c.n, c.total, got, c.want)
		}
	}
}
