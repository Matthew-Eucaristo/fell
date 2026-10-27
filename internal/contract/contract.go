// Package contract is fell's stable machine interface (schema_version 1):
// the scan document, capabilities, plans and apply results. docs/contract.md
// describes it for other tools. Fields may be added; existing fields and
// codes are never renamed or repurposed within a schema version.
package contract

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Matthew-Eucaristo/fell/internal/agents"
	"github.com/Matthew-Eucaristo/fell/internal/clean"
	"github.com/Matthew-Eucaristo/fell/internal/scan"
)

// SchemaVersion of every document fell emits or accepts.
const SchemaVersion = 1

// Tool identifies the emitter.
type Tool struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Ecosystem string `json:"ecosystem"`
}

// NewTool describes fell at version v.
func NewTool(v string) Tool { return Tool{Name: "fell", Version: v, Ecosystem: "git-worktrees"} }

// Bytes is a clone- and hard-link-aware size. Exclusive is what removal can
// free; Shared is held by hard links outside the item.
type Bytes struct {
	Allocated int64 `json:"allocated"`
	Shared    int64 `json:"shared"`
	Exclusive int64 `json:"exclusive"`
	Files     int64 `json:"file_count"`
}

func bytesOf(s scan.Size) Bytes {
	return Bytes{Allocated: s.Allocated, Shared: s.Shared, Exclusive: s.Exclusive(), Files: s.Files}
}

// Item is one worktree.
type Item struct {
	ID          string          `json:"id"`
	Fingerprint string          `json:"fingerprint"`
	Kind        scan.Kind       `json:"kind"`
	Path        string          `json:"path"`
	Name        string          `json:"name"`
	Project     Project         `json:"project"`
	Owner       Owner           `json:"owner"`
	Measured    bool            `json:"measured"`
	Bytes       Bytes           `json:"bytes"`
	Strippable  Bytes           `json:"strippable_bytes"`
	IgnoredData int64           `json:"ignored_other_bytes"`
	SizeNote    string          `json:"size_note"`
	Artifacts   []Artifact      `json:"artifacts"`
	Git         Git             `json:"git"`
	Precious    []Precious      `json:"precious"`
	Activity    Activity        `json:"activity"`
	Liveness    Liveness        `json:"liveness"`
	Safety      scan.Safety     `json:"safety"`
	Actions     []scan.Decision `json:"actions"`
	Errors      []string        `json:"errors,omitempty"`
}

type Project struct {
	Root string `json:"root"` // main checkout ("" for bare repositories)
	Name string `json:"name"`
}

type Owner struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Tracked     bool   `json:"tracked"`
	Live        bool   `json:"live"`
	HowToRemove string `json:"how_to_remove"`
}

type Artifact struct {
	ID         string `json:"id"`
	Path       string `json:"path"`
	Kind       string `json:"kind"`
	Regenerate string `json:"regenerate"`
	Eligible   bool   `json:"eligible"` // git-ignored with nothing tracked inside
	Bytes      Bytes  `json:"bytes"`
}

type Git struct {
	Head       string     `json:"head"`
	Branch     string     `json:"branch"`
	Detached   bool       `json:"detached"`
	Dirty      scan.Dirty `json:"dirty"`
	HasRemote  bool       `json:"has_remote"`
	Unpushed   int        `json:"unpushed"`  // -1 unknown or no remote
	OnlyHere   int        `json:"only_here"` // -1 unknown
	Locked     bool       `json:"locked"`
	LockReason string     `json:"lock_reason"`
	Prunable   string     `json:"prunable"`
	KeepMarker bool       `json:"keep_marker"`
}

type Precious struct {
	Path    string `json:"path"` // relative to the worktree
	Bytes   int64  `json:"bytes"`
	Symlink bool   `json:"symlink"`
	AtRisk  bool   `json:"at_risk"`
}

type Activity struct {
	Last    *time.Time `json:"last"`
	Source  string     `json:"source"` // commit, git, files
	AgeDays int        `json:"age_days"`
}

type Liveness struct {
	CwdKnown  bool      `json:"cwd_known"`
	InUse     bool      `json:"in_use"`
	Processes []Process `json:"processes"`
	OwnerLive bool      `json:"owner_live"`
	Why       string    `json:"why"`
}

type Process struct {
	PID     int    `json:"pid"`
	Command string `json:"command"`
}

// Tilde shows home as "~" so documents do not carry the username.
func Tilde(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home || strings.HasPrefix(p, home+string(filepath.Separator)) {
		return "~" + p[len(home):]
	}
	return p
}

