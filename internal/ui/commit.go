package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/allisonhere/ripple"
	"github.com/allisonhere/tidegit/internal/git"
	"github.com/allisonhere/tideui"
	tea "github.com/charmbracelet/bubbletea"
)

type commitState struct {
	editor                                          ripple.Model
	info                                            git.CommitInfo
	initial                                         string
	amend, signoff, confirmCancel, showOutput, help bool
	focus, selected                                 int
	errorText                                       string
	outputScroll                                    tideui.PaneScroller
	refreshing                                      bool
	preview                                         []diffLine
	previewLoading                                  bool
}
type commitPreviewMsg struct {
	id    int
	lines []diffLine
	err   error
}

func (m *Model) loadCommitPreview() tea.Cmd {
	c := m.compose
	if c == nil {
		return nil
	}
	if m.diffCancel != nil {
		m.diffCancel()
	}
	m.diffID++
	c.preview = nil
	c.previewLoading = false
	files := c.info.Status.Groups[git.Staged]
	if len(files) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
	m.diffCancel = cancel
	id, repo, file := m.diffID, m.repo, files[c.selected]
	c.previewLoading = true
	return func() tea.Msg {
		defer cancel()
		d, err := repo.Diff(ctx, git.Staged, file)
		return commitPreviewMsg{id, diffLines(d), err}
	}
}

type commitPreparedMsg struct {
	info    git.CommitInfo
	amend   bool
	err     error
	refresh bool
}
type commitFinishedMsg struct {
	result          git.CommitResult
	status          git.Status
	err, refreshErr error
}
type pulseMsg struct{}

func pulse() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return pulseMsg{} })
}

func (m *Model) openCommit(amend bool) tea.Cmd {
	if m.busy || m.loading || m.opening || m.compose != nil {
		return nil
	}
	m.opening = true
	m.notice = "Preparing commit review"
	if m.diffCancel != nil {
		m.diffCancel()
	}
	m.diffID++
	m.diffLoading = false
	repo, ctx := m.repo, m.ctx
	return tea.Batch(func() tea.Msg {
		c, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		info, err := repo.PrepareCommit(c, amend)
		return commitPreparedMsg{info: info, amend: amend, err: err}
	}, pulse())
}
func (m *Model) refreshCommit() tea.Cmd {
	c := m.compose
	if c == nil || m.busy || c.refreshing {
		return nil
	}
	c.refreshing = true
	repo, ctx, amend := m.repo, m.ctx, c.amend
	return tea.Batch(func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		info, err := repo.PrepareCommit(ctx, amend)
		return commitPreparedMsg{info: info, amend: amend, err: err, refresh: true}
	}, pulse())
}
func (m *Model) submitCommit() tea.Cmd {
	c := m.compose
	if c == nil || m.busy || c.refreshing {
		return nil
	}
	if strings.TrimSpace(c.editor.Value()) == "" {
		c.errorText = "Write a commit message before submitting."
		return nil
	}
	m.busy = true
	m.operation = "Committing · running Git hooks and signing"
	c.errorText = ""
	c.showOutput = false
	opts := git.CommitOptions{Message: c.editor.Value(), Amend: c.amend, Signoff: c.signoff, ExpectedHead: c.info.Status.OID, ExpectedIndex: c.info.IndexToken}
	repo, ctx := m.repo, m.ctx
	return tea.Batch(func() tea.Msg {
		result, err := repo.Commit(ctx, opts)
		refreshCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		status, scanErr := repo.RepositoryStatus(refreshCtx)
		return commitFinishedMsg{result, status, err, scanErr}
	}, pulse())
}

func (m *Model) handleCommitResult(msg tea.Msg) (bool, tea.Cmd) {
	switch msg := msg.(type) {
	case commitPreviewMsg:
		if m.compose != nil && msg.id == m.diffID {
			m.compose.previewLoading = false
			if msg.err != nil {
				m.compose.errorText = msg.err.Error()
			} else {
				m.compose.preview = msg.lines
			}
		}
		return true, nil
	case omarchyThemeTickMsg:
		return true, m.handleOmarchyTick()
	case pulseMsg:
		m.frame++
		if m.busy || m.opening || m.loading || m.diffLoading || m.compose != nil && m.compose.refreshing {
			return true, pulse()
		}
		if h := m.history; h != nil && (h.loading || h.loadingMore || h.detailLoading || h.diffLoading) {
			return true, pulse()
		}
		if b := m.branches; b != nil && (b.loading || b.commitsLoading || b.extraLoading) {
			return true, pulse()
		}
		return true, nil
	case commitPreparedMsg:
		m.opening = false
		if msg.refresh {
			if m.compose == nil {
				return true, nil
			}
			m.compose.refreshing = false
			if msg.err != nil {
				m.compose.errorText = msg.err.Error()
				return true, nil
			}
			m.compose.info = msg.info
			m.compose.selected = min(m.compose.selected, max(0, len(msg.info.Status.Groups[git.Staged])-1))
			m.compose.errorText = ""
			return true, m.loadCommitPreview()
		}
		if msg.err != nil {
			m.notice = msg.err.Error()
			return true, m.loadDiff()
		}
		ed := ripple.New()
		ed.SetClipboard(systemClipboard{})
		ed.SetValue(msg.info.Message)
		ed.SetPlaceholder("Summarize this change…")
		ed.Focus()
		m.compose = &commitState{editor: ed, info: msg.info, initial: ed.Value(), amend: msg.amend}
		m.sizeEditor()
		m.notice = ""
		return true, m.loadCommitPreview()
	case commitFinishedMsg:
		m.busy = false
		m.operation = ""
		if m.compose == nil {
			return true, nil
		}
		if msg.err != nil {
			m.compose.errorText = msg.err.Error()
			if msg.refreshErr != nil {
				m.compose.errorText += "\nRefresh: " + msg.refreshErr.Error()
			}
			m.compose.outputScroll.ScrollToTop()
			return true, nil
		}
		m.compose = nil
		m.help = false
		m.notice = fmt.Sprintf("Committed %s  %s", msg.result.OID[:min(7, len(msg.result.OID))], safeText(msg.result.Subject))
		if msg.result.Warning != "" {
			m.notice = msg.result.Warning
		}
		if msg.refreshErr != nil {
			m.setError(fmt.Errorf("commit succeeded; status refresh failed: %w", msg.refreshErr))
			return true, nil
		}
		m.status = msg.status
		m.selectPath("", m.section)
		return true, m.loadDiff()
	}
	return false, nil
}

