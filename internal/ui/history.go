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

// historyFilter is one row of the History screen's left pane. Header rows are
// captions and are skipped by navigation.
type historyFilter struct {
	Label  string
	Rev    string
	All    bool
	Kind   git.RefKind
	Header bool
	Head   bool
}

func (f historyFilter) selectable() bool { return !f.Header }

// commitDetail is one cached inspection of a commit.
type commitDetail struct {
	commit git.Commit
	files  []git.FileChange
}

// detailCacheLimit bounds the inspection cache. Walking a long history with the
// cursor should not retain every commit it passed.
const detailCacheLimit = 256

type historyState struct {
	filters     []historyFilter
	filterIndex int

	commits []git.Commit
	graph   []git.GraphRow
	// selected indexes commits; top is the first commit row drawn, so the
	// viewport survives loading another page.
	selected, top int

	loading, loadingMore, exhausted bool
	search                          string
	searching                       bool

	detail        commitDetail
	detailOK      bool
	detailLoading bool
	fileIndex     int
	fileTop       int
	inspect       tideui.PaneScroller

	// showDiff swaps the inspector for one file's patch from this commit.
	showDiff    bool
	view        diffView
	diffLabel   string
	diffLoading bool

	cache map[string]commitDetail
	order []string // cache insertion order, oldest first
	err   string
}

func (h *historyState) current() (git.Commit, bool) {
	if h.selected < 0 || h.selected >= len(h.commits) {
		return git.Commit{}, false
	}
	return h.commits[h.selected], true
}

func (h *historyState) filter() historyFilter {
	if h.filterIndex < 0 || h.filterIndex >= len(h.filters) {
		return historyFilter{Label: "HEAD"}
	}
	return h.filters[h.filterIndex]
}

func (h *historyState) remember(oid string, d commitDetail) {
	if h.cache == nil {
		h.cache = map[string]commitDetail{}
	}
	if _, seen := h.cache[oid]; !seen {
		h.order = append(h.order, oid)
	}
	h.cache[oid] = d
	for len(h.order) > detailCacheLimit {
		delete(h.cache, h.order[0])
		h.order = h.order[1:]
	}
}

type historyMsg struct {
	id      int
	commits []git.Commit
	refs    []git.Ref
	head    git.Head
	more    bool // this page was appended to the list rather than replacing it
	err     error
}
type commitDetailMsg struct {
	id     int
	oid    string
	detail commitDetail
	err    error
}
type commitDiffMsg struct {
	id    int
	patch diff.Patch
	label string
	err   error
}

// detailDueMsg fires after the selection has been still long enough to be worth
// inspecting, so holding j does not launch a Git command per row.
type detailDueMsg struct{ id int }

const detailDebounce = 90 * time.Millisecond

// openHistory switches to the History screen, loading refs and the first page.
func (m *Model) openHistory() tea.Cmd {
	if m.history == nil {
		m.history = &historyState{}
	}
	m.screen = screenHistory
	m.focus = 1
	m.notice = ""
	// Returning to a loaded screen keeps its selection; r reloads on demand.
	if len(m.history.commits) > 0 {
		return nil
	}
	return m.loadHistory(false)
}

// loadHistory reads one page. When more is false the list is replaced, which is
// what a new filter, a new search or a refresh needs.
func (m *Model) loadHistory(more bool) tea.Cmd {
	h := m.history
	if h == nil {
		return nil
	}
	if more && (h.exhausted || h.loadingMore || h.loading) {
		return nil
	}
	if m.historyCancel != nil {
		m.historyCancel()
	}
	m.historyID++
	skip := 0
	if more {
		skip = len(h.commits)
		h.loadingMore = true
	} else {
		h.loading = true
		h.exhausted = false
		h.err = ""
	}
	opts := git.HistoryOptions{Skip: skip, Limit: m.historyBatch(), Search: h.search}
	if f := h.filter(); f.All {
		opts.All = true
	} else if f.Rev != "" {
		opts.Rev = f.Rev
	}
	ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
	m.historyCancel = cancel
	id, repo := m.historyID, m.repo
	return tea.Batch(func() tea.Msg {
		defer cancel()
		commits, err := repo.History(ctx, opts)
		msg := historyMsg{id: id, commits: commits, more: more, err: err}
		if !more {
			// Refs and HEAD only matter for a fresh list; a later page reuses
			// what the first page already established.
			if refs, refErr := repo.Refs(ctx); refErr == nil {
				msg.refs = refs
			}
			if head, headErr := repo.ResolveHead(ctx); headErr == nil {
				msg.head = head
			}
		}
		return msg
	}, pulse())
}

