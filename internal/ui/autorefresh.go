package ui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// autoRefreshMsg drives the optional periodic status rescan.
type autoRefreshMsg struct{}

const autoRefreshInterval = 5 * time.Second

// handleAutoRefresh re-reads the Status screen when idle, then re-arms. It is
// deliberately narrow: it only ever refreshes the working-tree snapshot, and
// only while the Status screen is in front.
func (m *Model) handleAutoRefresh() tea.Cmd {
	if m.cfg == nil || !m.cfg.Behavior.AutoRefresh {
		return nil
	}
	var cmd tea.Cmd
	if m.screen == screenStatus && !m.busy && !m.loading && !m.diffLoading && !m.opening {
		cmd = m.refresh()
	}
	return tea.Batch(cmd, m.armAutoRefresh())
}
