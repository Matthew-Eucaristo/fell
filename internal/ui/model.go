// Package ui is the interactive Bubble Tea front end.
package ui

import (
	"cmp"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Matthew-Eucaristo/fell/internal/clean"
	"github.com/Matthew-Eucaristo/fell/internal/report"
	"github.com/Matthew-Eucaristo/fell/internal/scan"
)

type status int

const (
	idle status = iota
	queued
	working
	done
	failed
)

type groupBy int

const (
	byOwner groupBy = iota
	byRepo
)

type sortBy int

const (
	bySize sortBy = iota
	byAge
	byName
)

func (s sortBy) String() string { return [...]string{"size", "oldest", "name"}[s] }

// row is a group header (w == nil) or a worktree.
type row struct {
	group string
	title string
	w     *scan.Worktree
}

func (r row) key() string {
	if r.w == nil {
		return "group:" + r.group
	}
	return r.w.ID
}

// confirm is a pending action awaiting y/n.
type confirm struct {
	action string
	ids    []string
	force  bool
	unlock bool
}

type (
	loadedMsg struct {
		res *scan.Result
		err error
	}
	measuredMsg scan.Measurement
	measureDone struct{}
	actedMsg    struct {
		out     *clean.Outcome
		updated *scan.Worktree // refreshed and measured after a strip
	}
)

// Model is the TUI state. All mutation happens in Update.
type Model struct {
	opts  scan.Options
	copts clean.Options
	home  string

	res       *scan.Result
	err       error
	sizes     chan scan.Measurement
	measuring bool
	freeDisk  int64

	group     groupBy
	sort      sortBy
	cursorKey string
	moved     bool // the user moved the cursor; until then it follows the top row
	offset    int
	width     int
	height    int
	legend    bool

	selected map[string]bool
	status   map[string]status
	errs     map[string]string
	remeas   map[string]bool // re-measured after a strip; ignore older sizes
	confirm  *confirm
	queue    []job
	quitWarn bool

	outcomes []*clean.Outcome
}

type job struct {
	action string
	id     string
	force  bool
	unlock bool
}

// New builds a model that scans with opts and acts through copts.
func New(opts scan.Options, copts clean.Options) *Model {
	return &Model{
		opts: opts, copts: copts, home: copts.Env.Home,
		selected: map[string]bool{}, status: map[string]status{}, errs: map[string]string{}, remeas: map[string]bool{},
		width: 100, height: 30, freeDisk: -1,
	}
}

// Outcomes reports what was done once the program has exited.
func (m *Model) Outcomes() []*clean.Outcome { return m.outcomes }

func (m *Model) Init() tea.Cmd { return m.load() }

func (m *Model) load() tea.Cmd {
	opts := m.opts
	return func() tea.Msg {
		res, err := scan.Scan(opts)
		return loadedMsg{res, err}
	}
}

func waitSize(ch chan scan.Measurement) tea.Cmd {
	return func() tea.Msg {
		s, ok := <-ch
		if !ok {
			return measureDone{}
		}
		return measuredMsg(s)
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	defer m.scroll()
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case loadedMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.res = msg.res
		m.freeDisk = diskFree(m.opts.Roots)
		m.sizes = make(chan scan.Measurement)
		m.measuring = true
		go m.res.MeasureInto(m.sizes)
		if rows := m.rows(); len(rows) > 0 && m.cursorKey == "" {
			m.cursorKey = firstItem(rows)
		}
		return m, waitSize(m.sizes)
	case measuredMsg:
		if w := m.find(msg.ID); w != nil && !m.remeas[msg.ID] {
			w.Apply(scan.Measurement(msg))
		}
		if rows := m.rows(); !m.moved && len(rows) > 0 {
			m.cursorKey = firstItem(rows) // sizes reorder the list as they arrive
		}
		return m, waitSize(m.sizes)
	case measureDone:
		m.measuring = false
	case actedMsg:
		o := msg.out
		m.outcomes = append(m.outcomes, o)
		delete(m.selected, o.ID)
		switch o.Status {
		case clean.StatusDone, clean.StatusDryRun:
			m.status[o.ID] = done
			if o.Action == scan.ActionStrip {
				m.status[o.ID] = idle // the worktree stays; it can be retired later
				if msg.updated != nil {
					m.replace(msg.updated)
					m.remeas[o.ID] = true
				}
			}
			delete(m.errs, o.ID)
		default:
			m.status[o.ID] = failed
			m.errs[o.ID] = o.Error
		}
		m.freeDisk = diskFree(m.opts.Roots)
		return m, m.next()
	case tea.KeyPressMsg:
		return m, m.key(msg.String())
	}
	return m, nil
}

