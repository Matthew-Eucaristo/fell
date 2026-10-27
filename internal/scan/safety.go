package scan

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Matthew-Eucaristo/fell/internal/format"
)

// Level summarises how safe an item is to act on.
type Level string

const (
	Safe      Level = "safe"      // retire allowed as is
	Caution   Level = "caution"   // allowed, but something is saved to the rescue folder or worth a look
	Protected Level = "protected" // retire needs an explicit override
	Live      Level = "live"      // its owning app or session is running: fell does not touch it
)

// Reason codes. They are part of the JSON contract; never rename one.
const (
	ReasonLive            = "live"
	ReasonInUse           = "in_use"
	ReasonDirty           = "dirty"
	ReasonOnlyHere        = "only_here"
	ReasonAppTracked      = "app_tracked"
	ReasonKeep            = "keep_marker"
	ReasonLocked          = "locked"
	ReasonUnknown         = "unknown_state"
	ReasonNested          = "nested_worktree"
	ReasonSubmodules      = "submodules"
	ReasonUnpushed        = "unpushed"
	ReasonNoRemote        = "no_remote"
	ReasonPrecious        = "ignored_precious"
	ReasonIgnoredData     = "ignored_data"
	ReasonLivenessUnknown = "liveness_unknown"
	ReasonRecent          = "recent"
)

// Reason is one piece of safety evidence.
type Reason struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

// Safety is the classification of a worktree.
type Safety struct {
	Level   Level    `json:"level"`
	Reasons []Reason `json:"reasons"`
}

// Has reports whether code is among the reasons.
func (s Safety) Has(code string) bool {
	for _, r := range s.Reasons {
		if r.Code == code {
			return true
		}
	}
	return false
}

// Action names, part of the contract.
const (
	ActionStrip  = "strip"
	ActionRetire = "retire"
)

// Decision says whether an action may run and what it needs.
type Decision struct {
	Action string `json:"action"`
	// Allowed: may run with no flags.
	Allowed bool `json:"allowed"`
	// Override lists reason codes that --force would override.
	Override []string `json:"needs_override,omitempty"`
	// Unlock is set when the worktree is locked and needs --unlock consent.
	Unlock bool `json:"needs_unlock,omitempty"`
	// Blocked lists reason codes that nothing overrides.
	Blocked []string `json:"blocked,omitempty"`
}

// Possible reports whether the action can run with the right flags.
func (d Decision) Possible() bool { return len(d.Blocked) == 0 }

// Permits reports whether the action may run given the user's flags.
func (d Decision) Permits(force, unlock bool) bool {
	return d.Possible() && (len(d.Override) == 0 || force) && (!d.Unlock || unlock)
}

// ignoredDataThreshold flags worktrees holding real ignored data.
const ignoredDataThreshold = 10 << 20

// RecentWindow marks a worktree changed this recently as worth a look: an
// agent session may still resume in it. Same window as the rest of the
// family (nodekill's "recent").
const RecentWindow = 7 * 24 * time.Hour

// now is the clock Classify reads; tests replace it.
var now = time.Now

