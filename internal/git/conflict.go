package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ConflictStages holds the blob ids Git recorded for an unmerged path. A stage
// is empty when that side does not exist, which is itself meaningful: a
// delete/modify conflict has only two stages.
type ConflictStages struct {
	Base, Ours, Theirs string
	Modes              [3]string // index modes for base, ours, theirs
}

// HasBase reports whether a common ancestor was recorded.
func (s ConflictStages) HasBase() bool { return s.Base != "" }

// Blob is one blob's contents as read from the object database.
type Blob struct {
	Data   []byte
	Text   string
	Binary bool
}

// Unmerged reads Git's unmerged index entries in one call. Stage content is
// fetched later by blob id, which avoids putting a path into revision syntax.
func (r Repository) Unmerged(ctx context.Context) (map[string]ConflictStages, error) {
	res, err := run(ctx, r.Root, "ls-files", "--unmerged", "-z")
	if err != nil {
		return nil, err
	}
	if res.Truncated {
		return nil, fmt.Errorf("unmerged index exceeds 4 MiB")
	}
	out := map[string]ConflictStages{}
	for _, record := range strings.Split(res.Stdout, "\x00") {
		if record == "" {
			continue
		}
		meta, path, ok := strings.Cut(record, "\t")
		if !ok {
			return nil, fmt.Errorf("malformed unmerged index record")
		}
		fields := strings.Fields(meta)
		if len(fields) != 3 {
			return nil, fmt.Errorf("malformed unmerged index entry for %q", path)
		}
		stage := 0
		switch fields[2] {
		case "1":
			stage = 0
		case "2":
			stage = 1
		case "3":
			stage = 2
		default:
			return nil, fmt.Errorf("unknown merge stage %q", fields[2])
		}
		entry := out[path]
		entry.Modes[stage] = fields[0]
		switch stage {
		case 0:
			entry.Base = fields[1]
		case 1:
			entry.Ours = fields[1]
		case 2:
			entry.Theirs = fields[1]
		}
		out[path] = entry
	}
	return out, nil
}

var blobID = regexp.MustCompile(`^[0-9a-fA-F]{4,64}$`)

// emptyBlob is Git's well-known empty blob, used when a conflict side does not
// exist and the comparison is against nothing.
const emptyBlob = "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391"

// BlobDiff renders a patch between two blob ids so conflict stages can be shown
// through the shared diff renderer. Either side may be empty, which compares
// against the empty blob.
func (r Repository) BlobDiff(ctx context.Context, left, right string, contextLines ...int) (string, error) {
	return r.BlobDiffWith(ctx, left, right, DiffOptions{Context: contextArg(contextLines)})
}

// BlobDiffWith renders a patch between two blobs with explicit context and
// whitespace options.
func (r Repository) BlobDiffWith(ctx context.Context, left, right string, opts DiffOptions) (string, error) {
	if left == "" {
		left = emptyBlob
	}
	if right == "" {
		right = emptyBlob
	}
	if !blobID.MatchString(left) || !blobID.MatchString(right) {
		return "", fmt.Errorf("invalid object id in comparison")
	}
	n := ContextLines(opts.Context)
	args := []string{"diff", "--no-color", "--no-ext-diff", "--no-textconv",
		"--unified=" + strconv.Itoa(n), "--src-prefix=a/", "--dst-prefix=b/", "--no-relative",
		"--inter-hunk-context=0"}
	args = append(args, opts.Whitespace.Args()...)
	args = append(args, left, right, "--")
	res, err := run(ctx, r.Root, args...)
	if err != nil {
		return "", err
	}
	if res.Truncated {
		return "", fmt.Errorf("comparison exceeds 4 MiB")
	}
	return res.Stdout, nil
}

// BlobContent reads a blob from the object database by id. Only hex object ids
// reach the revision argument, so nothing path-shaped is ever interpreted.
func (r Repository) BlobContent(ctx context.Context, oid string) (Blob, error) {
	if !blobID.MatchString(oid) {
		return Blob{}, fmt.Errorf("invalid object id %q", oid)
	}
	res, err := run(ctx, r.Root, "cat-file", "blob", oid)
	if err != nil {
		return Blob{}, err
	}
	if res.Truncated {
		return Blob{}, fmt.Errorf("blob %s exceeds 4 MiB", oid[:min(8, len(oid))])
	}
	blob := Blob{Data: []byte(res.Stdout), Text: res.Stdout}
	blob.Binary = strings.IndexByte(res.Stdout, 0) >= 0
	return blob, nil
}

