package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Matthew-Eucaristo/fell/internal/clean"
	"github.com/Matthew-Eucaristo/fell/internal/format"
	"github.com/Matthew-Eucaristo/fell/internal/report"
	"github.com/Matthew-Eucaristo/fell/internal/scan"
)

// Plain ANSI colors so the terminal theme decides the actual shades.
var (
	accent  = lipgloss.Color("2")
	brand   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(accent).Padding(0, 1)
	bold    = lipgloss.NewStyle().Bold(true)
	faint   = lipgloss.NewStyle().Faint(true)
	cyan    = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	green   = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	yellow  = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	red     = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	magenta = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))
	struck  = lipgloss.NewStyle().Faint(true).Strikethrough(true)
	heading = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	cursorC = green.Render("▌")
)

const (
	barWidth   = 14
	detailRows = 6
	// header, blank, column header, separator + detail, separator, summary, help
	chromeLines = 6 + detailRows
)

func (m *Model) bodyHeight() int { return max(3, m.height-chromeLines) }

func (m *Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.WindowTitle = "fell"
	return v
}

func (m *Model) render() string {
	if m.err != nil {
		return red.Render("fell: "+m.err.Error()) + "\n\n" + faint.Render("q quit")
	}
	if m.res == nil {
		return m.header() + "\n\n  " + faint.Render("Scanning "+m.whereText()+" …")
	}
	var b strings.Builder
	line := func(s string) { b.WriteString(ansi.Truncate(s, m.width, "…") + "\n") }
	line(m.header())
	line("")
	rows := m.rows()
	cur := m.cursor(rows)
	body := m.bodyHeight()
	var lines []string
	switch {
	case m.legend:
		lines = m.legendLines()
	case m.confirm != nil:
		lines = m.confirmLines(body + 1)
	case len(rows) == 0:
		lines = []string{"", "  " + faint.Render("No linked worktrees in "+m.whereText()+"."), "",
			"  " + faint.Render("fell lists worktrees created with `git worktree add` by you or by agents such as Claude Code, Codex, Cursor or Conductor.")}
	default:
		line(faint.Render(fmt.Sprintf("    %-*s  %9s  %4s  %-8s  %-10s %s", barWidth, "", "SIZE", "REB.", "ACTIVE", "SAFETY", "WORKTREE  (sort: "+m.sort.String()+")")))
		body--
		if cur < m.offset {
			m.offset = cur
		}
		if cur >= m.offset+body {
			m.offset = cur - body + 1
		}
		m.offset = max(0, min(m.offset, max(0, len(rows)-body)))
		largest := int64(1)
		for _, r := range rows {
			if r.w != nil {
				largest = max(largest, r.w.Total().Allocated)
			}
		}
		for i := m.offset; i < min(len(rows), m.offset+body); i++ {
			lines = append(lines, m.row(rows[i], i == cur, largest))
		}
	}
	for i := range body {
		if i < len(lines) {
			line(lines[i])
		} else {
			line("")
		}
	}
	line(faint.Render(strings.Repeat("─", max(0, m.width))))
	var detail []string
	if m.confirm == nil && !m.legend && len(rows) > 0 {
		detail = m.detail(rows[cur])
	}
	for i := range detailRows {
		if i < len(detail) {
			line(detail[i])
		} else {
			line("")
		}
	}
	line(m.summary())
	b.WriteString(ansi.Truncate(m.help(), m.width, "…"))
	return b.String()
}

func (m *Model) rootsText() string {
	var rs []string
	for _, r := range m.opts.Roots {
		rs = append(rs, format.Tilde(r, m.home))
	}
	return strings.Join(rs, ", ")
}

// whereText names what was scanned: the roots, plus agent locations unless
// --no-agents.
func (m *Model) whereText() string {
	if m.opts.Agents {
		return m.rootsText() + " + agent locations"
	}
	return m.rootsText()
}

func (m *Model) header() string {
	mode := green.Render("● trash")
	switch m.copts.Mode {
	case clean.DryRun:
		mode = yellow.Render("● dry run")
	case clean.Permanent:
		mode = red.Render("● permanent")
	}
	parts := []string{brand.Render("fell") + " " + mode}
	if m.res != nil {
		var total, strip scan.Size
		for _, w := range m.res.Worktrees {
			if m.status[w.ID] == done {
				continue
			}
			total = total.Add(w.Total())
			strip = strip.Add(w.ArtifactBytes())
		}
		parts = append(parts, bold.Render(format.Bytes(total.Allocated))+fmt.Sprintf(" in %d worktrees", len(m.res.Worktrees)),
			cyan.Render(format.Bytes(strip.Allocated))+" rebuildable"+freeable(strip))
		if m.freeDisk >= 0 {
			parts = append(parts, format.Bytes(m.freeDisk)+" free")
		}
		if m.measuring {
			parts = append(parts, faint.Render("measuring…"))
		}
	}
	parts = append(parts, faint.Render(m.whereText()))
	return " " + strings.Join(parts, faint.Render("  ·  "))
}

