package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/allisonhere/tidegit/internal/git"
	"github.com/allisonhere/tideui"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Commit rows drop metadata as the pane narrows, in reverse priority: the
// author goes first, then the age, then the hash, then refs. The graph and the
// subject are never sacrificed.
const (
	widthForRefs   = 40
	widthForHash   = 48
	widthForAge    = 54
	widthForAuthor = 60
)

// commitColumns is decided once per render so the hash and the right-hand
// gutter line up down the whole pane instead of moving from row to row.
type commitColumns struct {
	hash, age, author, refs bool
	gutter                  int // fixed cells reserved on the right
}

func columnsFor(width int) commitColumns {
	c := commitColumns{
		hash:   width >= widthForHash,
		age:    width >= widthForAge,
		author: width >= widthForAuthor,
		refs:   width >= widthForRefs,
	}
	if c.age {
		c.gutter += 4
	}
	if c.author {
		c.gutter += 3
	}
	return c
}

func (m *Model) historyView(r tideui.Renderer) string {
	h := m.history
	mode, w := m.paneLayout()
	if mode == tideui.ThreeColumn {
		w = [3]int{m.width*2/10 - 2, m.width*5/10 - 2, m.width - m.width*2/10 - m.width*5/10 - 2}
	}
	height := m.historyPaneHeight()
	// Stacked panes share the height, so each gets roughly half of it.
	// Only the stacked layout splits the height between the two right panes;
	// side-by-side columns each get the full height.
	paneHeight, inspectorHeight := height, height
	if mode == tideui.StackedRight {
		paneHeight = max(1, height*2/5)
		inspectorHeight = max(1, height-paneHeight)
	}

	refs := m.historyFilterPane(r, w[0], height)
	commits := m.historyCommitPane(r, w[1], paneHeight)
	inspector := m.historyInspectorPane(r, w[2], inspectorHeight)

	title, hint := "COMMIT", ""
	if c, ok := h.current(); ok {
		hint = c.Short
		if h.showDiff {
			title = "COMMIT DIFF"
		}
	}
	position := ""
	if len(h.commits) > 0 {
		position = fmt.Sprintf("%d/%d", h.selected+1, len(h.commits))
		if !h.exhausted {
			position += "+"
		}
	}
	layout := tideui.Layout{Width: m.width, Height: m.height - 3, Mode: tideui.ThreeColumn,
		ColumnRatios: [3]float64{2, 5, 3}, SidebarRatio: 0.28, Panes: [3]tideui.Pane{
			{Title: "REFS", Hint: h.filter().Label, Content: refs, Focused: m.focus == 0},
			{Title: "HISTORY", Hint: position, Content: commits, Focused: m.focus == 1},
			{Title: title, Hint: hint, Content: inspector, Focused: m.focus == 2},
		}, Status: &tideui.StatusBar{Left: clip(safeText(m.historyStatus(r)), m.width-3)}}
	layout.Mode = mode
	layout.UpperRightRatio = 0.40
	base := m.globalHeader(r, "HISTORY") + "\n" + r.Render(layout) + "\n" + m.historyHints(r)
	return m.withOverlays(r, base)
}

func (m *Model) historyStatus(r tideui.Renderer) string {
	h := m.history
	switch {
	case h.err != "":
		return "History could not be read · " + firstLine(h.err)
	case h.loading:
		return m.activity() + " Reading history"
	case h.loadingMore:
		return m.activity() + " Loading older commits"
	case h.detailLoading || h.diffLoading:
		return m.activity() + " Inspecting commit"
	case m.notice != "":
		return m.notice
	case h.searching || h.search != "":
		return fmt.Sprintf("Filtering subjects by %q · %d shown", h.search, len(h.commits))
	}
	if c, ok := h.current(); ok {
		age := relativeTime(c.AuthorTime, time.Now())
		return fmt.Sprintf("%s · %s · %s ago · %s", c.Short, safeText(c.Author), age,
			c.AuthorTime.Format("2006-01-02 15:04"))
	}
	return m.branchLabel() + " · no commits to show"
}

