package ui

import (
	"fmt"
	"strings"

	"github.com/allisonhere/tidegit/internal/diff"
	"github.com/allisonhere/tidegit/internal/git"
	"github.com/allisonhere/tideui"
	"github.com/charmbracelet/lipgloss"
)

// diffView is the shared, screen-independent viewer state for one file's patch.
// Every diff pane in TideGit keeps one of these and renders it through the same
// code, so unified/split, gutters, syntax, word emphasis, search and hunk
// navigation behave identically in every context.
type diffView struct {
	patch     diff.Patch
	label     string
	hunk      int
	line      int // selected flattened line index
	collapsed map[int]bool
	search    string
	searching bool
	matchAt   int
	matches   []int
	scroll    tideui.PaneScroller
	across    int
}

// diffOptions is the resolved set of viewer preferences for one render.
type diffOptions struct {
	mode       string
	numbers    bool
	syntax     bool
	word       bool
	whitespace bool
	collapse   bool
	context    int
	large      bool
}

// largeDiffLines is the point past which syntax and word emphasis are disabled
// to keep scrolling responsive.
const largeDiffLines = 4000

func (v *diffView) reset(patch diff.Patch, label string) {
	v.patch = patch
	v.label = label
	v.hunk = 0
	v.line = 0
	v.collapsed = map[int]bool{}
	v.search = ""
	v.searching = false
	v.matches = nil
	v.matchAt = 0
	v.scroll.ScrollToTop()
	v.across = 0
}

func (v *diffView) file() *diff.File {
	if len(v.patch.Files) == 0 {
		return nil
	}
	return &v.patch.Files[0]
}

func (v *diffView) hunkCount() int {
	if f := v.file(); f != nil {
		return len(f.Hunks)
	}
	return 0
}

func (v *diffView) currentHunk() *diff.Hunk {
	f := v.file()
	if f == nil || v.hunk < 0 || v.hunk >= len(f.Hunks) {
		return nil
	}
	return &f.Hunks[v.hunk]
}

// hunkHeaderIndex is the flattened index of a hunk's header row.
func (v *diffView) hunkHeaderIndex(hunk int, opts diffOptions) int {
	for i, rl := range v.flat(opts, false) {
		if rl.hunkIndex == hunk && rl.kind == '@' {
			return i
		}
	}
	return 0
}

// moveHunk selects the next or previous hunk.
func (v *diffView) moveHunk(delta int, opts diffOptions) {
	n := v.hunkCount()
	if n == 0 {
		return
	}
	v.hunk = min(max(0, v.hunk+delta), n-1)
	v.line = v.hunkHeaderIndex(v.hunk, opts)
	v.scroll.ScrollToTop()
}

// renderLine is one flattened row of the unified viewer.
type renderLine struct {
	hunkIndex int
	line      *diff.Line
	kind      byte // ' ', '+', '-', '@', '~', '\\'
	text      string
	oldNum    int
	newNum    int
	gapStart  int
}

// flat builds the unified row list for the whole patch.
func (v *diffView) flat(opts diffOptions, applyCollapse bool) []renderLine {
	var out []renderLine
	f := v.file()
	if f == nil {
		return out
	}
	for hi, h := range f.Hunks {
		out = append(out, renderLine{hunkIndex: hi, kind: '@', text: h.Header, oldNum: h.OldStart, newNum: h.NewStart})
		var display []diff.DisplayLine
		if applyCollapse && opts.collapse {
			threshold := max(6, 2*opts.context+2)
			display = diff.CollapseContext(h.Lines, max(1, opts.context), threshold, v.collapsed)
		} else {
			for i := range h.Lines {
				display = append(display, diff.DisplayLine{Line: &h.Lines[i]})
			}
		}
		for _, d := range display {
			if d.Collapsed {
				out = append(out, renderLine{hunkIndex: hi, kind: '~', text: fmt.Sprintf("⋯ %d unchanged lines", d.Hidden), gapStart: d.GapStart})
				continue
			}
			l := d.Line
			kind := byte(' ')
			switch l.Type {
			case diff.Addition:
				kind = '+'
			case diff.Deletion:
				kind = '-'
			case diff.NoNewline:
				kind = '\\'
			}
			out = append(out, renderLine{hunkIndex: hi, line: l, kind: kind, oldNum: l.Old, newNum: l.New})
		}
	}
	return out
}

