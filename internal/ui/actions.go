package ui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
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
type viewPosition struct{ scroll, hunk, line int }

// markKey identifies a marked file by its group and path. A file can appear in
// two groups at once, so the section is part of the identity.
func markKey(section int, path string) string { return strconv.Itoa(section) + "\x00" + path }

func (m *Model) isMarked(section int, path string) bool {
	return m.marked[markKey(section, path)]
}

// markedCount is the number of marked files in a section.
func (m *Model) markedCount(section int) int {
	prefix := strconv.Itoa(section) + "\x00"
	n := 0
	for key := range m.marked {
		if strings.HasPrefix(key, prefix) {
			n++
		}
	}
	return n
}

// markedFiles returns the marked, currently visible files of a section.
func (m *Model) markedFiles(section int) []git.File {
	if len(m.marked) == 0 {
		return nil
	}
	var out []git.File
	for _, f := range m.files() {
		if m.isMarked(section, f.Path) {
			out = append(out, f)
		}
	}
	return out
}

// toggleMark marks or unmarks the selected file.
func (m *Model) toggleMark() {
	if m.focus != 1 {
		m.focus = 1
	}
	files := m.files()
	if len(files) == 0 {
		return
	}
	if m.marked == nil {
		m.marked = map[string]bool{}
	}
	key := markKey(m.section, files[m.selected].Path)
	if m.marked[key] {
		delete(m.marked, key)
		return
	}
	m.marked[key] = true
}

func (m *Model) clearMarksFor(section int, files []git.File) {
	for _, f := range files {
		delete(m.marked, markKey(section, f.Path))
	}
}

func (m *Model) clearSectionMarks(section int) {
	prefix := strconv.Itoa(section) + "\x00"
	for key := range m.marked {
		if strings.HasPrefix(key, prefix) {
			delete(m.marked, key)
		}
	}
}

// act performs one action. File actions operate on the marked set when one
// exists, otherwise on the selected file; hunk actions always use the selected
// file's current hunk.
func (m *Model) act(action Action) tea.Cmd {
	if m.busy || m.loading || m.opening || m.err != "" {
		return nil
	}
	if action == StageHunk || action == UnstageHunk {
		return m.actHunk(action)
	}
	reverse := action == UnstageFile
	if reverse && m.section != int(git.Staged) || !reverse && m.section != int(git.Unstaged) && m.section != int(git.Untracked) {
		m.notice = "Select " + map[bool]string{true: "a Staged", false: "an Unstaged or Untracked"}[reverse] + " file first"
		return nil
	}
	targets := m.markedFiles(m.section)
	if len(targets) == 0 {
		files := m.files()
		if len(files) == 0 {
			return nil
		}
		targets = []git.File{files[m.selected]}
	}
	return m.actFiles(action, targets)
}

// actFiles stages or unstages a batch under one mutation gate and one rescan.
func (m *Model) actFiles(action Action, files []git.File) tea.Cmd {
	if len(files) == 0 || m.busy || m.loading || m.opening || m.err != "" {
		return nil
	}
	reverse := action == UnstageFile
	if reverse && m.section != int(git.Staged) || !reverse && m.section != int(git.Unstaged) && m.section != int(git.Untracked) {
		m.notice = "Select " + map[bool]string{true: "a Staged", false: "an Unstaged or Untracked"}[reverse] + " file first"
		return nil
	}
	targets := append([]git.File(nil), files...)
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
	m.operation += " " + plural(len(targets), "file")
	position := viewPosition{m.view.scroll.Offset(), m.view.hunk, m.view.line}
	anchor := targets[0].Path
	m.clearMarksFor(m.section, targets)
	repo := m.repo
	ctx, cancel := context.WithTimeout(m.ctx, 60*time.Second)
	return tea.Batch(func() tea.Msg {
		defer cancel()
		failed := 0
		var firstErr error
		for _, f := range targets {
			var err error
			if reverse {
				err = repo.UnstageFile(ctx, f)
			} else {
				err = repo.StageFile(ctx, f)
			}
			if err != nil {
				failed++
				if firstErr == nil {
					firstErr = fmt.Errorf("%s: %w", safeText(f.Path), err)
				}
			}
		}
		// Even a failed or cancelled command may have completed an index write,
		// so the repository is always rescanned before anything is shown.
		refreshCtx, refreshCancel := context.WithTimeout(m.ctx, 30*time.Second)
		defer refreshCancel()
		s, scanErr := repo.RepositoryStatus(refreshCtx)
		notice := fmt.Sprintf("%s %s", verb, plural(len(targets), "file"))
		if failed > 0 {
			notice = fmt.Sprintf("%s %d of %d files", verb, len(targets)-failed, len(targets))
			if failed > 1 {
				firstErr = fmt.Errorf("%w (and %d more failed)", firstErr, failed-1)
			}
		}
		return actionMsg{s, firstErr, scanErr, anchor, preferred, notice, position}
	}, pulse())
}

// actHunk stages or unstages the selected hunk of the selected file.
func (m *Model) actHunk(action Action) tea.Cmd {
	reverse := action == UnstageHunk
	if reverse && m.section != int(git.Staged) || !reverse && m.section != int(git.Unstaged) && m.section != int(git.Untracked) {
		m.notice = "Select " + map[bool]string{true: "a Staged", false: "an Unstaged or Untracked"}[reverse] + " file first"
		return nil
	}
	files := m.files()
	if len(files) == 0 {
		return nil
	}
	file := files[m.selected]
	if m.diffLoading {
		return nil
	}
	if len(m.diff.Hunks) == 0 {
		m.notice = m.diff.HunkUnavailable
		return nil
	}
	hunk := m.diff.Hunks[m.view.hunk]
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
	preferred := m.section
	if reverse {
		verb = "Unstaged"
		m.operation = "Unstaging"
	}
	notice := fmt.Sprintf("%s hunk %d of %s", verb, m.view.hunk+1, safeText(file.Path))
	m.operation += " " + safeText(file.Path)
	position := viewPosition{m.view.scroll.Offset(), m.view.hunk, m.view.line}
	target := hunk
	repo := m.repo
	ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
	return tea.Batch(func() tea.Msg {
		defer cancel()
		var err error
		if reverse {
			err = repo.UnstageHunk(ctx, target)
		} else {
			err = repo.StageHunk(ctx, target)
		}
		refreshCtx, refreshCancel := context.WithTimeout(m.ctx, 30*time.Second)
		defer refreshCancel()
		s, scanErr := repo.RepositoryStatus(refreshCtx)
		return actionMsg{s, err, scanErr, file.Path, preferred, notice, position}
	}, pulse())
}

// stageAllInSection stages every visible file in the current stageable section.
func (m *Model) stageAllInSection() tea.Cmd {
	if m.section != int(git.Unstaged) && m.section != int(git.Untracked) {
		m.notice = "Select the Unstaged or Untracked section to stage all"
		return nil
	}
	files := m.files()
	if len(files) == 0 {
		m.notice = "Nothing to stage"
		return nil
	}
	return m.actFiles(StageFile, files)
}

// unstageAllInSection unstages every visible file in the Staged section.
func (m *Model) unstageAllInSection() tea.Cmd {
	if m.section != int(git.Staged) {
		m.notice = "Select the Staged section to unstage all"
		return nil
	}
	files := m.files()
	if len(files) == 0 {
		m.notice = "Nothing to unstage"
		return nil
	}
	return m.actFiles(UnstageFile, files)
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
	m.view.moveHunk(delta, m.diffOptionsFrom())
	m.hunk = m.view.hunk
}
