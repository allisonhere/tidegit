package ui

import (
	"strings"

	"github.com/allisonhere/tideui"
	"github.com/charmbracelet/lipgloss"
)

// helpEntry is one binding: the key(s) in the left column and the action in the
// right. helpSection groups them under a heading, with an optional prose note
// for the few things that are not a binding (glyph legends, caveats).
type helpEntry struct {
	key, action string
}
type helpSection struct {
	title   string
	entries []helpEntry
	note    string
}

// contextualHelp is the guide for one screen, before the shared bindings are
// appended.
func contextualHelp(s screen) []helpSection {
	var sections []helpSection
	switch s {
	case screenHistory:
		sections = []helpSection{
			{title: "HISTORY", entries: []helpEntry{
				{"j / k", "move through commits"},
				{"Enter", "inspect the selected commit"},
				{"/", "filter subjects"},
				{"y", "copy full hash"},
				{"n", "branch from this commit"},
				{"Tab", "refs → commits → inspector"},
			}},
			{title: "INSPECTOR", entries: []helpEntry{
				{"j / k", "choose a changed file"},
				{"Enter", "open that file's patch"},
				{"Esc", "back to the commit"},
			}},
			{title: "GRAPH", note: "● commit   ◆ merge   ○ first commit   ◉ HEAD\n" +
				"@ branch   # tag   origin/… remote\n" +
				"each line of development keeps its own colour"},
		}
	case screenBranches:
		sections = []helpSection{
			{title: "BRANCHES", entries: []helpEntry{
				{"j / k", "move"},
				{"Enter / s", "switch to branch"},
				{"n", "new branch"},
				{"R", "rename"},
				{"D", "delete"},
				{"/", "filter the list"},
				{"y", "copy target hash"},
			}, note: "↑ n ahead   ↓ n behind   ✓ up to date\n" +
				"— no upstream   gone upstream lost\n" +
				"remote-tracking branches are read-only here"},
		}
	case screenStash:
		sections = []helpSection{
			{title: "STASHES", entries: []helpEntry{
				{"j / k", "move through stashes"},
				{"a", "apply the selected stash"},
				{"p", "pop the selected stash"},
				{"d", "drop the selected stash (confirms)"},
				{"n", "stash tracked changes"},
				{"N", "stash including untracked"},
				{"/", "filter stashes"},
				{"Enter", "file list → patch"},
			}, note: "apply keeps the entry; pop removes it only after success"},
		}
	case screenRemotes:
		sections = []helpSection{
			{title: "REMOTES", entries: []helpEntry{
				{"j / k", "choose a remote; move within a pane"},
				{"f", "fetch the selected remote"},
				{"F", "fetch all remotes"},
				{"p / P", "pull / push the current branch"},
				{"/", "filter remotes"},
				{"Enter", "tracked branches → inspector"},
			}, note: "◆ default remote · URLs are secondary metadata\n" +
				"Git handles authentication; TideGit stores nothing"},
		}
	case screenConflicts:
		sections = []helpSection{
			{title: "CONFLICTS", entries: []helpEntry{
				{"j / k", "move through unmerged files"},
				{"[ / ]", "previous / next conflict region"},
				{"v", "cycle the inspector view"},
				{"o / t", "write ours / theirs to the file"},
				{"O / T", "write and mark resolved"},
				{"b", "keep both sides"},
				{"m", "mark resolved"},
				{"e", "open in $EDITOR"},
				{"/", "filter files"},
			}, note: "ours is HEAD; during a rebase the sides swap meaning,\nso the labels follow the operation"},
			{title: "OPERATION", entries: []helpEntry{
				{"c", "continue"},
				{"x", "skip the current step"},
				{"A", "abort and restore (confirms)"},
			}, note: "Git performs its own checks; unresolved files block a continue"},
		}
	case screenReflog:
		sections = []helpSection{
			{title: "REFLOG", entries: []helpEntry{
				{"j / k", "move through the timeline"},
				{"Enter", "inspect; open a changed file's patch"},
				{"b", "create a recovery branch here"},
				{"R", "reset to this entry (confirms)"},
				{"s", "switch here, detached (confirms)"},
				{"y", "copy the full hash"},
				{"/", "filter entries"},
			}, note: "a recovery branch is the safest way to keep work\nreachable after a mistake"},
		}
	case screenSettings:
		sections = []helpSection{
			{title: "SETTINGS", entries: []helpEntry{
				{"Enter", "edit the selected setting"},
				{"← / →", "adjust a value"},
				{"x / X", "reset this setting / reset all"},
				{"/", "search settings"},
				{"E", "open config.toml in your editor"},
				{"L", "reload configuration"},
				{"V", "show the effective configuration"},
			}, note: "in-app changes are saved to overrides.toml;\nconfig.toml is never rewritten"},
		}
	default:
		sections = []helpSection{
			{title: "STAGING", entries: []helpEntry{
				{"space", "mark files for a batch"},
				{"s / u", "stage / unstage marked files, or the selected file"},
				{"S / U", "stage / unstage the selected hunk"},
				{"[ / ]", "previous / next hunk"},
				{"/", "filter files"},
			}},
			{title: "COMMIT", entries: []helpEntry{
				{"c", "compose a commit"},
				{"A", "amend HEAD"},
			}, note: "unstage preserves working-tree content"},
			{title: "DIFF", entries: []helpEntry{
				{"v", "toggle unified / split"},
				{"[ / ]", "previous / next hunk"},
				{"Enter", "expand / collapse a context gap"},
				{"Ctrl-F", "search the diff; n / N step matches"},
			}},
		}
	}
	return sections
}

