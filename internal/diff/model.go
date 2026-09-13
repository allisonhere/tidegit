// Package diff is TideGit's structured, reusable diff model. It parses raw
// unified patch text into files, hunks and semantically typed lines, computes
// side-by-side alignment and word-level change spans, and collapses context.
// It never runs Git and never imports a terminal library: callers generate the
// patch and render the result.
package diff

import (
	"fmt"
	"strconv"
	"strings"
)

// LineType is the semantic kind of one diff line.
type LineType uint8

const (
	Context LineType = iota
	Addition
	Deletion
	HunkHeader
	MetaLine
	NoNewline
)

// Marker is the single character shown in front of a line in unified mode.
func (t LineType) Marker() string {
	switch t {
	case Addition:
		return "+"
	case Deletion:
		return "-"
	case Context:
		return " "
	case NoNewline:
		return "\\"
	default:
		return ""
	}
}

// Span is a half-open rune range within a line's Text. Changed marks a range
// that differs from the corresponding line on the other side.
type Span struct {
	Start, End int
	Changed    bool
}

// Line is one line of a hunk body or a header/meta line.
type Line struct {
	Type      LineType
	Text      string // content without the +/-/space marker for body lines
	Old, New  int    // 1-based line numbers; 0 when the side has no line
	Spans     []Span // word-level change spans for modified lines
	NoNewline bool   // the following "\ No newline" belongs to this line
}

// Hunk is one @@ block with its body.
type Hunk struct {
	Header                                 string
	Section                                string
	OldStart, OldCount, NewStart, NewCount int
	Lines                                  []Line
	Index                                  int
	BodyStart                              int // index into File.Lines of the first body line
}

// File is one changed path with its metadata and hunks.
type File struct {
	OldPath, NewPath string
	Status           byte // A, M, D, R, C, T; 0 when unknown
	Binary           bool
	Rename, Copy     bool
	Similarity       int
	OldMode, NewMode string
	Meta             []string
	Hunks            []Hunk
}

// Display returns the path to show for the file.
func (f File) Display() string {
	if f.NewPath != "" && f.NewPath != "/dev/null" {
		return f.NewPath
	}
	return f.OldPath
}

// Patch is a parsed multi-file diff.
type Patch struct {
	Files     []File
	Raw       string
	Truncated bool
	Conflict  bool
	// Label names the snapshot, e.g. "STAGED" or "COMMIT 7ac92be". The viewer
	// shows it so the context is never a guess.
	Label string
}

// TotalHunks counts hunks across every file.
func (p Patch) TotalHunks() int {
	n := 0
	for _, f := range p.Files {
		n += len(f.Hunks)
	}
	return n
}

// TotalLines counts hunk body lines across every file, for the large-diff
// threshold and for progress.
func (p Patch) TotalLines() int {
	n := 0
	for _, f := range p.Files {
		for _, h := range f.Hunks {
			n += len(h.Lines)
		}
	}
	return n
}

