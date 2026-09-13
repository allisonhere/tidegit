package ui

import (
	"context"
	"fmt"
	"html"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/allisonhere/tideui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/cellbuf"
	"github.com/muesli/termenv"
)

// Opt-in captures of actual View output, not hand-authored design mockups.
// TIDEGIT_CAPTURE_DIR=/tmp/tidegit-captures go test ./internal/ui -run TestCaptures
func TestCaptures(t *testing.T) {
	dir := os.Getenv("TIDEGIT_CAPTURE_DIR")
	if dir == "" {
		t.Skip("set TIDEGIT_CAPTURE_DIR for visual review artifacts")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	dirRepo := filepath.Join(t.TempDir(), "tidegit")
	if err := os.MkdirAll(filepath.Join(dirRepo, "internal", "git"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	uiGit(t, dirRepo, "init", "-b", "main")
	uiGit(t, dirRepo, "config", "user.name", "TideGit Test")
	uiGit(t, dirRepo, "config", "user.email", "test@example.invalid")
	source, err := os.ReadFile("../git/staging.go")
	if err != nil {
		t.Fatal(err)
	}
	baseline := strings.ReplaceAll(string(source), "\t\tif err := validPath(f.Path); err != nil {\n\t\t\treturn err\n\t\t}\n", "")
	uiWrite(t, dirRepo, "internal/git/staging.go", baseline)
	uiWrite(t, dirRepo, "README.md", "# TideGit\n")
	uiGit(t, dirRepo, "add", ".")
	uiGit(t, dirRepo, "commit", "-m", "Initial staging service")
	uiWrite(t, dirRepo, "internal/git/staging.go", string(source))
	uiWrite(t, dirRepo, "README.md", "# TideGit\n\nStage exactly what you mean.\n")
	uiWrite(t, dirRepo, "notes.md", "A new idea\n")
	m := New(context.Background(), dirRepo, tideui.CatppuccinMocha)
	drain(t, m, m.Init())
	m.selected = 1
	drain(t, m, m.loadDiff())
	m.width = 132
	m.height = 34
	m.focus = 2
	key(t, m, "S")
	capture(t, dir, "status-dark", m)
	key(t, m, "c")
	if m.compose == nil {
		t.Fatal(m.notice)
	}
	m.compose.editor.SetValue("Validate staging paths before updating the index\n\nKeep operations scoped to the selected file.\nReject paths outside the repository before asking\nGit to update the index.\n\n# Subject length is guidance, not a hard limit.\n")
	m.sizeEditor()
	capture(t, dir, "commit-dark", m)
	m.theme = tideui.CatppuccinLatte
	capture(t, dir, "commit-light", m)
	m.compose.amend = true
	capture(t, dir, "amend-light", m)
	m.compose.amend = false
	m.theme = tideui.CatppuccinMocha
	m.compose.errorText = "commit was not completed: git commit --file=- --cleanup=strip\ncommit-msg: Subject must include a ticket reference.\nYour message and staged content are unchanged."
	capture(t, dir, "hook-error", m)
	m.compose.showOutput = true
	capture(t, dir, "hook-output", m)
	m.compose.showOutput = false
	m.compose.errorText = ""
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	capture(t, dir, "commit-narrow", m)
	m.compose = nil
	m.width = 132
	m.height = 34
	m.theme = tideui.CatppuccinLatte
	capture(t, dir, "status-light", m)
	captureHistoryAndBranches(t, dir)
}

// captureHistoryAndBranches renders the Milestone 4 screens over a repository
// with a merge, a diverged branch, tags and remote-tracking refs.
func captureHistoryAndBranches(t *testing.T, dir string) {
	t.Helper()
	m := historyModel(t)
	m.width, m.height = 132, 34
	drain(t, m, m.openHistory())
	capture(t, dir, "history-dark", m)

	m.theme = tideui.CatppuccinLatte
	capture(t, dir, "history-light", m)
	m.theme = tideui.CatppuccinMocha

	// A commit further down, with the inspector focused on a changed file.
	for i := 0; i < 3; i++ {
		drain(t, m, m.moveCommit(1))
	}
	m.focus = 2
	capture(t, dir, "history-inspector", m)
	drain(t, m, m.loadCommitFileDiff())
	capture(t, dir, "history-commit-diff", m)
	m.history.showDiff = false

	drain(t, m, m.openPalette())
	capture(t, dir, "palette", m)
	m.palette = nil

	drain(t, m, m.goToScreen(screenBranches))
	capture(t, dir, "branches-dark", m)
	m.theme = tideui.CatppuccinLatte
	capture(t, dir, "branches-light", m)
	m.theme = tideui.CatppuccinMocha

	// The delete confirmation for an unmerged branch is the loudest thing the
	// screen can say, so it gets its own capture.
	m.branches.selectByName("wip/unmerged-work")
	drain(t, m, m.loadBranchDetail())
	drain(t, m, m.confirmDeleteBranch())
	capture(t, dir, "branch-delete-confirm", m)
	m.confirm = nil

	drain(t, m, m.promptNewBranch())
	m.prompt.value = "feature/graph-lanes"
	capture(t, dir, "branch-create-prompt", m)
	m.prompt = nil

	// Detached HEAD, shown on History where the header is most visible.
	uiGit(t, m.repo.Root, "switch", "--detach", "HEAD~3")
	drain(t, m, m.goToScreen(screenHistory))
	drain(t, m, m.refreshScreen())
	capture(t, dir, "history-detached", m)
	uiGit(t, m.repo.Root, "switch", "main")

	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	drain(t, m, m.refreshScreen())
	capture(t, dir, "history-narrow", m)
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	capture(t, dir, "history-very-narrow", m)
	m.theme = tideui.VT52
	capture(t, dir, "history-plain", m)
}

func capture(t *testing.T, dir, name string, m *Model) {
	t.Helper()
	view := m.View()
	if err := os.WriteFile(filepath.Join(dir, name+".ansi"), []byte(view), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".txt"), []byte(ansi.Strip(view)), 0644); err != nil {
		t.Fatal(err)
	}
	buf := cellbuf.NewBuffer(m.width, m.height)
	cellbuf.SetContent(buf, view)
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d"><rect width="100%%" height="100%%" fill="%s"/>`, m.width*10+32, m.height*20+32, m.theme.Bg)
	colorHex := func(c color.Color, fallback string) string {
		if c == nil {
			return fallback
		}
		r, g, b, _ := c.RGBA()
		return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
	}
	for y := 0; y < m.height; y++ {
		for x := 0; x < m.width; x++ {
			c := buf.Cell(x, y)
			if c == nil {
				continue
			}
			fg := colorHex(c.Style.Fg, string(m.theme.Fg))
			bg := colorHex(c.Style.Bg, string(m.theme.Bg))
			if c.Style.Attrs.Contains(cellbuf.ReverseAttr) {
				fg, bg = bg, fg
			}
			fmt.Fprintf(&b, `<rect x="%d" y="%d" width="%d" height="20" fill="%s"/>`, 16+x*10, 16+y*20, max(1, c.Width)*10, bg)
			if c.Rune != 0 && c.Rune != ' ' {
				weight := "normal"
				if c.Style.Attrs.Contains(cellbuf.BoldAttr) {
					weight = "bold"
				}
				fmt.Fprintf(&b, `<text x="%d" y="%d" fill="%s" font-family="DejaVu Sans Mono,monospace" font-size="15" font-weight="%s">%s</text>`, 16+x*10, 31+y*20, fg, weight, html.EscapeString(c.String()))
			}
		}
	}
	b.WriteString("</svg>")
	if err := os.WriteFile(filepath.Join(dir, name+".svg"), []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}
}