// Classify derives Safety from the evidence. It is pure and can be rerun
// after sizes arrive.
func (w *Worktree) Classify() {
	rs := []Reason{}
	add := func(code, detail string) { rs = append(rs, Reason{code, detail}) }
	if w.OwnerLive {
		add(ReasonLive, w.LiveWhy)
	}
	if len(w.InUse) > 0 {
		var names []string
		for _, p := range w.InUse {
			names = append(names, fmt.Sprintf("%s (%d)", p.Command, p.PID))
		}
		add(ReasonInUse, "working directory of "+strings.Join(names, ", "))
	}
	if len(w.Nested) > 0 {
		add(ReasonNested, "contains other worktrees: "+strings.Join(w.Nested, ", "))
	}
	if w.Kind == KindOrphan {
		add(ReasonUnknown, "git no longer knows this worktree; its state cannot be checked")
	}
	if w.Kind == KindWorktree && w.Head == "" {
		add(ReasonUnknown, "git could not read this worktree")
	}
	if w.Kind != KindOrphan && w.Head != "" && w.OnlyHere < 0 {
		add(ReasonUnknown, "could not tell which commits only this worktree holds")
	}
	if w.HasRemotes && w.Head != "" && w.Unpushed < 0 {
		add(ReasonUnknown, "could not count unpushed commits")
	}
	if w.walkTruncated {
		add(ReasonUnknown, "too many files to check for secrets and nested repositories")
	}
	if w.Submodules {
		add(ReasonSubmodules, "has submodules whose repositories live in git's record of this worktree; remove it with git after checking them")
	}
	if n := w.Dirty.Total(); n > 0 {
		add(ReasonDirty, fmt.Sprintf("%d uncommitted change(s)", n))
	}
	if w.OnlyHere > 0 {
		add(ReasonOnlyHere, fmt.Sprintf("%d commit(s) exist only in this worktree", w.OnlyHere))
	}
	if w.Tracked {
		add(ReasonAppTracked, w.Owner.Name+" still tracks it. "+w.Owner.Removal)
	}
	if w.Keep {
		add(ReasonKeep, ".worktree-keep asks to keep it")
	}
	if w.Locked {
		d := "locked"
		if w.LockReason != "" {
			d += ": " + w.LockReason
		}
		add(ReasonLocked, d)
	}
	if w.Unpushed > 0 {
		add(ReasonUnpushed, fmt.Sprintf("%d commit(s) not on any remote", w.Unpushed))
	}
	if !w.HasRemotes && w.Kind != KindOrphan {
		add(ReasonNoRemote, "the repository has no remote")
	}
	if n := w.PreciousAtRisk(); n > 0 {
		add(ReasonPrecious, fmt.Sprintf("%d ignored secret/data file(s), copied to the rescue folder on retire", n))
	}
	if t := w.LastActivity; !t.IsZero() && now().Sub(t) < RecentWindow {
		add(ReasonRecent, "changed in the last 7 days ("+activityText[w.ActivitySource]+" "+format.Ago(t, now())+"); an agent session may still resume in it")
	}
	if w.IgnoredOther >= ignoredDataThreshold {
		add(ReasonIgnoredData, format.Bytes(w.IgnoredOther)+" of git-ignored files that are not rebuildable artifacts")
	}
	if !w.CwdKnown {
		add(ReasonLivenessUnknown, "could not list processes' working directories (lsof missing?)")
	}

	level := Safe
	for _, r := range rs {
		switch r.Code {
		case ReasonLive:
			level = Live
		case ReasonInUse, ReasonNested, ReasonSubmodules, ReasonUnknown, ReasonDirty, ReasonOnlyHere, ReasonAppTracked, ReasonKeep, ReasonLocked, ReasonLivenessUnknown:
			if level != Live {
				level = Protected
			}
		default:
			if level == Safe {
				level = Caution
			}
		}
	}
	w.Safety = Safety{Level: level, Reasons: rs}
}

var activityText = map[string]string{"commit": "last commit", "git": "last git activity", "files": "last file change", "": "last activity"}

// Decide applies the rules for one action.
func (w *Worktree) Decide(action string) Decision {
	d := Decision{Action: action}
	s := w.Safety
	if s.Has(ReasonLive) {
		d.Blocked = append(d.Blocked, ReasonLive)
	}
	switch action {
	case ActionStrip:
		if w.Kind != KindWorktree {
			d.Blocked = append(d.Blocked, ReasonUnknown)
		}
		if s.Has(ReasonInUse) {
			d.Override = append(d.Override, ReasonInUse)
		}
		if s.Has(ReasonLivenessUnknown) {
			d.Override = append(d.Override, ReasonLivenessUnknown)
		}
	case ActionRetire:
		// An app that still tracks the worktree must remove it itself, the
		// official way; a .worktree-keep marker is the user's own "no".
		for _, code := range []string{ReasonNested, ReasonSubmodules, ReasonAppTracked, ReasonKeep} {
			if s.Has(code) {
				d.Blocked = append(d.Blocked, code)
			}
		}
		for _, code := range []string{ReasonInUse, ReasonDirty, ReasonOnlyHere, ReasonUnknown, ReasonLivenessUnknown} {
			if s.Has(code) {
				d.Override = append(d.Override, code)
			}
		}
		d.Unlock = w.Locked
	default:
		d.Blocked = append(d.Blocked, "unknown_action")
	}
	d.Allowed = d.Possible() && len(d.Override) == 0 && !d.Unlock
	return d
}

// computeIDs sets ID and Fingerprint. The fingerprint covers what the
// safety decision rests on (HEAD, branch, changes, locks, tracking, the
// directory's identity) but not sizes or activity, so a plan stays valid
// while a build writes into node_modules and goes stale when work changes.
func (w *Worktree) computeIDs() {
	w.ID = "wt-" + shortHash(w.Path)
	h := sha256.New()
	field := func(s string) { h.Write([]byte(s)); h.Write([]byte{0}) }
	field(string(w.Kind))
	field(w.Path)
	field(w.Admin)
	field(w.Head)
	field(w.Branch)
	field(fmt.Sprint(w.Detached, w.Locked, w.Keep, w.Tracked, w.Record.Leased(), w.Submodules, w.OnlyHere))
	field(w.LockReason)
	field(w.Prunable)
	field(w.StatusHash)
	for _, p := range w.Precious {
		field(p.Rel)
	}
	field(strings.Join(w.Nested, "\n"))
	if fi, err := os.Lstat(w.Path); err == nil {
		_, id, _ := diskUsage(fi)
		field(fmt.Sprint(id.dev, id.ino))
	}
	w.Fingerprint = "fp-" + hex.EncodeToString(h.Sum(nil))[:16]
	for _, a := range w.Artifacts {
		a.ID = "art-" + shortHash(a.Path)
	}
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:12]
}
