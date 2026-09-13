package ui

import (
	"context"
	"fmt"
	"time"

	"github.com/allisonhere/tidegit/internal/git"
	tea "github.com/charmbracelet/bubbletea"
)

// Action is independent of bindings so the future palette can dispatch it too.
type Action int

const (
	StageFile Action = iota
	UnstageFile
	StageHunk
	UnstageHunk
)

type actionMsg struct {
	status          git.Status
	err, refreshErr error
	path            string
	preferred       int
	notice          string
	position        viewPosition
}
type viewPosition struct{ scroll, hunk int }

func (m *Model) act(action Action) tea.Cmd {
	if m.busy || m.loading || m.err != "" {
		return nil
	}
	files := m.files()
	if len(files) == 0 {
		return nil
	}
	file := files[m.selected]
	reverse := action == UnstageFile || action == UnstageHunk
	partial := action == StageHunk || action == UnstageHunk
	if reverse && m.section != int(git.Staged) || !reverse && m.section != int(git.Unstaged) && m.section != int(git.Untracked) {
		m.notice = "Select " + map[bool]string{true: "a Staged", false: "an Unstaged or Untracked"}[reverse] + " file first"
		return nil
	}
	var hunk git.Hunk
	if partial {
		if m.diffLoading {
			return nil
		}
		if len(m.diff.Hunks) == 0 {
			m.notice = m.diff.HunkUnavailable
			return nil
		}
		hunk = m.diff.Hunks[m.hunk]
	}
	if m.scanCancel != nil {
		m.scanCancel()
	}
	if m.diffCancel != nil {
		m.diffCancel()
	}
	m.scanID++
	m.diffID++
	m.diffLoading = false
	m.busy = true
	m.notice = ""
	verb := "Staged"
	m.operation = "Staging"
	preferred := int(git.Staged)
	if reverse {
		verb = "Unstaged"
		m.operation = "Unstaging"
		preferred = int(git.Unstaged)
	}
	notice := verb + " " + safeText(file.Path)
	if partial {
		notice = fmt.Sprintf("%s hunk %d of %s", verb, m.hunk+1, safeText(file.Path))
		preferred = m.section
	}
	m.operation += " " + safeText(file.Path)
	position := viewPosition{m.scroll.Offset(), m.hunk}
	repo := m.repo
	ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
	return func() tea.Msg {
		defer cancel()
		var err error
		switch action {
		case StageFile:
			err = repo.StageFile(ctx, file)
		case UnstageFile:
			err = repo.UnstageFile(ctx, file)
		case StageHunk:
			err = repo.StageHunk(ctx, hunk)
		case UnstageHunk:
			err = repo.UnstageHunk(ctx, hunk)
		}
		// Even a failed/cancelled command may have completed its index write.
		// Rescan with a fresh deadline, keeping the UI mutation gate closed.
		refreshCtx, refreshCancel := context.WithTimeout(m.ctx, 30*time.Second)
		defer refreshCancel()
		s, scanErr := repo.RepositoryStatus(refreshCtx)
		return actionMsg{s, err, scanErr, file.Path, preferred, notice, position}
	}
}

func (m *Model) selectPath(path string, preferred int) {
	// Prefer the requested destination; otherwise keep the same logical file
	// in any group (including a rename's old path after whole-file unstage).
	order := []int{preferred, m.section, 0, 1, 2, 3}
	for _, section := range order {
		for _, f := range m.status.Groups[section] {
			if path != "" && (f.Path == path || f.OriginalPath == path) {
				m.section = section
				for i, visible := range m.files() {
					if visible.Path == f.Path {
						m.selected = i
						return
					}
				}
			}
		}
	}
	m.selected = min(m.selected, max(0, len(m.files())-1))
	if len(m.files()) == 0 && m.query == "" {
		for i, g := range m.status.Groups {
			if len(g) > 0 {
				m.section = i
				m.selected = 0
				return
			}
		}
	}
}

func (m *Model) moveHunk(delta int) {
	if m.diffLoading || m.err != "" || len(m.diff.Hunks) == 0 {
		return
	}
	m.focus = 2
	m.hunk = min(len(m.diff.Hunks)-1, max(0, m.hunk+delta))
	m.scroll.ScrollToTop()
	m.scroll.ScrollDown(m.diff.Hunks[m.hunk].PatchLine)
	m.scroll.ClampTo(len(m.lines), max(1, m.height-5))
}
