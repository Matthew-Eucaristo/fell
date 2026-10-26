// Package agents knows which tool created a worktree, whether that tool
// still tracks it, and how that tool removes it. It only ever reads other
// tools' files.
package agents

import (
	"github.com/Matthew-Eucaristo/fell/internal/pathx"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

// Owner is the tool a worktree is attributed to.
type Owner struct {
	ID   string `json:"id"`   // stable identifier, part of the JSON contract
	Name string `json:"name"` // display name
	// Processes are executable names whose presence means the app is running.
	Processes []string `json:"-"`
	// Managed: the app keeps its own record of the worktrees it creates and
	// removes them itself, so they count as app-tracked and fell never
	// retires them.
	Managed bool `json:"-"`
	// Removal tells the user how the tool removes its own worktrees.
	Removal string `json:"how_to_remove"`
	// Short is the official removal in a few words, for narrow UI.
	Short string `json:"-"`

	roots    func(Env) []string // directories it creates worktrees in
	segments []string           // path fragments ("/.claude/worktrees/")
	markers  []string           // files it writes in git's worktree admin dir
	branches []string           // branch-name prefixes it uses
	lock     *regexp.Regexp     // lock reasons it writes; group 1 is a PID
}

// Lock reasons. Claude Code: "claude session <slug> (pid <PID> start <TOKEN>)"
// or "claude agent <id> (pid <PID>)". Hermes: "hermes pid=<PID> …" or
// "hermes session=<id> …".
var (
	claudeLock = regexp.MustCompile(`^claude (?:agent|session) .{1,255} \(pid (\d{1,10})(?: start (.{1,255}))?\)$`)
	hermesLock = regexp.MustCompile(`^hermes (?:pid=(\d{1,10})|session=\S+)(?:\s|$)`)
)

func fixed(paths ...string) func(Env) []string {
	return func(e Env) []string {
		out := make([]string, len(paths))
		for i, p := range paths {
			out[i] = filepath.Join(e.Home, p)
		}
		return out
	}
}

var (
	ClaudeCode = Owner{ID: "claude-code", Name: "Claude Code", Processes: []string{"claude"},
		Removal:  "Claude Code removes clean, pushed agent worktrees itself once they are older than cleanupPeriodDays (default 30); an interactive session offers to remove its worktree on exit. Otherwise `git worktree remove`.",
		Short:    "`git worktree remove`, or Claude Code's own sweep",
		roots:    fixed(".claude-worktrees"),
		segments: []string{"/.claude/worktrees/"}, markers: []string{"CLAUDE_BASE"}, lock: claudeLock}
	ClaudeDesktop = Owner{ID: "claude-desktop", Name: "Claude Desktop", Processes: []string{"Claude"},
		Removal: "Claude Desktop pools these for reuse and removes idle clean ones itself. To free one now, use the app's storage cleanup in Settings or archive the session; kept worktrees of archived sessions go about 30 days later.",
		Short:   "Claude Desktop → Settings → storage cleanup",
		markers: []string{"claude-desktop-worktree"}}
	Codex = Owner{ID: "codex", Name: "Codex", Processes: []string{"ChatGPT", "Codex"}, Managed: true,
		Removal: "Codex removes a chat's worktree when you archive the chat, or when more than worktree-keep-count (default 15) exist; it snapshots first so reopening the chat restores it.",
		Short:   "archive the chat in Codex",
		roots:   func(e Env) []string { return []string{e.codexRoot()} }, markers: []string{"codex-thread.json"}}
	Cursor = Owner{ID: "cursor", Name: "Cursor", Processes: []string{"Cursor"}, Managed: true,
		Removal: "Cursor caps its worktrees (cursor.worktreeMaxCount) and prunes them every cursor.worktreeCleanupIntervalHours; run /delete-worktree in the agent chat to remove one now.",
		Short:   "run /delete-worktree in Cursor",
		roots:   fixed(".cursor/worktrees")}
	Conductor = Owner{ID: "conductor", Name: "Conductor", Processes: []string{"Conductor"}, Managed: true,
		Removal: "Archive the workspace in Conductor; it saves the git state, including uncommitted files, and can restore it.",
		Short:   "archive the workspace in Conductor",
		roots:   fixed("conductor"), segments: []string{"/.conductor/"}}
	T3Code = Owner{ID: "t3code", Name: "T3 Code", Processes: []string{"T3 Code"}, Managed: true,
		Removal: "In T3 Code use Settings → Storage → Delete now, or delete the thread.",
		Short:   "T3 Code → Settings → Storage → Delete now",
		roots:   func(e Env) []string { return []string{e.t3Root()} }}
	VibeKanban = Owner{ID: "vibe-kanban", Name: "Vibe Kanban", Processes: []string{"vibe-kanban"}, Managed: true,
		Removal: "Delete the workspace in Vibe Kanban; it also removes idle worktrees itself and recreates them when a workspace is reopened.",
		Short:   "delete the workspace in Vibe Kanban",
		roots: func(e Env) []string {
			return []string{filepath.Join(e.TmpDir, "vibe-kanban", "worktrees"), "/var/tmp/vibe-kanban/worktrees"}
		},
		segments: []string{"/.vibe-kanban-workspaces/"}}
	OpenCode = Owner{ID: "opencode", Name: "OpenCode", Processes: []string{"opencode"}, Managed: true,
		Removal: "OpenCode keeps its worktrees until you delete them: TUI /worktrees then ctrl+d, or Settings → Worktrees in the desktop app.",
		Short:   "OpenCode → /worktrees → ctrl+d",
		roots:   func(e Env) []string { return []string{filepath.Join(e.dataHome(), "opencode", "worktree")} }}
	Kilo = Owner{ID: "kilo", Name: "Kilo Code", Processes: []string{"kilo", "kilocode"}, Managed: true,
		Removal: "Use `kilo worktree remove <name>`, or Delete worktree in Kilo's Agent Manager.",
		Short:   "`kilo worktree remove <name>`",
		roots: func(e Env) []string {
			return []string{filepath.Join(e.dataHome(), "kilo", "worktree"), filepath.Join(e.dataHome(), "kilo", "worktree-pool")}
		},
		segments: []string{"/.kilo/worktrees/"}, markers: []string{"kilo-agent-manager-metadata.json"}}
	Hermes = Owner{ID: "hermes", Name: "Hermes Agent", Processes: []string{"hermes", "hermes-agent", "hermes-acp", "Hermes"}, Managed: true,
		Removal:  "Hermes removes clean, pushed worktrees itself (on exit and in its periodic sweep); use `hermes worktree list` / `hermes worktree prune`, or its desktop app.",
		Short:    "`hermes worktree prune`",
		segments: []string{"/.worktrees/hermes-"}, branches: []string{"hermes/", "hermes-subagent/"}, lock: hermesLock}
	Gemini = Owner{ID: "gemini-cli", Name: "Gemini CLI", Processes: []string{"gemini"},
		Removal:  "Gemini CLI keeps no record of its worktrees and prints `git worktree remove <path>` on exit.",
		Short:    "`git worktree remove`",
		segments: []string{"/.gemini/worktrees/"}}
	VSCode = Owner{ID: "vscode", Name: "VS Code agent sessions", Processes: []string{"Code", "Code - Insiders"}, Managed: true,
		Removal: "VS Code removes a session's worktree when you archive or delete the session (shared worktrees go with the last session using them).",
		Short:   "archive or delete the session in VS Code"}
	CopilotCLI = Owner{ID: "copilot-cli", Name: "Copilot CLI", Processes: []string{"copilot"},
		Removal: "Copilot CLI keeps its worktrees next to the repository; remove one with `git worktree remove <path>`.",
		Short:   "`git worktree remove`"}
	Cline = Owner{ID: "cline", Name: "Cline", Processes: []string{"cline"},
		Removal: "Use Delete Worktree in Cline's Worktrees view, or `git worktree remove <path>`.",
		Short:   "Cline → Worktrees → Delete Worktree",
		roots:   fixed("Documents/Cline/Worktrees", ".cline/worktrees")}
	Roo = Owner{ID: "roo-code", Name: "Roo Code",
		Removal: "Use Delete worktree in Roo Code's Worktrees view, or `git worktree remove <path>`.",
		Short:   "Roo Code → Worktrees → Delete worktree",
		roots:   fixed(".roo/worktrees")}
	Warp = Owner{ID: "warp", Name: "Warp", Processes: []string{"Warp", "stable"},
		Removal: "Warp's worktree tabs create plain git worktrees; remove one with `git worktree remove <path>`.",
		Short:   "`git worktree remove`",
		roots:   fixed(".warp/worktrees")}
	Superset = Owner{ID: "superset", Name: "Superset", Processes: []string{"Superset"}, Managed: true,
		Removal: "Delete or archive the workspace in Superset, or `superset workspaces archive`.",
		Short:   "archive the workspace in Superset",
		roots:   fixed(".superset/worktrees")}
	Sculptor = Owner{ID: "sculptor", Name: "Sculptor", Processes: []string{"Sculptor", "sculptor"}, Managed: true,
		Removal: "Delete the workspace in Sculptor, or `sculpt workspace delete`.",
		Short:   "`sculpt workspace delete`",
		roots:   fixed(".sculptor/workspaces")}
	Windsurf = Owner{ID: "windsurf", Name: "Devin Desktop / Windsurf", Processes: []string{"Windsurf", "Devin"}, Managed: true,
		Removal: "Cascade keeps at most 20 worktrees per workspace and removes the oldest itself; deleting a Cascade conversation deletes its worktree.",
		Short:   "delete the Cascade conversation",
		roots:   fixed(".windsurf/worktrees")}
	Git = Owner{ID: "git", Name: "git (manual)",
		Removal: "`git worktree remove <path>` (or your worktree manager: `wt remove`, `git gtr rm`).",
		Short:   "`git worktree remove`"}
)

// Owners lists every known owner in display order.
var Owners = []Owner{ClaudeCode, ClaudeDesktop, Codex, Cursor, Conductor, OpenCode, Kilo, Hermes, Gemini,
	VSCode, CopilotCLI, Cline, Roo, Warp, Superset, Sculptor, Windsurf, T3Code, VibeKanban, Git}

// Env is where to look; tests point it at a temp home.
type Env struct {
	Home   string
	TmpDir string
	Getenv func(string) string
}

// DefaultEnv uses the real home and environment.
func DefaultEnv() Env {
	home, _ := os.UserHomeDir()
	return Env{Home: home, TmpDir: os.TempDir(), Getenv: os.Getenv}
}

func (e Env) dataHome() string {
	if d := e.Getenv("XDG_DATA_HOME"); filepath.IsAbs(d) {
		return d
	}
	return filepath.Join(e.Home, ".local", "share")
}

func (e Env) codexRoot() string {
	codexHome := e.Getenv("CODEX_HOME")
	if codexHome == "" {
		codexHome = filepath.Join(e.Home, ".codex")
	}
	if root := tomlString(filepath.Join(codexHome, "config.toml"), "git-worktree-root"); filepath.IsAbs(root) {
		return root
	}
	return filepath.Join(codexHome, "worktrees")
}

func (e Env) t3Root() string {
	if h := e.Getenv("T3CODE_HOME"); h != "" {
		return filepath.Join(h, "worktrees")
	}
	return filepath.Join(e.Home, ".t3", "worktrees")
}

// Roots are agent-owned directories worth walking for worktrees whose main
// repository may live outside the user's roots. Missing ones are skipped by
// the caller.
func (e Env) Roots() []string {
	var out []string
	for _, o := range Owners {
		if o.roots != nil {
			out = append(out, o.roots(e)...)
		}
	}
	return out
}

// DesktopRegistryPath is Claude Desktop's worktree pool file.
func (e Env) DesktopRegistryPath() string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(e.Home, "Library", "Application Support", "Claude", "git-worktrees.json")
	}
	cfg := e.Getenv("XDG_CONFIG_HOME")
	if cfg == "" {
		cfg = filepath.Join(e.Home, ".config")
	}
	return filepath.Join(cfg, "Claude", "git-worktrees.json")
}