// gutterWidths returns the old/new number column widths for a patch.
func (v *diffView) gutterWidths() (int, int) {
	oldW, newW := 1, 1
	f := v.file()
	if f == nil {
		return 2, 2
	}
	for _, h := range f.Hunks {
		oldW = max(oldW, len(fmt.Sprint(h.OldStart+max(0, h.OldCount))))
		newW = max(newW, len(fmt.Sprint(h.NewStart+max(0, h.NewCount))))
	}
	return max(2, oldW), max(2, newW)
}

// ensureVisible scrolls so the selected line stays on screen.
func (v *diffView) ensureVisible(height int) {
	if height <= 0 {
		return
	}
	if v.line < v.scroll.Offset() {
		v.scroll.ScrollToTop()
		v.scroll.ScrollDown(v.line)
	}
	if v.line >= v.scroll.Offset()+height {
		v.scroll.ScrollToTop()
		v.scroll.ScrollDown(v.line - height + 1)
	}
}

// toggleGap expands or collapses the context marker under the selection.
func (v *diffView) toggleGap(opts diffOptions) bool {
	flat := v.flat(opts, true)
	if v.line < 0 || v.line >= len(flat) || flat[v.line].kind != '~' {
		return false
	}
	start := flat[v.line].gapStart
	v.collapsed[start] = !v.collapsed[start]
	v.hunk = flat[v.line].hunkIndex
	return true
}

// searchText finds every occurrence of the query and selects the first match.
func (v *diffView) searchText(opts diffOptions, query string) {
	v.search = query
	v.matches = nil
	v.matchAt = 0
	if strings.TrimSpace(query) == "" {
		return
	}
	needle := strings.ToLower(query)
	for i, rl := range v.flat(opts, true) {
		text := rl.text
		if rl.line != nil {
			text = rl.line.Text
		}
		if strings.Contains(strings.ToLower(text), needle) {
			v.matches = append(v.matches, i)
		}
	}
	if len(v.matches) > 0 {
		v.line = v.matches[0]
	}
}

func (v *diffView) stepMatch(delta int) {
	if len(v.matches) == 0 {
		return
	}
	v.matchAt = (v.matchAt + delta + len(v.matches)) % len(v.matches)
	v.line = v.matches[v.matchAt]
}

// diffOptionsFrom builds the viewer options from settings.
func (m *Model) diffOptionsFrom() diffOptions {
	opts := diffOptions{mode: "unified", numbers: true, syntax: true, word: true, collapse: true, context: 3}
	if m.cfg == nil {
		return opts
	}
	opts.mode = m.cfg.Diff.Mode
	if opts.mode != "split" {
		opts.mode = "unified"
	}
	opts.numbers = m.cfg.Diff.LineNumbers
	opts.syntax = m.cfg.Diff.Syntax
	opts.word = m.cfg.Diff.WordHighlight
	opts.whitespace = m.cfg.Diff.ShowWhitespace
	opts.collapse = m.cfg.Diff.Collapse
	opts.context = m.cfg.Diff.ContextLines
	return opts
}

// whitespaceMode reads the configured Git whitespace comparison.
func (m *Model) whitespaceMode() git.WhitespaceMode {
	if m.cfg == nil {
		return git.WhitespaceNormal
	}
	return git.ParseWhitespaceMode(m.cfg.Diff.Whitespace)
}

// renderDiffView renders one file's patch, unified or split, windowed to the
// viewport. Syntax and word emphasis are computed only for visible lines.
func (m *Model) renderDiffView(v *diffView, r tideui.Renderer, width, height int, focused bool) string {
	opts := m.diffOptionsFrom()
	opts.large = v.patch.TotalLines() > largeDiffLines
	f := v.file()
	if f == nil {
		return ""
	}
	if f.Binary {
		return m.binaryNotice(r, f, width)
	}
	if len(f.Hunks) == 0 {
		// A combined (--cc) conflict diff has no ordinary hunks; show it as a
		// raw comparison rather than pretending it is empty.
		if v.patch.Conflict {
			return m.renderRawConflict(v, r, width, height, opts)
		}
		return m.emptyDiffNotice(r, f, width)
	}
	if opts.mode == "split" && width >= 66 {
		return m.renderSplit(v, r, width, height, focused, opts)
	}
	return m.renderUnified(v, r, width, height, focused, opts)
}

