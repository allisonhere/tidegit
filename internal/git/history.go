package git

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Records use ASCII unit/record separators rather than newlines or spaces so a
// subject, author name or ref decoration can contain any printable text without
// making the stream ambiguous. Git never emits these bytes from %-placeholders.
const (
	fieldSep  = "\x1f"
	recordSep = "\x1e"
)

// historyFormat and its field order must stay in step with parseCommit.
const historyFormat = "%H" + fieldSep + "%h" + fieldSep + "%P" + fieldSep +
	"%an" + fieldSep + "%ae" + fieldSep + "%at" + fieldSep +
	"%cn" + fieldSep + "%ce" + fieldSep + "%ct" + fieldSep +
	"%s" + fieldSep + "%D" + recordSep

// Commit is one history entry. Body is loaded separately by CommitDetail so a
// history page stays small; Decorations carry the refs Git reported inline.
type Commit struct {
	OID, Short             string
	Parents                []string
	Author, AuthorEmail    string
	AuthorTime             time.Time
	Committer, CommitEmail string
	CommitTime             time.Time
	Subject, Body          string
	Decorations            []Ref
}

// Merge reports whether this commit has more than one parent.
func (c Commit) Merge() bool { return len(c.Parents) > 1 }

// HistoryOptions selects and pages through history. The zero value walks HEAD.
type HistoryOptions struct {
	Rev    string // a ref name, commit-ish, or "" for HEAD
	All    bool   // walk every ref instead of Rev
	Skip   int    // commits to skip; pages are Skip = page * Limit
	Limit  int    // commits to return; HistoryBatch when zero
	Search string // literal, case-insensitive subject match
}

// HistoryBatch is the default page size. History is never loaded whole: the UI
// requests the next page as the selection approaches the end of the list.
const HistoryBatch = 120

// History returns one page of commits in topological order. Topological order
// keeps graph lanes stable between pages, which date order does not.
func (r Repository) History(ctx context.Context, opts HistoryOptions) ([]Commit, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = HistoryBatch
	}
	args := []string{"log", "--topo-order", "--no-color", "--decorate=short",
		"--pretty=format:" + historyFormat,
		"--max-count=" + strconv.Itoa(limit)}
	if opts.Skip > 0 {
		args = append(args, "--skip="+strconv.Itoa(opts.Skip))
	}
	if opts.Search != "" {
		args = append(args, "--fixed-strings", "--regexp-ignore-case", "--grep="+opts.Search)
	}
	switch {
	case opts.All:
		args = append(args, "--all")
	case opts.Rev != "":
		args = append(args, opts.Rev)
	}
	// Separate revisions from paths so a ref named like a file cannot be
	// reinterpreted, and so an unborn HEAD fails as a revision error.
	args = append(args, "--")
	res, err := run(ctx, r.Root, args...)
	if err != nil {
		if isUnbornRevision(err) {
			return nil, nil
		}
		return nil, err
	}
	if res.Truncated {
		return nil, fmt.Errorf("history page exceeds 4 MiB; narrow the filter and try again")
	}
	return parseHistory(res.Stdout)
}

// isUnbornRevision reports whether Git refused because no commit exists yet.
// A fresh repository has a valid HEAD that resolves to nothing, which is an
// empty history rather than a failure worth showing.
func isUnbornRevision(err error) bool {
	var ce *CommandError
	if !errors.As(err, &ce) {
		return false
	}
	s := ce.Result.Stderr
	return strings.Contains(s, "does not have any commits yet") ||
		strings.Contains(s, "unknown revision or path not in the working tree")
}

func parseHistory(raw string) ([]Commit, error) {
	var commits []Commit
	for _, record := range strings.Split(raw, recordSep) {
		record = strings.TrimLeft(record, "\n")
		if strings.TrimSpace(record) == "" {
			continue
		}
		c, err := parseCommit(record)
		if err != nil {
			return nil, err
		}
		commits = append(commits, c)
	}
	return commits, nil
}