// Expand reverses Tilde.
func Expand(p string) string {
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, rest)
		}
	}
	return p
}

// NewItem converts a scanned worktree.
func NewItem(w *scan.Worktree, now time.Time) Item {
	it := Item{
		ID: w.ID, Fingerprint: w.Fingerprint, Kind: w.Kind,
		Path: Tilde(w.Path), Name: filepath.Base(w.Path),
		Project:  Project{Root: Tilde(w.MainPath), Name: w.Repo},
		Owner:    Owner{ID: w.Owner.ID, Name: w.Owner.Name, Tracked: w.Tracked, Live: w.OwnerLive, HowToRemove: w.Owner.Removal},
		Measured: w.Measured, IgnoredData: w.IgnoredOther,
		SizeNote: "allocated blocks; hard links counted once; shared = not freed by removal (outside hard links, and on macOS APFS clone/snapshot blocks); Btrfs/XFS reflinks count in full; freed space is measured after each action",
		Git: Git{Head: w.Head, Branch: w.Branch, Detached: w.Detached, Dirty: w.Dirty, HasRemote: w.HasRemotes,
			Unpushed: w.Unpushed, OnlyHere: w.OnlyHere, Locked: w.Locked, LockReason: w.LockReason, Prunable: w.Prunable, KeepMarker: w.Keep},
		Liveness: Liveness{CwdKnown: w.CwdKnown, InUse: len(w.InUse) > 0, OwnerLive: w.OwnerLive, Why: w.LiveWhy, Processes: []Process{}},
		Safety:   w.Safety,
		Actions:  []scan.Decision{w.Decide(scan.ActionStrip), w.Decide(scan.ActionRetire)},
		Errors:   w.Errors, Artifacts: []Artifact{}, Precious: []Precious{},
	}
	if w.Measured {
		it.Bytes, it.Strippable = bytesOf(w.Total()), bytesOf(w.ArtifactBytes())
	}
	for _, a := range w.Artifacts {
		ab := Bytes{}
		if w.Measured {
			ab = bytesOf(a.Size)
		}
		it.Artifacts = append(it.Artifacts, Artifact{ID: a.ID, Path: a.Rel, Kind: a.Rule.Name, Regenerate: a.Rule.Regenerate, Eligible: a.Eligible(), Bytes: ab})
	}
	for _, p := range w.Precious {
		it.Precious = append(it.Precious, Precious{Path: p.Rel, Bytes: p.Bytes, Symlink: p.Link != "", AtRisk: p.AtRisk})
	}
	for _, p := range w.InUse {
		it.Liveness.Processes = append(it.Liveness.Processes, Process{PID: p.PID, Command: p.Command})
	}
	if !w.LastActivity.IsZero() {
		t := w.LastActivity.UTC()
		it.Activity = Activity{Last: &t, Source: w.ActivitySource, AgeDays: int(now.Sub(t).Hours() / 24)}
	}
	return it
}

// Summary totals a scan.
type Summary struct {
	Items      int            `json:"items"`
	Bytes      Bytes          `json:"bytes"`
	Strippable Bytes          `json:"strippable_bytes"`
	ByLevel    map[string]int `json:"by_safety_level"`
	ByOwner    map[string]int `json:"by_owner"`
}

// ScanDoc is `fell scan --json`.
type ScanDoc struct {
	SchemaVersion int       `json:"schema_version"`
	Tool          Tool      `json:"tool"`
	GeneratedAt   time.Time `json:"generated_at"`
	Roots         []string  `json:"roots"`
	Warnings      []string  `json:"warnings"`
	Summary       Summary   `json:"summary"`
	Items         []Item    `json:"items"`
}

// NewScanDoc converts a measured scan.
func NewScanDoc(r *scan.Result, roots []string, tool Tool) ScanDoc {
	now := time.Now()
	doc := ScanDoc{SchemaVersion: SchemaVersion, Tool: tool, GeneratedAt: now.UTC(), Warnings: r.Warnings, Items: []Item{}}
	if doc.Warnings == nil {
		doc.Warnings = []string{}
	}
	for _, root := range roots {
		doc.Roots = append(doc.Roots, Tilde(root))
	}
	for _, w := range r.Worktrees {
		doc.Items = append(doc.Items, NewItem(w, now))
	}
	doc.Summary = Summarize(doc.Items)
	return doc
}

