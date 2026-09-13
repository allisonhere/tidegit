package ui

import (
	"context"
	"strings"
	"testing"

	"github.com/allisonhere/tidegit/internal/git"
	"github.com/allisonhere/tideui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestStaleResultsIgnored(t *testing.T) {
	m := New(context.Background(), ".", tideui.CatppuccinMocha)
	m.scanID = 3
	m.diffID = 5
	m.Update(statusMsg{id: 2, status: git.Status{Branch: "wrong"}})
	m.Update(diffMsg{id: 4, diff: git.Diff{Patch: "wrong"}})
	if m.status.Branch != "" || m.diff.Patch != "" {
		t.Fatal("stale result replaced current selection")
	}
}
func TestFocusAndFiltering(t *testing.T) {
	m := New(context.Background(), ".", tideui.CatppuccinMocha)
	m.loading = true
	m.status.Groups[0] = []git.File{{Path: "one.go"}, {Path: "two.txt"}}
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.focus != 1 {
		t.Fatal("tab focus")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("go")})
	if len(m.files()) != 1 || m.files()[0].Path != "one.go" {
		t.Fatal("filter")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if len(m.files()) != 2 || m.filtering {
		t.Fatal("escape filter")
	}
}
func TestRenderingBoundedAndSafe(t *testing.T) {
	m := New(context.Background(), ".", tideui.CatppuccinMocha)
	m.status.Branch = "main"
	m.status.Groups[0] = []git.File{{Path: "evil\x1b]52;c;test\a"}}
	m.diff = git.Diff{Patch: "@@ -1 +1 @@\n-old\n+new\x1b[2J\n"}
	m.lines = diffLines(m.diff, true)
	for _, size := range [][2]int{{120, 30}, {65, 12}, {40, 10}, {8, 4}, {1, 1}} {
		m.width, m.height = size[0], size[1]
		v := m.View()
		if lipgloss.Width(v) > m.width || lipgloss.Height(v) > m.height {
			t.Fatalf("overflow %v: %dx%d", size, lipgloss.Width(v), lipgloss.Height(v))
		}
		if strings.Contains(v, "\x1b]52") || strings.Contains(v, "\x1b[2J") {
			t.Fatal("terminal injection")
		}
	}
}
func TestDiffLineNumbers(t *testing.T) {
	lines := diffLines(git.Diff{Patch: "@@ -10,2 +20,2 @@\n-old\n+new\n same\n"}, true)
	if !strings.Contains(lines[1].text, "10") || !strings.Contains(lines[2].text, "20") || !strings.Contains(lines[3].text, "11    21") {
		t.Fatalf("%+v", lines)
	}
}

func TestPatchContentResemblingHeaders(t *testing.T) {
	lines := diffLines(git.Diff{Patch: "@@ -1 +1 @@\n--- content\n+++ content\n"}, true)
	if lines[1].kind != '-' || lines[2].kind != '+' || !strings.Contains(lines[2].text, "1") {
		t.Fatalf("content mistaken for header: %+v", lines)
	}
}

func TestPreviewLineLimit(t *testing.T) {
	lines := diffLines(git.Diff{Patch: strings.Repeat("+x\n", 25000)}, true)
	if len(lines) != 20001 || !strings.Contains(lines[len(lines)-1].text, "limited") {
		t.Fatalf("unbounded preview: %d", len(lines))
	}
}

func TestRefreshPreservesSelection(t *testing.T) {
	m := New(context.Background(), ".", tideui.CatppuccinMocha)
	m.status.Groups[0] = []git.File{{Path: "b"}, {Path: "c"}}
	m.selected = 1
	s := git.Status{}
	s.Groups[0] = []git.File{{Path: "a"}, {Path: "b"}, {Path: "c"}}
	_, cmd := m.Update(statusMsg{id: 0, status: s, repo: git.Repository{Root: "."}})
	if cmd == nil || m.selected != 2 || !m.diffLoading {
		t.Fatal("refresh lost selected path")
	}
	if m.diffCancel != nil {
		m.diffCancel()
	}
}