// WorkingFile reads a working-tree file. ok is false when it does not exist,
// which is the normal state for one side of a delete conflict.
func (r Repository) WorkingFile(path string) (string, bool, error) {
	if err := validPath(path); err != nil {
		return "", false, err
	}
	data, err := os.ReadFile(filepath.Join(r.Root, path))
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return string(data), true, nil
}

// ConflictRegion is one conflict marker block in a working-tree file. Line
// numbers are 1-based and inclusive; a range is zero when that side is absent.
type ConflictRegion struct {
	StartLine, EndLine                int
	OursStart, OursEnd                int
	BaseStart, BaseEnd                int
	TheirsStart, TheirsEnd            int
	OursLabel, BaseLabel, TheirsLabel string
	HasBase, HasOurs, HasTheirs       bool
	OursLines, BaseLines, TheirsLines []string
}

// MarkerKind identifies a conflict marker line. A marker only counts when its
// run of characters is at least seven and, for labelled markers, is followed by
// the end of the line or a space.
func markerKind(line string) (byte, string) {
	if len(line) < 7 {
		return 0, ""
	}
	c := line[0]
	switch c {
	case '<', '|', '>', '=':
	default:
		return 0, ""
	}
	n := 0
	for n < len(line) && line[n] == c {
		n++
	}
	if n < 7 {
		return 0, ""
	}
	rest := line[n:]
	if c == '=' {
		// The separator carries no label; anything else is content.
		if strings.TrimSpace(rest) != "" {
			return 0, ""
		}
		return '=', ""
	}
	if rest != "" && !strings.HasPrefix(rest, " ") {
		return 0, ""
	}
	return c, strings.TrimSpace(rest)
}

// ParseConflictRegions extracts only complete conflict blocks: a start marker,
// a separator, and an end marker. Partial or malformed runs are ignored rather
// than guessed at. Every line number is 1-based; a side's range is empty when
// that side has no lines.
func ParseConflictRegions(content string) []ConflictRegion {
	lines := strings.Split(content, "\n")
	var regions []ConflictRegion
	var cur *ConflictRegion
	state := byte(0)
	for i, line := range lines {
		lineNo := i + 1
		kind, label := markerKind(line)
		switch state {
		case 0:
			if kind == '<' {
				cur = &ConflictRegion{StartLine: lineNo, OursLabel: label, HasOurs: true, OursStart: lineNo + 1}
				state = '<'
			}
		case '<':
			switch kind {
			case '|':
				cur.HasBase = true
				cur.BaseLabel = label
				cur.OursEnd = lineNo - 1
				cur.BaseStart = lineNo + 1
				state = '|'
			case '=':
				cur.OursEnd = lineNo - 1
				cur.TheirsStart = lineNo + 1
				state = '='
			default:
				cur.OursLines = append(cur.OursLines, line)
			}
		case '|':
			if kind == '=' {
				cur.BaseEnd = lineNo - 1
				cur.TheirsStart = lineNo + 1
				state = '='
			} else {
				cur.BaseLines = append(cur.BaseLines, line)
			}
		case '=':
			if kind == '>' {
				cur.TheirsLabel = label
				cur.HasTheirs = true
				cur.TheirsEnd = lineNo - 1
				cur.EndLine = lineNo
				regions = append(regions, *cur)
				cur = nil
				state = 0
			} else {
				cur.TheirsLines = append(cur.TheirsLines, line)
			}
		}
	}
	return regions
}

// resolveSide writes one side of an unmerged file to the working tree. It only
// changes the file; whether the result is staged and marked resolved is a
// separate, explicit decision.
func (r Repository) resolveSide(ctx context.Context, path string, ours bool, mark bool) error {
	return r.mutate(ctx, func() error {
		if err := validPath(path); err != nil {
			return err
		}
		unmerged, err := r.Unmerged(ctx)
		if err != nil {
			return err
		}
		stages, ok := unmerged[path]
		if !ok {
			return fmt.Errorf("%q is not an unmerged path", path)
		}
		oid, mode, side := stages.Theirs, stages.Modes[2], "theirs"
		if ours {
			oid, mode, side = stages.Ours, stages.Modes[1], "ours"
		}
		if oid == "" {
			// That side deleted the file, so accepting it means removing it.
			if err := os.Remove(filepath.Join(r.Root, path)); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("could not remove %q for %s: %w", path, side, err)
			}
		} else {
			blob, err := r.BlobContent(ctx, oid)
			if err != nil {
				return fmt.Errorf("could not read %s version of %q: %w", side, path, err)
			}
			if blob.Binary {
				return fmt.Errorf("%q is binary: choose a side with an external tool", path)
			}
			if err := writeWorktreeFile(r.Root, path, blob.Data, mode); err != nil {
				return err
			}
		}
		if mark {
			if _, err := run(ctx, r.Root, "add", "-A", "--", path); err != nil {
				return fmt.Errorf("wrote %s of %q but could not mark it resolved: %w", side, path, err)
			}
		}
		return nil
	})
}

