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
	return graphGlyphs{
		vertical: "│", horizontal: "─", cross: "┼",
		mergeLeft: "╰", mergeRight: "╯", forkLeft: "╭", forkRight: "╮",
		forkTee: "┬", mergeTee: "┴",
		node: "●", mergeNode: "◆", rootNode: "○", headNode: "◉",
		overflow: "›", blank: " ",
	}
}

// graphWidth is the rendered width of a graph column holding up to lanes lanes.
// Each lane occupies two cells so connectors have room to travel.
func graphWidth(lanes int) int {
	return 2*min(max(lanes, 1), graphLaneLimit) + 1
}

// renderGraphRow draws one commit's lane geometry. Lines stay quiet and nodes
// take the accent so a branch can be followed down the column. On the selected
// row the lines brighten instead of dimming out: selection must never hide the
// topology it is sitting on.
func renderGraphRow(r tideui.Renderer, row git.GraphRow, head, selected bool, width int, base lipgloss.Style) string {
	g := glyphSet(r.Styles.Theme.UsesASCII())
	lineStyle := base.Foreground(r.Styles.Theme.Dimmed)
	nodeStyle := base.Foreground(r.Styles.Theme.BorderFocus).Bold(true)
	if head {
		nodeStyle = base.Foreground(r.Styles.Theme.Unread).Bold(true)
	}
	if selected {
		// The selection background is close to the dimmed colour, so the row's
		// own foreground carries the lines and the node keeps its accent shape.
		lineStyle = base
		nodeStyle = base.Bold(true)
	}
	var b strings.Builder
	shown := min(row.Width(), graphLaneLimit)
	for lane := 0; lane < shown; lane++ {
		glyph, style := g.blank, lineStyle
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
		filler := g.blank
		if lane+1 < shown && spans(row, lane) {
			filler = g.horizontal
		}
		b.WriteString(lineStyle.Render(filler))
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
