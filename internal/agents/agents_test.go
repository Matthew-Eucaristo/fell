package agents

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCodexRootFromConfig(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte("[desktop]\ngit-worktree-root = \"/elsewhere/wt\"\n"), 0o644)
	e := Env{Home: home, TmpDir: home, Getenv: func(string) string { return "" }}
	if got := e.codexRoot(); got != "/elsewhere/wt" {
		t.Errorf("codexRoot = %s", got)
	}
	if got := e.Attribute(Candidate{Path: "/elsewhere/wt/ab12/app"}, &Registry{}); got.ID != Codex.ID {
		t.Errorf("Attribute = %s", got.ID)
	}
}

func TestAttributeByPath(t *testing.T) {
	home := "/home/u"
	e := Env{Home: home, TmpDir: "/tmp", Getenv: func(k string) string {
		if k == "T3CODE_HOME" {
			return "/opt/t3"
		}
		return ""
	}}
	cases := map[string]string{
		"/home/u/dev/app/.claude/worktrees/agent-a1":                ClaudeCode.ID,
		"/home/u/.cursor/worktrees/app/x7k2":                        Cursor.ID,
		"/home/u/conductor/workspaces/app/lisbon":                   Conductor.ID,
		"/opt/t3/worktrees/app/t3-feature":                          T3Code.ID,
		"/tmp/vibe-kanban/worktrees/ab12-task/app":                  VibeKanban.ID,
		"/home/u/dev/app-worktrees/feature":                         Git.ID,
		"/home/u/.local/share/opencode/worktree/a1b2c3/brave-otter": OpenCode.ID,
		"/home/u/dev/app/.kilo/worktrees/fix":                       Kilo.ID,
		"/home/u/dev/app/.worktrees/hermes-1a2b3c4d":                Hermes.ID,
		"/home/u/dev/app/.gemini/worktrees/try":                     Gemini.ID,
		"/home/u/.superset/worktrees/app/feature":                   Superset.ID,
		"/home/u/.sculptor/workspaces/42/code":                      Sculptor.ID,
		"/home/u/.windsurf/worktrees/app/x1":                        Windsurf.ID,
		"/home/u/Documents/Cline/Worktrees/app-ab12c":               Cline.ID,
	}
	for path, want := range cases {
		if got := e.Attribute(Candidate{Path: path}, &Registry{}); got.ID != want {
			t.Errorf("%s -> %s, want %s", path, got.ID, want)
		}
	}
}

func TestEveryOwnerSaysHowToRemove(t *testing.T) {
	for _, o := range Owners {
		if o.Removal == "" || o.Short == "" {
			t.Errorf("%s lacks removal guidance", o.ID)
		}
	}
}

func TestAttributeByBranchLockAndSibling(t *testing.T) {
	e := Env{Home: "/home/u", TmpDir: "/tmp", Getenv: func(string) string { return "" }}
	cases := []struct {
		c    Candidate
		want string
	}{
		{Candidate{Path: "/home/u/dev/app/.worktrees/subagent-7", Branch: "hermes-subagent/x"}, Hermes.ID},
		{Candidate{Path: "/home/u/dev/app/.worktrees/t_ab", LockReason: "hermes pid=31 db=/x/state.db"}, Hermes.ID},
		{Candidate{Path: "/home/u/dev/app.worktrees/fix", MainPath: "/home/u/dev/app", Branch: "agents/brave-otter"}, VSCode.ID},
		{Candidate{Path: "/home/u/dev/app.worktrees/fix", MainPath: "/home/u/dev/app", Branch: "fix-login"}, CopilotCLI.ID},
		{Candidate{Path: "/home/u/dev/elsewhere", LockReason: "claude session s (pid 9)"}, ClaudeCode.ID},
	}
	for _, tc := range cases {
		if got := e.Attribute(tc.c, &Registry{}); got.ID != tc.want {
			t.Errorf("%+v -> %s, want %s", tc.c, got.ID, tc.want)
		}
	}
}

func TestLockOwner(t *testing.T) {
	cases := []struct {
		reason string
		owner  string
		pid    int
		ok     bool
	}{
		{"claude session my-feature (pid 4242 start Mon Oct 26 22:45:50 2026)", ClaudeCode.ID, 4242, true},
		{"claude agent a1b2c3d4e5f6a7b8 (pid 7)", ClaudeCode.ID, 7, true},
		{"hermes pid=31 db=/x/state.db", Hermes.ID, 31, true},
		{"hermes session=abc db=/x/state.db", Hermes.ID, 0, true},
		{"on a USB disk", "", 0, false},
		{"claude session x (pid abc)", "", 0, false},
	}
	for _, c := range cases {
		o, pid, _, ok := LockOwner(c.reason)
		if o.ID != c.owner || pid != c.pid || ok != c.ok {
			t.Errorf("LockOwner(%q) = %s %d %v", c.reason, o.ID, pid, ok)
		}
	}
}

func TestSkipDir(t *testing.T) {
	for name, want := range map[string]bool{".ccd-trash~x": true, ".kilo-delete-1": true, "node": false} {
		if SkipDir(name) != want {
			t.Errorf("SkipDir(%s)", name)
		}
	}
}