func helpSectionsFor(s screen) []helpSection {
	sections := contextualHelp(s)
	sections = append(sections, helpSection{title: "EVERYWHERE", entries: []helpEntry{
		{"1 .. 8", "status · history · branches · stash · remotes · conflicts · reflog · settings"},
		{"Tab", "next pane"},
		{"z", "expand the focused pane"},
		{"r", "refresh"},
		{"t", "theme picker"},
		{"f / p / P", "fetch / pull / push"},
		{"Ctrl-P", "command palette"},
		{"?", "this guide"},
		{"q", "back / quit"},
	}})
	return sections
}

// helpPanel shows the bindings that act on the current screen as a key/action
// table, plus the few that work everywhere. Every cell is painted with the
// overlay's own surface so no pane-background block shows through the modal.
func (m *Model) helpPanel(r tideui.Renderer) *tideui.Overlay {
	width := min(68, m.width-6)
	inner := max(1, width-4)
	bodyStyle := r.Styles.OverlayBody
	hintStyle := r.Styles.OverlayHint
	headingStyle := bodyStyle.Foreground(r.Styles.Theme.BorderFocus).Bold(true)
	keyStyle := bodyStyle.Foreground(r.Styles.Theme.BorderFocus)

	sections := helpSectionsFor(m.screen)

	var rows []string
	if m.height >= 25 {
		// The key column is one width for every section, so the actions line up
		// down the whole guide.
		keyWidth := 0
		for _, s := range sections {
			for _, e := range s.entries {
				keyWidth = max(keyWidth, lipgloss.Width(e.key))
			}
		}
		keyWidth = min(keyWidth, 12) + 2
		for _, s := range sections {
			rows = append(rows, headingStyle.Render(s.title))
			for _, e := range s.entries {
				padded := e.key + strings.Repeat(" ", max(0, keyWidth-lipgloss.Width(e.key)))
				rows = append(rows, keyStyle.Render(padded)+hintStyle.Render(clip(e.action, inner-keyWidth)))
			}
			if s.note != "" {
				for _, line := range strings.Split(s.note, "\n") {
					rows = append(rows, hintStyle.Render(clip(line, inner)))
				}
			}
			rows = append(rows, "")
		}
	}

	// Fall back to the compact prose guide when the terminal cannot hold the
	// table, so nothing is ever clipped off the bottom of the modal.
	for len(rows) > 0 && rows[len(rows)-1] == "" {
		rows = rows[:len(rows)-1]
	}
	if m.height < 25 || len(rows)+2 > m.height {
		content := headingStyle.Render(strings.ToUpper(screenNames[m.screen])) + "\n" +
			hintStyle.Render(compactHelp(m.screen)) + "\n\n" +
			headingStyle.Render("EVERYWHERE") + "\n" +
			hintStyle.Render("1/2/3/4/5 screens   Ctrl-P commands   Tab panes\n"+
				"f fetch   p pull   P push   o last operation\n"+
				"r refresh   t theme   z expand   q back   ? close")
		o := r.SoftPanelOverlay(tideui.SoftPanel{Prefix: "tidegit", Title: "keyboard guide",
			Width: width, Content: inset(content, width)})
		return &o
	}
	content := inset(strings.TrimRight(strings.Join(rows, "\n"), "\n"), width)
	o := r.SoftPanelOverlay(tideui.SoftPanel{Prefix: "tidegit", Title: "keyboard guide",
		Width: width, Content: content})
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
	case screenStash:
		return "j/k stashes   Enter patch   a apply\np pop   d drop   n stash   N +untracked"
	case screenRemotes:
		return "j/k remotes   f fetch   F fetch all\np pull   P push   / filter"
	default:
		return "s/u file   S/U hunk   [/] choose hunk\nc commit   A amend   / filter"
	}
}
