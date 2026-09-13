package git

// The commit graph is a pure model: commits in, per-row lane geometry out. It
// holds no styling, glyphs or widths, so a renderer can draw it with box
// characters, ASCII or anything else, and so later screens (rebase, compare)
// can reuse the same topology without the history view.

// closedLane marks a lane released earlier in the same row. Reusing such a
// lane immediately would draw one line both ending and starting in a single
// cell, so it stays reserved until the row is finished.
const closedLane = "\x00closed"

// GraphGlyph is the shape one lane takes on one row.
type GraphGlyph int

const (
	GraphEmpty      GraphGlyph = iota
	GraphNode                  // the commit itself
	GraphVertical              // a lane passing straight through
	GraphMergeLeft             // a lane ending here, joining a node to its left
	GraphMergeRight            // a lane ending here, joining a node to its right
	GraphForkLeft              // a lane starting here, leaving a node to its left
	GraphForkRight             // a lane starting here, leaving a node to its right
	GraphHorizontal            // the span between a node and a lane it reaches
	GraphCross                 // a passing lane crossed by that span
	GraphForkTee               // a lane starting here that a further span crosses
	GraphMergeTee              // a lane ending here that a further span crosses
)

// GraphRow is the lane geometry for one commit.
type GraphRow struct {
	Lane   int          // lane holding the commit node
	Glyphs []GraphGlyph // one entry per active lane, left to right
	Merge  bool         // the commit has more than one parent
	Root   bool         // the commit has no parents
	// Tip marks a commit that no earlier row was waiting for: the start of a
	// branch line rather than a continuation of one.
	Tip bool
}

// Width reports how many lanes this row occupies.
func (g GraphRow) Width() int { return len(g.Glyphs) }

// GraphLanes assigns lanes to commits in the order given, which must be the
// order they are displayed in (Git's topological order). Lane identity is
// stable: a lane keeps its column for as long as a commit is still expected in
// it, so scrolling never reshuffles the drawing.
//
// The algorithm is the standard one: each lane remembers the commit id it is
// waiting for. A commit claims the leftmost lane waiting for it, other lanes
// waiting for the same commit collapse into that one, and the commit's parents
// are handed back out — the first to the commit's own lane, the rest to
// existing lanes already expecting them or to newly opened ones.
func GraphLanes(commits []Commit) []GraphRow {
	if len(commits) == 0 {
		return nil
	}
	var lanes []string // per lane: the commit id that lane is waiting for
	rows := make([]GraphRow, 0, len(commits))

	// place finds the leftmost lane expecting oid, else the leftmost free lane,
	// opening a new lane only when neither exists.
	place := func(oid string) int {
		for i, waiting := range lanes {
			if waiting == oid {
				return i
			}
		}
		for i, waiting := range lanes {
			if waiting == "" {
				lanes[i] = oid
				return i
			}
		}
		lanes = append(lanes, oid)
		return len(lanes) - 1
	}

	for _, c := range commits {
		tip := true
		for _, waiting := range lanes {
			if waiting == c.OID {
				tip = false
				break
			}
		}
		node := place(c.OID)

		// Lanes other than the node's that were also waiting for this commit
		// converge into it and are released.
		var joins []int
		for i, waiting := range lanes {
			if i != node && waiting == c.OID {
				joins = append(joins, i)
				lanes[i] = closedLane
			}
		}

		// Hand out the parents. The first keeps the commit's own lane so a
		// straight history never drifts sideways.
		var forks []int
		if len(c.Parents) == 0 {
			lanes[node] = ""
		} else {
			lanes[node] = c.Parents[0]
			for _, parent := range c.Parents[1:] {
				lane := place(parent)
				if lane != node {
					forks = append(forks, lane)
				}
			}
		}

		rows = append(rows, buildRow(lanes, node, joins, forks, c, tip))
		for i, waiting := range lanes {
			if waiting == closedLane {
				lanes[i] = ""
			}
		}
		trimLanes(&lanes)
	}
	return rows
}

// buildRow draws one row: the node, every lane still passing through it, and
// the connectors for lanes joining or leaving at this commit.
func buildRow(lanes []string, node int, joins, forks []int, c Commit, tip bool) GraphRow {
	width := len(lanes)
	for _, lane := range append(append([]int{node}, joins...), forks...) {
		if lane+1 > width {
			width = lane + 1
		}
	}
	glyphs := make([]GraphGlyph, width)
	for i := 0; i < width; i++ {
		if i < len(lanes) && lanes[i] != "" && lanes[i] != closedLane {
			glyphs[i] = GraphVertical
		}
	}
	// Connectors are drawn before the node so the node always wins its cell.
	connect := func(lane int, ending bool) {
		if lane == node {
			return
		}
		left, right := node, lane
		if lane < node {
			left, right = lane, node
		}
		for i := left + 1; i < right; i++ {
			switch glyphs[i] {
			case GraphVertical:
				glyphs[i] = GraphCross
			case GraphEmpty:
				glyphs[i] = GraphHorizontal
			case GraphForkLeft, GraphForkRight:
				// A commit with three or more parents reaches past the lanes
				// it already opened. Such a corner carries the run through it,
				// which is a tee rather than a corner.
				glyphs[i] = GraphForkTee
			case GraphMergeLeft, GraphMergeRight:
				glyphs[i] = GraphMergeTee
			}
		}
		switch {
		case ending && lane > node:
			glyphs[lane] = GraphMergeRight
		case ending:
			glyphs[lane] = GraphMergeLeft
		case lane > node:
			glyphs[lane] = GraphForkRight
		default:
			glyphs[lane] = GraphForkLeft
		}
	}
	for _, lane := range joins {
		connect(lane, true)
	}
	for _, lane := range forks {
		connect(lane, false)
	}
	glyphs[node] = GraphNode
	return GraphRow{Lane: node, Glyphs: glyphs, Merge: c.Merge(), Root: len(c.Parents) == 0, Tip: tip}
}

// trimLanes drops trailing lanes that are no longer waiting for anything, so
// the graph narrows again once a branch is fully drawn.
func trimLanes(lanes *[]string) {
	l := *lanes
	for len(l) > 0 && l[len(l)-1] == "" {
		l = l[:len(l)-1]
	}
	*lanes = l
}
