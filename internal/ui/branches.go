package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/allisonhere/tidegit/internal/git"
	"github.com/allisonhere/tideui"
	tea "github.com/charmbracelet/bubbletea"
)

// branchState backs the Branches screen. Local and remote branches are two
// groups of one list so Tab-free navigation walks straight from one into the
// other, the way the eye reads the pane.
type branchState struct {
	local, remote []git.Branch
	group         int // 0 local, 1 remote
	index         int
	top           int
	loading       bool
	err           string

	filter    string
	filtering bool

	// showRemote reflects git.show_remote_branches; the remote group is hidden
	// from the list and from navigation when false.
	showRemote bool

	// commits holds the middle pane: the selected branch's own history.
	commits        []git.Commit
	graph          []git.GraphRow
	commitIndex    int
	commitTop      int
	commitsLoading bool
	commitsFor     string

	// extra carries inspector facts that need their own Git calls, and is only
	// filled for the branch currently being inspected.
	extra        branchExtra
	extraFor     string
	extraLoading bool

	inspect tideui.PaneScroller
}

// branchExtra is the inspector detail that for-each-ref cannot answer.
type branchExtra struct {
	MergeBase     string
	Ahead, Behind int // against HEAD, for a branch with no tracking data
	Comparable    bool
}

func (b *branchState) groups() [2][]git.Branch {
	return [2][]git.Branch{b.visible(b.local), b.visible(b.remote)}
}

// visible applies the branch filter.
func (b *branchState) visible(list []git.Branch) []git.Branch {
	if b.filter == "" {
		return list
	}
	out := make([]git.Branch, 0, len(list))
	for _, br := range list {
		if strings.Contains(strings.ToLower(br.Name), strings.ToLower(b.filter)) {
			out = append(out, br)
		}
	}
	return out
}

func (b *branchState) current() (git.Branch, bool) {
	g := b.groups()
	if b.group < 0 || b.group > 1 {
		return git.Branch{}, false
	}
	list := g[b.group]
	if b.index < 0 || b.index >= len(list) {
		return git.Branch{}, false
	}
	return list[b.index], true
}

// rows flattens both groups into the order the pane draws them, so one cursor
// can walk captions and branches together.
type branchRow struct {
	branch  git.Branch
	caption string
	group   int
	index   int
}

func (b *branchState) rows() []branchRow {
	g := b.groups()
	rows := []branchRow{{caption: "LOCAL"}}
	for i, br := range g[0] {
		rows = append(rows, branchRow{branch: br, group: 0, index: i})
	}
	if !b.showRemote {
		return rows
	}
	rows = append(rows, branchRow{caption: "REMOTE"})
	for i, br := range g[1] {
		rows = append(rows, branchRow{branch: br, group: 1, index: i})
	}
	return rows
}

// cursor is the index into rows() of the selected branch.
func (b *branchState) cursor() int {
	for i, row := range b.rows() {
		if row.caption == "" && row.group == b.group && row.index == b.index {
			return i
		}
	}
	return 0
}

type branchesMsg struct {
	id            int
	local, remote []git.Branch
	head          git.Head
	err           error
}
type branchCommitsMsg struct {
	id      int
	ref     string
	commits []git.Commit
	err     error
}
type branchExtraMsg struct {
	id    int
	ref   string
	extra branchExtra
	err   error
}

// branchActionMsg reports a completed mutation together with the rescan that
// followed it, so the screen never shows state from before the change.
type branchActionMsg struct {
	notice          string
	err, refreshErr error
	local, remote   []git.Branch
	head            git.Head
	selectName      string
	// unmerged carries Git's refusal to discard unmerged work back to the UI,
	// which then offers a forced delete as a separate, explicit confirmation.
	unmerged bool
	target   string
}

func (m *Model) openBranches() tea.Cmd {
	if m.branches == nil {
		m.branches = &branchState{}
	}
	m.branches.showRemote = m.showRemoteBranches()
	m.screen = screenBranches
	m.focus = 0
	m.notice = ""
	if len(m.branches.local) > 0 || len(m.branches.remote) > 0 {
		return nil
	}
	return m.loadBranches()
}

