package ui

import (
	"strings"

	"github.com/allisonhere/tideui"
	tea "github.com/charmbracelet/bubbletea"
)

// paletteAction identifies one command. Commands dispatch through the same
// methods the keys use, so the palette can never drift from the bindings.
type paletteAction int

const (
	paletteGoStatus paletteAction = iota
	paletteGoHistory
	paletteGoBranches
	paletteCheckout
	paletteCreateBranch
	paletteRenameBranch
	paletteDeleteBranch
	paletteJumpToRef
	paletteFilterHistory
	paletteCopyHash
	paletteInspectCommit
	paletteRefresh
	paletteTheme
	paletteFollowOmarchy
	paletteHelp
)

type paletteItem struct {
	title  string
	hint   string
	action paletteAction
	// screens limits where a command is offered; empty means everywhere.
	screens []screen
}

type paletteState struct {
	query string
	index int
	items []paletteItem // the filtered list currently shown
}

// paletteCommands is the whole command set. It stays deliberately small: every
// entry does something a key alone cannot, or names something hard to discover.
func paletteCommands() []paletteItem {
	return []paletteItem{
		{title: "Go to Status", hint: "1", action: paletteGoStatus},
		{title: "Go to History", hint: "2", action: paletteGoHistory},
		{title: "Go to Branches", hint: "3", action: paletteGoBranches},
		{title: "Checkout branch", hint: "Branches · Enter", action: paletteCheckout, screens: []screen{screenBranches}},
		{title: "Create branch here", hint: "n", action: paletteCreateBranch},
		{title: "Rename branch", hint: "Branches · R", action: paletteRenameBranch, screens: []screen{screenBranches}},
		{title: "Delete branch", hint: "Branches · D", action: paletteDeleteBranch, screens: []screen{screenBranches}},
		{title: "Jump to ref or commit", hint: "resolves a name or hash", action: paletteJumpToRef},
		{title: "Filter history", hint: "History · /", action: paletteFilterHistory},
		{title: "Copy commit hash", hint: "History · y", action: paletteCopyHash, screens: []screen{screenHistory}},
		{title: "Open commit in inspector", hint: "History · Enter", action: paletteInspectCommit, screens: []screen{screenHistory}},
		{title: "Refresh", hint: "r", action: paletteRefresh},
		{title: "Change theme", hint: "t", action: paletteTheme},
		{title: "Follow the desktop theme", hint: "match-omarchy", action: paletteFollowOmarchy},
		{title: "Keyboard help", hint: "?", action: paletteHelp},
	}
}

func (m *Model) openPalette() tea.Cmd {
	m.palette = &paletteState{}
	m.palette.items = m.filterPalette("")
	return nil
}

// filterPalette matches on the command title, in order, allowing gaps so
// "cb" reaches "Create branch here".
func (m *Model) filterPalette(query string) []paletteItem {
	var out []paletteItem
	for _, item := range paletteCommands() {
		if !item.availableOn(m.screen) {
			continue
		}
		if subsequenceMatch(strings.ToLower(item.title), strings.ToLower(query)) {
			out = append(out, item)
		}
	}
	return out
}

func (i paletteItem) availableOn(s screen) bool {
	if len(i.screens) == 0 {
		return true
	}
	for _, allowed := range i.screens {
		if allowed == s {
			return true
		}
	}
	return false
}

// subsequenceMatch reports whether every rune of query appears in text in
// order. An empty query matches everything.
func subsequenceMatch(text, query string) bool {
	if query == "" {
		return true
	}
	runes := []rune(text)
	i := 0
	for _, want := range query {
		for i < len(runes) && runes[i] != want {
			i++
		}
		if i == len(runes) {
			return false
		}
		i++
	}
	return true
}

func (m *Model) updatePaletteKey(key string, msg tea.KeyMsg) tea.Cmd {
	p := m.palette
	switch key {
	case "esc", "ctrl+p":
		m.palette = nil
		return nil
	case "enter":
		if p.index < 0 || p.index >= len(p.items) {
			m.palette = nil
			return nil
		}
		action := p.items[p.index].action
		m.palette = nil
		return m.runPalette(action)
	case "down", "ctrl+n":
		p.index = min(p.index+1, max(0, len(p.items)-1))
		return nil
	case "up", "ctrl+k":
		p.index = max(0, p.index-1)
		return nil
	case "backspace":
		r := []rune(p.query)
		if len(r) > 0 {
			p.query = string(r[:len(r)-1])
		}
	default:
		if msg.Type == tea.KeyRunes {
			p.query += string(msg.Runes)
		} else {
			return nil
		}
	}
	p.items = m.filterPalette(p.query)
	p.index = min(p.index, max(0, len(p.items)-1))
	return nil
}

