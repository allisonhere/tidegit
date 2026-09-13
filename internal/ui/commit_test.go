package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/allisonhere/tidegit/internal/git"
	"github.com/allisonhere/tideui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func composingModel(t *testing.T) *Model {
	t.Helper()
	m, _ := stagingModel(t)
	key(t, m, "s")
	key(t, m, "c")
	if m.compose == nil {
		t.Fatalf("no editor: %s", m.notice)
	}
	return m
}
func control(t *testing.T, m *Model, k tea.KeyType) {
	t.Helper()
	_, cmd := m.Update(tea.KeyMsg{Type: k})
	drain(t, m, cmd)
}
func TestCommitWorkflowReturnsToStatus(t *testing.T) {
	m := composingModel(t)
	message := "Stage exactly what you mean\n\nKeep unrelated changes in the working tree.\n"
	key(t, m, message)
	control(t, m, tea.KeyCtrlS)
	if m.compose != nil || m.busy || len(m.status.Groups[git.Staged]) != 0 || !strings.Contains(m.notice, "Committed ") {
		t.Fatalf("commit lifecycle: %s", m.notice)
	}
	if got := uiGit(t, m.repo.Root, "show", "-s", "--format=format:%B", "HEAD"); got != message {
		t.Fatalf("Ripple message changed: %q", got)
	}
}
func TestHookFailureKeepsRippleDraft(t *testing.T) {
	m := composingModel(t)
	message := "A draft worth keeping\n\nBody stays after rejection."
	key(t, m, message)
	path := filepath.Join(m.repo.Root, ".git", "hooks", "commit-msg")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho 'Policy: include a ticket reference' >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	old := m.status.OID
	control(t, m, tea.KeyCtrlS)
	if m.compose == nil || m.compose.editor.Value() != message || !strings.Contains(m.compose.errorText, "ticket reference") || m.busy {
		t.Fatal("draft or hook error lost")
	}
	if strings.TrimSpace(uiGit(t, m.repo.Root, "rev-parse", "HEAD")) != old {
		t.Fatal("hook failure created commit")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	control(t, m, tea.KeyCtrlS)
	if m.compose != nil {
		t.Fatalf("retry failed: %s", m.compose.errorText)
	}
}
func TestRippleEditingAndCancellation(t *testing.T) {
	m := composingModel(t)
	key(t, m, "draft")
	control(t, m, tea.KeyCtrlZ)
	if m.compose.editor.Value() != "" {
		t.Fatal("Ripple undo not routed")
	}
	control(t, m, tea.KeyCtrlY)
	control(t, m, tea.KeyEsc)
	if !m.compose.confirmCancel {
		t.Fatal("edited draft discarded without confirmation")
	}
	control(t, m, tea.KeyEnter)
	if m.compose.confirmCancel {
		t.Fatal("keep editing failed")
	}
	control(t, m, tea.KeyEsc)
	control(t, m, tea.KeyCtrlD)
	if m.compose != nil || len(m.status.Groups[git.Staged]) != 1 {
		t.Fatal("cancel changed staging")
	}
	key(t, m, "c")
	control(t, m, tea.KeyEsc)
	if m.compose != nil {
		t.Fatal("unchanged editor prompted")
	}
}
func TestAmendEditorAndSignoff(t *testing.T) {
	m, _ := stagingModel(t)
	key(t, m, "A")
	if m.compose == nil || !m.compose.amend || m.compose.editor.Value() != "baseline\n" {
		t.Fatal("amend did not load HEAD")
	}
	m.compose.editor.SetValue("Amended baseline")
	control(t, m, tea.KeyCtrlO)
	control(t, m, tea.KeyCtrlS)
	if m.compose != nil {
		t.Fatal("amend failed")
	}
	if !strings.Contains(uiGit(t, m.repo.Root, "show", "-s", "--format=format:%B", "HEAD"), "Signed-off-by:") {
		t.Fatal("signoff toggle ignored")
	}
	if len(m.status.Groups[git.Unstaged]) != 1 {
		t.Fatal("amend staged unrelated working changes")
	}
}
func TestCommitViewSizesAndThemes(t *testing.T) {
	m := composingModel(t)
	m.compose.editor.SetValue("A clear subject\n\nA longer body with Unicode 海 and selection.\n# Comment guidance\n")
	for _, theme := range []tideui.Theme{tideui.CatppuccinMocha, tideui.CatppuccinLatte, tideui.VT52} {
		m.theme = theme
		for _, size := range [][2]int{{140, 40}, {100, 28}, {80, 24}, {54, 16}, {40, 10}, {1, 1}} {
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			v := m.View()
			if lipgloss.Width(v) > size[0] || lipgloss.Height(v) > size[1] {
				t.Fatalf("%s overflow %v: %dx%d", theme.Name, size, lipgloss.Width(v), lipgloss.Height(v))
			}
			if size[0] >= 54 && !strings.Contains(v, "NEW COMMIT") {
				t.Fatal("missing commit mode")
			}
		}
	}
}
func TestStagedReviewRefreshPreservesMessage(t *testing.T) {
	m := composingModel(t)
	m.compose.editor.SetValue("Keep this draft")
	uiWrite(t, m.repo.Root, "external", "new staged file")
	uiGit(t, m.repo.Root, "add", "external")
	control(t, m, tea.KeyCtrlS)
	if m.compose == nil || !strings.Contains(m.compose.errorText, "staged content changed") {
		t.Fatal("unreviewed external index was committed")
	}
	control(t, m, tea.KeyCtrlR)
	if m.compose.editor.Value() != "Keep this draft" || len(m.compose.info.Status.Groups[git.Staged]) != 2 {
		t.Fatal("refresh lost draft or metadata")
	}
}
func TestNothingStagedKeepsStatusScreen(t *testing.T) {
	m, _ := stagingModel(t)
	key(t, m, "c")
	if m.compose != nil || m.opening {
		t.Fatal("opened an empty commit")
	}
	if !strings.Contains(m.notice, "nothing staged") {
		t.Fatalf("unexplained refusal: %q", m.notice)
	}
	if m.View() == "" || !strings.Contains(ansi.Strip(m.View()), "nothing staged") {
		t.Fatal("refusal not visible")
	}
}
func TestGitOutputPanelAndStagedReview(t *testing.T) {
	m := composingModel(t)
	uiWrite(t, m.repo.Root, "second", "another staged file\n")
	uiGit(t, m.repo.Root, "add", "second")
	control(t, m, tea.KeyCtrlR)
	if len(m.compose.info.Status.Groups[git.Staged]) != 2 {
		t.Fatal("refresh missed external staging")
	}
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	control(t, m, tea.KeyTab)
	if m.compose.focus != 1 {
		t.Fatal("tab did not reach the staged review")
	}
	first := m.compose.preview
	key(t, m, "j")
	if m.compose.selected != 1 {
		t.Fatal("review selection stuck")
	}
	if len(first) > 0 && len(m.compose.preview) > 0 && first[0] == m.compose.preview[0] {
		t.Fatal("preview did not follow the selection")
	}
	if !strings.Contains(ansi.Strip(m.View()), "STAGED PREVIEW") {
		t.Fatal("staged preview not rendered")
	}
	control(t, m, tea.KeyTab)
	key(t, m, "draft")
	path := filepath.Join(m.repo.Root, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho 'line one of git output' >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	control(t, m, tea.KeyCtrlR)
	control(t, m, tea.KeyCtrlS)
	if m.compose == nil || m.compose.errorText == "" {
		t.Fatal("hook failure not reported")
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyF6})
	drain(t, m, cmd)
	if !m.compose.showOutput {
		t.Fatal("F6 did not open Git output")
	}
	if !strings.Contains(ansi.Strip(m.View()), "line one of git output") {
		t.Fatal("Git output not shown")
	}
	control(t, m, tea.KeyEsc)
	if m.compose.showOutput || m.compose == nil || m.compose.editor.Value() != "draft" {
		t.Fatal("closing output disturbed the draft")
	}
}
func TestRunningCommitCanBeAbandoned(t *testing.T) {
	m := composingModel(t)
	key(t, m, "waiting on a hook")
	// A commit has no deadline; Ctrl-C must still leave while Git runs.
	m.busy = true
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("no escape from a running commit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("Ctrl-C did not quit while Git was running")
	}
	m.busy = false
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Fatal("Ctrl-C quit while editing instead of copying")
		}
	}
	if m.compose == nil || m.compose.editor.Value() != "waiting on a hook" {
		t.Fatal("draft lost")
	}
}
