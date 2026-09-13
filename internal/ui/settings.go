package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/allisonhere/tidegit/internal/config"
	"github.com/allisonhere/tidegit/internal/git"
	"github.com/allisonhere/tideui"
	tea "github.com/charmbracelet/bubbletea"
)

// settingRow is one editable line: either a fixed setting from the catalog or
// an action binding.
type settingRow struct {
	path        string
	title       string
	category    string
	kind        config.Kind
	enum        []string
	min, max    int
	restart     bool
	description string
	keybinding  bool
	actionID    string
}

func rowFromSetting(s config.Setting) settingRow {
	return settingRow{
		path: s.Path, title: s.Title, category: s.Category, kind: s.Kind,
		enum: s.Enum, min: s.Min, max: s.Max, restart: s.Restart, description: s.Description,
	}
}

func keybindingRow(a keyAction) settingRow {
	return settingRow{
		path: "keybindings." + a.ID, title: a.ID, category: "Keybindings",
		kind: config.KindText, keybinding: true, actionID: a.ID,
		description: a.Description,
	}
}

// settingsState backs the Settings screen.
type settingsState struct {
	categories []string
	catIndex   int
	index      int
	top        int

	searching bool
	query     string

	capture bool // the next key becomes the selected binding
	inspect tideui.PaneScroller

	effective       bool
	effectiveScroll tideui.PaneScroller
}

func (m *Model) openSettings() tea.Cmd {
	if m.settings == nil {
		m.settings = &settingsState{}
	}
	m.screen = screenSettings
	m.focus = 0
	m.notice = ""
	m.settings.categories = append(append([]string{}, config.Categories()...), "Keybindings")
	m.settings.catIndex = min(m.settings.catIndex, len(m.settings.categories)-1)
	m.settings.index = 0
	m.settings.capture = false
	m.settings.inspect.ScrollToTop()
	return nil
}

// reloadSettings re-reads the catalog after a config or keybinding change.
func (m *Model) reloadSettings() tea.Cmd {
	if m.settings == nil {
		return nil
	}
	m.settings.categories = append(append([]string{}, config.Categories()...), "Keybindings")
	m.settings.catIndex = min(m.settings.catIndex, len(m.settings.categories)-1)
	rows := m.settingsRows()
	m.settings.index = min(m.settings.index, max(0, len(rows)-1))
	return nil
}

// settingsRows is the middle pane's content for the active category, or the
// cross-category search results when a query is present.
func (m *Model) settingsRows() []settingRow {
	s := m.settings
	if s == nil {
		return nil
	}
	if s.query != "" {
		q := strings.ToLower(s.query)
		var rows []settingRow
		for _, set := range config.Settings() {
			haystack := strings.ToLower(set.Path + " " + set.Title + " " + set.Category + " " + set.Description)
			if strings.Contains(haystack, q) {
				rows = append(rows, rowFromSetting(set))
			}
		}
		for _, a := range keyActions() {
			if strings.Contains(strings.ToLower(a.ID+" "+a.Description), q) {
				rows = append(rows, keybindingRow(a))
			}
		}
		return rows
	}
	if s.catIndex < 0 || s.catIndex >= len(s.categories) {
		return nil
	}
	category := s.categories[s.catIndex]
	if category == "Keybindings" {
		rows := make([]settingRow, 0, len(keyActions()))
		for _, a := range keyActions() {
			rows = append(rows, keybindingRow(a))
		}
		return rows
	}
	var rows []settingRow
	for _, set := range config.Settings() {
		if set.Category == category {
			rows = append(rows, rowFromSetting(set))
		}
	}
	return rows
}

func (m *Model) currentSetting() (settingRow, bool) {
	rows := m.settingsRows()
	if m.settings == nil || m.settings.index < 0 || m.settings.index >= len(rows) {
		return settingRow{}, false
	}
	return rows[m.settings.index], true
}

// settingValue reads the effective value of a row.
func (m *Model) settingValue(row settingRow) any {
	if row.keybinding {
		return m.keys[row.actionID]
	}
	if set, ok := config.SettingByPath(row.path); ok {
		return set.Get(m.cfg)
	}
	return ""
}

func (m *Model) settingDefault(row settingRow) any {
	if row.keybinding {
		for _, a := range keyActions() {
			if a.ID == row.actionID {
				return a.Default
			}
		}
		return ""
	}
	if set, ok := config.SettingByPath(row.path); ok {
		return set.Get(config.Default())
	}
	return ""
}

func (m *Model) settingChanged(row settingRow) bool {
	if row.keybinding {
		return m.store.IsOverridden(row.path)
	}
	return config.ValueString(m.settingValue(row)) != config.ValueString(m.settingDefault(row)) || m.store.IsOverridden(row.path)
}

