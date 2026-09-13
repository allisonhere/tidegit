package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/allisonhere/tidegit/internal/git"
	"github.com/allisonhere/tideui"
	"github.com/charmbracelet/lipgloss"
)

func (m *Model) branchesView(r tideui.Renderer) string {
	b := m.branches
	mode, w := m.paneLayout()
	if mode == tideui.ThreeColumn {
		w = [3]int{m.width*3/10 - 2, m.width*4/10 - 2, m.width - m.width*3/10 - m.width*4/10 - 2}
	}
	height := m.historyPaneHeight()
	// Only the stacked layout splits the height between the two right panes;
	// side-by-side columns each get the full height.
	paneHeight, inspectorHeight := height, height
	if mode == tideui.StackedRight {
		paneHeight = max(1, height*2/5)
		inspectorHeight = max(1, height-paneHeight)
	}

	branch, hasBranch := b.current()
	hint := ""
	if hasBranch {
		hint = branch.Short
	}
	layout := tideui.Layout{Width: m.width, Height: m.height - 3, Mode: tideui.ThreeColumn,
		ColumnRatios: [3]float64{3, 4, 3}, SidebarRatio: 0.28, Panes: [3]tideui.Pane{
			{Title: "BRANCHES", Hint: fmt.Sprint(len(b.local) + len(b.remote)),
				Content: m.branchListPane(r, w[0], height), Focused: m.focus == 0},
			{Title: "COMMITS", Hint: branchHint(b), Content: m.branchCommitsPane(r, w[1], paneHeight), Focused: m.focus == 1},
			{Title: "BRANCH", Hint: hint, Content: m.branchInspectorPane(r, w[2], inspectorHeight), Focused: m.focus == 2},
		}, Status: &tideui.StatusBar{Left: clip(safeText(m.branchStatus()), m.width-3)}}
	layout.Mode = mode
	layout.UpperRightRatio = 0.40
	hints := []tideui.SoftHint{{Key: "Enter", Label: "switch"}, {Key: "n", Label: "new"},
		{Key: "R", Label: "rename"}, {Key: "D", Label: "delete"}, {Key: "?", Label: "help"}}
	if b.filtering {
		hints = []tideui.SoftHint{{Key: "/", Label: safeText(b.filter)},
			{Key: "Enter", Label: "keep"}, {Key: "Esc", Label: "clear"}}
	}
	base := m.globalHeader(r, "BRANCHES") + "\n" + r.Render(layout) + "\n" + m.hintBar(r, hints...)
	return m.withOverlays(r, base)
}

func branchHint(b *branchState) string {
	if b.commitsFor == "" {
		return ""
	}
	return safeText(b.commitsFor)
}

func (m *Model) branchStatus() string {
	b := m.branches
	switch {
	case b.err != "":
		return "Git could not complete the request · " + firstLine(b.err)
	case m.busy:
		return m.activity() + " " + m.operation
	case b.loading:
		return m.activity() + " Reading branches"
	case m.notice != "":
		return m.notice
	}
	if branch, ok := b.current(); ok {
		kind := "local branch"
		if branch.Remote {
			kind = "remote-tracking branch"
		}
		state := aheadBehindWords(branch)
		if branch.Remote {
			state = "read-only in this milestone"
			if len(branch.TrackedByLocals) > 0 {
				state = "tracked by " + strings.Join(branch.TrackedByLocals, ", ")
			}
		}
		return fmt.Sprintf("%s · %s · %s", safeText(branch.Name), kind, state)
	}
	return fmt.Sprintf("%d local · %d remote-tracking", len(b.local), len(b.remote))
}

