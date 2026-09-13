package git

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Stash is one entry in Git's stash list. Ref is the stable reflog selector
// ("stash@{2}"); callers must reload the list after any mutation because the
// numbered selectors shift. OID is the identity that survives a renumber.
type Stash struct {
	Ref     string
	OID     string
	Short   string
	Subject string
	Message string
	Branch  string
	Author  string
	Time    time.Time
}

var stashRefPattern = regexp.MustCompile(`^stash@\{[0-9]+\}$`)

func validStashRef(ref string) error {
	if !stashRefPattern.MatchString(ref) {
		return fmt.Errorf("expected a stash reference, got %q", ref)
	}
	return nil
}

// stashFormat is the same separator scheme history uses, so a stash message
// containing any printable text cannot make the stream ambiguous.
const stashFormat = "%gd" + fieldSep + "%H" + fieldSep + "%h" + fieldSep +
	"%s" + fieldSep + "%ct" + fieldSep + "%an" + recordSep

// Stashes lists every stash, newest first, parsing Git's reflog stream into a
// structured model. Source branch and message come from the stash commit's own
// subject, which Git generated, never from a display string.
func (r Repository) Stashes(ctx context.Context) ([]Stash, error) {
	res, err := run(ctx, r.Root, "stash", "list", "--format="+stashFormat)
	if err != nil {
		return nil, err
	}
	if res.Truncated {
		return nil, fmt.Errorf("stash list exceeds 4 MiB")
	}
	var stashes []Stash
	for _, record := range strings.Split(res.Stdout, recordSep) {
		record = strings.TrimLeft(record, "\n")
		if strings.TrimSpace(record) == "" {
			continue
		}
		f := strings.Split(record, fieldSep)
		if len(f) != 6 {
			return nil, fmt.Errorf("malformed stash record with %d fields", len(f))
		}
		stash := Stash{Ref: f[0], OID: f[1], Short: f[2], Subject: f[3], Author: f[5]}
		stash.Branch, stash.Message = stashSubjectParts(stash.Subject)
		if f[4] != "" {
			n, err := strconv.ParseInt(f[4], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("malformed stash timestamp %q", f[4])
			}
			stash.Time = time.Unix(n, 0)
		}
		stashes = append(stashes, stash)
	}
	return stashes, nil
}

// stashSubjectParts separates Git's generated subject into the branch it was
// taken on and the message a person recognises. The default subject is
// "WIP on <branch>: <hash> <subject>"; a named stash is "On <branch>: <message>".
func stashSubjectParts(subject string) (branch, message string) {
	switch {
	case strings.HasPrefix(subject, "WIP on "):
		rest := strings.TrimPrefix(subject, "WIP on ")
		branch, message, _ = strings.Cut(rest, ": ")
		return branch, message
	case strings.HasPrefix(subject, "On "):
		rest := strings.TrimPrefix(subject, "On ")
		branch, message, _ = strings.Cut(rest, ": ")
		return branch, message
	}
	return "", subject
}

// StashFiles lists the paths a stash changes against its first parent. Untracked
// files captured by `stash push -u` are included, which is why -u is always
// passed: it is a no-op for a stash that has none.
func (r Repository) StashFiles(ctx context.Context, ref string) ([]FileChange, error) {
	if err := validStashRef(ref); err != nil {
		return nil, err
	}
	numstat, err := r.stashShow(ctx, ref, "--numstat")
	if err != nil {
		return nil, err
	}
	nameStatus, err := r.stashShow(ctx, ref, "--name-status")
	if err != nil {
		return nil, err
	}
	files, err := parseNumstat(numstat)
	if err != nil {
		return nil, err
	}
	kinds, err := parseNameStatus(nameStatus)
	if err != nil {
		return nil, err
	}
	for i := range files {
		if kind, ok := kinds[files[i].Path]; ok {
			files[i].Status = kind
		}
	}
	return files, nil
}

// stashShow runs one -z variant of `git stash show` for parsing.
func (r Repository) stashShow(ctx context.Context, ref, mode string) (string, error) {
	res, err := run(ctx, r.Root, "stash", "show", "-u", "-z", mode, ref)
	if err != nil {
		return "", err
	}
	if res.Truncated {
		return "", fmt.Errorf("stash %s exceeds the 4 MiB inspection limit", ref)
	}
	return res.Stdout, nil
}

// StashDiff returns one file's patch from a stash, shaped like a working-tree
// Diff so the shared renderer can draw it unchanged. Hunk staging is never
// offered: a stash is history, not the index.
func (r Repository) StashDiff(ctx context.Context, ref string, f FileChange, contextLines ...int) (Diff, error) {
	return r.StashDiffWith(ctx, ref, f, DiffOptions{Context: contextArg(contextLines)})
}

// StashDiffWith renders one file's patch from a stash with explicit context and
// whitespace options.
func (r Repository) StashDiffWith(ctx context.Context, ref string, f FileChange, opts DiffOptions) (Diff, error) {
	if err := validStashRef(ref); err != nil {
		return Diff{}, err
	}
	n := ContextLines(opts.Context)
	args := []string{"stash", "show", "-u", "--no-color", "--no-ext-diff",
		"--no-textconv", "--unified=" + strconv.Itoa(n), "--src-prefix=a/", "--dst-prefix=b/",
		"--no-relative", "--inter-hunk-context=0", "--output-indicator-new=+",
		"--output-indicator-old=-", "--output-indicator-context= "}
	args = append(args, opts.Whitespace.Args()...)
	args = append(args, ref)
	res, err := run(ctx, r.Root, args...)
	if err != nil {
		return Diff{}, err
	}
	if res.Truncated {
		return Diff{}, fmt.Errorf("stash %s exceeds the 4 MiB inspection limit", ref)
	}
	section := patchSection(res.Stdout, f.Path)
	return Diff{
		Patch:           section,
		File:            File{Path: f.Path, OriginalPath: f.OriginalPath},
		Source:          Unstaged,
		Context:         n,
		Whitespace:      opts.Whitespace,
		Commit:          ref,
		HunkUnavailable: "Stash entry: apply it from the stash list instead.",
	}, nil
}