// setSetting persists one change, reapplies the runtime configuration and
// refreshes the screen.
func (m *Model) setSetting(path string, value any) tea.Cmd {
	if m.store == nil {
		return nil
	}
	if err := m.store.Set(path, value); err != nil {
		m.notice = err.Error()
		return nil
	}
	m.applyConfig()
	m.settings.capture = false
	m.notice = "Saved " + path
	return m.afterSettingChange(path)
}

// resetSetting returns one setting to its default by dropping the app-managed
// override. A value set in config.toml is left untouched.
func (m *Model) resetSetting(path string) tea.Cmd {
	if err := m.store.Reset(path); err != nil {
		m.notice = err.Error()
		return nil
	}
	m.applyConfig()
	m.notice = "Reset " + path
	return m.afterSettingChange(path)
}

// afterSettingChange invalidates whatever the change affects.
func (m *Model) afterSettingChange(path string) tea.Cmd {
	switch {
	case strings.HasPrefix(path, "git."):
		if m.history != nil {
			m.history.cache, m.history.order = nil, nil
			m.history.filters = nil
		}
		if m.branches != nil {
			m.branches.local, m.branches.remote = nil, nil
		}
	case path == "behavior.auto_refresh":
		return m.armAutoRefresh()
	}
	return nil
}

func (m *Model) updateSettingsKey(key string, msg tea.KeyMsg) tea.Cmd {
	s := m.settings
	if s == nil {
		return nil
	}
	if s.effective {
		switch key {
		case "esc", "q", "V":
			s.effective = false
		case "j", "down":
			s.effectiveScroll.ScrollDown(1)
		case "k", "up":
			s.effectiveScroll.ScrollUp(1)
		case "pgdown":
			s.effectiveScroll.ScrollDown(max(1, m.historyPaneHeight()-4))
		case "pgup":
			s.effectiveScroll.ScrollUp(max(1, m.historyPaneHeight()-4))
		case "home":
			s.effectiveScroll.ScrollToTop()
		case "end":
			s.effectiveScroll.ScrollDown(len(config.Effective(m.cfg)))
		}
		return nil
	}
	if s.searching {
		switch key {
		case "enter":
			s.searching = false
			s.index = 0
			return nil
		case "esc":
			s.searching = false
			s.query = ""
			s.index = 0
			return nil
		case "backspace":
			runes := []rune(s.query)
			if len(runes) > 0 {
				s.query = string(runes[:len(runes)-1])
			}
		default:
			if msg.Type == tea.KeyRunes {
				s.query += string(msg.Runes)
			}
		}
		s.index = 0
		return nil
	}
	switch key {
	case "/":
		m.focus = 1
		s.searching = true
		return nil
	case "esc":
		if s.query != "" {
			s.query = ""
			s.index = 0
			return nil
		}
		return nil
	case "x":
		if row, ok := m.currentSetting(); ok {
			return m.resetSetting(row.path)
		}
		return nil
	case "X":
		return m.confirmResetSettings()
	case "E":
		return m.openConfigInEditor()
	case "L":
		return m.reloadConfiguration()
	case "V":
		return m.showEffectiveConfig()
	case "enter", " ":
		return m.activateSetting()
	case "left", "h":
		return m.adjustSetting(-1)
	case "right", "l":
		return m.adjustSetting(1)
	case "tab", "shift+tab":
		// handled by the global keymap
		return nil
	case "j", "down", "k", "up", "pgdown", "pgup", "home", "end":
		delta := 1
		if key == "k" || key == "up" || key == "pgup" {
			delta = -1
		}
		if key == "pgdown" || key == "pgup" {
			delta *= 8
		}
		switch m.focus {
		case 0:
			s.catIndex = min(max(0, s.catIndex+delta), len(s.categories)-1)
			s.index, s.top = 0, 0
			s.inspect.ScrollToTop()
		case 1:
			rows := m.settingsRows()
			if len(rows) == 0 {
				return nil
			}
			s.index = min(max(0, s.index+delta), len(rows)-1)
			s.inspect.ScrollToTop()
		default:
			if delta < 0 {
				s.inspect.ScrollUp(-delta)
			} else {
				s.inspect.ScrollDown(delta)
			}
		}
		return nil
	}
	return nil
}

// captureKey records the next key as the selected binding.
func (m *Model) captureKey(key string) tea.Cmd {
	s := m.settings
	if key == "esc" {
		s.capture = false
		m.notice = "Cancelled"
		return nil
	}
	if key == "enter" {
		return nil
	}
	row, ok := m.currentSetting()
	if !ok || !row.keybinding {
		s.capture = false
		return nil
	}
	return m.setSetting(row.path, key)
}