func parseCommit(record string) (Commit, error) {
	f := strings.Split(record, fieldSep)
	if len(f) != 11 {
		return Commit{}, fmt.Errorf("malformed commit record with %d fields", len(f))
	}
	unix := func(v string) (time.Time, error) {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return time.Time{}, fmt.Errorf("malformed commit timestamp %q", v)
		}
		return time.Unix(n, 0), nil
	}
	c := Commit{OID: f[0], Short: f[1], Author: f[3], AuthorEmail: f[4],
		Committer: f[6], CommitEmail: f[7], Subject: f[9]}
	if parents := strings.Fields(f[2]); len(parents) > 0 {
		c.Parents = parents
	}
	var err error
	if c.AuthorTime, err = unix(f[5]); err != nil {
		return Commit{}, err
	}
	if c.CommitTime, err = unix(f[8]); err != nil {
		return Commit{}, err
	}
	c.Decorations = parseDecorations(f[10])
	return c, nil
}

// CommitDetail loads one commit including its full message body.
func (r Repository) CommitDetail(ctx context.Context, rev string) (Commit, error) {
	if err := validRevision(rev); err != nil {
		return Commit{}, err
	}
	res, err := run(ctx, r.Root, "show", "--no-patch", "--no-color", "--decorate=short",
		"--pretty=format:"+historyFormat+"%B", rev)
	if err != nil {
		return Commit{}, fmt.Errorf("could not read commit %s: %w", rev, err)
	}
	if res.Truncated {
		return Commit{}, fmt.Errorf("commit %s exceeds the 4 MiB inspection limit", rev)
	}
	record, body, found := strings.Cut(res.Stdout, recordSep)
	if !found {
		return Commit{}, fmt.Errorf("malformed commit record for %s", rev)
	}
	c, err := parseCommit(record)
	if err != nil {
		return Commit{}, err
	}
	c.Body = body
	return c, nil
}

// FileChange is one path touched by a commit. Additions and Deletions are -1
// for binary files, which Git reports as "-" rather than a count.
type FileChange struct {
	Path, OriginalPath string
	Status             byte // A, M, D, R, C, T
	Additions          int
	Deletions          int
	Binary             bool
}

