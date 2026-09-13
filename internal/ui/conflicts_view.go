package ui

import (
	"fmt"
	"strings"

	"github.com/allisonhere/tideui"
)

func (m *Model) conflictsView(r tideui.Renderer) string {
	c := m.conflicts
	mode, w := m.paneLayout()
	if mode == tideui.ThreeColumn {
		w = [3]int{m.width*3/10 - 2, m.width*3/10 - 2, m.width - m.width*3/10 - m.width*3/10 - 2}
	}
	height := m.historyPaneHeight()

	title, hint := "CONFLICT", ""
	if conflict, ok := c.current(); ok {
		hint = safeText(conflict.File.Path)
		title = strings.ToUpper(conflict.Kind.Label())
	}
	if c.mode == inspectWorking {
		title = "WORKING RESULT"
	}
	layout := tideui.Layout{Width: m.width, Height: m.height - 3, Mode: tideui.ThreeColumn,
		ColumnRatios: [3]float64{3, 3, 4}, SidebarRatio: 0.30, Panes: [3]tideui.Pane{
			{Title: "CONFLICTS", Hint: fmt.Sprint(len(c.conflicts)), Content: m.conflictListPane(r, w[0], height), Focused: m.focus == 0},
			{Title: "REGIONS", Hint: fmt.Sprint(len(c.regions)), Content: m.conflictRegionPane(r, w[1], height), Focused: m.focus == 1},
			{Title: title, Hint: hint, Content: m.conflictInspectorPane(r, w[2], height), Focused: m.focus == 2},
		}, Status: &tideui.StatusBar{Left: clip(safeText(m.conflictsStatus()), m.width-3)}}
	layout.Mode = mode
	layout.UpperRightRatio = 0.40

	base := m.globalHeader(r, "CONFLICTS") + "\n" + r.Render(layout) + "\n" + m.conflictsHints(r)
	return m.withOverlays(r, base)
}

func (m *Model) conflictsStatus() string {
	c := m.conflicts
	switch {
	case c.err != "":
		return "Git could not complete the request · " + firstLine(c.err)
	case m.busy:
		return m.activity() + " " + m.operation
	case c.loading || c.detailLoading:
		return m.activity() + " Reading conflicts"
	case m.notice != "":
		return m.notice
	}
	if len(c.conflicts) == 0 {
		if m.repoState.InProgress() {
			return "All conflicts resolved · continue or abort the " + m.repoState.Operation.Label()
		}
		return "No conflicts · the working tree is clean"
	}
	conflict, _ := c.current()
	return fmt.Sprintf("%d unresolved · %s · %s", len(c.conflicts), conflict.Kind.Label(), safeText(conflict.File.Path))
}

func (m *Model) conflictsHints(r tideui.Renderer) string {
	c := m.conflicts
	if c.filtering {
		return m.hintBar(r, tideui.SoftHint{Key: "/", Label: safeText(c.filter)},
			tideui.SoftHint{Key: "Enter", Label: "keep"}, tideui.SoftHint{Key: "Esc", Label: "clear"})
	}
	if len(c.conflicts) == 0 && m.repoState.InProgress() {
		hints := []tideui.SoftHint{{Key: "c", Label: "continue"}, {Key: "A", Label: "abort"}}
		if m.repoState.CanSkip() {
			hints = append(hints, tideui.SoftHint{Key: "x", Label: "skip"})
		}
		return m.hintBar(r, hints...)
	}
	hints := []tideui.SoftHint{
		{Key: "o / t", Label: "ours / theirs"}, {Key: "O / T", Label: "and resolve"},
		{Key: "b", Label: "keep both"}, {Key: "m", Label: "mark resolved"},
		{Key: "e", Label: "editor"}, {Key: "c", Label: "continue"}, {Key: "A", Label: "abort"},
	}
	return m.hintBar(r, hints...)
}

