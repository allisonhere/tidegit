package ui

import (
	"fmt"
	"strings"

	"github.com/allisonhere/tidegit/internal/config"
	"github.com/allisonhere/tideui"
)

func (m *Model) settingsView(r tideui.Renderer) string {
	mode, w := m.paneLayout()
	if mode == tideui.ThreeColumn {
		w = [3]int{m.width*3/10 - 2, m.width*4/10 - 2, m.width - m.width*3/10 - m.width*4/10 - 2}
	}
	height := m.historyPaneHeight()

	title, hint := "SETTING", ""
	if row, ok := m.currentSetting(); ok {
		hint = row.path
	}
	layout := tideui.Layout{Width: m.width, Height: m.height - 3, Mode: tideui.ThreeColumn,
		ColumnRatios: [3]float64{3, 4, 3}, SidebarRatio: 0.30, Panes: [3]tideui.Pane{{Title: "SETTINGS", Hint: fmt.Sprint(len(m.settings.categories)),
			Content: m.settingsCategoryPane(r, w[0], height), Focused: m.focus == 0},
			{Title: "VALUES", Hint: fmt.Sprint(len(m.settingsRows())),
				Content: m.settingsListPane(r, w[1], height), Focused: m.focus == 1},
			{Title: title, Hint: hint, Content: m.settingsInspectorPane(r, w[2], height), Focused: m.focus == 2},
		}, Status: &tideui.StatusBar{Left: clip(safeText(m.settingsStatus()), m.width-3)}}
	layout.Mode = mode
	layout.UpperRightRatio = 0.45

	base := m.globalHeader(r, "SETTINGS") + "\n" + r.Render(layout) + "\n" + m.settingsHints(r)
	return m.withOverlays(r, base)
}

func (m *Model) settingsStatus() string {
	s := m.settings
	if s == nil {
		return "Settings"
	}
	if s.searching || s.query != "" {
		return fmt.Sprintf("Search %q · %d matches", s.query, len(m.settingsRows()))
	}
	if s.capture {
		return "Press a key to bind · Esc cancels"
	}
	if m.notice != "" {
		return m.notice
	}
	if len(m.configWarnings) > 0 {
		return fmt.Sprintf("config · %s · L reloads", firstLine(m.configWarnings[0]))
	}
	return fmt.Sprintf("%s · %d settings · %d changed", m.settings.categories[min(s.catIndex, len(m.settings.categories)-1)], len(m.settingsRows()), m.changedCount())
}

func (m *Model) changedCount() int {
	if m.store == nil {
		return 0
	}
	n := 0
	for _, set := range config.Settings() {
		if m.store.IsOverridden(set.Path) {
			n++
		}
	}
	for _, a := range keyActions() {
		if m.store.IsOverridden("keybindings." + a.ID) {
			n++
		}
	}
	return n
}

func (m *Model) settingsHints(r tideui.Renderer) string {
	s := m.settings
	if s != nil && s.searching {
		return m.hintBar(r, tideui.SoftHint{Key: "/", Label: safeText(s.query)},
			tideui.SoftHint{Key: "Enter", Label: "keep"}, tideui.SoftHint{Key: "Esc", Label: "clear"})
	}
	if s != nil && s.capture {
		return m.hintBar(r, tideui.SoftHint{Key: "key", Label: "set the binding"}, tideui.SoftHint{Key: "Esc", Label: "cancel"})
	}
	return m.hintBar(r,
		tideui.SoftHint{Key: "Enter", Label: "edit"}, tideui.SoftHint{Key: "← / →", Label: "adjust"},
		tideui.SoftHint{Key: "x", Label: "reset"}, tideui.SoftHint{Key: "/", Label: "search"},
		tideui.SoftHint{Key: "E", Label: "config"}, tideui.SoftHint{Key: "L", Label: "reload"},
		tideui.SoftHint{Key: "?", Label: "help"})
}

func (m *Model) settingsCategoryPane(r tideui.Renderer, width, height int) string {
	s := m.settings
	inner := max(1, width-2)
	var rows []string
	for i, category := range s.categories {
		suffix := ""
		if count := m.categoryChanged(category); count > 0 {
			suffix = "•"
			_ = count
		}
		rows = append(rows, r.RenderRow(tideui.Row{
			Text: category, Suffix: suffix, Selected: i == s.catIndex && s.query == "",
		}, inner))
	}
	if s.query != "" {
		rows = append(rows, "", muted(r, "Showing matches across"))
		rows = append(rows, muted(r, "every category."))
	}
	if issues := m.keyIssues; len(issues) > 0 && s.categories[s.catIndex] == "Keybindings" {
		rows = append(rows, "", muted(r, "ISSUES"))
		for _, issue := range issues[:min(len(issues), 4)] {
			rows = append(rows, r.Styles.DetailBody.Foreground(r.Styles.Theme.Error).Render(clip(issue, inner)))
		}
	}
	return inset("\n"+strings.Join(rows, "\n"), width)
}

func (m *Model) categoryChanged(category string) int {
	if m.store == nil {
		return 0
	}
	if category == "Keybindings" {
		n := 0
		for _, a := range keyActions() {
			if m.store.IsOverridden("keybindings." + a.ID) {
				n++
			}
		}
		return n
	}
	n := 0
	for _, set := range config.Settings() {
		if set.Category == category && m.store.IsOverridden(set.Path) {
			n++
		}
	}
	return n
}