// renderRawConflict renders a combined conflict patch line by line, keeping the
// multi-parent markers visible. The Conflicts screen offers the structured
// two-way comparison; this is the Status preview.
func (m *Model) renderRawConflict(v *diffView, r tideui.Renderer, width, height int, opts diffOptions) string {
	lines := diffLines(git.Diff{Patch: v.patch.Raw, Conflict: true}, opts.numbers)
	v.scroll.ClampTo(len(lines), max(1, height))
	return renderDiff(lines, r, v.scroll.Offset(), v.across, height, width, -1)
}

func (m *Model) renderUnified(v *diffView, r tideui.Renderer, width, height int, focused bool, opts diffOptions) string {
	flat := v.flat(opts, true)
	if len(flat) == 0 {
		return ""
	}
	v.line = min(max(0, v.line), len(flat)-1)
	v.ensureVisible(height)
	start := min(v.scroll.Offset(), max(0, len(flat)-height))
	end := min(len(flat), start+height)
	oldW, newW := v.gutterWidths()
	lang := ""
	if opts.syntax && !opts.large {
		if f := v.file(); f != nil {
			lang = diff.LanguageForPath(f.Display())
		}
	}
	var out []string
	for i := start; i < end; i++ {
		rl := flat[i]
		selected := focused && i == v.line
		out = append(out, m.renderUnifiedRow(r, rl, selected, opts, lang, oldW, newW, width))
	}
	if opts.large {
		out = append(out, muted(r, "  Large diff · syntax and word highlighting are off"))
	}
	return strings.Join(out, "\n")
}

func (m *Model) renderUnifiedRow(r tideui.Renderer, rl renderLine, selected bool, opts diffOptions, lang string, oldW, newW, width int) string {
	switch rl.kind {
	case '@':
		lead := " "
		if selected {
			lead = ">"
		}
		body := clip(rl.text, max(1, width-2))
		if selected {
			return tideui.StyleOver(r.Styles.ItemSelected.Width(width), lead+" "+body)
		}
		return lead + " " + diffHunkStyle(r).Render(body)
	case '~':
		marker := "  " + rl.text
		if selected {
			marker = "> " + rl.text
			return tideui.StyleOver(r.Styles.ItemSelected.Width(width), muted(r, marker))
		}
		return muted(r, marker)
	}
	if rl.kind == '\\' {
		return m.renderMetaLine(r, " "+muted(r, clip(rl.text, max(1, width-3))), selected, width)
	}
	gutter := gutterText(rl, oldW, newW, opts.numbers)
	body := m.renderBodyLine(r, rl.line, opts, lang)
	lead := " "
	if selected {
		lead = ">"
	}
	prefix := lead + gutter + " " + string(rl.kind) + " "
	if selected {
		return tideui.StyleOver(r.Styles.ItemSelected.Width(width), prefix+body)
	}
	return prefix + body
}

// gutterText formats the old/new line-number columns.
func gutterText(rl renderLine, oldW, newW int, show bool) string {
	if !show {
		return strings.Repeat(" ", oldW+newW+1)
	}
	oldStr := strings.Repeat(" ", oldW)
	newStr := strings.Repeat(" ", newW)
	if rl.oldNum > 0 {
		oldStr = fmt.Sprintf("%*d", oldW, rl.oldNum)
	}
	if rl.newNum > 0 {
		newStr = fmt.Sprintf("%*d", newW, rl.newNum)
	}
	return oldStr + " " + newStr
}

// renderMetaLine renders a meta row with an optional selection background.
func (m *Model) renderMetaLine(r tideui.Renderer, text string, selected bool, width int) string {
	if selected {
		return tideui.StyleOver(r.Styles.ItemSelected.Width(width), text)
	}
	return text
}

