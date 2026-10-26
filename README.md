# fell

Find the git worktrees your AI coding agents left behind, see exactly what is in each one, and cut them down safely. Nothing is deleted permanently, and nothing an app still uses is touched.

```
 fell  ● trash  ·  807.5 MB in 6 worktrees  ·  807.4 MB rebuildable  ·  212.4 GB free  ·  ~/dev + agent locations

                         SIZE  REB.  ACTIVE    SAFETY     WORKTREE  (sort: size)
  ◐ Claude Code  2 worktree(s) · 429.9 MB
▌ ○ █████████████▓   241.2 MB   99%  4mo ago   ● safe      shop/agent-b81d0e44c2a95f13  detached
  ● ██████████▓···   188.8 MB   99%  3mo ago   ■ protected shop/agent-a3f9c2e1b7d04a6e  detached ↑1 only-here:1
  ○ Codex  1 worktree(s) · 157.3 MB
  ○ █████████▓····   157.3 MB   99%  3w ago    ■ protected shop/7c1e  detached app-tracked
  ○ Cursor  1 worktree(s) · 125.8 MB
  ○ ███████▓······   125.8 MB   99%  9d ago    ■ protected shop/q4k2  detached app-tracked
  ○ git (manual)  2 worktree(s) · 94.4 MB
  ○ █████▓········    94.4 MB   99%  today     ■ protected shop/shop-hotfix  hotfix/tax ✎1 secrets:1
  ○ ··············        0 B        5w ago    ▲ caution   shop/shop-spike  checkout missing ↑1
──────────────────────────────────────────────────────────────────────────────────────────────────────
  shop/agent-b81d0e44c2a95f13  Claude Code · detached at 7b4a0f5f · last git activity 4mo ago
  ~/dev/shop/.claude/worktrees/agent-b81d0e44c2a95f13  [wt-ac80840e6477]
  rebuildable: .next 62.9 MB, node_modules 178.3 MB  ·  commits: all on a remote
  safe  strip ✓  retire ✓
  1 selected (188.8 MB)
  ↑↓ move  space select  a all  s strip  r retire  tab group  o sort  R rescan  ? legend  q quit
```

*To fell* is to cut down a tree. Claude Code, Codex, Cursor, Conductor and friends now give every chat, session or subagent its own git worktree. Each one is a full checkout with its own `node_modules` and build output, and they pile up in half a dozen places. Some are detached HEADs that `git branch` never shows. Vendor cleanups either leak them or delete live work and git-ignored files without a trace. `fell` is the careful middle:

- **Sees every worktree, attributed.** It walks your code roots and every known agent location, asks git (`git worktree list --porcelain`), and labels each worktree with the tool that made it: Claude Code, Claude Desktop, Codex, Cursor, Conductor, OpenCode, Kilo Code, Hermes, Gemini CLI, VS Code and Copilot, and more, or you.
- **Shows the evidence.** For each worktree: size and how much of it is rebuildable (`node_modules`, `.venv`, `target`, build output), uncommitted changes, commits that are on no remote (including on detached HEADs) and commits that exist *only* in that worktree, git-ignored secrets and data (`.env*`, local databases), last activity, processes working inside it, and locks and missing checkouts.
- **Strip.** It moves the rebuildable artifacts to the Trash and keeps the worktree. This is the safe answer for a worktree that still holds work.
- **Retire.** It first writes a rescue folder to the Trash: a verified `git bundle` of the commits only that worktree has, a patch of uncommitted changes, copies of ignored secrets, and a `RESTORE.md`. Then it moves the checkout to the Trash and lets `git worktree prune` drop git's record. Branches are kept.
- **Respects other tools.** It never edits another tool's files and never touches a worktree whose app or session is running. It never removes a worktree an app still tracks; it tells you how that app removes it. When it can't read a tool's format, it fails closed.
- **Honest numbers.** Hard links (pnpm) count once. On macOS, blocks shared with APFS clones or snapshots are reported as shared, using the filesystem's own private-size attribute. Freed space is *measured* from the disk, not assumed.
- **Scriptable.** `--list`, `--json`, `--ndjson`, `--dry-run`, and a stable [machine contract](docs/contract.md) with IDs and fingerprints, so other tools can drive it without bypassing its checks.

