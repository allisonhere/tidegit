package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/allisonhere/tidegit/internal/diff"
	"github.com/allisonhere/tidegit/internal/git"
	"github.com/allisonhere/tideui"
	"github.com/charmbracelet/x/ansi"
)

// diffFixture builds a repository whose single file has two separated hunks and
// Go-like content for highlighting.
func diffFixture(t *testing.T) *Model {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := filepath.Join(t.TempDir(), "diffview")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	uiGit(t, dir, "init", "-b", "main")
	uiGit(t, dir, "config", "user.name", "TideGit Test")
	uiGit(t, dir, "config", "user.email", "test@example.invalid")
	var base strings.Builder
	for i := 0; i < 60; i++ {
		fmt.Fprintf(&base, "func line%d() { return %d }\n", i, i)
	}
	uiWrite(t, dir, "main.go", base.String())
	uiGit(t, dir, "add", "main.go")
	uiGit(t, dir, "commit", "-m", "base")
	changed := strings.Replace(base.String(), "func line5() { return 5 }", "func line5() { return 500 }", 1)
	changed = strings.Replace(changed, "func line50() { return 50 }", "func line50() { return 999 }", 1)
	uiWrite(t, dir, "main.go", changed)
	m := New(context.Background(), dir, tideui.CatppuccinMocha)
	m.width, m.height = 132, 34
	drain(t, m, m.Init())
	return m
}

func TestDiffViewerLoadsAndNavigatesHunks(t *testing.T) {
	m := diffFixture(t)
	if m.err != "" {
		t.Fatalf("diff error: %s", m.err)
	}
	if len(m.diff.Hunks) != 2 || m.view.hunkCount() != 2 {
		t.Fatalf("hunks: git=%d view=%d", len(m.diff.Hunks), m.view.hunkCount())
	}
	// The first hunk is selected and the pane hint reports progress.
	if !strings.Contains(ansi.Strip(m.View()), "Hunk 1/2") {
		t.Fatalf("hunk hint missing: %s", ansi.Strip(m.View()))
	}
	m.moveHunk(1)
	if m.view.hunk != 1 || m.hunk != 1 {
		t.Fatalf("next hunk: view=%d model=%d", m.view.hunk, m.hunk)
	}
	m.moveHunk(-1)
	if m.view.hunk != 0 {
		t.Fatalf("previous hunk: %d", m.view.hunk)
	}
	// Selection lands on the hunk header so it is visibly marked.
	flat := m.view.flat(m.diffOptionsFrom(), true)
	if flat[m.view.line].kind != '@' {
		t.Fatalf("selection not on header: %+v", flat[m.view.line])
	}
}

func TestDiffViewerSplitMode(t *testing.T) {
	m := diffFixture(t)
	m.toggleDiffMode()
	if m.cfg.Diff.Mode != "split" {
		t.Fatalf("mode: %q", m.cfg.Diff.Mode)
	}
	view := ansi.Strip(m.renderDiffView(&m.view, m.renderer(), 120, 20, true))
	if !strings.Contains(view, "│") {
		t.Fatalf("split separator missing:\n%s", view)
	}
	m.toggleDiffMode()
	if m.cfg.Diff.Mode != "unified" {
		t.Fatalf("mode after toggle back: %q", m.cfg.Diff.Mode)
	}
}

func TestDiffViewerSearch(t *testing.T) {
	m := diffFixture(t)
	m.view.searchText(m.diffOptionsFrom(), "line50")
	if len(m.view.matches) == 0 {
		t.Fatal("search found no match")
	}
	first := m.view.line
	m.view.stepMatch(1)
	if len(m.view.matches) > 1 && m.view.line == first {
		t.Fatal("next match did not move")
	}
	m.view.searchText(m.diffOptionsFrom(), "no-such-text-xyz")
	if len(m.view.matches) != 0 {
		t.Fatal("unexpected matches")
	}
}

