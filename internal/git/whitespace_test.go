package git

import (
	"context"
	"strings"
	"testing"
)

func TestWhitespaceModesUseGitOptions(t *testing.T) {
	r := fixture(t)
	write(t, r, "f", "a\nb\n")
	commit(t, r)
	// A whitespace-only change to the existing line.
	write(t, r, "f", "a   \nb\n")

	normal, err := r.DiffWith(context.Background(), Unstaged, File{Path: "f"}, DiffOptions{Context: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(normal.Patch, "-a") || !strings.Contains(normal.Patch, "+a   ") {
		t.Fatalf("normal diff should show the whitespace change: %q", normal.Patch)
	}
	if normal.Whitespace != WhitespaceNormal {
		t.Fatalf("mode recorded: %v", normal.Whitespace)
	}

	ignored, err := r.DiffWith(context.Background(), Unstaged, File{Path: "f"}, DiffOptions{Context: 3, Whitespace: WhitespaceIgnoreAll})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ignored.Patch, "+a   ") {
		t.Fatalf("ignore-all should hide the whitespace change: %q", ignored.Patch)
	}
	if ignored.Whitespace != WhitespaceIgnoreAll {
		t.Fatalf("mode recorded: %v", ignored.Whitespace)
	}

	// A real content change still appears under ignore-all.
	write(t, r, "f", "a   \nB\n")
	content, err := r.DiffWith(context.Background(), Unstaged, File{Path: "f"}, DiffOptions{Context: 3, Whitespace: WhitespaceIgnoreAll})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content.Patch, "+B") {
		t.Fatalf("content change hidden: %q", content.Patch)
	}
}

func TestParseWhitespaceMode(t *testing.T) {
	cases := map[string]WhitespaceMode{
		"":                WhitespaceNormal,
		"normal":          WhitespaceNormal,
		"ignore-trailing": WhitespaceIgnoreTrailing,
		"ignore-change":   WhitespaceIgnoreChange,
		"ignore-all":      WhitespaceIgnoreAll,
		"whatever":        WhitespaceNormal,
	}
	for input, want := range cases {
		if got := ParseWhitespaceMode(input); got != want {
			t.Fatalf("ParseWhitespaceMode(%q) = %v, want %v", input, got, want)
		}
	}
	if args := WhitespaceIgnoreAll.Args(); len(args) != 1 || args[0] != "--ignore-all-space" {
		t.Fatalf("ignore-all args: %v", args)
	}
	if args := WhitespaceNormal.Args(); args != nil {
		t.Fatalf("normal args: %v", args)
	}
}
