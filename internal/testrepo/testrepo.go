// Package testrepo builds throwaway git repositories and worktrees for
// tests. Nothing here touches real user directories.
package testrepo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Dir returns a temp dir with symlinks resolved (/var -> /private/var on
// macOS), as fell's scan does.
func Dir(t testing.TB) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// Git runs git in dir with a fixed identity and returns trimmed stdout.
func Git(t testing.TB, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// Write creates a file (and its parents) under dir.
func Write(t testing.TB, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Repo creates <root>/<name> with a package.json, a .gitignore for
// node_modules/.env, one commit, and a bare "origin" remote it is pushed to.
func Repo(t testing.TB, root, name string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	Git(t, dir, "init", "-q", "-b", "main")
	Write(t, dir, "package.json", "{}\n")
	Write(t, dir, ".gitignore", "node_modules/\n.env\n.next/\n")
	Write(t, dir, "README.md", "demo\n")
	Git(t, dir, "add", ".")
	Git(t, dir, "commit", "-q", "-m", "init")
	origin := filepath.Join(root, name+"-origin.git")
	Git(t, root, "init", "-q", "--bare", origin)
	Git(t, dir, "remote", "add", "origin", origin)
	Git(t, dir, "push", "-q", "origin", "main")
	return dir
}

// Worktree adds a linked worktree at path; extra args go to `git worktree add`.
func Worktree(t testing.TB, repo, path string, extra ...string) string {
	t.Helper()
	Git(t, repo, append([]string{"worktree", "add", "-q"}, append(extra, path)...)...)
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return real
}

// NodeModules creates an ignored node_modules with a few files.
func NodeModules(t testing.TB, wt string) {
	t.Helper()
	Write(t, wt, "node_modules/left-pad/index.js", strings.Repeat("x", 9000))
	Write(t, wt, "node_modules/left-pad/package.json", "{}")
}
