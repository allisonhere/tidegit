package ui

import (
	"fmt"
	"strings"

	"github.com/allisonhere/tidegit/internal/git"
	"github.com/allisonhere/tideui"
	"github.com/charmbracelet/lipgloss"
)

// graphLaneLimit caps how far the drawing may spread sideways. A history wider
// than this keeps its leftmost lanes, which are the ones the eye follows, and
// marks the rest rather than pushing the subject off the row.
const graphLaneLimit = 6

// graphGlyphs maps lane geometry to box-drawing characters. The ASCII set is
// used by plain themes, where the box characters are not available.
type graphGlyphs struct {
	vertical, horizontal, cross         string
	mergeLeft, mergeRight               string
	forkLeft, forkRight                 string
	forkTee, mergeTee                   string
	node, mergeNode, rootNode, headNode string
	overflow, blank                     string
}

func glyphSet(plain bool) graphGlyphs {
	if plain {
		return graphGlyphs{
			vertical: "|", horizontal: "-", cross: "+",
			mergeLeft: "\\", mergeRight: "/", forkLeft: "/", forkRight: "\\",
			forkTee: "+", mergeTee: "+",
			node: "*", mergeNode: "%", rootNode: "o", headNode: "@",
			overflow: ">", blank: " ",
		}
	}
	// Heavy lines with light arcs for the corners. The heavy stroke is what
	// makes a lane read as a continuous line rather than a column of dots,
	// which is what the branch colours need to be legible; the arcs keep the
	// turns soft, matching the rounded panes around them.
	//
	// Unicode has no heavy arc, so a corner is lighter than the lines it joins.
	// At a terminal's cell size that reads as a taper into the turn rather than
	// a break, and it was worth the trade for keeping the curves.
	return graphGlyphs{
		vertical: "┃", horizontal: "━", cross: "╋",
		mergeLeft: "╰", mergeRight: "╯", forkLeft: "╭", forkRight: "╮",
		forkTee: "┳", mergeTee: "┻",
		node: "●", mergeNode: "◆", rootNode: "○", headNode: "◉",
		overflow: "›", blank: " ",
	}
}

// allGlyphs returns every character a graph can draw, for callers that need to
// recognise graph cells without repeating the table.
func (g graphGlyphs) all() string {
	return g.vertical + g.horizontal + g.cross + g.mergeLeft + g.mergeRight +
		g.forkLeft + g.forkRight + g.forkTee + g.mergeTee +
		g.node + g.mergeNode + g.rootNode + g.headNode + g.overflow
}

// graphWidth is the rendered width of a graph column holding up to lanes lanes.
// Each lane occupies two cells so connectors have room to travel.
func graphWidth(lanes int) int {
	return 2*min(max(lanes, 1), graphLaneLimit) + 1
}

// laneHues are the rotations applied to a theme's accent to build the lane
// palette. They are uneven on purpose: evenly spaced hues put muddy neighbours
// next to each other, and adjacent lanes are exactly where two colours have to
// be told apart.
var laneHues = []float64{0, 48, 152, 205, 96, 268}

// laneMinContrast keeps every lane legible against whatever it is drawn on.
const laneMinContrast = 3.0

// lanePalette builds the colours a graph's lines are drawn in, derived from the
// theme's own accent so they belong to it rather than being picked arbitrarily,
// and corrected so each one is legible against the surface behind it.
//
// A theme with no hue variety of its own — an amber or green phosphor terminal —
// gets no palette at all. Inventing colour for it would destroy the thing that
// makes it that theme.
func lanePalette(theme tideui.Theme, on lipgloss.Color) []lipgloss.Color {
	if theme.UsesASCII() || monochromeTheme(theme) {
		return nil
	}
	palette := make([]lipgloss.Color, 0, len(laneHues))
	for _, shift := range laneHues {
		palette = append(palette,
			tideui.AccentReadableOn(tideui.ShiftHue(theme.BorderFocus, shift), on, laneMinContrast))
	}
	return palette
}