func (m *Model) loadBranches() tea.Cmd {
	if m.branches == nil {
		m.branches = &branchState{}
	}
	m.branches.showRemote = m.showRemoteBranches()
	if m.branchCancel != nil {
		m.branchCancel()
	}
	m.branchID++
	m.branches.loading = true
	m.branches.err = ""
	ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
	m.branchCancel = cancel
	id, repo := m.branchID, m.repo
	return tea.Batch(func() tea.Msg {
		defer cancel()
		local, remote, err := repo.Branches(ctx)
		msg := branchesMsg{id: id, local: local, remote: remote, err: err}
		if head, headErr := repo.ResolveHead(ctx); headErr == nil {
			msg.head = head
		}
		return msg
	}, pulse())
}

// loadBranchDetail reads the selected branch's commits and inspector extras.
func (m *Model) loadBranchDetail() tea.Cmd {
	b := m.branches
	branch, ok := b.current()
	if !ok {
		b.commits, b.graph, b.commitsFor = nil, nil, ""
		return nil
	}
	if b.commitsFor == branch.Name && b.extraFor == branch.Name {
		return nil
	}
	if m.detailCancel != nil {
		m.detailCancel()
	}
	m.detailID++
	b.commitsLoading = true
	b.extraLoading = true
	b.commitIndex, b.commitTop = 0, 0
	b.inspect.ScrollToTop()
	// The two loads run concurrently, so each owns its own context: sharing one
	// would let whichever finished first cancel the other.
	commitCtx, cancelCommits := context.WithTimeout(m.ctx, 30*time.Second)
	extraCtx, cancelExtra := context.WithTimeout(m.ctx, 30*time.Second)
	m.detailCancel = func() { cancelCommits(); cancelExtra() }
	id, repo, name, oid := m.detailID, m.repo, branch.Name, branch.OID
	head := m.head
	return tea.Batch(func() tea.Msg {
		defer cancelCommits()
		commits, err := repo.History(commitCtx, git.HistoryOptions{Rev: name, Limit: 80})
		return branchCommitsMsg{id: id, ref: name, commits: commits, err: err}
	}, func() tea.Msg {
		defer cancelExtra()
		ctx := extraCtx
		var extra branchExtra
		if head.OID == "" || head.OID == oid {
			return branchExtraMsg{id: id, ref: name, extra: extra}
		}
		base, err := repo.MergeBase(ctx, "HEAD", oid)
		if err != nil {
			return branchExtraMsg{id: id, ref: name, err: err}
		}
		extra.MergeBase = base
		if base != "" {
			ahead, behind, err := repo.Divergence(ctx, "HEAD", oid)
			if err != nil {
				return branchExtraMsg{id: id, ref: name, err: err}
			}
			// Divergence counts HEAD first; the inspector reads from the
			// branch's point of view, so the two sides swap.
			extra.Ahead, extra.Behind, extra.Comparable = behind, ahead, true
		}
		return branchExtraMsg{id: id, ref: name, extra: extra}
	}, pulse())
}

