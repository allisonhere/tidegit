package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/allisonhere/tideui"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Semantic presentation is derived entirely from TideUI's theme and styles.
func (m *Model) renderer() tideui.Renderer {
	return tideui.NewRenderer(m.activeTheme(), tideui.StyleOptions{Density: tideui.Compact, PaneCorners: tideui.RoundCorners, ModalShadow: true})
}
func clip(s string, w int) string { return ansi.Truncate(s, max(0, w), "") }

// padLine fills a line to width with style. It goes through StyleOver because
// the header and hint bars are built by concatenating styled segments with
// plain separators: a nested reset would otherwise clear the background and
// leave the separators showing the terminal's own colour.
func padLine(s string, w int, style lipgloss.Style) string {
	return tideui.StyleOver(style.Width(max(0, w)), clip(s, w))
}
func muted(r tideui.Renderer, s string) string { return r.Styles.DetailMeta.Italic(false).Render(s) }
func accent(r tideui.Renderer, s string) string {
	return r.Styles.DetailBody.Foreground(r.Styles.Theme.BorderFocus).Bold(true).Render(s)
}
func inset(text string, w int) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = " " + clip(line, max(0, w-2)) + " "
	}
	return strings.Join(lines, "\n")
}

// detached reports whether HEAD points straight at a commit. Either source is
// authoritative: the Status scan reports it through porcelain, and the History
// and Branches screens resolve HEAD directly.
func (m *Model) detached() bool {
	return m.head.Detached || m.status.Branch == "(detached)"
}

// headShort is the abbreviated commit HEAD points at.
func (m *Model) headShort() string {
	if m.head.Short != "" {
		return m.head.Short
	}
	if len(m.status.OID) >= 7 {
		return m.status.OID[:7]
	}
	return m.status.OID
}

func (m *Model) branchLabel() string {
	if m.detached() {
		return "detached at " + m.headShort()
	}
	if m.head.Branch != "" {
		return safeText(m.head.Branch)
	}
	if m.status.Branch == "" {
		return "opening repository"
	}
	return safeText(m.status.Branch)
}

// headLabel renders what HEAD points at for the global header. A detached HEAD
// says so in words rather than being dressed up as a branch.
func (m *Model) headLabel(r tideui.Renderer) string {
	if m.detached() {
		badge := lipgloss.NewStyle().Background(r.Styles.Theme.Error).
			Foreground(r.Styles.Theme.Bg).Bold(true).Padding(0, 1).Render("DETACHED HEAD")
		return badge + muted(r, " at ") + accent(r, m.headShort())
	}
	return accent(r, m.branchLabel())
}
func (m *Model) globalHeader(r tideui.Renderer, mode string) string {
	name := filepath.Base(m.repo.Root)
	if m.repo.Root == "" {
		name = filepath.Base(m.path)
	}
	brand := r.Styles.DetailTitle.Render("tidegit")
	left := brand + "  " + r.Styles.DetailBody.Bold(true).Render(safeText(name)) + muted(r, "  /  ") + m.headLabel(r)
	right := muted(r, mode)
	if m.width >= 100 {
		right = muted(r, fmt.Sprintf("%d staged  ·  %d unstaged  ·  %d new   ", len(m.status.Groups[0]), len(m.status.Groups[1]), len(m.status.Groups[2]))) + right
	}
	gap := max(1, m.width-2-lipgloss.Width(left)-lipgloss.Width(right))
	line := padLine(" "+left+strings.Repeat(" ", gap)+right, m.width, r.Styles.DetailBody)
	separator := "─"
	if m.theme.UsesASCII() {
		separator = "-"
	}
	return line + "\n" + r.Styles.DetailMeta.Italic(false).Render(strings.Repeat(separator, max(0, m.width)))
}
func (m *Model) hintBar(r tideui.Renderer, hints ...tideui.SoftHint) string {
	var parts []string
	for _, h := range hints {
		parts = append(parts, accent(r, h.Key)+" "+muted(r, h.Label))
	}
	return padLine(" "+strings.Join(parts, "   "), m.width, r.Styles.DetailBody)
}
func (m *Model) minimumView(r tideui.Renderer) string {
	text := "TideGit\nResize to at least 54 x 16\nYour draft and selection are preserved."
	lines := strings.Split(text, "\n")
	lines = lines[:min(len(lines), max(0, m.height))]
	for i, s := range lines {
		lines[i] = padLine(s, m.width, r.Styles.DetailBody)
	}
	return strings.Join(lines, "\n")
}

