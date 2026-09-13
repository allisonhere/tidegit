package ui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/allisonhere/tidegit/internal/git"
	"github.com/allisonhere/tideui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// runGitTolerant runs a Git command that is expected to fail (a conflicting
// merge, for example) without failing the test.
func runGitTolerant(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	_ = cmd.Run()
}

func conflictRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := filepath.Join(t.TempDir(), "conflict")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	uiGit(t, dir, "init", "-b", "main")
	uiGit(t, dir, "config", "user.name", "TideGit Test")
	uiGit(t, dir, "config", "user.email", "test@example.invalid")
	uiGit(t, dir, "config", "merge.conflictStyle", "diff3")
	uiWrite(t, dir, "f", "base\n")
	uiGit(t, dir, "add", "f")
	uiGit(t, dir, "commit", "-m", "base")
	uiGit(t, dir, "checkout", "-b", "other")
	uiWrite(t, dir, "f", "theirs\n")
	uiGit(t, dir, "commit", "-am", "theirs")
	uiGit(t, dir, "checkout", "main")
	uiWrite(t, dir, "f", "ours\n")
	uiGit(t, dir, "commit", "-am", "ours")
	runGitTolerant(t, dir, "merge", "other")
	return dir
}

func conflictModel(t *testing.T) *Model {
	t.Helper()
	m := New(context.Background(), conflictRepo(t), tideui.CatppuccinMocha)
	m.width, m.height = 132, 34
	drain(t, m, m.Init())
	return m
}

func TestConflictScreenShowsAndResolves(t *testing.T) {
	m := conflictModel(t)
	if m.repoState.Operation != git.OpMerge || !m.repoState.InProgress() {
		t.Fatalf("merge state not detected on load: %+v", m.repoState)
	}
	// The global ribbon names the operation without a red wall.
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "MERGE IN PROGRESS") {
		t.Fatalf("operation ribbon missing: %s", view)
	}

	drain(t, m, m.goToScreen(screenConflicts))
	if len(m.conflicts.conflicts) != 1 {
		t.Fatalf("conflicts: %+v", m.conflicts.conflicts)
	}
	if m.conflicts.conflicts[0].Kind != git.ConflictBothModified {
		t.Fatalf("kind: %+v", m.conflicts.conflicts[0])
	}
	if m.conflicts.detailFor != "f" || len(m.conflicts.regions) != 1 {
		t.Fatalf("detail: %+v", m.conflicts)
	}
	if !m.conflicts.stages.HasBase() {
		t.Fatal("base stage not available in a diff3 conflict")
	}
	view = ansi.Strip(m.View())
	for _, want := range []string{"CONFLICTS", "REGIONS", "CONFLICT REGION", "OURS", "THEIRS", "BASE"} {
		if !strings.Contains(view, want) {
			t.Fatalf("conflicts view missing %q", want)
		}
	}

	// Writing a side does not resolve the index on its own.
	drain(t, m, m.resolveSelected(true, false))
	if got := readFile(t, m.repo.Root, "f"); strings.TrimSpace(got) != "ours" {
		t.Fatalf("ours not written: %q", got)
	}
	if len(m.conflicts.conflicts) != 1 {
		t.Fatal("writing a side resolved the index unexpectedly")
	}
	if !strings.Contains(m.notice, "Applied ours") {
		t.Fatalf("no feedback: %q", m.notice)
	}

	// Marking resolved clears the conflict; the screen shows the calm success
	// state and the continue action.
	drain(t, m, m.markSelectedResolved())
	if len(m.conflicts.conflicts) != 0 {
		t.Fatalf("conflict not cleared: %+v", m.conflicts.conflicts)
	}
	view = ansi.Strip(m.View())
	if !strings.Contains(view, "All conflicts resolved") || !strings.Contains(view, "continue") {
		t.Fatalf("success state missing: %s", view)
	}

	drain(t, m, m.continueOperation())
	if m.repoState.InProgress() {
		t.Fatalf("merge still in progress: %+v", m.repoState)
	}
	if parents := strings.Fields(uiGit(t, m.repo.Root, "rev-list", "--parents", "-n", "1", "HEAD")); len(parents) != 3 {
		t.Fatalf("continue did not create the merge commit: %v", parents)
	}
}

