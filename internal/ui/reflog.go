package ui

import (
	"context"
	"strings"
	"time"

	"github.com/allisonhere/tidegit/internal/diff"
	"github.com/allisonhere/tidegit/internal/git"
	"github.com/allisonhere/tideui"
	tea "github.com/charmbracelet/bubbletea"
)

// reflogState backs the Reflog screen: the recovery timeline, the selected
// entry's commit, and one changed file's patch.
type reflogState struct {
	entries []git.ReflogEntry
	index   int
	top     int
	loading bool
	err     string

	filter    string
	filtering bool

	detailFor     string
	detailLoading bool
	commit        git.Commit
	detailOK      bool
	files         []git.FileChange
	fileIndex     int
	fileTop       int
	inspect       tideui.PaneScroller

	showDiff    bool
	view        diffView
	diffLabel   string
	diffLoading bool
}

func (r *reflogState) visible() []git.ReflogEntry {
	if r.filter == "" {
		return r.entries
	}
	var out []git.ReflogEntry
	for _, entry := range r.entries {
		haystack := strings.ToLower(entry.Action + " " + entry.Detail + " " + entry.Subject + " " + entry.Short)
		if strings.Contains(haystack, strings.ToLower(r.filter)) {
			out = append(out, entry)
		}
	}
	return out
}

func (r *reflogState) current() (git.ReflogEntry, bool) {
	list := r.visible()
	if r.index < 0 || r.index >= len(list) {
		return git.ReflogEntry{}, false
	}
	return list[r.index], true
}

type reflogMsg struct {
	id      int
	entries []git.ReflogEntry
	err     error
}
type reflogDetailMsg struct {
	id     int
	oid    string
	commit git.Commit
	files  []git.FileChange
	err    error
}
type reflogDiffMsg struct {
	id    int
	patch diff.Patch
	label string
	err   error
}

func (m *Model) openReflog() tea.Cmd {
	if m.reflog == nil {
		m.reflog = &reflogState{}
	}
	m.screen = screenReflog
	m.focus = 0
	m.notice = ""
	return tea.Batch(m.loadReflog(), m.loadReflogDetail())
}

func (m *Model) loadReflog() tea.Cmd {
	if m.reflog == nil {
		m.reflog = &reflogState{}
	}
	r := m.reflog
	r.loading = true
	r.err = ""
	m.reflogID++
	id, repo := m.reflogID, m.repo
	return tea.Batch(func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
		defer cancel()
		entries, err := repo.Reflog(ctx, git.ReflogBatch)
		return reflogMsg{id: id, entries: entries, err: err}
	}, pulse())
}

func (m *Model) loadReflogDetail() tea.Cmd {
	r := m.reflog
	if r == nil {
		return nil
	}
	entry, ok := r.current()
	if !ok {
		r.detailFor, r.detailOK = "", false
		r.files = nil
		return nil
	}
	if r.detailFor == entry.OID {
		return nil
	}
	m.reflogDetailID++
	id, repo, oid := m.reflogDetailID, m.repo, entry.OID
	r.detailFor = oid
	r.detailLoading = true
	r.showDiff = false
	return tea.Batch(func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
		defer cancel()
		commit, err := repo.CommitDetail(ctx, oid)
		if err != nil {
			return reflogDetailMsg{id: id, oid: oid, err: err}
		}
		files, err := repo.CommitFiles(ctx, commit)
		return reflogDetailMsg{id: id, oid: oid, commit: commit, files: files, err: err}
	}, pulse())
}

func (m *Model) loadReflogFileDiff() tea.Cmd {
	r := m.reflog
	if r == nil || !r.detailOK || len(r.files) == 0 {
		return nil
	}
	r.fileIndex = min(max(0, r.fileIndex), len(r.files)-1)
	file := r.files[r.fileIndex]
	m.reflogDiffID++
	id, repo, commit := m.reflogDiffID, m.repo, r.commit
	ctxLines, whitespace := m.diffContext(), m.whitespaceMode()
	r.showDiff = true
	r.diffLoading = true
	r.diffLabel = "Commit " + commit.Short + " · " + safeText(file.Path)
	label := r.diffLabel
	return tea.Batch(func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
		defer cancel()
		d, err := repo.CommitDiffWith(ctx, commit, file, git.DiffOptions{Context: ctxLines, Whitespace: whitespace})
		patch := diff.Parse(d.Patch)
		patch.Label = "COMMIT " + commit.Short
		return reflogDiffMsg{id: id, patch: patch, label: label, err: err}
	}, pulse())
}