// patchSection extracts the per-file hunk of a multi-file patch, matching on
// the new path (or the old path for a deletion). A missing section returns the
// whole patch rather than an empty renderer.
func patchSection(patch, path string) string {
	sections := splitPatch(patch)
	for _, section := range sections {
		if sectionPath(section) == path {
			return section
		}
	}
	if len(sections) == 1 {
		return sections[0]
	}
	return patch
}

func splitPatch(patch string) []string {
	var sections []string
	var cur strings.Builder
	for _, line := range strings.SplitAfter(patch, "\n") {
		if strings.HasPrefix(line, "diff --git ") && cur.Len() > 0 {
			sections = append(sections, cur.String())
			cur.Reset()
		}
		cur.WriteString(line)
	}
	if cur.Len() > 0 {
		sections = append(sections, cur.String())
	}
	return sections
}

// sectionPath reads the new path from a patch section, falling back to the old
// path for a deletion or a binary notice.
func sectionPath(section string) string {
	newPath, oldPath := "", ""
	for _, line := range strings.Split(section, "\n") {
		switch {
		case strings.HasPrefix(line, "+++ "):
			newPath = stripPatchPrefix(strings.TrimPrefix(line, "+++ "))
		case strings.HasPrefix(line, "--- "):
			oldPath = stripPatchPrefix(strings.TrimPrefix(line, "--- "))
		}
	}
	if newPath != "" && newPath != "/dev/null" {
		return newPath
	}
	if oldPath != "" && oldPath != "/dev/null" {
		return oldPath
	}
	return ""
}

func stripPatchPrefix(path string) string {
	path = strings.TrimSuffix(path, "\t")
	if rest, ok := strings.CutPrefix(path, "a/"); ok {
		return rest
	}
	if rest, ok := strings.CutPrefix(path, "b/"); ok {
		return rest
	}
	return path
}

// StashCreate saves the working tree with Git's own stash. includeUntracked
// adds -u; an empty message uses Git's default WIP wording.
func (r Repository) StashCreate(ctx context.Context, message string, includeUntracked bool) (Result, error) {
	var result Result
	err := r.mutate(ctx, func() error {
		args := []string{"stash", "push"}
		if includeUntracked {
			args = append(args, "-u")
		}
		if strings.TrimSpace(message) != "" {
			args = append(args, "-m", message)
		}
		var runErr error
		result, runErr = runNoLiteral(ctx, r.Root, args...)
		if runErr != nil {
			if strings.Contains(result.Stdout+result.Stderr, "No local changes to save") {
				return fmt.Errorf("there are no local changes to stash")
			}
			return fmt.Errorf("stash was not created: %w", runErr)
		}
		return nil
	})
	return result, err
}

// StashApply restores a stash's changes without removing the entry.
func (r Repository) StashApply(ctx context.Context, ref string) (Result, error) {
	return r.stashMutate(ctx, "apply", ref)
}

// StashPop applies a stash and removes it only when application succeeds. A
// conflicted pop leaves the stash in place exactly as Git does; TideGit never
// drops it on Git's behalf.
func (r Repository) StashPop(ctx context.Context, ref string) (Result, error) {
	return r.stashMutate(ctx, "pop", ref)
}

// StashDrop discards one stash entry. It is destructive and the caller is
// responsible for confirming it first.
func (r Repository) StashDrop(ctx context.Context, ref string) (Result, error) {
	return r.stashMutate(ctx, "drop", ref)
}

func (r Repository) stashMutate(ctx context.Context, action, ref string) (Result, error) {
	var result Result
	err := r.mutate(ctx, func() error {
		if err := validStashRef(ref); err != nil {
			return err
		}
		var runErr error
		result, runErr = run(ctx, r.Root, "stash", action, ref)
		if runErr != nil {
			if strings.Contains(result.Stdout+result.Stderr, "CONFLICT") {
				return &StashConflictError{Action: action, Ref: ref, Err: runErr}
			}
			return fmt.Errorf("could not %s %s: %w", action, ref, runErr)
		}
		return nil
	})
	return result, err
}

// StashConflictError reports that an apply or pop left conflicts in the working
// tree. Git preserves the stash in this case; the caller must not drop it.
type StashConflictError struct {
	Action string
	Ref    string
	Err    error
}

func (e *StashConflictError) Error() string {
	return fmt.Sprintf("%s of %s produced conflicts · resolve them on the Status screen", e.Action, e.Ref)
}
func (e *StashConflictError) Unwrap() error { return e.Err }

// DescribeStashCreate words a successful stash creation without leaking Git's
// exact output.
func DescribeStashCreate(res Result) string {
	text := res.Stdout + res.Stderr
	if strings.Contains(text, "No local changes") {
		return "No local changes to stash"
	}
	return "Stashed working tree"
}