func TestConflictAcceptTheirsAndResolve(t *testing.T) {
	m := conflictModel(t)
	drain(t, m, m.goToScreen(screenConflicts))
	drain(t, m, m.resolveSelected(false, true))
	if got := readFile(t, m.repo.Root, "f"); strings.TrimSpace(got) != "theirs" {
		t.Fatalf("theirs not written: %q", got)
	}
	if len(m.conflicts.conflicts) != 0 {
		t.Fatalf("not resolved: %+v", m.conflicts.conflicts)
	}
	if !strings.Contains(m.notice, "Resolved") {
		t.Fatalf("feedback: %q", m.notice)
	}
}

func TestConflictKeepBothAndInspectorModes(t *testing.T) {
	m := conflictModel(t)
	drain(t, m, m.goToScreen(screenConflicts))

	// The inspector cycles through labelled views.
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "CONFLICT REGION") {
		t.Fatalf("default inspector mode: %s", view)
	}
	key(t, m, "v")
	if !strings.Contains(ansi.Strip(m.View()), "WORKING RESULT") {
		t.Fatal("working mode not reached")
	}
	key(t, m, "v")
	if !strings.Contains(ansi.Strip(m.View()), "OURS") {
		t.Fatal("ours/base mode not reached")
	}

	// Keep both is a predictable concatenation, left unstaged.
	drain(t, m, m.keepBothSelected())
	got := readFile(t, m.repo.Root, "f")
	if !strings.Contains(got, "ours") || !strings.Contains(got, "theirs") {
		t.Fatalf("keep both: %q", got)
	}
	if strings.Contains(got, "<<<<<<<") || strings.Contains(got, "=======") {
		t.Fatalf("markers remain: %q", got)
	}
	if len(m.conflicts.conflicts) != 1 {
		t.Fatal("keep both marked the file resolved")
	}
}

func TestConflictAbortFlow(t *testing.T) {
	m := conflictModel(t)
	drain(t, m, m.goToScreen(screenConflicts))
	before := m.status.OID
	drain(t, m, m.confirmAbortOperation())
	if m.confirm == nil || m.confirm.accept != "A" || !m.confirm.danger {
		t.Fatalf("abort did not confirm safely: %+v", m.confirm)
	}
	if !strings.Contains(ansi.Strip(m.View()), "Abort the merge") {
		t.Fatal("abort confirmation does not name the operation")
	}
	key(t, m, "A")
	if m.repoState.InProgress() {
		t.Fatalf("merge not aborted: %+v", m.repoState)
	}
	if after := m.status.OID; after != before {
		t.Fatalf("abort moved HEAD: %s -> %s", before, after)
	}
}

func TestConflictRestartDetection(t *testing.T) {
	dir := conflictRepo(t)
	// Two independent model instances stand in for a restart: nothing about
	// the conflict is remembered in process, so the second must rediscover it.
	first := New(context.Background(), dir, tideui.CatppuccinMocha)
	first.width, first.height = 132, 34
	drain(t, first, first.Init())
	if first.repoState.Operation != git.OpMerge {
		t.Fatalf("first instance: %+v", first.repoState)
	}
	restarted := New(context.Background(), dir, tideui.CatppuccinMocha)
	restarted.width, restarted.height = 132, 34
	drain(t, restarted, restarted.Init())
	if restarted.repoState.Operation != git.OpMerge || restarted.repoState.Conflicts != 1 {
		t.Fatalf("restart did not rediscover state: %+v", restarted.repoState)
	}
	drain(t, restarted, restarted.goToScreen(screenConflicts))
	if len(restarted.conflicts.conflicts) != 1 {
		t.Fatalf("restart conflict list: %+v", restarted.conflicts.conflicts)
	}
}

