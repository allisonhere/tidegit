package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/allisonhere/tidegit/internal/diff"
	"github.com/allisonhere/tidegit/internal/git"
	"github.com/allisonhere/tideui"
	tea "github.com/charmbracelet/bubbletea"
)

// inspectMode selects what the conflict inspector compares.
type inspectMode int

const (
	inspectRegion inspectMode = iota
	inspectWorking
	inspectOursBase
	inspectTheirsBase
	inspectModes = 4
)

func (mode inspectMode) label(op git.OperationKind) string {
	switch mode {
	case inspectWorking:
		return "WORKING RESULT"
	case inspectOursBase:
		return "OURS ↔ BASE"
	case inspectTheirsBase:
		if op != git.OpNone {
			return "INCOMING ↔ BASE"
		}
		return "THEIRS ↔ BASE"
	default:
		return "CONFLICT REGION"
	}
}

// conflictState backs the Conflicts screen: the grouped unmerged files, the
// selected file's regions and stages, and the inspector's view mode.
type conflictState struct {
	conflicts []git.Conflict
	index     int
	// top is the first flattened row drawn, so the viewport survives a reload.
	top     int
	loading bool
	err     string

	filter    string
	filtering bool

	detailFor     string
	detailLoading bool
	stages        git.ConflictStages
	regions       []git.ConflictRegion
	working       []string
	workingOK     bool
	workingBinary bool

	regionIndex, regionTop int
	mode                   inspectMode

	oursView, theirsView   diffView
	oursTitle, theirsTitle string

	inspect tideui.PaneScroller

	opMsg string
}

// conflictRow is a flattened list item: either a group caption or a conflict.
type conflictRow struct {
	caption  string
	conflict git.Conflict
	index    int
}

func (c *conflictState) visible() []git.Conflict {
	if c.filter == "" {
		return c.conflicts
	}
	var out []git.Conflict
	for _, conflict := range c.conflicts {
		if strings.Contains(strings.ToLower(conflict.File.Path), strings.ToLower(c.filter)) {
			out = append(out, conflict)
		}
	}
	return out
}

// rows groups conflicts by kind, in a fixed order so the list is stable.
func (c *conflictState) rows() []conflictRow {
	list := c.visible()
	order := []git.ConflictKind{
		git.ConflictBothModified, git.ConflictBothAdded,
		git.ConflictDeletedByUs, git.ConflictDeletedByThem,
		git.ConflictAddedByUs, git.ConflictAddedByThem,
		git.ConflictBothDeleted, git.ConflictOther,
	}
	var rows []conflictRow
	index := 0
	for _, kind := range order {
		var group []git.Conflict
		for _, conflict := range list {
			if conflict.Kind == kind {
				group = append(group, conflict)
			}
		}
		if len(group) == 0 {
			continue
		}
		rows = append(rows, conflictRow{caption: kind.Group() + fmt.Sprintf("  %d", len(group))})
		for _, conflict := range group {
			rows = append(rows, conflictRow{conflict: conflict, index: index})
			index++
		}
	}
	return rows
}

func (c *conflictState) current() (git.Conflict, bool) {
	list := c.visible()
	if c.index < 0 || c.index >= len(list) {
		return git.Conflict{}, false
	}
	return list[c.index], true
}

func (c *conflictState) currentRegion() (git.ConflictRegion, bool) {
	if c.regionIndex < 0 || c.regionIndex >= len(c.regions) {
		return git.ConflictRegion{}, false
	}
	return c.regions[c.regionIndex], true
}

type conflictsMsg struct {
	id        int
	conflicts []git.Conflict
	err       error
}
type conflictDetailMsg struct {
	id          int
	path        string
	stages      git.ConflictStages
	working     string
	workingOK   bool
	binary      bool
	regions     []git.ConflictRegion
	oursPatch   diff.Patch
	theirsPatch diff.Patch
	oursTitle   string
	theirsTitle string
	err         error
}

func (m *Model) openConflicts() tea.Cmd {
	if m.conflicts == nil {
		m.conflicts = &conflictState{}
	}
	m.screen = screenConflicts
	m.focus = 0
	m.notice = ""
	return tea.Batch(m.loadConflicts(), m.loadConflictDetail())
}

func (m *Model) loadConflicts() tea.Cmd {
	if m.conflicts == nil {
		m.conflicts = &conflictState{}
	}
	c := m.conflicts
	c.loading = true
	c.err = ""
	m.conflictID++
	id, repo := m.conflictID, m.repo
	return tea.Batch(func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
		defer cancel()
		conflicts, err := repo.Conflicts(ctx)
		return conflictsMsg{id: id, conflicts: conflicts, err: err}
	}, pulse())
}