// branchListPane draws local and remote branches as one navigable list.
func (m *Model) branchListPane(r tideui.Renderer, width, height int) string {
	b := m.branches
	inner := max(1, width-2)
	rows := b.rows()
	if len(b.local) == 0 && len(b.remote) == 0 {
		body := "This repository has no branches yet.\nMake a commit to create one."
		if b.loading {
			body = m.activity() + " Reading branches…"
		} else if b.filter != "" {
			body = "No branch matches " + safeText(b.filter) + "."
		}
		return inset("\n\n"+accent(r, "No branches")+"\n\n"+muted(r, body), width)
	}

	visible := max(1, height-2)
	cursor := b.cursor()
	if cursor < b.top {
		b.top = cursor
	}
	if cursor >= b.top+visible {
		b.top = cursor - visible + 1
	}
	b.top = max(0, min(b.top, max(0, len(rows)-visible)))

	var out []string
	for i := b.top; i < min(len(rows), b.top+visible); i++ {
		row := rows[i]
		if row.caption != "" {
			out = append(out, muted(r, row.caption))
			continue
		}
		out = append(out, m.branchRow(r, row, i == cursor, inner))
	}
	return inset(strings.Join(out, "\n"), width)
}

// branchRow renders one branch with its marker and tracking summary.
func (m *Model) branchRow(r tideui.Renderer, row branchRow, selected bool, width int) string {
	br := row.branch
	prefix := "  "
	if br.Current {
		prefix = "@ "
	}
	suffix := ""
	switch {
	case br.Remote:
		suffix = br.Short
	case br.UpstreamGone:
		suffix = "gone"
	case br.Upstream == "":
		suffix = "—"
	case br.Ahead > 0 && br.Behind > 0:
		suffix = fmt.Sprintf("↑%d ↓%d", br.Ahead, br.Behind)
	case br.Ahead > 0:
		suffix = fmt.Sprintf("↑%d", br.Ahead)
	case br.Behind > 0:
		suffix = fmt.Sprintf("↓%d", br.Behind)
	default:
		suffix = "✓"
	}
	return r.RenderRow(tideui.Row{Prefix: prefix, Text: safeText(br.Name), Suffix: suffix,
		Selected: selected, Muted: br.Remote && !selected}, width)
}

// branchCommitsPane lists the selected branch's own history with the graph.
func (m *Model) branchCommitsPane(r tideui.Renderer, width, height int) string {
	b := m.branches
	inner := max(1, width-2)
	if b.commitsLoading && len(b.commits) == 0 {
		return inset("\n"+muted(r, m.activity()+" Reading commits…"), width)
	}
	if len(b.commits) == 0 {
		return inset("\n\n"+accent(r, "No commits")+"\n\n"+muted(r, "This ref has no history to show."), width)
	}
	visible := max(1, height-2)
	if b.commitIndex < b.commitTop {
		b.commitTop = b.commitIndex
	}
	if b.commitIndex >= b.commitTop+visible {
		b.commitTop = b.commitIndex - visible + 1
	}
	b.commitTop = max(0, min(b.commitTop, max(0, len(b.commits)-visible)))

	lanes := 1
	for i := b.commitTop; i < min(len(b.commits), b.commitTop+visible); i++ {
		if i < len(b.graph) {
			lanes = max(lanes, b.graph[i].Width())
		}
	}
	gw := graphWidth(lanes)
	now := time.Now()
	var rows []string
	for i := b.commitTop; i < min(len(b.commits), b.commitTop+visible); i++ {
		rows = append(rows, m.branchCommitRow(r, i, inner, gw, now))
	}
	return strings.Join(rows, "\n")
}

func (m *Model) branchCommitRow(r tideui.Renderer, index, width, graphW int, now time.Time) string {
	b := m.branches
	c := b.commits[index]
	selected := index == b.commitIndex && m.focus == 1
	base := r.Styles.Item
	if selected {
		base = r.Styles.ItemSelected
	}
	base = base.UnsetPadding().UnsetWidth()
	isHead := false
	for _, ref := range c.Decorations {
		if ref.Head {
			isHead = true
		}
	}
	var row git.GraphRow
	if index < len(b.graph) {
		row = b.graph[index]
	}
	graph := renderGraphRow(r, row, isHead, selected, graphW, base)
	meta := base.Foreground(r.Styles.Theme.Dimmed)
	if selected {
		meta = base
	}
	right := meta.Render(fmt.Sprintf("%4s", relativeTime(c.AuthorTime, now)))
	hash := ""
	if width >= widthForHash {
		hash = meta.Render(c.Short + " ")
	}
	subjectWidth := max(1, width-graphW-lipgloss.Width(hash)-lipgloss.Width(right)-1)
	left := graph + hash + base.Render(clip(safeText(c.Subject), subjectWidth))
	gap := max(0, width-lipgloss.Width(left)-lipgloss.Width(right))
	return left + base.Render(strings.Repeat(" ", gap)) + right
}