func (m *Model) handleBranchResult(msg tea.Msg) (bool, tea.Cmd) {
	b := m.branches
	switch msg := msg.(type) {
	case branchesMsg:
		if b == nil || msg.id != m.branchID {
			return true, nil
		}
		b.loading = false
		if msg.err != nil {
			b.err = msg.err.Error()
			return true, nil
		}
		b.err = ""
		first := len(b.local) == 0 && len(b.remote) == 0
		b.local, b.remote, m.head = msg.local, msg.remote, msg.head
		// Opening the screen should land on the branch you are on, not on
		// whichever branch happens to have the newest commit.
		if first {
			for _, branch := range b.local {
				if branch.Current {
					b.selectByName(branch.Name)
					break
				}
			}
		}
		b.clampSelection()
		return true, m.loadBranchDetail()
	case branchCommitsMsg:
		if b == nil || msg.id != m.detailID {
			return true, nil
		}
		b.commitsLoading = false
		if msg.err != nil {
			b.err = msg.err.Error()
			return true, nil
		}
		b.commits, b.commitsFor = msg.commits, msg.ref
		b.graph = git.GraphLanes(msg.commits)
		return true, nil
	case branchExtraMsg:
		if b == nil || msg.id != m.detailID {
			return true, nil
		}
		b.extraLoading = false
		if msg.err != nil {
			// The comparison is supporting detail: the rest of the inspector
			// still stands, so this is noted rather than raised as an error.
			b.extra, b.extraFor = branchExtra{}, ""
			return true, nil
		}
		b.extra, b.extraFor = msg.extra, msg.ref
		return true, nil
	case branchActionMsg:
		m.busy = false
		m.operation = ""
		if b == nil {
			return true, nil
		}
		if msg.refreshErr != nil {
			b.err = msg.refreshErr.Error()
			return true, nil
		}
		b.local, b.remote, m.head = msg.local, msg.remote, msg.head
		if msg.err != nil {
			b.err = msg.err.Error()
			m.notice = ""
		} else {
			b.err = ""
			m.notice = msg.notice
		}
		if msg.selectName != "" {
			b.selectByName(msg.selectName)
		}
		b.clampSelection()
		b.commitsFor, b.extraFor = "", ""
		// HEAD or the ref set may have moved, so History reloads next visit
		// rather than showing decorations from before the change.
		if msg.err == nil && m.history != nil {
			m.history.commits, m.history.graph = nil, nil
			m.history.cache, m.history.order = nil, nil
		}
		// A refused safe delete becomes a separate, explicitly worded offer;
		// nothing escalates without the user agreeing to it again.
		if msg.unmerged && m.deleteTarget != "" {
			m.offerForceDelete(m.deleteTarget)
			b.err = ""
		}
		m.deleteTarget = ""
		return true, m.loadBranchDetail()
	}
	return false, nil
}

// clampSelection keeps the cursor on a real branch after the list changes.
func (b *branchState) clampSelection() {
	g := b.groups()
	if len(g[0]) == 0 && len(g[1]) == 0 {
		b.group, b.index = 0, 0
		return
	}
	if b.group > 1 || b.group < 0 {
		b.group = 0
	}
	if !b.showRemote && b.group == 1 {
		b.group = 0
	}
	if len(g[b.group]) == 0 {
		b.group = 1 - b.group
	}
	b.index = min(max(0, b.index), max(0, len(g[b.group])-1))
}

// selectByName moves the cursor to a branch by name, in either group.
func (b *branchState) selectByName(name string) {
	g := b.groups()
	for group := 0; group < 2; group++ {
		for i, br := range g[group] {
			if br.Name == name {
				b.group, b.index = group, i
				return
			}
		}
	}
}

// moveBranch walks the flattened list, skipping captions.
func (m *Model) moveBranch(delta int) tea.Cmd {
	b := m.branches
	rows := b.rows()
	if len(rows) == 0 {
		return nil
	}
	next := b.cursor()
	for i := 0; i < len(rows); i++ {
		next += delta
		if next < 0 || next >= len(rows) {
			return nil
		}
		if rows[next].caption == "" {
			break
		}
	}
	if next < 0 || next >= len(rows) || rows[next].caption != "" {
		return nil
	}
	b.group, b.index = rows[next].group, rows[next].index
	return m.loadBranchDetail()
}