// renderBodyLine applies syntax, word emphasis and whitespace markers.
func (m *Model) renderBodyLine(r tideui.Renderer, line *diff.Line, opts diffOptions, lang string) string {
	if line == nil {
		return ""
	}
	text := safeText(line.Text)
	runes := []rune(text)
	classes := make([]diff.TokenClass, len(runes))
	if opts.syntax && !opts.large && lang != "" {
		for _, tok := range diff.Highlight(lang, text) {
			for i := tok.Start; i < tok.End && i < len(runes); i++ {
				classes[i] = tok.Class
			}
		}
	}
	changed := make([]bool, len(runes))
	if opts.word && !opts.large {
		for _, span := range line.Spans {
			for i := span.Start; i < span.End && i < len(runes); i++ {
				changed[i] = true
			}
		}
	}
	lastNonSpace := -1
	for i, rr := range runes {
		if rr != ' ' && rr != '\t' {
			lastNonSpace = i
		}
	}
	var b strings.Builder
	i := 0
	for i < len(runes) {
		cls := classes[i]
		ch := changed[i]
		ws := wsKind(runes, i, lastNonSpace, opts.whitespace)
		j := i
		for j < len(runes) && classes[j] == cls && changed[j] == ch && wsKind(runes, j, lastNonSpace, opts.whitespace) == ws {
			j++
		}
		segment := string(runes[i:j])
		if opts.whitespace {
			segment = wsGlyphs(runes[i:j])
		}
		b.WriteString(styleSegment(r, line.Type, cls, ch, ws, segment))
		i = j
	}
	return b.String()
}

func wsKind(runes []rune, i, lastNonSpace int, enabled bool) byte {
	if !enabled || i >= len(runes) {
		return 0
	}
	switch runes[i] {
	case '\t':
		return 't'
	case ' ':
		if i > lastNonSpace {
			return 'T'
		}
		return ' '
	}
	return 0
}

