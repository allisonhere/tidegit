package ui

import (
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
	cases := []struct {
		name string
		row  git.GraphRow
		want []string
	}{
		{"plain commit", git.GraphRow{Lane: 0, Glyphs: []git.GraphGlyph{git.GraphNode}}, []string{"●"}},
		{"root commit", git.GraphRow{Lane: 0, Root: true, Glyphs: []git.GraphGlyph{git.GraphNode}}, []string{"○"}},
		{"merge commit", git.GraphRow{Lane: 0, Merge: true,
			Glyphs: []git.GraphGlyph{git.GraphNode, git.GraphForkRight}}, []string{"◆", "╮"}},
		{"lane passing through", git.GraphRow{Lane: 0,
			Glyphs: []git.GraphGlyph{git.GraphNode, git.GraphVertical}}, []string{"●", "│"}},
		{"lane joining from the right", git.GraphRow{Lane: 0,
			Glyphs: []git.GraphGlyph{git.GraphNode, git.GraphMergeRight}}, []string{"●", "╯"}},
		{"crossed lane", git.GraphRow{Lane: 0, Merge: true,
			Glyphs: []git.GraphGlyph{git.GraphNode, git.GraphCross, git.GraphForkRight}}, []string{"◆", "┼", "╮"}},
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
	if !strings.Contains(head, "◉") {
		t.Fatalf("HEAD node: %q", head)
	}
}

func TestGraphRowUsesASCIIForPlainThemes(t *testing.T) {
	r := testRenderer(t, tideui.VT52)
	base := r.Styles.Item.UnsetPadding().UnsetWidth()
	row := git.GraphRow{Lane: 0, Merge: true,
		Glyphs: []git.GraphGlyph{git.GraphNode, git.GraphForkRight}}
	got := ansi.Strip(renderGraphRow(r, row, false, false, graphWidth(2), base))
	if strings.ContainsAny(got, "●◆○◉│╭╮╰╯┼─") {
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
	if !strings.Contains(ansi.Strip(selected), "│") {
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
	if !strings.Contains(ansi.Strip(out), "›") {
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
	got := ansi.Strip(renderGraphRow(r, row, false, false, graphWidth(4), base))
	if !strings.Contains(got, "┬") {
		t.Fatalf("no tee drawn for an octopus merge: %q", got)
	}
	if strings.Count(got, "╮") != 1 {
		t.Fatalf("expected exactly one closing corner: %q", got)
	}
	// The run has to be continuous from the node to the last lane.
	if strings.Contains(got, "╮─") {
		t.Fatalf("run continues past its closing corner: %q", got)
	}
	plain := ansi.Strip(renderGraphRow(testRenderer(t, tideui.VT52), row, false, false, graphWidth(4),
		testRenderer(t, tideui.VT52).Styles.Item.UnsetPadding().UnsetWidth()))
	if strings.ContainsAny(plain, "┬╮") {
		t.Fatalf("plain theme used box drawing: %q", plain)
	}
}