// loadConflictDetail reads the selected file's stages, working content and
// conflict regions, then builds the two stage comparisons the inspector cycles
// through. All of it is derived from Git's index, never from scanning arbitrary
// text for markers.
func (m *Model) loadConflictDetail() tea.Cmd {
	c := m.conflicts
	if c == nil {
		return nil
	}
	conflict, ok := c.current()
	if !ok {
		c.detailFor, c.detailLoading = "", false
		c.regions, c.stages, c.working = nil, git.ConflictStages{}, nil
		return nil
	}
	if c.detailFor == conflict.File.Path {
		return nil
	}
	m.conflictDetailID++
	id, repo, path := m.conflictDetailID, m.repo, conflict.File.Path
	c.detailLoading = true
	c.detailFor, c.regions, c.working = "", nil, nil
	c.regionIndex, c.regionTop = 0, 0
	c.mode = inspectRegion
	c.inspect.ScrollToTop()
	op := m.repoState.Operation
	ctxLines := m.diffContext()
	return tea.Batch(func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
		defer cancel()
		msg := conflictDetailMsg{id: id, path: path}
		stages, err := repo.Unmerged(ctx)
		if err != nil {
			msg.err = err
			return msg
		}
		msg.stages = stages[path]
		content, ok, err := repo.WorkingFile(path)
		if err != nil {
			msg.err = err
			return msg
		}
		msg.working, msg.workingOK = content, ok
		msg.binary = strings.IndexByte(content, 0) >= 0
		if !msg.binary {
			msg.regions = git.ParseConflictRegions(content)
		}
		oursTitle, theirsTitle := "OURS", "THEIRS"
		if op == git.OpRebase {
			oursTitle, theirsTitle = "UPSTREAM", "YOUR COMMIT"
		}
		msg.oursTitle, msg.theirsTitle = oursTitle, theirsTitle
		if patch, err := repo.BlobDiffWith(ctx, msg.stages.Base, msg.stages.Ours, git.DiffOptions{Context: ctxLines}); err == nil {
			msg.oursPatch = diff.Parse(patch)
			msg.oursPatch.Label = oursTitle + " ↔ BASE"
		}
		if patch, err := repo.BlobDiffWith(ctx, msg.stages.Base, msg.stages.Theirs, git.DiffOptions{Context: ctxLines}); err == nil {
			msg.theirsPatch = diff.Parse(patch)
			msg.theirsPatch.Label = theirsTitle + " ↔ BASE"
		}
		return msg
	}, pulse())
}

func (m *Model) handleConflictResult(msg tea.Msg) (bool, tea.Cmd) {
	c := m.conflicts
	switch msg := msg.(type) {
	case conflictsMsg:
		if c == nil || msg.id != m.conflictID {
			return true, nil
		}
		c.loading = false
		if msg.err != nil {
			c.err = msg.err.Error()
			c.conflicts = nil
			return true, nil
		}
		c.err = ""
		c.conflicts = msg.conflicts
		c.index = min(c.index, max(0, len(c.visible())-1))
		return true, m.loadConflictDetail()
	case conflictDetailMsg:
		if c == nil || msg.id != m.conflictDetailID {
			return true, nil
		}
		c.detailLoading = false
		if msg.err != nil {
			c.err = msg.err.Error()
			return true, nil
		}
		c.detailFor = msg.path
		c.stages, c.working, c.workingOK = msg.stages, strings.Split(msg.working, "\n"), msg.workingOK
		c.workingBinary, c.regions = msg.binary, msg.regions
		c.oursView.reset(msg.oursPatch, msg.oursPatch.Label)
		c.theirsView.reset(msg.theirsPatch, msg.theirsPatch.Label)
		c.oursTitle, c.theirsTitle = msg.oursTitle, msg.theirsTitle
		c.regionIndex, c.regionTop = 0, 0
		c.inspect.ScrollToTop()
		return true, nil
	}
	return false, nil
}

// reloadConflicts refreshes the conflict list and the selected file's detail.
func (m *Model) reloadConflicts() tea.Cmd {
	c := m.conflicts
	if c != nil {
		c.detailFor = ""
	}
	return tea.Batch(m.loadConflicts(), m.loadConflictDetail())
}

