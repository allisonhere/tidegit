package ui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/allisonhere/tideui"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// An ANSI reset clears the background of whatever style wraps it, and lipgloss
// does not re-open it. Every Tide app hit this by composing styled segments
// with plain separators and then filling the line: the separators, and any
// padding after them, rendered on the terminal's own background instead of the
// theme's. TideUI's StyleOver is the fix; these tests make sure no screen
// regresses back to a hole.
var backgroundHole = regexp.MustCompile(`\x1b\[0?m {2,}`)

// unbackedCells replays a rendered view and reports cells drawn with no
// background at all, which is what the bug looks like on screen.
func unbackedCells(view string) int {
	sgr := regexp.MustCompile(`\x1b\[([0-9;]*)m`)
	holes := 0
	for _, line := range strings.Split(view, "\n") {
		background := false
		pos := 0
		count := func(text string) {
			if !background {
				holes += len([]rune(text))
			}
		}
		for _, m := range sgr.FindAllStringSubmatchIndex(line, -1) {
			count(line[pos:m[0]])
			params := line[m[2]:m[3]]
			if params == "" {
				params = "0"
			}
			for _, p := range strings.Split(params, ";") {
				switch {
				case p == "0" || p == "":
					background = false
				case p == "48", p == "49":
					background = true
				case len(p) == 2 && p[0] == '4' && p[1] <= '7':
					background = true
				case len(p) == 3 && strings.HasPrefix(p, "10") && p[2] <= '7':
					background = true
				}
			}
			pos = m[1]
		}
		count(line[pos:])
	}
	return holes
}

func trueColorModel(t *testing.T) {
	t.Helper()
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
}

func TestNoBackgroundHolesOnAnyScreen(t *testing.T) {
	trueColorModel(t)
	m := historyModel(t)

	screens := map[string]func(){
		"status":    func() { drain(t, m, m.goToScreen(screenStatus)) },
		"history":   func() { drain(t, m, m.goToScreen(screenHistory)) },
		"branches":  func() { drain(t, m, m.goToScreen(screenBranches)) },
		"stash":     func() { drain(t, m, m.goToScreen(screenStash)) },
		"remotes":   func() { drain(t, m, m.goToScreen(screenRemotes)) },
		"conflicts": func() { drain(t, m, m.goToScreen(screenConflicts)) },
		"reflog":    func() { drain(t, m, m.goToScreen(screenReflog)) },
		"settings":  func() { drain(t, m, m.goToScreen(screenSettings)) },
	}
	for _, theme := range []tideui.Theme{tideui.CatppuccinMocha, tideui.CatppuccinLatte, tideui.Nord} {
		m.theme = theme
		for name, open := range screens {
			open()
			for _, size := range [][2]int{{132, 34}, {80, 24}, {60, 20}} {
				m.width, m.height = size[0], size[1]
				view := m.View()
				for i, line := range strings.Split(view, "\n") {
					if backgroundHole.MatchString(line) {
						t.Fatalf("%s/%s at %v line %d: %q", theme.Name, name, size, i, line)
					}
				}
				if holes := unbackedCells(view); holes > 0 {
					t.Fatalf("%s/%s at %v has %d cells with no background",
						theme.Name, name, size, holes)
				}
			}
		}
	}
}

func TestNoBackgroundHolesInOverlays(t *testing.T) {
	trueColorModel(t)
	m := historyModel(t)
	m.width, m.height = 132, 34
	drain(t, m, m.goToScreen(screenBranches))

	overlays := map[string]func(){
		"palette": func() { drain(t, m, m.openPalette()) },
		"prompt":  func() { drain(t, m, m.promptNewBranch()) },
		"confirm": func() {
			m.branches.selectByName("wip/unmerged-work")
			drain(t, m, m.loadBranchDetail())
			drain(t, m, m.confirmDeleteBranch())
		},
		"choice": func() {
			m.choice = &choiceState{title: "fetch", label: "Choose a remote to fetch",
				options: []choiceOption{{label: "origin", value: "origin", hint: "default"}}}
		},
		"operation": func() {
			m.op = &operationState{kind: "fetch", verb: "Fetching", title: "Fetch origin",
				target: "origin", done: true, ok: false, summary: "Authentication failed", raw: &progressBuffer{}}
			m.op.show = true
		},
		"help": func() { m.help = true },
	}
	for name, open := range overlays {
		m.palette, m.prompt, m.confirm, m.choice, m.op, m.help = nil, nil, nil, nil, nil, false
		open()
		view := m.View()
		for i, line := range strings.Split(view, "\n") {
			if backgroundHole.MatchString(line) {
				t.Fatalf("%s overlay line %d: %q", name, i, line)
			}
		}
		if holes := unbackedCells(view); holes > 0 {
			t.Fatalf("%s overlay has %d cells with no background", name, holes)
		}
	}
	m.palette, m.prompt, m.confirm, m.choice, m.op, m.help = nil, nil, nil, nil, nil, false
}

// The commit screen composes its own chrome outside the pane system, so it is
// checked separately from the three-pane screens.
func TestNoBackgroundHolesWhileComposing(t *testing.T) {
	trueColorModel(t)
	m := composingModel(t)
	m.compose.editor.SetValue("Subject line\n\nBody with a paragraph.\n# comment\n")
	m.sizeEditor()
	for _, size := range [][2]int{{132, 34}, {100, 28}, {80, 24}} {
		m.width, m.height = size[0], size[1]
		m.sizeEditor()
		view := m.View()
		for i, line := range strings.Split(view, "\n") {
			if backgroundHole.MatchString(line) {
				t.Fatalf("commit screen at %v line %d: %q", size, i, line)
			}
		}
	}
}

// padLine is the choke point for the header and hint bars; a plain separator
// between two styled segments is the exact shape that used to bleed.
func TestPadLineKeepsSeparatorsFilled(t *testing.T) {
	trueColorModel(t)
	r := testRenderer(t, tideui.CatppuccinMocha)
	composed := accent(r, "Enter") + " " + muted(r, "inspect") + "   " + accent(r, "/") + " " + muted(r, "search")
	got := padLine(" "+composed, 60, r.Styles.DetailBody)
	if backgroundHole.MatchString(got) {
		t.Fatalf("padLine left a hole: %q", got)
	}
	if unbackedCells(got) > 0 {
		t.Fatalf("padLine left %d unbacked cells: %q", unbackedCells(got), got)
	}
	if lipgloss.Width(got) != 60 {
		t.Fatalf("padLine changed width to %d", lipgloss.Width(got))
	}
}
