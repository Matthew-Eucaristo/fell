# fell machine contract (schema_version 1)

fell exposes a small, stable JSON interface so that other programs (an umbrella TUI, scripts, agents) can show what fell sees and ask it to act, without fell's guards being bypassed. The contract is meant to be reusable: any cleanup tool can adopt the same shape (`scan` → `plan` → `apply`, plus `capabilities`) and become discoverable the same way.

## Principles

1. **The tool that knows the domain does the removal.** A caller never deletes paths from a scan; it hands a plan back to the tool, which re-reads every item and re-applies its own rules.
2. **Plans are pinned to what the user saw.** Each item carries a `fingerprint` of its safety-relevant state. If the state changed since the scan (a new commit, new uncommitted work, a lock, an app starting to track it), the item is refused as stale.
3. **Evidence, not just bytes.** Items carry the reasons behind their safety level, so a caller can explain the "why" without re-implementing the checks.
4. **No usernames in output.** Paths under the home directory are written as `~/…`.
5. **Additive evolution.** Within a schema version, fields, reason codes and exit codes may be added but never renamed, removed or given a new meaning. Consumers must ignore unknown fields.

## Discovery: `fell capabilities --json`

```json
{
  "schema_version": 1,
  "tool": { "name": "fell", "version": "0.1.0", "ecosystem": "git-worktrees" },
  "commands": { "scan": "fell scan --json [--ndjson] [--no-agents] [root...]", "apply": "fell apply --plan <file|-> …", "capabilities": "fell capabilities --json" },
  "actions": [
    { "name": "strip", "reversible": true, "description": "…" },
    { "name": "retire", "reversible": true, "description": "…" }
  ],
  "safety_levels": ["safe", "caution", "protected", "live"],
  "reason_codes": [{ "code": "dirty", "level": "protected", "effects": ["retire needs --force"] }],
  "owners": [{ "id": "claude-code", "name": "Claude Code", "app_managed": false, "how_to_remove": "…" }],
  "artifact_kinds": [{ "name": "node_modules", "regenerate": "npm / pnpm / yarn / bun install" }],
  "exit_codes": { "ok": 0, "error": 1, "usage": 2, "refused": 3, "partial": 4 },
  "trash": "Trash"
}
```

An umbrella finds tools by looking for executables on `PATH` that answer `<tool> capabilities --json` with a `schema_version` it supports.

## Scan: `fell scan --json [root...]`