func (m *Model) row(r row, isCursor bool, largest int64) string {
	cur := " "
	if isCursor {
		cur = cursorC
	}
	if r.w == nil {
		var n int
		var size int64
		for _, x := range m.rows() {
			if x.w != nil && x.group == r.group {
				n++
				size += x.w.Total().Allocated
			}
		}
		check := m.check(m.leaves(r))
		return fmt.Sprintf("%s %s %s  %s", cur, check, heading.Render(r.title), faint.Render(fmt.Sprintf("%d worktree(s) · %s", n, format.Bytes(size))))
	}
	w := r.w
	size := format.Bytes(w.Total().Allocated)
	share := "    "
	if !w.Measured && w.Kind != scan.KindPrunable {
		size = format.Bytes(-1)
	} else if w.Kind == scan.KindWorktree && w.Total().Allocated > 0 {
		share = fmt.Sprintf("%3d%%", w.ArtifactBytes().Allocated*100/w.Total().Allocated)
	}
	line := fmt.Sprintf("%s  %9s  %s  %s  %s %s  %s",
		m.bar(w, largest), size, cyan.Render(share), m.age(w.LastActivity), badge(w.Safety.Level),
		bold.Render(report.Name(w)), m.flags(w))
	switch m.status[w.ID] {
	case done:
		line = struck.Render(ansi.Strip(line))
	case working, queued:
		line = faint.Render(ansi.Strip(line))
	case failed:
		line = red.Render(ansi.Strip(line))
	}
	return fmt.Sprintf("%s %s %s", cur, m.check([]string{w.ID}), line)
}

// bar shows size relative to the largest worktree with eighth-block
// resolution; the rebuildable part is cyan, the rest in the safety colour.
func (m *Model) bar(w *scan.Worktree, largest int64) string {
	t := w.Total().Allocated
	if !w.Measured || t <= 0 {
		return faint.Render(strings.Repeat("·", barWidth))
	}
	cells := []rune(fine(t, largest, barWidth))
	reb := int(int64(len(cells)) * w.ArtifactBytes().Allocated / t)
	return cyan.Render(string(cells[:reb])) + levelStyle(w.Safety.Level).Render(string(cells[reb:])) +
		faint.Render(strings.Repeat("·", barWidth-len(cells)))
}

// eighths are partial block glyphs, so small differences still show.
var eighths = []string{"", "▏", "▎", "▍", "▌", "▋", "▊", "▉"}

// fine draws n/total of width cells with eighth-block resolution.
func fine(n, total int64, width int) string {
	if total <= 0 || n <= 0 {
		return ""
	}
	units := int(n * int64(width) * 8 / total)
	units = max(1, min(units, width*8))
	return strings.Repeat("█", units/8) + eighths[units%8]
}

func levelStyle(l scan.Level) lipgloss.Style {
	switch l {
	case scan.Safe:
		return green
	case scan.Caution:
		return yellow
	case scan.Protected:
		return red
	}
	return magenta
}

func badge(l scan.Level) string {
	icon := map[scan.Level]string{scan.Safe: "●", scan.Caution: "▲", scan.Protected: "■", scan.Live: "◉"}[l]
	return levelStyle(l).Render(fmt.Sprintf("%s %-9s", icon, l))
}

func (m *Model) flags(w *scan.Worktree) string {
	var parts []string
	for _, f := range report.Flags(w) {
		switch {
		case strings.HasPrefix(f, "↑"), strings.HasPrefix(f, "only-here"), strings.HasPrefix(f, "✎"), f == "in-use":
			parts = append(parts, yellow.Render(f))
		case strings.HasPrefix(f, "secrets"), f == "locked", f == "orphaned", f == "checkout missing":
			parts = append(parts, red.Render(f))
		case f == "app-tracked":
			parts = append(parts, magenta.Render(f))
		default:
			parts = append(parts, faint.Render(f))
		}
	}
	if e := m.errs[w.ID]; e != "" {
		parts = append(parts, red.Render("✗ "+e))
	}
	return strings.Join(parts, " ")
}