func TestRebaseConflictLabelsFollowOperation(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := filepath.Join(t.TempDir(), "rebase")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	uiGit(t, dir, "init", "-b", "main")
	uiGit(t, dir, "config", "user.name", "TideGit Test")
	uiGit(t, dir, "config", "user.email", "test@example.invalid")
	uiWrite(t, dir, "f", "base\n")
	uiGit(t, dir, "add", "f")
	uiGit(t, dir, "commit", "-m", "base")
	uiGit(t, dir, "checkout", "-b", "feature")
	uiWrite(t, dir, "f", "feature\n")
	uiGit(t, dir, "commit", "-am", "feature")
	uiGit(t, dir, "checkout", "main")
	uiWrite(t, dir, "f", "main\n")
	uiGit(t, dir, "commit", "-am", "main")
	uiGit(t, dir, "checkout", "feature")
	runGitTolerant(t, dir, "rebase", "main")

	m := New(context.Background(), dir, tideui.CatppuccinMocha)
	m.width, m.height = 132, 34
	drain(t, m, m.Init())
	if m.repoState.Operation != git.OpRebase {
		t.Fatalf("rebase state: %+v", m.repoState)
	}
	if m.repoState.Step == 0 || m.repoState.Total == 0 {
		t.Fatalf("rebase progress not shown: %+v", m.repoState)
	}
	drain(t, m, m.goToScreen(screenConflicts))
	if m.conflicts.oursTitle != "UPSTREAM" || m.conflicts.theirsTitle != "YOUR COMMIT" {
		t.Fatalf("rebase side labels not adapted: %+v", m.conflicts)
	}
	if !strings.Contains(ansi.Strip(m.View()), "REBASE 1 /") && !strings.Contains(ansi.Strip(m.View()), "REBASE ") {
		t.Fatalf("rebase ribbon missing: %s", ansi.Strip(m.View()))
	}
	// Skip finishes the rebase.
	drain(t, m, m.skipOperation())
	if m.repoState.InProgress() {
		t.Fatalf("rebase not skipped: %+v", m.repoState)
	}
}

func TestReflogScreenAndRecoveryBranch(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := filepath.Join(t.TempDir(), "reflog")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	uiGit(t, dir, "init", "-b", "main")
	uiGit(t, dir, "config", "user.name", "TideGit Test")
	uiGit(t, dir, "config", "user.email", "test@example.invalid")
	uiWrite(t, dir, "f", "one\n")
	uiGit(t, dir, "add", "f")
	uiGit(t, dir, "commit", "-m", "one")
	uiWrite(t, dir, "f", "two\n")
	uiGit(t, dir, "commit", "-am", "two")
	gitCmdReset(t, dir)

	m := New(context.Background(), dir, tideui.CatppuccinMocha)
	m.width, m.height = 132, 34
	drain(t, m, m.Init())
	drain(t, m, m.goToScreen(screenReflog))
	if len(m.reflog.entries) < 2 {
		t.Fatalf("reflog entries: %+v (%s)", m.reflog.entries, m.reflog.err)
	}
	if !strings.Contains(ansi.Strip(m.View()), "REFLOG") || !strings.Contains(ansi.Strip(m.View()), "reset") {
		t.Fatalf("reflog view: %s", ansi.Strip(m.View()))
	}
	// Pick an older entry and create a branch there: the safe recovery action.
	m.reflog.index = 1
	m.reflog.detailFor = ""
	drain(t, m, m.loadReflogDetail())
	target := m.reflog.entries[1].OID
	drain(t, m, m.promptRecoveryBranch())
	if m.prompt == nil || m.prompt.context != target {
		t.Fatalf("recovery prompt: %+v", m.prompt)
	}
	m.prompt.value = "recover/work"
	drain(t, m, m.submitPrompt(false))
	if got := strings.TrimSpace(uiGit(t, dir, "rev-parse", "recover/work")); got != target {
		t.Fatalf("recovery branch at %s, want %s", got, target)
	}
}

