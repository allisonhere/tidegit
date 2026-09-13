package ui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/allisonhere/tidegit/internal/config"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func settingsModel(t *testing.T) (*Model, *config.Store) {
	t.Helper()
	dir := t.TempDir()
	store := config.Open(filepath.Join(dir, "config.toml"), filepath.Join(dir, "overrides.toml"))
	m := NewConfigured(context.Background(), ".", store, config.DefaultState(), "")
	m.width, m.height = 132, 34
	drain(t, m, m.goToScreen(screenSettings))
	return m, store
}

// selectSetting moves the settings cursor to a row by its dotted path.
func selectSetting(t *testing.T, m *Model, path string) {
	t.Helper()
	m.settings.query = ""
	m.settings.searching = false
	for i := range m.settings.categories {
		m.settings.catIndex = i
		for j, row := range m.settingsRows() {
			if row.path == path {
				m.settings.index = j
				return
			}
		}
	}
	t.Fatalf("setting %q not found", path)
}

func TestSettingsScreenRendersAndToggles(t *testing.T) {
	m, store := settingsModel(t)
	view := ansi.Strip(m.View())
	for _, want := range []string{"SETTINGS", "Appearance", "Layout", "Diff", "Editor", "Behavior", "Git", "Remote", "Keybindings"} {
		if !strings.Contains(view, want) {
			t.Fatalf("settings view missing %q", want)
		}
	}

	selectSetting(t, m, "appearance.compact")
	before := m.cfg.Appearance.Compact
	drain(t, m, m.activateSetting())
	if m.cfg.Appearance.Compact == before {
		t.Fatal("toggle did not change the value")
	}
	if !store.IsOverridden("appearance.compact") {
		t.Fatal("toggle was not persisted")
	}
	view = ansi.Strip(m.View())
	if !strings.Contains(view, "•") || !strings.Contains(view, "appearance.compact") {
		t.Fatalf("changed indicator missing: %s", view)
	}
	if !strings.Contains(ansi.Strip(m.View()), "modified") {
		t.Fatal("inspector does not mark the setting modified")
	}

	// Reset returns it to the default and clears the override.
	drain(t, m, m.resetSetting("appearance.compact"))
	if m.cfg.Appearance.Compact != before || store.IsOverridden("appearance.compact") {
		t.Fatal("reset did not restore the default")
	}
}

func TestSettingsSearch(t *testing.T) {
	m, _ := settingsModel(t)
	key(t, m, "/")
	for _, r := range "mouse" {
		key(t, m, string(r))
	}
	if m.settings.query != "mouse" {
		t.Fatalf("query: %q", m.settings.query)
	}
	rows := m.settingsRows()
	if len(rows) == 0 || rows[0].path != "behavior.mouse" {
		t.Fatalf("search results: %+v", rows)
	}
	key(t, m, "esc")
	if m.settings.query != "" {
		t.Fatal("escape did not clear the search")
	}
}

func TestSettingsEnumAndIntControls(t *testing.T) {
	m, _ := settingsModel(t)
	selectSetting(t, m, "diff.context_lines")
	start := m.cfg.Diff.ContextLines
	drain(t, m, m.adjustSetting(1))
	if m.cfg.Diff.ContextLines != start+1 {
		t.Fatalf("int adjust: %d -> %d", start, m.cfg.Diff.ContextLines)
	}
	drain(t, m, m.adjustSetting(-1))
	if m.cfg.Diff.ContextLines != start {
		t.Fatalf("int adjust back: %d", m.cfg.Diff.ContextLines)
	}

	selectSetting(t, m, "appearance.border_style")
	drain(t, m, m.adjustSetting(1))
	if m.cfg.Appearance.BorderStyle != "square" {
		t.Fatalf("enum cycle: %q", m.cfg.Appearance.BorderStyle)
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, "square") {
		t.Fatalf("enum value not shown: %s", view)
	}
}

func TestSettingsResetAll(t *testing.T) {
	m, store := settingsModel(t)
	selectSetting(t, m, "git.show_tags")
	drain(t, m, m.activateSetting())
	if !store.IsOverridden("git.show_tags") || m.cfg.Git.ShowTags {
		t.Fatalf("expected show_tags off and overridden: %+v", m.cfg.Git)
	}
	drain(t, m, m.confirmResetSettings())
	if m.confirm == nil {
		t.Fatal("reset all did not confirm")
	}
	key(t, m, "X")
	if len(store.Overrides()) != 0 {
		t.Fatalf("reset all left overrides: %+v", store.Overrides())
	}
	if !m.cfg.Git.ShowTags {
		t.Fatal("reset all did not restore the default")
	}
}

