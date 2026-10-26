package scan

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/Matthew-Eucaristo/fell/internal/pathx"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Matthew-Eucaristo/fell/internal/agents"
	"github.com/Matthew-Eucaristo/fell/internal/gitx"
	"github.com/Matthew-Eucaristo/fell/internal/procs"
)

var errNoGit = errors.New("git not found on PATH")

// gather fills in everything except sizes. all lists every item's path, to
// find worktrees nested inside this one.
func gather(w *Worktree, env agents.Env, reg *agents.Registry, pt *procs.Table, all []string) {
	w.Owner = env.Attribute(agents.Candidate{Path: w.Path, Admin: w.Admin, Branch: w.Branch, LockReason: w.LockReason, MainPath: w.MainPath}, reg)
	w.Record = reg.Lookup(w.Path)
	switch {
	case w.Record != nil:
		w.Tracked = true
	case w.Owner.Managed:
		w.Tracked = true
	case !reg.Supported() && (w.Owner.ID == agents.ClaudeDesktop.ID || w.Owner.ID == agents.ClaudeCode.ID):
		// Fail closed: Desktop may track it in a registry fell cannot read.
		w.Tracked = true
		w.Errors = append(w.Errors, "Claude Desktop's registry is unreadable; treated as app-tracked")
	}
	for _, p := range all {
		if pathx.Within(w.Path, p) {
			w.Nested = append(w.Nested, p)
		}
	}
	if w.Kind == KindWorktree {
		w.Keep = exists(filepath.Join(w.Path, agents.KeepMarker))
	}
	// Submodules of a linked worktree keep their whole repository in git's
	// record of it, which pruning deletes.
	w.Submodules = w.Admin != "" && exists(filepath.Join(w.Admin, "modules"))

	gitEvidence(w)
	if w.Kind == KindWorktree {
		w.ignored = ignoredSet(w.Path)
		lightWalk(w)
	} else if w.Kind == KindOrphan {
		lightWalk(w)
	}
	liveness(w, pt)
	activity(w)
	w.computeIDs()
	w.Classify()
}

// gitEvidence reads changes and commit reachability.
func gitEvidence(w *Worktree) {
	if w.Kind == KindOrphan || w.Admin == "" && w.Kind == KindPrunable {
		return
	}
	read := func(args ...string) ([]byte, error) {
		if w.Kind == KindWorktree {
			return gitx.Read(w.Path, args...)
		}
		return gitx.ReadGitDir(w.Admin, args...)
	}
	if w.Kind == KindWorktree {
		out, err := read("status", "--porcelain=v1", "-z", "--untracked-files=normal")
		if err != nil {
			w.Errors = append(w.Errors, err.Error())
			w.Head = ""
			return
		}
		w.Dirty = parseStatus(out)
		sum := sha256.Sum256(out)
		w.StatusHash = hex.EncodeToString(sum[:8])
	}
	if w.Head == "" {
		return
	}
	if w.HasRemotes {
		out, err := read("rev-list", "--count", "HEAD", "--not", "--remotes")
		if n, perr := strconv.Atoi(strings.TrimSpace(string(out))); err == nil && perr == nil {
			w.Unpushed = n
		} else {
			w.Errors = append(w.Errors, "counting unpushed commits failed")
		}
	}
	if w.Admin == "" {
		return
	}
	n, err := onlyHere(w)
	if err != nil {
		w.Errors = append(w.Errors, "finding commits only this worktree holds: "+err.Error())
		n = -1
	}
	w.OnlyHere = n
}

// parseStatus counts `git status --porcelain=v1 -z` entries.
func parseStatus(out []byte) Dirty {
	var d Dirty
	fields := strings.Split(string(out), "\x00")
	for i := 0; i < len(fields); i++ {
		e := fields[i]
		if len(e) < 3 {
			continue
		}
		x, y := e[0], e[1]
		switch {
		case x == '?' && y == '?':
			d.Untracked++
		case x == 'U' || y == 'U' || (x == 'A' && y == 'A') || (x == 'D' && y == 'D'):
			d.Conflicts++
		default:
			if x != ' ' {
				d.Staged++
			}
			if y != ' ' {
				d.Modified++
			}
		}
		if x == 'R' || x == 'C' {
			i++ // the next field is the rename source
		}
	}
	return d
}