// monochromeTheme reports whether a theme's own colours share a single hue.
// Error is left out of the comparison: it is red in every theme, so including
// it would make every palette look varied.
func monochromeTheme(theme tideui.Theme) bool {
	var hues []float64
	for _, c := range []lipgloss.Color{theme.Fg, theme.BorderFocus, theme.Unread, theme.Selected} {
		h, saturation, ok := tideui.Hue(c)
		if ok && saturation > 0.12 {
			hues = append(hues, h)
		}
	}
	if len(hues) < 2 {
		return true
	}
	for _, a := range hues {
		for _, b := range hues {
			if hueDistance(a, b) > 40 {
				return false
			}
		}
	}
	return true
}

// hueDistance is the shorter way round the colour wheel between two hues.
func hueDistance(a, b float64) float64 {
	d := a - b
	if d < 0 {
		d = -d
	}
	if d > 180 {
		d = 360 - d
	}
	return d
}

// laneColor picks a line's colour by its identity, so a branch keeps one colour
// for as long as it exists rather than taking the colour of whichever column it
// happens to occupy.
func laneColor(palette []lipgloss.Color, track int) (lipgloss.Color, bool) {
	if len(palette) == 0 || track < 0 {
		return "", false
	}
	return palette[track%len(palette)], true
}

// renderGraphRow draws one commit's lane geometry. Each line carries its own
// colour so a branch can be followed down the column, and the node is the bold
// one so the commits still lead. On the selected row the palette is rebuilt
// against the selection background: selection must never hide the topology it
// is sitting on.
func renderGraphRow(r tideui.Renderer, row git.GraphRow, head, selected bool, width int, base lipgloss.Style) string {
	g := glyphSet(r.Styles.Theme.UsesASCII())
	surface := r.Styles.Theme.Bg
	if selected {
		surface = selectionSurface(r)
	}
	palette := lanePalette(r.Styles.Theme, surface)

	lineStyle := base.Foreground(r.Styles.Theme.Dimmed)
	nodeStyle := base.Foreground(r.Styles.Theme.BorderFocus).Bold(true)
	if head {
		nodeStyle = base.Foreground(r.Styles.Theme.Unread).Bold(true)
	}
	if selected {
		// Without a palette the selection background sits too close to the
		// dimmed colour, so the row's own foreground carries the lines.
		lineStyle = base
		nodeStyle = base.Bold(true)
	}
	var b strings.Builder
	shown := min(row.Width(), graphLaneLimit)
	for lane := 0; lane < shown; lane++ {
		glyph, style := g.blank, lineStyle
		if c, ok := laneColor(palette, row.Track(lane)); ok {
			style = base.Foreground(c)
		}
		switch row.Glyphs[lane] {
		case git.GraphVertical:
			glyph = g.vertical
		case git.GraphHorizontal:
			glyph = g.horizontal
		case git.GraphCross:
			glyph = g.cross
		case git.GraphMergeLeft:
			glyph = g.mergeLeft
		case git.GraphMergeRight:
			glyph = g.mergeRight
		case git.GraphForkLeft:
			glyph = g.forkLeft
		case git.GraphForkRight:
			glyph = g.forkRight
		case git.GraphForkTee:
			glyph = g.forkTee
		case git.GraphMergeTee:
			glyph = g.mergeTee
		case git.GraphNode:
			style = nodeStyle
			if c, ok := laneColor(palette, row.Track(lane)); ok && !head {
				// HEAD keeps its own colour; every other node takes its
				// line's, so a branch reads as one colour end to end.
				style = base.Foreground(c).Bold(true)
			}
			switch {
			case head:
				glyph = g.headNode
			case row.Merge:
				glyph = g.mergeNode
			case row.Root:
				glyph = g.rootNode
			default:
				glyph = g.node
			}
		}
		b.WriteString(style.Render(glyph))
		// The cell after a lane carries any horizontal run passing over it.
		// That run belongs to the commit reaching across, so it is drawn in the
		// node's colour rather than the colour of the lane it passes over.
		filler, fillerStyle := g.blank, lineStyle
		if lane+1 < shown && spans(row, lane) {
			filler = g.horizontal
			if c, ok := laneColor(palette, row.Track(row.Lane)); ok {
				fillerStyle = base.Foreground(c)
			}
		}
		b.WriteString(fillerStyle.Render(filler))
	}
	if row.Width() > graphLaneLimit {
		b.WriteString(lineStyle.Render(g.overflow))
	} else {
		b.WriteString(base.Render(g.blank))
	}
	return padLine(b.String(), width, base)
}

