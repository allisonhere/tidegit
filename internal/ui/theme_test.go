package ui

import (
	"strings"
	"testing"

	"github.com/allisonhere/tidegit/internal/omarchy"
	"github.com/allisonhere/tideui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestResolveThemeNames(t *testing.T) {
	if _, ok := ResolveTheme("nord"); !ok {
		t.Fatal("a built-in theme did not resolve")
	}
	if _, ok := ResolveTheme("no-such-theme"); ok {
		t.Fatal("an unknown theme resolved")
	}
	// The Omarchy name always resolves: without Omarchy it falls back to a
	// known-good palette rather than failing the launch.
	theme, ok := ResolveTheme(ThemeNameMatchOmarchy)
	if !ok || theme.Name != ThemeNameMatchOmarchy {
		t.Fatalf("match-omarchy resolved to %+v (ok=%v)", theme.Name, ok)
	}
	for _, spelling := range []string{"MATCH-OMARCHY", " match-omarchy ", "Match-Omarchy"} {
		if !isMatchOmarchy(spelling) {
			t.Fatalf("%q was not recognised as the Omarchy theme", spelling)
		}
	}
	if isMatchOmarchy("nord") {
		t.Fatal("a built-in name matched the Omarchy theme")
	}
	names := ThemeNames()
	if len(names) != len(tideui.BuiltinThemes)+1 || names[len(names)-1] != ThemeNameMatchOmarchy {
		t.Fatalf("theme list: %v", names)
	}
}

// A desktop palette is chosen for a desktop. Whatever it contains, the mapped
// theme has to come out readable.
func TestOmarchyPaletteIsCorrectedForReadability(t *testing.T) {
	// A palette that would be unusable if taken at face value: foreground and
	// accent almost identical to the background, and a status bar equal to it.
	hostile := omarchy.Palette{
		Name: "hostile", Mode: "dark",
		Background: "#101014", Foreground: "#121216", Accent: "#111117",
		Selection: "#111116", Muted: "#101015", StatusBg: "#101014",
		Error: "#121218", Ok: "#101116",
	}
	raw := omarchyTheme(hostile)
	if tideui.ContrastRatio(raw.Fg, raw.Bg) > 2 {
		t.Fatal("the fixture is not actually hostile")
	}
	got := tideui.CorrectThemeContrast(raw)
	if r := tideui.ContrastRatio(got.Fg, got.Bg); r < 4.5 {
		t.Errorf("corrected foreground contrast %.2f", r)
	}
	if r := tideui.ContrastRatio(got.BorderFocus, got.Bg); r < 3 {
		t.Errorf("corrected accent contrast %.2f", r)
	}
	if tideui.ContrastRatio(got.StatusBar, got.Bg) < 1.2 {
		t.Error("a status bar identical to the page was not lifted")
	}
	if got.Bg != raw.Bg {
		t.Error("correction moved the background it corrects against")
	}

	// A palette missing optional fields still produces a complete theme.
	sparse := omarchyTheme(omarchy.Palette{Background: "#1e1e2e", Foreground: "#cdd6f4"})
	for name, c := range map[string]string{
		"Border": string(sparse.Border), "BorderFocus": string(sparse.BorderFocus),
		"Selected": string(sparse.Selected), "Unread": string(sparse.Unread),
		"Error": string(sparse.Error), "StatusBar": string(sparse.StatusBar),
	} {
		if c == "" {
			t.Errorf("%s was left empty by a sparse palette", name)
		}
	}
}

func TestThemePickerPreviewsAndConfirms(t *testing.T) {
	trueColorModel(t)
	m := historyModel(t)
	m.width, m.height = 132, 34
	start := m.theme.Name

	key(t, m, "t")
	if m.picker == nil {
		t.Fatal("t did not open the theme picker")
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "theme") || !strings.Contains(view, ThemeNameMatchOmarchy) {
		t.Fatalf("picker did not render its list: %s", view)
	}

	// Moving the cursor previews: the whole app renders in the highlighted
	// palette before anything is confirmed.
	control(t, m, tea.KeyDown)
	if m.activeTheme().Name == start {
		t.Fatal("moving the cursor did not preview another theme")
	}
	if m.theme.Name != start {
		t.Fatal("a preview changed the confirmed theme")
	}

	previewed := m.activeTheme().Name
	control(t, m, tea.KeyEnter)
	if m.picker != nil {
		t.Fatal("confirming left the picker open")
	}
	if m.theme.Name != previewed {
		t.Fatalf("confirmed %q, wanted %q", m.theme.Name, previewed)
	}
	if !strings.Contains(m.notice, previewed) {
		t.Fatalf("no confirmation feedback: %q", m.notice)
	}
}