func (m *Model) activateSetting() tea.Cmd {
	row, ok := m.currentSetting()
	if !ok {
		return nil
	}
	switch {
	case row.keybinding:
		m.settings.capture = true
		m.notice = "Press a key for " + row.actionID + " · Esc cancels"
		return nil
	case row.path == "appearance.theme":
		return m.openThemePicker()
	case row.kind == config.KindBool:
		return m.setSetting(row.path, !m.settingValue(row).(bool))
	case row.kind == config.KindEnum:
		return m.cycleEnum(row, 1)
	case row.kind == config.KindText:
		m.prompt = &promptState{
			title:   row.title,
			label:   row.description,
			help:    "Enter saves · Esc cancels",
			kind:    promptSettingText,
			context: row.path,
			value:   config.ValueString(m.settingValue(row)),
		}
		return nil
	}
	return nil
}

func (m *Model) adjustSetting(delta int) tea.Cmd {
	row, ok := m.currentSetting()
	if !ok {
		return nil
	}
	switch row.kind {
	case config.KindInt:
		current, _ := toInt(m.settingValue(row))
		next := min(max(row.min, current+delta), row.max)
		if next == current {
			return nil
		}
		return m.setSetting(row.path, next)
	case config.KindEnum:
		return m.cycleEnum(row, delta)
	case config.KindBool:
		if delta > 0 {
			return m.setSetting(row.path, true)
		}
		return m.setSetting(row.path, false)
	}
	return nil
}

func (m *Model) cycleEnum(row settingRow, delta int) tea.Cmd {
	current := config.ValueString(m.settingValue(row))
	index := 0
	for i, option := range row.enum {
		if option == current {
			index = i
			break
		}
	}
	index = (index + delta + len(row.enum)) % len(row.enum)
	return m.setSetting(row.path, row.enum[index])
}

func toInt(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case string:
		n := 0
		for _, r := range x {
			if r < '0' || r > '9' {
				return 0, false
			}
			n = n*10 + int(r-'0')
		}
		return n, true
	}
	return 0, false
}

// confirmResetSettings asks before clearing every in-app override.
func (m *Model) confirmResetSettings() tea.Cmd {
	if len(m.store.Overrides()) == 0 {
		m.notice = "No settings have been changed in-app"
		return nil
	}
	m.confirm = &confirmState{
		title:  "reset all settings?",
		body:   "Reset every setting changed in TideGit to its default?\n\nYour config.toml is not touched, and recent repositories\nand other state are kept.",
		accept: "X",
		run: func() tea.Cmd {
			if err := m.store.ResetAll(); err != nil {
				m.notice = err.Error()
				return nil
			}
			m.applyConfig()
			m.notice = "All in-app settings reset"
			return m.reloadSettings()
		},
	}
	return nil
}

// externalEditor returns the configured editor override, then $VISUAL, then
// $EDITOR. That order is documented in the Settings inspector.
func (m *Model) externalEditor() string {
	if m.cfg != nil {
		if override := strings.TrimSpace(m.cfg.Editor.ExternalEditor); override != "" {
			return override
		}
	}
	if visual := strings.TrimSpace(os.Getenv("VISUAL")); visual != "" {
		return visual
	}
	return strings.TrimSpace(os.Getenv("EDITOR"))
}

// configEditedMsg reports that the external editor returned.
type configEditedMsg struct{ err error }

// openConfigInEditor opens config.toml, offering to create a starter file when
// it does not exist yet.
func (m *Model) openConfigInEditor() tea.Cmd {
	path := ""
	if m.store != nil {
		path = m.store.PrimaryPath()
	}
	if path == "" {
		m.notice = "No config path is available"
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		m.confirm = &confirmState{
			title:  "create config file?",
			body:   "No config file exists yet.\n\nCreate " + path + " with a commented starter?",
			accept: "C",
			run: func() tea.Cmd {
				if err := config.WriteStarter(path); err != nil {
					m.notice = err.Error()
					return nil
				}
				return m.launchConfigEditor(path)
			},
		}
		return nil
	}
	return m.launchConfigEditor(path)
}

func (m *Model) launchConfigEditor(path string) tea.Cmd {
	editor := m.externalEditor()
	if editor == "" {
		m.notice = "Set $VISUAL, $EDITOR or editor.external_editor to open the config"
		return nil
	}
	parts := strings.Fields(editor)
	if len(parts) == 0 {
		m.notice = "editor.external_editor is empty"
		return nil
	}
	cmd := exec.Command(parts[0], append(parts[1:], path)...)
	cmd.Dir = filepath.Dir(path)
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return configEditedMsg{err: err} })
}

