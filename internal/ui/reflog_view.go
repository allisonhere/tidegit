package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/allisonhere/tidegit/internal/git"
	"github.com/allisonhere/tideui"
	"github.com/charmbracelet/lipgloss"
)

func (m *Model) reflogView(r tideui.Renderer) string {
	rl := m.reflog
	mode, w := m.paneLayout()
	if mode == tideui.ThreeColumn {
		w = [3]int{m.width*4/10 - 2, m.width*3/10 - 2, m.width - m.width*4/10 - m.width*3/10 - 2}
	}
	height := m.historyPaneHeight()

	title, hint := "ENTRY", ""
	if entry, ok := rl.current(); ok {
		hint = entry.Short
	}
	if rl.showDiff {
		title = "COMMIT DIFF"
	}
	layout := tideui.Layout{Width: m.width, Height: m.height - 3, Mode: tideui.ThreeColumn,
		ColumnRatios: [3]float64{4, 3, 3}, SidebarRatio: 0.30, Panes: [3]tideui.Pane{
			{Title: "REFLOG", Hint: fmt.Sprint(len(rl.entries)), Content: m.reflogListPane(r, w[0], height), Focused: m.focus == 0},
			{Title: title, Hint: hint, Content: m.reflogInspectorPane(r, w[1], height), Focused: m.focus == 1},
			{Title: "DIFF", Hint: rl.diffLabel, Content: m.reflogDiffPane(r, w[2], height), Focused: m.focus == 2},
		}, Status: &tideui.StatusBar{Left: clip(safeText(m.reflogStatus()), m.width-3)}}
	layout.Mode = mode
	layout.UpperRightRatio = 0.40

	base := m.globalHeader(r, "REFLOG") + "\n" + r.Render(layout) + "\n" + m.reflogHints(r)
	return m.withOverlays(r, base)
}

func (m *Model) reflogStatus() string {
	rl := m.reflog
	switch {
	case rl.err != "":
		return "Reflog could not be read · " + firstLine(rl.err)
	case m.busy:
		return m.activity() + " " + m.operation
	case rl.loading:
		return m.activity() + " Reading the reflog"
	case rl.detailLoading:
		return m.activity() + " Inspecting entry"
	case m.notice != "":
		return m.notice
	}
	if entry, ok := rl.current(); ok {
		return fmt.Sprintf("%s · %s · %s ago", entry.Selector, safeText(entry.Action), m.timeAgo(entry.Time))
	}
	return "Recovery timeline"
}

func (m *Model) reflogHints(r tideui.Renderer) string {
	rl := m.reflog
	if rl.filtering {
		return m.hintBar(r, tideui.SoftHint{Key: "/", Label: safeText(rl.filter)},
			tideui.SoftHint{Key: "Enter", Label: "keep"}, tideui.SoftHint{Key: "Esc", Label: "clear"})
	}
	hints := []tideui.SoftHint{{Key: "b", Label: "recovery branch"}, {Key: "enter", Label: "inspect"},
		{Key: "R", Label: "reset here"}, {Key: "s", Label: "switch here"},
		{Key: "y", Label: "copy hash"}, {Key: "/", Label: "filter"}, {Key: "?", Label: "help"}}
	return m.hintBar(r, hints...)
}

// reflogListPane renders the recovery timeline: selector, short hash, action
// and age, with the action as the readable headline.
func (m *Model) reflogListPane(r tideui.Renderer, width, height int) string {
	rl := m.reflog
	inner := max(1, width-2)
	list := rl.visible()
	if len(list) == 0 {
		if rl.loading {
			return inset("\n"+muted(r, m.activity()+" Reading the reflog…"), width)
		}
		body := "No reflog entries yet."
		if rl.filter != "" {
			body = "No entry matches " + safeText(rl.filter) + "."
		}
		return inset("\n\n"+accent(r, "The reflog is empty")+"\n\n"+muted(r, body), width)
	}
	visible := max(1, height-2)
	if rl.index < rl.top {
		rl.top = rl.index
	}
	if rl.index >= rl.top+visible {
		rl.top = rl.index - visible + 1
	}
	rl.top = max(0, min(rl.top, max(0, len(list)-visible)))

	now := time.Now()
	var rows []string
	for i := rl.top; i < min(len(list), rl.top+visible); i++ {
		rows = append(rows, m.reflogRow(r, list[i], i == rl.index, inner, now))
	}
	return inset(strings.Join(rows, "\n"), width)
}