// scheduleDetail debounces inspection so a fast cursor does not queue work.
func (m *Model) scheduleDetail() tea.Cmd {
	h := m.history
	if h == nil {
		return nil
	}
	c, ok := h.current()
	if !ok {
		h.detailOK = false
		h.detail = commitDetail{}
		return nil
	}
	h.showDiff = false
	h.fileIndex = 0
	h.fileTop = 0
	h.inspect.ScrollToTop()
	// A commit already inspected is shown immediately; nothing is launched.
	if cached, ok := h.cache[c.OID]; ok {
		h.detail, h.detailOK, h.detailLoading = cached, true, false
		return nil
	}
	h.detailOK = false
	h.detailLoading = true
	m.detailID++
	id := m.detailID
	return tea.Batch(tea.Tick(detailDebounce, func(time.Time) tea.Msg { return detailDueMsg{id} }), pulse())
}

func (m *Model) loadCommitDetail() tea.Cmd {
	h := m.history
	if h == nil {
		return nil
	}
	c, ok := h.current()
	if !ok {
		return nil
	}
	if m.detailCancel != nil {
		m.detailCancel()
	}
	ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
	m.detailCancel = cancel
	id, repo, oid := m.detailID, m.repo, c.OID
	return func() tea.Msg {
		defer cancel()
		detail, err := repo.CommitDetail(ctx, oid)
		if err != nil {
			return commitDetailMsg{id: id, oid: oid, err: err}
		}
		files, err := repo.CommitFiles(ctx, detail)
		return commitDetailMsg{id: id, oid: oid, detail: commitDetail{detail, files}, err: err}
	}
}

// loadCommitFileDiff loads the patch for the selected file of the selected
// commit, reusing the working-tree diff renderer for the result.
func (m *Model) loadCommitFileDiff() tea.Cmd {
	h := m.history
	if h == nil || !h.detailOK || len(h.detail.files) == 0 {
		return nil
	}
	if h.fileIndex >= len(h.detail.files) {
		return nil
	}
	if m.diffCancel != nil {
		m.diffCancel()
	}
	m.diffID++
	file := h.detail.files[h.fileIndex]
	h.showDiff = true
	h.diffLoading = true
	h.diffLabel = fmt.Sprintf("Commit %s · %s", h.detail.commit.Short, safeText(file.Path))
	ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
	m.diffCancel = cancel
	id, repo, commit, label := m.diffID, m.repo, h.detail.commit, h.diffLabel
	ctxLines, whitespace := m.diffContext(), m.whitespaceMode()
	return tea.Batch(func() tea.Msg {
		defer cancel()
		d, err := repo.CommitDiffWith(ctx, commit, file, git.DiffOptions{Context: ctxLines, Whitespace: whitespace})
		patch := diff.Parse(d.Patch)
		patch.Label = "COMMIT " + commit.Short
		return commitDiffMsg{id: id, patch: patch, label: label, err: err}
	}, pulse())
}

