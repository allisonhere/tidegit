package git

import (
	"context"
	"errors"
)

// Diff retains Git's patch unchanged alongside addressable hunks. Presentation
// and future line selections remain independent of Git patch application.
type Diff struct {
	Patch           string
	Truncated       bool
	Conflict        bool
	File            File
	Source          Section
	Hunks           []Hunk
	HunkUnavailable string
}

func (r Repository) Diff(ctx context.Context, section Section, file File) (Diff, error) {
	args := []string{"diff", "--no-ext-diff", "--no-textconv", "--no-color", "--unified=3", "--src-prefix=a/", "--dst-prefix=b/", "--no-relative", "--inter-hunk-context=0", "--output-indicator-new=+", "--output-indicator-old=-", "--output-indicator-context= "}
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
		if file.OriginalPath != "" && section == Staged {
			args = append(args, file.OriginalPath)
		}
	}
	res, err := run(ctx, r.Root, args...)
	var ce *CommandError
	if section == Untracked && errors.As(err, &ce) && res.ExitCode == 1 {
		err = nil
	}
	d := Diff{Patch: res.Stdout, Truncated: res.Truncated, Conflict: section == Conflicted, File: file, Source: section}
	if err == nil {
		d.Hunks, d.HunkUnavailable = parseHunks(d)
	}
	return d, err
}
