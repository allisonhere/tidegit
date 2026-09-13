package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/allisonhere/tidegit/internal/config"
	"github.com/allisonhere/tidegit/internal/diff"
	"github.com/allisonhere/tidegit/internal/git"
	"github.com/allisonhere/tideui"
	tea "github.com/charmbracelet/bubbletea"
)

type statusMsg struct {
	id     int
	repo   git.Repository
	status git.Status
	state  git.RepoState
	err    error
}
type diffMsg struct {
	id       int
	diff     git.Diff
	patch    diff.Patch
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
	view                                    diffView
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
	compose                                 *commitState
	opening                                 bool
	frame                                   int

	// marked is the multi-select set on the Status screen, keyed by section and
	// path so a file appearing in two groups is tracked independently.
	marked map[string]bool

	// Configuration and persistent state. Views read resolved values through
	// cfg; only the settings screen writes through the store.
	store          *config.Store
	cfg            *config.Config
	state          *config.State
	statePath      string
	keys           map[string]string
	keyIssues      []string
	recordedRoot   string
	pendingScreen  screen
	configWarnings []string

	// Screens beyond Status. Each keeps its own state so switching away and
	// back does not discard a selection or reload what is already known.
	screen                                                               screen
	head                                                                 git.Head
	repoState                                                            git.RepoState
	history                                                              *historyState
	branches                                                             *branchState
	stash                                                                *stashState
	remotes                                                              *remoteState
	conflicts                                                            *conflictState
	reflog                                                               *reflogState
	settings                                                             *settingsState
	palette                                                              *paletteState
	prompt                                                               *promptState
	choice                                                               *choiceState
	confirm                                                              *confirmState
	op                                                                   *operationState
	remoteCache                                                          []git.Remote
	deleteTarget                                                         string
	picker                                                               *tideui.ThemePicker
	omarchySignature                                                     string
	omarchyWatching                                                      bool
	historyID, detailID, branchID                                        int
	remoteID, stashID, stashDetailID, stashDiffID                        int
	conflictID, conflictDetailID, reflogID, reflogDetailID, reflogDiffID int
	historyCancel, detailCancel, branchCancel                            context.CancelFunc
}

func New(ctx context.Context, path string, theme tideui.Theme) *Model {
	// Tests and callers that already resolved a theme keep this constructor.
	store := config.Open("", "")
	_ = store.Override("appearance.theme", theme.Name)
	return newModel(ctx, path, store, config.DefaultState(), "")
}

// NewConfigured builds a model from a resolved configuration store and
// persistent state. This is what the command line uses.
func NewConfigured(ctx context.Context, path string, store *config.Store, state *config.State, statePath string) *Model {
	return newModel(ctx, path, store, state, statePath)
}

func newModel(ctx context.Context, path string, store *config.Store, state *config.State, statePath string) *Model {
	m := &Model{ctx: ctx, path: path, width: 100, height: 28, store: store, state: state, statePath: statePath}
	if m.state == nil {
		m.state = config.DefaultState()
	}
	m.applyConfig()
	m.pendingScreen = screenByName(m.cfg.Layout.DefaultScreen)
	return m
}