func (m *Model) check(ids []string) string {
	sel, fin := 0, 0
	for _, id := range ids {
		switch {
		case m.status[id] == done:
			fin++
		case m.status[id] == working || m.status[id] == queued:
			return faint.Render("◌")
		case m.selected[id]:
			sel++
		}
	}
	switch {
	case len(ids) > 0 && fin == len(ids):
		return green.Render("✓")
	case sel > 0 && sel+fin == len(ids):
		return cyan.Render("●")
	case sel > 0:
		return cyan.Render("◐")
	}
	return faint.Render("○")
}

func (m *Model) age(t time.Time) string {
	s := fmt.Sprintf("%-8s", format.Ago(t, time.Now()))
	switch d := time.Since(t); {
	case t.IsZero():
		return faint.Render(s)
	case d < 7*24*time.Hour:
		return green.Render(s)
	case d >= 90*24*time.Hour:
		return yellow.Render(s)
	}
	return s
}

// detail is the evidence for the row under the cursor.
func (m *Model) detail(r row) []string {
	pad := "  "
	if r.w == nil {
		return []string{pad + heading.Render(r.title), pad + faint.Render("space selects the whole group · s strip · r retire")}
	}
	w := r.w
	var out []string
	where := w.Owner.Name
	switch {
	case w.Detached:
		where += " · detached at " + short(w.Head)
	case w.Branch != "":
		where += " · branch " + w.Branch
	}
	src := map[string]string{"commit": "last commit", "git": "last git activity", "files": "last file change", "": "activity unknown"}[w.ActivitySource]
	out = append(out, pad+bold.Render(report.Name(w))+"  "+faint.Render(where+" · "+src+" "+format.Ago(w.LastActivity, time.Now())))
	where2 := format.Tilde(w.Path, m.home) + "  [" + w.ID + "]"
	if w.Measured && w.Kind != scan.KindPrunable {
		where2 += "  ·  " + format.Bytes(w.Total().Allocated) + " on disk" + freeable(w.Total())
	}
	out = append(out, pad+faint.Render(where2))

	var ev []string
	if len(w.Artifacts) > 0 {
		var arts []string
		for _, a := range w.Artifacts {
			s := a.Rel
			if w.Measured {
				s += " " + format.Bytes(a.Size.Allocated)
				if a.Size.Shared > 0 {
					s += fmt.Sprintf(" (%s shared)", format.Bytes(a.Size.Shared))
				}
			}
			switch {
			case a.Repo:
				s += " (holds a repository, kept)"
			case !a.Ignored:
				s += " (not ignored, kept)"
			}
			arts = append(arts, s)
		}
		ev = append(ev, cyan.Render("rebuildable: ")+strings.Join(arts, ", "))
	}
	commits := "commits: all on a remote"
	switch {
	case w.Kind == scan.KindOrphan:
		commits = "commits: unknown (git record gone)"
	case w.OnlyHere > 0:
		commits = yellow.Render(fmt.Sprintf("commits: %d only here (bundled on retire)", w.OnlyHere))
	case w.Unpushed > 0:
		commits = yellow.Render(fmt.Sprintf("commits: %d not pushed, kept on branch %s", w.Unpushed, w.Branch))
	case !w.HasRemotes:
		commits = "commits: repo has no remote; all on local branches"
	}
	ev = append(ev, commits)
	if n := w.Dirty.Total(); n > 0 {
		ev = append(ev, yellow.Render(fmt.Sprintf("uncommitted: %d", n)))
	}
	var prec []string
	for _, p := range w.Precious {
		if p.AtRisk {
			prec = append(prec, p.Rel)
		} else if p.Link != "" {
			prec = append(prec, p.Rel+"→link")
		}
	}
	if len(prec) > 0 {
		ev = append(ev, red.Render("ignored secrets/data: ")+strings.Join(prec, ", "))
	}
	out = append(out, pad+strings.Join(ev, faint.Render("  ·  ")))

	st, rt := w.Decide(scan.ActionStrip), w.Decide(scan.ActionRetire)
	out = append(out, pad+levelStyle(w.Safety.Level).Render(string(w.Safety.Level))+"  "+decision("strip", st)+"  "+decision("retire", rt))
	var why []string
	for _, rs := range w.Safety.Reasons {
		if rs.Code == scan.ReasonAppTracked || rs.Code == scan.ReasonPrecious {
			continue // shown elsewhere
		}
		why = append(why, rs.Detail)
	}
	if len(why) > 0 {
		out = append(out, pad+faint.Render(strings.Join(why, " · ")))
	}
	if e := m.errs[w.ID]; e != "" {
		out = append(out, pad+red.Render(e))
	}
	if w.Tracked {
		// The official way, wrapped into whatever detail rows are left.
		text := ansi.Wordwrap(w.Owner.Name+" tracks it. "+w.Owner.Removal, max(20, m.width-4), "")
		for _, l := range strings.Split(text, "\n") {
			out = append(out, pad+magenta.Render(l))
		}
	}
	if len(out) > detailRows {
		out = append(out[:detailRows-1], ansi.Truncate(out[detailRows-1], m.width-1, "…"))
	}
	return out
}

