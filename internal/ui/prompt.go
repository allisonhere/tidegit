package ui

import (
	"context"
	"strings"

	"github.com/allisonhere/tidegit/internal/git"
	"github.com/allisonhere/tideui"
	tea "github.com/charmbracelet/bubbletea"
)

// promptKind selects what a confirmed prompt does. The prompt itself only ever
// collects one line of text.
type promptKind int

const (
	promptCreateBranch promptKind = iota
	promptRenameBranch
	promptJumpToRef
	promptStashMessage
	promptSettingText
	promptDiffSearch
)

// promptState is a single-line text field shown in a soft panel. Ripple owns
// multi-line message editing; a branch name needs neither wrapping nor undo, so
// this stays a small field rather than a second editor.
type promptState struct {
	title, label, help string
	value              string
	kind               promptKind
	context            string // start point, old name, or unused
	err                string
}

// confirmKind selects what an accepted confirmation does.
type confirmKind int

const (
	confirmDeleteBranch confirmKind = iota
	confirmForceDeleteBranch
	confirmDropStash
)

// confirmState is a yes/no question. accept is the single key that agrees, so
// no destructive action is ever one Enter away from a cursor resting on it.
type confirmState struct {
	title, body string
	accept      string
	danger      bool
	kind        confirmKind
	target      string
	// run is an optional action taken when the accept key is pressed. It lets a
	// destructive confirmation carry its own closure instead of growing the
	// kind enum for every new recovery action.
	run func() tea.Cmd
}

func (m *Model) updatePromptKey(key string, msg tea.KeyMsg) tea.Cmd {
	p := m.prompt
	switch key {
	case "esc":
		m.prompt = nil
		return nil
	case "enter", "ctrl+s":
		return m.submitPrompt(key == "ctrl+s")
	case "backspace":
		r := []rune(p.value)
		if len(r) > 0 {
			p.value = string(r[:len(r)-1])
		}
		p.err = ""
		return nil
	case "ctrl+u":
		p.value = ""
		p.err = ""
		return nil
	default:
		if msg.Type == tea.KeyRunes {
			p.value += string(msg.Runes)
			p.err = ""
		}
		return nil
	}
}

func (m *Model) submitPrompt(alsoSwitch bool) tea.Cmd {
	p := m.prompt
	value := strings.TrimSpace(p.value)
	// The stash prompt is the one place an empty value is a valid submission:
	// a stash message is optional.
	if value == "" && p.kind != promptStashMessage && p.kind != promptSettingText && p.kind != promptDiffSearch {
		p.err = "Enter a name."
		return nil
	}
	switch p.kind {
	case promptDiffSearch:
		raw := p.value
		m.prompt = nil
		return m.submitDiffSearch(strings.TrimSpace(raw))
	case promptSettingText:
		path := p.context
		raw := p.value
		m.prompt = nil
		if path == "editor.external_editor" {
			return m.setSetting(path, strings.TrimSpace(raw))
		}
		return m.setSetting(path, value)
	case promptStashMessage:
		includeUntracked := p.context == "untracked"
		m.prompt = nil
		return m.stashChanges(value, includeUntracked)
	case promptCreateBranch:
		name, start := value, p.context
		m.prompt = nil
		notice := "Created branch " + safeText(name)
		if alsoSwitch {
			notice = "Created and switched to " + safeText(name)
		}
		// Creating from the History screen should show the result there, so the
		// branch list is refreshed underneath without leaving the screen.
		return m.runBranchAction("Creating "+safeText(name), name, notice,
			func(ctx context.Context, repo git.Repository) error {
				return repo.CreateBranch(ctx, name, start, alsoSwitch)
			})
	case promptRenameBranch:
		oldName, newName := p.context, value
		if oldName == newName {
			m.prompt = nil
			return nil
		}
		m.prompt = nil
		return m.runBranchAction("Renaming "+safeText(oldName), newName,
			"Renamed "+safeText(oldName)+" to "+safeText(newName),
			func(ctx context.Context, repo git.Repository) error {
				return repo.RenameBranch(ctx, oldName, newName)
			})
	case promptJumpToRef:
		m.prompt = nil
		return m.jumpToRef(value)
	}
	return nil
}

func (m *Model) updateConfirmKey(key string) tea.Cmd {
	c := m.confirm
	switch key {
	case "esc", "q", "n":
		m.confirm = nil
		return nil
	case c.accept:
		kind, target, run := c.kind, c.target, c.run
		m.confirm = nil
		if run != nil {
			return run()
		}
		switch kind {
		case confirmDeleteBranch:
			return m.deleteBranch(target, false)
		case confirmForceDeleteBranch:
			return m.deleteBranch(target, true)
		case confirmDropStash:
			return m.dropStash(target)
		}
	}
	return nil
}

// offerForceDelete turns Git's refusal to discard unmerged work into a second,
// separate confirmation. It is never reached without the user having asked for
// a safe delete first, and it never runs on its own.
func (m *Model) offerForceDelete(name string) {
	m.confirm = &confirmState{
		title: "branch is not merged",
		body: "Git refused to delete " + safeText(name) + " because it holds\n" +
			"commits that are not merged into HEAD.\n\n" +
			"Deleting it anyway discards those commits.\n" +
			"This cannot be undone from TideGit.",
		accept: "D",
		danger: true,
		kind:   confirmForceDeleteBranch,
		target: name,
	}
}

// promptPanel renders the text field.
func (m *Model) promptPanel(r tideui.Renderer) string {
	p := m.prompt
	width := min(64, m.width-6)
	inner := max(1, width-4)
	// A soft panel already carries a border; a bordered input inside it would
	// draw a second box. The field is a highlighted line instead.
	field := r.Styles.DetailFocusLine.Width(inner).Render(clip(" "+safeText(p.value)+"▎", inner))
	body := muted(r, clip(p.label, inner)) + "\n\n" + field + "\n"
	if p.err != "" {
		body += "\n" + r.Styles.DetailBody.Foreground(r.Styles.Theme.Error).Render(clip(p.err, inner))
	} else {
		body += "\n" + muted(r, clip(p.help, inner))
	}
	panel := r.SoftPanelOverlay(tideui.SoftPanel{Prefix: "tidegit", Title: p.title,
		Width: width, Content: inset(body, width)})
	return panel.Content
}

// confirmPanel renders the yes/no question.
func (m *Model) confirmPanel(r tideui.Renderer) string {
	c := m.confirm
	width := min(62, m.width-6)
	inner := max(1, width-4)
	var lines []string
	for _, line := range strings.Split(c.body, "\n") {
		style := r.Styles.DetailBody
		if c.danger && (strings.Contains(line, "NOT merged") || strings.Contains(line, "cannot be undone")) {
			style = style.Foreground(r.Styles.Theme.Error).Bold(true)
		}
		lines = append(lines, style.Render(clip(line, inner)))
	}
	body := strings.Join(lines, "\n") + "\n\n" +
		accent(r, c.accept+"  confirm") + "\n" + muted(r, "Esc / n  cancel")
	panel := r.SoftPanelOverlay(tideui.SoftPanel{Prefix: "tidegit", Title: c.title,
		Width: width, Content: inset(body, width)})
	return panel.Content
}
