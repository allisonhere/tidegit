package ui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidegit/internal/git"
	"github.com/allisonhere/tideui"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// testRenderer pins the colour profile: without a terminal lipgloss strips
// colour, and these tests are about which colours are used.
func testRenderer(t *testing.T, theme tideui.Theme) tideui.Renderer {
	t.Helper()
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
	return tideui.NewRenderer(theme, tideui.StyleOptions{Density: tideui.Compact})
}

func TestGraphRowGlyphsMatchTopology(t *testing.T) {
	r := testRenderer(t, tideui.CatppuccinMocha)
	base := r.Styles.Item.UnsetPadding().UnsetWidth()
	render := func(row git.GraphRow, head bool) string {
		return ansi.Strip(renderGraphRow(r, row, head, false, graphWidth(row.Width()), base))
	}
	g := glyphSet(false)
	cases := []struct {
		name string
		row  git.GraphRow
		want []string
	}{
		{"plain commit", git.GraphRow{Lane: 0, Glyphs: []git.GraphGlyph{git.GraphNode}}, []string{g.node}},
		{"root commit", git.GraphRow{Lane: 0, Root: true, Glyphs: []git.GraphGlyph{git.GraphNode}}, []string{g.rootNode}},
		{"merge commit", git.GraphRow{Lane: 0, Merge: true,
			Glyphs: []git.GraphGlyph{git.GraphNode, git.GraphForkRight}}, []string{g.mergeNode, g.forkRight}},
		{"lane passing through", git.GraphRow{Lane: 0,
			Glyphs: []git.GraphGlyph{git.GraphNode, git.GraphVertical}}, []string{g.node, g.vertical}},
		{"lane joining from the right", git.GraphRow{Lane: 0,
			Glyphs: []git.GraphGlyph{git.GraphNode, git.GraphMergeRight}}, []string{g.node, g.mergeRight}},
		{"crossed lane", git.GraphRow{Lane: 0, Merge: true,
			Glyphs: []git.GraphGlyph{git.GraphNode, git.GraphCross, git.GraphForkRight}},
			[]string{g.mergeNode, g.cross, g.forkRight}},
	}
	for _, tc := range cases {
		got := render(tc.row, false)
		for _, want := range tc.want {
			if !strings.Contains(got, want) {
				t.Fatalf("%s: %q missing %q", tc.name, got, want)
			}
		}
	}
	// HEAD is marked by its own glyph, not only by colour.
	head := render(git.GraphRow{Lane: 0, Glyphs: []git.GraphGlyph{git.GraphNode}}, true)
	if !strings.Contains(head, g.headNode) {
		t.Fatalf("HEAD node: %q", head)
	}
	// The four node shapes have to stay distinguishable from one another.
	for _, pair := range [][2]string{{g.node, g.mergeNode}, {g.node, g.rootNode},
		{g.node, g.headNode}, {g.mergeNode, g.rootNode}} {
		if pair[0] == pair[1] {
			t.Fatalf("two node shapes are the same glyph: %q", pair[0])
		}
	}
}

func TestGraphRowUsesASCIIForPlainThemes(t *testing.T) {
	r := testRenderer(t, tideui.VT52)
	base := r.Styles.Item.UnsetPadding().UnsetWidth()
	row := git.GraphRow{Lane: 0, Merge: true,
		Glyphs: []git.GraphGlyph{git.GraphNode, git.GraphForkRight}}
	got := ansi.Strip(renderGraphRow(r, row, false, false, graphWidth(2), base))
	if strings.ContainsAny(got, glyphSet(false).all()) {
		t.Fatalf("plain theme used box drawing: %q", got)
	}
	if !strings.Contains(got, "%") || !strings.Contains(got, "\\") {
		t.Fatalf("plain merge glyphs missing: %q", got)
	}
}

