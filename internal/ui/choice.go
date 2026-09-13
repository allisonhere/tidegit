package ui

import (
	"fmt"
	"strings"

	"github.com/allisonhere/tidegit/internal/git"
	"github.com/allisonhere/tideui"
	tea "github.com/charmbracelet/bubbletea"
)

// choiceKind selects what accepting a choice does. A choice is a short list of
// named options shown in a soft panel; it exists so TideGit never silently
// picks a remote.
type choiceKind int

const (
	choiceFetchRemote choiceKind = iota
	choicePushRemote
)

type choiceOption struct {
	label string
	value string
	hint  string
	// run is an optional action taken when the option is accepted. It lets a
	// chooser carry its own closure instead of growing the kind enum.
	run func(value string) tea.Cmd
}

type choiceState struct {
	title, label string
	options      []choiceOption
	index        int
	kind         choiceKind
	// setUpstream carries whether the chosen push should record an upstream.
	setUpstream bool
}

func (m *Model) updateChoiceKey(key string) tea.Cmd {
	c := m.choice
	switch key {
	case "esc", "q":
		m.choice = nil
		return nil
	case "enter":
		if c.index < 0 || c.index >= len(c.options) {
			m.choice = nil
			return nil
		}
		option := c.options[c.index]
		kind, setUpstream := c.kind, c.setUpstream
		m.choice = nil
		if option.run != nil {
			return option.run(option.value)
		}
		switch kind {
		case choiceFetchRemote:
			return m.fetchRemote(option.value)
		case choicePushRemote:
			return m.pushToRemote(option.value, setUpstream)
		}
		return nil
	case "j", "down", "ctrl+n":
		c.index = min(c.index+1, max(0, len(c.options)-1))
	case "k", "up", "ctrl+k":
		c.index = max(0, c.index-1)
	}
	return nil
}

// chooseRemote opens the remote chooser for an action that cannot safely guess.
func (m *Model) chooseRemote(kind choiceKind, title, label string, remotes []git.Remote, setUpstream bool) tea.Cmd {
	options := make([]choiceOption, 0, len(remotes))
	for _, remote := range remotes {
		hint := "default"
		if !remote.Default {
			hint = fmt.Sprintf("%d branches", len(remote.Branches))
		}
		options = append(options, choiceOption{label: remote.Name, value: remote.Name, hint: hint})
	}
	m.choice = &choiceState{title: title, label: label, options: options, kind: kind, setUpstream: setUpstream}
	return nil
}

func (m *Model) choicePanel(r tideui.Renderer) string {
	c := m.choice
	width := min(60, m.width-6)
	inner := max(1, width-4)
	var rows []string
	for _, line := range wrapText(c.label, inner) {
		rows = append(rows, muted(r, line))
	}
	rows = append(rows, "")
	for i, option := range c.options {
		rows = append(rows, r.RenderSoftRow(tideui.SoftRow{
			Text: option.label, Suffix: option.hint, Selected: i == c.index}, inner))
	}
	rows = append(rows, "", muted(r, "Enter choose · Esc cancel"))
	body := strings.Join(rows, "\n")
	panel := r.SoftPanelOverlay(tideui.SoftPanel{Prefix: "tidegit", Title: c.title,
		Width: width, Content: inset(body, width)})
	return panel.Content
}
