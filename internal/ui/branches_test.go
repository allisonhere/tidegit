package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func branchesModel(t *testing.T) *Model {
	t.Helper()
	m := historyModel(t)
	drain(t, m, m.goToScreen(screenBranches))
	if m.branches == nil || len(m.branches.local) == 0 {
		t.Fatalf("branches did not load: %v", m.branches)
	}
	return m
}

// selectBranch moves the cursor onto a branch by name and loads its detail.
func selectBranch(t *testing.T, m *Model, name string) {
	t.Helper()
	m.branches.selectByName(name)
	if b, ok := m.branches.current(); !ok || b.Name != name {
		t.Fatalf("could not select %q", name)
	}
	drain(t, m, m.loadBranchDetail())
}

func TestBranchesScreenListsAndInspects(t *testing.T) {
	m := branchesModel(t)
	view := ansi.Strip(m.View())
	for _, want := range []string{"BRANCHES", "LOCAL", "REMOTE", "main",
		"feature/hunks", "wip/unmerged-work", "origin/main"} {
		if !strings.Contains(view, want) {
			t.Fatalf("branch list missing %q", want)
		}
	}
	// The current branch is marked, and its tracking state is stated in words
	// as well as arrows.
	selectBranch(t, m, "main")
	m.focus = 2
	view = ansi.Strip(m.View())
	if !strings.Contains(view, "current branch") {
		t.Fatal("current branch not marked")
	}
	if !strings.Contains(view, "origin/main") || !strings.Contains(view, "ahead") {
		t.Fatalf("tracking not presented: %s", view)
	}
	if !strings.Contains(view, "↑") {
		t.Fatal("ahead count has no symbol")
	}
	if !strings.Contains(view, "LOCAL BRANCH") {
		t.Fatal("inspector does not say what kind of branch this is")
	}
	// The middle pane shows that branch's own commits.
	if len(m.branches.commits) == 0 || m.branches.commitsFor != "main" {
		t.Fatalf("branch commits: %d for %q", len(m.branches.commits), m.branches.commitsFor)
	}
	if !strings.Contains(view, "Bound captured Git output") {
		t.Fatal("branch commits not rendered")
	}
}

func TestBranchInspectorRemoteAndUnmerged(t *testing.T) {
	m := branchesModel(t)
	selectBranch(t, m, "origin/main")
	m.focus = 2
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "REMOTE-TRACKING") {
		t.Fatal("remote branch not identified")
	}
	if !strings.Contains(view, "tracked by") || !strings.Contains(view, "main") {
		t.Fatalf("remote branch does not name its local tracker: %s", view)
	}

	selectBranch(t, m, "wip/unmerged-work")
	view = ansi.Strip(m.View())
	if !strings.Contains(view, "not merged into HEAD") {
		t.Fatal("unmerged branch not reported as unmerged")
	}
	if !strings.Contains(view, "ahead") {
		t.Fatal("divergence against HEAD missing")
	}
	if b, _ := m.branches.current(); b.Merged {
		t.Fatal("unmerged branch reported as merged")
	}
	selectBranch(t, m, "feature/hunks")
	if b, _ := m.branches.current(); !b.Merged {
		t.Fatal("merged branch reported as unmerged")
	}
}

func TestBranchSwitch(t *testing.T) {
	m := branchesModel(t)
	selectBranch(t, m, "feature/hunks")
	key(t, m, "enter")
	if m.busy {
		t.Fatal("switch left the UI busy")
	}
	if head := strings.TrimSpace(uiGit(t, m.repo.Root, "rev-parse", "--abbrev-ref", "HEAD")); head != "feature/hunks" {
		t.Fatalf("HEAD is %q", head)
	}
	// The screen must follow the change: current marker, header and selection.
	if b, _ := m.branches.current(); !b.Current || b.Name != "feature/hunks" {
		t.Fatalf("selection did not follow the switch: %+v", b)
	}
	if m.head.Branch != "feature/hunks" {
		t.Fatalf("header state stale: %+v", m.head)
	}
	if !strings.Contains(m.notice, "Switched to feature/hunks") {
		t.Fatalf("no feedback: %q", m.notice)
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "@ feature/hunks") {
		t.Fatal("current-branch marker did not move")
	}
}