func (m *Model) updateConflictKey(key string, msg tea.KeyMsg) tea.Cmd {
	c := m.conflicts
	if c.filtering {
		switch key {
		case "enter":
			c.filtering = false
			return nil
		case "esc":
			c.filtering = false
			c.filter = ""
			c.index = 0
		case "backspace":
			runes := []rune(c.filter)
			if len(runes) > 0 {
				c.filter = string(runes[:len(runes)-1])
			}
		default:
			if msg.Type == tea.KeyRunes {
				c.filter += string(msg.Runes)
			}
		}
		c.index = min(c.index, max(0, len(c.visible())-1))
		return m.loadConflictDetail()
	}
	switch key {
	case "/":
		m.focus = 0
		c.filtering = true
		return nil
	case "v":
		c.mode = (c.mode + 1) % inspectModes
		c.oursView.scroll.ScrollToTop()
		c.theirsView.scroll.ScrollToTop()
		return nil
	case "[":
		return m.moveRegion(-1)
	case "]":
		return m.moveRegion(1)
	case "o":
		return m.resolveSelected(true, false)
	case "O":
		return m.resolveSelected(true, true)
	case "t":
		return m.resolveSelected(false, false)
	case "T":
		return m.resolveSelected(false, true)
	case "b":
		return m.keepBothSelected()
	case "m":
		return m.markSelectedResolved()
	case "e":
		return m.openConflictInEditor()
	case "c":
		return m.continueOperation()
	case "x":
		return m.skipOperation()
	case "A":
		return m.confirmAbortOperation()
	case "y":
		conflict, ok := c.current()
		if !ok {
			return nil
		}
		if err := (systemClipboard{}).Write(conflict.File.Path); err != nil {
			m.notice = "Clipboard: " + err.Error()
			return nil
		}
		m.notice = "Copied " + safeText(conflict.File.Path)
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
			list := c.visible()
			if len(list) == 0 {
				return nil
			}
			c.index = min(max(0, c.index+delta), len(list)-1)
			return m.loadConflictDetail()
		case 1:
			if delta < 0 {
				c.inspect.ScrollUp(-delta)
			} else {
				c.inspect.ScrollDown(delta)
			}
		default:
			if c.mode == inspectRegion || c.mode == inspectWorking {
				if delta < 0 {
					c.inspect.ScrollUp(-delta)
				} else {
					c.inspect.ScrollDown(delta)
				}
				return nil
			}
			view := &c.oursView
			if c.mode == inspectTheirsBase {
				view = &c.theirsView
			}
			flat := view.flat(m.diffOptionsFrom(), true)
			if delta < 0 {
				view.line = max(0, view.line+delta)
			} else {
				view.line = min(view.line+delta, max(0, len(flat)-1))
			}
			view.ensureVisible(m.historyPaneHeight() - 3)
		}
		return nil
	}
	return nil
}

func (m *Model) moveRegion(delta int) tea.Cmd {
	c := m.conflicts
	if len(c.regions) == 0 {
		return nil
	}
	c.regionIndex = min(max(0, c.regionIndex+delta), len(c.regions)-1)
	c.mode = inspectRegion
	c.inspect.ScrollToTop()
	return nil
}

// resolveSelected writes one side into the working file, optionally marking the
// file resolved in the same step. Writing alone leaves the conflict in place,
// which is what the lowercase keys do.
func (m *Model) resolveSelected(ours, mark bool) tea.Cmd {
	conflict, ok := m.conflicts.current()
	if !ok || m.busy {
		return nil
	}
	side, verb := "theirs", "Applied theirs to "
	if ours {
		side, verb = "ours", "Applied ours to "
	}
	notice := verb + safeText(conflict.File.Path)
	if mark {
		notice = "Resolved " + safeText(conflict.File.Path) + " using " + side
	}
	return m.runRecovery("Resolving "+safeText(conflict.File.Path), notice, func(ctx context.Context, repo git.Repository) error {
		if ours {
			return repo.ResolveOurs(ctx, conflict.File.Path, mark)
		}
		return repo.ResolveTheirs(ctx, conflict.File.Path, mark)
	})
}

func (m *Model) keepBothSelected() tea.Cmd {
	conflict, ok := m.conflicts.current()
	if !ok || m.busy {
		return nil
	}
	return m.runRecovery("Combining "+safeText(conflict.File.Path),
		"Kept both sides in "+safeText(conflict.File.Path),
		func(ctx context.Context, repo git.Repository) error {
			return repo.KeepBoth(ctx, conflict.File.Path)
		})
}

func (m *Model) markSelectedResolved() tea.Cmd {
	conflict, ok := m.conflicts.current()
	if !ok || m.busy {
		return nil
	}
	return m.runRecovery("Marking resolved", "Marked "+safeText(conflict.File.Path)+" resolved",
		func(ctx context.Context, repo git.Repository) error {
			return repo.MarkResolved(ctx, conflict.File.Path)
		})
}

// editorFinishedMsg reports that an external editor returned. Editing never
// marks a file resolved; it only refreshes what TideGit knows about the file.
type editorFinishedMsg struct {
	path string
	err  error
}

func (m *Model) openConflictInEditor() tea.Cmd {
	conflict, ok := m.conflicts.current()
	if !ok {
		return nil
	}
	editor := strings.TrimSpace(os.Getenv("VISUAL"))
	if editor == "" {
		editor = strings.TrimSpace(os.Getenv("EDITOR"))
	}
	if editor == "" {
		m.notice = "Set $EDITOR or $VISUAL to edit files"
		return nil
	}
	if m.repo.Root == "" {
		return nil
	}
	parts := strings.Fields(editor)
	full := filepath.Join(m.repo.Root, conflict.File.Path)
	cmd := exec.Command(parts[0], append(parts[1:], full)...)
	cmd.Dir = m.repo.Root
	path := conflict.File.Path
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editorFinishedMsg{path: path, err: err}
	})
}

func (m *Model) handleEditorFinished(msg editorFinishedMsg) tea.Cmd {
	if msg.err != nil {
		m.notice = "Editor: " + msg.err.Error()
	} else {
		m.notice = "Returned from editing " + safeText(msg.path)
	}
	if m.conflicts == nil {
		return m.refresh()
	}
	m.conflicts.detailFor = ""
	return tea.Batch(m.loadConflicts(), m.loadConflictDetail())
}
