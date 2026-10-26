// Package scan finds git worktrees and gathers the evidence fell needs to
// judge them. It only reads: git runs with --no-optional-locks, and other
// tools' files are opened read-only.
package scan

import (
	"time"

	"github.com/Matthew-Eucaristo/fell/internal/agents"
	"github.com/Matthew-Eucaristo/fell/internal/procs"
)

// Kind is what a found item is.
type Kind string

const (
	// KindWorktree is a registered linked worktree with its checkout present.
	KindWorktree Kind = "worktree"
	// KindPrunable is a registered worktree whose checkout is gone; git
	// still keeps its HEAD, so commits only it holds are still recoverable.
	KindPrunable Kind = "prunable"
	// KindOrphan is a checkout whose .git file points at a worktree entry
	// git no longer has.
	KindOrphan Kind = "orphan"
)

// Worktree is one item with all of its evidence.
type Worktree struct {
	ID          string
	Fingerprint string
	Kind        Kind

	Path      string // real absolute checkout path
	Admin     string // <common>/worktrees/<id>; "" for orphans
	CommonDir string
	MainPath  string // main worktree; "" for bare repositories
	Repo      string // display name of the repository

	Owner   agents.Owner
	Record  *agents.Record // Claude Desktop pool record, if any
	Tracked bool           // the owning app keeps its own record of it

	Head       string
	Branch     string
	Detached   bool
	Locked     bool
	LockReason string
	LockPID    int  // from an agent lock reason, else 0
	LockLive   bool // lock owner process is running
	Prunable   string
	Keep       bool // .worktree-keep present
	Submodules bool // submodule repositories live in git's record of this worktree

	Dirty      Dirty
	StatusHash string
	HasRemotes bool
	Unpushed   int // commits on HEAD not reachable from any remote ref; -1 unknown
	OnlyHere   int // commits reachable only from this HEAD, lost if the entry is pruned; -1 unknown

	Precious     []Precious
	IgnoredOther int64 // git-ignored bytes outside artifacts (local data, generated files)
	Artifacts    []*Artifact
	Source       Size // everything outside artifacts
	Measured     bool // artifact sizes are final
	Nested       []string

	LastActivity   time.Time
	ActivitySource string

	InUse     []procs.Proc
	CwdKnown  bool
	OwnerLive bool   // the owning app or session is running for this worktree
	LiveWhy   string // why OwnerLive is set

	Safety Safety
	Errors []string

	ignored       map[string]bool // git-ignored paths, dirs as "dir/"
	walkTruncated bool
	newestChange  time.Time // newest mtime among non-ignored files
}

// Dirty counts uncommitted changes, as `git status` shows them.
type Dirty struct {
	Staged    int `json:"staged"`
	Modified  int `json:"modified"`
	Untracked int `json:"untracked"`
	Conflicts int `json:"conflicts"`
}

// Total is the number of entries with any change.
func (d Dirty) Total() int { return d.Staged + d.Modified + d.Untracked + d.Conflicts }

// Precious is a git-ignored file that is usually irreplaceable.
type Precious struct {
	Rel    string // relative to the checkout
	Bytes  int64
	Link   string // symlink target when it is a link (not at risk)
	AtRisk bool   // a real file that leaves with the worktree
}

// Artifact is a rebuildable directory inside a worktree.
type Artifact struct {
	ID      string
	Rel     string // relative to the checkout, slash-separated
	Path    string
	Rule    *ArtifactRule
	Size    Size
	Ignored bool // git ignores it and tracks nothing inside
	Repo    bool // holds a .git below (another repository or worktree); never stripped
}

// Eligible reports whether strip may move the artifact.
func (a *Artifact) Eligible() bool { return a.Ignored && !a.Repo }

// Total is the measured size of the whole worktree.
func (w *Worktree) Total() Size {
	s := w.Source
	for _, a := range w.Artifacts {
		s = s.Add(a.Size)
	}
	return s
}

// ArtifactBytes is what strip can reclaim (allocated, eligible artifacts).
func (w *Worktree) ArtifactBytes() Size {
	var s Size
	for _, a := range w.Artifacts {
		if a.Eligible() {
			s = s.Add(a.Size)
		}
	}
	return s
}

// PreciousAtRisk counts precious files that are real files.
func (w *Worktree) PreciousAtRisk() int {
	n := 0
	for _, p := range w.Precious {
		if p.AtRisk {
			n++
		}
	}
	return n
}

// Clone copies w so a background action can read it while the UI goroutine
// keeps updating the original.
func (w *Worktree) Clone() *Worktree {
	c := *w
	c.Artifacts = make([]*Artifact, len(w.Artifacts))
	for i, a := range w.Artifacts {
		ac := *a
		c.Artifacts[i] = &ac
	}
	c.Precious = append([]Precious(nil), w.Precious...)
	c.Nested = append([]string(nil), w.Nested...)
	c.InUse = append(w.InUse[:0:0], w.InUse...)
	return &c
}
