package ui

import (
	"strings"
	"time"

	"github.com/allisonhere/tidegit/internal/omarchy"
	"github.com/allisonhere/tideui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ThemeNameMatchOmarchy is the theme that follows the desktop's current Omarchy
// theme instead of carrying its own palette. It is not one of TideUI's built-in
// themes — its colours are resolved at runtime — but it is offered alongside
// them, and it is what `-theme match-omarchy` selects.
const ThemeNameMatchOmarchy = "match-omarchy"

// omarchyPlaceholder is what the picker lists and what the app falls back to
// while the live palette is unavailable: a known-good built-in wearing the
// Omarchy name.
var omarchyPlaceholder = func() tideui.Theme {
	t := tideui.CatppuccinMocha
	t.Name = ThemeNameMatchOmarchy
	return t
}()

// isMatchOmarchy reports whether name selects the Omarchy-following theme.
func isMatchOmarchy(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), ThemeNameMatchOmarchy)
}

// pickableThemes is every theme the picker offers: TideUI's built-ins plus the
// Omarchy-following one.
func pickableThemes() []tideui.Theme {
	out := make([]tideui.Theme, 0, len(tideui.BuiltinThemes)+1)
	out = append(out, tideui.BuiltinThemes...)
	return append(out, omarchyPlaceholder)
}

// ThemeNames lists every selectable theme, for a usage message.
func ThemeNames() []string {
	themes := pickableThemes()
	names := make([]string, len(themes))
	for i, t := range themes {
		names[i] = t.Name
	}
	return names
}

// ResolveTheme turns a theme name into a theme, resolving the Omarchy-following
// one against the live desktop palette. ok is false for an unknown name, so the
// caller can reject it rather than silently substituting something.
func ResolveTheme(name string) (tideui.Theme, bool) {
	if isMatchOmarchy(name) {
		if theme, ok := resolveOmarchyTheme(); ok {
			return theme, true
		}
		// Omarchy is not installed or its state is unreadable. The name is
		// still valid; it just has nothing to follow yet.
		return omarchyPlaceholder, true
	}
	return tideui.ThemeByName(name)
}

// omarchyTheme maps a raw Omarchy palette onto a TideUI theme. The result is
// not readable until it has been through contrast correction: a desktop palette
// is chosen for a desktop, not for a dense terminal UI.
func omarchyTheme(p omarchy.Palette) tideui.Theme {
	color := func(s string) lipgloss.Color { return lipgloss.Color(s) }
	bg, fg := color(p.Background), color(p.Foreground)

	border := color(p.Muted)
	if p.Muted == "" {
		border = tideui.MixColors(fg, bg, 0.5)
	}
	accent := color(p.Accent)
	if p.Accent == "" {
		accent = fg
	}
	statusBg := color(p.StatusBg)
	if p.StatusBg == "" {
		// Lift a surface off the page in whichever direction the page is not.
		delta := 0.05
		if !tideui.IsDark(bg) {
			delta = -delta
		}
		statusBg = tideui.AdjustLightness(bg, delta)
	}
	unread := color(p.Ok)
	if p.Ok == "" {
		unread = accent
	}
	failure := color(p.Error)
	if p.Error == "" {
		failure = accent
	}
	selected := color(p.Selection)
	if p.Selection == "" {
		selected = accent
	}

	return tideui.Theme{
		Name:          ThemeNameMatchOmarchy,
		Bg:            bg,
		Fg:            fg,
		Border:        border,
		BorderFocus:   accent,
		Selected:      selected,
		Unread:        unread,
		Dimmed:        border,
		StatusBar:     statusBg,
		StatusFg:      fg,
		Error:         failure,
		Overlay:       statusBg,
		OverlayBorder: accent,
	}
}

// resolveOmarchyTheme reads the live palette and corrects it for readability.
// ok is false when Omarchy is not present.
func resolveOmarchyTheme() (tideui.Theme, bool) {
	p, ok := omarchy.CurrentPalette()
	if !ok {
		return tideui.Theme{}, false
	}
	return tideui.CorrectThemeContrast(omarchyTheme(p)), true
}

// omarchyThemeTickMsg drives the poll that keeps the app in step with the
// desktop while match-omarchy is the active theme.
type omarchyThemeTickMsg struct{}

const omarchyWatchInterval = 2 * time.Second

func omarchyWatchCmd() tea.Cmd {
	return tea.Tick(omarchyWatchInterval, func(time.Time) tea.Msg { return omarchyThemeTickMsg{} })
}

// followOmarchy starts the poll if the active theme follows Omarchy and no poll
// is already running. Polling is cheap: it stats one file and compares a token.
func (m *Model) followOmarchy() tea.Cmd {
	if !isMatchOmarchy(m.theme.Name) {
		return nil
	}
	m.omarchySignature = omarchy.CurrentSignature()
	if m.omarchyWatching {
		return nil
	}
	m.omarchyWatching = true
	return omarchyWatchCmd()
}

// handleOmarchyTick re-resolves the palette when the desktop theme has changed
// and re-arms itself, stopping once the active theme is something else.
func (m *Model) handleOmarchyTick() tea.Cmd {
	if !isMatchOmarchy(m.theme.Name) {
		m.omarchyWatching = false
		return nil
	}
	m.omarchyWatching = true
	signature := omarchy.CurrentSignature()
	if signature == m.omarchySignature {
		return omarchyWatchCmd()
	}
	m.omarchySignature = signature
	if theme, ok := resolveOmarchyTheme(); ok {
		m.theme = theme
		m.notice = "Followed the desktop theme" + omarchyNameSuffix()
	}
	return omarchyWatchCmd()
}

// omarchyNameSuffix names the desktop theme when it can be read, for feedback.
func omarchyNameSuffix() string {
	if p, ok := omarchy.CurrentPalette(); ok && p.Name != "" {
		return " · " + safeText(p.Name)
	}
	return ""
}
