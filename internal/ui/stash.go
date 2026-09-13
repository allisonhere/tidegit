package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/allisonhere/tidegit/internal/diff"
	"github.com/allisonhere/tidegit/internal/git"
	"github.com/allisonhere/tideui"
	tea "github.com/charmbracelet/bubbletea"
)

// stashState backs the Stash screen. The list, the selected stash's files and
// one file's patch are three panes of one selection, so a stash mutation
// reloads the list and re-derives the rest.
type stashState struct {
	stashes     []git.Stash
	index, top  int
	loading     bool
	err         string
	selectedOID string // identity preserved across a renumbering drop/pop

	filter    string
	filtering bool

	files        []git.FileChange
	fileIndex    int
	fileTop      int
	filesFor     string
	filesLoading bool

	view        diffView
	diffLabel   string
	diffLoading bool

	inspect tideui.PaneScroller
}

func (s *stashState) visible() []git.Stash {
	if s.filter == "" {
		return s.stashes
	}
	var out []git.Stash
	for _, stash := range s.stashes {
		haystack := strings.ToLower(stash.Message + " " + stash.Branch + " " + stash.Ref)
		if strings.Contains(haystack, strings.ToLower(s.filter)) {
			out = append(out, stash)
		}
	}
	return out
}

func (s *stashState) current() (git.Stash, bool) {
	list := s.visible()
	if s.index < 0 || s.index >= len(list) {
		return git.Stash{}, false
	}
	return list[s.index], true
}

type stashesMsg struct {
	id      int
	stashes []git.Stash
	err     error
}
type stashFilesMsg struct {
	id    int
	ref   string
	files []git.FileChange
	err   error
}
type stashDiffMsg struct {
	id    int
	ref   string
	patch diff.Patch
	label string
	err   error
}

func (m *Model) openStash() tea.Cmd {
	if m.stash == nil {
		m.stash = &stashState{}
	}
	m.screen = screenStash
	m.focus = 0
	m.notice = ""
	return m.loadStashes()
}

func (m *Model) loadStashes() tea.Cmd {
	if m.stash == nil {
		m.stash = &stashState{}
	}
	m.stash.loading = true
	m.stash.err = ""
	m.stashID++
	id, repo := m.stashID, m.repo
	return tea.Batch(func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
		defer cancel()
		stashes, err := repo.Stashes(ctx)
		return stashesMsg{id: id, stashes: stashes, err: err}
	}, pulse())
}

// loadStashDetail reads the selected stash's changed files, then the selected
// file's patch. It is skipped when the same stash is already loaded.
func (m *Model) loadStashDetail() tea.Cmd {
	s := m.stash
	if s == nil {
		return nil
	}
	stash, ok := s.current()
	if !ok {
		s.files, s.filesFor, s.filesLoading = nil, "", false
		return nil
	}
	if s.filesFor == stash.Ref {
		return nil
	}
	m.stashDetailID++
	id, repo, ref := m.stashDetailID, m.repo, stash.Ref
	s.filesLoading = true
	s.files, s.filesFor = nil, ""
	s.fileIndex, s.fileTop = 0, 0
	return tea.Batch(func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
		defer cancel()
		files, err := repo.StashFiles(ctx, ref)
		return stashFilesMsg{id: id, ref: ref, files: files, err: err}
	}, pulse())
}

func (m *Model) loadStashFileDiff() tea.Cmd {
	s := m.stash
	stash, ok := s.current()
	if !ok || len(s.files) == 0 {
		s.diffLabel, s.diffLoading = "", false
		return nil
	}
	s.fileIndex = min(max(0, s.fileIndex), len(s.files)-1)
	file := s.files[s.fileIndex]
	m.stashDiffID++
	id, repo := m.stashDiffID, m.repo
	s.diffLoading = true
	ref := stash.Ref
	s.diffLabel = fmt.Sprintf("Stash %s · %s", ref, safeText(file.Path))
	ctxLines, whitespace := m.diffContext(), m.whitespaceMode()
	return tea.Batch(func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
		defer cancel()
		d, err := repo.StashDiffWith(ctx, ref, file, git.DiffOptions{Context: ctxLines, Whitespace: whitespace})
		patch := diff.Parse(d.Patch)
		patch.Label = ref
		if strings.Count(d.Patch, "\n") > 20000 {
			d.Hunks = nil
			d.HunkUnavailable = "Hunk actions unavailable: preview exceeds 20,000 lines."
		}
		return stashDiffMsg{id: id, ref: ref, patch: patch, label: s.diffLabel, err: err}
	}, pulse())
}

