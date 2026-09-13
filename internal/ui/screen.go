package ui

import (
	"github.com/allisonhere/tideui"
	tea "github.com/charmbracelet/bubbletea"
)

// screen selects which of TideGit's top-level views is on screen. Each screen
// owns its own panes, keys and footer hints; the shell, theme and Git service
// are shared.
type screen int

const (
	screenStatus screen = iota
	screenHistory
	screenBranches
	screenStash
	screenRemotes
	screenConflicts
	screenReflog
	screenSettings
)

var screenNames = map[screen]string{
	screenStatus:    "STATUS",
	screenHistory:   "HISTORY",
	screenBranches:  "BRANCHES",
	screenStash:     "STASH",
	screenRemotes:   "REMOTES",
	screenConflicts: "CONFLICTS",
	screenReflog:    "REFLOG",
	screenSettings:  "SETTINGS",
}

// screenDefaultName maps a screen to the identifier layout.default_screen
// accepts.
func screenDefaultName(s screen) string {
	switch s {
	case screenHistory:
		return "history"
	case screenBranches:
		return "branches"
	case screenStash:
		return "stash"
	case screenRemotes:
		return "remotes"
	case screenConflicts:
		return "conflicts"
	case screenReflog:
		return "reflog"
	case screenSettings:
		return "settings"
	default:
		return "status"
	}
}

// screenByName is the inverse, for the configured startup screen.
func screenByName(name string) screen {
	for s := screenStatus; s <= screenSettings; s++ {
		if screenDefaultName(s) == name {
			return s
		}
	}
	return screenStatus
}

// handleKey routes one keystroke: global gates first, then whichever overlay is
// open, then the active screen.
func (m *Model) handleKey(msg tea.KeyMsg) tea.Cmd {
	key := msg.String()
	if key == "ctrl+c" {
		return tea.Quit
	}
	if (m.width < 54 || m.height < 16) && key != "q" {
		return nil
	}
	if m.picker != nil {
		return m.updateThemePickerKey(msg)
	}
	if m.palette != nil {
		return m.updatePaletteKey(key, msg)
	}
	if m.prompt != nil {
		return m.updatePromptKey(key, msg)
	}
	if m.choice != nil {
		return m.updateChoiceKey(key)
	}
	if m.confirm != nil {
		return m.updateConfirmKey(key)
	}
	if m.op != nil && m.op.show {
		return m.updateOperationKey(key)
	}
	if m.help {
		if key == "esc" || key == "q" || key == "?" {
			m.help = false
		}
		return nil
	}
	// While capturing a keybinding, the next key is the binding, not a command.
	if m.screen == screenSettings && m.settings != nil && m.settings.effective {
		return m.updateSettingsKey(key, msg)
	}
	if m.screen == screenSettings && m.settings != nil && m.settings.capture {
		return m.captureKey(key)
	}
	// A running operation keeps its own controls live even though the rest of
	// the screen is gated: o watches details, Esc cancels where it is safe.
	if m.operationRunning() {
		switch key {
		case "o":
			m.op.show = true
			return nil
		case "esc":
			m.cancelOperation()
			return nil
		}
	}
	// Keep the visible target stable until a mutation and its scan finish.
	// Focus, expansion, help and quit remain responsive.
	if (m.busy || m.opening) && key != "tab" && key != "shift+tab" && key != "z" && key != "?" && key != "q" {
		return nil
	}
	if cmd, handled := m.globalKey(key); handled {
		return cmd
	}
	switch m.screen {
	case screenHistory:
		return m.updateHistoryKey(key, msg)
	case screenBranches:
		return m.updateBranchKey(key, msg)
	case screenStash:
		return m.updateStashKey(key, msg)
	case screenRemotes:
		return m.updateRemoteKey(key, msg)
	case screenConflicts:
		return m.updateConflictKey(key, msg)
	case screenReflog:
		return m.updateReflogKey(key, msg)
	case screenSettings:
		return m.updateSettingsKey(key, msg)
	default:
		return m.updateStatusKey(key, msg)
	}
}

// globalKey handles the bindings every screen shares, resolved through the
// customizable keymap. Screens that are typing opt out so a letter is not
// swallowed as a command.
func (m *Model) globalKey(key string) (tea.Cmd, bool) {
	if m.typing() {
		return nil, false
	}
	// A few screens own a key that also has a global default; their local
	// meaning wins on that screen.
	if m.screen == screenStash && key == "p" {
		return nil, false
	}
	if m.screen == screenConflicts && (key == "o" || key == "t") {
		return nil, false
	}
	if id, ok := m.actionForKey(key); ok && m.screenAllowsAction(id) {
		return m.runKeyAction(id), true
	}
	return nil, false
}

// paneLayout picks how the three panes are arranged for the current width.
// Wide terminals get three columns; medium ones keep the sidebar and stack the
// other two rather than squeezing three narrow columns; small ones tab.
func (m *Model) paneLayout() (tideui.LayoutMode, [3]int) {
	switch {
	case m.expanded || m.width < 70:
		return tideui.Tabbed, [3]int{m.width, m.width, m.width}
	case m.width < 92:
		side := int(float64(m.width) * 0.28)
		return tideui.StackedRight, [3]int{side - 2, m.width - side - 2, m.width - side - 2}
	default:
		return tideui.ThreeColumn, [3]int{}
	}
}