func (m *Model) handleReflogResult(msg tea.Msg) (bool, tea.Cmd) {
	r := m.reflog
	switch msg := msg.(type) {
	case reflogMsg:
		if r == nil || msg.id != m.reflogID {
			return true, nil
		}
		r.loading = false
		if msg.err != nil {
			r.err = msg.err.Error()
			r.entries = nil
			return true, nil
		}
		r.err = ""
		r.entries = msg.entries
		r.index = min(r.index, max(0, len(r.visible())-1))
		return true, m.loadReflogDetail()
	case reflogDetailMsg:
		if r == nil || msg.id != m.reflogDetailID {
			return true, nil
		}
		r.detailLoading = false
		if msg.err != nil {
			r.err = msg.err.Error()
			return true, nil
		}
		r.commit, r.files = msg.commit, msg.files
		r.detailOK = true
		r.fileIndex, r.fileTop = 0, 0
		return true, nil
	case reflogDiffMsg:
		if r == nil || msg.id != m.reflogDiffID {
			return true, nil
		}
		r.diffLoading = false
		if msg.err != nil {
			r.err = msg.err.Error()
			r.showDiff = false
			return true, nil
		}
		r.view.reset(msg.patch, msg.patch.Label)
		r.diffLabel = msg.label
		return true, nil
	}
	return false, nil
}

func (m *Model) updateReflogKey(key string, msg tea.KeyMsg) tea.Cmd {
	r := m.reflog
	if r.filtering {
		switch key {
		case "enter":
			r.filtering = false
			return nil
		case "esc":
			r.filtering = false
			r.filter = ""
			r.index = 0
		case "backspace":
			runes := []rune(r.filter)
			if len(runes) > 0 {
				r.filter = string(runes[:len(runes)-1])
			}
		default:
			if msg.Type == tea.KeyRunes {
				r.filter += string(msg.Runes)
			}
		}
		r.index = min(r.index, max(0, len(r.visible())-1))
		return m.loadReflogDetail()
	}
	if r.showDiff && m.focus == 2 {
		flat := r.view.flat(m.diffOptionsFrom(), true)
		switch key {
		case "esc", "q", "backspace":
			r.showDiff = false
			return nil
		case "j", "down":
			r.view.line = min(r.view.line+1, max(0, len(flat)-1))
		case "k", "up":
			r.view.line = max(0, r.view.line-1)
		case "pgdown":
			r.view.line = min(r.view.line+max(1, m.historyPaneHeight()-4), max(0, len(flat)-1))
		case "pgup":
			r.view.line = max(0, r.view.line-max(1, m.historyPaneHeight()-4))
		case "home":
			r.view.line = 0
		case "end":
			r.view.line = max(0, len(flat)-1)
		case "]":
			r.view.moveHunk(1, m.diffOptionsFrom())
		case "[":
			r.view.moveHunk(-1, m.diffOptionsFrom())
		case "ctrl+f":
			return m.startDiffSearch()
		case "v":
			return m.toggleDiffMode()
		}
		r.view.ensureVisible(m.historyPaneHeight() - 2)
		return nil
	}
	switch key {
	case "/":
		m.focus = 0
		r.filtering = true
		return nil
	case "b":
		return m.promptRecoveryBranch()
	case "R":
		entry, ok := r.current()
		if !ok {
			return nil
		}
		return m.promptReset(entry.OID, entry.Subject)
	case "s":
		entry, ok := r.current()
		if !ok {
			return nil
		}
		return m.confirmSwitchDetached(entry.OID, entry.Subject)
	case "y":
		entry, ok := r.current()
		if !ok {
			return nil
		}
		if err := (systemClipboard{}).Write(entry.OID); err != nil {
			m.notice = "Clipboard: " + err.Error()
			return nil
		}
		m.notice = "Copied " + entry.Short + " to the clipboard"
		return nil
	case "enter":
		switch m.focus {
		case 0:
			m.focus = 1
			return nil
		case 1:
			entry, ok := r.current()
			if !ok {
				return nil
			}
			return m.jumpToRef(entry.OID)
		default:
			return m.loadReflogFileDiff()
		}
	case "j", "down", "k", "up", "pgdown", "pgup", "home", "end":
		delta := 1
		if key == "k" || key == "up" || key == "pgup" {
			delta = -1
		}
		if key == "pgdown" || key == "pgup" {
			delta *= max(1, m.historyPaneHeight()-2)
		}
		switch m.focus {
		case 0:
			list := r.visible()
			if len(list) == 0 {
				return nil
			}
			r.index = min(max(0, r.index+delta), len(list)-1)
			return m.loadReflogDetail()
		case 1:
			if !r.detailOK || len(r.files) == 0 {
				if delta < 0 {
					r.inspect.ScrollUp(-delta)
				} else {
					r.inspect.ScrollDown(delta)
				}
				return nil
			}
			r.fileIndex = min(max(0, r.fileIndex+delta), len(r.files)-1)
			return nil
		default:
			if delta < 0 {
				r.inspect.ScrollUp(-delta)
			} else {
				r.inspect.ScrollDown(delta)
			}
			return nil
		}
	}
	return nil
}

// promptRecoveryBranch creates a new branch at the selected reflog entry. It is
// the safest recovery action, so it is the one given a direct key.
func (m *Model) promptRecoveryBranch() tea.Cmd {
	entry, ok := m.reflog.current()
	if !ok {
		return nil
	}
	label := entry.Short
	if entry.Subject != "" {
		label += " · " + clip(safeText(entry.Subject), 40)
	}
	m.prompt = &promptState{
		title:   "recovery branch",
		label:   "Create a branch at " + label,
		help:    "Enter creates · Ctrl-S creates and switches · Esc cancels",
		kind:    promptCreateBranch,
		context: entry.OID,
	}
	return nil
}