// Candidate is what attribution looks at.
type Candidate struct {
	Path       string // real absolute checkout path
	Admin      string // <common>/worktrees/<id>, "" if unknown
	Branch     string
	LockReason string
	MainPath   string // main checkout, "" if unknown
}

// Attribute names the tool that created a worktree. Strongest evidence
// first: Claude Desktop's registry, markers in git's admin dir, lock
// reasons, the tool's own directories, path conventions, branch prefixes.
func (e Env) Attribute(c Candidate, reg *Registry) Owner {
	if reg.Lookup(c.Path) != nil {
		return ClaudeDesktop
	}
	if c.Admin != "" {
		for _, o := range Owners {
			for _, m := range o.markers {
				if exists(filepath.Join(c.Admin, m)) {
					return o
				}
			}
		}
	}
	if c.LockReason != "" {
		if o, _, _, ok := LockOwner(c.LockReason); ok {
			return o
		}
	}
	for _, o := range Owners {
		if o.roots == nil {
			continue
		}
		for _, r := range o.roots(e) {
			if pathx.Within(r, c.Path) || pathx.Within(pathx.Real(r), c.Path) {
				return o
			}
		}
	}
	slashed := strings.ToLower(filepath.ToSlash(c.Path))
	for _, o := range Owners {
		for _, s := range o.segments {
			if strings.Contains(slashed, s) {
				return o
			}
		}
		for _, b := range o.branches {
			if strings.HasPrefix(c.Branch, b) {
				return o
			}
		}
	}
	// VS Code agent sessions and Copilot CLI share <repo>.worktrees/ next to
	// the repository; VS Code names its branches "…agents/…".
	if c.MainPath != "" && filepath.Base(filepath.Dir(c.Path)) == filepath.Base(c.MainPath)+".worktrees" {
		if strings.Contains(c.Branch, "agents/") {
			return VSCode
		}
		return CopilotCLI
	}
	return Git
}