func decision(name string, d scan.Decision) string {
	switch {
	case d.Allowed:
		return green.Render(name + " ✓")
	case !d.Possible():
		return red.Render(name + " ✗ " + strings.Join(d.Blocked, ","))
	}
	var need []string
	if len(d.Override) > 0 {
		need = append(need, "force: "+strings.Join(d.Override, ","))
	}
	if d.Unlock {
		need = append(need, "unlock")
	}
	return yellow.Render(name + " needs " + strings.Join(need, "; "))
}

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

func (m *Model) summary() string {
	var sel int
	var bytes int64
	for id := range m.selected {
		if w := m.find(id); w != nil {
			sel++
			bytes += w.Total().Allocated
		}
	}
	parts := []string{fmt.Sprintf("%d selected (%s)", sel, format.Bytes(bytes))}
	var est, freed int64
	var n, bad int
	for _, o := range m.outcomes {
		switch o.Status {
		case clean.StatusDone, clean.StatusDryRun:
			n++
			est += o.Estimate
			if o.Freed > 0 {
				freed += o.Freed
			}
		default:
			bad++
		}
	}
	if n > 0 {
		verb := "Moved to " + clean.TrashName
		if m.copts.Mode == clean.DryRun {
			verb = "Would move"
		}
		parts = append(parts, green.Render(fmt.Sprintf("%s: %s", verb, format.Bytes(est))))
		if m.copts.Mode != clean.DryRun {
			parts = append(parts, faint.Render("free space "+signed(freed)+" (measured; empty the "+clean.TrashName+" to reclaim)"))
		}
	}
	if bad > 0 {
		parts = append(parts, red.Render(fmt.Sprintf("%d refused/failed", bad)))
	}
	if len(m.queue) > 0 {
		parts = append(parts, faint.Render(fmt.Sprintf("%d queued", len(m.queue))))
	}
	return "  " + strings.Join(parts, faint.Render("  ·  "))
}

func signed(n int64) string {
	if n > 0 {
		return "+" + format.Bytes(n)
	}
	return format.Bytes(max(n, 0))
}

func (m *Model) help() string {
	switch {
	case m.legend:
		return faint.Render("  any key closes")
	case m.confirm != nil:
		return yellow.Render("  Proceed?") + "  " + bold.Render("y") + faint.Render(" yes   ") + bold.Render("f") + faint.Render(" toggle force   ") +
			bold.Render("u") + faint.Render(" toggle unlock   any other key cancels")
	case m.quitWarn:
		return yellow.Render("  Actions still running. Press ctrl+c again to quit anyway, or wait.")
	}
	return faint.Render("  ↑↓ move  space select  a all  s strip  r retire  tab group  o sort  R rescan  ? legend  q quit")
}

