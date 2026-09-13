package ui

import (
	"context"
	"strings"

	"github.com/allisonhere/tidegit/internal/git"
	tea "github.com/charmbracelet/bubbletea"
)

// promptReset offers the three reset modes with their exact consequences, then
// confirms the chosen one. Soft and mixed are recoverable through the reflog;
// hard is marked destructive and asks with stronger wording.
func (m *Model) promptReset(target, subject string) tea.Cmd {
	if m.busy {
		return nil
	}
	label := shortTarget(target)
	if subject != "" {
		label += " · " + clip(safeText(subject), 40)
	}
	m.choice = &choiceState{
		title: "reset",
		label: "Reset to " + label,
		options: []choiceOption{
			{label: "Soft reset", hint: "keep changes staged", value: "soft",
				run: func(string) tea.Cmd { return m.confirmReset(git.ResetSoft, target) }},
			{label: "Mixed reset", hint: "keep changes unstaged", value: "mixed",
				run: func(string) tea.Cmd { return m.confirmReset(git.ResetMixed, target) }},
			{label: "Hard reset", hint: "discard tracked changes", value: "hard",
				run: func(string) tea.Cmd { return m.confirmReset(git.ResetHard, target) }},
		},
	}
	return nil
}

// confirmReset states exactly what the mode changes and whether the result is
// recoverable, then requires the explicit accept key.
func (m *Model) confirmReset(mode git.ResetMode, target string) tea.Cmd {
	label := shortTarget(target)
	body := mode.Description() + "\n\n"
	if mode.Destructive() {
		body += "This will discard tracked working-tree and staged changes.\nUncommitted changes may not be recoverable."
	} else {
		body += "Committed work is not lost: it stays reachable through the reflog."
	}
	m.confirm = &confirmState{
		title:  mode.Label() + "?",
		body:   "Reset to " + label + "?\n\n" + body,
		accept: "R",
		danger: mode.Destructive(),
		run: func() tea.Cmd {
			return m.runRecovery(mode.Label(), "Reset to "+label,
				func(ctx context.Context, repo git.Repository) error {
					_, err := repo.Reset(ctx, mode, target)
					return err
				})
		},
	}
	return nil
}

// promptUndoLastCommit guides the two safe undo shapes. Discarding everything
// is deliberately not offered here.
func (m *Model) promptUndoLastCommit() tea.Cmd {
	if m.busy {
		return nil
	}
	if m.status.OID == "" || m.status.OID == "(initial)" {
		m.notice = "There is no commit to undo"
		return nil
	}
	m.choice = &choiceState{
		title: "undo last commit",
		label: "Undo the last commit? HEAD moves back one commit; the commit stays in the reflog and can be restored.",
		options: []choiceOption{
			{label: "Keep changes staged", hint: "soft · index untouched", value: "soft",
				run: func(string) tea.Cmd { return m.confirmReset(git.ResetSoft, "HEAD~1") }},
			{label: "Keep changes unstaged", hint: "mixed · index reset", value: "mixed",
				run: func(string) tea.Cmd { return m.confirmReset(git.ResetMixed, "HEAD~1") }},
		},
	}
	return nil
}

// confirmRevertCommit reverts the commit selected in History. Revert is the
// safe way to undo published history, so it is offered prominently.
func (m *Model) confirmRevertCommit() tea.Cmd {
	if m.busy {
		return nil
	}
	c, ok := m.history.current()
	if !ok {
		m.notice = "Select a commit in History first"
		return nil
	}
	m.confirm = &confirmState{
		title:  "revert commit?",
		body:   "Revert " + c.Short + " · " + safeText(clip(c.Subject, 40)) + "?\n\nA new commit is created that undoes it.\nThe original commit is left in place, so shared history is unchanged.",
		accept: "R",
		run: func() tea.Cmd {
			return m.runRecovery("Reverting "+c.Short, "Reverted "+c.Short,
				func(ctx context.Context, repo git.Repository) error {
					_, err := repo.Revert(ctx, c.OID)
					return err
				})
		},
	}
	return nil
}

// confirmSwitchDetached checks out a reflog entry directly. Local changes are
// never discarded: Git refuses rather than overwriting them.
func (m *Model) confirmSwitchDetached(rev, subject string) tea.Cmd {
	if m.busy {
		return nil
	}
	label := shortTarget(rev)
	if subject != "" {
		label += " · " + clip(safeText(subject), 40)
	}
	m.confirm = &confirmState{
		title:  "switch to commit?",
		body:   "Check out " + label + " with a detached HEAD?\n\nYou are no longer on a branch.\nCreate a branch instead unless you mean to look around.",
		accept: "s",
		run: func() tea.Cmd {
			return m.runRecovery("Switching to "+shortTarget(rev), "Detached at "+shortTarget(rev),
				func(ctx context.Context, repo git.Repository) error {
					return repo.SwitchDetached(ctx, rev)
				})
		},
	}
	return nil
}

