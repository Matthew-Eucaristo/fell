#!/usr/bin/env bash
# Builds a synthetic workspace for the demo recording: fake HOME, one fake
# app repository with worktrees left by several agent tools, fixed git
# identity and prompt. Nothing from the real machine appears in the recording.
# Usage: source demo/setup.sh   (from the repository root)
set -e

DEMO="$(cd /tmp && pwd -P)/fell-demo"
[ -d "$DEMO" ] && chmod -R u+w "$DEMO" && rm -rf "$DEMO"
mkdir -p "$DEMO/bin" "$DEMO/home/dev"
go build -trimpath -o "$DEMO/bin/fell" .

export HOME="$DEMO/home" GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
export GIT_AUTHOR_NAME=demo GIT_AUTHOR_EMAIL=demo@example.com GIT_COMMITTER_NAME=demo GIT_COMMITTER_EMAIL=demo@example.com
blob() { mkdir -p "$1" && dd if=/dev/urandom of="$1/blob" bs=1048576 count="$2" status=none; }
days_ago() { date -u -d "@$(($(date +%s) - $1 * 86400))" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -v-"$1"d +%Y-%m-%dT%H:%M:%SZ; }
commit() { # dir days_ago message
  GIT_AUTHOR_DATE=$(days_ago "$2") GIT_COMMITTER_DATE=$(days_ago "$2") git -C "$1" commit -q --allow-empty -m "$3"
}

APP="$HOME/dev/shop"
mkdir -p "$APP/src"
git init -q -b main "$APP"
printf '{"name":"shop"}\n' > "$APP/package.json"
printf 'node_modules/\n.next/\n.env\n.claude/worktrees/\n' > "$APP/.gitignore"
echo 'export default function App() {}' > "$APP/src/app.js"
git -C "$APP" add . && commit "$APP" 200 init
git init -q --bare "$DEMO/origin.git"
git -C "$APP" remote add origin "$DEMO/origin.git"
git -C "$APP" push -q origin main

wt() { git -C "$APP" worktree add -q "$@"; }
stamp_ago() { local t=$(($(date +%s) - $1 * 86400)); date -d "@$t" +%Y%m%d%H%M 2>/dev/null || date -r "$t" +%Y%m%d%H%M; }
age() { # worktree-path admin-name days: make files and git's record look that old
  find "$1" "$APP/.git/worktrees/$2" -exec touch -h -t "$(stamp_ago "$3")" {} + 2>/dev/null
}

# Claude Code subagent lanes: detached, one with work that exists nowhere else.
wt --detach "$APP/.claude/worktrees/agent-a3f9c2e1b7d04a6e"
blob "$APP/.claude/worktrees/agent-a3f9c2e1b7d04a6e/node_modules" 180
commit "$APP/.claude/worktrees/agent-a3f9c2e1b7d04a6e" 95 "refactor checkout"
wt --detach "$APP/.claude/worktrees/agent-b81d0e44c2a95f13"
blob "$APP/.claude/worktrees/agent-b81d0e44c2a95f13/node_modules" 170
blob "$APP/.claude/worktrees/agent-b81d0e44c2a95f13/.next" 60

# Codex: managed worktree in its own home, marked with its thread file.
wt --detach "$HOME/.codex/worktrees/7c1e/shop"
blob "$HOME/.codex/worktrees/7c1e/shop/node_modules" 150
echo '{"version":1,"ownerThreadId":"demo"}' > "$APP/.git/worktrees/shop/codex-thread.json"

# Cursor parallel agent.
wt --detach "$HOME/.cursor/worktrees/shop/q4k2"
blob "$HOME/.cursor/worktrees/shop/q4k2/node_modules" 120

# Your own hotfix worktree, with an ignored .env and an uncommitted change.
wt -b hotfix/tax "$HOME/dev/shop-hotfix"
blob "$HOME/dev/shop-hotfix/node_modules" 90
echo 'STRIPE_KEY=sk_test_demo' > "$HOME/dev/shop-hotfix/.env"

age "$APP/.claude/worktrees/agent-a3f9c2e1b7d04a6e" agent-a3f9c2e1b7d04a6e 95
age "$APP/.claude/worktrees/agent-b81d0e44c2a95f13" agent-b81d0e44c2a95f13 140
age "$HOME/.codex/worktrees/7c1e/shop" shop 21
age "$HOME/.cursor/worktrees/shop/q4k2" q4k2 9
age "$HOME/dev/shop-hotfix" shop-hotfix 2
echo '// wip' >> "$HOME/dev/shop-hotfix/src/app.js"

# A worktree whose folder was deleted by hand: git still holds its commit.
wt -b spike "$HOME/dev/shop-spike"
commit "$HOME/dev/shop-spike" 40 "spike"
rm -rf "$HOME/dev/shop-spike"
find "$APP/.git/worktrees/shop-spike" -exec touch -h -t "$(stamp_ago 40)" {} +

export PATH="$DEMO/bin:/usr/bin:/bin" PS1='$ '
unset CODEX_HOME T3CODE_HOME XDG_CONFIG_HOME XDG_DATA_HOME
cd "$HOME"
set +e
