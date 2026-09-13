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
)

var screenNames = map[screen]string{
	screenStatus:   "STATUS",
	screenHistory:  "HISTORY",
	screenBranches: "BRANCHES",
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
	if m.confirm != nil {
		return m.updateConfirmKey(key)
	}
	if m.help {
		if key == "esc" || key == "q" || key == "?" {
			m.help = false
		}
		return nil
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
	default:
		return m.updateStatusKey(key, msg)
	}
}

// globalKey handles the bindings every screen shares. Screens that are typing
// into a field opt out so a letter is not swallowed as a command.
func (m *Model) globalKey(key string) (tea.Cmd, bool) {
	if m.typing() {
		return nil, false
	}
	switch key {
	case "ctrl+p":
		return m.openPalette(), true
	case "t":
		return m.openThemePicker(), true
	case "1":
		return m.goToScreen(screenStatus), true
	case "2":
		return m.goToScreen(screenHistory), true
	case "3":
		return m.goToScreen(screenBranches), true
	case "tab":
		m.focus = (m.focus + 1) % 3
		return nil, true
	case "shift+tab":
		m.focus = (m.focus + 2) % 3
		return nil, true
	case "z":
		m.expanded = !m.expanded
		return nil, true
	case "?":
		m.help = true
		return nil, true
	case "r", "ctrl+r":
		return m.refreshScreen(), true
	case "q":
		switch {
		case m.expanded:
			m.expanded = false
		case m.screen != screenStatus:
			return m.goToScreen(screenStatus), true
		default:
			return tea.Quit, true
		}
		return nil, true
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
	case "c":
		return m.openCommit(false)
	case "A":
		return m.openCommit(true)
	case "s":
		return m.act(StageFile)
	case "u":
		return m.act(UnstageFile)
	case "S":
		return m.act(StageHunk)
	case "U":
		return m.act(UnstageHunk)
	case "]":
		m.moveHunk(1)
	case "[":
		m.moveHunk(-1)
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
		if m.focus < 2 {
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
			if key == "home" {
				m.scroll.ScrollToTop()
			} else if key == "end" {
				m.scroll.ScrollDown(len(m.lines))
			} else if delta < 0 {
				m.scroll.ScrollUp(-delta)
			} else {
				m.scroll.ScrollDown(delta)
			}
			m.scroll.ClampTo(len(m.lines), m.diffViewportHeight())
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