func (m *Model) handleStashResult(msg tea.Msg) (bool, tea.Cmd) {
	s := m.stash
	switch msg := msg.(type) {
	case stashesMsg:
		if s == nil || msg.id != m.stashID {
			return true, nil
		}
		s.loading = false
		if msg.err != nil {
			s.err = msg.err.Error()
			s.stashes, s.files = nil, nil
			return true, nil
		}
		s.err = ""
		s.stashes = msg.stashes
		// Preserve the selected stash by its commit id: indices shift after a
		// drop or pop, and acting on a stale index would be wrong.
		if s.selectedOID != "" {
			restored := false
			for i, stash := range s.visible() {
				if stash.OID == s.selectedOID {
					s.index, restored = i, true
					break
				}
			}
			if !restored {
				s.index = min(s.index, max(0, len(s.visible())-1))
			}
		} else {
			s.index = min(s.index, max(0, len(s.visible())-1))
		}
		if stash, ok := s.current(); ok {
			s.selectedOID = stash.OID
		}
		s.filesFor = ""
		if m.screen != screenStash {
			return true, nil
		}
		return true, m.loadStashDetail()
	case stashFilesMsg:
		if s == nil || msg.id != m.stashDetailID {
			return true, nil
		}
		s.filesLoading = false
		if msg.err != nil {
			s.err = msg.err.Error()
			return true, nil
		}
		s.files, s.filesFor = msg.files, msg.ref
		return true, m.loadStashFileDiff()
	case stashDiffMsg:
		if s == nil || msg.id != m.stashDiffID {
			return true, nil
		}
		s.diffLoading = false
		if msg.err != nil {
			s.err = msg.err.Error()
			return true, nil
		}
		s.view.reset(msg.patch, msg.patch.Label)
		s.diffLabel = msg.label
		return true, nil
	}
	return false, nil
}

func (m *Model) updateStashKey(key string, msg tea.KeyMsg) tea.Cmd {
	s := m.stash
	if s.filtering {
		switch key {
		case "enter":
			s.filtering = false
			return nil
		case "esc":
			s.filtering = false
			s.filter = ""
			s.index = 0
		case "backspace":
			runes := []rune(s.filter)
			if len(runes) > 0 {
				s.filter = string(runes[:len(runes)-1])
			}
		default:
			if msg.Type == tea.KeyRunes {
				s.filter += string(msg.Runes)
			}
		}
		s.index = min(s.index, max(0, len(s.visible())-1))
		return m.loadStashDetail()
	}
	switch key {
	case "/":
		m.focus = 0
		s.filtering = true
		return nil
	case "n":
		return m.promptStash(false)
	case "N":
		return m.promptStash(true)
	case "a":
		return m.applyStash()
	case "p":
		return m.popStash()
	case "d":
		return m.confirmDropStash()
	case "y":
		stash, ok := s.current()
		if !ok {
			return nil
		}
		if err := (systemClipboard{}).Write(stash.OID); err != nil {
			m.notice = "Clipboard: " + err.Error()
			return nil
		}
		m.notice = "Copied " + stash.Short + " to the clipboard"
		return nil
	case "enter":
		if m.focus < 2 {
			m.focus++
		}
		return nil
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
			list := s.visible()
			if len(list) == 0 {
				return nil
			}
			s.index = min(max(0, s.index+delta), len(list)-1)
			if stash, ok := s.current(); ok {
				s.selectedOID = stash.OID
			}
			return m.loadStashDetail()
		case 1:
			if len(s.files) > 0 {
				s.fileIndex = min(max(0, s.fileIndex+delta), len(s.files)-1)
				return m.loadStashFileDiff()
			}
			return nil
		default:
			flat := s.view.flat(m.diffOptionsFrom(), true)
			if delta < 0 {
				s.view.line = max(0, s.view.line+delta)
			} else {
				s.view.line = min(s.view.line+delta, max(0, len(flat)-1))
			}
			s.view.ensureVisible(m.historyPaneHeight() - 2)
			return nil
		}
	}
	return nil
}

