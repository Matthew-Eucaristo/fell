package scan

import (
	"io/fs"
	"path/filepath"
	"runtime"
	"sync"
)

// Measurement is the size result for one worktree, applied with Apply on
// the goroutine that owns the Worktree.
type Measurement struct {
	ID           string
	Source       Size
	IgnoredOther int64
	Artifacts    map[string]Size // by Rel
	Repos        map[string]bool // artifacts holding a .git below
}

// Measure walks the worktree once: everything outside artifacts, then each
// artifact, with one hard-link table so links between them count once.
// It reads w but does not modify it.
func Measure(w *Worktree) Measurement {
	m := Measurement{ID: w.ID, Artifacts: map[string]Size{}, Repos: map[string]bool{}}
	if w.Kind == KindPrunable {
		return m
	}
	z := newSizer()
	skip := map[string]bool{}
	for _, a := range w.Artifacts {
		skip[a.Path] = true
	}
	for _, n := range w.Nested {
		skip[n] = true
	}
	var ignoredBytes Size
	_ = filepath.WalkDir(w.Path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if skip[path] {
			return fs.SkipDir
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		bucket := &m.Source
		if path != w.Path && isIgnored(w.ignored, filepath.ToSlash(path[len(w.Path)+1:]), d.IsDir()) {
			bucket = &ignoredBytes
		}
		if !z.add(path, info, bucket) && d.IsDir() {
			return fs.SkipDir
		}
		return nil
	})
	sizes := make([]Size, len(w.Artifacts))
	for i, a := range w.Artifacts {
		if z.walk(a.Path, &sizes[i]) {
			m.Repos[a.Rel] = true
		}
	}
	z.finish()
	m.IgnoredOther = ignoredBytes.Allocated
	m.Source = m.Source.Add(ignoredBytes)
	for i, a := range w.Artifacts {
		m.Artifacts[a.Rel] = sizes[i]
	}
	return m
}

// Apply stores a measurement and reclassifies.
func (w *Worktree) Apply(m Measurement) {
	w.Source, w.IgnoredOther = m.Source, m.IgnoredOther
	for _, a := range w.Artifacts {
		if s, ok := m.Artifacts[a.Rel]; ok {
			a.Size = s
		}
		a.Repo = m.Repos[a.Rel]
	}
	w.Measured = true
	w.Classify()
}

// MeasureAll measures every worktree in parallel and applies the results.
func (r *Result) MeasureAll() {
	ch := make(chan Measurement)
	go r.MeasureInto(ch)
	byID := map[string]*Worktree{}
	for _, w := range r.Worktrees {
		byID[w.ID] = w
	}
	for m := range ch {
		byID[m.ID].Apply(m)
	}
}

// MeasureInto sends one Measurement per worktree, then closes ch.
func (r *Result) MeasureInto(ch chan<- Measurement) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, max(2, runtime.GOMAXPROCS(0)/2))
	for _, w := range r.Worktrees {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			m := Measure(w)
			<-sem
			ch <- m
		}()
	}
	wg.Wait()
	close(ch)
}