// The selection background is close to the dimmed line colour, so a selected
// row has to brighten its graph rather than let it disappear.
func TestGraphStaysVisibleUnderSelection(t *testing.T) {
	r := testRenderer(t, tideui.CatppuccinMocha)
	row := git.GraphRow{Lane: 0, Glyphs: []git.GraphGlyph{git.GraphNode, git.GraphVertical}}
	selected := renderGraphRow(r, row, false, true,
		graphWidth(2), r.Styles.ItemSelected.UnsetPadding().UnsetWidth())
	if !strings.Contains(ansi.Strip(selected), glyphSet(false).vertical) {
		t.Fatal("selected row lost its lane line")
	}
	if strings.Contains(selected, colorCode(r.Styles.Theme.Dimmed)) {
		t.Fatal("selected row still draws lanes in the dimmed colour")
	}
	unselected := renderGraphRow(r, row, false, false,
		graphWidth(2), r.Styles.Item.UnsetPadding().UnsetWidth())
	if !strings.Contains(unselected, colorCode(r.Styles.Theme.Dimmed)) {
		t.Fatal("unselected lanes are no longer quiet")
	}
}

// colorCode renders a theme colour as the SGR foreground sequence lipgloss
// emits for it, so a test can assert which colour was used.
func colorCode(c lipgloss.Color) string {
	rendered := lipgloss.NewStyle().Foreground(c).Render("x")
	code, _, _ := strings.Cut(strings.TrimPrefix(rendered, "\x1b["), "m")
	return code
}

func TestGraphWidthIsBoundedAndStable(t *testing.T) {
	if graphWidth(1) >= graphWidth(2) {
		t.Fatal("graph width does not grow with lanes")
	}
	// A pathological history must not push the subject off the row.
	if graphWidth(50) != graphWidth(graphLaneLimit) {
		t.Fatalf("graph width unbounded: %d", graphWidth(50))
	}
	r := testRenderer(t, tideui.CatppuccinMocha)
	base := r.Styles.Item.UnsetPadding().UnsetWidth()
	glyphs := make([]git.GraphGlyph, 20)
	for i := range glyphs {
		glyphs[i] = git.GraphVertical
	}
	glyphs[0] = git.GraphNode
	out := renderGraphRow(r, git.GraphRow{Lane: 0, Glyphs: glyphs}, false, false, graphWidth(20), base)
	if w := lipgloss.Width(out); w != graphWidth(graphLaneLimit) {
		t.Fatalf("overflowing graph rendered %d cells", w)
	}
	if !strings.Contains(ansi.Strip(out), glyphSet(false).overflow) {
		t.Fatal("hidden lanes are not marked")
	}
}

func TestRefBadgesAreDistinguishableWithoutColour(t *testing.T) {
	r := testRenderer(t, tideui.CatppuccinMocha)
	refs := []git.Ref{
		{Kind: git.RefTag, Name: "v1.0"},
		{Kind: git.RefRemote, Name: "origin/main"},
		{Kind: git.RefLocal, Name: "main", Head: true},
		{Kind: git.RefLocal, Name: "topic"},
	}
	out := ansi.Strip(refBadges(r, refs, 80))
	// HEAD first, then plain locals, then remotes, then tags.
	wantOrder := []string{"@ main", "topic", "origin/main", "# v1.0"}
	at := -1
	for _, want := range wantOrder {
		i := strings.Index(out, want)
		if i < 0 {
			t.Fatalf("badge %q missing from %q", want, out)
		}
		if i < at {
			t.Fatalf("badges out of order in %q", out)
		}
		at = i
	}
	// Every kind carries a text signal, so colour is never the only cue.
	if strings.Count(out, "@") != 1 || strings.Count(out, "#") != 1 {
		t.Fatalf("kind sigils missing: %q", out)
	}
}