// CommitFiles lists the paths a commit changed against its first parent. A
// merge is therefore summarised the way `git show` summarises it, and a root
// commit is compared against the empty tree Git uses for the same purpose.
func (r Repository) CommitFiles(ctx context.Context, c Commit) ([]FileChange, error) {
	numstat, err := r.commitDiffArgs(ctx, c, []string{"--numstat"}, nil)
	if err != nil {
		return nil, err
	}
	status, err := r.commitDiffArgs(ctx, c, []string{"--name-status"}, nil)
	if err != nil {
		return nil, err
	}
	files, err := parseNumstat(numstat)
	if err != nil {
		return nil, err
	}
	kinds, err := parseNameStatus(status)
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

// commitDiffArgs runs one diff of a commit against its first parent.
func (r Repository) commitDiffArgs(ctx context.Context, c Commit, mode []string, paths []string) (string, error) {
	if err := validRevision(c.OID); err != nil {
		return "", err
	}
	args := []string{"diff", "--no-ext-diff", "--no-textconv", "--no-color", "-z", "--find-renames", "--find-copies"}
	args = append(args, mode...)
	if len(c.Parents) == 0 {
		// A root commit has nothing to diff against, so ask Git for the commit
		// itself; `show` compares it with the empty tree.
		args = append([]string{"show", "--no-ext-diff", "--no-textconv", "--no-color", "-z",
			"--find-renames", "--find-copies", "--format="}, mode...)
		args = append(args, c.OID)
	} else {
		args = append(args, c.Parents[0], c.OID)
	}
	if len(paths) > 0 {
		args = append(args, "--")
		args = append(args, paths...)
	}
	res, err := run(ctx, r.Root, args...)
	if err != nil {
		return "", fmt.Errorf("could not read changes for %s: %w", c.Short, err)
	}
	if res.Truncated {
		return "", fmt.Errorf("commit %s exceeds the 4 MiB inspection limit", c.Short)
	}
	return res.Stdout, nil
}

// parseNumstat reads `--numstat -z`. Renames and copies emit three NUL fields:
// counts, original path, new path.
func parseNumstat(raw string) ([]FileChange, error) {
	var out []FileChange
	records := strings.Split(raw, "\x00")
	for i := 0; i < len(records); i++ {
		if records[i] == "" {
			continue
		}
		adds, dels, rest, ok := cutNumstat(records[i])
		if !ok {
			return nil, fmt.Errorf("malformed numstat record")
		}
		f := FileChange{Status: 'M'}
		if adds == "-" || dels == "-" {
			f.Binary = true
			f.Additions, f.Deletions = -1, -1
		} else {
			a, err := strconv.Atoi(adds)
			if err != nil {
				return nil, fmt.Errorf("malformed addition count %q", adds)
			}
			d, err := strconv.Atoi(dels)
			if err != nil {
				return nil, fmt.Errorf("malformed deletion count %q", dels)
			}
			f.Additions, f.Deletions = a, d
		}
		if rest != "" {
			f.Path = rest
		} else {
			// Rename or copy: the next two records are the old and new paths.
			if i+2 >= len(records) {
				return nil, fmt.Errorf("truncated rename record in numstat")
			}
			f.OriginalPath, f.Path = records[i+1], records[i+2]
			f.Status = 'R'
			i += 2
		}
		out = append(out, f)
	}
	return out, nil
}

// cutNumstat splits "adds\tdels\tpath" where path may be empty for renames.
func cutNumstat(record string) (adds, dels, rest string, ok bool) {
	adds, tail, ok := strings.Cut(record, "\t")
	if !ok {
		return "", "", "", false
	}
	dels, rest, ok = strings.Cut(tail, "\t")
	if !ok {
		return "", "", "", false
	}
	return adds, dels, rest, true
}

// parseNameStatus reads `--name-status -z`, keyed by the resulting path.
func parseNameStatus(raw string) (map[string]byte, error) {
	kinds := map[string]byte{}
	records := strings.Split(raw, "\x00")
	for i := 0; i < len(records); i++ {
		code := records[i]
		if code == "" {
			continue
		}
		if i+1 >= len(records) {
			return nil, fmt.Errorf("truncated name-status record")
		}
		path := records[i+1]
		i++
		// R and C carry a similarity score and a second path.
		if code[0] == 'R' || code[0] == 'C' {
			if i+1 >= len(records) {
				return nil, fmt.Errorf("truncated rename in name-status")
			}
			path = records[i+1]
			i++
		}
		kinds[path] = code[0]
	}
	return kinds, nil
}

// CommitDiff returns the patch for one path in one commit, shaped exactly like
// a working-tree Diff so the existing renderer can display it unchanged. Hunk
// staging is never offered: a historical patch is not a working-tree target.
func (r Repository) CommitDiff(ctx context.Context, c Commit, f FileChange, contextLines ...int) (Diff, error) {
	return r.CommitDiffWith(ctx, c, f, DiffOptions{Context: contextArg(contextLines)})
}

// CommitDiffWith renders one path's patch from a commit with explicit context
// and whitespace options.
func (r Repository) CommitDiffWith(ctx context.Context, c Commit, f FileChange, opts DiffOptions) (Diff, error) {
	n := ContextLines(opts.Context)
	paths := []string{f.Path}
	if f.OriginalPath != "" {
		paths = append(paths, f.OriginalPath)
	}
	flags := []string{"--unified=" + strconv.Itoa(n), "--src-prefix=a/", "--dst-prefix=b/",
		"--no-relative", "--inter-hunk-context=0", "--output-indicator-new=+",
		"--output-indicator-old=-", "--output-indicator-context= "}
	flags = append(flags, opts.Whitespace.Args()...)
	patch, err := r.commitDiffArgs(ctx, c, flags, paths)
	if err != nil {
		return Diff{}, err
	}
	return Diff{
		Patch:           patch,
		File:            File{Path: f.Path, OriginalPath: f.OriginalPath},
		Source:          Staged,
		Context:         n,
		Whitespace:      opts.Whitespace,
		Commit:          c.Short,
		HunkUnavailable: "Historical commit: staging actions apply to the working tree.",
	}, nil
}

// validRevision rejects input that Git would read as an option rather than a
// revision. Revisions reaching here come from Git's own output or a resolved
// user query, never from raw keystrokes.
func validRevision(rev string) error {
	if rev == "" || strings.HasPrefix(rev, "-") {
		return fmt.Errorf("expected a revision, got %q", rev)
	}
	return nil
}