func TestBranchSwitchBlockedByLocalChanges(t *testing.T) {
	m := branchesModel(t)
	// docs.md exists on main but not on feature/hunks, so Git must refuse
	// rather than discard the edit.
	uiWrite(t, m.repo.Root, "docs.md", "local work that must survive\n")
	selectBranch(t, m, "feature/hunks")
	key(t, m, "enter")
	if head := strings.TrimSpace(uiGit(t, m.repo.Root, "rev-parse", "--abbrev-ref", "HEAD")); head != "main" {
		t.Fatalf("refused switch moved HEAD to %q", head)
	}
	if m.branches.err == "" || !strings.Contains(m.branches.err, "working tree unchanged") {
		t.Fatalf("refusal not explained: %q", m.branches.err)
	}
	if !strings.Contains(ansi.Strip(m.View()), "Git could not complete") {
		t.Fatal("error not surfaced on screen")
	}
	if got := readFile(t, m.repo.Root, "docs.md"); got != "local work that must survive\n" {
		t.Fatalf("working tree changed: %q", got)
	}
}

func TestBranchSwitchRefusesRemoteAndCurrent(t *testing.T) {
	m := branchesModel(t)
	selectBranch(t, m, "origin/main")
	key(t, m, "enter")
	if !strings.Contains(m.notice, "read-only") {
		t.Fatalf("remote switch notice: %q", m.notice)
	}
	selectBranch(t, m, "main")
	key(t, m, "enter")
	if !strings.Contains(m.notice, "Already on main") {
		t.Fatalf("current switch notice: %q", m.notice)
	}
}

func TestBranchCreateFromBranchAndSwitch(t *testing.T) {
	m := branchesModel(t)
	selectBranch(t, m, "feature/hunks")
	key(t, m, "n")
	if m.prompt == nil || m.prompt.context != "feature/hunks" {
		t.Fatalf("prompt start point: %+v", m.prompt)
	}
	key(t, m, "spike/one")
	drain(t, m, m.submitPrompt(false))
	if !strings.Contains(uiGit(t, m.repo.Root, "branch", "--list", "spike/one"), "spike/one") {
		t.Fatal("branch not created")
	}
	// Creating must not switch unless asked.
	if head := strings.TrimSpace(uiGit(t, m.repo.Root, "rev-parse", "--abbrev-ref", "HEAD")); head != "main" {
		t.Fatalf("plain create switched to %q", head)
	}
	if b, _ := m.branches.current(); b.Name != "spike/one" {
		t.Fatalf("new branch not selected: %+v", b)
	}
	if a, e := strings.TrimSpace(uiGit(t, m.repo.Root, "rev-parse", "spike/one")),
		strings.TrimSpace(uiGit(t, m.repo.Root, "rev-parse", "feature/hunks")); a != e {
		t.Fatal("branch did not start at the selected ref")
	}

	// Ctrl-S is the explicit create-and-switch path.
	key(t, m, "n")
	key(t, m, "spike/two")
	drain(t, m, m.submitPrompt(true))
	if head := strings.TrimSpace(uiGit(t, m.repo.Root, "rev-parse", "--abbrev-ref", "HEAD")); head != "spike/two" {
		t.Fatalf("create-and-switch left HEAD at %q", head)
	}

	// An invalid name is refused by Git and reported, creating nothing.
	key(t, m, "n")
	key(t, m, "bad name")
	drain(t, m, m.submitPrompt(false))
	if m.branches.err == "" {
		t.Fatal("invalid branch name was accepted")
	}
	if strings.Contains(uiGit(t, m.repo.Root, "branch", "--list"), "bad") {
		t.Fatal("invalid branch was created")
	}
}

