package ui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/allisonhere/ripple"
	"github.com/allisonhere/tidegit/internal/git"
	"github.com/allisonhere/tideui"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func (m *Model) editorView(r tideui.Renderer) string {
	c := m.compose
	text := c.editor.Value()
	keys := make([]string, len([]rune(text)))
	offset := 0
	for lineIndex, line := range strings.Split(text, "\n") {
		key := ""
		if lineIndex == 0 {
			key = "subject"
		}
		if strings.HasPrefix(line, c.info.CommentPrefix) {
			key = "comment"
		}
		for range []rune(line) {
			keys[offset] = key
			offset++
		}
		offset++
	}
	return c.editor.View(ripple.Options{
		CursorRune:  func(s string) string { return r.Styles.ItemSelected.Reverse(true).Render(s) },
		Selected:    func(s string) string { return r.Styles.ItemSelected.Render(s) },
		Placeholder: func(s string) string { return muted(r, s) },
		StyleKey: func(i int) string {
			if i >= 0 && i < len(keys) {
				return keys[i]
			}
			return ""
		},
		Style: func(key, text string) string {
			switch key {
			case "subject":
				return r.Styles.DetailBody.Bold(true).Render(text)
			case "comment":
				return muted(r, text)
			default:
				return r.Styles.DetailBody.Render(text)
			}
		},
	})
}

