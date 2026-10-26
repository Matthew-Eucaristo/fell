// Package gitx runs git. Read is for queries and never writes into a
// repository; Write is for the few commands fell uses to change one
// (bundle, worktree prune, worktree unlock), and only internal/clean calls it.
package gitx

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// env keeps git from prompting, from writing optional locks or index
// refreshes, and from translating messages we parse.
func env() []string {
	return append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
}

// base flags: --no-optional-locks stops `git status` from refreshing the
// index, and core.fsmonitor=false keeps a repo's config from starting a hook.
var base = []string{"--no-optional-locks", "-c", "core.fsmonitor=false"}

// Read runs a read-only git query in dir and returns stdout.
func Read(dir string, args ...string) ([]byte, error) {
	return run(dir, nil, args...)
}

// ReadGitDir runs a read-only query against a git directory, for worktrees
// whose checkout is gone (the admin dir still knows HEAD).
func ReadGitDir(gitDir string, args ...string) ([]byte, error) {
	return run("", []string{"--git-dir=" + gitDir}, args...)
}

// Write runs a git command that changes repository state. Callers are
// limited to internal/clean.
func Write(dir string, args ...string) ([]byte, error) {
	return run(dir, nil, args...)
}

// WriteGitDir is Write against a git directory.
func WriteGitDir(gitDir string, args ...string) ([]byte, error) {
	return run("", []string{"--git-dir=" + gitDir}, args...)
}

func run(dir string, pre []string, args ...string) ([]byte, error) {
	full := append(append([]string{}, base...), pre...)
	if dir != "" {
		full = append(full, "-C", dir)
	}
	full = append(full, args...)
	cmd := exec.Command("git", full...)
	cmd.Env = env()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return out, fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return out, nil
}

// Available reports whether a usable git is on PATH.
func Available() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// ReadGitDirCombined is ReadGitDir returning stdout and stderr together, for
// commands that report on stderr (`worktree prune --dry-run -v`).
func ReadGitDirCombined(gitDir string, args ...string) ([]byte, error) {
	full := append(append([]string{}, base...), "--git-dir="+gitDir)
	cmd := exec.Command("git", append(full, args...)...)
	cmd.Env = env()
	return cmd.CombinedOutput()
}

// ReadGitDirStdin is ReadGitDir with input on stdin, for long revision
// lists (`rev-list --stdin`).
func ReadGitDirStdin(gitDir, stdin string, args ...string) ([]byte, error) {
	full := append(append([]string{}, base...), "--git-dir="+gitDir)
	cmd := exec.Command("git", append(full, args...)...)
	cmd.Env = env()
	cmd.Stdin = strings.NewReader(stdin)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return out, fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// WriteGitDirStdin is WriteGitDir with input on stdin.
func WriteGitDirStdin(gitDir, stdin string, args ...string) ([]byte, error) {
	return ReadGitDirStdin(gitDir, stdin, args...)
}