// ResolveOurs writes the "ours" side over the working file. It never marks the
// file resolved unless mark is set.
func (r Repository) ResolveOurs(ctx context.Context, path string, mark bool) error {
	return r.resolveSide(ctx, path, true, mark)
}

// ResolveTheirs writes the "theirs" side over the working file.
func (r Repository) ResolveTheirs(ctx context.Context, path string, mark bool) error {
	return r.resolveSide(ctx, path, false, mark)
}

// KeepBoth rewrites each conflict region as "ours" followed by "theirs", with
// the markers removed. It is a predictable concatenation, not a semantic
// merge, and the file is left unstaged for the user to edit.
func (r Repository) KeepBoth(ctx context.Context, path string) error {
	return r.mutate(ctx, func() error {
		if err := validPath(path); err != nil {
			return err
		}
		content, ok, err := r.WorkingFile(path)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%q has no working file to merge", path)
		}
		regions := ParseConflictRegions(content)
		if len(regions) == 0 {
			return fmt.Errorf("%q has no conflict regions to combine", path)
		}
		if strings.IndexByte(content, 0) >= 0 {
			return fmt.Errorf("%q is binary: keeping both sides is not available", path)
		}
		lines := strings.Split(content, "\n")
		var out []string
		prev := 0
		for _, region := range regions {
			out = append(out, lines[prev:region.StartLine-1]...)
			out = append(out, region.OursLines...)
			out = append(out, region.TheirsLines...)
			prev = region.EndLine
		}
		out = append(out, lines[prev:]...)
		if err := writeWorktreeFile(r.Root, path, []byte(strings.Join(out, "\n")), ""); err != nil {
			return err
		}
		return nil
	})
}

// MarkResolved stages the working-tree state of a path, which is how Git
// records that a conflict is resolved. Deletions are recorded too.
func (r Repository) MarkResolved(ctx context.Context, path string) error {
	return r.mutate(ctx, func() error {
		if err := validPath(path); err != nil {
			return err
		}
		if _, err := run(ctx, r.Root, "add", "-A", "--", path); err != nil {
			return fmt.Errorf("could not mark %q resolved: %w", path, err)
		}
		return nil
	})
}

// RestoreWorktree discards working-tree changes to a path.
func (r Repository) RestoreWorktree(ctx context.Context, path string) error {
	return r.mutate(ctx, func() error {
		if err := validPath(path); err != nil {
			return err
		}
		if _, err := run(ctx, r.Root, "restore", "--", path); err != nil {
			return fmt.Errorf("could not restore %q (working tree): %w", path, err)
		}
		return nil
	})
}

// RestoreStaged resets the index entry for a path from HEAD.
func (r Repository) RestoreStaged(ctx context.Context, path string) error {
	return r.mutate(ctx, func() error {
		if err := validPath(path); err != nil {
			return err
		}
		if _, err := run(ctx, r.Root, "restore", "--staged", "--", path); err != nil {
			return fmt.Errorf("could not restore %q (index): %w", path, err)
		}
		return nil
	})
}

// RestoreFrom replaces a path with its content at a revision, in both the index
// and the working tree.
func (r Repository) RestoreFrom(ctx context.Context, rev, path string) error {
	return r.mutate(ctx, func() error {
		if err := validPath(path); err != nil {
			return err
		}
		if err := validRevision(rev); err != nil {
			return err
		}
		if _, err := run(ctx, r.Root, "restore", "--source="+rev, "--staged", "--worktree", "--", path); err != nil {
			return fmt.Errorf("could not restore %q from %s: %w", path, rev, err)
		}
		return nil
	})
}

// writeWorktreeFile writes content and applies the index mode when one is
// known, so an executable file does not silently lose its bit.
func writeWorktreeFile(root, path string, data []byte, mode string) error {
	full := filepath.Join(root, path)
	perm := os.FileMode(0o644)
	if mode == "100755" {
		perm = 0o755
	} else if info, err := os.Stat(full); err == nil {
		perm = info.Mode().Perm()
	}
	if dir := filepath.Dir(full); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if err := os.WriteFile(full, data, perm); err != nil {
		return fmt.Errorf("could not write %q: %w", path, err)
	}
	return nil
}