func (m *Model) commitView(r tideui.Renderer) string {
	c := m.compose
	mode := "NEW COMMIT"
	if c.amend {
		mode = "AMEND HEAD"
	}
	leftWidth := m.width
	if m.width >= 100 {
		leftWidth = m.width * 7 / 10
	}
	inner := leftWidth - 2
	heading := accent(r, mode) + muted(r, "  /  MESSAGE")
	if c.amend {
		heading = r.Styles.DetailBody.Foreground(r.Styles.Theme.Error).Bold(true).Render("AMEND HEAD") + muted(r, "  replaces "+c.info.Status.OID[:min(7, len(c.info.Status.OID))])
	}
	subject, _, _ := strings.Cut(c.editor.Value(), "\n")
	count := utf8.RuneCountInString(subject)
	guide := muted(r, "Describe the change. Give the why room below.")
	stats := muted(r, fmt.Sprintf("Subject %d / 50 guide   ·   Body 72-column guide", count))
	if count > 50 {
		stats = muted(r, fmt.Sprintf("Subject %d characters · longer subjects are welcome", count))
	}
	errorLine := muted(r, "Your message stays here if a hook or signing fails.")
	if c.errorText != "" {
		first, _, _ := strings.Cut(c.errorText, "\n")
		errorLine = r.Styles.DetailBody.Foreground(r.Styles.Theme.Error).Render("! "+safeText(first)) + "\n" + muted(r, "F6  inspect output   ·   Ctrl-R  refresh review")
	}
	if m.busy {
		errorLine = accent(r, m.activity()+" Running Git hooks and signing…") + "\n" + muted(r, "Waiting for Git. Your draft is retained.   ·   Ctrl-C  leave TideGit")
	}
	editorLines := strings.Split(m.editorView(r), "\n")
	_, editorHeight := m.editorDimensions()
	for len(editorLines) < editorHeight {
		editorLines = append(editorLines, "")
	}
	editorText := strings.Join(editorLines, "\n")
	content := inset(heading+"\n\n"+guide+"\n\n"+editorText+"\n"+stats+"\n\n"+errorLine, inner)
	left := frameContent(r, content, leftWidth, m.height-4, c.focus == 0)
	main := left
	if m.width >= 100 {
		width := m.width - leftWidth
		files := c.info.Status.Groups[git.Staged]
		summary := accent(r, "STAGED FOR COMMIT") + "\n\n" + r.Styles.DetailBody.Bold(true).Render(plural(len(files), "file")) + "\n" + r.Styles.DetailBody.Foreground(r.Styles.Theme.Unread).Render(fmt.Sprintf("+%d additions", c.info.Additions)) + muted(r, "   ") + r.Styles.DetailBody.Foreground(r.Styles.Theme.Error).Render(fmt.Sprintf("−%d deletions", c.info.Deletions)) + "\n"
		if c.info.Binary > 0 {
			summary += muted(r, plural(c.info.Binary, "binary file")) + "\n"
		}
		summary += "\n"
		visibleFiles := max(1, min(6, m.height-20))
		start := max(0, c.selected-visibleFiles+1)
		for i := start; i < min(len(files), start+visibleFiles); i++ {
			f := files[i]
			summary += r.RenderRow(tideui.Row{Text: safeText(f.Path), Suffix: fileMark(f, int(git.Staged)), Selected: i == c.selected}, max(1, width-4)) + "\n"
		}
		if len(files) == 0 {
			summary += muted(r, "Message-only amendment") + "\n"
		}
		if len(files) > 0 {
			summary += "\n" + accent(r, "STAGED PREVIEW") + "\n" + muted(r, clip(safeText(files[c.selected].Path), width-4)) + "\n\n"
			visible := max(1, m.height-6-strings.Count(summary, "\n"))
			if c.previewLoading {
				summary += muted(r, "Reading staged diff…")
			} else {
				summary += m.renderDiffView(&c.view, r, width-4, visible, false)
			}
		}
		right := frameContent(r, inset(summary, width-2), width, m.height-4, c.focus == 1)
		main = lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	} else if c.focus == 1 {
		// Narrow screens give all width to either editor or review, never squeeze
		// Ripple beside a one-column inspector.
		var rows []string
		rows = append(rows, accent(r, "STAGED FOR COMMIT"), muted(r, fmt.Sprintf("%s · +%d / -%d", plural(len(c.info.Status.Groups[git.Staged]), "file"), c.info.Additions, c.info.Deletions)), "")
		files := c.info.Status.Groups[git.Staged]
		start := max(0, c.selected-max(1, m.height-12)+1)
		for i := start; i < min(len(files), start+max(1, m.height-12)); i++ {
			f := files[i]
			rows = append(rows, r.RenderRow(tideui.Row{Text: safeText(f.Path), Selected: i == c.selected}, m.width-4))
		}
		main = frameContent(r, inset(strings.Join(rows, "\n"), m.width-2), m.width, m.height-4, true)
	}
	signoff := "off"
	if c.signoff {
		signoff = "on"
	}
	status := fmt.Sprintf("%s · %s · +%d / -%d · signoff %s", mode, plural(len(c.info.Status.Groups[git.Staged]), "file"), c.info.Additions, c.info.Deletions, signoff)
	if c.refreshing {
		status = m.activity() + " Refreshing staged review"
	}
	if m.busy {
		status = m.activity() + " " + m.operation
	}
	base := m.globalHeader(r, mode) + "\n" + main + "\n" + padLine(" "+status, m.width, r.Styles.StatusBar.Padding(0)) + "\n" + m.hintBar(r, tideui.SoftHint{Key: "Ctrl-S", Label: "commit"}, tideui.SoftHint{Key: "Tab", Label: "review"}, tideui.SoftHint{Key: "Esc", Label: "back"}, tideui.SoftHint{Key: "F1", Label: "help"})
	if c.confirmCancel {
		p := r.SoftPanelOverlay(tideui.SoftPanel{Prefix: "tidegit", Title: "discard this draft?", Width: min(58, m.width-6), Content: inset("Your edited commit message will be discarded.\nStaged files and working files will stay unchanged.\n\n"+accent(r, "Ctrl-D  discard draft")+"\n"+muted(r, "Enter / Esc  keep editing"), min(58, m.width-6))})
		base = r.OverlayModal(base, p.Content, m.width, m.height)
	} else if c.showOutput {
		rawLines := strings.Split(c.errorText, "\n")
		for i, line := range rawLines {
			rawLines[i] = safeText(line)
		}
		lines := strings.Split(ansi.Hardwrap(strings.Join(rawLines, "\n"), m.width-12, true), "\n")
		if c.errorText == "" {
			lines = []string{"No Git errors to display."}
		}
		start := min(c.outputScroll.Offset(), max(0, len(lines)-max(1, m.height-9)))
		end := min(len(lines), start+max(1, m.height-9))
		var content []string
		for _, line := range lines[start:end] {
			content = append(content, safeText(line))
		}
		content = append(content, "", muted(r, "j/k scroll · F6 / Esc close · draft preserved"))
		p := r.SoftPanelOverlay(tideui.SoftPanel{Prefix: "tidegit", Title: "Git output", Width: m.width - 8, Content: inset(strings.Join(content, "\n"), m.width-8)})
		base = r.OverlayModal(base, p.Content, m.width, m.height)
	} else if c.help {
		p := r.SoftPanelOverlay(tideui.SoftPanel{Prefix: "tidegit", Title: "commit guide", Width: min(62, m.width-6), Content: inset(accent(r, "COMMIT")+"\nCtrl-S submit · Ctrl-O signoff · Ctrl-R refresh\nEsc return · F6 Git output\nCtrl-C quits only while Git is running\n\n"+accent(r, "RIPPLE EDITOR")+"\nArrows move · Shift+arrows select\nCtrl-Z undo · Ctrl-Y redo\nCtrl-C copy · Ctrl-X cut · Ctrl-V paste\n\n"+muted(r, "Tab switches editor / staged review.\nF1 / Esc closes this guide."), min(62, m.width-6))})
		base = r.OverlayModal(base, p.Content, m.width, m.height)
	}
	return base
}

// Unlike Layout's generic wrapping body, this clips Ripple output to exactly
// its allocated cells. Ripple owns grapheme wrapping and its viewport.
func frameContent(r tideui.Renderer, content string, width, height int, focused bool) string {
	innerW, innerH := max(1, width-2), max(1, height-2)
	lines := strings.Split(content, "\n")
	if len(lines) > innerH {
		lines = lines[:innerH]
	}
	for i, line := range lines {
		lines[i] = padLine(line, innerW, r.Styles.DetailBody)
	}
	return r.Styles.PaneFrame(focused, "").Width(innerW).Height(innerH).Render(strings.Join(lines, "\n"))
}
