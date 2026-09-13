package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/allisonhere/tidegit/internal/git"
	"github.com/allisonhere/tideui"
	"github.com/charmbracelet/lipgloss"
)

func (m *Model) stashView(r tideui.Renderer) string {
	s := m.stash
	mode, w := m.paneLayout()
	if mode == tideui.ThreeColumn {
		w = [3]int{m.width*4/10 - 2, m.width*3/10 - 2, m.width - m.width*4/10 - m.width*3/10 - 2}
	}
	height := m.historyPaneHeight()
	paneHeight, inspectorHeight := height, height
	if mode == tideui.StackedRight {
		paneHeight = max(1, height*2/5)
		inspectorHeight = max(1, height-paneHeight)
	}

	title, hint := "STASH", ""
	if stash, ok := s.current(); ok {
		hint = stash.Ref
		if s.filesLoading || s.diffLoading {
			title = "STASH DIFF"
		}
	}
	layout := tideui.Layout{Width: m.width, Height: m.height - 3, Mode: tideui.ThreeColumn,
		ColumnRatios: [3]float64{4, 3, 3}, SidebarRatio: 0.30, Panes: [3]tideui.Pane{
			{Title: "STASHES", Hint: fmt.Sprint(len(s.stashes)), Content: m.stashListPane(r, w[0], height), Focused: m.focus == 0},
			{Title: "FILES", Hint: fmt.Sprint(len(s.files)), Content: m.stashFilesPane(r, w[1], paneHeight), Focused: m.focus == 1},
			{Title: title, Hint: hint, Content: m.stashDiffPane(r, w[2], inspectorHeight), Focused: m.focus == 2},
		}, Status: &tideui.StatusBar{Left: clip(safeText(m.stashStatus()), m.width-3)}}
	layout.Mode = mode
	layout.UpperRightRatio = 0.40

	hints := []tideui.SoftHint{{Key: "a", Label: "apply"}, {Key: "p", Label: "pop"},
		{Key: "d", Label: "drop"}, {Key: "n", Label: "stash"}, {Key: "/", Label: "filter"}, {Key: "?", Label: "help"}}
	if s.filtering {
		hints = []tideui.SoftHint{{Key: "/", Label: safeText(s.filter)},
			{Key: "Enter", Label: "keep"}, {Key: "Esc", Label: "clear"}}
	}
	base := m.globalHeader(r, "STASH") + "\n" + r.Render(layout) + "\n" + m.hintBar(r, hints...)
	return m.withOverlays(r, base)
}

func (m *Model) stashStatus() string {
	s := m.stash
	switch {
	case s.err != "":
		return "Stash could not be read · " + firstLine(s.err)
	case m.operationRunning():
		return m.activity() + " " + m.operation
	case s.loading:
		return m.activity() + " Reading stashes"
	case m.notice != "":
		return m.notice
	}
	if stash, ok := s.current(); ok {
		when := m.timeAgo(stash.Time)
		return fmt.Sprintf("%s · %s · %s ago", stash.Ref, firstLine(safeText(stash.Message)), when)
	}
	if len(s.stashes) == 0 {
		return "No stash entries · press n to stash working changes"
	}
	return fmt.Sprintf("%d stash entries", len(s.stashes))
}

func (m *Model) stashListPane(r tideui.Renderer, width, height int) string {
	s := m.stash
	if len(s.stashes) == 0 {
		body := "Nothing is stashed.\n\nPress n to stash tracked changes, or N\nto include untracked files."
		if s.loading {
			body = m.activity() + " Reading stashes…"
		} else if s.filter != "" {
			body = "No stash matches " + safeText(s.filter) + "."
		}
		return inset("\n\n"+accent(r, "No stashes")+"\n\n"+muted(r, body), width)
	}
	list := s.visible()
	visible := max(1, height-2)
	if s.index < s.top {
		s.top = s.index
	}
	if s.index >= s.top+visible {
		s.top = s.index - visible + 1
	}
	s.top = max(0, min(s.top, max(0, len(list)-visible)))

	now := time.Now()
	var rows []string
	for i := s.top; i < min(len(list), s.top+visible); i++ {
		rows = append(rows, m.stashRow(r, list[i], i == s.index, max(1, width-2), now))
	}
	return inset(strings.Join(rows, "\n"), width)
}