func meaningfulDraft(text, comment string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) != "" && !strings.HasPrefix(line, comment) {
			return true
		}
	}
	return false
}
func (m *Model) cancelCommit() tea.Cmd {
	c := m.compose
	if c == nil || m.busy || c.refreshing {
		return nil
	}
	if c.editor.Value() != c.initial && meaningfulDraft(c.editor.Value(), c.info.CommentPrefix) {
		c.confirmCancel = true
		return nil
	}
	m.compose = nil
	return m.loadDiff()
}
func (m *Model) updateCommit(msg tea.Msg) tea.Cmd {
	c := m.compose
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.width, m.height = size.Width, size.Height
		m.sizeEditor()
		return nil
	}
	if key, ok := msg.(tea.KeyMsg); ok {
		k := key.String()
		// Ripple owns Ctrl-C as copy while editing. A commit has no deadline, so
		// an unattended hook or signing prompt would otherwise trap the program:
		// while Git runs, Ctrl-C leaves instead. The draft is the only loss.
		if k == "ctrl+c" && (m.busy || c.refreshing) {
			return tea.Quit
		}
		if (m.width < 54 || m.height < 16) && k != "esc" && k != "ctrl+q" {
			return nil
		}
		if c.confirmCancel {
			switch k {
			case "ctrl+d":
				m.compose = nil
				return m.loadDiff()
			case "esc", "enter":
				c.confirmCancel = false
			}
			return nil
		}
		if c.help {
			if k == "f1" || k == "esc" {
				c.help = false
			}
			return nil
		}
		if c.showOutput {
			switch k {
			case "f6", "esc":
				c.showOutput = false
			case "j", "down":
				c.outputScroll.ScrollDown(1)
			case "k", "up":
				c.outputScroll.ScrollUp(1)
			case "pgdown":
				c.outputScroll.ScrollDown(10)
			case "pgup":
				c.outputScroll.ScrollUp(10)
			}
			return nil
		}
		if k == "f6" {
			c.showOutput = true
			return nil
		}
		if m.busy || c.refreshing {
			return nil
		}
		switch k {
		case "ctrl+s":
			return m.submitCommit()
		case "ctrl+r":
			return m.refreshCommit()
		case "ctrl+o":
			c.signoff = !c.signoff
			return nil
		case "esc", "ctrl+q":
			return m.cancelCommit()
		case "f1":
			c.help = true
			return nil
		case "tab", "shift+tab":
			c.focus = 1 - c.focus
			if c.focus == 0 {
				c.editor.Focus()
			} else {
				c.editor.Blur()
			}
			return nil
		}
		if c.focus == 1 {
			old := c.selected
			switch k {
			case "j", "down":
				c.selected = min(max(0, len(c.info.Status.Groups[git.Staged])-1), c.selected+1)
			case "k", "up":
				c.selected = max(0, c.selected-1)
			}
			if c.selected != old {
				return m.loadCommitPreview()
			}
			return nil
		}
	}
	if copied, ok := msg.(ripple.CopiedMsg); ok && copied.Err != nil {
		c.errorText = "Clipboard: " + copied.Err.Error()
		return nil
	}
	if paste, ok := msg.(ripple.PasteMsg); ok && paste.Err != nil {
		c.errorText = "Clipboard: " + paste.Err.Error()
		return nil
	}
	if _, ok := msg.(ripple.SubmitMsg); ok {
		return m.submitCommit()
	}
	if _, ok := msg.(ripple.CancelMsg); ok {
		return m.cancelCommit()
	}
	var cmd tea.Cmd
	c.editor, cmd = c.editor.Update(msg)
	return cmd
}

func (m *Model) editorDimensions() (int, int) {
	w := m.width - 4
	if m.width >= 100 {
		w = m.width*7/10 - 4
	}
	return max(1, w-2), max(1, m.height-14)
}
func (m *Model) sizeEditor() {
	if m.compose != nil {
		w, h := m.editorDimensions()
		m.compose.editor.SetSize(w, h)
	}
}
