package ui

import (
	"context"
	"fmt"
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

// milestone5Repo builds a working repository with a local bare remote, an
// upstream, a second clone for divergence, and one stash entry. Everything is
// offline.
func milestone5Repo(t *testing.T) (dir, bare, clone string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	root := t.TempDir()
	dir = filepath.Join(root, "work")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	uiGit(t, dir, "init", "-b", "main")
	uiGit(t, dir, "config", "user.name", "TideGit Test")
	uiGit(t, dir, "config", "user.email", "test@example.invalid")
	uiWrite(t, dir, "f", "base\n")
	uiGit(t, dir, "add", "f")
	uiGit(t, dir, "commit", "-m", "base")
	bare = filepath.Join(root, "bare.git")
	uiGit(t, root, "init", "--bare", "-b", "main", bare)
	uiGit(t, dir, "remote", "add", "origin", bare)
	uiGit(t, dir, "push", "-u", "origin", "main")
	clone = filepath.Join(root, "clone")
	uiGit(t, root, "clone", bare, clone)
	uiGit(t, clone, "config", "user.name", "TideGit Clone")
	uiGit(t, clone, "config", "user.email", "clone@example.invalid")
	return dir, bare, clone
}

func milestone5Model(t *testing.T) (*Model, string, string) {
	t.Helper()
	dir, bare, clone := milestone5Repo(t)
	m := New(context.Background(), dir, tideui.CatppuccinMocha)
	m.width, m.height = 132, 34
	drain(t, m, m.Init())
	return m, bare, clone
}

// pushFromClone adds a commit in the clone and publishes it, creating remote
// divergence without touching the working TideGit repository.
func pushFromClone(t *testing.T, clone, subject string) {
	t.Helper()
	uiWrite(t, clone, "f", subject+"\n")
	uiGit(t, clone, "add", "f")
	uiGit(t, clone, "commit", "-m", subject)
	uiGit(t, clone, "push", "origin", "main")
}

func TestRemotesScreenRendersAndFetches(t *testing.T) {
	m, _, clone := milestone5Model(t)
	drain(t, m, m.goToScreen(screenRemotes))
	view := ansi.Strip(m.View())
	for _, want := range []string{"REMOTES", "origin", "TRACKED BRANCHES", "FETCH URL", "LAST FETCH"} {
		if !strings.Contains(view, want) {
			t.Fatalf("remotes view missing %q", want)
		}
	}
	if len(m.remotes.remotes) != 1 || !m.remotes.remotes[0].Default {
		t.Fatalf("remote model: %+v", m.remotes.remotes)
	}

	pushFromClone(t, clone, "clone change")
	drain(t, m, m.fetchRemote("origin"))
	if !m.op.ok || m.op.running {
		t.Fatalf("fetch did not succeed: ok=%v summary=%q", m.op.ok, m.op.summary)
	}
	if !strings.Contains(m.op.summary, "Fetched origin") {
		t.Fatalf("fetch summary: %q", m.op.summary)
	}
	if s := m.status; s.Behind != 1 || s.Ahead != 0 {
		t.Fatalf("ahead/behind not refreshed after fetch: %+v", s)
	}
	if s := m.remotes.remotes[0]; !s.Default {
		t.Fatalf("remote state lost after fetch: %+v", s)
	}
}

func TestFetchAllAndFetchKey(t *testing.T) {
	m, _, clone := milestone5Model(t)
	pushFromClone(t, clone, "clone change")
	key(t, m, "f")
	if !m.op.ok || !strings.Contains(m.op.summary, "Fetched origin") {
		t.Fatalf("f key fetch: ok=%v summary=%q", m.op.ok, m.op.summary)
	}
	drain(t, m, m.fetchAll())
	if !m.op.ok {
		t.Fatalf("fetch all: ok=%v summary=%q", m.op.ok, m.op.summary)
	}
	if !strings.Contains(m.op.summary, "Fetched") && !strings.Contains(m.op.summary, "up to date") {
		t.Fatalf("fetch all summary: %q", m.op.summary)
	}
}

func TestPullFastForwardThroughUI(t *testing.T) {
	m, _, clone := milestone5Model(t)
	pushFromClone(t, clone, "clone change")
	before := m.status.OID
	key(t, m, "p")
	if !m.op.ok || !strings.Contains(m.op.summary, "Fast-forwarded") {
		t.Fatalf("pull: ok=%v summary=%q", m.op.ok, m.op.summary)
	}
	if m.status.OID == before {
		t.Fatal("pull did not advance the branch")
	}
	if s := m.status; s.Ahead != 0 || s.Behind != 0 {
		t.Fatalf("not level after pull: %+v", s)
	}
}

func TestPushSetsUpstreamForNewBranch(t *testing.T) {
	m, _, _ := milestone5Model(t)
	uiGit(t, m.repo.Root, "switch", "-c", "feature/work")
	uiWrite(t, m.repo.Root, "f", "branch change\n")
	uiGit(t, m.repo.Root, "add", "f")
	uiGit(t, m.repo.Root, "commit", "-m", "branch change")
	drain(t, m, m.refresh())
	if m.status.Upstream != "" {
		t.Fatalf("unexpected upstream: %q", m.status.Upstream)
	}
	drain(t, m, m.pushCurrent(true))
	if !m.op.ok {
		t.Fatalf("push and set upstream failed: %q", m.op.summary)
	}
	if m.status.Upstream != "origin/feature/work" {
		t.Fatalf("upstream not recorded: %q", m.status.Upstream)
	}
}

func TestPushRejectedOpensDetailsPanel(t *testing.T) {
	m, _, clone := milestone5Model(t)
	uiWrite(t, m.repo.Root, "f", "local only\n")
	uiGit(t, m.repo.Root, "add", "f")
	uiGit(t, m.repo.Root, "commit", "-m", "local only")
	pushFromClone(t, clone, "remote change")
	drain(t, m, m.pushCurrent(false))
	if m.op.ok || !m.op.show {
		t.Fatalf("rejected push did not open the panel: %+v", m.op)
	}
	if !strings.Contains(m.op.summary, "Push rejected") {
		t.Fatalf("rejection wording: %q", m.op.summary)
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "failed") || !strings.Contains(view, "tidegit · push") {
		t.Fatalf("operation panel not rendered: %s", view)
	}
	// The failure is explained, and the raw Git output stays available.
	key(t, m, "e")
	if !strings.Contains(ansi.Strip(m.View()), "non-fast-forward") && !strings.Contains(ansi.Strip(m.View()), "rejected") {
		t.Fatal("raw Git output not reachable from the panel")
	}
}

func TestPullWithoutUpstreamOffersChoice(t *testing.T) {
	m, _, _ := milestone5Model(t)
	uiGit(t, m.repo.Root, "switch", "-c", "detached-work")
	uiWrite(t, m.repo.Root, "f", "new\n")
	uiGit(t, m.repo.Root, "add", "f")
	uiGit(t, m.repo.Root, "commit", "-m", "new")
	drain(t, m, m.refresh())
	drain(t, m, m.pullCurrent())
	if m.choice == nil {
		t.Fatal("pull with no upstream did not offer a deliberate choice")
	}
	if m.choice.kind != choicePushRemote || !m.choice.setUpstream {
		t.Fatalf("wrong choice: %+v", m.choice)
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(squeeze(view), "hasnoupstream") || !strings.Contains(squeeze(view), "choosearemotetopush") {
		t.Fatalf("missing-upstream state not explained: %s", view)
	}
}

func TestStashScreenLifecycle(t *testing.T) {
	m, _, _ := milestone5Model(t)
	uiWrite(t, m.repo.Root, "f", "stashed\n")
	uiGit(t, m.repo.Root, "stash", "push", "-m", "wip: parser")
	drain(t, m, m.goToScreen(screenStash))
	if len(m.stash.stashes) != 1 {
		t.Fatalf("stash list: %+v (%s)", m.stash.stashes, m.stash.err)
	}
	if m.stash.filesFor != "stash@{0}" || len(m.stash.files) == 0 {
		t.Fatalf("stash files not loaded: %+v", m.stash)
	}
	view := ansi.Strip(m.View())
	for _, want := range []string{"STASHES", "stash@{0}", "wip: parser", "main", "Stash stash@{0}"} {
		if !strings.Contains(view, want) {
			t.Fatalf("stash view missing %q", want)
		}
	}

	// Apply restores the changes and keeps the entry.
	drain(t, m, m.applyStash())
	if !m.op.ok {
		t.Fatalf("apply failed: %q", m.op.summary)
	}
	if s := m.status; len(s.Groups[git.Unstaged]) != 1 {
		t.Fatalf("apply did not restore changes: %+v", s)
	}
	if len(m.stash.stashes) != 1 {
		t.Fatalf("apply removed the stash: %+v", m.stash.stashes)
	}

	// Discard the restored change and pop: changes return, entry is gone.
	uiGit(t, m.repo.Root, "checkout", "--", "f")
	drain(t, m, m.popStash())
	if !m.op.ok {
		t.Fatalf("pop failed: %q", m.op.summary)
	}
	if len(m.stash.stashes) != 0 {
		t.Fatalf("pop left the stash: %+v", m.stash.stashes)
	}
	if s := m.status; len(s.Groups[git.Unstaged]) != 1 {
		t.Fatalf("pop did not restore changes: %+v", s)
	}
}

func TestStashCreateWithMessageAndUntracked(t *testing.T) {
	m, _, _ := milestone5Model(t)
	uiWrite(t, m.repo.Root, "f", "changed\n")
	uiWrite(t, m.repo.Root, "untracked.txt", "new\n")
	drain(t, m, m.promptStash(true))
	if m.prompt == nil || m.prompt.kind != promptStashMessage {
		t.Fatal("stash prompt did not open")
	}
	m.prompt.value = "named stash"
	drain(t, m, m.submitPrompt(false))
	if !m.op.ok {
		t.Fatalf("stash create failed: %q", m.op.summary)
	}
	if len(m.stash.stashes) != 1 {
		t.Fatalf("stash not created: %+v", m.stash.stashes)
	}
	stash := m.stash.stashes[0]
	if stash.Message != "named stash" {
		t.Fatalf("message not preserved: %+v", stash)
	}
	if s := m.status; len(s.Groups[git.Untracked]) != 0 || len(s.Groups[git.Unstaged]) != 0 {
		t.Fatalf("working tree not cleaned: %+v", s)
	}
}

func TestStashDropRequiresConfirmationAndHitsTheRightEntry(t *testing.T) {
	m, _, _ := milestone5Model(t)
	for _, message := range []string{"first", "second", "third"} {
		uiWrite(t, m.repo.Root, "f", message+"\n")
		uiGit(t, m.repo.Root, "stash", "push", "-m", message)
	}
	drain(t, m, m.goToScreen(screenStash))
	if len(m.stash.stashes) != 3 {
		t.Fatalf("setup stashes: %+v", m.stash.stashes)
	}
	// Select the middle entry by name and drop it.
	for i, stash := range m.stash.stashes {
		if stash.Message == "second" {
			m.stash.index = i
			m.stash.selectedOID = stash.OID
		}
	}
	target := m.stash.stashes[m.stash.index]
	drain(t, m, m.confirmDropStash())
	if m.confirm == nil || m.confirm.kind != confirmDropStash || !m.confirm.danger {
		t.Fatalf("drop did not confirm: %+v", m.confirm)
	}
	if !strings.Contains(ansi.Strip(m.View()), "Drop stash@{") {
		t.Fatal("confirmation does not name the stash")
	}
	key(t, m, "esc")
	if len(m.stash.stashes) != 3 {
		t.Fatal("cancelled drop removed an entry")
	}
	drain(t, m, m.confirmDropStash())
	key(t, m, "d")
	if len(m.stash.stashes) != 2 {
		t.Fatalf("drop did not remove one entry: %+v", m.stash.stashes)
	}
	for _, stash := range m.stash.stashes {
		if stash.OID == target.OID {
			t.Fatalf("dropped the wrong entry: %+v", stash)
		}
	}
}

func TestStashConflictIsSurfacedNotHidden(t *testing.T) {
	m, _, _ := milestone5Model(t)
	uiWrite(t, m.repo.Root, "f", "stashed\n")
	uiGit(t, m.repo.Root, "stash", "push", "-m", "conflicting")
	uiWrite(t, m.repo.Root, "f", "local\n")
	uiGit(t, m.repo.Root, "add", "f")
	uiGit(t, m.repo.Root, "commit", "-m", "local")
	drain(t, m, m.goToScreen(screenStash))
	drain(t, m, m.applyStash())
	if m.op.ok || !m.op.show {
		t.Fatalf("conflict was not surfaced: %+v", m.op)
	}
	if !strings.Contains(m.op.summary, "conflict") {
		t.Fatalf("conflict wording: %q", m.op.summary)
	}
	if s := m.status; len(s.Groups[git.Conflicted]) != 1 {
		t.Fatalf("conflict not present in status: %+v", s)
	}
	// Git preserves the stash on a conflicted apply, and so does TideGit.
	if len(m.stash.stashes) != 1 {
		t.Fatalf("stash lost on conflict: %+v", m.stash.stashes)
	}
}

func TestStashPopKeyDoesNotPull(t *testing.T) {
	m, _, _ := milestone5Model(t)
	uiWrite(t, m.repo.Root, "f", "stashed\n")
	uiGit(t, m.repo.Root, "stash", "push", "-m", "wip")
	drain(t, m, m.goToScreen(screenStash))
	key(t, m, "p")
	if m.op == nil || m.op.kind != "stash" || !strings.Contains(m.op.title, "Pop") {
		t.Fatalf("p on the stash screen did not pop: %+v", m.op)
	}
	if len(m.stash.stashes) != 0 {
		t.Fatalf("pop did not remove the entry: %+v", m.stash.stashes)
	}
}

func TestPaletteOffersMilestone5Commands(t *testing.T) {
	m, _, _ := milestone5Model(t)
	drain(t, m, m.openPalette())
	titles := map[string]bool{}
	for _, item := range m.palette.items {
		titles[item.title] = true
	}
	for _, want := range []string{"Fetch", "Fetch all remotes", "Pull", "Push",
		"Stash changes", "Stash including untracked", "View stashes", "View remotes"} {
		if !titles[want] {
			t.Fatalf("palette missing %q", want)
		}
	}
	// Apply/pop/drop are only meaningful on the Stash screen.
	for _, item := range m.palette.items {
		if item.action == paletteApplyStash {
			t.Fatal("stash-only command offered on Status")
		}
	}
	key(t, m, "esc")
	drain(t, m, m.goToScreen(screenStash))
	drain(t, m, m.openPalette())
	found := false
	for _, item := range m.palette.items {
		if item.action == paletteApplyStash {
			found = true
		}
	}
	if !found {
		t.Fatal("Apply stash not offered on the Stash screen")
	}
}

func TestNewScreensRenderAtEverySize(t *testing.T) {
	m, _, _ := milestone5Model(t)
	uiWrite(t, m.repo.Root, "f", "stashed\n")
	uiGit(t, m.repo.Root, "stash", "push", "-m", "wip")
	for _, screen := range []screen{screenStash, screenRemotes} {
		drain(t, m, m.goToScreen(screen))
		for _, size := range [][2]int{{160, 40}, {132, 34}, {100, 28}, {80, 24}, {60, 20}, {54, 16}, {40, 12}, {1, 1}} {
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			view := m.View()
			if w, h := lipgloss.Width(view), lipgloss.Height(view); w > size[0] || h > size[1] {
				t.Fatalf("screen %d overflow at %v: %dx%d", screen, size, w, h)
			}
		}
	}
}

func TestOperationPanelRendersRunningState(t *testing.T) {
	m, _, _ := milestone5Model(t)
	m.op = &operationState{kind: "fetch", verb: "Fetching", title: "Fetch origin",
		target: "origin", running: true, cancellable: true, raw: &progressBuffer{}}
	m.op.raw.write("Receiving objects: 42% (12/28)\r")
	m.op.show = true
	m.busy = true
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "Fetching") && !strings.Contains(view, "Fetch origin") {
		t.Fatalf("running panel missing its title: %s", view)
	}
	if !strings.Contains(view, "Receiving objects") {
		t.Fatal("progress line not shown while running")
	}
}

func TestRemotePanelsScroll(t *testing.T) {
	m, _, _ := milestone5Model(t)
	oid := strings.TrimSpace(uiGit(t, m.repo.Root, "rev-parse", "HEAD"))
	for i := 0; i < 14; i++ {
		uiGit(t, m.repo.Root, "update-ref",
			fmt.Sprintf("refs/remotes/origin/branch-%02d", i), oid)
	}
	drain(t, m, m.goToScreen(screenRemotes))
	drain(t, m, m.loadRemotes())
	if got := len(m.remotes.selectedBranches()); got < 12 {
		t.Fatalf("expected many tracked branches, got %d", got)
	}
	m.height = 20

	// The tracked-branch pane scrolls with j/k and keeps the cursor visible.
	m.focus = 1
	for i := 0; i < 10; i++ {
		key(t, m, "j")
	}
	if m.remotes.branchIndex != 10 {
		t.Fatalf("branch cursor did not move: %d", m.remotes.branchIndex)
	}
	view := ansi.Strip(m.View())
	if m.remotes.branchTop == 0 {
		t.Fatal("tracked-branch pane did not scroll")
	}
	selected := m.remotes.selectedBranches()[m.remotes.branchIndex]
	if !strings.Contains(view, strings.TrimPrefix(selected.Name, "origin/")) {
		t.Fatalf("selected branch %q not visible after scrolling", selected.Name)
	}

	// The inspector scrolls too.
	m.focus = 2
	key(t, m, "j")
	if m.remotes.inspect.Offset() == 0 {
		t.Fatal("inspector did not scroll")
	}
	key(t, m, "home")
	if m.remotes.inspect.Offset() != 0 {
		t.Fatal("Home did not return the inspector to the top")
	}
}

func TestContextualHelpForNewScreens(t *testing.T) {
	m, _, _ := milestone5Model(t)
	key(t, m, "4")
	key(t, m, "?")
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "STASHES") || !strings.Contains(view, "apply") {
		t.Fatalf("stash help: %s", view)
	}
	key(t, m, "?")
	key(t, m, "5")
	key(t, m, "?")
	view = ansi.Strip(m.View())
	if !strings.Contains(view, "REMOTES") || !strings.Contains(view, "fetch") {
		t.Fatalf("remotes help: %s", view)
	}
}
