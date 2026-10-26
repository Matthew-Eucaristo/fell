# Design notes

Why fell exists, the incidents its rules come from, and what each agent tool does with worktrees, as researched in October 2026. Vendor behaviour changes fast; when it does, fell fails closed (see the README's safety model) and this page should be updated with sources.

## The problem

AI coding tools now give every chat, session or subagent its own git worktree. Each one is a full checkout with its own `node_modules` and build output, and guides for parallel agents put that at 300–500 MB of dependencies per worktree. Reported leaks:

| Incident | Scale | Source |
| --- | --- | --- |
| Claude Code subagent isolation, all detached HEADs | 764 worktrees, ~36 GB, 220 MB left free | [anthropics/claude-code#58914](https://github.com/anthropics/claude-code/issues/58914) |
| Claude Code `.worktreeinclude` full copies | ~9.4 GB per worktree, ~200 GB across 22 | [anthropics/claude-code#99672](https://github.com/anthropics/claude-code/issues/99672) |
| Claude Code Workflows isolation lanes | 556 lanes from 18 runs in two weeks | [anthropics/claude-code#102956](https://github.com/anthropics/claude-code/issues/102956) |
| Codex repo copies in `/private/tmp` (mostly not worktrees) | 118 copies, 202 GB in ~3 days | [openai/codex#35383](https://github.com/openai/codex/issues/35383) |
| Cursor parallel agents | ~22 worktrees, ~140 GB in a week | [Cursor forum](https://forum.cursor.com/t/windows-request-to-disable-automatic-worktree-creation-critical-disk-space-issue/146189) |
| Multi-agent orchestration | 43 worktrees; `du` said ~85 GB, deleting freed ~9 GB (pnpm hard links, local snapshots) | [hardbeat920/monocode#764](https://github.com/hardbeat920/monocode/issues/764) |

Vendor cleanups fail in both directions:

- **Leaking.** Stale worktrees are never cleaned after crashes ([#26725](https://github.com/anthropics/claude-code/issues/26725)). Headless `-p` runs never clean up. Claude Code's only automatic reclaimer is tied to transcript retention (`cleanupPeriodDays`). Codex's keep-count had "no observable effect" ([openai/codex#35383](https://github.com/openai/codex/issues/35383)). Archiving often only parks a worktree ([#91472](https://github.com/anthropics/claude-code/issues/91472)).
- **Destroying.** A pool garbage collector ran `git worktree remove --force` and deleted git-ignored databases and ~2.9 GB of generated images, with "Nothing went to the Trash" ([#98186](https://github.com/anthropics/claude-code/issues/98186)). One session's cleanup discarded another session's live work, because it checked git state but not whether the worktree was in use ([#74386](https://github.com/anthropics/claude-code/issues/74386)). On Windows, an archive followed a junction and deleted its target ([openai/codex#34702](https://github.com/openai/codex/issues/34702)).

Existing third-party janitors are new, small, not packaged, and remove permanently. The gap fell fills is trust: reversible, evidence-first cleanup that cooperates with each tool's own lifecycle.

## Rules that follow

1. A clean `git status` is not enough. A worktree can hold git-ignored secrets and data, be in use by a process, or be locked by a session. It can also hold commits that `git branch` never shows, because detached HEADs are invisible there. For a detached HEAD, "unpushed" has to mean "not reachable from any remote ref". fell also computes "only here": commits no branch, tag or remote has, which pruning git's record would orphan.
2. Liveness comes from the lock owner (PID plus start time, so a reused PID doesn't count), from app leases, and from processes whose working directory is inside the worktree.
3. Freed space is measured as free disk space before and after, not estimated. Hard links are counted once and their shared bytes reported. On macOS, `getattrlist(ATTR_CMNEXT_PRIVATESIZE)` gives each file's bytes not shared with clones or snapshots, so clone-heavy trees (pnpm's clone import, Claude Desktop worktrees) are not overstated.
4. Anything a tool still tracks is removed by that tool, not by fell. fell reads vendor state only, and fails closed when it can't read it.
5. Removal is reversible. Everything goes into the Trash, together with a rescue folder (bundle, patch, ignored secrets, manifest). Emptying the Trash is the user's explicit act of forgetting, and uninstalling leaves no residue.
6. A caller (an umbrella UI, a script) never deletes from a scan. It hands back IDs pinned to fingerprints, and fell re-checks each item before acting ([contract](contract.md)).

## Why trash + `git worktree prune`, not `git worktree remove`

`git worktree remove` deletes the checkout permanently, including git-ignored files, and with `--force` dirty ones too. That is exactly the data-loss path above. The [git-worktree documentation](https://git-scm.com/docs/git-worktree) describes the other supported path: when a worktree's directory is deleted outside git, its administrative files are removed by `git worktree prune` (or later by `gc.worktreePruneExpire`). fell moves the directory to the Trash, then runs `git worktree prune`. It prunes only when `git worktree prune --dry-run -v` shows that nothing but that worktree's own entry would go. Before that, it bundles the commits only that worktree has, because pruning drops its HEAD and reflog. Locked entries are never pruned by git; fell runs `git worktree unlock` only with `--unlock`, and never for a live lock.

## Tool-by-tool facts (October 2026)

Unless noted, a tool neither locks its worktrees nor writes markers. "Managed" means the tool keeps its own record and removes the worktree itself; fell then only strips (and only while the tool isn't running).

### Claude Code (CLI)
- **Location and branches:** `--worktree <name>` creates `<repo>/.claude/worktrees/<name>` on branch `worktree-<name>`. Subagent, workflow and background lanes use ephemeral names (`agent-a<hex>`, `wf_…`, `bg-…`, `job-…`). ([docs](https://code.claude.com/docs/en/worktrees))
- **Marker:** `<gitdir>/worktrees/<n>/CLAUDE_BASE` holds the base commit.
- **Lock:** `git worktree lock --reason "claude session <slug> (pid <PID> start <TOKEN>)"` (or `claude agent <id> …`). TOKEN is the process start time from `LC_ALL=C TZ=UTC ps -o lstart=`. A lock counts as live while that PID runs with the same start time. ([#74386](https://github.com/anthropics/claude-code/issues/74386))
- **Own cleanup:** clean, unnamed sessions are removed on exit; otherwise it prompts. A sweep tied to `cleanupPeriodDays` (default 30) removes ephemeral worktrees only when they are clean, have no commits off a remote (or HEAD equals `CLAUDE_BASE`), and are unlocked or hold a dead Claude lock. There is no list or remove command; users run `git worktree …`.

### Claude Desktop
- **Location and branches:** `<repo>/.claude/worktrees/<adjective-name-hex6>`, branch `claude/<name>`. Pooled worktrees are parked on a detached HEAD.
- **Registry:** Application Support's `git-worktrees.json` (schemaVersion 2). It holds `worktrees.<name>.{path, baseRepo, branch, leasedBy, pooledAt, …}`; `leasedBy` is a session ID, a `spare:` reservation, or null. `untrackedDirGc.roots` lists the folders whose unregistered directories it garbage-collects. Marker: `<gitdir>/worktrees/<n>/claude-desktop-worktree`.
- **Own GC:** skips anything with a `.worktree-keep` file, a lock, recent changes, a live session or a dirty status; otherwise it runs `git worktree remove --force`. Tombstones are named `.ccd-trash~*`.
- **User removal:** storage cleanup in Settings (clean up or archive inactive sessions, discard kept worktrees). Archived sessions' kept worktrees go about 30 days later.

### Codex
- **Location:** `$CODEX_HOME/worktrees/<4 hex>/<repo>` (default `~/.codex/worktrees`), or `[desktop] git-worktree-root` in `config.toml`. Created with `--detach`.
- **Marker:** `<gitdir>/worktrees/<n>/codex-thread.json` (`{"version":1,"ownerThreadId":…}`).
- **Own cleanup (app only):** `worktree-keep-count` (default 15), pinned and running chats exempt, snapshot before deletion, internal `git worktree remove --force`. Users archive the chat to remove one. Since late 2026 the app's process is named `ChatGPT` (bundle `com.openai.codex`); the CLI is `codex`. ([docs](https://learn.chatgpt.com/docs/environments/git-worktrees), `codex-rs/worktree`)

### Cursor
`~/.cursor/worktrees/<repo>/<id>`, often detached. `cursor.worktreeMaxCount` and `cursor.worktreeCleanupIntervalHours` drive its own pruning, which also removes manual worktrees under that root. Users run `/delete-worktree`. ([docs](https://cursor.com/docs/configuration/worktrees))

### Conductor
`~/conductor/workspaces/<repo>/<city>` (configurable per repository) and `<repo>/.conductor/`. Archive saves git state, including uncommitted files, and can be undone. ([docs](https://conductor.build/docs))

### OpenCode
`$XDG_DATA_HOME/opencode/worktree/<project id>/<slug>` (`worktree.directory` changes it), detached, recorded in its SQLite database. It never removes worktrees itself; users run `/worktrees` → ctrl+d in the TUI or use Settings → Worktrees.

### Kilo Code
CLI: `$XDG_DATA_HOME/kilo/worktree/<project>/<slug>`, or `<repo>/.kilo/worktrees/<slug>` with `--worktree`; remove with `kilo worktree remove <name>`. IDE Agent Manager: `<repo>/.kilo/worktrees/<branch>` with marker `kilo-agent-manager-metadata.json`, plus a pool in `…/kilo/worktree-pool/`. Pending deletes are renamed `.kilo-delete-*`.

### Hermes Agent
`<repo>/.worktrees/hermes-<hex>` or `<repo>/.worktrees/<name>`, branches `hermes/…` and `hermes-subagent/…`. **Locks** with `hermes pid=<PID> …` or `hermes session=<id> …`. State lives in `~/.hermes`. It removes clean, pushed worktrees on exit and in a periodic sweep; users run `hermes worktree list|prune|restore`.

### Gemini CLI
With `experimental.worktrees`, `gemini -w` creates `<repo>/.gemini/worktrees/<name>` on `worktree-<name>`. It keeps no state, removes nothing, and prints `git worktree remove <path>` on exit.

### VS Code agent sessions and Copilot CLI
Both use `<repo>.worktrees/<name>` next to the repository. VS Code names branches `[prefix]agents/…`, records them per session, and removes them when the session is archived or deleted. Copilot CLI (`--worktree`, `/worktree`) records nothing known, so `git worktree remove` applies.

### Others
- **Cline:** `~/Documents/Cline/Worktrees/<project>-<id>` or `~/.cline/worktrees/<id>/<repo>`. Delete Worktree in the UI.
- **Roo Code:** `~/.roo/worktrees/<project>-<id>`.
- **Warp:** `~/.warp/worktrees/<repo>/<name>`.
- **Superset:** `~/.superset/worktrees/<project>/<branch>`, database-backed. Archive or delete the workspace.
- **Sculptor:** `~/.sculptor/workspaces/<id>/code`, database-backed, `sculpt workspace delete`. Its own sweep removes unknown directories under that folder, so fell never stages files there.
- **Devin Desktop / Windsurf Cascade:** `~/.windsurf/worktrees/<repo>/<id>`, LRU-capped at 20 per workspace. Deleting the conversation deletes the worktree.
- **T3 Code:** `$T3CODE_HOME/worktrees/<repo>/<branch>` (default `~/.t3`), database-backed. Settings → Storage → Delete now; it respects locks.
- **Vibe Kanban:** `$TMPDIR/vibe-kanban/worktrees/<id>/<repo>` (Linux `/var/tmp/…`) or `<dir>/.vibe-kanban-workspaces/`, branch `vk/…`. It removes idle worktrees itself and recreates them on reopen.
- **Worktrunk (`wt`):** sibling `<repo>.<branch>`; `wt remove` is its official removal. It stages removals in `<git-common-dir>/wt/trash/` and drains them in the background, so fell never looks inside `.git`.
- **git-worktree-runner (`git gtr`):** `<parent>/<repo>-worktrees/<branch>`; `git gtr rm` / `git gtr clean`.
- **No local worktrees:** Aider, Crush, Goose, Kiro, Jules (cloud).

### Unverified
- Exact process names of some Electron apps and node or python wrappers (Hermes may show as `python`). A process working inside a worktree is detected regardless.
- Codex's snapshot location; Cursor's state store; whether Copilot CLI records its worktrees.
- Amp's runner worktrees are siblings named like manual ones, so they show as git (manual).
