package git

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Hunk preserves original patch syntax independently of screen rows. Counts and
// body lines form a seam for future reduced patches built from selected lines.
type Hunk struct {
	ID                                     string
	File                                   File
	Source                                 Section
	OldPath, NewPath                       string
	FileHeader, Header                     string
	Body                                   []string
	OldStart, OldCount, NewStart, NewCount int
	PatchLine                              int // navigation hint only, never used to identify a staging target
}

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

func parseHunks(d Diff) ([]Hunk, string) {
	if d.Truncated {
		return nil, "Hunk actions unavailable: truncated patch; use whole-file staging."
	}
	if d.Conflict {
		return nil, "Hunk actions unavailable for conflicts."
	}
	if strings.HasPrefix(d.Patch, "Binary files ") || strings.Contains(d.Patch, "\nBinary files ") {
		return nil, "Binary file: use whole-file staging."
	}
	lines := strings.SplitAfter(d.Patch, "\n")
	var hunks []Hunk
	header := ""
	oldPath, newPath := "", ""
	fileCount := 0
	for i := 0; i < len(lines); {
		line := lines[i]
		if strings.HasPrefix(line, "diff --git ") {
			fileCount++
			header = ""
			oldPath = ""
			newPath = ""
		}
		if !strings.HasPrefix(line, "@@ ") {
			if strings.HasPrefix(line, "index ") && strings.HasSuffix(line, " 160000\n") || strings.HasPrefix(line, "new file mode 160000") || strings.HasPrefix(line, "deleted file mode 160000") {
				return nil, "Submodule hunks are not supported."
			}
			header += line
			if strings.HasPrefix(line, "--- ") {
				oldPath = strings.TrimSuffix(line[4:], "\n")
			}
			if strings.HasPrefix(line, "+++ ") {
				newPath = strings.TrimSuffix(line[4:], "\n")
			}
			i++
			continue
		}
		m := hunkHeader.FindStringSubmatch(line)
		if m == nil || oldPath == "" || newPath == "" {
			return nil, "Malformed patch headers; refresh before staging."
		}
		n := func(v string, def int) int {
			if v == "" {
				return def
			}
			x, _ := strconv.Atoi(v)
			return x
		}
		h := Hunk{File: d.File, Source: d.Source, OldPath: oldPath, NewPath: newPath, FileHeader: header, Header: line, OldStart: n(m[1], 0), OldCount: n(m[2], 1), NewStart: n(m[3], 0), NewCount: n(m[4], 1), PatchLine: i}
		i++
		old, newCount := 0, 0
		for i < len(lines) && !strings.HasPrefix(lines[i], "@@ ") && !strings.HasPrefix(lines[i], "diff --git ") {
			body := lines[i]
			if body == "" {
				i++
				break
			}
			switch body[0] {
			case ' ':
				old++
				newCount++
			case '-':
				old++
			case '+':
				newCount++
			case '\\':
				if !strings.HasPrefix(body, "\\ No newline at end of file") {
					return nil, "Malformed patch marker."
				}
			default:
				return nil, "Malformed patch body."
			}
			h.Body = append(h.Body, body)
			i++
		}
		if old != h.OldCount || newCount != h.NewCount {
			return nil, "Incomplete patch hunk; refresh before staging."
		}
		h.ID = fmt.Sprintf("%x", sha256.Sum256([]byte(header+h.Header+strings.Join(h.Body, ""))))
		hunks = append(hunks, h)
	}
	if fileCount > 1 {
		return nil, "This change contains multiple file patches; use whole-file staging."
	}
	if len(hunks) == 0 {
		return nil, "No text hunks; use whole-file staging for metadata changes."
	}
	return hunks, ""
}

// patch uses content headers only for existing files, keeping staged rename,
// copy and permission metadata intact when reversing an individual text hunk.
func (h Hunk) patch() string {
	header := h.FileHeader
	if h.OldPath != "/dev/null" && h.NewPath != "/dev/null" {
		header = "diff --git " + quotePath("a/"+h.File.Path) + " " + quotePath("b/"+h.File.Path) + "\n--- " + quotePath("a/"+h.File.Path) + "\n+++ " + quotePath("b/"+h.File.Path) + "\n"
	}
	return header + h.Header + strings.Join(h.Body, "")
}

// Git's quoted paths use C escapes with octal bytes, not Go's Unicode escapes.
func quotePath(path string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, c := range []byte(path) {
		switch {
		case c == '"' || c == '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		case c < 32 || c >= 127:
			b.WriteString(fmt.Sprintf("\\%03o", c))
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}