func (m *Model) reflogRow(r tideui.Renderer, entry git.ReflogEntry, selected bool, width int, now time.Time) string {
	base := r.Styles.Item
	if selected {
		base = r.Styles.ItemSelected
	}
	base = base.UnsetPadding().UnsetWidth()
	meta := base.Foreground(r.Styles.Theme.Dimmed)
	if selected {
		meta = base
	}
	selector := base.Foreground(r.Styles.Theme.BorderFocus).Bold(true).Render(fmt.Sprintf("%-10s", entry.Selector))
	hash := meta.Render(entry.Short + " ")
	age := meta.Render(fmt.Sprintf("%4s", m.timeAgo(entry.Time)))
	action := entry.Action
	if entry.Detail != "" {
		action += ": " + entry.Detail
	}
	subjectWidth := max(1, width-lipgloss.Width(selector)-lipgloss.Width(hash)-lipgloss.Width(age)-2)
	text := base.Render(clip(safeText(action), subjectWidth))
	left := " " + selector + hash + text
	gap := max(0, width-lipgloss.Width(left)-lipgloss.Width(age))
	return left + base.Render(strings.Repeat(" ", gap)) + age
}

// reflogInspectorPane shows the entry's commit and the action that created it.
func (m *Model) reflogInspectorPane(r tideui.Renderer, width, height int) string {
	rl := m.reflog
	entry, ok := rl.current()
	if !ok {
		return inset("\n\n"+accent(r, "Nothing selected")+"\n\n"+muted(r, "Choose a reflog entry."), width)
	}
	inner := max(1, width-2)
	var b strings.Builder
	b.WriteString("\n")
	for _, line := range wrapText(safeText(entry.Action), inner) {
		b.WriteString(r.Styles.DetailBody.Bold(true).Render(line) + "\n")
	}
	if entry.Detail != "" {
		for _, line := range wrapText(entry.Detail, inner) {
			b.WriteString(muted(r, line) + "\n")
		}
	}
	b.WriteString("\n")
	field := func(label, value string) {
		if value == "" {
			return
		}
		lines := wrapText(safeText(value), max(8, inner-9))
		b.WriteString(muted(r, fmt.Sprintf("%-9s", label)) + r.Styles.DetailBody.Render(lines[0]) + "\n")
		for _, line := range lines[1:] {
			b.WriteString(strings.Repeat(" ", 8) + r.Styles.DetailBody.Render(line) + "\n")
		}
	}
	field("selector", entry.Selector)
	field("commit", entry.OID)
	if !rl.detailOK {
		b.WriteString("\n" + muted(r, m.activity()+" Reading commit…") + "\n")
	} else {
		d := rl.commit
		field("subject", d.Subject)
		field("author", d.Author)
		if !d.AuthorTime.IsZero() {
			field("authored", d.AuthorTime.Format("2006-01-02 15:04"))
		}
		b.WriteString("\n" + m.reflogChangedFiles(r, inner, height) + "\n")
	}
	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	rl.inspect.ClampTo(len(lines), max(1, height-1))
	start := min(rl.inspect.Offset(), max(0, len(lines)-max(1, height-1)))
	end := min(len(lines), start+max(1, height-1))
	return inset(strings.Join(lines[start:end], "\n"), width)
}

// reflogChangedFiles lists the files the selected entry's commit changed.
func (m *Model) reflogChangedFiles(r tideui.Renderer, inner, height int) string {
	rl := m.reflog
	files := rl.files
	head := muted(r, "CHANGED FILES") + "  " + r.Styles.DetailBody.Bold(true).Render(plural(len(files), "file"))
	if len(files) == 0 {
		return head + "\n" + muted(r, "This reflog entry changed no files.")
	}
	visible := max(3, min(len(files), height/2))
	if rl.fileIndex < rl.fileTop {
		rl.fileTop = rl.fileIndex
	}
	if rl.fileIndex >= rl.fileTop+visible {
		rl.fileTop = rl.fileIndex - visible + 1
	}
	rl.fileTop = max(0, min(rl.fileTop, max(0, len(files)-visible)))
	rows := []string{head}
	for i := rl.fileTop; i < min(len(files), rl.fileTop+visible); i++ {
		f := files[i]
		text := safeText(f.Path)
		if f.OriginalPath != "" {
			text = safeText(f.OriginalPath) + " → " + text
		}
		rows = append(rows, r.RenderRow(tideui.Row{
			Prefix: string(changeMark(f.Status)) + " ", Text: text, Suffix: fileCounts(f),
			Selected: i == rl.fileIndex && m.focus == 1,
		}, inner))
	}
	return strings.Join(rows, "\n")
}

func (m *Model) reflogDiffPane(r tideui.Renderer, width, height int) string {
	rl := m.reflog
	if rl.diffLabel == "" && !rl.showDiff {
		return inset("\n\n"+accent(r, "No patch open")+"\n\n"+
			muted(r, "On the entry inspector, press Enter\nto open a changed file's patch."), width)
	}
	header := " " + accent(r, clip(rl.diffLabel, max(1, width-3))) + "\n\n"
	if rl.diffLoading {
		return header + " " + muted(r, m.activity()+" Reading patch…")
	}
	if len(rl.view.patch.Files) == 0 || len(rl.view.patch.Files[0].Hunks) == 0 {
		return header + " " + muted(r, "No textual changes to preview.")
	}
	return header + m.renderDiffView(&rl.view, r, width, max(1, height-3), m.focus == 2)
}