// plural keeps counted nouns calm: "1 file", not "1 files".
func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func (m *Model) activity() string {
	frames := []string{"◐", "◓", "◑", "◒"}
	if m.theme.UsesASCII() {
		frames = []string{"|", "/", "-", "\\"}
	}
	return frames[m.frame%len(frames)]
}

func (m *Model) diffViewportHeight() int {
	if m.err != "" {
		return max(1, m.height-8)
	}
	return max(1, m.height-11)
}

// helpPanel shows only the bindings that act on the current screen, plus the
// few that work everywhere.
func (m *Model) helpPanel(r tideui.Renderer) *tideui.Overlay {
	var sections []struct{ title, body string }
	switch m.screen {
	case screenHistory:
		sections = []struct{ title, body string }{
			{"HISTORY", "j / k  move through commits    [ Enter ]  inspect\n/  filter subjects            y  copy full hash\nn  branch from this commit"},
			{"INSPECTOR", "Tab  reach the inspector      j / k  choose a file\nEnter  open that file's patch  Esc  back to the commit"},
			{"GRAPH", "●  commit   ◆  merge   ○  first commit   ◉  HEAD\nEach branch keeps its own colour down the page.\n@ branch   # tag   origin/… remote"},
		}
	case screenBranches:
		sections = []struct{ title, body string }{
			{"BRANCHES", "j / k  move            Enter / s  switch to branch\nn  new branch          R  rename        D  delete\n/  filter the list    y  copy target hash"},
			{"READING IT", "@  current branch      ↑ n  ahead     ↓ n  behind\n✓  up to date         —  no upstream   gone  upstream lost\nRemote-tracking branches are read-only here."},
		}
	default:
		sections = []struct{ title, body string }{
			{"STAGING", "s / u  stage / unstage FILE\nS / U  stage / unstage HUNK\n[ / ]  choose hunk       /  filter files"},
			{"COMMIT", "c  compose commit         A  amend HEAD\nUnstage preserves working-tree content."},
		}
	}
	sections = append(sections, struct{ title, body string }{
		"EVERYWHERE", "1  status   2  history   3  branches   Ctrl-P  commands\nTab  panes    z  expand    r  refresh    t  theme\nq  back / quit    ? / Esc  close help"})
	var rows []string
	for _, s := range sections {
		rows = append(rows, accent(r, s.title), muted(r, s.body), "")
	}
	content := strings.Join(rows, "\n")
	if m.height < 25 {
		content = accent(r, strings.ToUpper(screenNames[m.screen])) + "\n" +
			muted(r, compactHelp(m.screen)) + "\n\n" + accent(r, "EVERYWHERE") + "\n" +
			muted(r, "1/2/3 screens   Ctrl-P commands   Tab panes\nr refresh   t theme   z expand   q back   ? close")
	}
	o := r.SoftPanelOverlay(tideui.SoftPanel{Prefix: "tidegit", Title: "keyboard guide",
		Width: min(62, m.width-6), Content: inset(content, min(62, m.width-6))})
	return &o
}

// compactHelp is the short form used when the terminal is too short for the
// full guide.
func compactHelp(s screen) string {
	switch s {
	case screenHistory:
		return "j/k commits   Enter inspect   / search\nn branch here   y copy hash"
	case screenBranches:
		return "j/k branches   Enter switch   n new\nR rename   D delete   / filter"
	default:
		return "s/u file   S/U hunk   [/] choose hunk\nc commit   A amend   / filter"
	}
}