func TestResetAndUndoFlows(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := filepath.Join(t.TempDir(), "reset")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	uiGit(t, dir, "init", "-b", "main")
	uiGit(t, dir, "config", "user.name", "TideGit Test")
	uiGit(t, dir, "config", "user.email", "test@example.invalid")
	uiWrite(t, dir, "f", "one\n")
	uiGit(t, dir, "add", "f")
	uiGit(t, dir, "commit", "-m", "one")
	uiWrite(t, dir, "f", "two\n")
	uiGit(t, dir, "commit", "-am", "two")

	m := New(context.Background(), dir, tideui.CatppuccinMocha)
	m.width, m.height = 132, 34
	drain(t, m, m.Init())

	// Undo last commit, keep changes staged.
	drain(t, m, m.promptUndoLastCommit())
	if m.choice == nil || len(m.choice.options) != 2 {
		t.Fatalf("undo options: %+v", m.choice)
	}
	key(t, m, "enter")
	if m.confirm == nil || m.confirm.danger {
		t.Fatalf("soft undo should not be dangerous: %+v", m.confirm)
	}
	key(t, m, "R")
	if s := m.status; len(s.Groups[git.Staged]) != 1 || len(s.Groups[git.Unstaged]) != 0 {
		t.Fatalf("soft undo state: %+v", s)
	}

	// Hard reset is destructive and must confirm with different wording.
	gitCmdReset(t, dir)
	drain(t, m, m.refresh())
	drain(t, m, m.confirmReset(git.ResetHard, "HEAD"))
	if m.confirm == nil || !m.confirm.danger {
		t.Fatalf("hard reset not dangerous: %+v", m.confirm)
	}
	if !strings.Contains(ansi.Strip(m.View()), "discard tracked") {
		t.Fatal("hard reset does not warn about discarding work")
	}
}

func TestRevertCommitFlow(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := filepath.Join(t.TempDir(), "revert")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	uiGit(t, dir, "init", "-b", "main")
	uiGit(t, dir, "config", "user.name", "TideGit Test")
	uiGit(t, dir, "config", "user.email", "test@example.invalid")
	uiWrite(t, dir, "f", "one\n")
	uiGit(t, dir, "add", "f")
	uiGit(t, dir, "commit", "-m", "one")
	uiWrite(t, dir, "g", "two\n")
	uiGit(t, dir, "add", "g")
	uiGit(t, dir, "commit", "-m", "two")

	m := New(context.Background(), dir, tideui.CatppuccinMocha)
	m.width, m.height = 132, 34
	drain(t, m, m.Init())
	drain(t, m, m.goToScreen(screenHistory))
	drain(t, m, m.confirmRevertCommit())
	if m.confirm == nil {
		t.Fatal("revert did not confirm")
	}
	if !strings.Contains(ansi.Strip(m.View()), "new commit") {
		t.Fatal("revert does not explain that it is non-destructive")
	}
	key(t, m, "R")
	subject := strings.TrimSpace(uiGit(t, dir, "log", "-1", "--format=%s"))
	if !strings.HasPrefix(subject, "Revert") {
		t.Fatalf("no inverse commit: %q", subject)
	}
}

func TestPaletteMilestone6Commands(t *testing.T) {
	m := conflictModel(t)
	drain(t, m, m.openPalette())
	titles := map[string]bool{}
	for _, item := range m.palette.items {
		titles[item.title] = true
	}
	for _, want := range []string{"Go to Conflicts", "Go to Reflog", "Continue operation",
		"Abort operation", "Reset…", "Undo last commit", "Recovery…"} {
		if !titles[want] {
			t.Fatalf("palette missing %q", want)
		}
	}
	for _, item := range m.palette.items {
		if item.action == paletteAcceptOurs {
			t.Fatal("conflict-only command offered on Status")
		}
	}
	key(t, m, "esc")
	drain(t, m, m.goToScreen(screenConflicts))
	drain(t, m, m.openPalette())
	found := false
	for _, item := range m.palette.items {
		switch item.action {
		case paletteAcceptOurs, paletteAcceptTheirs, paletteMarkResolved, paletteNextConflict:
			found = true
		}
	}
	if !found {
		t.Fatal("conflict commands not offered on the Conflicts screen")
	}
}

func TestConflictsAndReflogRenderAtEverySize(t *testing.T) {
	m := conflictModel(t)
	for _, screen := range []screen{screenConflicts, screenReflog} {
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

// gitCmdReset is a small helper that resets the working tree without a full
// reflog assertion.
func gitCmdReset(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "reset", "--soft", "HEAD")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git reset: %v %s", err, out)
	}
}