// Summarize totals items.
func Summarize(items []Item) Summary {
	s := Summary{Items: len(items), ByLevel: map[string]int{}, ByOwner: map[string]int{}}
	add := func(a *Bytes, b Bytes) {
		a.Allocated += b.Allocated
		a.Shared += b.Shared
		a.Exclusive += b.Exclusive
		a.Files += b.Files
	}
	for _, it := range items {
		add(&s.Bytes, it.Bytes)
		add(&s.Strippable, it.Strippable)
		s.ByLevel[string(it.Safety.Level)]++
		s.ByOwner[it.Owner.ID]++
	}
	return s
}

// Encode writes v as indented JSON.
func Encode(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// Line writes one NDJSON record of the given type.
func Line(w io.Writer, typ string, v any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return err
	}
	b := bytes.TrimRight(buf.Bytes(), "\n")
	// Splice "type" in front so consumers can switch on it.
	if len(b) > 1 && b[0] == '{' {
		sep := ","
		if b[1] == '}' {
			sep = ""
		}
		b = append([]byte(fmt.Sprintf(`{"type":%q%s`, typ, sep)), b[1:]...)
	}
	_, err := w.Write(append(b, '\n'))
	return err
}

// Plan is what a caller asks `fell apply` to do.
type Plan struct {
	SchemaVersion int        `json:"schema_version"`
	Items         []PlanItem `json:"items"`
}

// PlanItem selects one action on one item, pinned to its scan fingerprint.
type PlanItem struct {
	ID          string   `json:"id"`
	Fingerprint string   `json:"fingerprint"`
	Action      string   `json:"action"`
	Artifacts   []string `json:"artifacts,omitempty"` // strip only: artifact IDs; empty = all eligible
}

// ReadPlan parses and validates a plan.
func ReadPlan(r io.Reader) (*Plan, error) {
	var p Plan
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("plan: %w", err)
	}
	if p.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("plan: schema_version %d is not supported (want %d)", p.SchemaVersion, SchemaVersion)
	}
	if len(p.Items) == 0 {
		return nil, errors.New("plan: no items")
	}
	for i, it := range p.Items {
		if it.ID == "" || it.Fingerprint == "" {
			return nil, fmt.Errorf("plan: item %d needs id and fingerprint", i)
		}
		if it.Action != scan.ActionStrip && it.Action != scan.ActionRetire {
			return nil, fmt.Errorf("plan: item %d: unknown action %q", i, it.Action)
		}
		if len(it.Artifacts) > 0 && it.Action != scan.ActionStrip {
			return nil, fmt.Errorf("plan: item %d: artifacts only apply to strip", i)
		}
	}
	return &p, nil
}

// Result is one apply outcome with paths shown relative to home.
type Result struct {
	clean.Outcome
}

// MarshalJSON tilde-shortens paths.
func (r Result) MarshalJSON() ([]byte, error) {
	o := r.Outcome
	o.Path = Tilde(o.Path)
	o.Trashed = tildeAll(o.Trashed)
	o.Deleted = tildeAll(o.Deleted)
	return json.Marshal(o)
}

func tildeAll(ps []string) []string {
	var out []string
	for _, p := range ps {
		out = append(out, Tilde(p))
	}
	return out
}

// ApplyDoc is `fell apply --json` (and strip/retire --json).
type ApplyDoc struct {
	SchemaVersion int          `json:"schema_version"`
	Tool          Tool         `json:"tool"`
	Mode          string       `json:"mode"`
	Results       []Result     `json:"results"`
	Summary       ApplySummary `json:"summary"`
}

type ApplySummary struct {
	Done     int   `json:"done"`
	DryRun   int   `json:"dry_run"`
	Refused  int   `json:"refused"`
	Failed   int   `json:"failed"`
	Estimate int64 `json:"estimated_bytes"`
	// Freed is the sum of measured free-space changes; with the default
	// trash mode it stays near zero until the trash is emptied.
	Freed int64 `json:"freed_bytes_measured"`
}

// NewApplyDoc totals outcomes.
func NewApplyDoc(tool Tool, mode clean.Mode, outs []*clean.Outcome) ApplyDoc {
	d := ApplyDoc{SchemaVersion: SchemaVersion, Tool: tool, Mode: mode.String(), Results: []Result{}}
	for _, o := range outs {
		d.Results = append(d.Results, Result{*o})
		switch o.Status {
		case clean.StatusDone:
			d.Summary.Done++
		case clean.StatusDryRun:
			d.Summary.DryRun++
		case clean.StatusRefused:
			d.Summary.Refused++
		case clean.StatusFailed:
			d.Summary.Failed++
		}
		if o.Status == clean.StatusDone || o.Status == clean.StatusDryRun {
			d.Summary.Estimate += o.Estimate
		}
		if o.Freed > 0 {
			d.Summary.Freed += o.Freed
		}
	}
	return d
}