// promptStatusRestore offers the file-level restore shapes for the selected
// Status entry, keeping "discard the working tree" distinct from "restore from
// HEAD".
func (m *Model) promptStatusRestore() tea.Cmd {
	if m.busy {
		return nil
	}
	files := m.files()
	if len(files) == 0 {
		return nil
	}
	file := files[m.selected]
	m.choice = &choiceState{
		title: "restore file",
		label: "Restore " + safeText(clip(file.Path, 46)),
		options: []choiceOption{
			{label: "Discard working-tree changes", hint: "keep the index", value: "worktree",
				run: func(string) tea.Cmd { return m.confirmRestoreWorktree(file) }},
			{label: "Restore from HEAD", hint: "index and working tree", value: "head",
				run: func(string) tea.Cmd { return m.confirmRestoreHEAD(file) }},
		},
	}
	return nil
}

func (m *Model) confirmRestoreWorktree(file git.File) tea.Cmd {
	m.confirm = &confirmState{
		title:  "discard working-tree changes?",
		body:   "Discard uncommitted changes to " + safeText(file.Path) + "?\n\nStaged content is kept. This only affects the working tree.",
		accept: "D",
		danger: true,
		run: func() tea.Cmd {
			return m.runRecovery("Restoring "+safeText(file.Path), "Restored working tree for "+safeText(file.Path),
				func(ctx context.Context, repo git.Repository) error {
					return repo.RestoreWorktree(ctx, file.Path)
				})
		},
	}
	return nil
}

func (m *Model) confirmRestoreHEAD(file git.File) tea.Cmd {
	m.confirm = &confirmState{
		title:  "restore from HEAD?",
		body:   "Replace " + safeText(file.Path) + " with its content at HEAD?\n\nBoth the index and the working tree are reset.\nStaged and unstaged changes to this file are discarded.",
		accept: "D",
		danger: true,
		run: func() tea.Cmd {
			return m.runRecovery("Restoring "+safeText(file.Path), "Restored "+safeText(file.Path)+" from HEAD",
				func(ctx context.Context, repo git.Repository) error {
					return repo.RestoreFrom(ctx, "HEAD", file.Path)
				})
		},
	}
	return nil
}

// shortTarget abbreviates a full object id for display without hiding what the
// action targets.
func shortTarget(target string) string {
	if len(target) == 40 && isHex(target) {
		return target[:8]
	}
	return safeText(target)
}

func isHex(s string) bool {
	for _, r := range s {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return s != ""
}

// paletteReset picks a reset target from whatever is selected: a reflog entry,
// a history commit, or nothing. It never defaults to a destructive target.
func (m *Model) paletteReset() tea.Cmd {
	switch {
	case m.screen == screenReflog && m.reflog != nil:
		if entry, ok := m.reflog.current(); ok {
			return m.promptReset(entry.OID, entry.Subject)
		}
	case m.screen == screenHistory && m.history != nil:
		if c, ok := m.history.current(); ok {
			return m.promptReset(c.OID, c.Subject)
		}
	}
	m.notice = "Select a reflog entry or a commit to reset to"
	return nil
}

// openRecoveryCenter is the discoverability hub for safety actions. It stays
// small on purpose: it points at the real tools rather than duplicating them.
func (m *Model) openRecoveryCenter() tea.Cmd {
	options := []choiceOption{
		{label: "Open the reflog", hint: "7", value: "reflog",
			run: func(string) tea.Cmd { return m.goToScreen(screenReflog) }},
		{label: "Undo last commit", hint: "keep staged or unstaged", value: "undo",
			run: func(string) tea.Cmd { return m.promptUndoLastCommit() }},
	}
	if m.repoState.InProgress() {
		options = append(options, choiceOption{
			label: "Abort " + m.repoState.Operation.Label(), hint: "restore before it began", value: "abort",
			run: func(string) tea.Cmd { return m.confirmAbortOperation() },
		})
	}
	if m.screen == screenHistory && m.history != nil {
		options = append(options, choiceOption{
			label: "Revert selected commit", hint: "History · R", value: "revert",
			run: func(string) tea.Cmd { return m.confirmRevertCommit() },
		})
	}
	m.choice = &choiceState{
		title:   "recovery",
		label:   "Safe ways back. Every action here is reversible through the reflog unless it says otherwise.",
		options: options,
	}
	return nil
}
