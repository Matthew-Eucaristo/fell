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

// Tiny and odd terminal sizes must render without panicking, keep every
// line inside the width and keep the cursor row visible.
func TestRenderSmallTerminals(t *testing.T) {
	m, _ := fixture(t)
	for _, size := range [][2]int{{0, 0}, {20, 5}, {40, 12}, {80, 14}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m.key("end")
		m.scroll() // Update runs it after every message
		for _, view := range []func(){func() {}, func() { m.key("r") }, func() { m.key("n"); m.key("?") }} {
			view()
			for _, l := range strings.Split(m.render(), "\n") {
				if w := ansi.StringWidth(l); size[0] > 0 && w > size[0] {
					t.Errorf("%dx%d: line is %d wide: %q", size[0], size[1], w, ansi.Strip(l))
				}
			}
			m.key("esc")
		}
		if cur := m.cursor(m.rows()); cur < m.offset || cur >= m.offset+m.listHeight() {
			t.Errorf("%dx%d: cursor %d outside rows %d..%d", size[0], size[1], cur, m.offset, m.offset+m.listHeight())
		}
	}
}

func TestEmptyScan(t *testing.T) {
	m, _ := fixture(t)
	m.res = &scan.Result{}
	m.key("s")
	m.key("r")
	m.key("space")
	if m.confirm != nil {
		t.Error("an action was offered with nothing listed")
	}
	if !strings.Contains(screen(m), "No linked worktrees") {
		t.Error("empty state missing")
	}
}

// q while an action runs warns instead of quitting; ctrl+c then quits.
func TestQuitWhileBusy(t *testing.T) {
	m, _ := fixture(t)
	m.status[m.cursorKey] = working
	if cmd := m.key("q"); cmd != nil {
		t.Fatal("q quit while busy")
	}
	if !strings.Contains(screen(m), "Actions still running") {
		t.Error("no warning shown")
	}
	if cmd := m.key("ctrl+c"); cmd == nil {
		t.Error("ctrl+c after the warning did not quit")
	}
}

// A size measured before a strip must not overwrite the re-measured item.
func TestStaleMeasurementIgnoredAfterStrip(t *testing.T) {
	m, _ := fixture(t)
	w := m.find(m.cursorKey)
	nw := w.Clone()
	m.Update(actedMsg{&clean.Outcome{ID: w.ID, Action: scan.ActionStrip, Status: clean.StatusDone}, nw})
	before := nw.Total()
	m.Update(measuredMsg(scan.Measurement{ID: w.ID}))
	if m.find(w.ID).Total() != before {
		t.Error("a stale measurement replaced the fresh one")
	}
}

// Until the user moves, the cursor stays on the top row as sizes reorder it.
func TestCursorFollowsTopRowWhileMeasuring(t *testing.T) {
	m, _ := fixture(t)
	rows := m.rows()
	last := rows[len(rows)-1].w
	m.cursorKey = last.ID
	big := scan.Measure(last)
	big.Source.Allocated = 1 << 40
	m.Update(measuredMsg(big))
	if m.cursorKey != firstItem(m.rows()) {
		t.Errorf("cursor on %s, want the top row", m.cursorKey)
	}
	m.key("down")
	m.Update(measuredMsg(scan.Measure(last)))
	if m.cursorKey == firstItem(m.rows()) {
		t.Error("cursor jumped back after the user moved it")
	}
}