func wsGlyphs(runes []rune) string {
	var b strings.Builder
	for _, r := range runes {
		switch r {
		case ' ':
			b.WriteRune('·')
		case '\t':
			b.WriteRune('→')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// styleSegment composes the semantic diff style with syntax and word emphasis.
func styleSegment(r tideui.Renderer, lineType diff.LineType, cls diff.TokenClass, changed bool, ws byte, text string) string {
	style := diffBodyStyle(r, lineType)
	if cls != diff.TokPlain {
		style = syntaxStyle(r, cls, style)
	}
	if changed {
		style = style.Bold(true).Underline(true)
	}
	switch ws {
	case 'T':
		style = style.Background(r.Styles.Theme.Error).Foreground(r.Styles.Theme.Bg).Bold(true)
	case 't', ' ':
		style = style.Foreground(r.Styles.Theme.Dimmed)
	}
	return style.Render(text)
}

func diffBodyStyle(r tideui.Renderer, lineType diff.LineType) lipgloss.Style {
	switch lineType {
	case diff.Addition:
		return r.Styles.DetailBody.Foreground(r.Styles.Theme.Unread)
	case diff.Deletion:
		return r.Styles.DetailBody.Foreground(r.Styles.Theme.Error)
	case diff.NoNewline:
		return r.Styles.DetailMeta.Italic(false)
	default:
		return r.Styles.DetailBody
	}
}

func syntaxStyle(r tideui.Renderer, cls diff.TokenClass, base lipgloss.Style) lipgloss.Style {
	switch cls {
	case diff.TokKeyword:
		return base.Foreground(r.Styles.Theme.BorderFocus).Bold(true)
	case diff.TokString:
		return base.Foreground(r.Styles.Theme.Unread)
	case diff.TokComment:
		return base.Foreground(r.Styles.Theme.Dimmed).Italic(true)
	case diff.TokNumber:
		return base.Foreground(r.Styles.Theme.Error)
	default:
		return base
	}
}

func diffHunkStyle(r tideui.Renderer) lipgloss.Style {
	return r.Styles.DetailBody.Foreground(r.Styles.Theme.BorderFocus).Bold(true)
}

func (m *Model) renderSplit(v *diffView, r tideui.Renderer, width, height int, focused bool, opts diffOptions) string {
	f := v.file()
	half := (width - 3) / 2
	if half < 24 {
		return m.renderUnified(v, r, width, height, focused, opts)
	}
	lang := ""
	if opts.syntax && !opts.large {
		lang = diff.LanguageForPath(f.Display())
	}
	oldW, newW := v.gutterWidths()
	type rowRef struct{ hunk, row int }
	var refs []rowRef
	for hi, h := range f.Hunks {
		for ri := range h.SideRows() {
			refs = append(refs, rowRef{hi, ri})
		}
	}
	if len(refs) == 0 {
		return ""
	}
	v.ensureVisible(height)
	start := min(v.scroll.Offset(), max(0, len(refs)-height))
	end := min(len(refs), start+height)
	var out []string
	for i := start; i < end; i++ {
		ref := refs[i]
		if i == start || refs[i-1].hunk != ref.hunk {
			out = append(out, " "+diffHunkStyle(r).Render(clip(f.Hunks[ref.hunk].Header, max(1, width-2))))
		}
		rows := f.Hunks[ref.hunk].SideRows()
		selected := focused && i == v.line
		out = append(out, m.renderSplitRow(r, rows[ref.row], selected, opts, lang, oldW, newW, half, width))
	}
	if opts.large {
		out = append(out, muted(r, "  Large diff · syntax and word highlighting are off"))
	}
	return strings.Join(out, "\n")
}

func (m *Model) renderSplitRow(r tideui.Renderer, row diff.SideRow, selected bool, opts diffOptions, lang string, oldW, newW, half, width int) string {
	left := m.renderSplitCell(r, row.Left, oldW, false, opts, lang, half)
	right := m.renderSplitCell(r, row.Right, newW, true, opts, lang, half)
	sep := muted(r, " │ ")
	line := left + sep + right
	if selected {
		line = tideui.StyleOver(r.Styles.ItemSelected.Width(width), line)
	}
	return line
}

func (m *Model) renderSplitCell(r tideui.Renderer, line *diff.Line, numW int, isNew bool, opts diffOptions, lang string, half int) string {
	gutter := strings.Repeat(" ", numW)
	marker := " "
	if line != nil {
		if opts.numbers {
			if line.New > 0 && isNew {
				gutter = fmt.Sprintf("%*d", numW, line.New)
			} else if line.Old > 0 && !isNew {
				gutter = fmt.Sprintf("%*d", numW, line.Old)
			}
		}
		switch line.Type {
		case diff.Addition:
			marker = "+"
		case diff.Deletion:
			marker = "-"
		case diff.NoNewline:
			marker = "\\"
		}
	}
	body := ""
	if line != nil {
		body = m.renderBodyLine(r, line, opts, lang)
	}
	cell := muted(r, gutter) + " " + marker + " " + body
	return padCell(r, cell, half)
}

// padCell clips a cell's visible text to its width and pads with spaces that
// carry the pane background.
func padCell(r tideui.Renderer, cell string, width int) string {
	cell = clip(cell, width)
	if pad := width - lipgloss.Width(cell); pad > 0 {
		cell += r.Styles.DetailBody.Render(strings.Repeat(" ", pad))
	}
	return cell
}

// binaryNotice renders the binary-file state instead of raw data.
func (m *Model) binaryNotice(r tideui.Renderer, f *diff.File, width int) string {
	lines := []string{
		"",
		" " + accent(r, "Binary file"),
		"",
		" " + r.Styles.DetailBody.Render(clip(f.Display(), max(1, width-3))),
		" " + muted(r, diff.DescribeStatus(f.Status)+" · no textual diff to display"),
	}
	return strings.Join(lines, "\n")
}

// emptyDiffNotice explains an empty patch for the context.
func (m *Model) emptyDiffNotice(r tideui.Renderer, f *diff.File, width int) string {
	title := "No textual changes"
	body := "This change contains no hunks to show."
	switch f.Status {
	case 'A':
		title, body = "New file", "The file is added; its contents are the addition."
	case 'D':
		title, body = "File deleted", "The file is removed; its contents are the deletion."
	case 'R':
		title, body = "Renamed", "The content is unchanged; only the path moved."
	}
	return strings.Join([]string{"", " " + accent(r, title), "", " " + muted(r, body)}, "\n")
}
