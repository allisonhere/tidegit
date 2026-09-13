package ui

import (
	"context"
	"strings"
	"time"

	"github.com/allisonhere/tidegit/internal/git"
	"github.com/allisonhere/tideui"
	tea "github.com/charmbracelet/bubbletea"
)

// remoteState backs the Remotes screen: the configured remotes, their
// remote-tracking branches and the last fetch time.
type remoteState struct {
	remotes     []git.Remote
	branches    []git.Branch // remote-tracking branches, for ahead/behind
	lastFetch   time.Time
	lastFetchOK bool

	index, top int
	loading    bool
	err        string

	// branchIndex/branchTop scroll the selected remote's tracked branches.
	branchIndex, branchTop int

	filter    string
	filtering bool

	inspect tideui.PaneScroller
}

func (r *remoteState) visible() []git.Remote {
	if r.filter == "" {
		return r.remotes
	}
	var out []git.Remote
	for _, remote := range r.remotes {
		if strings.Contains(strings.ToLower(remote.Name), strings.ToLower(r.filter)) {
			out = append(out, remote)
		}
	}
	return out
}

func (r *remoteState) current() (git.Remote, bool) {
	list := r.visible()
	if r.index < 0 || r.index >= len(list) {
		return git.Remote{}, false
	}
	return list[r.index], true
}

// branchesFor returns the remote-tracking branches belonging to a remote.
func (r *remoteState) branchesFor(name string) []git.Branch {
	var out []git.Branch
	for _, branch := range r.branches {
		if branch.RemoteName == name {
			out = append(out, branch)
		}
	}
	return out
}

type remotesMsg struct {
	id          int
	remotes     []git.Remote
	branches    []git.Branch
	lastFetch   time.Time
	lastFetchOK bool
	err         error
}

func (m *Model) openRemotes() tea.Cmd {
	if m.remotes == nil {
		m.remotes = &remoteState{}
	}
	m.screen = screenRemotes
	m.focus = 0
	m.notice = ""
	return m.loadRemotes()
}

func (m *Model) loadRemotes() tea.Cmd {
	if m.remotes == nil {
		m.remotes = &remoteState{}
	}
	m.remotes.loading = true
	m.remotes.err = ""
	id, repo := m.remoteID+1, m.repo
	m.remoteID++
	return tea.Batch(func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
		defer cancel()
		msg := remotesMsg{id: id}
		remotes, err := repo.Remotes(ctx)
		if err != nil {
			msg.err = err
			return msg
		}
		msg.remotes = remotes
		if _, remote, err := repo.Branches(ctx); err == nil {
			msg.branches = remote
		}
		if when, ok := repo.LastFetchTime(ctx); ok {
			msg.lastFetch, msg.lastFetchOK = when, true
		}
		return msg
	}, pulse())
}

func (m *Model) handleRemotesResult(msg tea.Msg) (bool, tea.Cmd) {
	done, ok := msg.(remotesMsg)
	if !ok {
		return false, nil
	}
	r := m.remotes
	if r == nil || done.id != m.remoteID {
		return true, nil
	}
	r.loading = false
	if done.err != nil {
		r.err = done.err.Error()
		return true, nil
	}
	r.err = ""
	r.remotes = done.remotes
	m.remoteCache = done.remotes
	r.branches = done.branches
	r.lastFetch, r.lastFetchOK = done.lastFetch, done.lastFetchOK
	r.index = min(r.index, max(0, len(r.visible())-1))
	r.branchIndex = min(r.branchIndex, max(0, len(r.selectedBranches())-1))
	return true, nil
}