func (m *Model) key(k string) tea.Cmd {
	if k == "ctrl+c" {
		if m.busy() && !m.quitWarn {
			m.quitWarn = true
			return nil
		}
		return tea.Quit
	}
	if m.legend {
		m.legend = false
		return nil
	}
	if m.confirm != nil {
		switch k {
		case "y", "Y":
			return m.start()
		case "f":
			m.confirm.force = !m.confirm.force
		case "u":
			m.confirm.unlock = !m.confirm.unlock
		default:
			m.confirm = nil
		}
		return nil
	}
	if m.res == nil {
		if k == "q" || k == "esc" {
			return tea.Quit
		}
		return nil
	}
	rows := m.rows()
	i := m.cursor(rows)
	switch k {
	case "q", "esc":
		if m.busy() {
			m.quitWarn = true
			return nil
		}
		return tea.Quit
	case "?":
		m.legend = true
		return nil
	case "up", "k":
		i--
	case "down", "j":
		i++
	case "pgup", "ctrl+u":
		i -= m.bodyHeight()
	case "pgdown", "ctrl+d":
		i += m.bodyHeight()
	case "home":
		i = 0
	case "end":
		i = len(rows) - 1
	case "tab":
		m.group = 1 - m.group
		m.offset = 0
		return nil
	case "o":
		m.sort = (m.sort + 1) % 3
		return nil
	case "R", "ctrl+r":
		if m.busy() || m.measuring {
			return nil
		}
		m.moved = false
		m.res, m.status, m.errs, m.selected, m.remeas = nil, map[string]status{}, map[string]string{}, map[string]bool{}, map[string]bool{}
		return m.load()
	case "space":
		if len(rows) > 0 {
			m.toggle(m.leaves(rows[i]))
		}
		return nil
	case "a":
		var all []string
		for _, r := range rows {
			if r.w != nil && r.w.Safety.Level != scan.Live {
				all = append(all, r.w.ID)
			}
		}
		m.toggle(all)
		return nil
	case "s":
		m.ask(scan.ActionStrip, rows, i)
		return nil
	case "r", "d", "delete", "backspace":
		m.ask(scan.ActionRetire, rows, i)
		return nil
	default:
		return nil
	}
	if len(rows) > 0 {
		i = max(0, min(i, len(rows)-1))
		m.cursorKey, m.moved = rows[i].key(), true
	}
	return nil
}

// leaves are the worktree IDs a row stands for.
func (m *Model) leaves(r row) []string {
	if r.w != nil {
		return []string{r.w.ID}
	}
	var ids []string
	for _, x := range m.rows() {
		if x.w != nil && x.group == r.group {
			ids = append(ids, x.w.ID)
		}
	}
	return ids
}

func (m *Model) toggle(ids []string) {
	ids = slices.DeleteFunc(slices.Clone(ids), func(id string) bool { return m.status[id] == done || m.status[id] == working })
	all := len(ids) > 0
	for _, id := range ids {
		all = all && m.selected[id]
	}
	for _, id := range ids {
		if all {
			delete(m.selected, id)
		} else {
			m.selected[id] = true
		}
	}
}

// ask targets the selection, or the row under the cursor.
func (m *Model) ask(action string, rows []row, i int) {
	var ids []string
	for _, r := range rows {
		if r.w != nil && m.selected[r.w.ID] {
			ids = append(ids, r.w.ID)
		}
	}
	if len(ids) == 0 && len(rows) > 0 {
		ids = m.leaves(rows[i])
	}
	ids = slices.DeleteFunc(ids, func(id string) bool { s := m.status[id]; return s == done || s == working || s == queued })
	if len(ids) > 0 {
		m.confirm = &confirm{action: action, ids: ids}
	}
}

// plan splits the confirmation's items into those that will run and those
// refused, given the current force/unlock toggles.
func (m *Model) plan() (run, refuse []*scan.Worktree) {
	for _, id := range m.confirm.ids {
		w := m.find(id)
		if w == nil {
			continue
		}
		if w.Decide(m.confirm.action).Permits(m.confirm.force, m.confirm.unlock) {
			run = append(run, w)
		} else {
			refuse = append(refuse, w)
		}
	}
	return run, refuse
}

