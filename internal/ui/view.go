package ui

import (
	"fmt"
	"strings"

	"github.com/allisonhere/tidegit/internal/git"
	"github.com/allisonhere/tideui"
)

func (m *Model) View() string {
	r := m.renderer()
	if m.width < 54 || m.height < 16 {
		return m.minimumView(r)
	}
	if m.compose != nil {
		return m.commitView(r)
	}
	switch m.screen {
	case screenHistory:
		return m.historyView(r)
	case screenBranches:
		return m.branchesView(r)
	}
	return m.statusView(r)
}

// statusView renders the working-tree screen.
func (m *Model) statusView(r tideui.Renderer) string {
	w := [3]int{m.width*2/10 - 2, m.width*3/10 - 2, m.width - m.width*2/10 - m.width*3/10 - 2}
	tabbed := m.expanded || m.width < 76
	if tabbed {
		w = [3]int{m.width, m.width, m.width}
	}
	height := max(1, m.height-8)
	side := accent(r, "WORKING TREE") + "\n" + muted(r, "Index & working files") + "\n\n"
	for i, name := range git.SectionNames {
		row := r.RenderRow(tideui.Row{Text: strings.ToUpper(name), Suffix: fmt.Sprint(len(m.status.Groups[i])), Selected: m.section == i}, max(1, w[0]-2))
		side += row + "\n"
	}
	side += "\n" + muted(r, "TRACKING") + "\n"
	if m.status.Upstream != "" {
		side += clip(safeText(m.status.Upstream), w[0]-2) + "\n" + muted(r, fmt.Sprintf("%d ahead · %d behind", m.status.Ahead, m.status.Behind))
	} else {
		side += muted(r, "No upstream")
	}
	if m.status.OID == "(initial)" {
		side += "\n\n" + muted(r, "First commit awaits")
	}
	files := m.files()
	rows := []string{""}
	start := max(0, m.selected-height+3)
	for i := start; i < min(len(files), start+height-2); i++ {
		f := files[i]
		name := safeText(f.Path)
		rows = append(rows, r.RenderRow(tideui.Row{Text: name, Suffix: fileMark(f, m.section), Selected: i == m.selected}, max(1, w[1]-2)))
	}
	if len(files) == 0 {
		rows = []string{"", accent(r, emptyTitle(m.section)), "", muted(r, "Choose another section"), muted(r, "to inspect its changes.")}
	}
	title := strings.ToUpper(git.SectionNames[m.section])
	hint := ""
	preview := ""
	if len(files) > 0 {
		f := files[m.selected]
		active := -1
		if len(m.diff.Hunks) > 0 {
			active = m.diff.Hunks[m.hunk].PatchLine
			hint = fmt.Sprintf("Hunk %d/%d", m.hunk+1, len(m.diff.Hunks))
		}
		preview = " " + accent(r, clip(safeText(f.Path), w[2]-2)) + "\n " + muted(r, fileState(f, m.section)+" · "+strings.ToLower(git.SectionNames[m.section])) + "\n\n"
		preview += renderDiff(m.lines, r, m.scroll.Offset(), m.horizontal, max(1, height-3), w[2], active)
		if m.diff.Patch == "" && !m.diffLoading && !m.loading {
			preview += "\n " + muted(r, "No textual changes to preview.")
		}
	} else {
		preview = inset("\n"+accent(r, emptyTitle(m.section))+"\n\n"+muted(r, "Stage files or hunks to prepare your next commit."), w[2])
		total := 0
		for _, g := range m.status.Groups {
			total += len(g)
		}
		if total == 0 && m.repo.Root != "" {
			preview = inset("\n\n"+accent(r, "Working tree clean")+"\n\n"+muted(r, m.cleanContext())+"\n\n"+muted(r, "Ready for your next idea."), w[2])
		}
	}
	if m.err != "" {
		title = "GIT DIAGNOSTIC"
		preview = renderDiff(m.lines, r, m.scroll.Offset(), m.horizontal, height, w[2], -1)
	}
	state := fmt.Sprintf("%d staged  ·  %d unstaged  ·  %d new  ·  %d conflicts", len(m.status.Groups[0]), len(m.status.Groups[1]), len(m.status.Groups[2]), len(m.status.Groups[3]))
	if m.notice != "" {
		state = m.notice
	}
	if m.err != "" {
		state = "Git needs attention · inspect the diagnostic, then refresh"
	}
	if m.loading || m.diffLoading || m.opening {
		state = m.activity() + " Reading repository"
	}
	if m.busy {
		state = m.activity() + " " + m.operation
	}
	layout := tideui.Layout{Width: m.width, Height: m.height - 3, Mode: tideui.ThreeColumn, ColumnRatios: [3]float64{2, 3, 5}, Panes: [3]tideui.Pane{
		{Title: "STATUS", Hint: "01", Content: inset("\n"+side, w[0]), Focused: m.focus == 0},
		{Title: "CHANGES", Hint: fmt.Sprint(len(files)), Content: inset(strings.Join(rows, "\n"), w[1]), Focused: m.focus == 1},
		{Title: title, Hint: hint, Content: preview, Focused: m.focus == 2},
	}, Status: &tideui.StatusBar{Left: clip(safeText(state), m.width-3)}}
	if tabbed {
		layout.Mode = tideui.Tabbed
	}
	base := m.globalHeader(r, "STATUS") + "\n" + r.Render(layout) + "\n" + m.statusHints(r)
	return m.withOverlays(r, base)
}

