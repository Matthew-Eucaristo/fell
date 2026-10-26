// Package clean is the only code in fell that removes or changes anything.
// Every action re-reads the worktree, compares it with what the user saw,
// and passes the guards in guard.go first.
package clean

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Matthew-Eucaristo/fell/internal/agents"
	"github.com/Matthew-Eucaristo/fell/internal/scan"
)

// TrashName is what the OS calls its trash, for messages.
var TrashName = "Trash"

// Mode selects what "remove" means.
type Mode int

const (
	Trash     Mode = iota // move to the OS trash (default, recoverable)
	Permanent             // strip only: delete artifacts outright
	DryRun                // check everything, change nothing
)

func (m Mode) String() string { return [...]string{"trash", "permanent", "dry-run"}[m] }

// Options are the user's explicit consents.
type Options struct {
	Mode   Mode
	Force  bool // override refusals that can be overridden (dirty, in use, app-tracked, …)
	Unlock bool // allow `git worktree unlock` on a locked worktree
	Env    agents.Env
}

// Status of one action.
const (
	StatusDone    = "done"
	StatusDryRun  = "dry_run"
	StatusRefused = "refused"
	StatusFailed  = "failed"
)

// Outcome reports one action on one worktree.
type Outcome struct {
	ID        string   `json:"id"`
	Action    string   `json:"action"`
	Path      string   `json:"path"`
	Status    string   `json:"status"`
	Reasons   []string `json:"reasons,omitempty"` // refusal codes
	Error     string   `json:"error,omitempty"`
	Trashed   []string `json:"trashed,omitempty"`      // paths moved to the trash
	Deleted   []string `json:"deleted,omitempty"`      // paths deleted (strip --permanent)
	Skipped   []string `json:"skipped,omitempty"`      // artifacts left alone, with why
	Rescue    *Rescue  `json:"rescue,omitempty"`       // what retire saved
	Estimate  int64    `json:"estimated_bytes"`        // allocated size of what was removed
	Freed     int64    `json:"freed_bytes_measured"`   // change in free space, -1 unknown
	Pruned    bool     `json:"pruned,omitempty"`       // git's record removed
	PruneNote string   `json:"prune_note,omitempty"`   // why it was not
	Restore   string   `json:"restore_hint,omitempty"` // how to undo
}

// ErrStale means the worktree changed since it was scanned.
var ErrStale = errors.New("changed since it was scanned (fingerprint differs); rescan and review it again")

// fresh re-reads w and checks the fingerprint the caller saw, if any.
func fresh(w *scan.Worktree, fingerprint string, env agents.Env) (*scan.Worktree, error) {
	nw, err := scan.Refresh(w, env)
	if err != nil {
		return nil, err
	}
	if fingerprint != "" && nw.Fingerprint != fingerprint {
		return nil, ErrStale
	}
	return nw, nil
}

func refused(o *Outcome, d scan.Decision, opts Options) *Outcome {
	o.Status = StatusRefused
	o.Reasons = append(o.Reasons, d.Blocked...)
	if !opts.Force {
		o.Reasons = append(o.Reasons, d.Override...)
	}
	if d.Unlock && !opts.Unlock {
		o.Reasons = append(o.Reasons, scan.ReasonLocked)
	}
	var hint []string
	if len(d.Blocked) > 0 {
		hint = append(hint, "blocked: "+strings.Join(d.Blocked, ", "))
	}
	if len(d.Override) > 0 && !opts.Force {
		hint = append(hint, "needs --force for: "+strings.Join(d.Override, ", "))
	}
	if d.Unlock && !opts.Unlock {
		hint = append(hint, "locked: needs --unlock")
	}
	o.Error = strings.Join(hint, "; ")
	return o
}

// Strip moves a worktree's rebuildable artifacts to the trash and keeps the
// worktree. only limits it to these artifact paths (nil = all eligible).
func Strip(w *scan.Worktree, fingerprint string, only []string, opts Options) *Outcome {
	o := &Outcome{ID: w.ID, Action: scan.ActionStrip, Path: w.Path, Freed: -1}
	nw, err := fresh(w, fingerprint, opts.Env)
	if err != nil {
		o.Status, o.Error = StatusRefused, err.Error()
		return o
	}
	d := nw.Decide(scan.ActionStrip)
	if !d.Permits(opts.Force, false) {
		return refused(o, d, opts)
	}
	sizes := map[string]int64{}
	for _, a := range w.Artifacts {
		sizes[a.Path] = a.Size.Allocated
	}
	before := freeBytes(nw.Path)
	for _, a := range nw.Artifacts {
		if only != nil && !contains(only, a.Path) {
			continue
		}
		if err := CheckArtifact(nw, a.Path); err != nil {
			o.Skipped = append(o.Skipped, err.Error())
			continue
		}
		n := sizes[a.Path]
		if n <= 0 {
			n = scan.DirSize(a.Path).Allocated
		}
		switch opts.Mode {
		case DryRun:
		case Permanent:
			if err := os.RemoveAll(a.Path); err != nil {
				o.Skipped = append(o.Skipped, err.Error())
				continue
			}
			o.Deleted = append(o.Deleted, a.Path)
		default:
			if err := trash(a.Path); err != nil {
				o.Skipped = append(o.Skipped, err.Error())
				continue
			}
			o.Trashed = append(o.Trashed, a.Path)
		}
		o.Estimate += n
	}
	o.Status = StatusDone
	if opts.Mode == DryRun {
		o.Status = StatusDryRun
		return o
	}
	o.Freed = delta(before, freeBytes(nw.Path))
	if len(o.Trashed) > 0 {
		o.Restore = "Put the folders back from the " + TrashName + ", or reinstall: the artifacts are rebuilt by their package manager or build."
	}
	if len(o.Trashed)+len(o.Deleted) == 0 && len(o.Skipped) > 0 {
		o.Status = StatusFailed
		o.Error = "nothing could be stripped"
	}
	return o
}

// moveFunc is the trash mover; tests swap it for a temp directory so they
// never fill the real trash.
var moveFunc = moveToTrash

// trash moves path to the trash and confirms it is gone.
func trash(path string) error {
	if err := moveFunc(path); err != nil {
		return err
	}
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("%s still exists after moving to the trash", path)
	}
	return nil
}

func delta(before, after int64) int64 {
	if before < 0 || after < 0 {
		return -1
	}
	return after - before
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s || filepath.Clean(x) == s {
			return true
		}
	}
	return false
}

// FreeBytes is the space available on path's filesystem, -1 if unknown.
func FreeBytes(path string) int64 { return freeBytes(path) }
