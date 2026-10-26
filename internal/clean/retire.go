package clean

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Matthew-Eucaristo/fell/internal/gitx"
	"github.com/Matthew-Eucaristo/fell/internal/scan"
)

// Retire removes a worktree reversibly:
//  1. re-read it and refuse if it changed or the rules say no,
//  2. write a rescue folder (bundle of commits only this worktree holds,
//     a patch of uncommitted changes, copies of ignored secrets/data, a
//     manifest and RESTORE.md) and move it to the trash,
//  3. unlock it if the user consented,
//  4. move the checkout to the trash,
//  5. let `git worktree prune` drop git's record, only when nothing else in
//     that repository would be pruned with it.
//
// It never runs `git worktree remove`, which deletes ignored files
// permanently, and never deletes a branch.
func Retire(w *scan.Worktree, fingerprint string, opts Options) *Outcome {
	o := &Outcome{ID: w.ID, Action: scan.ActionRetire, Path: w.Path, Freed: -1}
	nw, err := fresh(w, fingerprint, opts.Env)
	if err != nil {
		o.Status, o.Error = StatusRefused, err.Error()
		return o
	}
	d := nw.Decide(scan.ActionRetire)
	if !d.Permits(opts.Force, opts.Unlock) {
		return refused(o, d, opts)
	}
	if err := CheckRetire(nw); err != nil {
		o.Status, o.Error = StatusRefused, err.Error()
		return o
	}
	o.Estimate = w.Total().Allocated
	if o.Estimate < 0 || !w.Measured {
		o.Estimate = 0
		if nw.Kind != scan.KindPrunable {
			o.Estimate = scan.DirSize(nw.Path).Allocated
		}
	}

	plan, err := planRescue(nw)
	if err != nil {
		o.Status, o.Error = StatusRefused, err.Error()
		return o
	}
	if opts.Mode == DryRun {
		o.Status, o.Rescue = StatusDryRun, plan
		return o
	}

	volume := nw.Path
	if nw.Kind == scan.KindPrunable {
		volume = nw.CommonDir
	}
	before := freeBytes(volume)

	staged, rescue, err := writeRescue(nw, plan)
	if err != nil {
		o.Status, o.Error = StatusFailed, "rescue folder: "+err.Error()+"; nothing was removed"
		return o
	}
	if err := trash(staged); err != nil {
		o.Status = StatusFailed
		o.Error = fmt.Sprintf("could not move the rescue folder to the %s (%v); it is kept at %s and nothing was removed", TrashName, err, staged)
		return o
	}
	cleanupStaging(staged)
	o.Rescue = rescue

	// Writing the rescue folder takes a moment; re-check that nothing
	// started using or changed the worktree before it moves.
	again, err := fresh(nw, nw.Fingerprint, opts.Env)
	if err == nil && !again.Decide(scan.ActionRetire).Permits(opts.Force, opts.Unlock) {
		err = fmt.Errorf("it became %s", again.Safety.Level)
	}
	if err != nil {
		o.Status = StatusRefused
		o.Error = "changed while the rescue folder was written (" + err.Error() + "); the worktree was left as is, the rescue folder is in the " + TrashName
		return o
	}

	// Checkout first: if it cannot move, the lock and git's record stay.
	if nw.Kind != scan.KindPrunable {
		if err := trash(nw.Path); err != nil {
			o.Status, o.Error = StatusFailed, "move to "+TrashName+": "+err.Error()+"; the worktree is unchanged (the rescue folder is in the "+TrashName+")"
			return o
		}
		o.Trashed = append(o.Trashed, nw.Path)
	}
	if nw.Kind != scan.KindOrphan {
		if nw.Locked && opts.Unlock {
			if _, err := gitx.WriteGitDir(nw.CommonDir, "worktree", "unlock", nw.Path); err != nil {
				o.Status = StatusDone
				o.PruneNote = "not pruned: unlock failed (" + err.Error() + ")"
				o.Restore = restoreHint(nw, rescue, false)
				return o
			}
		}
		if head, err := gitx.ReadGitDir(nw.Admin, "rev-parse", "--verify", "-q", "HEAD"); err != nil || strings.TrimSpace(string(head)) != nw.Head {
			o.PruneNote = "not pruned: the worktree's HEAD changed while retiring; its record keeps the new commit"
		} else {
			o.Pruned, o.PruneNote = prune(nw)
		}
	}
	o.Status = StatusDone
	o.Freed = delta(before, freeBytes(volume))
	o.Restore = restoreHint(nw, rescue, o.Pruned)
	return o
}

// prune runs `git worktree prune` for the repository only if the entries it
// would remove are exactly this worktree's, so fell never drops another
// worktree's record the user did not choose.
func prune(w *scan.Worktree) (bool, string) {
	out, err := gitx.ReadGitDirCombined(w.CommonDir, "worktree", "prune", "--dry-run", "-v")
	if err != nil {
		return false, "git worktree prune --dry-run failed: " + strings.TrimSpace(string(out))
	}
	id := filepath.Base(w.Admin)
	var others []string
	mine := false
	for _, line := range strings.Split(string(out), "\n") {
		rest, ok := strings.CutPrefix(line, "Removing worktrees/")
		if !ok {
			continue
		}
		name, _, _ := strings.Cut(rest, ":")
		if name == id {
			mine = true
		} else {
			others = append(others, name)
		}
	}
	switch {
	case len(others) > 0:
		return false, "not pruned: `git worktree prune` would also drop other stale entries (" + strings.Join(others, ", ") + "); review them, then run it yourself"
	case !mine:
		return false, "git does not consider the entry stale yet; `git worktree prune` will drop it later"
	}
	if out, err := gitx.WriteGitDir(w.CommonDir, "worktree", "prune", "-v"); err != nil {
		return false, "git worktree prune failed: " + strings.TrimSpace(string(out)) + " " + err.Error()
	}
	return true, ""
}

func restoreHint(w *scan.Worktree, r *Rescue, pruned bool) string {
	var b strings.Builder
	if w.Kind != scan.KindPrunable {
		fmt.Fprintf(&b, "The checkout is in the %s as %q. ", TrashName, filepath.Base(w.Path))
		if !pruned {
			b.WriteString("Put it back and it works as a worktree again. ")
		}
	}
	if r != nil {
		fmt.Fprintf(&b, "%q in the %s holds RESTORE.md", r.Folder, TrashName)
		if r.BundleCommits > 0 {
			fmt.Fprintf(&b, " and a bundle of %d commit(s)", r.BundleCommits)
		}
		b.WriteString(".")
	}
	return b.String()
}
