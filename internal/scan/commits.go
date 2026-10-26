package scan

import (
	"strconv"
	"strings"

	"github.com/Matthew-Eucaristo/fell/internal/gitx"
)

// Tips are the commits a worktree's own git record keeps alive: HEAD, every
// entry of its HEAD reflog, and its per-worktree refs (refs/worktree/*,
// refs/bisect/*). Pruning the record drops all of them, so commits reachable
// only from these are lost unless bundled.
func Tips(admin string) ([]string, error) {
	seen := map[string]bool{}
	var tips []string
	add := func(out []byte) {
		for _, l := range strings.Fields(string(out)) {
			if len(l) >= 40 && !seen[l] {
				seen[l] = true
				tips = append(tips, l)
			}
		}
	}
	out, err := gitx.ReadGitDir(admin, "rev-parse", "--verify", "-q", "HEAD")
	if err != nil {
		return nil, err
	}
	add(out)
	if out, err := gitx.ReadGitDir(admin, "log", "-g", "--format=%H", "HEAD", "--"); err == nil {
		add(out) // no reflog is fine
	}
	out, err = gitx.ReadGitDir(admin, "for-each-ref", "--format=%(objectname)", "refs/worktree", "refs/bisect")
	if err != nil {
		return nil, err
	}
	add(out)
	return tips, nil
}

// KeptBy lists the object names of refs that survive pruning the worktree:
// branches (all, or all except one), tags and remote-tracking refs.
func KeptBy(common, exceptBranch string) ([]string, error) {
	out, err := gitx.ReadGitDir(common, "for-each-ref", "--format=%(refname) %(objectname)", "refs/heads", "refs/tags", "refs/remotes")
	if err != nil {
		return nil, err
	}
	var shas []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		name, sha, ok := strings.Cut(l, " ")
		if !ok || (exceptBranch != "" && name == "refs/heads/"+exceptBranch) {
			continue
		}
		shas = append(shas, sha)
	}
	return shas, nil
}

// RevList returns the stdin for `rev-list --stdin`: include tips, exclude
// the kept refs.
func RevList(tips, exclude []string) string {
	var b strings.Builder
	for _, t := range tips {
		b.WriteString(t + "\n")
	}
	for _, e := range exclude {
		b.WriteString("^" + e + "\n")
	}
	return b.String()
}

// onlyHere counts commits that pruning this worktree's record would orphan.
func onlyHere(w *Worktree) (int, error) {
	tips, err := Tips(w.Admin)
	if err != nil {
		return -1, err
	}
	kept, err := KeptBy(w.CommonDir, "")
	if err != nil {
		return -1, err
	}
	out, err := gitx.ReadGitDirStdin(w.Admin, RevList(tips, kept), "rev-list", "--count", "--stdin")
	if err != nil {
		return -1, err
	}
	return strconv.Atoi(strings.TrimSpace(string(out)))
}