// stashChanges creates a stash and reports through the shared operation panel.
func (m *Model) stashChanges(message string, includeUntracked bool) tea.Cmd {
	if m.busy || m.loading {
		return nil
	}
	if m.stash == nil {
		m.stash = &stashState{}
	}
	kind := "tracked changes"
	if includeUntracked {
		kind = "all changes including untracked files"
	}
	op := &operationState{kind: "stash", verb: "Stashing", title: "Stash " + kind,
		target: "working tree", cancellable: false}
	repo := m.repo
	return m.beginOperation(op, func(ctx context.Context, progress git.ProgressFunc) (git.Result, error) {
		return repo.StashCreate(ctx, message, includeUntracked)
	})
}

func (m *Model) applyStash() tea.Cmd {
	stash, ok := m.stash.current()
	if !ok || m.busy {
		return nil
	}
	op := &operationState{kind: "stash", verb: "Applying", title: "Apply " + stash.Ref,
		target: stash.Ref, cancellable: false}
	op.retry = m.applyStash
	repo := m.repo
	return m.beginOperation(op, func(ctx context.Context, progress git.ProgressFunc) (git.Result, error) {
		return repo.StashApply(ctx, stash.Ref)
	})
}

func (m *Model) popStash() tea.Cmd {
	stash, ok := m.stash.current()
	if !ok || m.busy {
		return nil
	}
	op := &operationState{kind: "stash", verb: "Popping", title: "Pop " + stash.Ref,
		target: stash.Ref, cancellable: false}
	op.retry = m.popStash
	repo := m.repo
	return m.beginOperation(op, func(ctx context.Context, progress git.ProgressFunc) (git.Result, error) {
		return repo.StashPop(ctx, stash.Ref)
	})
}

// confirmDropStash asks before a destructive drop, naming the selected stash so
// the wrong one cannot be removed by a keystroke at a resting cursor.
func (m *Model) confirmDropStash() tea.Cmd {
	stash, ok := m.stash.current()
	if !ok {
		return nil
	}
	m.confirm = &confirmState{
		title:  "drop stash?",
		body:   "Drop " + stash.Ref + ": " + safeText(clip(stash.Message, 40)) + "?\n\nThis discards the stashed changes.\nIt cannot be undone from TideGit.",
		accept: "d",
		danger: true,
		kind:   confirmDropStash,
		target: stash.Ref,
	}
	return nil
}

func (m *Model) dropStash(ref string) tea.Cmd {
	op := &operationState{kind: "stash", verb: "Dropping", title: "Drop " + ref,
		target: ref, cancellable: false}
	repo := m.repo
	return m.beginOperation(op, func(ctx context.Context, progress git.ProgressFunc) (git.Result, error) {
		return repo.StashDrop(ctx, ref)
	})
}

// promptStash asks for an optional message. The stash prompt is the one place
// an empty value is submitted deliberately.
func (m *Model) promptStash(includeUntracked bool) tea.Cmd {
	kind := "tracked changes"
	if includeUntracked {
		kind = "tracked and untracked files"
	}
	m.prompt = &promptState{
		title:   "stash changes",
		label:   "Save " + kind + " · message is optional",
		help:    "Enter stashes · Esc cancels",
		kind:    promptStashMessage,
		context: map[bool]string{true: "untracked", false: "tracked"}[includeUntracked],
	}
	return nil
}
