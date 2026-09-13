package git

import (
	"context"
	"errors"
	"strconv"
	"strings"
)

// Diff retains Git's patch unchanged alongside addressable hunks. Presentation
// and future line selections remain independent of Git patch application.
type Diff struct {
	Patch           string
	Truncated       bool
	Conflict        bool
	File            File
	Source          Section
	Context         int
	Whitespace      WhitespaceMode
	Hunks           []Hunk
	HunkUnavailable string
	// Commit is the abbreviated hash when this patch came from history rather
	// than the working tree. The viewer labels the pane with it.
	Commit string
}

// ContextLines clamps a requested context size, defaulting to Git's own three.
func ContextLines(requested int) int {
	if requested < 0 {
		return 0
	}
	if requested > 50 {
		return 50
	}
	return requested
}

// WhitespaceMode selects a Git-native whitespace comparison.
type WhitespaceMode int

const (
	WhitespaceNormal WhitespaceMode = iota
	WhitespaceIgnoreTrailing
	WhitespaceIgnoreChange
	WhitespaceIgnoreAll
)

// Args are the Git flags for the mode.
func (m WhitespaceMode) Args() []string {
	switch m {
	case WhitespaceIgnoreTrailing:
		return []string{"--ignore-space-at-eol"}
	case WhitespaceIgnoreChange:
		return []string{"--ignore-space-change"}
	case WhitespaceIgnoreAll:
		return []string{"--ignore-all-space"}
	default:
		return nil
	}
}

// String is the stable config identifier.
func (m WhitespaceMode) String() string {
	switch m {
	case WhitespaceIgnoreTrailing:
		return "ignore-trailing"
	case WhitespaceIgnoreChange:
		return "ignore-change"
	case WhitespaceIgnoreAll:
		return "ignore-all"
	default:
		return "normal"
	}
}

// ParseWhitespaceMode reads the config value, defaulting to normal.
func ParseWhitespaceMode(s string) WhitespaceMode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "ignore-trailing", "ignore-space-at-eol", "trailing":
		return WhitespaceIgnoreTrailing
	case "ignore-change", "ignore-space-change", "change":
		return WhitespaceIgnoreChange
	case "ignore-all", "ignore-all-space", "all":
		return WhitespaceIgnoreAll
	default:
		return WhitespaceNormal
	}
}

// DiffOptions selects the context and whitespace comparison for a diff.
type DiffOptions struct {
	Context    int
	Whitespace WhitespaceMode
}

func contextArg(requested []int) int {
	if len(requested) == 0 {
		return 3
	}
	return ContextLines(requested[0])
}

func (r Repository) Diff(ctx context.Context, section Section, file File, contextLines ...int) (Diff, error) {
	return r.DiffWith(ctx, section, file, DiffOptions{Context: contextArg(contextLines)})
}

// DiffWith renders a working-tree diff with explicit context and whitespace
// options.
func (r Repository) DiffWith(ctx context.Context, section Section, file File, opts DiffOptions) (Diff, error) {
	n := ContextLines(opts.Context)
	args := []string{"diff", "--no-ext-diff", "--no-textconv", "--no-color", "--unified=" + strconv.Itoa(n), "--src-prefix=a/", "--dst-prefix=b/", "--no-relative", "--inter-hunk-context=0", "--output-indicator-new=+", "--output-indicator-old=-", "--output-indicator-context= "}
	args = append(args, opts.Whitespace.Args()...)
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
	d := Diff{Patch: res.Stdout, Truncated: res.Truncated, Conflict: section == Conflicted, File: file, Source: section, Context: n}
	d.Whitespace = opts.Whitespace
	if err == nil {
		d.Hunks, d.HunkUnavailable = parseHunks(d)
	}
	return d, err
}