// handleHistoryResult consumes the History screen's background results. It runs
// for every message so a result still lands after the user leaves the screen.
func (m *Model) handleHistoryResult(msg tea.Msg) (bool, tea.Cmd) {
	h := m.history
	switch msg := msg.(type) {
	case historyMsg:
		if h == nil || msg.id != m.historyID {
			return true, nil
		}
		h.loading, h.loadingMore = false, false
		if msg.err != nil {
			h.err = msg.err.Error()
			h.commits, h.graph = nil, nil
			return true, nil
		}
		h.err = ""
		if msg.more {
			// A short page means the walk reached the end of this filter.
			if len(msg.commits) < m.historyBatch() {
				h.exhausted = true
			}
			h.commits = append(h.commits, msg.commits...)
		} else {
			if len(msg.commits) < m.historyBatch() {
				h.exhausted = true
			}
			h.commits = msg.commits
			h.selected, h.top = 0, 0
			if msg.refs != nil {
				h.filters = m.buildFilters(msg.refs, msg.head)
				h.filterIndex = min(h.filterIndex, max(0, len(h.filters)-1))
			}
			m.head = msg.head
		}
		// Lanes are recomputed over the whole list so a new page continues the
		// lanes the earlier pages established.
		h.graph = git.GraphLanes(h.commits)
		h.selected = min(h.selected, max(0, len(h.commits)-1))
		return true, m.scheduleDetail()
	case detailDueMsg:
		if h == nil || msg.id != m.detailID {
			return true, nil
		}
		return true, m.loadCommitDetail()
	case commitDetailMsg:
		if h == nil || msg.id != m.detailID {
			return true, nil
		}
		h.detailLoading = false
		if msg.err != nil {
			h.err = msg.err.Error()
			return true, nil
		}
		h.detail, h.detailOK = msg.detail, true
		h.remember(msg.oid, msg.detail)
		return true, nil
	case commitDiffMsg:
		if h == nil || msg.id != m.diffID {
			return true, nil
		}
		h.diffLoading = false
		if msg.err != nil {
			h.err = msg.err.Error()
			h.showDiff = false
			return true, nil
		}
		h.view.reset(msg.patch, msg.patch.Label)
		h.diffLabel = msg.label
		return true, nil
	}
	return false, nil
}

// buildFilters turns the ref list into the left pane's rows. Remote branches
// and tags respect git.show_remote_branches and git.show_tags.
func (m *Model) buildFilters(refs []git.Ref, head git.Head) []historyFilter {
	filters := []historyFilter{
		{Label: "Current HEAD", Head: true},
		{Label: "All refs", All: true},
	}
	section := func(caption string, kind git.RefKind) {
		var group []historyFilter
		for _, ref := range refs {
			if ref.Kind != kind {
				continue
			}
			group = append(group, historyFilter{Label: ref.Name, Rev: ref.Name, Kind: kind, Head: ref.Head})
		}
		if len(group) == 0 {
			return
		}
		filters = append(filters, historyFilter{Label: caption, Header: true})
		filters = append(filters, group...)
	}
	section("LOCAL", git.RefLocal)
	if m.showRemoteBranches() {
		section("REMOTE", git.RefRemote)
	}
	if m.showTags() {
		section("TAGS", git.RefTag)
	}
	_ = head
	return filters
}

// moveFilter walks the left pane, skipping caption rows.
func (m *Model) moveFilter(delta int) tea.Cmd {
	h := m.history
	if len(h.filters) == 0 {
		return nil
	}
	next := h.filterIndex
	for i := 0; i < len(h.filters); i++ {
		next += delta
		if next < 0 || next >= len(h.filters) {
			return nil
		}
		if h.filters[next].selectable() {
			break
		}
	}
	if next == h.filterIndex || !h.filters[next].selectable() {
		return nil
	}
	h.filterIndex = next
	return m.loadHistory(false)
}

// moveCommit walks the commit list and loads the next page as the cursor nears
// the end, so a long history arrives while it is being read.
func (m *Model) moveCommit(delta int) tea.Cmd {
	h := m.history
	if len(h.commits) == 0 {
		return nil
	}
	next := min(max(0, h.selected+delta), len(h.commits)-1)
	if next == h.selected {
		if delta > 0 {
			return m.loadHistory(true)
		}
		return nil
	}
	h.selected = next
	cmds := []tea.Cmd{m.scheduleDetail()}
	if len(h.commits)-h.selected < m.historyBatch()/3 {
		cmds = append(cmds, m.loadHistory(true))
	}
	return tea.Batch(cmds...)
}