func TestThemePickerCancelReverts(t *testing.T) {
	trueColorModel(t)
	m := historyModel(t)
	before := m.theme.Name
	key(t, m, "t")
	control(t, m, tea.KeyDown)
	control(t, m, tea.KeyDown)
	control(t, m, tea.KeyEsc)
	if m.picker != nil {
		t.Fatal("escape left the picker open")
	}
	if m.theme.Name != before {
		t.Fatalf("escape changed the theme to %q", m.theme.Name)
	}
	if m.activeTheme().Name != before {
		t.Fatal("the preview outlived the picker")
	}
}

// While the picker is open it owns the keyboard, so a theme name's first letter
// cannot also trigger a screen action.
func TestThemePickerOwnsInputWhileOpen(t *testing.T) {
	trueColorModel(t)
	m := historyModel(t)
	drain(t, m, m.goToScreen(screenBranches))
	branchCount := len(m.branches.local)
	key(t, m, "t")
	for _, k := range []string{"n", "D", "3", "1"} {
		key(t, m, k)
	}
	if m.prompt != nil || m.confirm != nil {
		t.Fatal("a screen action fired while the picker was open")
	}
	if m.screen != screenBranches {
		t.Fatal("a screen switch fired while the picker was open")
	}
	control(t, m, tea.KeyEsc)
	if len(m.branches.local) != branchCount {
		t.Fatal("branches changed while the picker was open")
	}
}

func TestFollowingOmarchyStartsAndStopsCleanly(t *testing.T) {
	m := historyModel(t)
	if cmd := m.followOmarchy(); cmd != nil {
		t.Fatal("a built-in theme started the desktop poll")
	}

	// applyTheme returns a self-re-arming timer command; draining it would run
	// the poll loop forever, so the command is inspected rather than executed.
	if cmd := m.applyTheme(ThemeNameMatchOmarchy); cmd == nil {
		t.Fatal("following the desktop returned no poll command")
	}
	if m.theme.Name != ThemeNameMatchOmarchy {
		t.Fatalf("theme is %q", m.theme.Name)
	}
	if !m.omarchyWatching {
		t.Fatal("following the desktop did not start the poll")
	}
	if !strings.Contains(m.notice, "desktop theme") {
		t.Fatalf("no feedback: %q", m.notice)
	}
	// The poll re-arms itself while the theme still follows the desktop.
	if cmd := m.handleOmarchyTick(); cmd == nil {
		t.Fatal("the poll did not re-arm")
	}
	// Starting again must not stack a second poll loop.
	if cmd := m.followOmarchy(); cmd != nil {
		t.Fatal("a second poll loop was started")
	}

	// Switching away stops it.
	m.applyTheme("nord")
	if m.theme.Name != "nord" {
		t.Fatalf("theme is %q", m.theme.Name)
	}
	if cmd := m.handleOmarchyTick(); cmd != nil {
		t.Fatal("the poll kept running after the theme changed")
	}
	if m.omarchyWatching {
		t.Fatal("the poll did not record that it stopped")
	}
	// An unknown name is refused without disturbing the active theme.
	m.applyTheme("no-such-theme")
	if m.theme.Name != "nord" || !strings.Contains(m.notice, "Unknown theme") {
		t.Fatalf("unknown theme changed state: %q / %q", m.theme.Name, m.notice)
	}
}

// Every selectable theme has to render every screen without holes or overflow.
func TestEveryThemeRendersCleanly(t *testing.T) {
	trueColorModel(t)
	m := historyModel(t)
	m.width, m.height = 120, 30
	// Load each screen's data once; the loop after this is about rendering, and
	// re-reading the repository per theme would make it needlessly slow.
	screens := []screen{screenStatus, screenHistory, screenBranches}
	for _, s := range screens {
		drain(t, m, m.goToScreen(s))
	}
	for _, theme := range pickableThemes() {
		resolved, ok := ResolveTheme(theme.Name)
		if !ok {
			t.Fatalf("%s did not resolve", theme.Name)
		}
		m.theme = resolved
		for _, s := range screens {
			m.screen = s
			view := m.View()
			for i, line := range strings.Split(view, "\n") {
				if w := lipgloss.Width(line); w != 120 {
					t.Fatalf("%s screen %d line %d is %d cells", theme.Name, s, i, w)
				}
				if backgroundHole.MatchString(line) {
					t.Fatalf("%s screen %d line %d has a background hole", theme.Name, s, i)
				}
			}
		}
	}
}