// withOverlays composites whichever modal is open over a rendered screen. Every
// screen goes through it so overlay behaviour cannot drift between them.
func (m *Model) withOverlays(r tideui.Renderer, base string) string {
	switch {
	case m.picker != nil:
		return r.OverlayModal(base, m.themePickerPanel(r), m.width, m.height)
	case m.palette != nil:
		return r.OverlayModal(base, m.palettePanel(r), m.width, m.height)
	case m.prompt != nil:
		return r.OverlayModal(base, m.promptPanel(r), m.width, m.height)
	case m.confirm != nil:
		return r.OverlayModal(base, m.confirmPanel(r), m.width, m.height)
	case m.help:
		return r.OverlayModal(base, m.helpPanel(r).Content, m.width, m.height)
	}
	return base
}
func (m *Model) cleanContext() string {
	if m.status.Upstream != "" && m.status.Ahead == 0 && m.status.Behind == 0 {
		return m.branchLabel() + " is up to date with " + safeText(m.status.Upstream) + "."
	}
	if m.status.Upstream != "" {
		return fmt.Sprintf("%s · %d ahead, %d behind %s", m.branchLabel(), m.status.Ahead, m.status.Behind, safeText(m.status.Upstream))
	}
	return "No pending changes on " + m.branchLabel() + "."
}
func emptyTitle(section int) string {
	return [4]string{"Nothing staged", "No unstaged changes", "No untracked files", "No conflicts"}[section]
}
func (m *Model) statusHints(r tideui.Renderer) string {
	action := tideui.SoftHint{Key: "s", Label: "stage file"}
	if m.section == int(git.Staged) {
		action = tideui.SoftHint{Key: "u", Label: "unstage file"}
	}
	if m.focus == 2 {
		action.Key = strings.ToUpper(action.Key)
		action.Label = strings.Replace(action.Label, "file", "hunk", 1)
	}
	hints := []tideui.SoftHint{action, {Key: "c", Label: "commit"}, {Key: "Tab", Label: "panes"}, {Key: "?", Label: "help"}}
	if m.filtering || m.query != "" {
		return m.hintBar(r, tideui.SoftHint{Key: "/", Label: safeText(m.query)}, tideui.SoftHint{Key: "Enter", Label: "keep"}, tideui.SoftHint{Key: "Esc", Label: "clear"})
	}
	return m.hintBar(r, hints...)
}
func fileMark(f git.File, section int) string {
	if section == int(git.Untracked) {
		return "NEW"
	}
	if section == int(git.Conflicted) {
		return "!"
	}
	return strings.ToUpper(fileState(f, section)[:1])
}
func fileState(f git.File, section int) string {
	if section == int(git.Untracked) {
		return "New file"
	}
	if section == int(git.Conflicted) {
		return "Conflict"
	}
	if len(f.XY) != 2 {
		return "Changed"
	}
	code := f.XY[0]
	if section == int(git.Unstaged) {
		code = f.XY[1]
	}
	switch code {
	case 'A':
		return "Added"
	case 'D':
		return "Deleted"
	case 'R':
		return "Renamed"
	case 'C':
		return "Copied"
	case 'T':
		return "Type changed"
	default:
		return "Modified"
	}
}