func (m *Model) updateBranchKey(key string, msg tea.KeyMsg) tea.Cmd {
	b := m.branches
	if b.filtering {
		switch key {
		case "enter":
			b.filtering = false
			return nil
		case "esc":
			b.filtering = false
			b.filter = ""
			b.clampSelection()
			return m.loadBranchDetail()
		case "backspace":
			r := []rune(b.filter)
			if len(r) > 0 {
				b.filter = string(r[:len(r)-1])
			}
		default:
			if msg.Type == tea.KeyRunes {
				b.filter += string(msg.Runes)
			}
		}
		b.group, b.index = 0, 0
		b.clampSelection()
		return m.loadBranchDetail()
	}
	switch key {
	case "/":
		m.focus = 0
		b.filtering = true
		return nil
	case "enter", "s":
		return m.switchToSelectedBranch()
	case "n":
		return m.promptNewBranch()
	case "R":
		return m.promptRenameBranch()
	case "D":
		return m.confirmDeleteBranch()
	case "y":
		branch, ok := b.current()
		if !ok {
			return nil
		}
		if err := (systemClipboard{}).Write(branch.OID); err != nil {
			m.notice = "Clipboard: " + err.Error()
			return nil
		}
		m.notice = "Copied " + branch.Short + " to the clipboard"
		return nil
	case "j", "down", "k", "up", "pgdown", "pgup", "home", "end":
		delta := 1
		if key == "k" || key == "up" || key == "pgup" {
			delta = -1
		}
		if key == "pgdown" || key == "pgup" {
			delta *= 5
		}
		switch m.focus {
		case 0:
			return m.moveBranch(delta)
		case 1:
			if len(b.commits) > 0 {
				b.commitIndex = min(max(0, b.commitIndex+delta), len(b.commits)-1)
			}
			return nil
		default:
			if delta < 0 {
				b.inspect.ScrollUp(-delta)
			} else {
				b.inspect.ScrollDown(delta)
			}
			return nil
		}
	}
	return nil
}

// switchToSelectedBranch checks out a local branch. A remote-tracking branch is
// read-only in this milestone, so it explains itself instead of guessing at a
// local branch to create.
func (m *Model) switchToSelectedBranch() tea.Cmd {
	branch, ok := m.branches.current()
	if !ok || m.busy {
		return nil
	}
	if branch.Remote {
		m.notice = "Remote branches are read-only here · press n to create a local branch from it"
		return nil
	}
	if branch.Current {
		m.notice = "Already on " + safeText(branch.Name)
		return nil
	}
	return m.runBranchAction("Switching to "+safeText(branch.Name), branch.Name,
		"Switched to "+safeText(branch.Name),
		func(ctx context.Context, repo git.Repository) error {
			return repo.SwitchBranch(ctx, branch.Name)
		})
}

// runBranchAction performs one mutation and rescans, holding the UI's mutation
// gate closed until both finish. The worker only builds a message: model state
// is never written from the background goroutine.
func (m *Model) runBranchAction(operation, selectName, notice string, fn func(context.Context, git.Repository) error) tea.Cmd {
	m.busy = true
	m.operation = operation
	m.notice = ""
	repo := m.repo
	return tea.Batch(func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
		defer cancel()
		err := fn(ctx, repo)
		// Even a failed operation may have changed the repository, so the list
		// is always re-read before anything is shown.
		rescanCtx, rescanCancel := context.WithTimeout(m.ctx, 30*time.Second)
		defer rescanCancel()
		local, remote, scanErr := repo.Branches(rescanCtx)
		head, headErr := repo.ResolveHead(rescanCtx)
		if scanErr == nil {
			scanErr = headErr
		}
		msg := branchActionMsg{notice: notice, err: err, refreshErr: scanErr,
			local: local, remote: remote, head: head, selectName: selectName,
			unmerged: errors.Is(err, git.ErrUnmergedBranch)}
		if err != nil {
			msg.notice = ""
		}
		return msg
	}, pulse())
}

// promptNewBranch asks for a name for a branch starting at the selection.
func (m *Model) promptNewBranch() tea.Cmd {
	branch, ok := m.branches.current()
	start, from := "HEAD", "HEAD"
	if ok {
		start, from = branch.Name, branch.Name
		if branch.Remote {
			from = branch.Name + " (remote)"
		}
	}
	m.prompt = &promptState{
		title:   "new branch",
		label:   "Create a branch starting at " + safeText(from),
		help:    "Enter creates · Ctrl-S creates and switches · Esc cancels",
		kind:    promptCreateBranch,
		context: start,
	}
	return nil
}

// promptBranchFromSelection is the History screen's branch-here action.
func (m *Model) promptBranchFromSelection() tea.Cmd {
	c, ok := m.history.current()
	if !ok {
		return nil
	}
	m.prompt = &promptState{
		title:   "new branch",
		label:   "Create a branch at " + c.Short + " · " + safeText(clip(c.Subject, 40)),
		help:    "Enter creates · Ctrl-S creates and switches · Esc cancels",
		kind:    promptCreateBranch,
		context: c.OID,
	}
	return nil
}

