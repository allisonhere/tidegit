package ui

import (
	"fmt"
	"strings"

	"github.com/allisonhere/tideui"
)

func (m *Model) remotesView(r tideui.Renderer) string {
	rem := m.remotes
	mode, w := m.paneLayout()
	if mode == tideui.ThreeColumn {
		w = [3]int{m.width*3/10 - 2, m.width*4/10 - 2, m.width - m.width*3/10 - m.width*4/10 - 2}
	}
	height := m.historyPaneHeight()
	paneHeight, inspectorHeight := height, height
	if mode == tideui.StackedRight {
		paneHeight = max(1, height*3/5)
		inspectorHeight = max(1, height-paneHeight)
	}

	hint := ""
	if remote, ok := rem.current(); ok {
		hint = fmt.Sprint(len(remote.Branches))
	}
	layout := tideui.Layout{Width: m.width, Height: m.height - 3, Mode: tideui.ThreeColumn,
		ColumnRatios: [3]float64{3, 4, 3}, SidebarRatio: 0.30, Panes: [3]tideui.Pane{
			{Title: "REMOTES", Hint: fmt.Sprint(len(rem.remotes)), Content: m.remoteListPane(r, w[0], height), Focused: m.focus == 0},
			{Title: "TRACKED BRANCHES", Hint: hint, Content: m.remoteBranchesPane(r, w[1], paneHeight), Focused: m.focus == 1},
			{Title: "REMOTE", Hint: "inspect", Content: m.remoteInspectorPane(r, w[2], inspectorHeight), Focused: m.focus == 2},
		}, Status: &tideui.StatusBar{Left: clip(safeText(m.remoteStatus()), m.width-3)}}
	layout.Mode = mode
	layout.UpperRightRatio = 0.52

	hints := []tideui.SoftHint{{Key: "f", Label: "fetch"}, {Key: "F", Label: "fetch all"},
		{Key: "P", Label: "push"}, {Key: "p", Label: "pull"}, {Key: "Tab", Label: "panes"}, {Key: "?", Label: "help"}}
	if rem.filtering {
		hints = []tideui.SoftHint{{Key: "/", Label: safeText(rem.filter)},
			{Key: "Enter", Label: "keep"}, {Key: "Esc", Label: "clear"}}
	}
	base := m.globalHeader(r, "REMOTES") + "\n" + r.Render(layout) + "\n" + m.hintBar(r, hints...)
	return m.withOverlays(r, base)
}

func (m *Model) remoteStatus() string {
	rem := m.remotes
	switch {
	case rem.err != "":
		return "Git could not read the remotes · " + firstLine(rem.err)
	case m.operationRunning():
		return m.activity() + " " + m.operation
	case rem.loading:
		return m.activity() + " Reading remotes"
	case m.notice != "":
		return m.notice
	}
	if remote, ok := rem.current(); ok {
		state := fmt.Sprintf("%d tracked branches", len(remote.Branches))
		if remote.Default {
			state += " · default"
		}
		return remote.Name + " · " + state
	}
	if len(rem.remotes) == 0 {
		return "No remote is configured"
	}
	return fmt.Sprintf("%d remotes", len(rem.remotes))
}

func (m *Model) remoteListPane(r tideui.Renderer, width, height int) string {
	rem := m.remotes
	inner := max(1, width-2)
	if len(rem.remotes) == 0 {
		if rem.loading {
			return inset("\n"+muted(r, m.activity()+" Reading remotes…"), width)
		}
		body := "Add one with Git:\n\n  git remote add origin URL"
		if rem.filter != "" {
			body = "No remote matches " + safeText(rem.filter) + "."
		}
		return inset("\n\n"+accent(r, "No remotes")+"\n\n"+muted(r, body), width)
	}
	list := rem.visible()
	visible := max(1, height-2)
	if rem.index < rem.top {
		rem.top = rem.index
	}
	if rem.index >= rem.top+visible {
		rem.top = rem.index - visible + 1
	}
	rem.top = max(0, min(rem.top, max(0, len(list)-visible)))

	var rows []string
	for i := rem.top; i < min(len(list), rem.top+visible); i++ {
		remote := list[i]
		prefix := "  "
		if remote.Default {
			prefix = "◆ "
		}
		suffix := fmt.Sprintf("%d", len(remote.Branches))
		rows = append(rows, r.RenderRow(tideui.Row{Prefix: prefix, Text: safeText(remote.Name),
			Suffix: suffix, Selected: i == rem.index}, inner))
	}
	return inset(strings.Join(rows, "\n"), width)
}