func TestRefBadgesDegradeWithWidth(t *testing.T) {
	r := testRenderer(t, tideui.CatppuccinMocha)
	refs := []git.Ref{
		{Kind: git.RefLocal, Name: "main", Head: true},
		{Kind: git.RefRemote, Name: "origin/main"},
		{Kind: git.RefTag, Name: "v1.0"},
	}
	full := ansi.Strip(refBadges(r, refs, 60))
	if !strings.Contains(full, "# v1.0") {
		t.Fatalf("wide badges: %q", full)
	}
	// A narrow row keeps the most important ref and counts the rest.
	narrow := ansi.Strip(refBadges(r, refs, 12))
	if !strings.Contains(narrow, "main") {
		t.Fatalf("narrow badges dropped HEAD: %q", narrow)
	}
	if !strings.Contains(narrow, "+2") {
		t.Fatalf("hidden badges not counted: %q", narrow)
	}
	if lipgloss.Width(refBadges(r, refs, 12)) > 14 {
		t.Fatalf("narrow badges overflowed: %q", narrow)
	}
	// A ref too long for any room is shortened rather than dropped entirely.
	long := []git.Ref{{Kind: git.RefLocal, Name: "feature/a-very-long-branch-name"}}
	tight := ansi.Strip(refBadges(r, long, 10))
	if tight == "" {
		t.Fatal("a long ref was dropped instead of shortened")
	}
	if lipgloss.Width(refBadges(r, long, 10)) > 10 {
		t.Fatalf("shortened badge overflowed: %q", tight)
	}
	if refBadges(r, refs, 0) != "" || refBadges(r, nil, 40) != "" {
		t.Fatal("badges rendered with nothing to show")
	}
}

func TestRelativeTimeAndInitials(t *testing.T) {
	now := timeAt(t, "2026-09-13T12:00:00Z")
	cases := map[string]string{
		"2026-09-13T11:59:30Z": "30s",
		"2026-09-13T11:30:00Z": "30m",
		"2026-09-13T06:00:00Z": "6h",
		"2026-09-10T12:00:00Z": "3d",
		"2026-08-20T12:00:00Z": "3w",
		"2024-08-20T12:00:00Z": "2024-08",
	}
	for stamp, want := range cases {
		if got := relativeTime(timeAt(t, stamp), now); got != want {
			t.Fatalf("relativeTime(%s) = %q, want %q", stamp, got, want)
		}
	}
	// Ages must stay short enough for a fixed column.
	for stamp := range cases {
		if len(relativeTime(timeAt(t, stamp), now)) > 7 {
			t.Fatalf("age column too wide for %s", stamp)
		}
	}
	for name, want := range map[string]string{
		"Allison Bayless": "AB", "Rowan": "RO", "a": "A",
		"Mary Jane Watson": "MW", "": "··",
	} {
		if got := authorInitials(name); got != want {
			t.Fatalf("authorInitials(%q) = %q, want %q", name, got, want)
		}
	}
}

