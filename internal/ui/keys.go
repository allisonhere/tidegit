package ui

import (
	"strings"

	"github.com/allisonhere/tidegit/internal/config"
	"github.com/allisonhere/tideui"
	tea "github.com/charmbracelet/bubbletea"
)

// keyAction is one customizable binding. IDs are stable and are the public
// configuration API; function names are never exposed.
type keyAction struct {
	ID          string
	Description string
	Default     string
}

// keyActions is the catalog of every customizable action, in documentation
// order. The defaults reproduce the unconfigured bindings exactly.
func keyActions() []keyAction {
	return []keyAction{
		{"app.quit", "Quit TideGit, or step back to Status", "q"},
		{"app.command_palette", "Open the command palette", "ctrl+p"},
		{"app.theme", "Open the theme picker", "t"},
		{"app.help", "Show the keyboard guide", "?"},
		{"app.operation_details", "Show the last operation's details", "o"},
		{"repo.refresh", "Refresh the current screen", "r"},
		{"view.status", "Go to Status", "1"},
		{"view.history", "Go to History", "2"},
		{"view.branches", "Go to Branches", "3"},
		{"view.stash", "Go to Stash", "4"},
		{"view.remotes", "Go to Remotes", "5"},
		{"view.conflicts", "Go to Conflicts", "6"},
		{"view.reflog", "Go to Reflog", "7"},
		{"view.settings", "Go to Settings", "8"},
		{"git.fetch", "Fetch the default remote", "f"},
		{"git.pull", "Pull the current branch", "p"},
		{"git.push", "Push the current branch", "P"},
		{"git.stage", "Stage the selected file", "s"},
		{"git.unstage", "Unstage the selected file", "u"},
		{"git.commit", "Compose a commit", "c"},
		{"git.amend", "Amend HEAD", "A"},
		{"pane.next", "Focus the next pane", "tab"},
		{"pane.prev", "Focus the previous pane", "shift+tab"},
		{"pane.expand", "Expand or restore the focused pane", "z"},
	}
}

// resolveKeymap merges overrides over the defaults and reports conflicts and
// unknown action ids. It returns both directions so callers can show a binding
// and dispatch a key.
func resolveKeymap(cfg *config.Config) (map[string]string, []string) {
	byID := map[string]string{}
	for _, action := range keyActions() {
		byID[action.ID] = action.Default
	}
	var issues []string
	known := map[string]bool{}
	for _, action := range keyActions() {
		known[action.ID] = true
	}
	for id, key := range cfg.Keybindings {
		key = strings.TrimSpace(key)
		if !known[id] {
			issues = append(issues, "Unknown keybinding action: "+id)
			continue
		}
		if key == "" {
			issues = append(issues, id+" has an empty key")
			continue
		}
		byID[id] = key
	}
	// Detect two actions sharing one key.
	seen := map[string]string{}
	for _, action := range keyActions() {
		key := byID[action.ID]
		if other, ok := seen[key]; ok {
			issues = append(issues, "Key "+key+" is bound to both "+other+" and "+action.ID)
			continue
		}
		seen[key] = action.ID
	}
	return byID, issues
}

// actionForKey resolves a pressed key to an action id, first action winning.
func (m *Model) actionForKey(key string) (string, bool) {
	for _, action := range keyActions() {
		if m.keys[action.ID] == key {
			return action.ID, true
		}
	}
	return "", false
}

// screenAllowsAction keeps file/commit actions to the Status screen so a
// rebinding cannot make them fire from a screen that has no such selection.
func (m *Model) screenAllowsAction(id string) bool {
	switch id {
	case "git.stage", "git.unstage", "git.commit", "git.amend":
		return m.screen == screenStatus
	default:
		return true
	}
}

// runKeyAction dispatches a customizable action. It mirrors the original key
// handlers exactly, so rebinding changes the key, not the behavior.
func (m *Model) runKeyAction(id string) tea.Cmd {
	switch id {
	case "app.quit":
		switch {
		case m.expanded:
			m.expanded = false
		case m.screen != screenStatus:
			return m.goToScreen(screenStatus)
		default:
			return tea.Quit
		}
		return nil
	case "app.command_palette":
		return m.openPalette()
	case "app.theme":
		return m.openThemePicker()
	case "app.help":
		m.help = true
		return nil
	case "app.operation_details":
		if m.op != nil {
			m.op.show = !m.op.show
		}
		return nil
	case "repo.refresh":
		return m.refreshScreen()
	case "view.status":
		return m.goToScreen(screenStatus)
	case "view.history":
		return m.goToScreen(screenHistory)
	case "view.branches":
		return m.goToScreen(screenBranches)
	case "view.stash":
		return m.goToScreen(screenStash)
	case "view.remotes":
		return m.goToScreen(screenRemotes)
	case "view.conflicts":
		return m.goToScreen(screenConflicts)
	case "view.reflog":
		return m.goToScreen(screenReflog)
	case "view.settings":
		return m.openSettings()
	case "git.fetch":
		return m.fetchDefault()
	case "git.pull":
		return m.pullCurrent()
	case "git.push":
		return m.pushCurrent(false)
	case "git.stage":
		return m.act(StageFile)
	case "git.unstage":
		return m.act(UnstageFile)
	case "git.commit":
		return m.openCommit(false)
	case "git.amend":
		return m.openCommit(true)
	case "pane.next":
		m.focus = (m.focus + 1) % 3
		return nil
	case "pane.prev":
		m.focus = (m.focus + 2) % 3
		return nil
	case "pane.expand":
		m.expanded = !m.expanded
		return nil
	}
	return nil
}

// resolveConfiguredTheme resolves the configured theme name, treating "auto" as
// the Omarchy-following theme when the desktop is readable.
func resolveConfiguredTheme(name string) (tideui.Theme, bool) {
	if strings.EqualFold(strings.TrimSpace(name), "auto") {
		if theme, ok := resolveOmarchyTheme(); ok {
			return theme, true
		}
		return tideui.CatppuccinMocha, true
	}
	return ResolveTheme(name)
}
