package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/allisonhere/tidegit/internal/git"
	"github.com/allisonhere/tideui"
	tea "github.com/charmbracelet/bubbletea"
)

// progressBuffer collects an operation's output while it runs. Git writes from
// a background goroutine while the UI renders, so every access is guarded.
// Git redraws progress in place with carriage returns, so each segment is
// treated as its own line and the latest one is kept for the live chip.
type progressBuffer struct {
	mu     sync.Mutex
	lines  []string
	latest string
	limit  int
}

const progressLineLimit = 4000

func (p *progressBuffer) write(chunk string) {
	if p == nil {
		return
	}
	chunk = strings.ReplaceAll(chunk, "\r\n", "\n")
	chunk = strings.ReplaceAll(chunk, "\r", "\n")
	limit := p.limit
	if limit <= 0 {
		limit = progressLineLimit
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, line := range strings.Split(chunk, "\n") {
		line = strings.TrimRight(line, " ")
		if strings.TrimSpace(line) == "" {
			continue
		}
		p.latest = line
		p.lines = append(p.lines, line)
	}
	if len(p.lines) > limit {
		p.lines = append([]string(nil), p.lines[len(p.lines)-limit:]...)
	}
}

func (p *progressBuffer) snapshot() (latest string, lines []string) {
	if p == nil {
		return "", nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.latest, append([]string(nil), p.lines...)
}

// operationState backs the operation-details panel. It is deliberately generic:
// fetch, pull, push and stash all report through it, and the same component is
// ready for rebase, cherry-pick and clone later.
type operationState struct {
	kind    string // fetch, pull, push or stash
	verb    string // "Fetching", "Pulling", "Pushing", "Stashing"
	title   string // "Fetch origin"
	target  string // "origin" or "origin/main"
	summary string // concise success/failure wording, set when finished

	running bool
	ok      bool
	done    bool
	started time.Time
	elapsed time.Duration

	raw      *progressBuffer
	show     bool // the details panel is visible
	expanded bool // raw output is shown rather than the summary
	scroll   tideui.PaneScroller

	cancellable bool
	cancel      context.CancelFunc

	// retry rebuilds and reruns the operation. It is only set when rerunning is
	// safe and makes sense.
	retry func() tea.Cmd
}

// operationDoneMsg reports a finished background operation. The worker only
// builds the message; model state is never written from the goroutine.
type operationDoneMsg struct {
	kind   string
	result git.Result
	err    error
}

// beginOperation starts one serialized background operation. The model's busy
// gate stays closed for its duration, so pull, push, fetch, stash and the other
// mutations can never race each other.
func (m *Model) beginOperation(op *operationState, run func(context.Context, git.ProgressFunc) (git.Result, error)) tea.Cmd {
	if op.raw == nil {
		op.raw = &progressBuffer{limit: m.operationRetention()}
	}
	op.running = true
	op.ok = false
	op.done = false
	op.started = time.Now()
	m.op = op
	m.busy = true
	m.operation = op.title
	m.err = ""
	m.notice = ""
	ctx, cancel := context.WithCancel(m.ctx)
	op.cancel = cancel
	return tea.Batch(func() tea.Msg {
		result, err := run(ctx, op.raw.write)
		cancel()
		return operationDoneMsg{kind: op.kind, result: result, err: err}
	}, pulse())
}

// handleOperationResult records a finished operation, opens the details panel
// on failure, and refreshes whatever screen is showing.
func (m *Model) handleOperationResult(msg tea.Msg) (bool, tea.Cmd) {
	done, ok := msg.(operationDoneMsg)
	if !ok {
		return false, nil
	}
	op := m.op
	m.busy = false
	m.operation = ""
	if op == nil || op.kind != done.kind {
		return true, nil
	}
	op.running = false
	op.done = true
	op.elapsed = time.Since(op.started)
	if done.err != nil {
		op.ok = false
		op.summary = operationFailure(done.err)
		op.show = true
		op.expanded = false
		m.notice = ""
	} else {
		op.ok = true
		op.summary = operationSuccess(done.kind, op.target, done.result)
		m.notice = op.summary
	}
	return true, m.refreshAfterOperation()
}

// operationFailure turns a failure into a plain sentence while the raw Git
// output stays available in the panel.
func operationFailure(err error) string {
	var remote *git.RemoteError
	if errors.As(err, &remote) {
		return remote.Message
	}
	var conflict *git.StashConflictError
	if errors.As(err, &conflict) {
		return conflict.Error()
	}
	if errors.Is(err, context.Canceled) {
		return "Operation cancelled"
	}
	return err.Error()
}

// operationSuccess words a successful operation concisely.
func operationSuccess(kind, target string, res git.Result) string {
	switch kind {
	case "fetch":
		upToDate, updates := git.DescribeFetch(target, res)
		if upToDate && target == "all remotes" {
			return "All remotes are up to date"
		}
		if upToDate {
			return target + " is up to date"
		}
		return fmt.Sprintf("Fetched %s · %s", target, plural(updates, "branch updated"))
	case "push":
		upToDate, updates := git.DescribePush(res)
		if upToDate {
			return "Everything is already up to date"
		}
		return fmt.Sprintf("Pushed %s to %s", plural(updates, "ref"), target)
	case "pull":
		return git.PullOutcome(res)
	default:
		return git.DescribeStashCreate(res)
	}
}

// refreshAfterOperation re-reads everything an operation can have moved:
// refs, HEAD, ahead/behind, history, branch tracking and the working tree.
// Nothing cached from before the operation may be shown afterwards. The
// working-tree snapshot is always re-read because the header, tracking state
// and Status screen all depend on it.
func (m *Model) refreshAfterOperation() tea.Cmd {
	if m.history != nil {
		m.history.cache, m.history.order = nil, nil
		m.history.commits, m.history.graph = nil, nil
	}
	if m.branches != nil {
		m.branches.commitsFor, m.branches.extraFor = "", ""
		m.branches.commits, m.branches.graph = nil, nil
	}
	if m.stash != nil {
		m.stash.filesFor = ""
		m.stash.files = nil
	}
	var cmds []tea.Cmd
	switch m.screen {
	case screenHistory:
		cmds = append(cmds, m.loadHistory(false))
	case screenBranches:
		cmds = append(cmds, m.loadBranches())
	case screenStash:
		cmds = append(cmds, m.loadStashes(), m.loadStashDetail())
	case screenRemotes:
		cmds = append(cmds, m.loadRemotes())
	}
	// A stash operation started from Status still refreshes the stash list so
	// the next visit shows it without a reload.
	if m.stash != nil && m.screen != screenStash {
		cmds = append(cmds, m.loadStashes())
	}
	cmds = append(cmds, m.refreshStatus())
	return tea.Batch(cmds...)
}

// refreshStatus reloads the working-tree snapshot without clearing the
// operation summary that was just placed in the notice bar.
func (m *Model) refreshStatus() tea.Cmd {
	notice := m.notice
	cmd := m.refresh()
	m.notice = notice
	return cmd
}

// operation running reports whether a background operation currently blocks.
func (m *Model) operationRunning() bool { return m.op != nil && m.op.running }

// cancelOperation terminates a cancellable running operation. Git is killed by
// its context; the result is handled as a normal failure.
func (m *Model) cancelOperation() {
	if m.op == nil || !m.op.running || !m.op.cancellable || m.op.cancel == nil {
		return
	}
	m.op.cancel()
	m.notice = m.op.verb + " cancelled"
}

// updateOperationKey handles the details panel while it is visible. Running and
// finished states share the panel; only the footer differs.
func (m *Model) updateOperationKey(key string) tea.Cmd {
	op := m.op
	if op == nil {
		return nil
	}
	switch key {
	case "e":
		op.expanded = !op.expanded
		op.scroll.ScrollToTop()
	case "r":
		if !op.running && op.retry != nil {
			return op.retry()
		}
	case "esc", "q":
		if op.running {
			if op.cancellable {
				m.cancelOperation()
			}
			return nil
		}
		op.show = false
	case "o":
		op.show = false
	case "j", "down":
		if op.expanded {
			op.scroll.ScrollDown(1)
		}
	case "k", "up":
		if op.expanded {
			op.scroll.ScrollUp(1)
		}
	case "pgdown":
		if op.expanded {
			op.scroll.ScrollDown(10)
		}
	case "pgup":
		if op.expanded {
			op.scroll.ScrollUp(10)
		}
	case "home":
		op.scroll.ScrollToTop()
	case "end":
		op.scroll.ScrollDown(len(op.rawLines()))
	}
	return nil
}

func (op *operationState) rawLines() []string {
	if op.raw == nil {
		return nil
	}
	_, lines := op.raw.snapshot()
	return lines
}

// formatElapsed renders an operation's duration compactly: sub-second work in
// milliseconds, then seconds, then minutes.
func formatElapsed(d time.Duration) string {
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	default:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
}

// operationPanel renders the reusable operation-details component.
func (m *Model) operationPanel(r tideui.Renderer) string {
	op := m.op
	width := min(72, m.width-6)
	inner := max(1, width-4)
	var rows []string

	// State line: an activity glyph while running, then a resolved mark.
	state := m.activity() + " " + muted(r, "running")
	switch {
	case op.running:
	case op.ok:
		state = r.Styles.DetailBody.Foreground(r.Styles.Theme.Unread).Bold(true).Render("✓ done")
	case op.done:
		state = r.Styles.DetailBody.Foreground(r.Styles.Theme.Error).Bold(true).Render("✗ failed")
	}
	elapsed := op.elapsed
	if op.running {
		elapsed = time.Since(op.started)
	}
	if !op.started.IsZero() && (op.running || op.done) {
		state += muted(r, " · "+formatElapsed(elapsed))
	}
	rows = append(rows, accent(r, clip(op.title, inner-10))+"  "+state, "")
	if op.target != "" {
		rows = append(rows, muted(r, "target  ")+r.Styles.DetailBody.Render(clip(op.target, inner-8)), "")
	}

	if op.running {
		latest, _ := op.raw.snapshot()
		if latest == "" {
			latest = op.verb + "…"
		}
		for _, line := range wrapText(latest, inner) {
			rows = append(rows, muted(r, line))
		}
	} else if op.expanded {
		lines := op.rawLines()
		header := "output"
		if len(lines) == 0 {
			header = "no output"
		}
		rows = append(rows, muted(r, header))
		visible := max(3, min(14, m.height-14))
		op.scroll.ClampTo(len(lines), visible)
		start := min(op.scroll.Offset(), max(0, len(lines)-visible))
		for _, line := range lines[start:min(len(lines), start+visible)] {
			rows = append(rows, r.Styles.DetailMeta.Italic(false).Render(clip(safeText(line), inner)))
		}
	} else {
		// The concise summary wraps rather than being truncated.
		for _, line := range wrapText(op.summary, inner) {
			rows = append(rows, r.Styles.DetailBody.Render(line))
		}
		if op.done && len(op.rawLines()) > 0 {
			rows = append(rows, "", muted(r, "Press e for the full Git output."))
		}
	}

	var hints []tideui.SoftHint
	switch {
	case op.running && op.cancellable:
		hints = []tideui.SoftHint{{Key: "Esc", Label: "cancel"}, {Key: "e", Label: "output"}, {Key: "o", Label: "hide"}}
	case op.running:
		hints = []tideui.SoftHint{{Key: "e", Label: "output"}, {Key: "o", Label: "hide"}}
	case op.retry != nil:
		hints = []tideui.SoftHint{{Key: "e", Label: "raw output"}, {Key: "r", Label: "retry"}, {Key: "Esc", Label: "close"}}
	default:
		hints = []tideui.SoftHint{{Key: "e", Label: "raw output"}, {Key: "Esc", Label: "close"}}
	}
	rows = append(rows, "", "")
	body := strings.Join(rows, "\n") + "\n" + r.RenderSoftHints(inner+2, hints...)

	title := op.kind
	if op.target != "" {
		title = op.kind + " · " + op.target
	}
	panel := r.SoftPanelOverlay(tideui.SoftPanel{Prefix: "tidegit", Title: title,
		Width: width, Content: inset(body, width)})
	return panel.Content
}