// typing reports whether a screen is currently collecting text.
func (m *Model) typing() bool {
	if m.filtering {
		return true
	}
	if m.history != nil && m.history.searching && m.screen == screenHistory {
		return true
	}
	if m.branches != nil && m.branches.filtering && m.screen == screenBranches {
		return true
	}
	if m.stash != nil && m.stash.filtering && m.screen == screenStash {
		return true
	}
	if m.remotes != nil && m.remotes.filtering && m.screen == screenRemotes {
		return true
	}
	if m.conflicts != nil && m.conflicts.filtering && m.screen == screenConflicts {
		return true
	}
	if m.reflog != nil && m.reflog.filtering && m.screen == screenReflog {
		return true
	}
	if m.settings != nil && m.settings.searching && m.screen == screenSettings {
		return true
	}
	return false
}

// goToScreen switches views, loading the target's data the first time.
func (m *Model) goToScreen(s screen) tea.Cmd {
	if m.screen == s {
		return nil
	}
	m.expanded = false
	m.notice = ""
	switch s {
	case screenHistory:
		return m.openHistory()
	case screenBranches:
		return m.openBranches()
	case screenStash:
		return m.openStash()
	case screenRemotes:
		return m.openRemotes()
	case screenConflicts:
		return m.openConflicts()
	case screenReflog:
		return m.openReflog()
	case screenSettings:
		return m.openSettings()
	default:
		m.screen = screenStatus
		m.focus = min(m.focus, 2)
		// A branch switch made elsewhere changes the working tree, so the
		// Status screen always re-reads rather than trusting its last scan.
		return m.refresh()
	}
}

// refreshScreen re-reads whatever the active screen shows.
func (m *Model) refreshScreen() tea.Cmd {
	switch m.screen {
	case screenHistory:
		m.history.cache, m.history.order = nil, nil
		return m.loadHistory(false)
	case screenBranches:
		return m.loadBranches()
	case screenStash:
		return m.loadStashes()
	case screenRemotes:
		return m.loadRemotes()
	case screenConflicts:
		return m.reloadConflicts()
	case screenReflog:
		return m.loadReflog()
	case screenSettings:
		return m.reloadSettings()
	default:
		return m.refresh()
	}
}

// updateStatusKey handles the Status screen's own bindings.
func (m *Model) updateStatusKey(key string, msg tea.KeyMsg) tea.Cmd {
	if m.filtering {
		switch key {
		case "enter":
			m.filtering = false
			return nil
		case "esc":
			m.filtering = false
			m.query = ""
		case "backspace":
			r := []rune(m.query)
			if len(r) > 0 {
				m.query = string(r[:len(r)-1])
			}
		default:
			if msg.Type == tea.KeyRunes {
				m.query += string(msg.Runes)
			}
		}
		m.selected = 0
		return m.loadDiff()
	}
	switch key {
	case " ":
		m.toggleMark()
	case "S":
		return m.act(StageHunk)
	case "U":
		return m.act(UnstageHunk)
	case "]":
		m.moveHunk(1)
	case "[":
		m.moveHunk(-1)
	case "v":
		return m.toggleDiffMode()
	case "n":
		m.view.stepMatch(1)
		m.view.ensureVisible(m.diffViewportHeight())
		return nil
	case "N":
		m.view.stepMatch(-1)
		m.view.ensureVisible(m.diffViewportHeight())
		return nil
	case "ctrl+f":
		return m.startDiffSearch()
	case "y":
		return m.copyDiffLine()
	case "e":
		if m.focus == 2 {
			return m.openDiffInEditor()
		}
		return nil
	case "q":
		if m.expanded {
			m.expanded = false
		} else {
			return tea.Quit
		}
	case "esc":
		m.expanded = false
		m.query = ""
		m.err = ""
		return m.loadDiff()
	case "?":
		m.help = true
	case "tab":
		m.focus = (m.focus + 1) % 3
	case "shift+tab":
		m.focus = (m.focus + 2) % 3
	case "enter":
		switch {
		case m.focus == 2:
			m.view.toggleGap(m.diffOptionsFrom())
		case m.focus < 2:
			m.focus++
		}
	case "z":
		m.expanded = !m.expanded
	case "h", "left":
		if m.focus == 2 {
			m.horizontal = max(0, m.horizontal-8)
		}
	case "l", "right":
		if m.focus == 2 {
			m.horizontal = min(m.horizontal+8, outputWidth(m.lines))
		}
	case "/":
		m.focus = 1
		m.filtering = true
	case "r", "ctrl+r":
		return m.refresh()
	case "j", "down", "k", "up", "pgdown", "pgup", "home", "end":
		delta := 1
		if key == "k" || key == "up" || key == "pgup" {
			delta = -1
		}
		if key == "pgdown" || key == "pgup" {
			delta *= max(1, m.height-5)
		}
		if m.focus == 2 {
			flat := m.view.flat(m.diffOptionsFrom(), true)
			switch key {
			case "home":
				m.view.line = 0
			case "end":
				m.view.line = max(0, len(flat)-1)
			default:
				m.view.line = min(max(0, m.view.line+delta), max(0, len(flat)-1))
			}
			m.view.ensureVisible(m.diffViewportHeight())
		} else if m.focus == 0 {
			n := min(3, max(0, m.section+delta))
			if key == "home" {
				n = 0
			}
			if key == "end" {
				n = 3
			}
			if n != m.section {
				m.section = n
				m.selected = 0
				return m.loadDiff()
			}
		} else {
			n := min(max(0, len(m.files())-1), max(0, m.selected+delta))
			if key == "home" {
				n = 0
			}
			if key == "end" {
				n = max(0, len(m.files())-1)
			}
			if n != m.selected {
				m.selected = n
				return m.loadDiff()
			}
		}
	}
	return nil
}