func TestBranchRename(t *testing.T) {
	m := branchesModel(t)
	selectBranch(t, m, "feature/hunks")
	key(t, m, "R")
	if m.prompt == nil || m.prompt.value != "feature/hunks" {
		t.Fatalf("rename prompt: %+v", m.prompt)
	}
	m.prompt.value = "feature/hunk-staging"
	drain(t, m, m.submitPrompt(false))
	list := uiGit(t, m.repo.Root, "branch", "--list")
	if strings.Contains(list, "feature/hunks\n") || !strings.Contains(list, "feature/hunk-staging") {
		t.Fatalf("rename did not take: %s", list)
	}
	if b, _ := m.branches.current(); b.Name != "feature/hunk-staging" {
		t.Fatalf("selection did not follow the rename: %+v", b)
	}

	// Renaming the current branch is allowed and HEAD follows it.
	selectBranch(t, m, "main")
	key(t, m, "R")
	m.prompt.value = "trunk"
	drain(t, m, m.submitPrompt(false))
	if head := strings.TrimSpace(uiGit(t, m.repo.Root, "rev-parse", "--abbrev-ref", "HEAD")); head != "trunk" {
		t.Fatalf("HEAD after renaming the current branch: %q", head)
	}
	if m.head.Branch != "trunk" {
		t.Fatalf("header state stale after rename: %+v", m.head)
	}
	if b, _ := m.branches.current(); !b.Current || b.Name != "trunk" {
		t.Fatalf("renamed current branch lost its marker: %+v", b)
	}

	// A remote branch cannot be renamed from here.
	selectBranch(t, m, "origin/main")
	key(t, m, "R")
	if m.prompt != nil {
		t.Fatal("opened a rename prompt for a remote branch")
	}
	if !strings.Contains(m.notice, "cannot be renamed") {
		t.Fatalf("remote rename notice: %q", m.notice)
	}
}

func TestBranchDeleteRequiresConfirmationAndIsSafe(t *testing.T) {
	m := branchesModel(t)
	selectBranch(t, m, "feature/hunks")
	key(t, m, "D")
	if m.confirm == nil || m.confirm.kind != confirmDeleteBranch {
		t.Fatal("delete did not ask first")
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "Delete feature/hunks?") {
		t.Fatalf("confirmation does not name the branch: %s", view)
	}
	// Escaping keeps the branch.
	key(t, m, "esc")
	if m.confirm != nil || !strings.Contains(uiGit(t, m.repo.Root, "branch", "--list"), "feature/hunks") {
		t.Fatal("cancelled delete removed the branch")
	}

	// Confirming deletes a merged branch and leaves everything else alone.
	before := uiGit(t, m.repo.Root, "branch", "--list")
	key(t, m, "D")
	key(t, m, "d")
	if strings.Contains(uiGit(t, m.repo.Root, "branch", "--list"), "feature/hunks") {
		t.Fatal("confirmed delete did not remove the branch")
	}
	for _, keep := range []string{"main", "wip/unmerged-work"} {
		if !strings.Contains(before, keep) || !strings.Contains(uiGit(t, m.repo.Root, "branch", "--list"), keep) {
			t.Fatalf("delete disturbed %q", keep)
		}
	}
	if !strings.Contains(m.notice, "Deleted branch feature/hunks") {
		t.Fatalf("no feedback: %q", m.notice)
	}
}