// applyConfig resolves the effective configuration into runtime fields. It is
// called at startup, after a settings change and after a reload; nothing else
// rereads the store.
func (m *Model) applyConfig() {
	if m.store == nil {
		m.store = config.Open("", "")
	}
	m.cfg = m.store.Config()
	if theme, ok := resolveConfiguredTheme(m.cfg.Appearance.Theme); ok {
		m.theme = theme
	} else {
		m.theme = tideui.CatppuccinMocha
		m.notice = "Unknown theme " + safeText(m.cfg.Appearance.Theme) + "; using catppuccin-mocha"
	}
	m.keys, m.keyIssues = resolveKeymap(m.cfg)
	if m.store != nil {
		m.configWarnings = append(append([]string{}, m.store.Errors()...), m.store.Warnings()...)
	}
}
func (m *Model) Init() tea.Cmd {
	// A theme that follows the desktop starts its poll as soon as the program
	// does, not only when it is chosen from the picker.
	return tea.Batch(m.refresh(), m.followOmarchy(), m.armAutoRefresh())
}

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
	if m.busy || m.opening {
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
	m.restorePosition = &viewPosition{m.view.scroll.Offset(), m.view.hunk, m.view.line}
	ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
	m.scanCancel = cancel
	id, path := m.scanID, m.path
	return func() tea.Msg {
		defer cancel()
		r, err := git.Discover(ctx, path)
		var s git.Status
		var st git.RepoState
		if err == nil {
			s, err = r.RepositoryStatus(ctx)
		}
		if err == nil {
			// Operation state comes from Git's own state files, so a restart
			// rediscovers a half-finished merge or rebase immediately.
			if state, stateErr := r.State(ctx); stateErr == nil {
				st = state
			}
		}
		return statusMsg{id: id, repo: r, status: s, state: st, err: err}
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
	ctxLines, whitespace := m.diffContext(), m.whitespaceMode()
	label := m.diffLabel(file)
	m.diffLoading = true
	m.err = ""
	position := m.restorePosition
	m.restorePosition = nil
	return func() tea.Msg {
		defer cancel()
		d, err := repo.DiffWith(ctx, section, file, git.DiffOptions{Context: ctxLines, Whitespace: whitespace})
		patch := diff.Parse(d.Patch)
		patch.Truncated = d.Truncated
		patch.Label = label
		if strings.Count(d.Patch, "\n") > 20000 {
			d.Hunks = nil
			d.HunkUnavailable = "Hunk actions unavailable: preview exceeds 20,000 lines."
		}
		return diffMsg{id: id, diff: d, patch: patch, err: err, position: position}
	}
}

// diffLabel names the snapshot a Status diff shows, so the pane title makes the
// context unmistakable.
func (m *Model) diffLabel(file git.File) string {
	switch m.section {
	case int(git.Staged):
		return "STAGED"
	case int(git.Untracked):
		return "UNTRACKED"
	case int(git.Conflicted):
		return "CONFLICT"
	default:
		return "UNSTAGED"
	}
}
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(autoRefreshMsg); ok {
		return m, m.handleAutoRefresh()
	}
	if edited, ok := msg.(configEditedMsg); ok {
		return m, m.handleConfigEdited(edited)
	}
	if handled, cmd := m.handleCommitResult(msg); handled {
		return m, cmd
	}
	if handled, cmd := m.handleHistoryResult(msg); handled {
		return m, cmd
	}
	if handled, cmd := m.handleBranchResult(msg); handled {
		return m, cmd
	}
	if handled, cmd := m.handleOperationResult(msg); handled {
		return m, cmd
	}
	if handled, cmd := m.handleStashResult(msg); handled {
		return m, cmd
	}
	if handled, cmd := m.handleRemotesResult(msg); handled {
		return m, cmd
	}
	if handled, cmd := m.handleConflictResult(msg); handled {
		return m, cmd
	}
	if handled, cmd := m.handleReflogResult(msg); handled {
		return m, cmd
	}
	if handled, cmd := m.handleRecoveryResult(msg); handled {
		return m, cmd
	}
	if editor, ok := msg.(editorFinishedMsg); ok {
		return m, m.handleEditorFinished(editor)
	}
	if m.compose != nil {
		return m, m.updateCommit(msg)
	}
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
		m.repoState = msg.state
		m.repoState.Conflicts = len(msg.status.Groups[git.Conflicted])
		if msg.repo.Root != m.recordedRoot {
			m.recordedRoot = msg.repo.Root
			m.recordRepository()
		}
		m.head = git.Head{Branch: msg.status.Branch, OID: msg.status.OID,
			Detached: msg.status.Branch == "(detached)", Unborn: msg.status.OID == "(initial)"}
		if m.head.Detached {
			m.head.Branch = ""
		}
		if len(m.head.OID) >= 7 && !m.head.Unborn {
			m.head.Short = m.head.OID[:7]
		}
		m.selectPath(old, m.section)
		// A configured startup screen other than Status is entered once the
		// repository is known, so its data loads against a real repository.
		if m.pendingScreen != screenStatus {
			next := m.pendingScreen
			m.pendingScreen = screenStatus
			return m, m.goToScreen(next)
		}
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
			m.view.reset(msg.patch, msg.patch.Label)
			m.hunk = 0
			if msg.position != nil {
				m.hunk = min(max(0, len(m.diff.Hunks)-1), msg.position.hunk)
				m.view.hunk = m.hunk
				m.view.line = min(max(0, msg.position.line), max(0, len(m.view.flat(m.diffOptionsFrom(), true))-1))
				m.view.scroll.ScrollDown(msg.position.scroll)
			}
		}
	case tea.KeyMsg:
		return m, m.handleKey(msg)
	}
	return m, nil
}
