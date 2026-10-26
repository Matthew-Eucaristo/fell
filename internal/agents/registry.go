package agents

import (
	"encoding/json"
	"github.com/Matthew-Eucaristo/fell/internal/pathx"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Registry is a read-only view of Claude Desktop's git-worktrees.json, the
// pool of worktrees the app creates, leases to sessions and reuses. fell
// never writes it.
type Registry struct {
	Path    string
	Version int
	// Problem is set when the file exists but cannot be understood (corrupt,
	// unreadable, or a schema fell does not know). Callers then fail closed
	// and treat Claude worktrees as app-tracked.
	Problem string
	Entries []Record
	// Roots are the worktree parent dirs Desktop watches (untrackedDirGc.roots).
	Roots  []string
	byPath map[string]*Record
}

// Record is one pooled worktree.
type Record struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	BaseRepo string `json:"baseRepo"`
	Branch   string `json:"branch"`
	// LeasedBy is the session using it; nil when free or parked in the pool.
	LeasedBy *string `json:"leasedBy"`
	PooledAt int64   `json:"pooledAt"`
}

// Leased reports whether a session holds the worktree.
func (r *Record) Leased() bool { return r != nil && r.LeasedBy != nil && *r.LeasedBy != "" }

// LoadRegistry reads the registry at path. A missing or unreadable file
// gives an empty registry; an unknown schema gives one with only Version
// set, so callers treat nothing as tracked by mistake and the UI can warn.
func LoadRegistry(path string) *Registry {
	r := &Registry{Path: path, byPath: map[string]*Record{}}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return r
	}
	if err != nil {
		r.Problem = err.Error()
		return r
	}
	var raw struct {
		SchemaVersion int               `json:"schemaVersion"`
		Worktrees     map[string]Record `json:"worktrees"`
		UntrackedGC   struct {
			Roots map[string]json.RawMessage `json:"roots"`
		} `json:"untrackedDirGc"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		r.Problem = "cannot parse " + filepath.Base(path) + ": " + err.Error()
		return r
	}
	r.Version = raw.SchemaVersion
	if raw.SchemaVersion != 2 {
		r.Problem = "unknown schemaVersion " + strconv.Itoa(raw.SchemaVersion) + " in " + filepath.Base(path)
		return r
	}
	for _, rec := range raw.Worktrees {
		if rec.Path == "" || !filepath.IsAbs(rec.Path) {
			continue
		}
		r.Entries = append(r.Entries, rec)
	}
	for i := range r.Entries {
		r.byPath[key(r.Entries[i].Path)] = &r.Entries[i]
	}
	for root := range raw.UntrackedGC.Roots {
		if filepath.IsAbs(root) {
			r.Roots = append(r.Roots, root)
		}
	}
	return r
}

// Supported reports whether the file was absent or understood.
func (r *Registry) Supported() bool { return r == nil || r.Problem == "" }

// Lookup returns the record for a checkout path, or nil.
func (r *Registry) Lookup(path string) *Record {
	if r == nil {
		return nil
	}
	return r.byPath[key(path)]
}

// key compares paths the way macOS does by default (case-insensitive) and
// through symlinks when the path exists.
func key(p string) string {
	return strings.ToLower(pathx.Real(p))
}
