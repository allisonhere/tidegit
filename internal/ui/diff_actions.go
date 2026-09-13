package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/allisonhere/tidegit/internal/config"
	tea "github.com/charmbracelet/bubbletea"
)

// toggleDiffMode switches between unified and split and persists the choice, so
// the preference is global rather than per-screen.
func (m *Model) toggleDiffMode() tea.Cmd {
	if m.store == nil {
		return nil
	}
	next := "split"
	if m.cfg != nil && m.cfg.Diff.Mode == "split" {
		next = "unified"
	}
	err := m.store.Set("diff.mode", next)
	m.cfg = m.store.Config()
	m.notice = "Diff mode · " + next
	if err != nil {
		m.notice = "Diff mode · " + next + " (not saved)"
	}
	return nil
}

// toggleDiffSetting flips a boolean diff setting.
func (m *Model) toggleDiffSetting(path string) tea.Cmd {
	if m.store == nil {
		return nil
	}
	setting, ok := config.SettingByPath(path)
	if !ok {
		return nil
	}
	next := !asBoolValue(setting.Get(m.cfg))
	err := m.store.Set(path, next)
	m.cfg = m.store.Config()
	if err != nil {
		m.notice = fmt.Sprintf("%s · %v (not saved)", setting.Title, next)
	} else {
		m.notice = fmt.Sprintf("%s · %v", setting.Title, next)
	}
	if path == "diff.show_whitespace" || path == "diff.syntax" || path == "diff.collapse" {
		return nil
	}
	return m.reloadDiff()
}

func asBoolValue(v any) bool {
	b, _ := v.(bool)
	return b
}

// adjustDiffContext changes the context size and refetches the open diff.
func (m *Model) adjustDiffContext(delta int) tea.Cmd {
	if m.cfg == nil {
		return nil
	}
	return m.setDiffContext(min(50, max(0, m.cfg.Diff.ContextLines+delta)))
}

func (m *Model) setDiffContext(n int) tea.Cmd {
	if m.store == nil {
		return nil
	}
	err := m.store.Set("diff.context_lines", n)
	m.cfg = m.store.Config()
	if err != nil {
		m.notice = fmt.Sprintf("Diff context · %d lines (not saved)", n)
	} else {
		m.notice = fmt.Sprintf("Diff context · %d lines", n)
	}
	return m.reloadDiff()
}

// reloadDiff refetches the working-tree diff after a setting that changes what
// Git returns.
func (m *Model) reloadDiff() tea.Cmd {
	if m.screen == screenStatus {
		return m.loadDiff()
	}
	return nil
}

// cycleWhitespaceMode rotates the Git whitespace comparison and refetches.
func (m *Model) cycleWhitespaceMode() tea.Cmd {
	if m.store == nil {
		return nil
	}
	next := "ignore-trailing"
	if m.cfg != nil {
		switch m.cfg.Diff.Whitespace {
		case "normal":
			next = "ignore-trailing"
		case "ignore-trailing":
			next = "ignore-change"
		case "ignore-change":
			next = "ignore-all"
		default:
			next = "normal"
		}
	}
	err := m.store.Set("diff.whitespace", next)
	m.cfg = m.store.Config()
	if err != nil {
		m.notice = "Whitespace · " + next + " (not saved)"
	} else {
		m.notice = "Whitespace · " + next
	}
	return m.reloadDiff()
}

// startDiffSearch opens the one-line search prompt with the last query.
func (m *Model) startDiffSearch() tea.Cmd {
	m.prompt = &promptState{
		title: "search diff",
		label: "Search the current diff",
		help:  "Enter searches · Esc cancels",
		kind:  promptDiffSearch,
		value: m.view.search,
	}
	return nil
}

