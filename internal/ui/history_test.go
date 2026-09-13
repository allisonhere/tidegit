package ui

import (
	"context"
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

// historyRepo builds a repository with the shapes the History and Branches
// screens have to render: a merge, a branch that never merged, tags, and
// remote-tracking refs one of which the current branch tracks.
func historyRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := filepath.Join(t.TempDir(), "history")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	uiGit(t, dir, "init", "-b", "main")
	uiGit(t, dir, "config", "user.name", "Allison Bayless")
	uiGit(t, dir, "config", "user.email", "allie@example.invalid")
	commit := func(subject, path, body string) {
		uiWrite(t, dir, path, body)
		uiGit(t, dir, "add", "-A")
		uiGit(t, dir, "commit", "-m", subject)
	}
	commit("Set up the repository skeleton", "README.md", "# Tide\n")
	commit("Add the Git command runner", "runner.go", "package git\n")
	uiGit(t, dir, "tag", "v0.1.0")
	uiGit(t, dir, "switch", "-c", "feature/hunks", "HEAD")
	commit("Sketch the hunk parser", "patch.go", "package git\n")
	commit("Generate reduced patches", "patch.go", "package git\n\nfunc patch() {}\n")
	uiGit(t, dir, "switch", "main")
	commit("Document the staging rules", "docs.md", "safety\n")
	uiGit(t, dir, "merge", "--no-ff", "-m", "Merge feature/hunks into main", "feature/hunks")
	commit("Bound captured Git output", "runner.go", "package git\n\nconst limit = 1\n")
	uiGit(t, dir, "tag", "-a", "v0.2.0", "-m", "Release two")
	// A branch that was never merged, for the delete-confirmation path.
	uiGit(t, dir, "switch", "-c", "wip/unmerged-work", "main")
	commit("Work nobody has merged yet", "wip.md", "wip\n")
	uiGit(t, dir, "switch", "main")
	// Remote-tracking refs, created directly so the fixture stays offline.
	uiGit(t, dir, "remote", "add", "origin", filepath.Join(dir, "..", "unreachable.git"))
	uiGit(t, dir, "update-ref", "refs/remotes/origin/main",
		strings.TrimSpace(uiGit(t, dir, "rev-parse", "main~2")))
	uiGit(t, dir, "update-ref", "refs/remotes/origin/feature/hunks",
		strings.TrimSpace(uiGit(t, dir, "rev-parse", "feature/hunks")))
	uiGit(t, dir, "branch", "--set-upstream-to=origin/main", "main")
	return dir
}

func historyModel(t *testing.T) *Model {
	t.Helper()
	m := New(context.Background(), historyRepo(t), tideui.CatppuccinMocha)
	m.width, m.height = 132, 34
	drain(t, m, m.Init())
	return m
}

// squeeze removes layout whitespace so a wrapped value can be matched whole.
func squeeze(text string) string {
	return strings.Join(strings.Fields(text), "")
}

// commitBySubject finds a loaded commit by its subject.
func commitBySubject(t *testing.T, m *Model, subject string) int {
	t.Helper()
	for i, c := range m.history.commits {
		if c.Subject == subject {
			return i
		}
	}
	t.Fatalf("no commit %q in the loaded history", subject)
	return 0
}

func TestHistoryScreenLoadsAndRenders(t *testing.T) {
	m := historyModel(t)
	drain(t, m, m.openHistory())
	if m.screen != screenHistory {
		t.Fatal("did not switch to History")
	}
	if len(m.history.commits) == 0 {
		t.Fatalf("no commits: %s", m.history.err)
	}
	view := ansi.Strip(m.View())
	for _, want := range []string{"HISTORY", "Bound captured Git output", "Merge feature/hunks into main",
		"@ main", "# v0.2.0", "WALK FROM", "REFS"} {
		if !strings.Contains(view, want) {
			t.Fatalf("history view missing %q", want)
		}
	}
	// The graph must draw a node for every visible commit and a merge glyph.
	if !strings.Contains(view, "●") || !strings.Contains(view, "◆") {
		t.Fatal("graph glyphs missing")
	}
	// The HEAD commit is marked distinctly from an ordinary commit.
	if !strings.Contains(view, "◉") {
		t.Fatal("HEAD marker missing from the graph")
	}
	if !strings.Contains(view, "○") {
		t.Fatal("root commit marker missing")
	}
}

