package git

import (
	"context"
	"errors"
)

// Diff retains Git's patch unchanged. A future patch model can derive hunks and
// line selections from this data without coupling patch application to rendering.
type Diff struct {
	Patch     string
	Truncated bool
	Conflict  bool
}

func (r Repository) Diff(ctx context.Context, section Section, file File) (Diff, error) {
	args := []string{"diff", "--no-ext-diff", "--no-textconv", "--no-color", "--unified=3"}
	switch section {
	case Staged:
		args = append(args, "--cached")
	case Conflicted:
		args = append(args, "--cc")
	case Untracked:
		args = append(args, "--no-index", "--", "/dev/null", file.Path)
	}
	if section != Untracked {
		args = append(args, "--", file.Path)
		if file.OriginalPath != "" {
			args = append(args, file.OriginalPath)
		}
	}
	res, err := run(ctx, r.Root, args...)
	var ce *CommandError
	if section == Untracked && errors.As(err, &ce) && res.ExitCode == 1 {
		err = nil
	}
	return Diff{Patch: res.Stdout, Truncated: res.Truncated, Conflict: section == Conflicted}, err
}