// submitDiffSearch applies the query and reports the match count.
func (m *Model) submitDiffSearch(query string) tea.Cmd {
	m.view.searchText(m.diffOptionsFrom(), query)
	if len(m.view.matches) == 0 {
		m.notice = "No matches for " + safeText(query)
		return nil
	}
	m.notice = fmt.Sprintf("%d matches · n / N to step", len(m.view.matches))
	m.view.ensureVisible(m.diffViewportHeight())
	return nil
}

// flatAt returns the flattened row at the selection.
func (m *Model) flatAt() (renderLine, bool) {
	flat := m.view.flat(m.diffOptionsFrom(), true)
	if m.view.line < 0 || m.view.line >= len(flat) {
		return renderLine{}, false
	}
	return flat[m.view.line], true
}

// copyDiffLine copies the selected line's text.
func (m *Model) copyDiffLine() tea.Cmd {
	rl, ok := m.flatAt()
	if !ok {
		return nil
	}
	text := rl.text
	if rl.line != nil {
		text = rl.line.Text
	}
	if err := (systemClipboard{}).Write(text); err != nil {
		m.notice = "Clipboard: " + err.Error()
		return nil
	}
	m.notice = "Copied line to the clipboard"
	return nil
}

// copyDiffHunk copies the selected hunk as a patch.
func (m *Model) copyDiffHunk() tea.Cmd {
	h := m.view.currentHunk()
	if h == nil {
		m.notice = "No hunk selected"
		return nil
	}
	var b strings.Builder
	b.WriteString(h.Header)
	b.WriteByte('\n')
	for _, l := range h.Lines {
		b.WriteString(l.Type.Marker())
		b.WriteString(l.Text)
		b.WriteByte('\n')
	}
	if err := (systemClipboard{}).Write(b.String()); err != nil {
		m.notice = "Clipboard: " + err.Error()
		return nil
	}
	m.notice = "Copied the hunk to the clipboard"
	return nil
}

// copyDiffPath copies the file's path.
func (m *Model) copyDiffPath() tea.Cmd {
	f := m.view.file()
	if f == nil {
		return nil
	}
	if err := (systemClipboard{}).Write(f.Display()); err != nil {
		m.notice = "Clipboard: " + err.Error()
		return nil
	}
	m.notice = "Copied the path to the clipboard"
	return nil
}

// openDiffInEditor opens the file at the selected line in the configured
// external editor. Editors that accept +N are given it; others still open the
// file.
func (m *Model) openDiffInEditor() tea.Cmd {
	f := m.view.file()
	if f == nil {
		return nil
	}
	path := f.Display()
	if path == "" || path == "/dev/null" {
		m.notice = "This file does not exist in the working tree"
		return nil
	}
	line := 0
	if rl, ok := m.flatAt(); ok && rl.line != nil {
		if rl.line.New > 0 {
			line = rl.line.New
		} else {
			line = rl.line.Old
		}
	}
	return m.openFileInEditor(path, line)
}

// openFileInEditor launches the external editor with a line hint when the file
// exists. It never marks anything resolved.
func (m *Model) openFileInEditor(path string, line int) tea.Cmd {
	editor := m.externalEditor()
	if editor == "" {
		m.notice = "Set $VISUAL, $EDITOR or editor.external_editor to open files"
		return nil
	}
	full := path
	if m.repo.Root != "" {
		full = filepath.Join(m.repo.Root, path)
	}
	if _, err := os.Stat(full); err != nil {
		m.notice = "Cannot open " + safeText(path) + ": no working-tree file"
		return nil
	}
	parts := strings.Fields(editor)
	args := append([]string{}, parts[1:]...)
	if line > 0 {
		// +N is the common convention (vim, nano, emacs, less); editors that
		// do not understand it ignore it or open the file anyway.
		args = append(args, fmt.Sprintf("+%d", line))
	}
	args = append(args, full)
	cmd := exec.Command(parts[0], args...)
	cmd.Dir = filepath.Dir(full)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return editorFinishedMsg{path: path, err: err}
		}
		return editorFinishedMsg{path: path}
	})
}