// updateHistoryKey handles the History screen's keys. The caller has already
// dealt with global keys, overlays and the search field.
func (m *Model) updateHistoryKey(key string, msg tea.KeyMsg) tea.Cmd {
	h := m.history
	if h.searching {
		switch key {
		case "enter":
			h.searching = false
			return nil
		case "esc":
			h.searching = false
			if h.search == "" {
				return nil
			}
			h.search = ""
			return m.loadHistory(false)
		case "backspace":
			r := []rune(h.search)
			if len(r) == 0 {
				return nil
			}
			h.search = string(r[:len(r)-1])
			return m.loadHistory(false)
		default:
			if msg.Type == tea.KeyRunes {
				h.search += string(msg.Runes)
				return m.loadHistory(false)
			}
			return nil
		}
	}
	// The diff view owns navigation while it is open.
	if h.showDiff && m.focus == 2 {
		flat := h.view.flat(m.diffOptionsFrom(), true)
		switch key {
		case "esc", "q", "backspace":
			h.showDiff = false
			return nil
		case "j", "down":
			h.view.line = min(h.view.line+1, max(0, len(flat)-1))
		case "k", "up":
			h.view.line = max(0, h.view.line-1)
		case "pgdown":
			h.view.line = min(h.view.line+max(1, m.historyPaneHeight()-4), max(0, len(flat)-1))
		case "pgup":
			h.view.line = max(0, h.view.line-max(1, m.historyPaneHeight()-4))
		case "home":
			h.view.line = 0
		case "end":
			h.view.line = max(0, len(flat)-1)
		case "]":
			h.view.moveHunk(1, m.diffOptionsFrom())
		case "[":
			h.view.moveHunk(-1, m.diffOptionsFrom())
		case "ctrl+f":
			return m.startDiffSearch()
		case "v":
			return m.toggleDiffMode()
		}
		h.view.ensureVisible(m.historyPaneHeight() - 2)
		return nil
	}
	switch key {
	case "/":
		m.focus = 1
		h.searching = true
		return nil
	case "y":
		return m.copyCommitHash()
	case "n":
		return m.promptBranchFromSelection()
	case "enter":
		switch m.focus {
		case 0:
			m.focus = 1
		case 1:
			m.focus = 2
		default:
			return m.loadCommitFileDiff()
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
			if key == "home" || key == "end" {
				return nil
			}
			return m.moveFilter(delta)
		case 1:
			switch key {
			case "home":
				h.selected = 0
				return m.scheduleDetail()
			case "end":
				// Jumping to the end lands on the last loaded commit, which is
				// rarely the last commit: ask for the next page too.
				h.selected = max(0, len(h.commits)-1)
				return tea.Batch(m.scheduleDetail(), m.loadHistory(true))
			}
			return m.moveCommit(delta)
		default:
			return m.moveInspector(delta, key)
		}
	}
	return nil
}

// moveInspector walks the changed-file list, or scrolls the message when the
// commit changed nothing that can be listed.
func (m *Model) moveInspector(delta int, key string) tea.Cmd {
	h := m.history
	if !h.detailOK || len(h.detail.files) == 0 {
		if delta < 0 {
			h.inspect.ScrollUp(-delta)
		} else {
			h.inspect.ScrollDown(delta)
		}
		return nil
	}
	if key == "home" {
		h.fileIndex = 0
		return nil
	}
	if key == "end" {
		h.fileIndex = len(h.detail.files) - 1
		return nil
	}
	h.fileIndex = min(max(0, h.fileIndex+delta), len(h.detail.files)-1)
	return nil
}

// copyCommitHash puts the selected commit's full hash on the system clipboard.
func (m *Model) copyCommitHash() tea.Cmd {
	c, ok := m.history.current()
	if !ok {
		return nil
	}
	if err := (systemClipboard{}).Write(c.OID); err != nil {
		m.notice = "Clipboard: " + err.Error()
		return nil
	}
	m.notice = "Copied " + c.Short + " to the clipboard"
	return nil
}

// historyPaneHeight is the drawable height of the History screen's panes.
func (m *Model) historyPaneHeight() int { return max(1, m.height-8) }

// relativeTime renders a compact age, falling back to a date once a duration
// stops being useful. Widths stay short so the column never shifts.
func relativeTime(t time.Time, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t)
	switch {
	case d < 0:
		return "now"
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dw", int(d.Hours()/(24*7)))
	default:
		return t.Format("2006-01")
	}
}

// authorInitials abbreviates a name for the narrow author column.
func authorInitials(name string) string {
	fields := strings.Fields(name)
	switch len(fields) {
	case 0:
		return "··"
	case 1:
		r := []rune(fields[0])
		if len(r) == 1 {
			return strings.ToUpper(string(r))
		}
		return strings.ToUpper(string(r[:2]))
	default:
		first := []rune(fields[0])
		last := []rune(fields[len(fields)-1])
		return strings.ToUpper(string(first[:1]) + string(last[:1]))
	}
}