func (m *Model) promptRenameBranch() tea.Cmd {
	branch, ok := m.branches.current()
	if !ok {
		return nil
	}
	if branch.Remote {
		m.notice = "Remote branches cannot be renamed from TideGit"
		return nil
	}
	m.prompt = &promptState{
		title:   "rename branch",
		label:   "Rename " + safeText(branch.Name),
		help:    "Enter renames · Esc cancels",
		kind:    promptRenameBranch,
		context: branch.Name,
		value:   branch.Name,
	}
	return nil
}

// confirmDeleteBranch asks before deleting, and says plainly what would be
// lost. Deletion is never forced from this path.
func (m *Model) confirmDeleteBranch() tea.Cmd {
	branch, ok := m.branches.current()
	if !ok {
		return nil
	}
	if branch.Remote {
		m.notice = "Remote branch deletion is out of scope for this milestone"
		return nil
	}
	if branch.Current {
		m.notice = "Switch to another branch before deleting " + safeText(branch.Name)
		return nil
	}
	body := "Its commits stay in the repository until Git collects them.\nNothing in the working tree changes."
	if !branch.Merged {
		body = "This branch is NOT merged into HEAD.\nIt holds commits no other branch contains."
	}
	m.confirm = &confirmState{
		title:  "delete local branch?",
		body:   "Delete " + safeText(branch.Name) + "?\n\n" + body,
		accept: "d",
		danger: !branch.Merged,
		kind:   confirmDeleteBranch,
		target: branch.Name,
	}
	return nil
}

// deleteBranch runs a safe delete. An unmerged branch is refused and the
// refusal is offered as a separate, explicitly worded confirmation; nothing
// escalates to a forced delete on its own.
func (m *Model) deleteBranch(name string, force bool) tea.Cmd {
	notice := "Deleted branch " + safeText(name)
	operation := "Deleting " + safeText(name)
	if force {
		notice = "Force-deleted branch " + safeText(name)
		operation = "Force-deleting " + safeText(name)
	}
	cmd := m.runBranchAction(operation, "", notice, func(ctx context.Context, repo git.Repository) error {
		return repo.DeleteBranch(ctx, name, force)
	})
	m.deleteTarget = name
	return cmd
}

// branchAheadBehind renders tracking state as symbols. The caller pairs it
// with aheadBehindWords so the meaning never depends on reading the arrows;
// this function does not repeat those words itself.
func branchAheadBehind(r tideui.Renderer, b git.Branch) string {
	if b.UpstreamGone {
		return r.Styles.DetailBody.Foreground(r.Styles.Theme.Error).Render("upstream gone")
	}
	if b.Upstream == "" {
		return muted(r, "no upstream")
	}
	if b.Ahead == 0 && b.Behind == 0 {
		return muted(r, "up to date")
	}
	up := r.Styles.DetailBody.Foreground(r.Styles.Theme.Unread).Bold(true)
	down := r.Styles.DetailBody.Foreground(r.Styles.Theme.Error).Bold(true)
	var parts []string
	if b.Ahead > 0 {
		parts = append(parts, up.Render(fmt.Sprintf("↑ %d", b.Ahead)))
	}
	if b.Behind > 0 {
		parts = append(parts, down.Render(fmt.Sprintf("↓ %d", b.Behind)))
	}
	return strings.Join(parts, "  ")
}

// aheadBehindWords states tracking in plain language for the inspector and for
// anyone who cannot rely on the arrows.
func aheadBehindWords(b git.Branch) string {
	switch {
	case b.UpstreamGone:
		return "the configured upstream no longer exists"
	case b.Upstream == "":
		return "not tracking a remote branch"
	case b.Ahead > 0 && b.Behind > 0:
		return fmt.Sprintf("%d ahead, %d behind %s", b.Ahead, b.Behind, b.Upstream)
	case b.Ahead > 0:
		return fmt.Sprintf("%d ahead of %s", b.Ahead, b.Upstream)
	case b.Behind > 0:
		return fmt.Sprintf("%d behind %s", b.Behind, b.Upstream)
	default:
		return "up to date with " + b.Upstream
	}
}