// Exit codes, part of the contract.
const (
	ExitOK      = 0
	ExitError   = 1
	ExitUsage   = 2
	ExitRefused = 3 // at least one item refused, none failed
	ExitPartial = 4 // at least one item failed
)

// ExitCode for an apply.
func (d ApplyDoc) ExitCode() int {
	switch {
	case d.Summary.Failed > 0:
		return ExitPartial
	case d.Summary.Refused > 0:
		return ExitRefused
	}
	return ExitOK
}

// Capabilities is `fell capabilities --json`.
type Capabilities struct {
	SchemaVersion int               `json:"schema_version"`
	Tool          Tool              `json:"tool"`
	Commands      map[string]string `json:"commands"`
	Actions       []ActionInfo      `json:"actions"`
	Levels        []string          `json:"safety_levels"`
	Reasons       []ReasonInfo      `json:"reason_codes"`
	Owners        []OwnerInfo       `json:"owners"`
	Artifacts     []ArtifactInfo    `json:"artifact_kinds"`
	ExitCodes     map[string]int    `json:"exit_codes"`
	Trash         string            `json:"trash"`
}

type ActionInfo struct {
	Name        string `json:"name"`
	Reversible  bool   `json:"reversible"`
	Description string `json:"description"`
}

type ReasonInfo struct {
	Code    string   `json:"code"`
	Level   string   `json:"level"`
	Effects []string `json:"effects"`
}

type OwnerInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Managed     bool   `json:"app_managed"`
	HowToRemove string `json:"how_to_remove"`
}

type ArtifactInfo struct {
	Name       string `json:"name"`
	Regenerate string `json:"regenerate"`
}

// NewCapabilities describes what this build of fell can do.
func NewCapabilities(tool Tool) Capabilities {
	c := Capabilities{
		SchemaVersion: SchemaVersion, Tool: tool, Trash: clean.TrashName,
		Commands: map[string]string{
			"scan":         "fell scan --json [--ndjson] [--no-agents] [root...]",
			"apply":        "fell apply --plan <file|-> [--dry-run] [--force] [--unlock] [--permanent] [--json] [root...]",
			"capabilities": "fell capabilities --json",
		},
		Actions: []ActionInfo{
			{scan.ActionStrip, true, "Move the worktree's rebuildable, git-ignored artifacts (node_modules, .venv, build output…) to the trash; keep the worktree."},
			{scan.ActionRetire, true, "Save a rescue folder (bundle of commits only it holds, uncommitted patch, ignored secrets) to the trash, move the checkout to the trash, then prune git's record."},
		},
		Levels: []string{string(scan.Safe), string(scan.Caution), string(scan.Protected), string(scan.Live)},
		Reasons: []ReasonInfo{
			{scan.ReasonLive, "live", []string{"blocks strip", "blocks retire"}},
			{scan.ReasonNested, "protected", []string{"blocks retire"}},
			{scan.ReasonSubmodules, "protected", []string{"blocks retire: submodule repositories live in git's record of the worktree"}},
			{scan.ReasonInUse, "protected", []string{"strip needs --force", "retire needs --force"}},
			{scan.ReasonLivenessUnknown, "protected", []string{"strip needs --force", "retire needs --force"}},
			{scan.ReasonDirty, "protected", []string{"retire needs --force"}},
			{scan.ReasonOnlyHere, "protected", []string{"retire needs --force"}},
			{scan.ReasonAppTracked, "protected", []string{"blocks retire: remove it the owning app's way (owner.how_to_remove)"}},
			{scan.ReasonKeep, "protected", []string{"blocks retire: delete .worktree-keep first"}},
			{scan.ReasonUnknown, "protected", []string{"retire needs --force", "strip unavailable"}},
			{scan.ReasonLocked, "protected", []string{"retire needs --unlock"}},
			{scan.ReasonUnpushed, "caution", []string{"saved in the rescue bundle when no branch keeps it"}},
			{scan.ReasonNoRemote, "caution", nil},
			{scan.ReasonPrecious, "caution", []string{"copied to the rescue folder"}},
			{scan.ReasonIgnoredData, "caution", nil},
		},
		ExitCodes: map[string]int{"ok": ExitOK, "error": ExitError, "usage": ExitUsage, "refused": ExitRefused, "partial": ExitPartial},
	}
	for _, o := range agents.Owners {
		c.Owners = append(c.Owners, OwnerInfo{o.ID, o.Name, o.Managed, o.Removal})
	}
	for _, r := range scan.ArtifactRules {
		c.Artifacts = append(c.Artifacts, ArtifactInfo{r.Name, r.Regenerate})
	}
	return c
}