func TestDiffViewerCollapseAndExpand(t *testing.T) {
	// A synthetic hunk with changes at each end and a long unchanged middle.
	var b strings.Builder
	b.WriteString("diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1,24 +1,24 @@\n")
	b.WriteString("-old head\n+new head\n")
	for i := 0; i < 20; i++ {
		fmt.Fprintf(&b, " context %d\n", i)
	}
	b.WriteString("-old tail\n+new tail\n")
	m := diffFixture(t)
	m.view.reset(diff.Parse(b.String()), "TEST")
	opts := m.diffOptionsFrom()
	flat := m.view.flat(opts, true)
	gap := -1
	for i, rl := range flat {
		if rl.kind == '~' {
			gap = i
		}
	}
	if gap < 0 {
		t.Fatalf("long context did not collapse: %d rows", len(flat))
	}
	m.view.line = gap
	if !m.view.toggleGap(opts) {
		t.Fatal("toggleGap did not expand")
	}
	for _, rl := range m.view.flat(opts, true) {
		if rl.kind == '~' {
			t.Fatal("gap still collapsed after expand")
		}
	}
}

func TestDiffViewerWhitespaceAndSettings(t *testing.T) {
	m := diffFixture(t)
	start := m.cfg.Diff.Whitespace
	m.cycleWhitespaceMode()
	if m.cfg.Diff.Whitespace == start {
		t.Fatal("whitespace mode did not cycle")
	}
	m.toggleDiffSetting("diff.show_whitespace")
	if !m.cfg.Diff.ShowWhitespace {
		t.Fatal("whitespace markers not enabled")
	}
	m.toggleDiffSetting("diff.syntax")
	if m.cfg.Diff.Syntax {
		t.Fatal("syntax not toggled off")
	}
	// Context adjustment is clamped.
	m.setDiffContext(3)
	if m.cfg.Diff.ContextLines != 3 {
		t.Fatalf("context: %d", m.cfg.Diff.ContextLines)
	}
	m.adjustDiffContext(-10)
	if m.cfg.Diff.ContextLines != 0 {
		t.Fatalf("context lower bound: %d", m.cfg.Diff.ContextLines)
	}
}

func TestDiffViewerStagesHunk(t *testing.T) {
	m := diffFixture(t)
	// Select the first hunk and stage it through the shared viewer state.
	m.moveHunk(1)
	m.moveHunk(-1)
	if m.view.hunk != 0 {
		t.Fatalf("hunk selection: %d", m.view.hunk)
	}
	drain(t, m, m.act(StageHunk))
	if m.err != "" {
		t.Fatalf("stage hunk error: %s", m.err)
	}
	if len(m.status.Groups[git.Staged]) == 0 {
		t.Fatal("staging the hunk did not stage anything")
	}
	// One hunk remains unstaged.
	remaining := 0
	for _, f := range m.status.Groups[git.Unstaged] {
		if f.Path == "main.go" {
			remaining++
		}
	}
	if remaining == 0 {
		t.Fatalf("expected the other hunk to remain unstaged: %+v", m.status.Groups[git.Unstaged])
	}
}

func TestApplyOptionsIncludeWhitespace(t *testing.T) {
	m := diffFixture(t)
	opts := m.diffOptionsFrom()
	if opts.mode != "unified" || !opts.numbers || !opts.syntax || !opts.word || !opts.collapse {
		t.Fatalf("default options: %+v", opts)
	}
	if m.whitespaceMode() != git.WhitespaceNormal {
		t.Fatalf("default whitespace: %v", m.whitespaceMode())
	}
}

// TestLargeDiffStaysBounded builds a patch far past the syntax threshold and
// checks that parsing completes and rendering is windowed to the viewport.
func TestLargeDiffStaysBounded(t *testing.T) {
	var b strings.Builder
	b.WriteString("diff --git a/big.txt b/big.txt\n--- a/big.txt\n+++ b/big.txt\n@@ -1,30000 +1,30000 @@\n")
	for i := 0; i < 30000; i++ {
		if i%100 == 0 {
			fmt.Fprintf(&b, "-old line %d\n+new line %d\n", i, i)
		} else {
			fmt.Fprintf(&b, " context line %d\n", i)
		}
	}
	patch := diff.Parse(b.String())
	if patch.TotalLines() < 30000 {
		t.Fatalf("parse lost lines: %d", patch.TotalLines())
	}
	m := diffFixture(t)
	m.view.reset(patch, "LARGE")
	opts := m.diffOptionsFrom()
	if !(patch.TotalLines() > largeDiffLines) {
		t.Fatal("threshold not exceeded")
	}
	out := m.renderDiffView(&m.view, m.renderer(), 100, 20, true)
	if got := strings.Count(out, "\n") + 1; got > 21 {
		t.Fatalf("render not windowed: %d lines", got)
	}
	if !strings.Contains(ansi.Strip(out), "Large diff") {
		t.Fatal("large-diff degradation notice missing")
	}
	_ = opts
}