## Install

**Homebrew** (macOS, Linux):

```sh
brew install matthew-eucaristo/tap/fell
```

**Go** (1.26+):

```sh
go install github.com/Matthew-Eucaristo/fell@latest
```

Prebuilt archives for macOS and Linux (amd64, arm64) are on the [releases page](https://github.com/Matthew-Eucaristo/fell/releases). fell needs `git` on `PATH`.

## Usage

```sh
fell                         # interactive; scans ~/dev (or the current dir) plus agent locations
fell ~/code ~/work           # other roots
fell --dry-run               # try everything, change nothing
fell --list                  # table and exit (also the default when not in a terminal)
fell scan --json             # machine-readable scan; never changes anything
fell strip wt-ac80840e6477   # trash one worktree's node_modules & co., keep the worktree
fell retire --dry-run ~/dev/shop/.claude/worktrees/agent-b81d0e44c2a95f13
fell retire --force wt-f303f1dcea41   # retire although commits exist only there (they are bundled)
fell capabilities --json     # actions, reason codes, exit codes
```

| Key | Action |
| --- | --- |
| `↑` `↓` / `j` `k`, `PgUp` `PgDn`, `Home` `End` | Move |
| `space` | Select a worktree, or a whole group on its header |
| `a` | Select every worktree that is not live |
| `s` | Strip the selection (or the row under the cursor) |
| `r` | Retire the selection. The confirmation lists what runs, what is saved, and what is refused and why; `f` toggles force, `u` toggles unlock |
| `tab` | Group by agent or by repository |
| `o` | Sort by size, oldest activity, or name |
| `R` | Rescan |
| `?` | Legend |
| `q` / `esc` | Quit and print what was done |

| Flag | Applies to | Meaning |
| --- | --- | --- |
| `--dry-run` | all | Run every check and report; change nothing |
| `--list`, `--json` | `fell` | Print and exit |
| `--json`, `--ndjson` | `scan` | One document, or a header + one line per worktree as it is measured + a summary |
| `--no-agents` | all | Scan only the given roots, not agent locations |
| `--force` | strip, retire, apply | Override refusals that can be overridden: dirty, in use, commits only here, unknown state. Never live, app-tracked or keep-marked worktrees |
| `--unlock` | retire, apply | Allow `git worktree unlock` when the lock's owner is not running |
| `--permanent` | strip, apply | Delete artifacts instead of moving them to the Trash (retire never deletes) |
| `--root a,b` | strip, retire | Roots to search when resolving IDs (paths resolve on their own) |
| `--plan file\|-` | apply | Run a JSON plan of IDs and fingerprints from a scan |
| `--version` | `fell` | Print the version |

## Which worktrees it knows

| Owner | Recognised by | fell's stance | How that tool removes them (the official way) |
| --- | --- | --- | --- |
| Claude Code | `<repo>/.claude/worktrees/…`, `CLAUDE_BASE` marker, lock `claude session/agent … (pid N …)` | Strip and retire unless its session is live | Removes clean, pushed agent worktrees itself after `cleanupPeriodDays`; offers removal on exit |
| Claude Desktop | Entry in its `git-worktrees.json` pool, `claude-desktop-worktree` marker | Strip only; live while the app runs or a session leases it | Settings → storage cleanup, or archive the session |
| Codex | `$CODEX_HOME/worktrees/<id>/<repo>` (or `git-worktree-root`), `codex-thread.json` marker | Strip only | Archive the chat; keeps `worktree-keep-count` newest |
| Cursor | `~/.cursor/worktrees/<repo>/<id>` | Strip only | `/delete-worktree`; prunes past `cursor.worktreeMaxCount` |
| Conductor | `~/conductor/workspaces/<repo>/<name>`, `<repo>/.conductor/` | Strip only | Archive the workspace |
| OpenCode | `$XDG_DATA_HOME/opencode/worktree/…` | Strip only | TUI `/worktrees` → ctrl+d, or Settings → Worktrees |
| Kilo Code | `$XDG_DATA_HOME/kilo/worktree…`, `<repo>/.kilo/worktrees/`, `kilo-agent-manager-metadata.json` | Strip only | `kilo worktree remove <name>`, or Delete worktree |
| Hermes Agent | `<repo>/.worktrees/hermes-…`, `hermes/` branches, lock `hermes pid=N …` | Strip only | Its own sweep; `hermes worktree prune` |
| VS Code agent sessions | `<repo>.worktrees/…` on `…agents/…` branches | Strip only | Archive or delete the session |
| Superset · Sculptor · Devin Desktop/Windsurf | `~/.superset/worktrees`, `~/.sculptor/workspaces`, `~/.windsurf/worktrees` | Strip only | Archive/delete the workspace; `sculpt workspace delete`; delete the Cascade conversation |
| T3 Code · Vibe Kanban | `$T3CODE_HOME/worktrees` (`~/.t3/worktrees`); `$TMPDIR/vibe-kanban/worktrees`, `.vibe-kanban-workspaces/` | Strip only | Settings → Storage → Delete now; delete the workspace |
| Gemini CLI · Copilot CLI · Cline · Roo Code · Warp | `<repo>/.gemini/worktrees/`; `<repo>.worktrees/`; `~/Documents/Cline/Worktrees`, `~/.cline/worktrees`; `~/.roo/worktrees`; `~/.warp/worktrees` | Strip and retire (these tools keep no record) | `git worktree remove`, or the tool's Delete worktree button |
| git (manual, Worktrunk, gtr, …) | Anything else `git worktree list` reports | Strip and retire | `git worktree remove` (`wt remove`, `git gtr rm`) |

"Strip only" means the app keeps its own record of the worktree, so fell never retires it, even with `--force`. Strip is blocked as well while the app (or a session in the worktree) is running. Directories another tool is in the middle of deleting (`.ccd-trash~*`, `.kilo-delete-*`, Worktrunk's `wt/trash`) are left alone. Aider, Crush, Goose, Kiro and Jules don't create local worktrees.

Missing a tool? [Open a feature request](https://github.com/Matthew-Eucaristo/fell/issues/new?template=feature_request.md) with where it puts worktrees and how it removes them.

## What strip moves

Only directories that match the catalog **and** that git ignores with nothing tracked inside, never through a symlink:

| Directory | Next to | Comes back with |
| --- | --- | --- |
| `node_modules`, `.next`, `.nuxt`, `.svelte-kit`, `.turbo`, `.parcel-cache`, `.expo`, `dist`, `build` | `package.json` (and similar) | `npm`/`pnpm`/`yarn`/`bun install`, the next build |
| `.venv`, `venv` (with `pyvenv.cfg`), `.tox`, `.pytest_cache`, `.mypy_cache`, `.ruff_cache` | — | `uv sync` / `pip install`, the tool's next run |
| `target` | `Cargo.toml`, `pom.xml` | `cargo build` / `mvn package` |
| `.dart_tool`, `build` | `pubspec.yaml` | `flutter pub get`, the next build |
| `.gradle`, `.cxx` | `build.gradle(.kts)` | the next Gradle build |
| `Pods` | `Podfile` | `pod install` |
| `.build` | `Package.swift` | `swift build` |
| `vendor` | `composer.json` | `composer install` |
| `_build`, `deps` · `.zig-cache`, `zig-out` · `.stack-work` · `.angular` | `mix.exs` · `build.zig` · `stack.yaml` · `angular.json` | the matching build tool |

A symlinked `node_modules` (Claude Code's `worktree.symlinkDirectories`) is never followed: it belongs to the main checkout.

## Safety model

Every action re-reads the worktree right before acting and checks it against what you saw. The checks live in [`internal/clean`](internal/clean), the only code that changes anything.

1. **Fresh evidence, pinned to the scan.** Each worktree has a fingerprint covering HEAD, branch, uncommitted changes, locks, app tracking, ignored secrets and the directory's identity. If any of that changed since the scan, the action is refused.
2. **Live means hands off.** A worktree is live if any of these hold: a Claude Code lock names a running process (PID and start time checked); Claude Desktop has leased it to a session; its owning app is running while it tracks the worktree; or an agent process is working inside it. Nothing, not even `--force`, acts on a live worktree.
3. **The owner's way, not ours.** A worktree an app still tracks is never retired. fell shows how that app removes it. Strip stays available while the app isn't running. Claude Desktop's registry is read-only, and if it is unreadable or uses a schema fell doesn't know, Claude worktrees are treated as tracked.
4. **Never lose work.** Retire refuses worktrees that are dirty, in use, or hold commits found nowhere else, unless you pass `--force`. "Found nowhere else" covers HEAD, the worktree's own reflog (a commit you `reset` away) and its per-worktree refs. Even with `--force`, retire bundles those commits first (verified with `git bundle verify`, built in a throwaway repository so no ref is added to yours), saves a patch of the uncommitted changes, and copies ignored secrets. Worktrees with submodules are never retired: their repositories live in git's record of the worktree. When fell can't tell what would be lost, it treats the worktree as protected. Locked worktrees need `--unlock`, and fell never unlocks a lock whose owner is running. A `.worktree-keep` marker, or another repository nested inside, blocks retire.
5. **Git's documented removal.** fell does not use `git worktree remove`, which deletes ignored files permanently. The checkout moves to the Trash, and then `git worktree prune` removes git's record. That is the path git documents for a worktree deleted outside git. fell prunes only when the dry run shows that nothing but this worktree's entry would go; otherwise it leaves the record and tells you. It never deletes branches and never edits `.git` internals.
6. **Guards.** Main checkouts and bare repositories are never items. A checkout is refused if it is a symlink, your home directory or an ancestor of it, contains the main checkout or the git directory, or doesn't point back at git's record. Artifacts are refused if git tracks anything inside them, doesn't ignore them, or hides a repository anywhere below (an editable install or a stray clone in `node_modules`, `.venv` or `vendor`). Paths are compared case-insensitively on macOS, so a different spelling can't hide a running process.
7. **Trash by default.** On macOS, `/usr/bin/trash` (macOS 15+) or Finder; on Linux, the freedesktop.org home trash, refusing cross-filesystem moves rather than copying. `--permanent` exists only for strip. `--dry-run`, `--list`, `--json` and `scan` change nothing.
8. **Read-only scanning.** git runs with `--no-optional-locks` and `core.fsmonitor=false`, so scanning doesn't even refresh an index.

### Restoring a retired worktree

The Trash holds the checkout (named after the worktree, with every untracked and ignored file inside) and a `fell-rescue-<repo>-<worktree>-<time>` folder:

```
fell-rescue-shop-agent-a3f9c2e1b7d04a6e-20261027-101500/
  RESTORE.md          exact commands for this worktree
  manifest.json       what was saved, HEAD, branch, paths
  commits.bundle      commits no branch, tag or remote had (if any)
  uncommitted.patch   changes to tracked files (if any)
  untracked.txt       untracked files (they are in the trashed checkout)
  precious/.env       copies of git-ignored secrets and local data
```

```sh
git -C ~/dev/shop fetch ~/.Trash/fell-rescue-…/commits.bundle HEAD:refs/heads/fell-rescue/agent-a3f9
git -C ~/dev/shop worktree add ~/dev/shop-restored fell-rescue/agent-a3f9
```

Emptying the Trash is your explicit act of forgetting.

## Sizes and freed space

Sizes are allocated blocks like `du`. A file hard-linked several times counts once. Bytes that removing a worktree would not free are reported as *shared*. That covers hard links reaching outside it (a pnpm store, another worktree) and, on macOS, blocks APFS shares with clones or local snapshots: fell asks the filesystem for each file's private size (`ATTR_CMNEXT_PRIVATESIZE`). Claude Desktop, for example, clones files into its worktrees, and pnpm clones from its store. "Freeable" (exclusive) is therefore a conservative lower bound on macOS. Btrfs and XFS reflinks on Linux are not visible and count in full. After acting, fell reports the **measured** change in free disk space. Moving to the Trash frees nothing until you empty it, and Time Machine local snapshots can hold space a while longer. fell says so instead of claiming a number.

## For tools and scripts

`fell scan --json` and `fell capabilities --json` follow a versioned contract, and `fell apply --plan` executes a list of `{id, fingerprint, action}` items, refusing any that changed since the scan. Distinct exit codes report `refused` (3) and `partial` (4). See [docs/contract.md](docs/contract.md). Any cleanup tool can adopt the same shape.

```sh
fell scan --json | jq -r '.items[] | select(.safety.level=="safe") | {id, fingerprint, action:"strip"}' \
  | jq -s '{schema_version:1, items:.}' | fell apply --plan - --dry-run
```

## Privacy and uninstall

fell writes no config, cache, log or state files and makes no network requests. The only things it changes are the ones you choose: artifacts and checkouts moved to the Trash, rescue folders written to the Trash, and git records pruned. During a retire, a short-lived staging folder exists in `$TMPDIR` (macOS) or `$XDG_DATA_HOME` (Linux), and fell removes it once the rescue folder is in the Trash. On Linux, the first move creates the standard `~/.local/share/Trash` folders if they are missing. JSON output writes paths under your home as `~/…`.

To remove fell completely:

```sh
brew uninstall fell
brew autoremove                     # removes Go if it was installed only to build fell
brew cleanup --prune=all            # optional: clears Homebrew's own download and build cache, including fell's source tarball
brew untap matthew-eucaristo/tap    # only if nothing else is installed from this tap
```

Installed with `go install`? Run `rm "$(go env GOPATH)/bin/fell"`. Anything fell moved to the Trash stays there until you empty it or put it back.

## How it compares

| | fell | `git worktree remove` | agent built-in cleanup | lopper / bonsai | Mole `purge` |
| --- | --- | --- | --- | --- | --- |
| Finds worktrees across repos and agents | ✓ | one repo | own only | ✓ | inside known containers |
| Detached-HEAD commits found only there | ✓ bundled | lost (with `--force`) | varies | flagged | n/a |
| Ignored secrets and data | ✓ saved and kept in Trash | deleted | often deleted | deleted | n/a |
| Respects app-tracked and live worktrees | ✓ | — | own only | partly | n/a |
| Strip artifacts but keep the worktree | ✓ | — | — | — | ✓ (permanent) |
| Default action | Trash | permanent | permanent | permanent | permanent |
| Measured freed space | ✓ | — | — | — | — |

## Development

```sh
gofmt -l . && go vet ./... && go test -race ./...
go run . --dry-run
source demo/setup.sh && fell        # synthetic workspace; vhs demo/demo.tape records the GIF
```

See [CONTRIBUTING.md](CONTRIBUTING.md) and [AGENTS.md](AGENTS.md). Security reports: [SECURITY.md](SECURITY.md). Release notes: [GitHub Releases](https://github.com/Matthew-Eucaristo/fell/releases).

## License

[MIT](LICENSE)