// timeAt parses an RFC 3339 stamp for the age tests.
func timeAt(t *testing.T, stamp string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

// A commit with three or more parents reaches past lanes it has already
// opened, so those corners carry the run through instead of closing it.
func TestGraphOctopusRendersTees(t *testing.T) {
	r := testRenderer(t, tideui.CatppuccinMocha)
	base := r.Styles.Item.UnsetPadding().UnsetWidth()
	row := git.GraphRow{Lane: 0, Merge: true, Glyphs: []git.GraphGlyph{
		git.GraphNode, git.GraphForkTee, git.GraphForkTee, git.GraphForkRight,
	}}
	g := glyphSet(false)
	got := ansi.Strip(renderGraphRow(r, row, false, false, graphWidth(4), base))
	if !strings.Contains(got, g.forkTee) {
		t.Fatalf("no tee drawn for an octopus merge: %q", got)
	}
	if strings.Count(got, g.forkRight) != 1 {
		t.Fatalf("expected exactly one closing corner: %q", got)
	}
	// The run has to be continuous from the node to the last lane.
	if strings.Contains(got, g.forkRight+g.horizontal) {
		t.Fatalf("run continues past its closing corner: %q", got)
	}
	plain := ansi.Strip(renderGraphRow(testRenderer(t, tideui.VT52), row, false, false, graphWidth(4),
		testRenderer(t, tideui.VT52).Styles.Item.UnsetPadding().UnsetWidth()))
	if strings.ContainsAny(plain, g.forkTee+g.forkRight) {
		t.Fatalf("plain theme used box drawing: %q", plain)
	}
}

// laneColours turns a rendered row into the colour of each graph glyph.
func laneColours(rendered string) []string {
	sgr := regexp.MustCompile(`\x1b\[(?:1;)?38;2;(\d+);(\d+);(\d+)[^m]*m([^\x1b]*)`)
	var out []string
	for _, m := range sgr.FindAllStringSubmatch(rendered, -1) {
		if !strings.ContainsAny(m[4], glyphSet(false).all()) {
			continue
		}
		out = append(out, fmt.Sprintf("%s;%s;%s", m[1], m[2], m[3]))
	}
	return out
}

func TestLanePaletteIsDistinctAndReadable(t *testing.T) {
	for _, theme := range []tideui.Theme{tideui.CatppuccinMocha, tideui.CatppuccinLatte,
		tideui.Nord, tideui.Dracula, tideui.GruvboxLight} {
		palette := lanePalette(theme, theme.Bg)
		if len(palette) < 4 {
			t.Fatalf("%s produced %d lane colours", theme.Name, len(palette))
		}
		seen := map[lipgloss.Color]bool{}
		for i, c := range palette {
			if seen[c] {
				t.Errorf("%s lane colour %d repeats %s", theme.Name, i, c)
			}
			seen[c] = true
			if r := tideui.ContrastRatio(c, theme.Bg); r < laneMinContrast {
				t.Errorf("%s lane colour %d contrast %.2f against the page", theme.Name, i, r)
			}
		}
		// Neighbouring lanes are where two colours must be told apart.
		for i := 1; i < len(palette); i++ {
			h1, _, _ := tideui.Hue(palette[i-1])
			h2, _, _ := tideui.Hue(palette[i])
			if hueDistance(h1, h2) < 25 {
				t.Errorf("%s lanes %d and %d are only %.0f degrees apart",
					theme.Name, i-1, i, hueDistance(h1, h2))
			}
		}
	}
}

// A theme with no hue variety of its own must not be given one: that is the
// thing that makes it an amber or a green phosphor terminal.
func TestMonochromeThemesKeepTheirSingleColour(t *testing.T) {
	for _, theme := range []tideui.Theme{tideui.VT52, tideui.VT100} {
		if !monochromeTheme(theme) && !theme.UsesASCII() {
			t.Errorf("%s was not recognised as monochrome", theme.Name)
		}
		if palette := lanePalette(theme, theme.Bg); len(palette) != 0 {
			t.Errorf("%s was given %d lane colours", theme.Name, len(palette))
		}
	}
	for _, theme := range []tideui.Theme{tideui.CatppuccinMocha, tideui.Nord, tideui.TokyoNight} {
		if monochromeTheme(theme) {
			t.Errorf("%s was treated as monochrome", theme.Name)
		}
	}
	// A monochrome theme still draws the graph, and still separates the node
	// from the lines by brightness — but two different lines look the same,
	// because the theme has no second colour to tell them apart with.
	r := testRenderer(t, tideui.VT100)
	base := r.Styles.Item.UnsetPadding().UnsetWidth()
	row := git.GraphRow{Lane: 0, Tracks: []int{0, 1, 2},
		Glyphs: []git.GraphGlyph{git.GraphNode, git.GraphVertical, git.GraphVertical}}
	out := renderGraphRow(r, row, false, false, graphWidth(3), base)
	plainSet := glyphSet(false)
	if !strings.Contains(ansi.Strip(out), plainSet.node) ||
		!strings.Contains(ansi.Strip(out), plainSet.vertical) {
		t.Fatalf("monochrome graph lost its glyphs: %q", ansi.Strip(out))
	}
	colours := laneColours(out)
	if len(colours) < 3 {
		t.Fatalf("monochrome graph drew %d glyphs", len(colours))
	}
	if colours[1] != colours[2] {
		t.Fatalf("monochrome theme gave two lines different colours: %v", colours)
	}
	if colours[0] == colours[1] {
		t.Fatal("monochrome theme stopped separating the node from the lines")
	}
}

// A branch has to keep one colour for as long as it exists, whichever column
// it is in, or the colour says nothing.
func TestLaneColourFollowsTheLineNotTheColumn(t *testing.T) {
	r := testRenderer(t, tideui.CatppuccinMocha)
	base := r.Styles.Item.UnsetPadding().UnsetWidth()
	render := func(row git.GraphRow) []string {
		return laneColours(renderGraphRow(r, row, false, false, graphWidth(3), base))
	}
	// The same line (track 7) in two different columns keeps its colour.
	left := render(git.GraphRow{Lane: 0, Glyphs: []git.GraphGlyph{git.GraphNode}, Tracks: []int{7}})
	right := render(git.GraphRow{Lane: 2, Tracks: []int{-1, -1, 7},
		Glyphs: []git.GraphGlyph{git.GraphEmpty, git.GraphEmpty, git.GraphNode}})
	if len(left) == 0 || len(right) == 0 || left[0] != right[0] {
		t.Fatalf("a line changed colour when it changed column: %v vs %v", left, right)
	}
	// Two lines sharing a column at different times do not share a colour.
	other := render(git.GraphRow{Lane: 0, Glyphs: []git.GraphGlyph{git.GraphNode}, Tracks: []int{8}})
	if len(other) == 0 || other[0] == left[0] {
		t.Fatalf("two different lines in one column share a colour: %v", other)
	}
	// An untracked lane is never coloured as a line.
	none := render(git.GraphRow{Lane: 0, Glyphs: []git.GraphGlyph{git.GraphNode, git.GraphVertical},
		Tracks: []int{0, -1}})
	if len(none) < 2 || none[0] == none[1] {
		t.Fatalf("a lane with no line took a line colour: %v", none)
	}
}

// Selection must not hide topology, so the palette is rebuilt against the
// selection background rather than reused from the page.
func TestLaneColoursAreCorrectedForTheSelectedRow(t *testing.T) {
	r := testRenderer(t, tideui.CatppuccinMocha)
	selection := selectionSurface(r)
	if selection == r.Styles.Theme.Bg {
		t.Skip("this theme draws selection on the page background")
	}
	for i, c := range lanePalette(r.Styles.Theme, selection) {
		if got := tideui.ContrastRatio(c, selection); got < laneMinContrast {
			t.Errorf("selected lane colour %d contrast %.2f against the selection", i, got)
		}
	}
	base := r.Styles.ItemSelected.UnsetPadding().UnsetWidth()
	row := git.GraphRow{Lane: 0, Tracks: []int{0, 1, 2},
		Glyphs: []git.GraphGlyph{git.GraphNode, git.GraphVertical, git.GraphVertical}}
	out := renderGraphRow(r, row, false, true, graphWidth(3), base)
	if !strings.Contains(ansi.Strip(out), glyphSet(false).vertical) {
		t.Fatal("the selected row lost its lanes")
	}
	if colours := laneColours(out); len(colours) < 3 || colours[0] == colours[1] {
		t.Fatalf("the selected row lost its lane colours: %v", colours)
	}
}

// The palette belongs to the theme, so changing the theme changes the graph.
func TestLaneColoursComeFromTheTheme(t *testing.T) {
	mocha := lanePalette(tideui.CatppuccinMocha, tideui.CatppuccinMocha.Bg)
	gruvbox := lanePalette(tideui.GruvboxDark, tideui.GruvboxDark.Bg)
	if len(mocha) == 0 || len(gruvbox) == 0 {
		t.Fatal("a themed palette was empty")
	}
	if mocha[0] == gruvbox[0] {
		t.Fatal("two different themes produced the same first lane colour")
	}
	if mocha[0] != tideui.CatppuccinMocha.BorderFocus {
		t.Fatalf("the first lane is not the theme's own accent: %s", mocha[0])
	}
}