// branchInspectorPane presents one branch's metadata.
func (m *Model) branchInspectorPane(r tideui.Renderer, width, height int) string {
	b := m.branches
	branch, ok := b.current()
	if !ok {
		return inset("\n\n"+accent(r, "Nothing selected")+"\n\n"+muted(r, "Choose a branch to inspect it."), width)
	}
	inner := max(1, width-2)
	narrow := width < 34
	var out []string

	kind := "LOCAL BRANCH"
	if branch.Remote {
		kind = "REMOTE-TRACKING"
	}
	out = append(out, "", accent(r, kind))
	for _, line := range wrapText(safeText(branch.Name), inner) {
		out = append(out, r.Styles.DetailBody.Bold(true).Render(line))
	}
	if branch.Current {
		out = append(out, r.Styles.DetailTitle.Render(" current branch "))
	}
	out = append(out, "")

	// Tracking is the headline fact, so it leads and is stated in words as
	// well as arrows.
	if !branch.Remote {
		out = append(out, muted(r, "TRACKING"), branchAheadBehind(r, branch))
		// The words carry the same fact as the arrows, for anyone the arrows
		// do not reach; they wrap rather than being cut off.
		for _, line := range wrapText(aheadBehindWords(branch), inner) {
			out = append(out, muted(r, line))
		}
		out = append(out, "")
	} else {
		out = append(out, muted(r, "REMOTE"), r.Styles.DetailBody.Render(safeText(branch.RemoteName)))
		if len(branch.TrackedByLocals) > 0 {
			out = append(out, muted(r, "tracked by "+safeText(strings.Join(branch.TrackedByLocals, ", "))))
		} else {
			out = append(out, muted(r, "no local branch tracks it"))
		}
		out = append(out, "")
	}

	field := func(label, value string) {
		if value == "" {
			return
		}
		out = append(out, muted(r, fmt.Sprintf("%-9s", label))+r.Styles.DetailBody.Render(clip(value, max(1, inner-9))))
	}
	field("target", branch.OID)
	field("tip", safeText(branch.Subject))
	field("author", safeText(branch.Author))
	if !branch.CommitTime.IsZero() {
		field("when", fmt.Sprintf("%s ago · %s", relativeTime(branch.CommitTime, time.Now()),
			branch.CommitTime.Format("2006-01-02 15:04")))
	}
	if branch.Upstream != "" {
		field("upstream", safeText(branch.Upstream))
	}

	out = append(out, "", muted(r, "AGAINST HEAD"))
	switch {
	case branch.Current:
		out = append(out, r.Styles.DetailBody.Render("This is HEAD."))
	case b.extraLoading && b.extraFor != branch.Name:
		out = append(out, muted(r, m.activity()+" comparing…"))
	case b.extraFor != branch.Name:
		out = append(out, muted(r, "—"))
	case b.extra.MergeBase == "":
		out = append(out, r.Styles.DetailBody.Render("Unrelated history · no common ancestor"))
	default:
		merged := "not merged into HEAD"
		if branch.Merged {
			merged = "merged into HEAD"
		}
		out = append(out, r.Styles.DetailBody.Render(merged))
		if b.extra.Comparable {
			out = append(out, muted(r, fmt.Sprintf("%d ahead, %d behind HEAD", b.extra.Ahead, b.extra.Behind)))
		}
		if !narrow {
			out = append(out, muted(r, "base "+b.extra.MergeBase[:min(12, len(b.extra.MergeBase))]))
		}
	}

	lines := strings.Split(strings.Join(out, "\n"), "\n")
	b.inspect.ClampTo(len(lines), max(1, height-1))
	start := min(b.inspect.Offset(), max(0, len(lines)-max(1, height-1)))
	end := min(len(lines), start+max(1, height-1))
	return inset(strings.Join(lines[start:end], "\n"), width)
}
