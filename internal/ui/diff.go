package ui

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/allisonhere/tidegit/internal/git"
	"github.com/allisonhere/tideui"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// safeText prevents repository-controlled terminal escapes, including OSC, from
// becoming terminal commands. Newlines are only allowed in structural content.
func safeText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return '�'
		}
		return r
	}, s)
}

type diffLine struct {
	text string
	kind byte
}

func diffLines(d git.Diff) []diffLine {
	var lines []diffLine
	if d.Conflict {
		lines = append(lines, diffLine{"CONFLICT: edit the file externally; staging is not available in Milestone 1.", '!'})
	}
	old, newLine := 0, 0
	inHunk := false
	const maxLines = 20000
	rawLines := strings.SplitN(strings.TrimSuffix(d.Patch, "\n"), "\n", maxLines+1)
	for _, raw := range rawLines[:min(len(rawLines), maxLines)] {
		kind := byte(' ')
		text := safeText(strings.ReplaceAll(raw, "\t", "    "))
		switch {
		case strings.HasPrefix(raw, "diff "):
			inHunk = false
			kind = '@'
		case strings.HasPrefix(raw, "@@ "):
			inHunk = true
			var a, b string
			if _, err := fmt.Sscanf(raw, "@@ -%s +%s @@", &a, &b); err == nil {
				fmt.Sscanf(a, "%d", &old)
				fmt.Sscanf(b, "%d", &newLine)
			}
			kind = '@'
		case strings.HasPrefix(raw, "@@@"), !inHunk && (strings.HasPrefix(raw, "index ") || strings.HasPrefix(raw, "--- ") || strings.HasPrefix(raw, "+++ ") || strings.HasPrefix(raw, "Binary")):
			kind = '@'
		case strings.HasPrefix(raw, "+"):
			kind = '+'
			if !d.Conflict {
				text = fmt.Sprintf("     %5d %s", newLine, text)
				newLine++
			}
		case strings.HasPrefix(raw, "-"):
			kind = '-'
			if !d.Conflict {
				text = fmt.Sprintf("%5d       %s", old, text)
				old++
			}
		case strings.HasPrefix(raw, " "):
			if !d.Conflict {
				text = fmt.Sprintf("%5d %5d %s", old, newLine, text)
				old++
				newLine++
			}
		}
		lines = append(lines, diffLine{text, kind})
	}
	if d.Truncated {
		lines = append(lines, diffLine{"Preview truncated at 4 MiB. Inspect the full diff with Git.", '!'})
	}
	if len(rawLines) > maxLines {
		lines = append(lines, diffLine{"Preview limited to 20,000 lines. Inspect the full diff with Git.", '!'})
	}
	return lines
}
func outputWidth(lines []diffLine) int {
	w := 0
	for _, line := range lines {
		w = max(w, lipgloss.Width(line.text))
	}
	return max(0, w-1)
}
func renderDiff(lines []diffLine, r tideui.Renderer, offset, horizontal, height, width int) string {
	offset = min(offset, max(0, len(lines)-height))
	out := make([]string, 0, height)
	for _, line := range lines[offset:min(len(lines), offset+height)] {
		style := r.Styles.DetailBody
		switch line.kind {
		case '+':
			style = style.Foreground(r.Styles.Theme.Unread)
		case '-', '!':
			style = style.Foreground(r.Styles.Theme.Error)
		case '@':
			style = style.Foreground(r.Styles.Theme.BorderFocus).Bold(true)
		}
		out = append(out, style.Render(ansi.Cut(line.text, horizontal, horizontal+width)))
	}
	return strings.Join(out, "\n")
}