func TestKeybindingOverrideResolution(t *testing.T) {
	cfg := config.Default()
	if key, ok := actionForTest(cfg, "git.commit"); !ok || key != "c" {
		t.Fatalf("default binding: %q", key)
	}
	cfg.Keybindings["git.commit"] = "C"
	if key, _ := actionForTest(cfg, "git.commit"); key != "C" {
		t.Fatalf("override: %q", key)
	}
	// A conflict is reported, not hidden.
	cfg.Keybindings["app.help"] = "q"
	_, issues := resolveKeymap(cfg)
	joined := strings.Join(issues, " ")
	if !strings.Contains(joined, "q") || !strings.Contains(joined, "app.quit") {
		t.Fatalf("conflict not reported: %v", issues)
	}
}

func actionForTest(cfg *config.Config, id string) (string, bool) {
	byID, _ := resolveKeymap(cfg)
	key, ok := byID[id]
	return key, ok
}

func TestKeybindingCaptureFlow(t *testing.T) {
	m, store := settingsModel(t)
	// Open the Keybindings category and select app.theme.
	for i, category := range m.settings.categories {
		if category == "Keybindings" {
			m.settings.catIndex = i
		}
	}
	for j, row := range m.settingsRows() {
		if row.actionID == "app.theme" {
			m.settings.index = j
		}
	}
	drain(t, m, m.activateSetting())
	if !m.settings.capture {
		t.Fatal("did not enter key capture")
	}
	key(t, m, "T")
	if m.keys["app.theme"] != "T" {
		t.Fatalf("binding not captured: %q", m.keys["app.theme"])
	}
	if !store.IsOverridden("keybindings.app.theme") {
		t.Fatal("captured binding not persisted")
	}
	// The new key now triggers the action.
	if id, ok := m.actionForKey("T"); !ok || id != "app.theme" {
		t.Fatalf("actionForKey: %q %v", id, ok)
	}
}

func TestReloadAndEffectiveConfig(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "config.toml")
	overrides := filepath.Join(dir, "overrides.toml")
	if err := os.WriteFile(primary, []byte("version = 1\n[appearance]\ntheme = \"nord\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := config.Open(primary, overrides)
	m := NewConfigured(context.Background(), ".", store, config.DefaultState(), "")
	m.width, m.height = 132, 34
	drain(t, m, m.goToScreen(screenSettings))
	if m.theme.Name != "nord" {
		t.Fatalf("configured theme: %q", m.theme.Name)
	}
	// A manual edit is picked up by an explicit reload.
	if err := os.WriteFile(primary, []byte("version = 1\n[appearance]\ntheme = \"dracula\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	drain(t, m, m.reloadConfiguration())
	if m.theme.Name != "dracula" {
		t.Fatalf("reload did not apply the edit: %q", m.theme.Name)
	}
	// The effective-config overlay renders the resolved values.
	drain(t, m, m.showEffectiveConfig())
	if !m.settings.effective {
		t.Fatal("effective view did not open")
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "effective configuration") || !strings.Contains(view, "appearance.theme") {
		t.Fatalf("effective view: %s", view)
	}
	key(t, m, "V")
	if m.settings.effective {
		t.Fatal("effective view did not close")
	}
}

func TestInvalidConfigKeepsAppRunning(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "config.toml")
	overrides := filepath.Join(dir, "overrides.toml")
	if err := os.WriteFile(primary, []byte("version = 1\n[appearance\ntheme = "), 0o600); err != nil {
		t.Fatal(err)
	}
	store := config.Open(primary, overrides)
	m := NewConfigured(context.Background(), ".", store, config.DefaultState(), "")
	m.width, m.height = 132, 34
	if m.cfg.Appearance.Theme != "catppuccin-mocha" {
		t.Fatalf("invalid config should fall back to defaults: %+v", m.cfg.Appearance)
	}
	// Fixing the file and reloading recovers.
	if err := os.WriteFile(primary, []byte("version = 1\n[appearance]\ntheme = \"nord\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	drain(t, m, m.reloadConfiguration())
	if m.theme.Name != "nord" {
		t.Fatalf("recovery reload failed: %q", m.theme.Name)
	}
}

func TestSettingsRenderAtEverySize(t *testing.T) {
	m, _ := settingsModel(t)
	for _, size := range [][2]int{{160, 40}, {132, 34}, {100, 28}, {80, 24}, {60, 20}, {54, 16}, {40, 12}, {1, 1}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := m.View()
		if w, h := lipgloss.Width(view), lipgloss.Height(view); w > size[0] || h > size[1] {
			t.Fatalf("settings overflow at %v: %dx%d", size, w, h)
		}
	}
}