func TestBranchDeleteUnmergedNeedsSecondExplicitStep(t *testing.T) {
	m := branchesModel(t)
	selectBranch(t, m, "wip/unmerged-work")
	key(t, m, "D")
	if m.confirm == nil || !m.confirm.danger {
		t.Fatal("unmerged delete was not flagged as dangerous")
	}
	if !strings.Contains(ansi.Strip(m.View()), "NOT merged") {
		t.Fatal("confirmation does not warn that the branch is unmerged")
	}
	// Agreeing to the safe delete still does not discard the commits: Git
	// refuses, and the refusal becomes a second, differently worded question.
	key(t, m, "d")
	if !strings.Contains(uiGit(t, m.repo.Root, "branch", "--list"), "wip/unmerged-work") {
		t.Fatal("safe delete discarded unmerged work")
	}
	if m.confirm == nil || m.confirm.kind != confirmForceDeleteBranch {
		t.Fatalf("no separate force confirmation: %+v", m.confirm)
	}
	if m.confirm.accept == "d" {
		t.Fatal("force delete accepts the same key as the safe delete")
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "not merged into HEAD") || !strings.Contains(view, "cannot be undone") {
		t.Fatalf("force confirmation does not explain itself: %s", view)
	}
	// Declining leaves the branch in place.
	key(t, m, "esc")
	if !strings.Contains(uiGit(t, m.repo.Root, "branch", "--list"), "wip/unmerged-work") {
		t.Fatal("declining the force delete removed the branch")
	}

	// Only the explicit force key discards it.
	key(t, m, "D")
	key(t, m, "d")
	key(t, m, "D")
	if strings.Contains(uiGit(t, m.repo.Root, "branch", "--list"), "wip/unmerged-work") {
		t.Fatal("explicit force delete did not remove the branch")
	}
	if !strings.Contains(m.notice, "Force-deleted") {
		t.Fatalf("force delete feedback: %q", m.notice)
	}
}

func TestBranchDeleteRefusesCurrentAndRemote(t *testing.T) {
	m := branchesModel(t)
	selectBranch(t, m, "main")
	key(t, m, "D")
	if m.confirm != nil || !strings.Contains(m.notice, "Switch to another branch") {
		t.Fatalf("current branch delete: %q", m.notice)
	}
	selectBranch(t, m, "origin/main")
	key(t, m, "D")
	if m.confirm != nil || !strings.Contains(m.notice, "out of scope") {
		t.Fatalf("remote branch delete: %q", m.notice)
	}
	if !strings.Contains(uiGit(t, m.repo.Root, "branch", "--list"), "main") {
		t.Fatal("a refused delete removed a branch")
	}
}

func TestBranchFilterAndNavigation(t *testing.T) {
	m := branchesModel(t)
	m.focus = 0
	key(t, m, "/")
	key(t, m, "wip")
	if len(m.branches.groups()[0]) != 1 {
		t.Fatalf("filter matched %d local branches", len(m.branches.groups()[0]))
	}
	key(t, m, "esc")
	if m.branches.filter != "" || len(m.branches.groups()[0]) < 3 {
		t.Fatal("clearing the filter did not restore the list")
	}
	// Navigation walks captions without stopping on them.
	m.branches.selectByName("main")
	for i := 0; i < 10; i++ {
		drain(t, m, m.moveBranch(1))
		if b, ok := m.branches.current(); !ok || b.Name == "" {
			t.Fatal("navigation landed on a caption")
		}
	}
}

func TestBranchesScreenSizes(t *testing.T) {
	m := branchesModel(t)
	for _, size := range [][2]int{{160, 40}, {132, 34}, {100, 28}, {80, 24}, {60, 20}, {54, 16}, {40, 12}, {1, 1}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := m.View()
		if lipgloss.Width(view) > size[0] || lipgloss.Height(view) > size[1] {
			t.Fatalf("overflow at %v: %dx%d", size, lipgloss.Width(view), lipgloss.Height(view))
		}
		if size[0] >= 54 && !strings.Contains(ansi.Strip(view), "main") {
			t.Fatalf("branch names dropped at width %d", size[0])
		}
	}
}