// Parse turns unified patch text into the structured model. It is tolerant:
// unrecognised lines are metadata, and a malformed hunk header stops that
// hunk's body rather than failing the whole patch.
func Parse(raw string) Patch {
	p := Patch{Raw: raw, Conflict: strings.Contains(raw, "\n@@@ ") || strings.HasPrefix(raw, "@@@ ")}
	if strings.Contains(raw, "Binary files ") || strings.Contains(raw, "GIT binary patch") {
		// Binary state is per file; detected during the walk below.
	}
	lines := strings.Split(strings.TrimSuffix(raw, "\n"), "\n")
	var file *File
	var hunk *Hunk
	old, newLine := 0, 0

	flushHunk := func() {
		if file != nil && hunk != nil {
			file.Hunks = append(file.Hunks, *hunk)
			hunk = nil
		}
	}
	flushFile := func() {
		flushHunk()
		if file != nil {
			p.Files = append(p.Files, *file)
			file = nil
		}
	}

	for _, raw := range lines {
		switch {
		case strings.HasPrefix(raw, "diff --git "):
			flushFile()
			oldPath, newPath := parseDiffGit(raw)
			file = &File{OldPath: oldPath, NewPath: newPath, Status: 'M'}
			continue
		case file == nil:
			// Preamble before any file (rare); ignore.
			continue
		case strings.HasPrefix(raw, "@@ "):
			flushHunk()
			header, ok := parseHunkHeader(raw)
			if !ok {
				file.Meta = append(file.Meta, raw)
				continue
			}
			header.Index = len(file.Hunks)
			hunk = &header
			old, newLine = header.OldStart, header.NewStart
			continue
		case strings.HasPrefix(raw, "@@@") || strings.HasPrefix(raw, "diff --cc ") || strings.HasPrefix(raw, "diff --combined "):
			// Combined conflict diff: keep it visible as metadata; the
			// Conflicts screen renders its own two-way comparisons.
			flushHunk()
			file.Meta = append(file.Meta, raw)
			continue
		}

		if hunk != nil {
			line, nextOld, nextNew, handled := parseBodyLine(raw, old, newLine)
			if handled {
				if line != nil {
					hunk.Lines = append(hunk.Lines, *line)
					if line.NoNewline {
						applyNoNewline(hunk)
					}
				}
				old, newLine = nextOld, nextNew
				continue
			}
		}

		// Metadata outside a hunk.
		switch {
		case strings.HasPrefix(raw, "new file mode "):
			file.Status = 'A'
			file.NewMode = strings.TrimSpace(strings.TrimPrefix(raw, "new file mode "))
		case strings.HasPrefix(raw, "deleted file mode "):
			file.Status = 'D'
			file.OldMode = strings.TrimSpace(strings.TrimPrefix(raw, "deleted file mode "))
		case strings.HasPrefix(raw, "old mode "):
			file.OldMode = strings.TrimSpace(strings.TrimPrefix(raw, "old mode "))
		case strings.HasPrefix(raw, "new mode "):
			file.NewMode = strings.TrimSpace(strings.TrimPrefix(raw, "new mode "))
		case strings.HasPrefix(raw, "rename from "):
			file.Rename = true
			file.Status = 'R'
			file.OldPath = unquoteGitPath(strings.TrimPrefix(raw, "rename from "))
		case strings.HasPrefix(raw, "rename to "):
			file.Rename = true
			file.Status = 'R'
			file.NewPath = unquoteGitPath(strings.TrimPrefix(raw, "rename to "))
		case strings.HasPrefix(raw, "copy from "):
			file.Copy = true
			file.Status = 'C'
			file.OldPath = unquoteGitPath(strings.TrimPrefix(raw, "copy from "))
		case strings.HasPrefix(raw, "copy to "):
			file.Copy = true
			file.Status = 'C'
			file.NewPath = unquoteGitPath(strings.TrimPrefix(raw, "copy to "))
		case strings.HasPrefix(raw, "similarity index "):
			if n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(raw, "similarity index "), "%")); err == nil {
				file.Similarity = n
			}
		case strings.HasPrefix(raw, "dissimilarity index "):
			if n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(raw, "dissimilarity index "), "%")); err == nil {
				file.Similarity = 100 - n
			}
		case strings.HasPrefix(raw, "--- "):
			file.OldPath = stripAB(unquoteGitPath(strings.TrimPrefix(raw, "--- ")))
		case strings.HasPrefix(raw, "+++ "):
			file.NewPath = stripAB(unquoteGitPath(strings.TrimPrefix(raw, "+++ ")))
		case strings.HasPrefix(raw, "Binary files ") || strings.HasPrefix(raw, "GIT binary patch"):
			file.Binary = true
		}
		file.Meta = append(file.Meta, raw)
	}
	flushFile()
	for i := range p.Files {
		annotateWords(&p.Files[i])
	}
	return p
}

// parseBodyLine parses one hunk-body line and advances the line counters.
func parseBodyLine(raw string, old, newLine int) (*Line, int, int, bool) {
	if raw == "" {
		// A trailing empty line between hunks is not a context line.
		return nil, old, newLine, false
	}
	switch raw[0] {
	case ' ':
		line := Line{Type: Context, Text: raw[1:], Old: old, New: newLine}
		return &line, old + 1, newLine + 1, true
	case '+':
		line := Line{Type: Addition, Text: raw[1:], New: newLine}
		return &line, old, newLine + 1, true
	case '-':
		line := Line{Type: Deletion, Text: raw[1:], Old: old}
		return &line, old + 1, newLine, true
	case '\\':
		if strings.HasPrefix(raw, "\\ No newline at end of file") {
			return &Line{Type: NoNewline, Text: raw, NoNewline: true}, old, newLine, true
		}
	}
	return nil, old, newLine, false
}

func applyNoNewline(h *Hunk) {
	if len(h.Lines) >= 2 {
		h.Lines[len(h.Lines)-2].NoNewline = true
	}
}

func parseDiffGit(raw string) (string, string) {
	rest := strings.TrimPrefix(raw, "diff --git ")
	// Prefer the quoted form: "a/path" "b/path".
	if strings.HasPrefix(rest, "\"") {
		if first, tail, ok := cutQuoted(rest); ok {
			if second, _, ok := cutQuoted(strings.TrimSpace(tail)); ok {
				return stripAB(first), stripAB(second)
			}
		}
	}
	fields := strings.Fields(rest)
	if len(fields) >= 2 {
		return stripAB(fields[0]), stripAB(fields[1])
	}
	return "", ""
}

