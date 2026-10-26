// Package report prints scans and action outcomes as plain text.
package report

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Matthew-Eucaristo/fell/internal/clean"
	"github.com/Matthew-Eucaristo/fell/internal/format"
	"github.com/Matthew-Eucaristo/fell/internal/scan"
)

// Text writes worktrees grouped by the tool that created them.
func Text(w io.Writer, res *scan.Result, roots []string) {
	home, _ := os.UserHomeDir()
	now := time.Now()
	var total, strip scan.Size
	for _, wt := range res.Worktrees {
		total = total.Add(wt.Total())
		strip = strip.Add(wt.ArtifactBytes())
	}
	var rs []string
	for _, r := range roots {
		rs = append(rs, format.Tilde(r, home))
	}
	fmt.Fprintf(w, "fell: %d worktree(s), %s on disk (%s freeable), %s rebuildable (%s freeable)  (roots: %s + agent locations)\n",
		len(res.Worktrees), format.Bytes(total.Allocated), format.Bytes(total.Exclusive()), format.Bytes(strip.Allocated), format.Bytes(strip.Exclusive()), strings.Join(rs, ", "))
	for _, warn := range res.Warnings {
		fmt.Fprintln(w, "warning:", warn)
	}
	if len(res.Worktrees) == 0 {
		fmt.Fprintln(w, "\nNo linked worktrees found.")
		return
	}
	for _, owner := range res.Owners() {
		var group []*scan.Worktree
		var size int64
		for _, wt := range res.Worktrees {
			if wt.Owner.ID == owner {
				group = append(group, wt)
				size += wt.Total().Allocated
			}
		}
		fmt.Fprintf(w, "\n%s  %d worktree(s)  %s\n", strings.ToUpper(group[0].Owner.Name), len(group), format.Bytes(size))
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		for _, wt := range group {
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\t%s\t%s\n", format.Bytes(wt.Total().Allocated), Share(wt), format.Ago(wt.LastActivity, now),
				wt.Safety.Level, Name(wt), strings.Join(Flags(wt), " "), wt.ID)
		}
		tw.Flush()
		for _, wt := range group {
			if wt.Safety.Level == scan.Safe {
				continue
			}
			fmt.Fprintf(w, "    %s  %s\n", Name(wt), format.Tilde(wt.Path, home))
			for _, r := range wt.Safety.Reasons {
				fmt.Fprintf(w, "      - %s: %s\n", r.Code, r.Detail)
			}
		}
	}
	fmt.Fprintln(w, "\nSizes are allocated blocks with hard links counted once; \"freeable\" excludes blocks shared with hard links, APFS clones or snapshots. Freed space is measured after acting.")
	fmt.Fprintln(w, "Act with: fell strip <id>   fell retire <id>   (add --dry-run to preview)")
}

// Name is "repo/worktree". Tools that name the checkout after the repo
// (Codex: <root>/<id>/<repo>) are shown by their parent folder instead.
func Name(w *scan.Worktree) string {
	base := filepath.Base(w.Path)
	if base == w.Repo {
		base = filepath.Base(filepath.Dir(w.Path))
	}
	return w.Repo + "/" + base
}

// Share is the rebuildable percentage of a worktree.
func Share(w *scan.Worktree) string {
	t := w.Total().Allocated
	if !w.Measured || t <= 0 || w.Kind != scan.KindWorktree {
		return "-"
	}
	return fmt.Sprintf("%d%% rebuildable", w.ArtifactBytes().Allocated*100/t)
}

// Flags are short state markers.
func Flags(w *scan.Worktree) []string {
	var f []string
	switch {
	case w.Kind == scan.KindPrunable:
		f = append(f, "checkout missing")
	case w.Kind == scan.KindOrphan:
		f = append(f, "orphaned")
	case w.Detached:
		f = append(f, "detached")
	default:
		f = append(f, w.Branch)
	}
	if w.Unpushed > 0 {
		f = append(f, fmt.Sprintf("↑%d", w.Unpushed))
	}
	if w.OnlyHere > 0 {
		f = append(f, fmt.Sprintf("only-here:%d", w.OnlyHere))
	}
	if n := w.Dirty.Total(); n > 0 {
		f = append(f, fmt.Sprintf("✎%d", n))
	}
	if n := w.PreciousAtRisk(); n > 0 {
		f = append(f, fmt.Sprintf("secrets:%d", n))
	}
	if w.Locked {
		f = append(f, "locked")
	}
	if len(w.InUse) > 0 {
		f = append(f, "in-use")
	}
	if w.Tracked {
		f = append(f, "app-tracked")
	}
	return f
}

// Outcomes prints what actions did.
func Outcomes(w io.Writer, outs []*clean.Outcome, mode clean.Mode) {
	if len(outs) == 0 {
		return
	}
	home, _ := os.UserHomeDir()
	var est, freed int64
	done := 0
	for _, o := range outs {
		p := format.Tilde(o.Path, home)
		switch o.Status {
		case clean.StatusDone, clean.StatusDryRun:
			done++
			est += o.Estimate
			if o.Freed > 0 {
				freed += o.Freed
			}
			verb := map[string]string{scan.ActionStrip: "stripped", scan.ActionRetire: "retired"}[o.Action]
			if o.Status == clean.StatusDryRun {
				verb = "would " + o.Action
			}
			fmt.Fprintf(w, "%-14s %9s  %s\n", verb, format.Bytes(o.Estimate), p)
			if r := o.Rescue; r != nil {
				fmt.Fprintf(w, "               rescue: %d commit(s) bundled, patch=%v, %d secret file(s) copied → %s\n", r.BundleCommits, r.Patch, len(r.Precious), r.Folder)
			}
			for _, s := range o.Skipped {
				fmt.Fprintf(w, "               skipped: %s\n", s)
			}
			if o.PruneNote != "" {
				fmt.Fprintf(w, "               %s\n", o.PruneNote)
			}
		default:
			fmt.Fprintf(w, "%-14s %9s  %s\n               %s\n", o.Status, "", p, o.Error)
		}
	}
	switch mode {
	case clean.DryRun:
		fmt.Fprintf(w, "Dry run: %d action(s) would move about %s. Nothing was changed.\n", done, format.Bytes(est))
	case clean.Permanent:
		fmt.Fprintf(w, "Deleted about %s; free space grew by %s (measured).\n", format.Bytes(est), format.Bytes(freed))
	default:
		if done > 0 {
			fmt.Fprintf(w, "Moved about %s to the %s (free space changed by %s, measured). Empty the %s to reclaim it; restore with Put Back.\n",
				format.Bytes(est), clean.TrashName, format.Bytes(freed), clean.TrashName)
		}
	}
}
