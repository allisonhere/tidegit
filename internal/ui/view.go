package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/allisonhere/tidegit/internal/git"
	"github.com/allisonhere/tideui"
	"github.com/charmbracelet/x/ansi"
)

func (m *Model) View() string {
	r := tideui.NewRenderer(m.theme, tideui.StyleOptions{Density: tideui.Compact})
	w := [3]int{max(1, m.width*2/10-2), max(1, m.width*3/10-2), max(1, m.width-m.width*2/10-m.width*3/10-2)}
	if m.expanded || m.width < 65 {
		w = [3]int{m.width, m.width, m.width}
	}
	branch := m.status.Branch
	if branch == "(detached)" {
		branch = "Detached HEAD " + m.status.OID[:min(8, len(m.status.OID))]
	}
	if m.status.OID == "(initial)" {
		branch += " (no commits)"
	}
	if branch == "" {
		branch = "No repository loaded"
	}
	summary := safeText(filepath.Base(m.repo.Root)) + "\n" + safeText(branch) + "\n"
	if m.status.Upstream != "" {
		summary += fmt.Sprintf("%s\nAhead %d / behind %d", safeText(m.status.Upstream), m.status.Ahead, m.status.Behind)
	} else {
		summary += "No upstream"
	}
	summary += "\n\n"
	for i, name := range git.SectionNames {
		summary += r.RenderRow(tideui.Row{Text: name, Suffix: fmt.Sprint(len(m.status.Groups[i])), Selected: m.section == i}, w[0]) + "\n"
	}
	files := m.files()
	rows := []string{}
	height := max(1, m.height-5)
	start := max(0, m.selected-height+1)
	for i := start; i < min(len(files), start+height); i++ {
		f := files[i]
		label := safeText(f.Path)
		if f.OriginalPath != "" {
			label = safeText(f.OriginalPath) + " → " + label
		}
		rows = append(rows, r.RenderRow(tideui.Row{Text: label, Prefix: fileState(f, m.section) + " ", Selected: i == m.selected}, w[1]))
	}
	if len(rows) == 0 {
		rows = append(rows, "No files in this section.")
	}
	title := git.SectionNames[m.section] + " diff"
	preview := "Select a changed file to inspect its diff."
	if len(files) == 0 && m.repo.Root != "" {
		preview = "No changes in " + git.SectionNames[m.section] + "."
		total := 0
		for _, group := range m.status.Groups {
			total += len(group)
		}
		if total == 0 {
			preview = "Working tree clean.\n\nNo staged, unstaged, untracked or conflicted files."
		}
	}
	if len(files) > 0 {
		title += " · " + safeText(files[m.selected].Path)
		active := -1
		if len(m.diff.Hunks) > 0 {
			active = m.diff.Hunks[m.hunk].PatchLine
			title = fmt.Sprintf("%s · Hunk %d/%d · %s", git.SectionNames[m.section], m.hunk+1, len(m.diff.Hunks), safeText(files[m.selected].Path))
		}
		preview = renderDiff(m.lines, r, m.scroll.Offset(), m.horizontal, height, w[2], active)
		if m.diff.Patch == "" && !m.diff.Conflict {
			preview = "No textual diff. The file may contain only metadata or submodule changes."
		}
	}
	if m.diffLoading {
		preview = "Loading: git diff…"
	}
	if m.loading {
		preview = "Refreshing: git status --porcelain=v2 --branch…"
	}
	if m.err != "" {
		preview = renderDiff(m.lines, r, m.scroll.Offset(), m.horizontal, height, w[2], -1)
	}
	state := "ready"
	if m.loading {
		state = "git status…"
	} else if m.diffLoading {
		state = "git diff…"
	} else if m.err != "" {
		state = "Git error — see right pane"
	}
	left := fmt.Sprintf("TideGit · Status · %s · %s · +%d/-%d · S:%d U:%d ?:%d !:%d · %s", safeText(filepath.Base(m.repo.Root)), safeText(branch), m.status.Ahead, m.status.Behind, len(m.status.Groups[0]), len(m.status.Groups[1]), len(m.status.Groups[2]), len(m.status.Groups[3]), state)
	if m.filtering || m.query != "" {
		left = "Filter files: " + safeText(m.query) + "  (Enter keeps · Esc clears)"
	}
	if m.notice != "" {
		left = m.notice
	}
	if m.err != "" {
		left = "Git error · see diff pane · r refresh"
	}
	if m.busy {
		left = m.operation + "…"
	}
	left = ansi.Truncate(left, max(1, m.width-23), "…")
	layout := tideui.Layout{Width: m.width, Height: m.height, Mode: tideui.ThreeColumn, ColumnRatios: [3]float64{2, 3, 5}, Panes: [3]tideui.Pane{
		{Title: "Status", Content: summary, Focused: m.focus == 0},
		{Title: git.SectionNames[m.section], Content: strings.Join(rows, "\n"), Focused: m.focus == 1},
		{Title: title, Content: preview, Focused: m.focus == 2},
	}, Status: &tideui.StatusBar{Left: left, Right: "s/u file · ? help"}}
	if m.expanded || m.width < 65 {
		layout.Mode = tideui.Tabbed
	}
	if m.help {
		overlay := r.SoftPanelOverlay(tideui.SoftPanel{Prefix: "tidegit", Title: "status help", Width: min(66, max(10, m.width-4)), Content: helpText})
		layout.Modal = &overlay
	}
	return r.Render(layout)
}

func fileState(f git.File, section int) string {
	if section == int(git.Untracked) {
		return "New"
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
		return "Type"
	default:
		return "Modified"
	}
}

const helpText = `Inspect and prepare changes

Tab / Shift-Tab   Focus next / previous pane
j / k or arrows   Select section, file; scroll diff
h / l, Left/Right Scroll diff horizontally
PageUp / PageDown Scroll faster
Home / End        First / last item or diff line
Enter             Focus the next pane
/                 Filter files in the current section
s / u             Stage / unstage selected FILE
S / U             Stage / unstage selected HUNK
[ / ]             Previous / next hunk (marked >)
r / Ctrl-R        Refresh repository status
z                 Expand / restore focused pane
Esc               Clear filter / error; restore panes
q                 Restore expanded pane, otherwise quit
?                 Open / close help

Unstage preserves working-tree content.
Commit workflow is not implemented.`
