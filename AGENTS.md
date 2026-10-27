# fell

Machine-wide, safety-first TUI/CLI that finds git worktrees (especially ones AI coding agents leave behind), shows the evidence for each, and strips or retires them reversibly. Go + Bubble Tea v2 + Lip Gloss v2 (+ charmbracelet/x/ansi, golang.org/x/sys). macOS and Linux. Family rules: `../AGENTS.md`.

## Layout
- `internal/gitx` — runs git. `Read*` for queries (`--no-optional-locks`, `core.fsmonitor=false`, `GIT_OPTIONAL_LOCKS=0`); `Write*` only from `internal/clean`.
- `internal/agents` — table of owners (Claude Code/Desktop, Codex, Cursor, Conductor, OpenCode, Kilo, Hermes, Gemini CLI, VS Code, Copilot CLI, Cline, Roo, Warp, Superset, Sculptor, Windsurf, T3 Code, Vibe Kanban, git): roots, path segments, admin markers, branch prefixes, lock-reason formats, process names, `Managed`, official removal text (`Removal`, `Short`); read-only Claude Desktop registry (`git-worktrees.json`, schemaVersion 2). Sources per tool: `docs/design.md`.
- `internal/procs` — process table and working directories (`ps` + `lsof` on macOS, `/proc` on Linux).
- `internal/scan` — discovery (walk roots + agent roots + registry, then `git worktree list --porcelain -z` per common dir), evidence, artifact catalog (`ArtifactRules`), hard-link-aware sizing, safety classification (`Classify`, `Decide`), fingerprints, `Refresh`.
- `internal/clean` — the only code that changes anything: `Strip`, `Retire`, rescue folder, guards (`guard.go`), trash movers, statfs.
- `internal/contract` — JSON contract (schema_version 1); `docs/contract.md` documents it.
- `internal/ui` — Bubble Tea model/view. `internal/report` — text output. `main.go` — subcommands and flags.
- `internal/testrepo` — temp repos/worktrees for tests. `demo/` — synthetic workspace + VHS tape. `packaging/homebrew/fell.rb` — tap formula template; `RELEASING.md`.

## Invariants
- Official way only: worktrees are removed by moving the checkout to the Trash and then `git worktree prune` (git's documented path), and only when the prune would drop nothing but that entry. Never `git worktree remove` (it deletes ignored files permanently), never delete branches, never `--force` git.
- Never edit another tool's state; parse it read-only. Unknown/unparseable vendor formats fail closed (treated as app-tracked/protected, with a warning). Unrecognised lock reasons count as held.
- App-tracked worktrees (Desktop registry entry, Codex `codex-thread.json`, anything under a `Managed` owner's root) are never retired, even with `--force`; show `Owner.Removal`. Strip stays available unless live.
- Live (blocked for every action, no override): Claude lock whose PID and start token are alive, Desktop `leasedBy`, owning app running while it tracks the worktree, or an agent process with its cwd inside.
- Every action re-reads the item (`scan.Refresh`), refuses on fingerprint mismatch, re-runs `Decide`, then the guards. Retire writes and verifies the rescue folder (bundle of commits only it holds, uncommitted patch, precious copies, manifest, RESTORE.md) and moves it to the Trash before touching the checkout.
- `--dry-run`, `--list`, `--json`, `scan` never change the filesystem or git state. Default is Trash; `--permanent` exists for strip only.
- Freed space is reported as the measured statfs change; sizes count hard links once; `shared` = bytes removal would not free (hard links leaving the item; on macOS APFS clone/snapshot blocks via `ATTR_CMNEXT_PRIVATESIZE`, so `exclusive` is a lower bound there). Btrfs/XFS reflinks are not visible.
- Main checkouts and bare repos are never items. Symlinks are never followed or removed through. Path comparisons go through `internal/pathx` (case-folded on macOS).
- "Only here" commits = tips git's record of the worktree keeps (HEAD, its reflog, refs/worktree/*, refs/bisect/*) minus branches/tags/remotes; bundles are built in a throwaway bare repo with alternates, never by writing refs into the user's repo. Submodules (`<admin>/modules`) block retire. Unknown evidence → `unknown_state`.
- The contract only grows within a schema version.

## Release & state (family publishing rules: `../AGENTS.md`)
- Demos only via `demo/setup.sh` (fake HOME on a small disk image on macOS, so no real paths or disk sizes); `vhs demo/demo.tape` renders `demo/demo.gif` locally, never in CI. JSON output shows `~/` paths.
- fell writes no config, cache or state. Rescue folders go to the Trash; a staging dir (`$TMPDIR` on macOS, `$XDG_DATA_HOME` on Linux) exists only during a retire.
- Homebrew: source-build formula in `Matthew-Eucaristo/homebrew-tap` (`Formula/fell.rb`), bumped by hand per RELEASING.md.

## Verify
`gofmt -l .` (empty), `go vet ./...`, `GOOS=linux go vet ./...`, `go test -race ./...`; CI runs on macOS and Linux. Removal is tested only on temp repos with the trash swapped (`clean.moveFunc`). On real directories use `fell --list`, `fell scan --json` or `--dry-run`.
