package git

import (
	"context"
	"fmt"
	"strings"
)

// ResetMode selects how much of the working state a reset discards. The three
// modes differ substantially in what they destroy, so the UI treats them
// differently and this type carries the wording for each.
type ResetMode int

const (
	ResetSoft ResetMode = iota
	ResetMixed
	ResetHard
)

// Flag is the Git argument for the mode.
func (m ResetMode) Flag() string {
	switch m {
	case ResetSoft:
		return "--soft"
	case ResetHard:
		return "--hard"
	default:
		return "--mixed"
	}
}

// Label is the short name shown in menus and confirmations.
func (m ResetMode) Label() string {
	switch m {
	case ResetSoft:
		return "soft reset"
	case ResetHard:
		return "hard reset"
	default:
		return "mixed reset"
	}
}

// Description states exactly what changes, in the order it happens.
func (m ResetMode) Description() string {
	switch m {
	case ResetSoft:
		return "Moves HEAD to the target. The index and working tree are left exactly as they are, so the changes stay staged."
	case ResetHard:
		return "Moves HEAD to the target and resets the index. Tracked working-tree changes are discarded."
	default:
		return "Moves HEAD to the target and resets the index. Working-tree changes are kept but become unstaged."
	}
}

// Destructive reports whether the mode can lose uncommitted work.
func (m ResetMode) Destructive() bool { return m == ResetHard }

// Reset moves HEAD to target. It is Git's own reset, so reflog, safety checks
// and index behaviour are unchanged.
func (r Repository) Reset(ctx context.Context, mode ResetMode, target string) (Result, error) {
	var result Result
	err := r.mutate(ctx, func() error {
		if err := validRevision(target); err != nil {
			return err
		}
		var runErr error
		result, runErr = runEnv(ctx, r.Root, operationEnv, "reset", mode.Flag(), target)
		if runErr != nil {
			return fmt.Errorf("%s to %s failed: %w", mode.Label(), shortRev(target), runErr)
		}
		return nil
	})
	return result, err
}

// Revert creates the inverse of a normal commit. A merge commit needs a parent
// selection that this milestone does not offer, so it is refused rather than
// guessed at.
func (r Repository) Revert(ctx context.Context, rev string) (Result, error) {
	var result Result
	err := r.mutate(ctx, func() error {
		if err := validRevision(rev); err != nil {
			return err
		}
		res, err := run(ctx, r.Root, "rev-parse", "--verify", "--quiet", rev+"^2")
		if err == nil && strings.TrimSpace(res.Stdout) != "" {
			return fmt.Errorf("%s is a merge commit; reverting it needs a parent choice, which TideGit does not offer yet", shortRev(rev))
		}
		var runErr error
		result, runErr = runEnv(ctx, r.Root, operationEnv, "revert", "--no-edit", "--", rev)
		if runErr != nil {
			return fmt.Errorf("revert of %s did not complete: %w", shortRev(rev), runErr)
		}
		return nil
	})
	return result, err
}

// CherryPick applies one commit onto HEAD. A conflict leaves the repository in
// the cherry-pick state, which the caller continues or aborts.
func (r Repository) CherryPick(ctx context.Context, rev string) (Result, error) {
	var result Result
	err := r.mutate(ctx, func() error {
		if err := validRevision(rev); err != nil {
			return err
		}
		var runErr error
		result, runErr = runEnv(ctx, r.Root, operationEnv, "cherry-pick", "--no-edit", "--", rev)
		if runErr != nil {
			return fmt.Errorf("cherry-pick of %s did not complete: %w", shortRev(rev), runErr)
		}
		return nil
	})
	return result, err
}

// SwitchDetached checks out a commit directly, leaving HEAD detached. It is a
// recovery action, so it never discards local changes: Git refuses rather than
// overwriting them.
func (r Repository) SwitchDetached(ctx context.Context, rev string) error {
	return r.mutate(ctx, func() error {
		if err := validRevision(rev); err != nil {
			return err
		}
		if _, err := run(ctx, r.Root, "switch", "--detach", rev); err != nil {
			return fmt.Errorf("could not switch to %s (working tree unchanged): %w", shortRev(rev), err)
		}
		return nil
	})
}

func shortRev(rev string) string {
	if len(rev) > 10 {
		return rev[:10]
	}
	return rev
}