Read-only. Roots default to `~/dev`; known agent locations (Codex, Cursor, Conductor, OpenCode, Kilo, Superset, Sculptor, Windsurf, T3 Code, Vibe Kanban and others, plus Claude Desktop's registry) are always included unless `--no-agents` is passed.

```json
{
  "schema_version": 1,
  "tool": { "name": "fell", "version": "0.1.0", "ecosystem": "git-worktrees" },
  "generated_at": "2026-10-27T05:00:00Z",
  "roots": ["~/dev"],
  "warnings": [],
  "summary": {
    "items": 3,
    "bytes": { "allocated": 2788884480, "shared": 0, "exclusive": 2788884480, "file_count": 95219 },
    "strippable_bytes": { "allocated": 2458939392, "shared": 0, "exclusive": 2458939392, "file_count": 89254 },
    "by_safety_level": { "safe": 1, "caution": 1, "live": 1 },
    "by_owner": { "claude-code": 1, "claude-desktop": 1, "git": 1 }
  },
  "items": [ /* Item */ ]
}
```

### Item

| Field | Meaning |
| --- | --- |
| `id` | Stable ID (`wt-` + hash of the checkout path). Use it in plans. |
| `fingerprint` | Hash of the safety-relevant state: kind, path, git record, HEAD, branch, uncommitted changes, lock, prunable state, keep marker, app tracking, precious files, nested repositories, directory identity. Sizes and activity are excluded, so a build writing into `node_modules` does not invalidate a plan. |
| `kind` | `worktree` (registered, checkout present), `prunable` (registered, checkout missing; git still holds its HEAD), `orphan` (checkout whose git record is gone). |
| `path`, `name` | Checkout path (`~/…`) and its base name. |
| `project` | `{root, name}`: the main checkout and repository name. |
| `owner` | `{id, name, tracked, live, how_to_remove}`. `id` is one of the owner IDs listed by `capabilities` (`claude-code`, `claude-desktop`, `codex`, `cursor`, `conductor`, `opencode`, `kilo`, `hermes`, `gemini-cli`, `vscode`, `copilot-cli`, `cline`, `roo-code`, `warp`, `superset`, `sculptor`, `windsurf`, `t3code`, `vibe-kanban`, `git`; more may be added). `tracked` means the owning app keeps its own record of the worktree; `live` means its app or session is running for it. `how_to_remove` is the owner's official removal path. |
| `measured` | `false` until sizes are known (always `true` in `--json`, per item in `--ndjson`). |
| `bytes` | `{allocated, shared, exclusive, file_count}` for the whole checkout. Allocated blocks like `du`; hard-linked files count once; `shared` is held by hard links outside the worktree (a pnpm store, another worktree), so removing it frees nothing; `exclusive = allocated − shared`. |
| `strippable_bytes` | Same shape, for the eligible artifacts only. |
| `ignored_other_bytes` | Git-ignored bytes that are not rebuildable artifacts (local databases, generated media). Retire keeps them in the trashed checkout. |
| `size_note` | How sizes were computed. On macOS `shared` includes blocks shared with APFS clones and local snapshots (from `ATTR_CMNEXT_PRIVATESIZE`), so `exclusive` is a conservative lower bound; Btrfs/XFS reflinks count in full. Treat sizes as estimates and use `freed_bytes_measured` from apply. |
| `artifacts[]` | `{id, path (relative), kind, regenerate, eligible, bytes}`. `eligible` = git ignores it, tracks nothing inside, and no repository lies below it, so strip may move it. |
| `git` | `{head, branch, detached, dirty{staged,modified,untracked,conflicts}, has_remote, unpushed, only_here, locked, lock_reason, prunable, keep_marker}`. `unpushed` = commits on HEAD not reachable from any remote ref (works for detached HEADs); `-1` when unknown or there is no remote. `only_here` = commits reachable from no branch, tag or remote but kept by this worktree's record (HEAD, its reflog, `refs/worktree/*`, `refs/bisect/*`), which pruning the record would orphan. |
| `precious[]` | Git-ignored files that are usually irreplaceable (`.env*`, `.envrc`, `.dev.vars`, `*.local.json`, keys, SQLite databases): `{path, bytes, symlink, at_risk}`. |
| `activity` | `{last, source, age_days}`; `source` is `commit`, `git` (index or HEAD changes in git's record) or `files`. |
| `liveness` | `{cwd_known, in_use, processes[{pid, command}], owner_live, why}`. |
| `safety` | `{level, reasons[{code, detail}]}`. |
| `actions[]` | One decision per action: `{action, allowed, needs_override[], needs_unlock, blocked[]}`. `allowed` = may run with no flags; `needs_override` = codes `--force` would override; `blocked` = codes nothing overrides. |
| `errors[]` | Non-fatal problems reading the item. |

### Safety levels and reason codes

| Level | Meaning |
| --- | --- |
| `safe` | Retire allowed as is; nothing would be lost. |
| `caution` | Allowed; something is saved to the rescue folder or is worth a look. |
| `protected` | Retire needs `--force` / `--unlock`, or is blocked. |
| `live` | The owning app or session is running for it. Nothing is allowed. |

| Code | Level | Effect |
| --- | --- | --- |
| `live` | live | blocks strip and retire (lock owned by a running agent session, a Claude Desktop lease, the owning app running while it tracks the worktree, or an agent process working inside it) |
| `nested_worktree` | protected | blocks retire (another repository or worktree inside) |
| `submodules` | protected | blocks retire (its submodule repositories live in git's record of the worktree, which pruning deletes) |
| `app_tracked` | protected | blocks retire; remove it the owning app's way (`owner.how_to_remove`) |
| `keep_marker` | protected | blocks retire (`.worktree-keep` exists) |
| `in_use` | protected | strip and retire need `--force` (a process's working directory is inside) |
| `liveness_unknown` | protected | strip and retire need `--force` (process working directories could not be read) |
| `dirty` | protected | retire needs `--force`; changes are saved as a patch and stay in the trashed checkout |
| `only_here` | protected | retire needs `--force`; the commits are bundled |
| `unknown_state` | protected | retire needs `--force`; strip unavailable for orphans (orphaned checkout, unreadable git state, commits that could not be counted, a tree too large to check) |
| `locked` | protected | retire needs `--unlock` (the lock's owner is not running, or the lock is not an agent's) |
| `unpushed` | caution | bundled when no branch keeps the commits |
| `no_remote` | caution | informational |
| `recent` | caution | changed in the last 7 days (newest of the HEAD commit, git activity in the worktree's record such as checkouts and the HEAD reflog, and non-ignored file changes); an agent session may still resume in it. Same window as nodekill's `recent`; tools driving fell should not act on it in bulk |
| `ignored_precious` | caution | copied to the rescue folder |
| `ignored_data` | caution | ≥ 10 MB of ignored non-artifact data; stays in the trashed checkout |

### Streaming: `fell scan --ndjson`

One JSON object per line, each with a `type`:

```
{"type":"header","schema_version":1,"tool":{…},"roots":["~/dev"],"warnings":[],"items":3}
{"type":"item", …Item…}            ← one per worktree, as soon as it is measured
{"type":"summary", …Summary…}
```

## Plan and apply: `fell apply --plan <file|-> [flags] [root...]`

```json
{
  "schema_version": 1,
  "items": [
    { "id": "wt-572bf2da6e83", "fingerprint": "fp-7bc4c3f502b81124", "action": "strip" },
    { "id": "wt-65bbfafb3d5f", "fingerprint": "fp-0d1c…", "action": "strip", "artifacts": ["art-1a2b3c4d5e6f"] },
    { "id": "wt-78a87af7f8b7", "fingerprint": "fp-91aa…", "action": "retire" }
  ]
}
```

- `id` and `fingerprint` come from a scan; `action` is `strip` or `retire`; `artifacts` (strip only) limits the strip to these artifact IDs.
- Unknown fields are rejected, so a plan cannot smuggle options. Consent is given only by flags on the command line, by the user: `--dry-run`, `--force`, `--unlock`, `--permanent` (strip only).
- Pass the same roots as the scan (or none for the default) so fell can find every ID.
- Items run one at a time. For each one, fell rescans it, refuses it if the fingerprint changed, re-applies the rules and guards, and only then acts.

### Result (`--json`)

```json
{
  "schema_version": 1,
  "tool": { "name": "fell", "version": "0.1.0", "ecosystem": "git-worktrees" },
  "mode": "trash",
  "results": [
    {
      "id": "wt-78a87af7f8b7",
      "action": "retire",
      "path": "~/dev/app/.claude/worktrees/agent-a1b2",
      "status": "done",
      "trashed": ["~/dev/app/.claude/worktrees/agent-a1b2"],
      "rescue": { "folder": "fell-rescue-app-agent-a1b2-20261027-050000", "bundle_commits": 2, "uncommitted_patch": false, "precious_saved": [".env"], "untracked_files": 0 },
      "estimated_bytes": 945426432,
      "freed_bytes_measured": 0,
      "pruned": true,
      "restore_hint": "…"
    }
  ],
  "summary": { "done": 1, "dry_run": 0, "refused": 0, "failed": 0, "estimated_bytes": 945426432, "freed_bytes_measured": 0 }
}
```

- `status`: `done`, `dry_run`, `refused` (rules said no, or the item is stale or missing; nothing changed), `failed` (an error after checks passed; `error` says what was and was not done).
- `reasons` lists the codes behind a refusal; `error` explains it in words.
- `estimated_bytes` is the scan's allocated size of what moved. `freed_bytes_measured` is the measured change in free space on that filesystem (`-1` if unknown). In the default trash mode it stays near zero until the trash is emptied; that is expected and is reported honestly rather than claimed.
- `pruned` / `prune_note`: fell runs `git worktree prune` only when the entries it would remove are exactly this worktree's; otherwise it leaves the record and says why.

### Exit codes

| Code | Meaning |
| --- | --- |
| 0 | Every item done (or dry run). |
| 1 | Error before acting (bad roots, git missing). |
| 2 | Usage error (bad flags, invalid plan). |
| 3 | At least one item refused, none failed. |
| 4 | At least one item failed (partial). |

`fell strip` and `fell retire` with IDs or paths print the same result document with `--json` and use the same exit codes.

## Adopting the contract in another tool

Keep the envelope (`schema_version`, `tool{name, version, ecosystem}`, `generated_at`, `roots`, `warnings`, `summary`, `items[]`), the item core (`id`, `fingerprint`, `kind`, `path`, `owner`, `bytes`, `safety`, `actions`, `errors`) and the apply result (`status`, `estimated_bytes`, `freed_bytes_measured`, `restore_hint`), and the exit codes. Add domain fields beside them (fell adds `git`, `artifacts`, `precious`, `liveness`). Define your own reason codes, and list them with their effects in `capabilities` so callers can show them without knowing them in advance.