func (m *Model) handleConfigEdited(msg configEditedMsg) tea.Cmd {
	if msg.err != nil {
		m.notice = "Editor: " + msg.err.Error()
		return nil
	}
	return m.reloadConfiguration()
}

// reloadConfiguration re-reads both config layers and reports the outcome.
func (m *Model) reloadConfiguration() tea.Cmd {
	err := m.store.Reload()
	m.applyConfig()
	if err != nil {
		m.notice = "Config reload failed; keeping the previous values · " + firstLine(err.Error())
	} else if issues := m.store.Warnings(); len(issues) > 0 {
		m.notice = "Config reloaded with warnings · " + firstLine(issues[0])
	} else {
		m.notice = "Configuration reloaded"
	}
	return m.reloadSettings()
}

// showEffectiveConfig opens the resolved-configuration view.
func (m *Model) showEffectiveConfig() tea.Cmd {
	if m.settings == nil {
		m.settings = &settingsState{}
	}
	m.settings.effective = true
	m.settings.effectiveScroll.ScrollToTop()
	return nil
}

// effectivePanel lists the merged configuration with its source, so a value set
// in overrides.toml, config.toml or the defaults is unambiguous.
func (m *Model) effectivePanel(r tideui.Renderer) string {
	pairs := config.Effective(m.cfg)
	width := min(84, m.width-6)
	inner := max(1, width-4)
	rows := []string{
		muted(r, "config    "+safeText(m.store.PrimaryPath())),
		muted(r, "overrides "+safeText(m.store.OverridesPath())),
		"",
	}
	visible := max(6, min(20, m.height-13))
	m.settings.effectiveScroll.ClampTo(len(pairs), visible)
	start := min(m.settings.effectiveScroll.Offset(), max(0, len(pairs)-visible))
	for _, p := range pairs[start:min(len(pairs), start+visible)] {
		marker := "  "
		switch m.store.Source(p[0]) {
		case "overrides.toml":
			marker = "o "
		case "config.toml":
			marker = "c "
		}
		label := muted(r, fmt.Sprintf("%-34s", clip(p[0], 34)))
		rows = append(rows, marker+label+r.Styles.DetailBody.Render(clip(p[1], max(1, inner-38))))
	}
	rows = append(rows, "", muted(r, "o overrides.toml   c config.toml   blank: default"))
	body := strings.Join(rows, "\n") + "\n" +
		r.RenderSoftHints(inner+2, tideui.SoftHint{Key: "j / k", Label: "scroll"},
			tideui.SoftHint{Key: "Esc", Label: "close"})
	panel := r.SoftPanelOverlay(tideui.SoftPanel{Prefix: "tidegit", Title: "effective configuration",
		Width: width, Content: inset(body, width)})
	return panel.Content
}

// recordRepository remembers the current repository in persistent state.
func (m *Model) recordRepository() {
	if m.state == nil || m.repo.Root == "" {
		return
	}
	m.state.TouchRepository(m.repo.Root, m.cfg.Behavior.RecentRepositoryCount)
	m.state.LastScreen = screenDefaultName(m.screen)
	if m.statePath != "" {
		_ = m.state.Save(m.statePath)
	}
}

// timeAgo renders a timestamp as a relative age when appearance.relative_time
// is on, and as an absolute date otherwise.
func (m *Model) timeAgo(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	if m.cfg == nil || m.cfg.Appearance.RelativeTime {
		return relativeTime(t, time.Now())
	}
	return t.Format("2006-01-02")
}

// historyBatch is the configured History page size.
func (m *Model) historyBatch() int {
	if m.cfg == nil {
		return git.HistoryBatch
	}
	return m.cfg.Git.HistoryPageSize
}

// showRemoteBranches and showTags gate the ref filters and branch list.
func (m *Model) showRemoteBranches() bool { return m.cfg == nil || m.cfg.Git.ShowRemoteBranches }
func (m *Model) showTags() bool           { return m.cfg == nil || m.cfg.Git.ShowTags }

// operationRetention bounds the raw output kept for the details panel.
func (m *Model) operationRetention() int {
	if m.cfg == nil {
		return 2000
	}
	return m.cfg.Remote.OperationOutputRetention
}

// armAutoRefresh starts the periodic status refresh when enabled.
func (m *Model) armAutoRefresh() tea.Cmd {
	if m.cfg == nil || !m.cfg.Behavior.AutoRefresh {
		return nil
	}
	return tea.Tick(autoRefreshInterval, func(time.Time) tea.Msg { return autoRefreshMsg{} })
}
