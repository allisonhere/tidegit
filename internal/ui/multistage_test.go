package ui

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/allisonhere/tidegit/internal/git"
	"github.com/allisonhere/tideui"
	"github.com/charmbracelet/x/ansi"
)

// multiStageModel has one commit and three untracked files, ready to stage in a
// batch.
func multiStageModel(t *testing.T) *Model {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := filepath.Join(t.TempDir(), "multi")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	uiGit(t, dir, "init", "-b", "main")
	uiGit(t, dir, "config", "user.name", "TideGit Test")
	uiGit(t, dir, "config", "user.email", "test@example.invalid")
	uiWrite(t, dir, "base", "base\n")
	uiGit(t, dir, "add", "base")
	uiGit(t, dir, "commit", "-m", "base")
	for _, name := range []string{"a", "b", "c"} {
		uiWrite(t, dir, name, name+"\n")
	}
	m := New(context.Background(), dir, tideui.CatppuccinMocha)
	m.width, m.height = 132, 34
	drain(t, m, m.Init())
	return m
}

func markPaths(t *testing.T, m *Model, section int, paths ...string) {
	t.Helper()
	m.section, m.focus = section, 1
	visible := m.files()
	for _, path := range paths {
		found := false
		for i, f := range visible {
			if f.Path == path {
				m.selected = i
				m.toggleMark()
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("no file %q in section %d: %+v", path, section, visible)
		}
	}
}

func groupPaths(files []git.File) []string {
	var out []string
	for _, f := range files {
		out = append(out, f.Path)
	}
	sort.Strings(out)
	return out
}

func TestMarkedFilesStageTogether(t *testing.T) {
	m := multiStageModel(t)
	if len(m.status.Groups[git.Untracked]) != 3 {
		t.Fatalf("setup: %+v", m.status.Groups[git.Untracked])
	}
	markPaths(t, m, int(git.Untracked), "a", "b")
	if m.markedCount(int(git.Untracked)) != 2 {
		t.Fatalf("marked count: %d", m.markedCount(int(git.Untracked)))
	}

	// The view shows the marks and the count.
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "✓") || !strings.Contains(view, "2 marked") {
		t.Fatalf("marks not visible: %s", view)
	}

	drain(t, m, m.act(StageFile))
	if got := groupPaths(m.status.Groups[git.Staged]); strings.Join(got, ",") != "a,b" {
		t.Fatalf("staged: %v", got)
	}
	if got := groupPaths(m.status.Groups[git.Untracked]); strings.Join(got, ",") != "c" {
		t.Fatalf("untracked: %v", got)
	}
	if m.markedCount(int(git.Untracked)) != 0 {
		t.Fatal("marks were not cleared after staging")
	}
	if !strings.Contains(m.notice, "Staged 2 files") {
		t.Fatalf("notice: %q", m.notice)
	}
}

func TestMarkedFilesUnstageTogether(t *testing.T) {
	m := multiStageModel(t)
	drain(t, m, m.stageAllInSection())
	if len(m.status.Groups[git.Staged]) != 3 {
		t.Fatalf("stage all: %+v", m.status.Groups[git.Staged])
	}
	markPaths(t, m, int(git.Staged), "b")
	drain(t, m, m.act(UnstageFile))
	if got := groupPaths(m.status.Groups[git.Staged]); strings.Join(got, ",") != "a,c" {
		t.Fatalf("staged after unstage: %v", got)
	}
	if !strings.Contains(m.notice, "Unstaged 1 file") {
		t.Fatalf("notice: %q", m.notice)
	}
}

func TestSingleStageStillWorksWithoutMarks(t *testing.T) {
	m := multiStageModel(t)
	m.section, m.focus = int(git.Untracked), 1
	m.selected = 0
	drain(t, m, m.act(StageFile))
	if len(m.status.Groups[git.Staged]) != 1 {
		t.Fatalf("single stage staged %d files", len(m.status.Groups[git.Staged]))
	}
}

func TestStageAllInSection(t *testing.T) {
	m := multiStageModel(t)
	drain(t, m, m.stageAllInSection())
	if len(m.status.Groups[git.Staged]) != 3 || len(m.status.Groups[git.Untracked]) != 0 {
		t.Fatalf("stage all: %+v", m.status.Groups)
	}
	drain(t, m, m.unstageAllInSection())
	if len(m.status.Groups[git.Staged]) != 0 {
		t.Fatalf("unstage all left staged: %+v", m.status.Groups[git.Staged])
	}
}

func TestClearMarks(t *testing.T) {
	m := multiStageModel(t)
	markPaths(t, m, int(git.Untracked), "a", "b", "c")
	m.clearSectionMarks(int(git.Untracked))
	if m.markedCount(int(git.Untracked)) != 0 {
		t.Fatal("marks not cleared")
	}
}

func TestMarksSurviveRefresh(t *testing.T) {
	m := multiStageModel(t)
	markPaths(t, m, int(git.Untracked), "a")
	drain(t, m, m.refresh())
	if m.markedCount(int(git.Untracked)) != 1 {
		t.Fatal("marks lost on refresh")
	}
}