func TestHistoryInspectorAndCommitDiff(t *testing.T) {
	m := historyModel(t)
	drain(t, m, m.openHistory())
	m.history.selected = commitBySubject(t, m, "Bound captured Git output")
	drain(t, m, m.scheduleDetail())
	if !m.history.detailOK {
		t.Fatal("commit detail not loaded")
	}
	d := m.history.detail
	if d.commit.Subject != "Bound captured Git output" || len(d.commit.Parents) != 1 {
		t.Fatalf("wrong commit inspected: %+v", d.commit)
	}
	if len(d.files) != 1 || d.files[0].Path != "runner.go" {
		t.Fatalf("changed files: %+v", d.files)
	}
	m.focus = 2
	view := ansi.Strip(m.View())
	for _, want := range []string{"Allison Bayless", "allie@example.invalid",
		"CHANGED FILES", "runner.go", "# v0.2.0"} {
		if !strings.Contains(view, want) {
			t.Fatalf("inspector missing %q", want)
		}
	}
	// The full hash is shown even though the column is narrower than 40 cells,
	// so it wraps rather than being truncated. The pane is checked on its own
	// because a composed row interleaves all three panes.
	pane := ansi.Strip(m.historyInspectorPane(m.renderer(), 40, 30))
	if !strings.Contains(squeeze(pane), d.commit.OID) {
		t.Fatalf("inspector does not show the full hash %s in:\n%s", d.commit.OID, pane)
	}

	// Opening a changed file shows that commit's patch, labelled as history.
	drain(t, m, m.loadCommitFileDiff())
	if !m.history.showDiff || len(m.history.view.patch.Files) == 0 || len(m.history.view.patch.Files[0].Hunks) == 0 {
		t.Fatalf("commit diff not loaded: %s", m.history.err)
	}
	view = ansi.Strip(m.View())
	if !strings.Contains(view, "Commit "+d.commit.Short) || !strings.Contains(view, "runner.go") {
		t.Fatal("commit diff is not labelled with its commit")
	}
	if !strings.Contains(view, "historical patch") {
		t.Fatal("commit diff does not distinguish itself from the working tree")
	}
	if !strings.Contains(view, "const limit") {
		t.Fatalf("patch content missing: %v", m.history.view.patch)
	}
	// Escape returns to the commit rather than leaving the screen.
	key(t, m, "esc")
	if m.history.showDiff || m.screen != screenHistory {
		t.Fatal("escape from the diff left the screen")
	}
}

func TestHistoryMergeInspectorShowsBothParents(t *testing.T) {
	m := historyModel(t)
	drain(t, m, m.openHistory())
	m.history.selected = commitBySubject(t, m, "Merge feature/hunks into main")
	drain(t, m, m.scheduleDetail())
	if len(m.history.detail.commit.Parents) != 2 {
		t.Fatalf("merge parents: %+v", m.history.detail.commit.Parents)
	}
	m.focus = 2
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "parents") || !strings.Contains(view, "(merge)") {
		t.Fatal("inspector does not present a merge as a merge")
	}
	// A merge is summarised against its first parent, so it lists what it brought in.
	if !strings.Contains(view, "patch.go") {
		t.Fatalf("merge changed files: %+v", m.history.detail.files)
	}
}

func TestHistoryFilterByRefAndSearch(t *testing.T) {
	m := historyModel(t)
	drain(t, m, m.openHistory())
	headCount := len(m.history.commits)

	// Every ref includes the branch that never merged; HEAD's own walk does not.
	for i, f := range m.history.filters {
		if f.All {
			m.history.filterIndex = i
		}
	}
	drain(t, m, m.loadHistory(false))
	if len(m.history.commits) <= headCount {
		t.Fatalf("all refs (%d) did not add to HEAD (%d)", len(m.history.commits), headCount)
	}
	commitBySubject(t, m, "Work nobody has merged yet")

	// Choosing a single branch walks only that branch.
	for i, f := range m.history.filters {
		if f.Rev == "feature/hunks" {
			m.history.filterIndex = i
		}
	}
	drain(t, m, m.loadHistory(false))
	for _, c := range m.history.commits {
		if c.Subject == "Bound captured Git output" {
			t.Fatal("branch filter leaked commits from main")
		}
	}
	commitBySubject(t, m, "Sketch the hunk parser")

	// Search filters by subject through Git rather than in memory.
	m.history.filterIndex = 0
	drain(t, m, m.loadHistory(false))
	m.focus = 1
	key(t, m, "/")
	if !m.history.searching {
		t.Fatal("search not opened")
	}
	key(t, m, "Bound")
	if len(m.history.commits) != 1 || m.history.commits[0].Subject != "Bound captured Git output" {
		t.Fatalf("search results: %v", m.history.commits)
	}
	if !strings.Contains(ansi.Strip(m.View()), "Filtering subjects") {
		t.Fatal("search state not shown")
	}
	key(t, m, "esc")
	if m.history.search != "" || len(m.history.commits) != headCount {
		t.Fatalf("clearing search left %d commits", len(m.history.commits))
	}
}