func (m *Model) historyHints(r tideui.Renderer) string {
	h := m.history
	if h.searching {
		return m.hintBar(r, tideui.SoftHint{Key: "/", Label: safeText(h.search)},
			tideui.SoftHint{Key: "Enter", Label: "keep"}, tideui.SoftHint{Key: "Esc", Label: "clear"})
	}
	hints := []tideui.SoftHint{{Key: "Enter", Label: "inspect"}, {Key: "/", Label: "search"},
		{Key: "n", Label: "branch here"}, {Key: "y", Label: "copy hash"}, {Key: "?", Label: "help"}}
	if h.showDiff && m.focus == 2 {
		hints = []tideui.SoftHint{{Key: "Esc", Label: "back to commit"}, {Key: "j / k", Label: "scroll"},
			{Key: "h / l", Label: "across"}, {Key: "?", Label: "help"}}
	}
	return m.hintBar(r, hints...)
}

// historyFilterPane lists the refs history can be walked from.
func (m *Model) historyFilterPane(r tideui.Renderer, width, height int) string {
	h := m.history
	inner := max(1, width-2)
	rows := []string{"", accent(r, "WALK FROM"), ""}
	if len(h.filters) == 0 {
		rows = append(rows, muted(r, "Reading refs…"))
	}
	// Keep the selected filter on screen without moving it more than necessary.
	visible := max(1, height-6)
	start := 0
	if h.filterIndex >= visible {
		start = h.filterIndex - visible + 1
	}
	for i := start; i < min(len(h.filters), start+visible); i++ {
		f := h.filters[i]
		if f.Header {
			rows = append(rows, "", muted(r, f.Label))
			continue
		}
		suffix := ""
		if f.Head {
			suffix = "@"
		}
		rows = append(rows, r.RenderRow(tideui.Row{Text: safeText(f.Label), Suffix: suffix,
			Selected: i == h.filterIndex}, inner))
	}
	return inset(strings.Join(rows, "\n"), width)
}

// historyCommitPane draws the graph and commit rows.
func (m *Model) historyCommitPane(r tideui.Renderer, width, height int) string {
	h := m.history
	inner := max(1, width-2)
	if h.err != "" {
		return inset("\n"+accent(r, "HISTORY UNAVAILABLE")+"\n\n"+
			r.Styles.DetailBody.Foreground(r.Styles.Theme.Error).Render(safeText(firstLine(h.err)))+
			"\n\n"+muted(r, "Press r to retry."), width)
	}
	if len(h.commits) == 0 {
		if h.loading {
			return inset("\n"+muted(r, m.activity()+" Reading history…"), width)
		}
		title, body := "No commits yet", "This branch has no history.\nMake your first commit on the Status screen."
		if h.search != "" {
			title, body = "No matching commits", "Nothing in this history mentions\n"+safeText(h.search)+"."
		}
		return inset("\n\n"+accent(r, title)+"\n\n"+muted(r, body), width)
	}

	visible := max(1, height-2)
	// Scroll only when the cursor would leave the window, so the graph stays
	// visually still while the selection moves inside it.
	if h.selected < h.top {
		h.top = h.selected
	}
	if h.selected >= h.top+visible {
		h.top = h.selected - visible + 1
	}
	h.top = max(0, min(h.top, max(0, len(h.commits)-visible)))

	lanes := 1
	for i := h.top; i < min(len(h.commits), h.top+visible); i++ {
		if i < len(h.graph) {
			lanes = max(lanes, h.graph[i].Width())
		}
	}
	gw := graphWidth(lanes)
	now := time.Now()
	cols := columnsFor(inner)

	var rows []string
	for i := h.top; i < min(len(h.commits), h.top+visible); i++ {
		rows = append(rows, m.commitRow(r, i, inner, gw, now, cols))
	}
	// The end marker belongs after the oldest commit, so it is only drawn when
	// that commit is actually on screen.
	atEnd := h.top+visible >= len(h.commits)
	if h.loadingMore {
		rows = append(rows, muted(r, " "+m.activity()+" older commits…"))
	} else if h.exhausted && atEnd && len(h.commits) > 1 {
		rows = append(rows, muted(r, " ─ beginning of history ─"))
	}
	return strings.Join(rows, "\n")
}

