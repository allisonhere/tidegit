package ui

import (
	"context"
	"fmt"
	"time"

	"github.com/allisonhere/tidegit/internal/git"
	tea "github.com/charmbracelet/bubbletea"
)

// recoveryMsg reports a completed resolution or recovery mutation together
// with the rescan that followed it, so no screen shows state from before the
// change. The worker only builds the message.
type recoveryMsg struct {
	operation       string
	notice          string
	err, refreshErr error
	status          git.Status
	state           git.RepoState
}

// runRecovery performs one repository-changing action under the same busy gate
// and mutation semaphore as everything else, then rescans status and operation
// state. It is the shared path for conflict resolution, continue/skip/abort,
// reset, revert and restore.
func (m *Model) runRecovery(operation, notice string, fn func(context.Context, git.Repository) error) tea.Cmd {
	if m.busy {
		return nil
	}
	m.busy = true
	m.operation = operation
	m.err = ""
	repo := m.repo
	return tea.Batch(func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 60*time.Second)
		defer cancel()
		err := fn(ctx, repo)
		refreshCtx, refreshCancel := context.WithTimeout(m.ctx, 30*time.Second)
		defer refreshCancel()
		status, scanErr := repo.RepositoryStatus(refreshCtx)
		state, stateErr := repo.State(refreshCtx)
		if scanErr == nil {
			scanErr = stateErr
		}
		return recoveryMsg{operation: operation, notice: notice, err: err,
			refreshErr: scanErr, status: status, state: state}
	}, pulse())
}

func (m *Model) handleRecoveryResult(msg tea.Msg) (bool, tea.Cmd) {
	done, ok := msg.(recoveryMsg)
	if !ok {
		return false, nil
	}
	m.busy = false
	m.operation = ""
	if done.refreshErr != nil {
		m.setError(fmt.Errorf("%s; repository refresh failed: %w", done.operation, done.refreshErr))
		return true, nil
	}
	m.status = done.status
	m.repoState = done.state
	m.repoState.Conflicts = len(done.status.Groups[git.Conflicted])
	if done.err != nil {
		if m.conflicts != nil {
			m.conflicts.err = done.err.Error()
		}
		m.setError(done.err)
		return true, m.reloadActiveScreen()
	}
	m.err = ""
	if m.conflicts != nil {
		m.conflicts.err = ""
	}
	m.notice = done.notice
	return true, m.reloadActiveScreen()
}

// reloadActiveScreen re-reads whatever the current screen shows after a
// repository-changing action.
func (m *Model) reloadActiveScreen() tea.Cmd {
	switch m.screen {
	case screenConflicts:
		return m.reloadConflicts()
	case screenReflog:
		return m.loadReflog()
	case screenBranches:
		return m.loadBranches()
	case screenStash:
		return m.loadStashes()
	case screenRemotes:
		return m.loadRemotes()
	default:
		return m.loadDiff()
	}
}

// continueOperation finishes the paused operation. Git performs its own checks,
// so unresolved conflicts are reported by Git rather than bypassed.
func (m *Model) continueOperation() tea.Cmd {
	if !m.repoState.InProgress() {
		m.notice = "No merge, rebase, cherry-pick or revert is in progress"
		return nil
	}
	label := m.repoState.Operation.Label()
	return m.runRecovery("Continuing "+label, "Continued "+label,
		func(ctx context.Context, repo git.Repository) error {
			_, err := repo.Continue(ctx)
			return err
		})
}

// skipOperation drops the current step of a rebase, cherry-pick or revert.
func (m *Model) skipOperation() tea.Cmd {
	if !m.repoState.CanSkip() {
		m.notice = "This operation has no step to skip"
		return nil
	}
	label := m.repoState.Operation.Label()
	return m.runRecovery("Skipping "+label, "Skipped the current "+label+" step",
		func(ctx context.Context, repo git.Repository) error {
			_, err := repo.Skip(ctx)
			return err
		})
}

// confirmAbortOperation asks before discarding the operation, naming exactly
// what will be restored.
func (m *Model) confirmAbortOperation() tea.Cmd {
	if !m.repoState.CanAbort() {
		m.notice = "No operation is in progress"
		return nil
	}
	label := m.repoState.Operation.Label()
	body := fmt.Sprintf("Abort the %s and return the repository to its state\nbefore it began?\n\n", label)
	switch m.repoState.Operation {
	case git.OpRebase:
		body += "The branch will be reset to where it was before the rebase.\nCommits already replayed are not kept."
	case git.OpMerge:
		body += "The merge will be abandoned and the working tree restored."
	case git.OpCherryPick:
		body += "The cherry-pick sequence will be abandoned."
	case git.OpRevert:
		body += "The revert sequence will be abandoned."
	default:
		body += "The operation will be abandoned."
	}
	m.confirm = &confirmState{
		title:  "abort " + label + "?",
		body:   body,
		accept: "A",
		danger: true,
		run: func() tea.Cmd {
			return m.runRecovery("Aborting "+label, "Aborted "+label,
				func(ctx context.Context, repo git.Repository) error {
					_, err := repo.Abort(ctx)
					return err
				})
		},
	}
	return nil
}