func (m *Model) start() tea.Cmd {
	run, _ := m.plan()
	for _, w := range run {
		m.status[w.ID] = queued
		m.queue = append(m.queue, job{m.confirm.action, w.ID, m.confirm.force, m.confirm.unlock})
	}
	m.confirm = nil
	if m.working() {
		return nil
	}
	return m.next()
}

// next runs queued jobs one at a time, so prunes in one repository never
// race each other.
func (m *Model) next() tea.Cmd {
	if len(m.queue) == 0 {
		m.quitWarn = false
		return nil
	}
	j := m.queue[0]
	m.queue = m.queue[1:]
	w := m.find(j.id)
	if w == nil {
		return m.next()
	}
	m.status[j.id] = working
	wc := w.Clone()
	opts := m.copts
	opts.Force, opts.Unlock = j.force, j.unlock
	return func() tea.Msg {
		if j.action == scan.ActionStrip {
			o := clean.Strip(wc, wc.Fingerprint, nil, opts)
			var upd *scan.Worktree
			if o.Status == clean.StatusDone && opts.Mode != clean.DryRun {
				if nw, err := scan.Refresh(wc, opts.Env); err == nil {
					nw.Apply(scan.Measure(nw))
					upd = nw
				}
			}
			return actedMsg{o, upd}
		}
		return actedMsg{clean.Retire(wc, wc.Fingerprint, opts), nil}
	}
}

// scroll keeps the cursor row inside the visible list.
func (m *Model) scroll() {
	rows := m.rows()
	cur, body := m.cursor(rows), m.listHeight()
	if cur < m.offset {
		m.offset = cur
	}
	if cur >= m.offset+body {
		m.offset = cur - body + 1
	}
	m.offset = max(0, min(m.offset, len(rows)-body))
}

func (m *Model) busy() bool { return m.working() || len(m.queue) > 0 }

func (m *Model) working() bool {
	for _, s := range m.status {
		if s == working {
			return true
		}
	}
	return false
}

func (m *Model) find(id string) *scan.Worktree {
	if m.res == nil {
		return nil
	}
	for _, w := range m.res.Worktrees {
		if w.ID == id {
			return w
		}
	}
	return nil
}

func (m *Model) replace(nw *scan.Worktree) {
	for i, w := range m.res.Worktrees {
		if w.ID == nw.ID {
			m.res.Worktrees[i] = nw
		}
	}
}

// rows lists groups and their worktrees in the current order.
func (m *Model) rows() []row {
	if m.res == nil {
		return nil
	}
	groups := map[string][]*scan.Worktree{}
	var order []string
	title := map[string]string{}
	for _, w := range m.res.Worktrees {
		g, t := w.Owner.ID, w.Owner.Name
		if m.group == byRepo {
			g, t = w.CommonDir+"\x00"+w.Repo, w.Repo
			if w.CommonDir == "" {
				g = "orphans"
				t = "orphaned checkouts"
			}
		}
		if _, ok := groups[g]; !ok {
			order = append(order, g)
			title[g] = t
		}
		groups[g] = append(groups[g], w)
	}
	if m.group == byRepo {
		slices.SortStableFunc(order, func(a, b string) int { return cmp.Compare(strings.ToLower(title[a]), strings.ToLower(title[b])) })
	}
	var rows []row
	for _, g := range order {
		ws := groups[g]
		slices.SortStableFunc(ws, func(a, b *scan.Worktree) int {
			switch m.sort {
			case byAge:
				return a.LastActivity.Compare(b.LastActivity)
			case byName:
				return cmp.Compare(report.Name(a), report.Name(b))
			}
			return cmp.Compare(b.Total().Allocated, a.Total().Allocated)
		})
		rows = append(rows, row{group: g, title: title[g]})
		for _, w := range ws {
			rows = append(rows, row{group: g, title: title[g], w: w})
		}
	}
	return rows
}

func firstItem(rows []row) string {
	for _, r := range rows {
		if r.w != nil {
			return r.key()
		}
	}
	return rows[0].key()
}

func (m *Model) cursor(rows []row) int {
	for i, r := range rows {
		if r.key() == m.cursorKey {
			return i
		}
	}
	return 0
}