// LockOwner parses a lock reason written by an agent. ok is false for locks
// fell does not recognise; those are always treated as held. pid is 0 when
// the reason names no process.
func LockOwner(reason string) (o Owner, pid int, start string, ok bool) {
	reason = strings.TrimSpace(reason)
	if m := claudeLock.FindStringSubmatch(reason); m != nil {
		pid, _ = strconv.Atoi(m[1])
		return ClaudeCode, pid, strings.TrimSpace(m[2]), pid > 0
	}
	if m := hermesLock.FindStringSubmatch(reason); m != nil {
		pid, _ = strconv.Atoi(m[1])
		return Hermes, pid, "", true
	}
	return Owner{}, 0, "", false
}

// KeepMarker is the file Claude Desktop honours to keep a worktree; fell
// honours it too.
const KeepMarker = ".worktree-keep"

// SkipDir reports directory names that are another tool's removal in
// progress (Claude Desktop's tombstones, Kilo's pending deletes); fell
// neither lists nor descends into them.
func SkipDir(name string) bool {
	return strings.HasPrefix(name, ".ccd-trash~") || strings.HasPrefix(name, ".kilo-delete-")
}

// tomlString reads `key = "value"` from a TOML file without a parser; fell
// only needs one string setting and must not depend on the file's layout.
func tomlString(path, key string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(k) != key {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			return v[1 : len(v)-1]
		}
	}
	return ""
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}