// conflictListPane groups unmerged files by kind, so the shape of the conflict
// is legible before any file is opened.
func (m *Model) conflictListPane(r tideui.Renderer, width, height int) string {
	c := m.conflicts
	inner := max(1, width-2)
	if len(c.conflicts) == 0 {
		if c.loading {
			return inset("\n"+muted(r, m.activity()+" Reading conflicts…"), width)
		}
		if m.repoState.InProgress() {
			return inset("\n\n"+accent(r, "All conflicts resolved")+"\n\n"+
				muted(r, "Continue with "+m.repoState.Operation.Label()+"\nfrom the footer, or abort."), width)
		}
		body := "Nothing is unmerged.\nResolve changes with Git, then refresh."
		if c.filter != "" {
			body = "No conflict matches " + safeText(c.filter) + "."
		}
		return inset("\n\n"+accent(r, "No conflicts")+"\n\n"+muted(r, body), width)
	}

	rows := c.rows()
	visible := max(1, height-2)
	cursor := c.cursor()
	if cursor < c.top {
		c.top = cursor
	}
	if cursor >= c.top+visible {
		c.top = cursor - visible + 1
	}
	c.top = max(0, min(c.top, max(0, len(rows)-visible)))

	var out []string
	for i := c.top; i < min(len(rows), c.top+visible); i++ {
		row := rows[i]
		if row.caption != "" {
			out = append(out, muted(r, row.caption))
			continue
		}
		selected := row.index == c.index
		file := row.conflict.File
		suffix := ""
		if len(c.regions) > 0 && c.detailFor == file.Path {
			suffix = plural(len(c.regions), "region")
		} else {
			suffix = "..."
		}
		out = append(out, r.RenderRow(tideui.Row{
			Prefix: "! ", Text: safeText(file.Path), Suffix: suffix, Selected: selected,
		}, inner))
	}
	return inset(strings.Join(out, "\n"), width)
}

// cursor is the flattened row index of the selected conflict.
func (c *conflictState) cursor() int {
	for i, row := range c.rows() {
		if row.caption == "" && row.index == c.index {
			return i
		}
	}
	return 0
}

// conflictRegionPane lists the file's conflict regions with their side sizes.
func (m *Model) conflictRegionPane(r tideui.Renderer, width, height int) string {
	c := m.conflicts
	inner := max(1, width-2)
	if c.detailLoading {
		return inset("\n"+muted(r, m.activity()+" Reading file…"), width)
	}
	if _, ok := c.current(); !ok {
		return inset("\n\n"+accent(r, "Nothing selected")+"\n\n"+muted(r, "Choose a conflict to inspect it."), width)
	}
	if c.workingBinary {
		return inset("\n\n"+accent(r, "Binary file")+"\n\n"+
			muted(r, "No conflict markers to navigate.\nChoose a side with o or t,\nor resolve it externally with e."), width)
	}
	if len(c.regions) == 0 {
		return inset("\n\n"+accent(r, "No conflict regions")+"\n\n"+
			muted(r, "Git reports this path as unmerged, but it\ncontains no conflict markers.\nMark it resolved with m when satisfied."), width)
	}
	visible := max(1, height-2)
	if c.regionIndex < c.regionTop {
		c.regionTop = c.regionIndex
	}
	if c.regionIndex >= c.regionTop+visible {
		c.regionTop = c.regionIndex - visible + 1
	}
	c.regionTop = max(0, min(c.regionTop, max(0, len(c.regions)-visible)))

	var out []string
	for i := c.regionTop; i < min(len(c.regions), c.regionTop+visible); i++ {
		region := c.regions[i]
		label := fmt.Sprintf("#%d  %s · %s", i+1,
			plural(len(region.OursLines), "ours line"), plural(len(region.TheirsLines), "theirs line"))
		out = append(out, r.RenderRow(tideui.Row{
			Text: label, Suffix: fmt.Sprintf("L%d", region.StartLine), Selected: i == c.regionIndex,
		}, inner))
	}
	return inset(strings.Join(out, "\n"), width)
}

// conflictInspectorPane is the heart of the screen: it shows one conflict
// region, the whole working file, or a stage comparison, always with text
// labels so colour is never the only signal.
func (m *Model) conflictInspectorPane(r tideui.Renderer, width, height int) string {
	c := m.conflicts
	if _, ok := c.current(); !ok {
		return inset("\n\n"+accent(r, "Nothing selected")+"\n\n"+muted(r, "Choose a conflict to inspect it."), width)
	}
	if c.detailLoading {
		return inset("\n"+muted(r, m.activity()+" Reading conflict…"), width)
	}
	label := " " + accent(r, c.mode.label(m.repoState.Operation)) + muted(r, "   v switches view") + "\n\n"
	switch c.mode {
	case inspectOursBase:
		return label + m.renderDiffView(&c.oursView, r, width, max(1, height-3), m.focus == 2)
	case inspectTheirsBase:
		return label + m.renderDiffView(&c.theirsView, r, width, max(1, height-3), m.focus == 2)
	case inspectWorking:
		lines := m.workingConflictLines()
		c.inspect.ClampTo(len(lines), max(1, height-3))
		return label + renderConflictLines(lines, r, c.inspect.Offset(), 0, max(1, height-3), width)
	default:
		return label + m.regionInspector(r, width, height)
	}
}

