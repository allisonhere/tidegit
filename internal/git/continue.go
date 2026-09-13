package git

import (
	"context"
	"fmt"
)

// operationEnv pins Git's editor so a continue or revert that would normally
// open an editor instead accepts the prepared message. It also denies the
// interactive credential prompt, matching network operations.
var operationEnv = []string{"GIT_EDITOR=true", "GIT_TERMINAL_PROMPT=0"}

// Continue finishes the operation Git is paused in. Git performs every safety
// check itself: unresolved conflicts, an empty commit, or anything else Git
// refuses is returned unchanged.
func (r Repository) Continue(ctx context.Context) (Result, error) {
	return r.operationCommand(ctx, "continue")
}

// Skip drops the current step of a rebase, cherry-pick or revert sequence.
func (r Repository) Skip(ctx context.Context) (Result, error) {
	return r.operationCommand(ctx, "skip")
}

// Abort abandons the operation and restores the pre-operation state.
func (r Repository) Abort(ctx context.Context) (Result, error) {
	return r.operationCommand(ctx, "abort")
}

func (r Repository) operationCommand(ctx context.Context, action string) (Result, error) {
	var result Result
	err := r.mutate(ctx, func() error {
		state, err := r.State(ctx)
		if err != nil {
			return err
		}
		if state.Operation == OpNone {
			return fmt.Errorf("no merge, rebase, cherry-pick or revert is in progress")
		}
		args, err := operationArgs(state.Operation, action)
		if err != nil {
			return err
		}
		var runErr error
		result, runErr = runEnv(ctx, r.Root, operationEnv, args...)
		if runErr != nil {
			return fmt.Errorf("%s cannot %s: %w", state.Operation.Label(), action, runErr)
		}
		return nil
	})
	return result, err
}

func operationArgs(kind OperationKind, action string) ([]string, error) {
	switch kind {
	case OpMerge:
		switch action {
		case "continue":
			return []string{"merge", "--continue"}, nil
		case "abort":
			return []string{"merge", "--abort"}, nil
		}
		return nil, fmt.Errorf("a merge has nothing to %s", action)
	case OpRebase:
		return []string{"rebase", "--" + action}, nil
	case OpCherryPick:
		return []string{"cherry-pick", "--" + action}, nil
	case OpRevert:
		return []string{"revert", "--" + action}, nil
	case OpBisect:
		if action == "abort" {
			return []string{"bisect", "reset"}, nil
		}
		return nil, fmt.Errorf("bisect cannot %s", action)
	default:
		return nil, fmt.Errorf("no operation is in progress")
	}
}

// CanSkip reports whether the current operation supports skipping.
func (s RepoState) CanSkip() bool {
	switch s.Operation {
	case OpRebase, OpCherryPick, OpRevert:
		return true
	default:
		return false
	}
}

// CanAbort reports whether the current operation can be aborted.
func (s RepoState) CanAbort() bool {
	return s.Operation != OpNone
}
