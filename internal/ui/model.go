package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/allisonhere/tidegit/internal/git"
	"github.com/allisonhere/tideui"
	tea "github.com/charmbracelet/bubbletea"
)

type statusMsg struct {
	id     int
	repo   git.Repository
	status git.Status
	err    error
}
type diffMsg struct {
	id       int
	diff     git.Diff
	lines    []diffLine
	err      error
	position *viewPosition
}

type Model struct {
	ctx                                     context.Context
	path                                    string
	repo                                    git.Repository
	status                                  git.Status
	width, height, focus, section, selected int
	scanID, diffID                          int
	scanCancel, diffCancel                  context.CancelFunc
	loading, diffLoading                    bool
	err                                     string
	diff                                    git.Diff
	lines                                   []diffLine
	horizontal                              int
	scroll                                  tideui.PaneScroller
	query                                   string
	filtering, help, expanded               bool
	theme                                   tideui.Theme
	busy                                    bool
	operation, notice                       string
	hunk                                    int
	restorePosition                         *viewPosition
}

func New(ctx context.Context, path string, theme tideui.Theme) *Model {
	return &Model{ctx: ctx, path: path, theme: theme, width: 100, height: 28}
}
func (m *Model) Init() tea.Cmd { return m.refresh() }

func (m *Model) setError(err error) {
	m.err = err.Error()
	m.scroll.ScrollToTop()
	m.horizontal = 0
	m.lines = []diffLine{{"Git could not complete the request:", '!'}}
	for _, line := range strings.Split(m.err, "\n") {
		m.lines = append(m.lines, diffLine{safeText(line), '!'})
	}
	m.lines = append(m.lines, diffLine{"Press r to retry. Open another repository with tidegit PATH.", ' '})
}
func (m *Model) refresh() tea.Cmd {
	if m.busy {
		return nil
	}
	if m.scanCancel != nil {
		m.scanCancel()
	}
	if m.diffCancel != nil {
		m.diffCancel()
	}
	m.scanID++
	m.diffID++
	m.loading = true
	m.diffLoading = false
	m.err = ""
	m.notice = ""
	m.restorePosition = &viewPosition{m.scroll.Offset(), m.hunk}
	ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
	m.scanCancel = cancel
	id, path := m.scanID, m.path
	return func() tea.Msg {
		defer cancel()
		r, err := git.Discover(ctx, path)
		var s git.Status
		if err == nil {
			s, err = r.RepositoryStatus(ctx)
		}
		return statusMsg{id, r, s, err}
	}
}
func (m *Model) files() []git.File {
	files := m.status.Groups[m.section]
	if m.query == "" {
		return files
	}
	filtered := make([]git.File, 0, len(files))
	for _, f := range files {
		if strings.Contains(strings.ToLower(f.Path), strings.ToLower(m.query)) {
			filtered = append(filtered, f)
		}
	}
	return filtered
}
func (m *Model) loadDiff() tea.Cmd {
	if m.diffCancel != nil {
		m.diffCancel()
	}
	m.diffID++
	m.diff = git.Diff{}
	m.lines = nil
	m.horizontal = 0
	m.hunk = 0
	m.scroll.ScrollToTop()
	m.diffLoading = false
	files := m.files()
	m.selected = min(m.selected, max(0, len(files)-1))
	if len(files) == 0 || m.loading || m.busy {
		return nil
	}
	ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
	m.diffCancel = cancel
	id, repo, section, file := m.diffID, m.repo, git.Section(m.section), files[m.selected]
	m.diffLoading = true
	m.err = ""
	position := m.restorePosition
	m.restorePosition = nil
	return func() tea.Msg {
		defer cancel()
		d, err := repo.Diff(ctx, section, file)
		if strings.Count(d.Patch, "\n") > 20000 {
			d.Hunks = nil
			d.HunkUnavailable = "Hunk actions unavailable: preview exceeds 20,000 lines."
		}
		return diffMsg{id: id, diff: d, lines: diffLines(d), err: err, position: position}
	}
}
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case actionMsg:
		m.busy = false
		m.operation = ""
		if msg.refreshErr != nil {
			m.setError(fmt.Errorf("%s; repository refresh failed: %w", msg.notice, msg.refreshErr))
			if msg.err != nil {
				m.setError(fmt.Errorf("%w\nRefresh also failed: %v", msg.err, msg.refreshErr))
			}
			return m, nil
		}
		m.status = msg.status
		m.selectPath(msg.path, msg.preferred)
		if msg.err != nil {
			m.setError(msg.err)
			return m, nil
		}
		m.notice = msg.notice
		m.restorePosition = &msg.position
		return m, m.loadDiff()
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case statusMsg:
		if msg.id != m.scanID {
			return m, nil
		}
		m.loading = false
		if msg.err != nil {
			m.setError(msg.err)
			return m, nil
		}
		old := ""
		if f := m.files(); len(f) > m.selected {
			old = f[m.selected].Path
		}
		m.repo, m.status = msg.repo, msg.status
		m.selectPath(old, m.section)
		return m, m.loadDiff()
	case diffMsg:
		if msg.id != m.diffID {
			return m, nil
		}
		m.diffLoading = false
		if msg.err != nil {
			m.setError(msg.err)
		} else {
			m.diff = msg.diff
			m.lines = msg.lines
			m.hunk = 0
			if msg.position != nil {
				m.hunk = min(max(0, len(m.diff.Hunks)-1), msg.position.hunk)
				m.scroll.ScrollDown(msg.position.scroll)
				m.scroll.ClampTo(len(m.lines), max(1, m.height-5))
			}
		}
	case tea.KeyMsg:
		key := msg.String()
		if key == "ctrl+c" {
			return m, tea.Quit
		}
		if m.help {
			if key == "esc" || key == "q" || key == "?" {
				m.help = false
			}
			return m, nil
		}
		// Keep the visible target stable until mutation and its scan finish.
		// Focus, expansion, help and quit remain responsive.
		if m.busy && key != "tab" && key != "shift+tab" && key != "z" && key != "?" && key != "q" {
			return m, nil
		}
		if m.filtering {
			switch key {
			case "enter":
				m.filtering = false
				return m, nil
			case "esc":
				m.filtering = false
				m.query = ""
			case "backspace":
				r := []rune(m.query)
				if len(r) > 0 {
					m.query = string(r[:len(r)-1])
				}
			default:
				if msg.Type == tea.KeyRunes {
					m.query += string(msg.Runes)
				}
			}
			m.selected = 0
			return m, m.loadDiff()
		}
		switch key {
		case "s":
			return m, m.act(StageFile)
		case "u":
			return m, m.act(UnstageFile)
		case "S":
			return m, m.act(StageHunk)
		case "U":
			return m, m.act(UnstageHunk)
		case "]":
			m.moveHunk(1)
		case "[":
			m.moveHunk(-1)
		case "q":
			if m.expanded {
				m.expanded = false
			} else {
				return m, tea.Quit
			}
		case "esc":
			m.expanded = false
			m.query = ""
			m.err = ""
			return m, m.loadDiff()
		case "?":
			m.help = true
		case "tab":
			m.focus = (m.focus + 1) % 3
		case "shift+tab":
			m.focus = (m.focus + 2) % 3
		case "enter":
			if m.focus < 2 {
				m.focus++
			}
		case "z":
			m.expanded = !m.expanded
		case "h", "left":
			if m.focus == 2 {
				m.horizontal = max(0, m.horizontal-8)
			}
		case "l", "right":
			if m.focus == 2 {
				m.horizontal = min(m.horizontal+8, outputWidth(m.lines))
			}
		case "/":
			m.focus = 1
			m.filtering = true
		case "r", "ctrl+r":
			return m, m.refresh()
		case "j", "down", "k", "up", "pgdown", "pgup", "home", "end":
			delta := 1
			if key == "k" || key == "up" || key == "pgup" {
				delta = -1
			}
			if key == "pgdown" || key == "pgup" {
				delta *= max(1, m.height-5)
			}
			if m.focus == 2 {
				if key == "home" {
					m.scroll.ScrollToTop()
				} else if key == "end" {
					m.scroll.ScrollDown(len(m.lines))
				} else if delta < 0 {
					m.scroll.ScrollUp(-delta)
				} else {
					m.scroll.ScrollDown(delta)
				}
				m.scroll.ClampTo(len(m.lines), max(1, m.height-5))
			} else if m.focus == 0 {
				n := min(3, max(0, m.section+delta))
				if key == "home" {
					n = 0
				}
				if key == "end" {
					n = 3
				}
				if n != m.section {
					m.section = n
					m.selected = 0
					return m, m.loadDiff()
				}
			} else {
				n := min(max(0, len(m.files())-1), max(0, m.selected+delta))
				if key == "home" {
					n = 0
				}
				if key == "end" {
					n = max(0, len(m.files())-1)
				}
				if n != m.selected {
					m.selected = n
					return m, m.loadDiff()
				}
			}
		}
	}
	return m, nil
}
