package ui

import (
	"context"
	"fmt"
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

func uiGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return string(out)
}
func uiWrite(t *testing.T, dir, path, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, path), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}
func stagingModel(t *testing.T) (*Model, string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := filepath.Join(t.TempDir(), "tidegit")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	uiGit(t, dir, "init", "-b", "main")
	uiGit(t, dir, "config", "user.name", "TideGit Test")
	uiGit(t, dir, "config", "user.email", "test@example.invalid")
	var base strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&base, "line %d\n", i)
	}
	uiWrite(t, dir, "f", base.String())
	uiGit(t, dir, "add", "f")
	uiGit(t, dir, "commit", "-m", "baseline")
	changed := strings.ReplaceAll(strings.ReplaceAll(base.String(), "line 3\n", "first\n"), "line 33\n", "second\n")
	uiWrite(t, dir, "f", changed)
	m := New(context.Background(), dir, tideui.CatppuccinMocha)
	drain(t, m, m.Init())
	return m, changed
}
func drain(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for i := 0; len(queue) > 0; i++ {
		if i > 100 {
			t.Fatal("command loop")
		}
		cmd = queue[0]
		queue = queue[1:]
		if cmd == nil {
			continue
		}
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, batch...)
			continue
		}
		_, next := m.Update(msg)
		if next != nil {
			queue = append(queue, next)
		}
	}
}
func key(t *testing.T, m *Model, k string) {
	t.Helper()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
	drain(t, m, cmd)
}

func TestStagingActionWorkflow(t *testing.T) {
	m, changed := stagingModel(t)
	m.focus = 2
	if m.section != int(git.Unstaged) || len(m.diff.Hunks) != 2 {
		t.Fatal("bad initial selection")
	}
	key(t, m, "]")
	if m.hunk != 1 {
		t.Fatal("next hunk")
	}
	key(t, m, "S")
	if m.err != "" || m.busy || m.focus != 2 || m.section != int(git.Unstaged) || len(m.diff.Hunks) != 1 || !strings.Contains(m.notice, "Staged hunk 2") {
		t.Fatalf("hunk action: err=%s notice=%s section=%d", m.err, m.notice, m.section)
	}
	if len(m.status.Groups[git.Staged]) != 1 || len(m.status.Groups[git.Unstaged]) != 1 {
		t.Fatal("no mixed state")
	}
	key(t, m, "S")
	if m.section != int(git.Staged) || m.files()[m.selected].Path != "f" {
		t.Fatal("did not follow final hunk to staged")
	}
	key(t, m, "U")
	if m.section != int(git.Staged) || len(m.diff.Hunks) != 1 {
		t.Fatal("did not preserve other staged hunk")
	}
	key(t, m, "u")
	if m.section != int(git.Unstaged) || m.files()[m.selected].Path != "f" || m.focus != 2 {
		t.Fatal("whole unstage did not follow path")
	}
	key(t, m, "s")
	if m.section != int(git.Staged) {
		t.Fatal("whole stage did not follow path")
	}
	b, err := os.ReadFile(filepath.Join(m.repo.Root, "f"))
	if err != nil || string(b) != changed {
		t.Fatal("UI workflow changed working tree")
	}
}

func TestMutationGateAndStaleRead(t *testing.T) {
	m, _ := stagingModel(t)
	oldID := m.diffID
	cmd := m.act(StageHunk)
	if cmd == nil || !m.busy {
		t.Fatal("mutation did not start")
	}
	if m.act(StageFile) != nil || m.act(UnstageFile) != nil || m.refresh() != nil {
		t.Fatal("overlapping mutation or refresh")
	}
	m.Update(diffMsg{id: oldID, err: fmt.Errorf("old failure")})
	if m.err != "" {
		t.Fatal("old diff replaced mutation")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.focus != 1 {
		t.Fatal("UI blocked focus while busy")
	}
	drain(t, m, cmd)
	if m.busy || m.err != "" {
		t.Fatalf("operation failed: %s", m.err)
	}
}

func TestStaleHunkErrorPersistsAndRefreshesStatus(t *testing.T) {
	m, _ := stagingModel(t)
	uiWrite(t, m.repo.Root, "f", "changed externally\n")
	key(t, m, "S")
	if !strings.Contains(m.err, "stale") || m.busy {
		t.Fatalf("missing stale failure: %s", m.err)
	}
	if m.act(StageFile) != nil {
		t.Fatal("mutated without acknowledging error")
	}
	key(t, m, "r")
	if m.err != "" || !strings.Contains(m.diff.Patch, "changed externally") {
		t.Fatal("refresh did not recover")
	}
}

func TestSelectionAndHunkRenderingAcrossSizes(t *testing.T) {
	m, _ := stagingModel(t)
	key(t, m, "]")
	for _, size := range [][2]int{{120, 30}, {80, 24}, {65, 12}, {40, 10}, {8, 4}, {1, 1}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		v := m.View()
		if lipgloss.Width(v) > size[0] || lipgloss.Height(v) > size[1] {
			t.Fatalf("overflow %v", size)
		}
	}
	m.width = 120
	m.height = 30
	v := m.View()
	if !strings.Contains(v, "Hunk 2/2") || !strings.Contains(v, "> @@") {
		t.Fatal("selected hunk is not visibly marked")
	}
	key(t, m, "?")
	help := ansi.Strip(m.View())
	for _, hint := range []string{"s / u", "S / U", "[ / ]", "compose a commit", "amend HEAD", "working-tree"} {
		if !strings.Contains(help, hint) {
			t.Fatalf("missing help: %s", hint)
		}
	}
	key(t, m, "?")
	if m.help {
		t.Fatal("help did not close")
	}
	m.height = 20
	key(t, m, "?")
	compact := ansi.Strip(m.View())
	for _, hint := range []string{"s/u file", "S/U hunk", "c commit", "A amend"} {
		if !strings.Contains(compact, hint) {
			t.Fatalf("missing compact help: %s", hint)
		}
	}
	key(t, m, "?")
}

func TestFollowUntrackedAndPreserveOtherSelection(t *testing.T) {
	m, _ := stagingModel(t)
	uiWrite(t, m.repo.Root, "new", "new\n")
	key(t, m, "r")
	m.section = int(git.Untracked)
	m.selected = 0
	drain(t, m, m.loadDiff())
	key(t, m, "s")
	if m.section != int(git.Staged) || m.files()[m.selected].Path != "new" {
		t.Fatal("untracked stage lost selection")
	}
	key(t, m, "u")
	if m.section != int(git.Untracked) || m.files()[m.selected].Path != "new" {
		t.Fatal("added unstage lost selection")
	}
}

func TestInitialSelectionDoesNotMatchEmptyRenameSource(t *testing.T) {
	m := New(context.Background(), ".", tideui.CatppuccinMocha)
	m.status.Groups[git.Staged] = []git.File{{Path: "new", OriginalPath: "old", XY: "R."}}
	m.status.Groups[git.Unstaged] = []git.File{{Path: "other", XY: ".M"}}
	m.selectPath("", int(git.Staged))
	if m.section != int(git.Staged) {
		t.Fatal("empty path matched an unrelated file's empty rename source")
	}
}