// regionInspector stacks ours, theirs and base with explicit labels.
func (m *Model) regionInspector(r tideui.Renderer, width, height int) string {
	c := m.conflicts
	region, ok := c.currentRegion()
	if !ok {
		return inset("\n\n"+accent(r, "No region")+"\n\n"+muted(r, "This file has no conflict markers."), width)
	}
	inner := max(1, width-3)
	var rows []string
	section := func(title, side string, style func(string, tideui.Renderer) string, lines []string, present bool) {
		badge := accent(r, title)
		if side != "" {
			badge += muted(r, "  "+side)
		}
		rows = append(rows, " "+badge)
		if !present {
			rows = append(rows, " "+muted(r, "(empty)"))
			return
		}
		for _, line := range lines {
			rows = append(rows, " "+style(line, r))
		}
	}
	section(c.oursTitle, region.OursLabel, styleOurs, region.OursLines, region.HasOurs)
	rows = append(rows, "", " "+muted(r, strings.Repeat("─", max(1, inner))), "")
	section(c.theirsTitle, region.TheirsLabel, styleTheirs, region.TheirsLines, region.HasTheirs)
	if region.HasBase {
		rows = append(rows, "", " "+muted(r, strings.Repeat("─", max(1, inner))), "")
		section("BASE", region.BaseLabel, styleBase, region.BaseLines, true)
	}
	c.inspect.ClampTo(len(rows), max(1, height-3))
	start := min(c.inspect.Offset(), max(0, len(rows)-max(1, height-3)))
	end := min(len(rows), start+max(1, height-3))
	return strings.Join(rows[start:end], "\n")
}

// workingConflictLines annotates the whole working file: region sides get the
// same semantic colour as the region inspector and markers are accented.
func (m *Model) workingConflictLines() []conflictLine {
	c := m.conflicts
	kind := make([]byte, len(c.working))
	for i := range kind {
		kind[i] = ' '
	}
	mark := func(line int, k byte) {
		if line >= 1 && line <= len(kind) {
			kind[line-1] = k
		}
	}
	fill := func(from, to int, k byte) {
		for l := from; l <= to; l++ {
			mark(l, k)
		}
	}
	for _, region := range c.regions {
		mark(region.StartLine, 'm')
		fill(region.StartLine+1, region.OursEnd, 'o')
		separator := region.OursEnd + 1
		if region.HasBase {
			mark(region.OursEnd+1, 'm') // the ||||||| marker
			fill(region.BaseStart, region.BaseEnd, 'b')
			separator = region.BaseEnd + 1
		}
		mark(separator, 'm')
		fill(region.TheirsStart, region.TheirsEnd, 't')
		mark(region.EndLine, 'm')
	}
	out := make([]conflictLine, 0, len(c.working))
	for i, line := range c.working {
		out = append(out, conflictLine{line: line, kind: kind[i]})
	}
	return out
}

type conflictLine struct {
	line string
	kind byte // ' ' context, 'o' ours, 't' theirs, 'b' base, 'm' marker
}

func renderConflictLines(lines []conflictLine, r tideui.Renderer, offset, horizontal, height, width int) string {
	offset = min(offset, max(0, len(lines)-height))
	out := make([]string, 0, height)
	for _, cl := range lines[offset:min(len(lines), offset+height)] {
		text := safeText(cl.line)
		switch cl.kind {
		case 'o':
			out = append(out, styleOurs(text, r))
		case 't':
			out = append(out, styleTheirs(text, r))
		case 'b':
			out = append(out, styleBase(text, r))
		case 'm':
			out = append(out, styleMarker(cl.line, r))
		default:
			out = append(out, r.Styles.DetailBody.Render(text))
		}
	}
	return strings.Join(out, "\n")
}

// styleOurs, styleTheirs and styleBase are the one place conflict sides get
// their colour, always paired with a text label elsewhere.
func styleOurs(s string, r tideui.Renderer) string {
	return r.Styles.DetailBody.Foreground(r.Styles.Theme.Unread).Render(s)
}
func styleTheirs(s string, r tideui.Renderer) string {
	return r.Styles.DetailBody.Foreground(r.Styles.Theme.BorderFocus).Render(s)
}
func styleBase(s string, r tideui.Renderer) string {
	return r.Styles.DetailMeta.Italic(false).Render(s)
}
func styleMarker(s string, r tideui.Renderer) string {
	return r.Styles.DetailBody.Foreground(r.Styles.Theme.Error).Bold(true).Render(s)
}