// commitRow renders one commit: graph, hash, subject, refs, author and age.
func (m *Model) commitRow(r tideui.Renderer, index, width, graphW int, now time.Time, cols commitColumns) string {
	h := m.history
	c := h.commits[index]
	selected := index == h.selected && !h.searching
	isHead := false
	for _, ref := range c.Decorations {
		if ref.Head {
			isHead = true
		}
	}

	base := r.Styles.Item
	if selected {
		base = r.Styles.ItemSelected
	}
	// Item styles carry padding; the row is assembled from bare cells so the
	// graph column lines up exactly between rows.
	base = base.UnsetPadding().UnsetWidth()

	var row git.GraphRow
	if index < len(h.graph) {
		row = h.graph[index]
	}
	graph := renderGraphRow(r, row, isHead, selected, graphW, base)

	meta := base.Foreground(r.Styles.Theme.Dimmed)
	if selected {
		meta = base
	}
	// The gutter and hash columns are the same on every row; only the refs and
	// the subject share what is left, and the subject keeps at least a third.
	right := ""
	if cols.author {
		right += meta.Render(fmt.Sprintf("%-3s", authorInitials(c.Author)))
	}
	if cols.age {
		right += meta.Render(fmt.Sprintf("%4s", relativeTime(c.AuthorTime, now)))
	}
	hash := ""
	if cols.hash {
		hash = meta.Render(c.Short + " ")
	}
	avail := width - graphW - lipgloss.Width(hash) - cols.gutter - 1
	badges := ""
	if cols.refs && len(c.Decorations) > 0 {
		subjectFloor := max(10, avail/3)
		badges = refBadges(r, c.Decorations, min(avail-subjectFloor, avail/2))
		if badges != "" {
			badges += " "
			avail -= lipgloss.Width(badges)
		}
	}
	subject := base.Render(clip(safeText(c.Subject), max(1, avail)))

	left := graph + hash + badges + subject
	gap := max(0, width-lipgloss.Width(left)-lipgloss.Width(right))
	return left + base.Render(strings.Repeat(" ", gap)) + right
}

// historyInspectorPane shows the selected commit, or one file's patch from it.
func (m *Model) historyInspectorPane(r tideui.Renderer, width, height int) string {
	h := m.history
	if h.showDiff {
		return m.commitDiffPane(r, width, height)
	}
	c, ok := h.current()
	if !ok {
		return inset("\n\n"+accent(r, "Nothing selected")+"\n\n"+
			muted(r, "Choose a commit to inspect it."), width)
	}
	if !h.detailOK {
		return inset("\n"+accent(r, safeText(clip(c.Subject, width-4)))+"\n\n"+
			muted(r, m.activity()+" Reading "+c.Short+"…"), width)
	}
	d := h.detail.commit
	inner := max(1, width-2)
	var b strings.Builder

	b.WriteString("\n")
	for _, line := range wrapText(safeText(d.Subject), inner) {
		b.WriteString(r.Styles.DetailBody.Bold(true).Render(line) + "\n")
	}
	if badges := refBadges(r, d.Decorations, inner); badges != "" {
		b.WriteString("\n" + badges + "\n")
	}
	b.WriteString("\n")

	// Values wrap under their label rather than being clipped, so a full hash
	// or a long address stays readable.
	const labelWidth = 10
	field := func(label, value string) {
		if value == "" {
			return
		}
		lines := wrapText(value, max(8, inner-labelWidth))
		b.WriteString(muted(r, fmt.Sprintf("%-*s", labelWidth, label)) +
			r.Styles.DetailBody.Render(lines[0]) + "\n")
		for _, line := range lines[1:] {
			b.WriteString(strings.Repeat(" ", labelWidth) + r.Styles.DetailBody.Render(line) + "\n")
		}
	}
	field("commit", d.OID)
	field("author", fmt.Sprintf("%s <%s>", safeText(d.Author), safeText(d.AuthorEmail)))
	field("authored", d.AuthorTime.Format("2006-01-02 15:04:05 -0700"))
	// The committer is only worth a line when it differs from the author.
	if d.Committer != d.Author || d.CommitEmail != d.AuthorEmail {
		field("committer", fmt.Sprintf("%s <%s>", safeText(d.Committer), safeText(d.CommitEmail)))
	}
	if !d.CommitTime.Equal(d.AuthorTime) {
		field("committed", d.CommitTime.Format("2006-01-02 15:04:05 -0700"))
	}
	switch len(d.Parents) {
	case 0:
		field("parent", "none · first commit")
	case 1:
		field("parent", d.Parents[0][:min(12, len(d.Parents[0]))])
	default:
		var shorts []string
		for _, p := range d.Parents {
			shorts = append(shorts, p[:min(8, len(p))])
		}
		field("parents", strings.Join(shorts, "  ")+"  (merge)")
	}

	if body := strings.TrimSpace(messageBody(d)); body != "" {
		b.WriteString("\n" + muted(r, "MESSAGE") + "\n")
		for _, line := range strings.Split(body, "\n") {
			for _, wrapped := range wrapText(safeText(line), inner) {
				b.WriteString(r.Styles.DetailBody.Render(wrapped) + "\n")
			}
		}
	}

	b.WriteString("\n" + m.changedFiles(r, inner, height) + "\n")

	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	h.inspect.ClampTo(len(lines), max(1, height-1))
	start := min(h.inspect.Offset(), max(0, len(lines)-max(1, height-1)))
	end := min(len(lines), start+max(1, height-1))
	return inset(strings.Join(lines[start:end], "\n"), width)
}