func TestPaletteRunsScreenCommands(t *testing.T) {
	m := historyModel(t)
	key(t, m, "ctrl+p")
	if m.palette == nil {
		t.Fatal("palette did not open")
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "command palette") || !strings.Contains(view, "Go to History") {
		t.Fatalf("palette not rendered: %s", view)
	}
	// Matching allows gaps, so short queries reach long command names.
	key(t, m, "gtb")
	if len(m.palette.items) == 0 || !strings.Contains(m.palette.items[0].title, "Branches") {
		t.Fatalf("palette match: %+v", m.palette.items)
	}
	key(t, m, "enter")
	if m.palette != nil || m.screen != screenBranches {
		t.Fatalf("palette command did not run: screen %d", m.screen)
	}

	// Commands that only make sense elsewhere are not offered.
	key(t, m, "ctrl+p")
	for _, item := range m.palette.items {
		if item.action == paletteCopyHash {
			t.Fatal("History-only command offered on Branches")
		}
	}
	key(t, m, "esc")
	if m.palette != nil {
		t.Fatal("escape did not close the palette")
	}
}

func TestPaletteJumpToRef(t *testing.T) {
	m := historyModel(t)
	drain(t, m, m.openHistory())
	target := strings.TrimSpace(uiGit(t, m.repo.Root, "rev-parse", "v0.1.0^{commit}"))
	drain(t, m, m.runPalette(paletteJumpToRef))
	if m.prompt == nil || m.prompt.kind != promptJumpToRef {
		t.Fatal("jump prompt did not open")
	}
	m.prompt.value = "v0.1.0"
	drain(t, m, m.submitPrompt(false))
	c, ok := m.history.current()
	if !ok || c.OID != target {
		t.Fatalf("jump selected %+v, wanted %s", c, target)
	}
	if !strings.Contains(m.notice, "Jumped to") {
		t.Fatalf("no jump feedback: %q", m.notice)
	}

	// An unknown ref reports itself instead of moving the selection.
	drain(t, m, m.runPalette(paletteJumpToRef))
	m.prompt.value = "no-such-thing"
	drain(t, m, m.submitPrompt(false))
	if !strings.Contains(m.notice, "no commit matches") {
		t.Fatalf("unknown ref notice: %q", m.notice)
	}
}

func TestScreenSwitchingKeepsState(t *testing.T) {
	m := historyModel(t)
	key(t, m, "2")
	if m.screen != screenHistory {
		t.Fatal("2 did not reach History")
	}
	drain(t, m, m.moveCommit(2))
	selected := m.history.selected
	key(t, m, "3")
	key(t, m, "1")
	if m.screen != screenStatus {
		t.Fatal("1 did not reach Status")
	}
	key(t, m, "2")
	if m.history.selected != selected {
		t.Fatalf("History lost its selection: %d vs %d", m.history.selected, selected)
	}
	// q steps back to Status rather than quitting from another screen.
	key(t, m, "3")
	key(t, m, "q")
	if m.screen != screenStatus {
		t.Fatal("q did not return to Status")
	}
}

func TestContextualHelpPerScreen(t *testing.T) {
	m := historyModel(t)
	key(t, m, "2")
	key(t, m, "?")
	view := ansi.Strip(m.View())
	for _, want := range []string{"HISTORY", "copy full hash", "GRAPH", "merge"} {
		if !strings.Contains(view, want) {
			t.Fatalf("History help missing %q", want)
		}
	}
	if strings.Contains(view, "stage / unstage HUNK") {
		t.Fatal("History help shows Status bindings")
	}
	key(t, m, "?")
	key(t, m, "3")
	key(t, m, "?")
	view = ansi.Strip(m.View())
	if !strings.Contains(view, "switch to branch") || !strings.Contains(view, "rename") {
		t.Fatalf("Branches help missing its actions: %s", view)
	}
	if !strings.Contains(view, "EVERYWHERE") {
		t.Fatal("shared bindings missing from help")
	}
}

// readFile reads a working-tree file for a test assertion.
func readFile(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestBranchesOpenOnCurrentBranch(t *testing.T) {
	m := branchesModel(t)
	b, ok := m.branches.current()
	if !ok || !b.Current || b.Name != "main" {
		t.Fatalf("branches opened on %+v rather than the current branch", b)
	}
	// The list is ordered by recency, so the current branch is not simply first.
	if m.branches.local[0].Name == "main" {
		t.Skip("fixture happens to list main first; the check needs a different order")
	}
}