// remoteBranchesPane lists the selected remote's tracking branches with the
// ahead/behind state of the local branch that tracks each one.
func (m *Model) remoteBranchesPane(r tideui.Renderer, width, height int) string {
	rem := m.remotes
	remote, ok := rem.current()
	if !ok {
		return inset("\n\n"+accent(r, "No remote")+"\n\n"+muted(r, "Choose a remote to inspect."), width)
	}
	inner := max(1, width-2)
	branches := rem.branchesFor(remote.Name)
	if len(branches) == 0 {
		return inset("\n\n"+accent(r, "No tracked branches")+"\n\n"+
			muted(r, "Fetch "+safeText(remote.Name)+" to bring its\nbranches into the repository."), width)
	}
	visible := max(1, height-2)
	if rem.branchIndex < rem.branchTop {
		rem.branchTop = rem.branchIndex
	}
	if rem.branchIndex >= rem.branchTop+visible {
		rem.branchTop = rem.branchIndex - visible + 1
	}
	rem.branchTop = max(0, min(rem.branchTop, max(0, len(branches)-visible)))

	var rows []string
	for i := rem.branchTop; i < min(len(branches), rem.branchTop+visible); i++ {
		branch := branches[i]
		short := strings.TrimPrefix(branch.Name, remote.Name+"/")
		suffix := branch.Short
		if branch.Ahead > 0 || branch.Behind > 0 {
			suffix = fmt.Sprintf("↑%d ↓%d", branch.Ahead, branch.Behind)
		}
		rows = append(rows, r.RenderRow(tideui.Row{Text: safeText(short), Suffix: suffix,
			Selected: i == rem.branchIndex && m.focus == 1}, inner))
	}
	if rem.branchTop+visible < len(branches) {
		rows = append(rows, muted(r, fmt.Sprintf(" %d more…", len(branches)-rem.branchTop-visible)))
	}
	return inset(strings.Join(rows, "\n"), width)
}

// remoteInspectorPane gives the relationship hierarchy: remote first, tracked
// branches second, relationship state third, URLs last and styled as secondary
// metadata so a long URL never dominates.
func (m *Model) remoteInspectorPane(r tideui.Renderer, width, height int) string {
	rem := m.remotes
	remote, ok := rem.current()
	if !ok {
		return inset("\n\n"+accent(r, "Nothing selected")+"\n\n"+muted(r, "Choose a remote to inspect it."), width)
	}
	inner := max(1, width-2)
	var out []string
	out = append(out, "", accent(r, "REMOTE"))
	for _, line := range wrapText(safeText(remote.Name), inner) {
		out = append(out, r.Styles.DetailBody.Bold(true).Render(line))
	}
	if remote.Default {
		out = append(out, r.Styles.DetailTitle.Render(" default "))
	}
	out = append(out, "")

	// Tracked branches and their relationship to local work.
	out = append(out, muted(r, "TRACKED BRANCHES"))
	branches := rem.branchesFor(remote.Name)
	if len(branches) == 0 {
		out = append(out, muted(r, "none yet · fetch this remote"))
	}
	for _, branch := range branches {
		short := strings.TrimPrefix(branch.Name, remote.Name+"/")
		relation := ""
		switch {
		case len(branch.TrackedByLocals) > 0:
			relation = "← " + strings.Join(branch.TrackedByLocals, ", ")
		case branch.Ahead > 0 || branch.Behind > 0:
			relation = fmt.Sprintf("↑%d ↓%d", branch.Ahead, branch.Behind)
		}
		out = append(out, r.Styles.DetailBody.Render(short)+muted(r, "  "+relation))
	}
	out = append(out, "")

	out = append(out, muted(r, "LAST FETCH"))
	if rem.lastFetchOK {
		out = append(out, r.Styles.DetailBody.Render(m.timeAgo(rem.lastFetch)+" ago")+
			muted(r, " · "+rem.lastFetch.Format("2006-01-02 15:04")))
	} else {
		out = append(out, muted(r, "not recorded in this repository"))
	}
	out = append(out, "")

	// URLs are the noisiest fact, so they are last and de-emphasised.
	out = append(out, muted(r, "FETCH URL"))
	for _, line := range wrapText(safeText(remote.FetchURL), inner) {
		out = append(out, muted(r, line))
	}
	if remote.PushURL != remote.FetchURL {
		out = append(out, muted(r, "PUSH URL"))
		for _, line := range wrapText(safeText(remote.PushURL), inner) {
			out = append(out, muted(r, line))
		}
	}

	lines := strings.Split(strings.Join(out, "\n"), "\n")
	rem.inspect.ClampTo(len(lines), max(1, height-1))
	start := min(rem.inspect.Offset(), max(0, len(lines)-max(1, height-1)))
	end := min(len(lines), start+max(1, height-1))
	return inset(strings.Join(lines[start:end], "\n"), width)
}
