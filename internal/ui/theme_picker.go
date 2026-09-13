package ui

import (
	"github.com/allisonhere/tideui"
	tea "github.com/charmbracelet/bubbletea"
)

// The theme picker is TideUI's own component. TideGit owns only what TideUI
// leaves to the host: when it opens, what the preview renders with, and what
// happens to the confirmed choice.

// openThemePicker starts a preview session from the active theme.
func (m *Model) openThemePicker() tea.Cmd {
	picker := tideui.NewThemePicker(tideui.ThemePickerOptions{
		Themes:       pickableThemes(),
		InitialTheme: m.theme.Name,
		Title:        "theme",
	})
	picker.Open(m.theme.Name)
	m.picker = &picker
	return nil
}

// activeTheme is the theme the view renders with: the previewed one while the
// picker is open, so moving the cursor shows the whole app in that palette.
func (m *Model) activeTheme() tideui.Theme {
	if m.picker == nil || !m.picker.Opened() {
		return m.theme
	}
	preview := m.picker.PreviewTheme()
	// A preview of the Omarchy row should show the desktop's actual colours,
	// not the placeholder the picker lists it under.
	if isMatchOmarchy(preview.Name) {
		if live, ok := resolveOmarchyTheme(); ok {
			return live
		}
	}
	return preview
}

// updateThemePickerKey routes a keystroke to the picker and acts on the result.
func (m *Model) updateThemePickerKey(msg tea.KeyMsg) tea.Cmd {
	switch m.picker.Update(msg) {
	case tideui.ThemePickerConfirm:
		chosen := m.picker.ConfirmedTheme()
		m.picker = nil
		return m.applyTheme(chosen.Name)
	case tideui.ThemePickerCancel:
		m.picker = nil
		m.notice = "Kept " + safeText(m.theme.Name)
		return nil
	}
	return nil
}

// applyTheme switches to a theme by name, resolving the Omarchy-following one
// against the live desktop palette and starting the poll that keeps it current.
func (m *Model) applyTheme(name string) tea.Cmd {
	theme, ok := ResolveTheme(name)
	if !ok {
		m.notice = "Unknown theme " + safeText(name)
		return nil
	}
	m.theme = theme
	if isMatchOmarchy(name) {
		m.notice = "Following the desktop theme" + omarchyNameSuffix()
		return m.followOmarchy()
	}
	m.notice = "Theme " + safeText(theme.Name)
	return nil
}

// themePickerPanel renders the picker over the current screen.
func (m *Model) themePickerPanel(r tideui.Renderer) string {
	width := min(44, max(24, m.width-8))
	return m.picker.SoftModal(r, width, max(8, m.height-6), "tidegit").Content
}
