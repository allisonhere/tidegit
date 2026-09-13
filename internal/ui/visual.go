package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/allisonhere/tidegit/internal/git"
	"github.com/allisonhere/tideui"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Semantic presentation is derived entirely from TideUI's theme and styles.
func (m *Model) renderer() tideui.Renderer {
	return tideui.NewRenderer(m.activeTheme(), m.styleOptions())
}

// styleOptions maps appearance settings onto TideUI's own options.
func (m *Model) styleOptions() tideui.StyleOptions {
	density := tideui.Comfortable
	corners := tideui.RoundCorners
	if m.cfg == nil || m.cfg.Appearance.Compact {
		density = tideui.Compact
	}
	if m.cfg != nil && m.cfg.Appearance.BorderStyle == "square" {
		corners = tideui.SquareCorners
	}
	return tideui.StyleOptions{Density: density, PaneCorners: corners, ModalShadow: true}
}

// icons reports whether Unicode glyphs may be used.
func (m *Model) icons() bool { return m.cfg == nil || m.cfg.Appearance.Icons }

// animations reports whether the activity indicator may animate.
func (m *Model) animations() bool { return m.cfg == nil || m.cfg.Appearance.Animations }

// diffContext is the configured unified context size.
func (m *Model) diffContext() int {
	if m.cfg == nil {
		return 3
	}
	return m.cfg.Diff.ContextLines
}

// showLineNumbers reports whether the diff gutter shows line numbers.
func (m *Model) showLineNumbers() bool { return m.cfg == nil || m.cfg.Diff.LineNumbers }
func clip(s string, w int) string      { return ansi.Truncate(s, max(0, w), "") }

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
	// A running operation shows as a compact chip in the global header, so
	// network work is visible from any screen without taking it over.
	if op := m.op; op != nil && op.running {
		title := op.title
		if m.cfg != nil && m.cfg.Remote.VerboseProgress {
			if latest, _ := op.raw.snapshot(); latest != "" {
				title = clip(safeText(latest), max(8, m.width/3))
			}
		}
		chip := accent(r, m.activity()+" "+safeText(title))
		right = chip + muted(r, "   ") + right
	}
	gap := max(1, m.width-2-lipgloss.Width(left)-lipgloss.Width(right))
	line := padLine(" "+left+strings.Repeat(" ", gap)+right, m.width, r.Styles.DetailBody)
	// A paused merge, rebase, cherry-pick or revert replaces the thin rule with
	// a deliberate ribbon, so the repository's state is the first thing read
	// without turning the whole screen into an alarm.
	if ribbon, ok := m.operationRibbon(r); ok {
		return line + "\n" + ribbon
	}
	separator := "─"
	if m.theme.UsesASCII() || !m.icons() {
		separator = "-"
	}
	return line + "\n" + r.Styles.DetailMeta.Italic(false).Render(strings.Repeat(separator, max(0, m.width)))
}

// operationRibbon renders the global operation state as one full-width line.
// It names the operation and its progress, states whether conflicts remain,
// and points at the key that opens the Conflicts screen.
func (m *Model) operationRibbon(r tideui.Renderer) (string, bool) {
	state := m.repoState
	if !state.InProgress() {
		return "", false
	}
	label := state.Operation.Banner()
	if state.Operation == git.OpRebase && state.Total > 0 {
		label = fmt.Sprintf("REBASE %d / %d", state.Step, state.Total)
	}
	note := "ready to continue"
	right := "6 continue"
	if state.Conflicts > 0 {
		note = plural(state.Conflicts, "unresolved conflict")
		right = "6 resolve conflicts"
	}
	if state.Operation == git.OpBisect {
		right = "6 inspect"
	}
	left := " " + label + "  ·  " + note
	color := r.Styles.Theme.Unread
	if state.Conflicts == 0 {
		color = r.Styles.Theme.BorderFocus
	}
	style := r.Styles.DetailBody.Foreground(color).Bold(true)
	gap := max(1, m.width-lipgloss.Width(left)-lipgloss.Width(right)-1)
	return padLine(left+strings.Repeat(" ", gap)+right, m.width, style), true
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
	if m.theme.UsesASCII() || !m.icons() {
		frames = []string{"|", "/", "-", "\\"}
	}
	if !m.animations() {
		return frames[0]
	}
	return frames[m.frame%len(frames)]
}

func (m *Model) diffViewportHeight() int {
	if m.err != "" {
		return max(1, m.height-8)
	}
	return max(1, m.height-11)
}