// stashRow draws one elegant entry: the selector, the source branch, the
// message and the age.
func (m *Model) stashRow(r tideui.Renderer, stash git.Stash, selected bool, width int, now time.Time) string {
	base := r.Styles.Item
	if selected {
		base = r.Styles.ItemSelected
	}
	base = base.UnsetPadding().UnsetWidth()
	meta := base.Foreground(r.Styles.Theme.Dimmed)
	if selected {
		meta = base
	}
	ref := base.Foreground(r.Styles.Theme.BorderFocus).Bold(true).Render(stash.Ref)
	branch := ""
	if stash.Branch != "" {
		branch = meta.Render(stash.Branch) + base.Render("  ")
	}
	age := meta.Render(fmt.Sprintf("%4s", m.timeAgo(stash.Time)))
	fixed := lipgloss.Width(ref) + 2 + lipgloss.Width(branch) + lipgloss.Width(age)
	message := base.Render(clip(safeText(stash.Message), max(1, width-fixed)))
	left := " " + ref + "  " + branch + message
	gap := max(0, width-lipgloss.Width(left)-lipgloss.Width(age))
	return left + base.Render(strings.Repeat(" ", gap)) + age
}

// stashFilesPane lists the selected stash's changed files with counts.
func (m *Model) stashFilesPane(r tideui.Renderer, width, height int) string {
	s := m.stash
	inner := max(1, width-2)
	if _, ok := s.current(); !ok {
		return inset("\n\n"+accent(r, "Nothing selected")+"\n\n"+muted(r, "Choose a stash to inspect it."), width)
	}
	if s.filesLoading {
		return inset("\n"+muted(r, m.activity()+" Reading files…"), width)
	}
	if len(s.files) == 0 {
		return inset("\n\n"+accent(r, "No files")+"\n\n"+muted(r, "This stash entry changed nothing."), width)
	}
	adds, dels := 0, 0
	for _, f := range s.files {
		if !f.Binary {
			adds += f.Additions
			dels += f.Deletions
		}
	}
	head := muted(r, "CHANGED FILES") + "  " +
		r.Styles.DetailBody.Foreground(r.Styles.Theme.Unread).Render(fmt.Sprintf("+%d", adds)) + " " +
		r.Styles.DetailBody.Foreground(r.Styles.Theme.Error).Render(fmt.Sprintf("−%d", dels))
	rows := []string{"", head}
	visible := max(1, height-4)
	if s.fileIndex < s.fileTop {
		s.fileTop = s.fileIndex
	}
	if s.fileIndex >= s.fileTop+visible {
		s.fileTop = s.fileIndex - visible + 1
	}
	s.fileTop = max(0, min(s.fileTop, max(0, len(s.files)-visible)))
	for i := s.fileTop; i < min(len(s.files), s.fileTop+visible); i++ {
		f := s.files[i]
		text := safeText(f.Path)
		if f.OriginalPath != "" {
			text = safeText(f.OriginalPath) + " → " + text
		}
		rows = append(rows, r.RenderRow(tideui.Row{
			Prefix:   string(changeMark(f.Status)) + " ",
			Text:     text,
			Suffix:   fileCounts(f),
			Selected: i == s.fileIndex && m.focus == 1,
		}, inner))
	}
	return strings.Join(rows, "\n")
}

// stashDiffPane renders the selected file's patch through the shared diff
// renderer, labelled so it is never mistaken for working-tree state.
func (m *Model) stashDiffPane(r tideui.Renderer, width, height int) string {
	s := m.stash
	if _, ok := s.current(); !ok {
		return inset("\n\n"+accent(r, "Nothing selected")+"\n\n"+muted(r, "Choose a stash to inspect it."), width)
	}
	header := " " + accent(r, clip(s.diffLabel, max(1, width-3))) + "\n " +
		muted(r, "stash entry · apply from the stash list") + "\n\n"
	if s.filesLoading || s.diffLoading {
		return header + " " + muted(r, m.activity()+" Reading patch…")
	}
	if len(s.view.patch.Files) == 0 || len(s.view.patch.Files[0].Hunks) == 0 {
		return header + " " + muted(r, "No textual changes to preview.")
	}
	return header + m.renderDiffView(&s.view, r, width, max(1, height-3), m.focus == 2)
}
