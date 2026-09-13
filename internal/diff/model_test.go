package diff

import (
	"strings"
	"testing"
)

const sample = `diff --git a/src/parser.go b/src/parser.go
index 1111111..2222222 100644
--- a/src/parser.go
+++ b/src/parser.go
@@ -1,4 +1,5 @@ func parse(
 package parser
 
-import "old/pkg"
+import "new/pkg"
+import "fmt"
 
 func Parse() {}
`

func TestParseBasics(t *testing.T) {
	p := Parse(sample)
	if len(p.Files) != 1 {
		t.Fatalf("files: %d", len(p.Files))
	}
	f := p.Files[0]
	if f.OldPath != "src/parser.go" || f.NewPath != "src/parser.go" {
		t.Fatalf("paths: %q %q", f.OldPath, f.NewPath)
	}
	if len(f.Hunks) != 1 {
		t.Fatalf("hunks: %d", len(f.Hunks))
	}
	h := f.Hunks[0]
	if h.OldStart != 1 || h.NewStart != 1 || h.Section != "func parse(" {
		t.Fatalf("header: %+v", h)
	}
	var adds, dels, ctx int
	for _, l := range h.Lines {
		switch l.Type {
		case Addition:
			adds++
		case Deletion:
			dels++
		case Context:
			ctx++
		}
	}
	if adds != 2 || dels != 1 || ctx != 4 {
		t.Fatalf("line types: +%d -%d ctx%d (%+v)", adds, dels, ctx, h.Lines)
	}
	sum := f.Summary()
	if sum.Additions != 2 || sum.Deletions != 1 || sum.Path != "src/parser.go" {
		t.Fatalf("summary: %+v", sum)
	}
}

func TestParseLineNumbersAndNoNewline(t *testing.T) {
	patch := "diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1,2 +1,2 @@\n one\n-two\n+three\n\\ No newline at end of file\n"
	p := Parse(patch)
	h := p.Files[0].Hunks[0]
	// context line: old 1 new 1
	if h.Lines[0].Old != 1 || h.Lines[0].New != 1 || h.Lines[0].Type != Context {
		t.Fatalf("context: %+v", h.Lines[0])
	}
	if h.Lines[1].Old != 2 || h.Lines[1].New != 0 || h.Lines[1].Type != Deletion {
		t.Fatalf("deletion: %+v", h.Lines[1])
	}
	if h.Lines[2].Old != 0 || h.Lines[2].New != 2 || h.Lines[2].Type != Addition {
		t.Fatalf("addition: %+v", h.Lines[2])
	}
	if h.Lines[3].Type != NoNewline {
		t.Fatalf("no-newline: %+v", h.Lines[3])
	}
	if !h.Lines[2].NoNewline {
		t.Fatal("no-newline marker not attached to the preceding line")
	}
}

func TestParseNewDeletedRenameCopy(t *testing.T) {
	p := Parse("diff --git a/new b/new\nnew file mode 100644\n--- /dev/null\n+++ b/new\n@@ -0,0 +1 @@\n+hello\n")
	if p.Files[0].Status != 'A' || !strings.Contains(p.Files[0].NewMode, "100644") {
		t.Fatalf("new file: %+v", p.Files[0])
	}
	p = Parse("diff --git a/old b/old\ndeleted file mode 100644\n--- a/old\n+++ /dev/null\n@@ -1 +0,0 @@\n-bye\n")
	if p.Files[0].Status != 'D' {
		t.Fatalf("deleted file: %+v", p.Files[0])
	}
	p = Parse("diff --git a/old.go b/new.go\nsimilarity index 92%\nrename from old.go\nrename to new.go\nindex 1..2 100644\n--- a/old.go\n+++ b/new.go\n")
	f := p.Files[0]
	if !f.Rename || f.Status != 'R' || f.Similarity != 92 || f.OldPath != "old.go" || f.NewPath != "new.go" {
		t.Fatalf("rename: %+v", f)
	}
	p = Parse("diff --git a/a b/b\nsimilarity index 100%\ncopy from a\ncopy to b\n")
	if !p.Files[0].Copy || p.Files[0].Status != 'C' {
		t.Fatalf("copy: %+v", p.Files[0])
	}
}

func TestParseQuotedPathsAndUnicode(t *testing.T) {
	p := Parse("diff --git \"a/with space.txt\" \"b/with space.txt\"\n--- \"a/with space.txt\"\n+++ \"b/with space.txt\"\n@@ -1 +1 @@\n-α\n+β\n")
	f := p.Files[0]
	if f.OldPath != "with space.txt" || f.NewPath != "with space.txt" {
		t.Fatalf("quoted paths: %q %q", f.OldPath, f.NewPath)
	}
	if got := f.Hunks[0].Lines[0].Text; got != "α" {
		t.Fatalf("unicode: %q", got)
	}
}

func TestParseBinary(t *testing.T) {
	p := Parse("diff --git a/img.png b/img.png\nindex 1..2 100644\nBinary files a/img.png and b/img.png differ\n")
	if !p.Files[0].Binary {
		t.Fatalf("binary: %+v", p.Files[0])
	}
}