// runPalette dispatches to the same handlers the keys use.
func (m *Model) runPalette(action paletteAction) tea.Cmd {
	switch action {
	case paletteGoStatus:
		return m.goToScreen(screenStatus)
	case paletteGoHistory:
		return m.goToScreen(screenHistory)
	case paletteGoBranches:
		return m.goToScreen(screenBranches)
	case paletteRefresh:
		return m.refreshScreen()
	case paletteTheme:
		return m.openThemePicker()
	case paletteFollowOmarchy:
		return m.applyTheme(ThemeNameMatchOmarchy)
	case paletteHelp:
		m.help = true
		return nil
	case paletteCheckout:
		return m.switchToSelectedBranch()
	case paletteRenameBranch:
		return m.promptRenameBranch()
	case paletteDeleteBranch:
		return m.confirmDeleteBranch()
	case paletteCreateBranch:
		if m.screen == screenHistory && m.history != nil {
			return m.promptBranchFromSelection()
		}
		if m.branches == nil {
			// Creating from anywhere else starts at HEAD, which needs the
			// branch list loaded to show the result.
			cmd := m.goToScreen(screenBranches)
			m.prompt = &promptState{title: "new branch", label: "Create a branch starting at HEAD",
				help: "Enter creates · Ctrl-S creates and switches · Esc cancels",
				kind: promptCreateBranch, context: "HEAD"}
			return cmd
		}
		return m.promptNewBranch()
	case paletteJumpToRef:
		m.prompt = &promptState{title: "jump to ref or commit",
			label: "Branch, tag, or commit hash", help: "Enter jumps · Esc cancels",
			kind: promptJumpToRef}
		return nil
	case paletteFilterHistory:
		cmd := m.goToScreen(screenHistory)
		if m.history != nil {
			m.history.searching = true
			m.focus = 1
		}
		return cmd
	case paletteCopyHash:
		return m.copyCommitHash()
	case paletteInspectCommit:
		if m.screen == screenHistory {
			m.focus = 2
			return m.loadCommitFileDiff()
		}
		return nil
	}
	return nil
}

// jumpToRef resolves a name or hash and selects that commit in History,
// loading history from it when it is not on the current page.
func (m *Model) jumpToRef(query string) tea.Cmd {
	cmd := m.goToScreen(screenHistory)
	if m.history == nil {
		return cmd
	}
	oid, err := m.repo.ResolveRef(m.ctx, query)
	if err != nil {
		m.notice = err.Error()
		return cmd
	}
	for i, c := range m.history.commits {
		if c.OID == oid {
			m.history.selected = i
			m.focus = 1
			m.notice = "Jumped to " + c.Short
			return tea.Batch(cmd, m.scheduleDetail())
		}
	}
	// Not on the loaded page: walk from the resolved commit instead, which also
	// makes its ancestors available without paging to it.
	m.history.filters = append([]historyFilter{{Label: query, Rev: oid}}, m.history.filters...)
	m.history.filterIndex = 0
	m.notice = "Walking history from " + query
	return tea.Batch(cmd, m.loadHistory(false))
}

// palettePanel renders the command list.
func (m *Model) palettePanel(r tideui.Renderer) string {
	p := m.palette
	width := min(66, m.width-6)
	inner := max(1, width-4)
	rows := []string{r.Styles.DetailFocusLine.Width(inner).
		Render(clip(" › "+safeText(p.query)+"▎", inner)), ""}
	if len(p.items) == 0 {
		rows = append(rows, muted(r, "No matching command"))
	}
	for i, item := range p.items {
		if i >= 9 {
			rows = append(rows, muted(r, "…"))
			break
		}
		rows = append(rows, r.RenderSoftRow(tideui.SoftRow{
			Text: item.title, Suffix: item.hint, Selected: i == p.index}, inner))
	}
	rows = append(rows, "", muted(r, "↑ ↓ choose · Enter run · Esc close"))
	panel := r.SoftPanelOverlay(tideui.SoftPanel{Prefix: "tidegit", Title: "command palette",
		Width: width, Content: inset(strings.Join(rows, "\n"), width)})
	return panel.Content
}