func cutQuoted(s string) (value, rest string, ok bool) {
	if !strings.HasPrefix(s, "\"") {
		return "", s, false
	}
	var b strings.Builder
	i := 1
	for i < len(s) {
		c := s[i]
		if c == '\\' && i+1 < len(s) {
			b.WriteByte(s[i+1])
			i += 2
			continue
		}
		if c == '"' {
			return b.String(), s[i+1:], true
		}
		b.WriteByte(c)
		i++
	}
	return "", s, false
}

func unquoteGitPath(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "\"") {
		if v, _, ok := cutQuoted(s); ok {
			return v
		}
	}
	return s
}

func stripAB(path string) string {
	if path == "/dev/null" {
		return path
	}
	if len(path) > 2 && path[1] == '/' && (path[0] == 'a' || path[0] == 'b') {
		return path[2:]
	}
	return path
}

func parseHunkHeader(raw string) (Hunk, bool) {
	// @@ -oldStart[,oldCount] +newStart[,newCount] @@[ section]
	rest, ok := strings.CutPrefix(raw, "@@ ")
	if !ok {
		return Hunk{}, false
	}
	end := strings.Index(rest, " @@")
	if end < 0 {
		return Hunk{}, false
	}
	ranges := rest[:end]
	section := strings.TrimSpace(rest[end+3:])
	h := Hunk{Header: raw, Section: section}
	parts := strings.Fields(ranges)
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "-") || !strings.HasPrefix(parts[1], "+") {
		return Hunk{}, false
	}
	oldStart, oldCount, ok := parseRange(parts[0][1:])
	if !ok {
		return Hunk{}, false
	}
	newStart, newCount, ok := parseRange(parts[1][1:])
	if !ok {
		return Hunk{}, false
	}
	h.OldStart, h.OldCount, h.NewStart, h.NewCount = oldStart, oldCount, newStart, newCount
	return h, true
}

func parseRange(s string) (start, count int, ok bool) {
	count = 1
	if i := strings.IndexByte(s, ','); i >= 0 {
		n, err := strconv.Atoi(s[i+1:])
		if err != nil {
			return 0, 0, false
		}
		count = n
		s = s[:i]
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, 0, false
	}
	return n, count, true
}

// annotateWords fills in word-level spans for replacement blocks within each
// hunk. A block is a maximal run of deletions immediately followed by a run of
// additions; lines are paired in order.
func annotateWords(f *File) {
	for hi := range f.Hunks {
		lines := f.Hunks[hi].Lines
		for i := 0; i < len(lines); {
			if lines[i].Type != Deletion {
				i++
				continue
			}
			delStart := i
			for i < len(lines) && lines[i].Type == Deletion {
				i++
			}
			delEnd := i
			addStart := i
			for i < len(lines) && lines[i].Type == Addition {
				i++
			}
			addEnd := i
			pairs := min(delEnd-delStart, addEnd-addStart)
			for k := 0; k < pairs; k++ {
				oldLine := &lines[delStart+k]
				newLine := &lines[addStart+k]
				oldLine.Spans, newLine.Spans = wordSpans(oldLine.Text, newLine.Text)
			}
			// Unpaired lines are wholly changed.
			for k := pairs; k < delEnd-delStart; k++ {
				lines[delStart+k].Spans = wholeLineSpans(lines[delStart+k].Text)
			}
			for k := pairs; k < addEnd-addStart; k++ {
				lines[addStart+k].Spans = wholeLineSpans(lines[addStart+k].Text)
			}
		}
	}
}

func wholeLineSpans(text string) []Span {
	if text == "" {
		return nil
	}
	return []Span{{Start: 0, End: len([]rune(text)), Changed: true}}
}

// Summary returns the compact per-file metadata shown above a diff.
type Summary struct {
	Path       string
	OldPath    string
	Status     byte
	Additions  int
	Deletions  int
	Hunks      int
	Binary     bool
	Rename     bool
	Similarity int
}

// Summary computes counts for one file.
func (f File) Summary() Summary {
	s := Summary{Path: f.Display(), OldPath: f.OldPath, Status: f.Status, Hunks: len(f.Hunks),
		Binary: f.Binary, Rename: f.Rename, Similarity: f.Similarity}
	for _, h := range f.Hunks {
		for _, l := range h.Lines {
			switch l.Type {
			case Addition:
				s.Additions++
			case Deletion:
				s.Deletions++
			}
		}
	}
	return s
}

// DescribeStatus words a status byte for a badge.
func DescribeStatus(status byte) string {
	switch status {
	case 'A':
		return "added"
	case 'D':
		return "deleted"
	case 'R':
		return "renamed"
	case 'C':
		return "copied"
	case 'T':
		return "type changed"
	case 'M':
		return "modified"
	default:
		return "changed"
	}
}

// String is a debugging aid.
func (f File) String() string {
	return fmt.Sprintf("%s (%c, %d hunks)", f.Display(), f.Status, len(f.Hunks))
}