func TestSideBySideAlignment(t *testing.T) {
	patch := "diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1,3 +1,3 @@\n a\n-b\n+bb\n c\n"
	h := Parse(patch).Files[0].Hunks[0]
	rows := h.SideRows()
	// a, modified b/bb, c
	if len(rows) != 3 {
		t.Fatalf("rows: %d", len(rows))
	}
	if !rows[1].Modified || rows[1].Left.Text != "b" || rows[1].Right.Text != "bb" {
		t.Fatalf("modified row: %+v", rows[1])
	}
	if rows[0].Left == nil || rows[0].Right == nil {
		t.Fatal("context should appear on both sides")
	}

	// Pure insertion: left blank.
	patch = "diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1,1 +1,2 @@\n a\n+b\n"
	rows = Parse(patch).Files[0].Hunks[0].SideRows()
	if rows[1].Left != nil || rows[1].Right == nil {
		t.Fatalf("insert row: %+v", rows[1])
	}
	// Pure deletion: right blank.
	patch = "diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1,2 +1,1 @@\n a\n-b\n"
	rows = Parse(patch).Files[0].Hunks[0].SideRows()
	if rows[1].Left == nil || rows[1].Right != nil {
		t.Fatalf("delete row: %+v", rows[1])
	}
}

func TestWordSpans(t *testing.T) {
	oldSpans, newSpans := wordSpans("return oldValue()", "return newValue()")
	if len(oldSpans) != 1 || len(newSpans) != 1 {
		t.Fatalf("spans: %v %v", oldSpans, newSpans)
	}
	oldText := "return oldValue()"
	newText := "return newValue()"
	if got := string([]rune(oldText)[oldSpans[0].Start:oldSpans[0].End]); got != "oldValue" {
		t.Fatalf("old changed text: %q", got)
	}
	if got := string([]rune(newText)[newSpans[0].Start:newSpans[0].End]); got != "newValue" {
		t.Fatalf("new changed text: %q", got)
	}

	// Insertion: only the added word changes.
	oldSpans, newSpans = wordSpans("a b", "a c b")
	if len(oldSpans) != 0 {
		t.Fatalf("insert should not change the old side: %v", oldSpans)
	}
	if len(newSpans) != 1 || strings.TrimSpace(string([]rune("a c b")[newSpans[0].Start:newSpans[0].End])) != "c" {
		t.Fatalf("insert spans: %v", newSpans)
	}

	// Punctuation change.
	oldSpans, newSpans = wordSpans("f(a)", "f(a,)")
	if len(oldSpans) != 0 && len(oldSpans) != 1 {
		t.Fatalf("punct old spans: %v", oldSpans)
	}
	_ = newSpans

	// Unicode tokens do not split mid-word.
	_, newSpans = wordSpans("héllo wörld", "héllo wörld!!")
	if len(newSpans) != 1 || string([]rune("héllo wörld!!")[newSpans[0].Start:newSpans[0].End]) != "!!" {
		t.Fatalf("unicode spans: %v", newSpans)
	}
}

func TestCollapseContext(t *testing.T) {
	var lines []Line
	for i := 0; i < 20; i++ {
		lines = append(lines, Line{Type: Context, Text: "x", Old: i + 1, New: i + 1})
	}
	// keep=2, threshold=8: collapse to 2 + marker + 2.
	out := CollapseContext(lines, 2, 8, nil)
	kinds := map[bool]int{}
	for _, d := range out {
		kinds[d.Collapsed]++
	}
	if kinds[true] != 1 {
		t.Fatalf("expected one collapsed marker: %+v", out)
	}
	var hidden int
	for _, d := range out {
		if d.Collapsed {
			hidden = d.Hidden
		}
	}
	if hidden != 20-4 {
		t.Fatalf("hidden count: %d", hidden)
	}
	// Expanding the gap shows every line.
	out = CollapseContext(lines, 2, 8, map[int]bool{0: true})
	for _, d := range out {
		if d.Collapsed {
			t.Fatal("expanded gap still collapsed")
		}
	}
	if len(out) != 20 {
		t.Fatalf("expanded length: %d", len(out))
	}
	// A short run is never collapsed.
	out = CollapseContext(lines[:4], 2, 8, nil)
	for _, d := range out {
		if d.Collapsed {
			t.Fatal("short run collapsed")
		}
	}
}

func TestHighlight(t *testing.T) {
	toks := Highlight("go", "func main() { x := 42; s := \"hi\" // c")
	var classes []TokenClass
	for _, tok := range toks {
		classes = append(classes, tok.Class)
	}
	has := func(c TokenClass) bool {
		for _, x := range classes {
			if x == c {
				return true
			}
		}
		return false
	}
	if !has(TokKeyword) || !has(TokNumber) || !has(TokString) || !has(TokComment) {
		t.Fatalf("highlight classes: %v", classes)
	}
	if Highlight("", "text") != nil || Highlight("go", "") != nil {
		t.Fatal("empty input should have no tokens")
	}
	if LanguageForPath("main.rs") != "rust" || LanguageForPath("a.unknown") != "" {
		t.Fatal("language detection")
	}
}