// changedFiles renders the commit's file list with its own change summary.
func (m *Model) changedFiles(r tideui.Renderer, inner, height int) string {
	h := m.history
	files := h.detail.files
	adds, dels, binary := 0, 0, 0
	for _, f := range files {
		if f.Binary {
			binary++
			continue
		}
		adds += f.Additions
		dels += f.Deletions
	}
	head := muted(r, "CHANGED FILES") + "  " +
		r.Styles.DetailBody.Bold(true).Render(plural(len(files), "file")) + "  " +
		r.Styles.DetailBody.Foreground(r.Styles.Theme.Unread).Render(fmt.Sprintf("+%d", adds)) + " " +
		r.Styles.DetailBody.Foreground(r.Styles.Theme.Error).Render(fmt.Sprintf("−%d", dels))
	if binary > 0 {
		head += muted(r, fmt.Sprintf("  %s", plural(binary, "binary file")))
	}
	if len(files) == 0 {
		return head + "\n" + muted(r, "This commit changed no files against its first parent.")
	}

	// Keep the cursor inside a window rather than listing every path.
	visible := max(3, min(len(files), height/2))
	if h.fileIndex < h.fileTop {
		h.fileTop = h.fileIndex
	}
	if h.fileIndex >= h.fileTop+visible {
		h.fileTop = h.fileIndex - visible + 1
	}
	h.fileTop = max(0, min(h.fileTop, max(0, len(files)-visible)))

	rows := []string{head}
	for i := h.fileTop; i < min(len(files), h.fileTop+visible); i++ {
		f := files[i]
		text := safeText(f.Path)
		if f.OriginalPath != "" {
			text = safeText(f.OriginalPath) + " → " + text
		}
		rows = append(rows, r.RenderRow(tideui.Row{
			Prefix:   string(changeMark(f.Status)) + " ",
			Text:     text,
			Suffix:   fileCounts(f),
			Selected: i == h.fileIndex && m.focus == 2,
		}, inner))
	}
	if h.fileTop+visible < len(files) {
		rows = append(rows, muted(r, fmt.Sprintf(" %d more…", len(files)-h.fileTop-visible)))
	}
	return strings.Join(rows, "\n")
}

func fileCounts(f git.FileChange) string {
	if f.Binary {
		return "bin"
	}
	return fmt.Sprintf("+%d −%d", f.Additions, f.Deletions)
}

// changeMark is the one-character status shown beside a changed path.
func changeMark(status byte) byte {
	switch status {
	case 'A', 'M', 'D', 'R', 'C', 'T':
		return status
	default:
		return 'M'
	}
}

// commitDiffPane shows one file's patch from the selected commit, using the
// same renderer as the working-tree viewer and saying plainly what it is.
func (m *Model) commitDiffPane(r tideui.Renderer, width, height int) string {
	h := m.history
	header := " " + accent(r, clip(h.diffLabel, max(1, width-3))) + "\n " +
		muted(r, "historical patch · staging applies to the working tree") + "\n\n"
	if h.diffLoading {
		return header + " " + muted(r, m.activity()+" Reading patch…")
	}
	if len(h.diffLines) == 0 {
		return header + " " + muted(r, "No textual changes to preview.")
	}
	h.diffScroll.ClampTo(len(h.diffLines), max(1, height-3))
	return header + renderDiff(h.diffLines, r, h.diffScroll.Offset(), h.diffAcross, max(1, height-3), width, -1)
}

// messageBody returns the commit message without its subject line.
func messageBody(c git.Commit) string {
	body := c.Body
	if body == "" {
		return ""
	}
	if _, rest, found := strings.Cut(body, "\n"); found {
		return rest
	}
	return ""
}

// wrapText breaks text to fit width, preferring spaces but splitting a token
// that has none — a full commit hash has to wrap rather than be truncated.
func wrapText(text string, width int) []string {
	if width <= 0 {
		return []string{""}
	}
	lines := strings.Split(ansi.Wrap(text, width, ""), "\n")
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