func (m *Model) updateRemoteKey(key string, msg tea.KeyMsg) tea.Cmd {
	r := m.remotes
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
		return nil
	}
	switch key {
	case "/":
		m.focus = 0
		r.filtering = true
		return nil
	case "f":
		if remote, ok := r.current(); ok {
			return m.fetchRemote(remote.Name)
		}
		return m.fetchDefault()
	case "F":
		return m.fetchAll()
	case "p":
		return m.pullCurrent()
	case "P":
		return m.pushCurrent(false)
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
		page := max(1, m.historyPaneHeight()-2)
		if key == "pgdown" || key == "pgup" {
			delta *= page
		}
		switch m.focus {
		case 0:
			list := r.visible()
			if len(list) == 0 {
				return nil
			}
			r.index = min(max(0, r.index+delta), len(list)-1)
			// A different remote has a different branch list; start at its top.
			r.branchIndex, r.branchTop = 0, 0
		case 1:
			branches := r.selectedBranches()
			if len(branches) == 0 {
				return nil
			}
			switch key {
			case "home":
				r.branchIndex = 0
			case "end":
				r.branchIndex = len(branches) - 1
			default:
				r.branchIndex = min(max(0, r.branchIndex+delta), len(branches)-1)
			}
		default:
			// The inspector's line count is only known while rendering, and
			// the renderer clamps an over-scroll, so a large step is safe.
			switch key {
			case "home":
				r.inspect.ScrollToTop()
			case "end":
				r.inspect.ScrollDown(1 << 30)
			default:
				if delta < 0 {
					r.inspect.ScrollUp(-delta)
				} else {
					r.inspect.ScrollDown(delta)
				}
			}
		}
		return nil
	}
	return nil
}

// selectedBranches is the tracked-branch list of the currently selected remote.
func (r *remoteState) selectedBranches() []git.Branch {
	remote, ok := r.current()
	if !ok {
		return nil
	}
	return r.branchesFor(remote.Name)
}

// snapshotRemotes resolves the remote list synchronously for an action that is
// about to run. It reads only local configuration, and the result is cached so
// the Remotes screen does not immediately re-read it.
func (m *Model) snapshotRemotes() ([]git.Remote, error) {
	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
	defer cancel()
	remotes, err := m.repo.Remotes(ctx)
	if err == nil {
		m.remoteCache = remotes
	}
	return remotes, err
}

// branchName is the current branch, or "" when HEAD is detached or unborn.
func (m *Model) branchName() string {
	if m.head.Detached {
		return ""
	}
	name := m.head.Branch
	if name == "" {
		name = m.status.Branch
	}
	switch name {
	case "", "(detached)", "(initial)":
		return ""
	}
	return name
}

// fetchDefault fetches the repository's default remote, asking which one when
// the choice is genuinely ambiguous.
func (m *Model) fetchDefault() tea.Cmd {
	if m.busy {
		return nil
	}
	remotes, err := m.snapshotRemotes()
	if err != nil {
		m.notice = err.Error()
		return nil
	}
	if len(remotes) == 0 {
		m.notice = "This repository has no remote to fetch"
		return nil
	}
	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
	defer cancel()
	if name, err := m.repo.DefaultRemote(ctx, remotes); err == nil && name != "" {
		return m.fetchRemote(name)
	}
	if len(remotes) == 1 {
		return m.fetchRemote(remotes[0].Name)
	}
	return m.chooseRemote(choiceFetchRemote, "fetch", "Choose a remote to fetch", remotes, false)
}

// chooseFetchRemote is the palette's explicit remote chooser.
func (m *Model) chooseFetchRemote() tea.Cmd {
	if m.busy {
		return nil
	}
	remotes, err := m.snapshotRemotes()
	if err != nil {
		m.notice = err.Error()
		return nil
	}
	if len(remotes) == 0 {
		m.notice = "This repository has no remote to fetch"
		return nil
	}
	return m.chooseRemote(choiceFetchRemote, "fetch", "Choose a remote to fetch", remotes, false)
}

func (m *Model) fetchRemote(remote string) tea.Cmd {
	if m.busy {
		return nil
	}
	op := &operationState{kind: "fetch", verb: "Fetching", title: "Fetch " + safeText(remote),
		target: remote, cancellable: true}
	op.retry = func() tea.Cmd { return m.fetchRemote(remote) }
	repo := m.repo
	return m.beginOperation(op, func(ctx context.Context, progress git.ProgressFunc) (git.Result, error) {
		return repo.Fetch(ctx, remote, false, progress)
	})
}