func (m *Model) settingsListPane(r tideui.Renderer, width, height int) string {
	s := m.settings
	inner := max(1, width-2)
	rows := m.settingsRows()
	if len(rows) == 0 {
		if s.query != "" {
			return inset("\n\n"+accent(r, "No matches")+"\n\n"+muted(r, "Nothing matches "+safeText(s.query)+"."), width)
		}
		return inset("\n\n"+accent(r, "No settings")+"\n\n"+muted(r, "This category is empty."), width)
	}
	visible := max(1, height-2)
	if s.index < s.top {
		s.top = s.index
	}
	if s.index >= s.top+visible {
		s.top = s.index - visible + 1
	}
	s.top = max(0, min(s.top, max(0, len(rows)-visible)))

	var out []string
	for i := s.top; i < min(len(rows), s.top+visible); i++ {
		row := rows[i]
		prefix := "  "
		if m.settingChanged(row) {
			prefix = "• "
		}
		out = append(out, r.RenderRow(tideui.Row{
			Prefix: prefix, Text: row.title, Suffix: m.settingDisplay(row),
			Selected: i == s.index && m.focus == 1,
		}, inner))
	}
	return inset(strings.Join(out, "\n"), width)
}

// settingDisplay is the right-aligned value shown beside a setting row.
func (m *Model) settingDisplay(row settingRow) string {
	current := config.ValueString(m.settingValue(row))
	if row.keybinding {
		if m.settingChanged(row) {
			return current + "  (" + config.ValueString(m.settingDefault(row)) + ")"
		}
		return current
	}
	switch row.kind {
	case config.KindBool:
		if current == "true" {
			return "on"
		}
		return "off"
	default:
		return clip(current, 22)
	}
}

func (m *Model) settingsInspectorPane(r tideui.Renderer, width, height int) string {
	row, ok := m.currentSetting()
	if !ok {
		return inset("\n\n"+accent(r, "Nothing selected")+"\n\n"+muted(r, "Choose a setting to inspect it."), width)
	}
	inner := max(1, width-2)
	var b strings.Builder
	b.WriteString("\n")
	for _, line := range wrapText(row.title, inner) {
		b.WriteString(r.Styles.DetailBody.Bold(true).Render(line) + "\n")
	}
	b.WriteString(muted(r, row.path) + "\n\n")
	for _, line := range wrapText(row.description, inner) {
		b.WriteString(r.Styles.DetailBody.Render(line) + "\n")
	}
	b.WriteString("\n")

	current := config.ValueString(m.settingValue(row))
	def := config.ValueString(m.settingDefault(row))
	source := "default"
	if m.store != nil {
		source = m.store.Source(row.path)
	}
	field := func(label, value string) {
		if value == "" {
			value = "—"
		}
		for i, line := range wrapText(value, max(8, inner-10)) {
			if i == 0 {
				b.WriteString(muted(r, fmt.Sprintf("%-9s", label)) + r.Styles.DetailBody.Render(line) + "\n")
			} else {
				b.WriteString(strings.Repeat(" ", 9) + r.Styles.DetailBody.Render(line) + "\n")
			}
		}
	}
	field("current", current)
	field("default", def)
	field("source", source)
	if row.kind == config.KindEnum && len(row.enum) > 0 {
		field("valid", strings.Join(row.enum, ", "))
	}
	if row.kind == config.KindInt {
		field("range", fmt.Sprintf("%d – %d", row.min, row.max))
	}
	if row.keybinding {
		field("action", row.actionID)
	}
	if row.path == "editor.external_editor" {
		field("precedence", "override → $VISUAL → $EDITOR")
	}
	if row.restart {
		b.WriteString("\n" + r.Styles.DetailBody.Foreground(r.Styles.Theme.Error).Render("Requires a restart.") + "\n")
	}
	if changed := m.settingChanged(row); changed {
		b.WriteString("\n" + accent(r, "modified") + muted(r, " · x resets to default") + "\n")
	}

	if m.previewApplies(row.path) {
		b.WriteString("\n" + m.settingsPreview(r, inner) + "\n")
	}

	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	m.settings.inspect.ClampTo(len(lines), max(1, height-1))
	start := min(m.settings.inspect.Offset(), max(0, len(lines)-max(1, height-1)))
	end := min(len(lines), start+max(1, height-1))
	return inset(strings.Join(lines[start:end], "\n"), width)
}

// previewApplies reports whether a setting is worth showing a live preview for.
func (m *Model) previewApplies(path string) bool {
	return strings.HasPrefix(path, "appearance.")
}

// settingsPreview renders a small sample of the real UI, using TideUI rows and
// a status bar, so the effect of an appearance change is visible immediately.
func (m *Model) settingsPreview(r tideui.Renderer, width int) string {
	var b strings.Builder
	b.WriteString(muted(r, "PREVIEW") + "\n")
	b.WriteString(r.RenderRow(tideui.Row{Text: "src/parser.rs", Suffix: "M", Selected: true}, width) + "\n")
	b.WriteString(r.RenderRow(tideui.Row{Text: "README.md", Suffix: "A"}, width) + "\n")
	b.WriteString(r.RenderRow(tideui.Row{Text: "old/notes.txt", Suffix: "D", Muted: true}, width) + "\n")
	bar := padLine(" "+accent(r, "2 staged")+muted(r, "   ·   ")+accent(r, "1 unstaged"), width, r.Styles.StatusBar)
	b.WriteString(bar)
	return b.String()
}
