package diff

// SideRow is one aligned row of a side-by-side view. Either side may be nil,
// which renders as a blank placeholder.
type SideRow struct {
	Left, Right *Line
	Modified    bool
}

// SideRows aligns one hunk's lines for a two-column view: context appears on
// both sides, and each replacement block pairs its deletions and additions.
func (h Hunk) SideRows() []SideRow {
	lines := h.Lines
	var rows []SideRow
	i := 0
	for i < len(lines) {
		switch lines[i].Type {
		case Addition:
			rows = append(rows, SideRow{Right: &lines[i]})
			i++
			continue
		case NoNewline:
			rows = append(rows, SideRow{Left: &lines[i]})
			i++
			continue
		case Deletion:
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
				rows = append(rows, SideRow{Left: &lines[delStart+k], Right: &lines[addStart+k], Modified: true})
			}
			for k := pairs; k < delEnd-delStart; k++ {
				rows = append(rows, SideRow{Left: &lines[delStart+k]})
			}
			for k := pairs; k < addEnd-addStart; k++ {
				rows = append(rows, SideRow{Right: &lines[addStart+k]})
			}
			continue
		default:
			rows = append(rows, SideRow{Left: &lines[i], Right: &lines[i]})
			i++
		}
	}
	return rows
}

// SideRowsForFiles flattens every file's hunks into side rows, stopping at the
// given file index. It is a convenience for single-file rendering.
func (f File) SideRows(hunkIndex int) []SideRow {
	if hunkIndex < 0 || hunkIndex >= len(f.Hunks) {
		return nil
	}
	return f.Hunks[hunkIndex].SideRows()
}

// DisplayLine is one rendered line for unified mode, possibly a collapsed
// context marker.
type DisplayLine struct {
	Line      *Line
	Collapsed bool
	Hidden    int
	GapStart  int
}

// CollapseContext hides long runs of unchanged context. A run longer than
// threshold is reduced to keep lines at each end plus a marker; the marker is
// keyed by its starting index so expansion survives navigation. Runs at or
// below threshold are shown in full.
func CollapseContext(lines []Line, keep, threshold int, expanded map[int]bool) []DisplayLine {
	if keep < 0 {
		keep = 0
	}
	var out []DisplayLine
	i := 0
	for i < len(lines) {
		if lines[i].Type != Context {
			out = append(out, DisplayLine{Line: &lines[i]})
			i++
			continue
		}
		start := i
		for i < len(lines) && lines[i].Type == Context {
			i++
		}
		run := i - start
		if run <= threshold || expanded[start] || run <= 2*keep {
			for j := start; j < i; j++ {
				out = append(out, DisplayLine{Line: &lines[j]})
			}
			continue
		}
		for j := start; j < start+keep; j++ {
			out = append(out, DisplayLine{Line: &lines[j]})
		}
		hidden := run - 2*keep
		out = append(out, DisplayLine{Collapsed: true, Hidden: hidden, GapStart: start})
		for j := i - keep; j < i; j++ {
			out = append(out, DisplayLine{Line: &lines[j]})
		}
	}
	return out
}