func (m *Model) fetchAll() tea.Cmd {
	if m.busy {
		return nil
	}
	op := &operationState{kind: "fetch", verb: "Fetching", title: "Fetch all remotes",
		target: "all remotes", cancellable: true}
	op.retry = m.fetchAll
	repo := m.repo
	return m.beginOperation(op, func(ctx context.Context, progress git.ProgressFunc) (git.Result, error) {
		return repo.Fetch(ctx, "", true, progress)
	})
}

// pullCurrent pulls the current branch. A missing upstream is surfaced with a
// deliberate offer rather than silently guessing at one.
func (m *Model) pullCurrent() tea.Cmd {
	if m.busy {
		return nil
	}
	if m.branchName() == "" {
		m.notice = "Cannot pull with a detached HEAD"
		return nil
	}
	if m.status.Upstream == "" {
		remotes, err := m.snapshotRemotes()
		if err != nil {
			m.notice = err.Error()
			return nil
		}
		if len(remotes) == 0 {
			m.notice = safeText(m.branchName()) + " has no upstream and no remote is configured"
			return nil
		}
		return m.chooseRemote(choicePushRemote, "no upstream",
			m.branchName()+" has no upstream · choose a remote to push and set one", remotes, true)
	}
	target := m.status.Upstream
	op := &operationState{kind: "pull", verb: "Pulling", title: "Pull " + safeText(target),
		target: target, cancellable: false}
	op.retry = m.pullCurrent
	repo := m.repo
	return m.beginOperation(op, func(ctx context.Context, progress git.ProgressFunc) (git.Result, error) {
		return repo.Pull(ctx, "", "", progress)
	})
}

// pushCurrent publishes the current branch, respecting push.default when an
// upstream exists and offering an explicit upstream setup when it does not.
func (m *Model) pushCurrent(setUpstream bool) tea.Cmd {
	if m.busy {
		return nil
	}
	branch := m.branchName()
	if branch == "" {
		m.notice = "Cannot push with a detached HEAD"
		return nil
	}
	if m.status.Upstream != "" && !setUpstream {
		target := m.status.Upstream
		op := &operationState{kind: "push", verb: "Pushing", title: "Push " + safeText(branch),
			target: target, cancellable: true}
		op.retry = func() tea.Cmd { return m.pushCurrent(false) }
		repo := m.repo
		return m.beginOperation(op, func(ctx context.Context, progress git.ProgressFunc) (git.Result, error) {
			// No explicit refspec: Git's own push.default decides.
			return repo.Push(ctx, git.PushOptions{}, progress)
		})
	}
	remotes, err := m.snapshotRemotes()
	if err != nil {
		m.notice = err.Error()
		return nil
	}
	if len(remotes) == 0 {
		m.notice = "This repository has no remote to push to"
		return nil
	}
	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
	defer cancel()
	remote, err := m.repo.RemoteForBranch(ctx, branch)
	if err != nil {
		m.notice = err.Error()
		return nil
	}
	if remote == "" {
		if name, err := m.repo.DefaultRemote(ctx, remotes); err == nil {
			remote = name
		}
	}
	if remote == "" {
		return m.chooseRemote(choicePushRemote, "push · choose remote",
			"Push "+branch+" and set its upstream", remotes, true)
	}
	return m.pushToRemote(remote, true)
}

// pushToRemote pushes a branch to an explicitly chosen remote, recording the
// upstream when asked.
func (m *Model) pushToRemote(remote string, setUpstream bool) tea.Cmd {
	if m.busy {
		return nil
	}
	branch := m.branchName()
	if branch == "" {
		m.notice = "Cannot push with a detached HEAD"
		return nil
	}
	title := "Push " + safeText(branch) + " → " + safeText(remote)
	target := remote + "/" + branch
	if !setUpstream {
		title = "Push " + safeText(branch)
	}
	op := &operationState{kind: "push", verb: "Pushing", title: title, target: target, cancellable: true}
	op.retry = func() tea.Cmd { return m.pushToRemote(remote, setUpstream) }
	repo := m.repo
	return m.beginOperation(op, func(ctx context.Context, progress git.ProgressFunc) (git.Result, error) {
		return repo.Push(ctx, git.PushOptions{Remote: remote, Branch: branch, SetUpstream: setUpstream}, progress)
	})
}