// spans reports whether a horizontal connector runs between lane and lane+1,
// which is true while both sides are part of the same connector run.
func spans(row git.GraphRow, lane int) bool {
	joined := func(g git.GraphGlyph) bool {
		switch g {
		case git.GraphHorizontal, git.GraphCross, git.GraphNode,
			git.GraphMergeLeft, git.GraphMergeRight, git.GraphForkLeft, git.GraphForkRight,
			git.GraphForkTee, git.GraphMergeTee:
			return true
		}
		return false
	}
	if !joined(row.Glyphs[lane]) || !joined(row.Glyphs[lane+1]) {
		return false
	}
	// A run only exists between a node and the lane it reaches, so at least one
	// side must be a connector rather than two unrelated nodes.
	left, right := row.Glyphs[lane], row.Glyphs[lane+1]
	if left == git.GraphNode && right == git.GraphNode {
		return false
	}
	// A run stops at the corner that closes it, but passes through a tee.
	switch {
	case lane < row.Lane:
		return left != git.GraphMergeRight && left != git.GraphForkRight
	default:
		return right != git.GraphMergeLeft && right != git.GraphForkLeft
	}
}

// refBadge renders one ref as a compact badge. Kind is carried by a text sigil
// as well as by colour: "@" marks where HEAD is, "#" marks a tag, and a remote
// keeps its "remote/" prefix, so the badges remain distinguishable without
// colour and in a plain terminal.
func refBadge(r tideui.Renderer, ref git.Ref) string {
	theme := r.Styles.Theme
	style := lipgloss.NewStyle().Padding(0, 1)
	label := safeText(ref.Name)
	switch {
	case ref.Kind == git.RefHead || ref.Head:
		style = style.Background(theme.BorderFocus).Foreground(theme.Bg).Bold(true)
		label = "@ " + label
	case ref.Kind == git.RefTag:
		style = style.Background(theme.Unread).Foreground(theme.Bg).Bold(true)
		label = "# " + label
	case ref.Kind == git.RefRemote:
		style = style.Background(theme.Border).Foreground(theme.Fg)
	default:
		style = style.Background(theme.Selected).Foreground(theme.Bg).Bold(true)
	}
	return style.Render(label)
}

// refBadges renders a commit's refs in a stable order — HEAD first, then local
// branches, remote branches and tags — stopping before it runs out of room.
func refBadges(r tideui.Renderer, refs []git.Ref, width int) string {
	if width <= 0 || len(refs) == 0 {
		return ""
	}
	ordered := append([]git.Ref(nil), refs...)
	rank := func(ref git.Ref) int {
		switch {
		case ref.Head:
			return 0
		case ref.Kind == git.RefLocal:
			return 1
		case ref.Kind == git.RefRemote:
			return 2
		default:
			return 3
		}
	}
	for i := 1; i < len(ordered); i++ {
		for j := i; j > 0 && rank(ordered[j]) < rank(ordered[j-1]); j-- {
			ordered[j], ordered[j-1] = ordered[j-1], ordered[j]
		}
	}
	var parts []string
	used := 0
	for i, ref := range ordered {
		badge := refBadge(r, ref)
		w := lipgloss.Width(badge)
		if used+w > width {
			// Refs outrank the hash and the author column, so the first badge
			// is shortened rather than dropped; later ones become a count.
			if used == 0 && width >= 6 {
				short := ref
				short.Name = clip(ref.Name, max(1, width-4))
				parts = append(parts, refBadge(r, short))
				used = width
				if i+1 < len(ordered) {
					parts = append(parts, r.Styles.DetailMeta.Italic(false).
						Render(fmt.Sprintf("+%d", len(ordered)-i-1)))
				}
				break
			}
			parts = append(parts, r.Styles.DetailMeta.Italic(false).
				Render(fmt.Sprintf("+%d", len(ordered)-i)))
			break
		}
		parts = append(parts, badge)
		used += w + 1
	}
	return strings.Join(parts, " ")
}

// selectionSurface is the background a selected row is drawn on, which lane
// colours must be legible against.
func selectionSurface(r tideui.Renderer) lipgloss.Color {
	if bg, ok := r.Styles.ItemSelected.GetBackground().(lipgloss.Color); ok && bg != "" {
		return bg
	}
	return r.Styles.Theme.Bg
}
