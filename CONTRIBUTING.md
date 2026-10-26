# Contributing

Thanks for helping. fell aims to stay small, safe and easy to audit.

## Development

```sh
gofmt -l .
go vet ./...
GOOS=linux go vet ./...
go test -race ./...
go run . --dry-run
```

Tests build throwaway repositories and worktrees in temp dirs (`internal/testrepo`) and swap the trash for a temp folder. Never test removal against real directories; use `--dry-run`, `--list` or `scan --json` there.

## Ground rules

- **The official way only.** fell removes worktrees through git's documented path (move the checkout to the trash, then `git worktree prune`) and never with `git worktree remove --force`. A worktree an app still tracks is never removed by fell, not even with `--force`: fell shows how that app removes it.
- **Read, never write, other tools' state.** Vendor files (Claude Desktop's registry, Codex's markers, lock reasons) are parsed read-only. When a format is unknown or unparseable, fail closed: warn and treat the worktree as tracked or protected.
- All changes to the filesystem and to git go through `internal/clean`, and every target is re-read and checked by `guard.go` right before acting. Keep it that way.
- A new agent tool is one `Owner` in `internal/agents` (location, markers, processes, how it removes its own worktrees, and whether it manages them) plus an attribution test. Link its docs or source in the PR.
- A new artifact is one `ArtifactRule` in `internal/scan/artifacts.go` with a marker file, a test, and a README row saying what regenerates it.
- The JSON contract (`internal/contract`, `docs/contract.md`) only grows within a schema version: never rename or repurpose a field, reason code or exit code.
- Keep dependencies to Charm modules and `golang.org/x/sys` unless there is a strong reason.
- No real paths, usernames or project names in code, tests, docs, screenshots or demos. Use placeholders such as `~/dev/my_app`.
- Pull requests are squash-merged, so the PR title becomes the commit and the release-notes entry. Use a conventional prefix: `feat:`, `fix:`, `docs:`, `test:`, `ci:` or `chore:` (the last four are left out of release notes).

## Reporting security issues

See [SECURITY.md](SECURITY.md).