// ignoredSet lists git-ignored paths, collapsing ignored directories to
// "dir/" so huge trees are not enumerated.
func ignoredSet(dir string) map[string]bool {
	out, err := gitx.Read(dir, "ls-files", "--others", "--ignored", "--exclude-standard", "--directory", "-z")
	if err != nil {
		return nil
	}
	m := map[string]bool{}
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			m[p] = true
		}
	}
	return m
}

// isIgnored reports whether rel (slash-separated) or a directory above it
// is in the ignored set.
func isIgnored(set map[string]bool, rel string, dir bool) bool {
	if set == nil {
		return false
	}
	if set[rel] || (dir && set[rel+"/"]) {
		return true
	}
	for i := len(rel) - 1; i > 0; i-- {
		if rel[i] == '/' && set[rel[:i+1]] {
			return true
		}
	}
	return false
}

// liveness decides whether the owning app or session is running.
func liveness(w *Worktree, pt *procs.Table) {
	w.CwdKnown = pt.CwdKnown
	if w.Kind == KindWorktree {
		w.InUse = pt.Inside(w.Path)
	}
	if w.Locked {
		if _, pid, start, ok := agents.LockOwner(w.LockReason); ok && pid > 0 {
			w.LockPID = pid
			w.LockLive = pt.Alive(pid) && (start == "" || procs.StartToken(pid) == start)
		}
	}
	switch {
	case w.LockLive:
		w.OwnerLive, w.LiveWhy = true, "locked by a running session (pid "+strconv.Itoa(w.LockPID)+")"
	case w.Record.Leased():
		w.OwnerLive, w.LiveWhy = true, w.Owner.Name+" has leased it to a session"
	case w.Tracked && pt.Running(w.Owner.Processes...):
		w.OwnerLive, w.LiveWhy = true, w.Owner.Name+" is running and tracks it; quit the app to act on it"
	}
	for _, p := range w.InUse {
		for _, o := range agents.Owners {
			for _, name := range o.Processes {
				if strings.EqualFold(p.Command, name) && !w.OwnerLive {
					w.OwnerLive, w.LiveWhy = true, o.Name+" is running inside it (pid "+strconv.Itoa(p.PID)+")"
				}
			}
		}
	}
}

// activity is the latest of the HEAD commit, git activity recorded in the
// worktree's admin dir (index refreshes, checkouts) and uncommitted files.
func activity(w *Worktree) {
	var best time.Time
	src := ""
	consider := func(t time.Time, s string) {
		if t.After(best) {
			best, src = t, s
		}
	}
	if w.Head != "" {
		var out []byte
		var err error
		if w.Kind == KindWorktree {
			out, err = gitx.Read(w.Path, "log", "-1", "--format=%ct", "HEAD")
		} else if w.Admin != "" {
			out, err = gitx.ReadGitDir(w.Admin, "log", "-1", "--format=%ct", "HEAD")
		}
		if err == nil {
			if sec, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64); err == nil {
				consider(time.Unix(sec, 0), "commit")
			}
		}
	}
	if w.Admin != "" {
		for _, f := range []string{"index", "HEAD", filepath.Join("logs", "HEAD")} {
			if fi, err := os.Stat(filepath.Join(w.Admin, f)); err == nil {
				consider(fi.ModTime(), "git")
			}
		}
	}
	if w.Kind != KindPrunable {
		consider(w.newestChange, "files")
	}
	if best.IsZero() {
		if fi, err := os.Stat(w.Path); err == nil {
			consider(fi.ModTime(), "files")
		}
	}
	w.LastActivity, w.ActivitySource = best, src
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}