// confirmLines lists exactly what the pending action will do and refuse.
func (m *Model) confirmLines(height int) []string {
	c := m.confirm
	run, refuse := m.plan()
	var total int64
	for _, w := range run {
		if c.action == scan.ActionStrip {
			total += w.ArtifactBytes().Allocated
		} else {
			total += w.Total().Allocated
		}
	}
	verb := map[string]string{scan.ActionStrip: "Strip rebuildable artifacts from", scan.ActionRetire: "Retire"}[c.action]
	dest := "to the " + clean.TrashName
	if m.copts.Mode == clean.DryRun {
		verb, dest = "Dry run: "+strings.ToLower(verb[:1])+verb[1:], "(nothing will change)"
	}
	toggles := ""
	if c.force {
		toggles += red.Render("  [force]")
	}
	if c.unlock {
		toggles += red.Render("  [unlock]")
	}
	lines := []string{"", yellow.Render(fmt.Sprintf("  %s %d worktree(s), about %s %s", verb, len(run), format.Bytes(total), dest)) + toggles, ""}
	if c.action == scan.ActionRetire {
		lines = append(lines, faint.Render("  Each: rescue folder (commits only it has, uncommitted patch, ignored secrets) → "+clean.TrashName+","),
			faint.Render("        checkout → "+clean.TrashName+", then `git worktree prune` if only its own record would go. Branches are kept."), "")
	} else {
		lines = append(lines, faint.Render("  Only git-ignored, untracked directories from the catalog move; the worktree stays."), "")
	}
	room := height - len(lines) - 2
	for i, w := range run {
		if i >= room/2 && len(run) > room/2 {
			lines = append(lines, faint.Render(fmt.Sprintf("    … and %d more", len(run)-i)))
			break
		}
		lines = append(lines, fmt.Sprintf("    %9s  %s  %s", format.Bytes(w.Total().Allocated), bold.Render(report.Name(w)), m.saves(c.action, w)))
	}
	if len(refuse) > 0 {
		lines = append(lines, "", red.Render(fmt.Sprintf("  Not included (%d):", len(refuse))))
		for i, w := range refuse {
			if len(lines) >= height-1 {
				lines = append(lines, faint.Render(fmt.Sprintf("    … and %d more", len(refuse)-i)))
				break
			}
			d := w.Decide(c.action)
			why := strings.Join(d.Blocked, ", ")
			if why == "" {
				var need []string
				if len(d.Override) > 0 && !c.force {
					need = append(need, "press f to force ("+strings.Join(d.Override, ", ")+")")
				}
				if d.Unlock && !c.unlock {
					need = append(need, "press u to unlock")
				}
				why = strings.Join(need, "; ")
			} else if w.Tracked && c.action == scan.ActionRetire {
				why = "tracked by " + w.Owner.Name + "; remove it the official way: " + w.Owner.Short
			}
			lines = append(lines, fmt.Sprintf("    %s  %s", report.Name(w), faint.Render(why)))
		}
	}
	return lines
}

func (m *Model) saves(action string, w *scan.Worktree) string {
	if action == scan.ActionStrip {
		var names []string
		for _, a := range w.Artifacts {
			if a.Eligible() {
				names = append(names, a.Rel)
			}
		}
		return cyan.Render(strings.Join(names, ", "))
	}
	var s []string
	if w.OnlyHere > 0 {
		s = append(s, fmt.Sprintf("bundle %d commit(s)", w.OnlyHere))
	}
	if w.Dirty.Total() > 0 {
		s = append(s, "uncommitted patch")
	}
	if n := w.PreciousAtRisk(); n > 0 {
		s = append(s, fmt.Sprintf("%d secret file(s)", n))
	}
	if len(s) == 0 {
		return faint.Render("nothing extra to save")
	}
	return yellow.Render("saves " + strings.Join(s, ", "))
}

func (m *Model) legendLines() []string {
	return []string{
		"",
		"  " + bold.Render("Safety"),
		"    " + badge(scan.Safe) + "  nothing would be lost; retire allowed",
		"    " + badge(scan.Caution) + "  allowed; something is saved to the rescue folder or worth a look",
		"    " + badge(scan.Protected) + "  retire needs force/unlock, or is blocked (app-tracked, keep marker)",
		"    " + badge(scan.Live) + "  its app or session is running; fell will not touch it",
		"",
		"  " + bold.Render("Bar") + "  " + cyan.Render("███") + " rebuildable artifacts  " + green.Render("██▍") + " the rest, in the safety colour; length is relative to the largest",
		"",
		"  " + bold.Render("strip") + "   moves node_modules, .venv, build output … (git-ignored, nothing tracked) to the " + clean.TrashName + "; keeps the worktree",
		"  " + bold.Render("retire") + "  saves a rescue folder, moves the checkout to the " + clean.TrashName + ", prunes git's record; keeps the branch",
		"",
		"  Sizes count hard links once; on macOS, blocks shared with APFS clones or snapshots are \"shared\" and not counted as freeable.",
		"  Freed space is measured from the disk after acting, not assumed.",
		"  Worktrees an app still tracks are only stripped; fell shows how that app removes them.",
	}
}

func diskFree(roots []string) int64 {
	if len(roots) == 0 {
		return -1
	}
	return clean.FreeBytes(roots[0])
}

// freeable notes the part removal would actually free, when it differs.
func freeable(s scan.Size) string {
	if s.Shared <= 0 {
		return ""
	}
	return " (" + format.Bytes(s.Exclusive()) + " freeable)"
}