func TestHistoryIncrementalLoading(t *testing.T) {
	m := historyModel(t)
	// A batch smaller than the history forces paging through the same path the
	// UI uses when a long history scrolls.
	drain(t, m, m.openHistory())
	whole := len(m.history.commits)
	if whole < 6 {
		t.Fatalf("fixture too small to page: %d", whole)
	}
	h := m.history
	h.commits = h.commits[:3]
	h.graph = git.GraphLanes(h.commits)
	h.exhausted = false
	h.selected = 2
	drain(t, m, m.loadHistory(true))
	if len(h.commits) <= 3 {
		t.Fatalf("loading more returned %d commits", len(h.commits))
	}
	// Paging must not disturb the selection or reorder what was already shown.
	if h.selected != 2 {
		t.Fatalf("selection moved to %d", h.selected)
	}
	if len(h.graph) != len(h.commits) {
		t.Fatal("graph not recomputed for the appended page")
	}
	seen := map[string]bool{}
	for _, c := range h.commits {
		if seen[c.OID] {
			t.Fatalf("commit %s appeared twice after paging", c.Short)
		}
		seen[c.OID] = true
	}
}

func TestHistoryDetailCacheAvoidsReloading(t *testing.T) {
	m := historyModel(t)
	drain(t, m, m.openHistory())
	drain(t, m, m.moveCommit(1))
	first := m.history.detail.commit.OID
	drain(t, m, m.moveCommit(-1))
	drain(t, m, m.moveCommit(1))
	if m.history.detail.commit.OID != first {
		t.Fatal("returning to a commit lost its detail")
	}
	// A cached commit is shown without scheduling any Git work.
	if m.history.detailLoading {
		t.Fatal("cached commit still triggered a load")
	}
	if _, ok := m.history.cache[first]; !ok {
		t.Fatal("commit was not cached")
	}
}

func TestHistoryNarrowWidthsKeepSubjectAndGraph(t *testing.T) {
	m := historyModel(t)
	drain(t, m, m.openHistory())
	for _, size := range [][2]int{{160, 40}, {132, 34}, {100, 28}, {80, 24}, {60, 20}, {54, 16}, {40, 12}, {1, 1}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := m.View()
		if lipgloss.Width(view) > size[0] || lipgloss.Height(view) > size[1] {
			t.Fatalf("overflow at %v: %dx%d", size, lipgloss.Width(view), lipgloss.Height(view))
		}
		if size[0] < 54 {
			continue
		}
		plain := ansi.Strip(view)
		// The subject and the graph survive every width that renders at all.
		if !strings.Contains(plain, "Bound captured") {
			t.Fatalf("subject dropped at width %d", size[0])
		}
		if !strings.Contains(plain, "●") {
			t.Fatalf("graph dropped at width %d", size[0])
		}
	}
}

func TestHistoryDetachedHead(t *testing.T) {
	m := historyModel(t)
	uiGit(t, m.repo.Root, "switch", "--detach", "HEAD~2")
	drain(t, m, m.openHistory())
	if !m.detached() {
		t.Fatalf("detached HEAD not detected: %+v", m.head)
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "DETACHED HEAD") {
		t.Fatal("header does not announce a detached HEAD")
	}
	if !strings.Contains(view, m.head.Short) {
		t.Fatal("header does not show the detached commit")
	}
	if strings.Contains(view, "/  main") {
		t.Fatal("header still presents the repository as being on a branch")
	}
	// Branch creation must still work from here.
	drain(t, m, m.promptBranchFromSelection())
	m.prompt.value = "from-detached"
	drain(t, m, m.submitPrompt(false))
	if out := uiGit(t, m.repo.Root, "branch", "--list", "from-detached"); !strings.Contains(out, "from-detached") {
		t.Fatal("could not create a branch while detached")
	}
}

func TestHistoryCopyHashAndBranchPrompt(t *testing.T) {
	m := historyModel(t)
	drain(t, m, m.openHistory())
	m.focus = 1
	key(t, m, "n")
	if m.prompt == nil || m.prompt.kind != promptCreateBranch {
		t.Fatal("n did not open the new-branch prompt")
	}
	c, _ := m.history.current()
	if m.prompt.context != c.OID {
		t.Fatalf("prompt start point %q is not the selected commit", m.prompt.context)
	}
	if !strings.Contains(ansi.Strip(m.View()), "new branch") {
		t.Fatal("prompt not rendered")
	}
	key(t, m, "esc")
	if m.prompt != nil {
		t.Fatal("escape did not close the prompt")
	}
}
