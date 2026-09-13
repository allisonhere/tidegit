package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func worktree(t *testing.T, r Repository, path string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(r.Root, path))
	must(t, err)
	return string(b)
}
func index(t *testing.T, r Repository, path string) string {
	t.Helper()
	return gitCmd(t, r.Root, "show", ":"+path)
}
func getDiff(t *testing.T, r Repository, s Section, f File) Diff {
	t.Helper()
	d, err := r.Diff(context.Background(), s, f)
	must(t, err)
	return d
}
func twoHunks(t *testing.T, path string) (Repository, string, string) {
	t.Helper()
	r := fixture(t)
	var lines []string
	for i := 1; i <= 40; i++ {
		lines = append(lines, fmt.Sprintf("line %02d\n", i))
	}
	base := strings.Join(lines, "")
	write(t, r, path, base)
	commit(t, r)
	changed := strings.ReplaceAll(strings.ReplaceAll(base, "line 03\n", "first change\n"), "line 33\n", "second change\n")
	write(t, r, path, changed)
	return r, base, changed
}

func TestStageAndUnstageFiles(t *testing.T) {
	ctx := context.Background()
	r := fixture(t)
	write(t, r, "modified", "base\n")
	write(t, r, "deleted", "bye\n")
	commit(t, r)
	write(t, r, "modified", "changed\n")
	write(t, r, "new [file]", "new\n")
	write(t, r, "unrelated", "leave me\n")
	must(t, os.Remove(filepath.Join(r.Root, "deleted")))
	for _, path := range []string{"modified", "new [file]", "deleted"} {
		must(t, r.StageFile(ctx, File{Path: path}))
	}
	s := status(t, r)
	if len(s.Groups[Staged]) != 3 || len(s.Groups[Untracked]) != 1 || s.Groups[Untracked][0].Path != "unrelated" {
		t.Fatalf("unexpected status: %+v", s)
	}
	must(t, r.UnstageFile(ctx, File{Path: "modified"}))
	if worktree(t, r, "modified") != "changed\n" || index(t, r, "modified") != "base\n" {
		t.Fatal("unstage changed working tree or failed to restore index")
	}
	if len(status(t, r).Groups[Staged]) != 2 {
		t.Fatal("unstaged unrelated file")
	}
	must(t, r.UnstageFile(ctx, File{Path: "deleted"}))
	if _, err := os.Stat(filepath.Join(r.Root, "deleted")); !os.IsNotExist(err) {
		t.Fatal("unstage resurrected deleted working file")
	}
	must(t, r.UnstageFile(ctx, File{Path: "new [file]"}))
	if worktree(t, r, "new [file]") != "new\n" {
		t.Fatal("lost added file")
	}
}

func TestUnstageUnbornWithDifferentIndexAndWorktree(t *testing.T) {
	ctx := context.Background()
	r := fixture(t)
	write(t, r, "new", "index\n")
	write(t, r, "other", "other\n")
	must(t, r.StageFile(ctx, File{Path: "new"}))
	must(t, r.StageFile(ctx, File{Path: "other"}))
	write(t, r, "new", "working\n")
	must(t, r.UnstageFile(ctx, File{Path: "new"}))
	if worktree(t, r, "new") != "working\n" || len(status(t, r).Groups[Staged]) != 1 {
		t.Fatal("unborn unstage changed content or unrelated index entry")
	}
}

func TestStageUnstageHunksAndMixedState(t *testing.T) {
	for _, path := range []string{"file.txt", "space tab\tline\n界 [x].txt"} {
		t.Run(path, func(t *testing.T) {
			ctx := context.Background()
			r, base, changed := twoHunks(t, path)
			f := File{Path: path}
			d := getDiff(t, r, Unstaged, f)
			if len(d.Hunks) != 2 {
				t.Fatalf("hunks=%d: %s", len(d.Hunks), d.HunkUnavailable)
			}
			must(t, r.StageHunk(ctx, d.Hunks[1]))
			if index(t, r, path) != strings.ReplaceAll(base, "line 33\n", "second change\n") {
				t.Fatal("staged more than selected hunk")
			}
			if worktree(t, r, path) != changed {
				t.Fatal("stage changed worktree")
			}
			s := status(t, r)
			if len(s.Groups[Staged]) != 1 || len(s.Groups[Unstaged]) != 1 {
				t.Fatalf("mixed state: %+v", s)
			}
			staged := getDiff(t, r, Staged, f)
			unstaged := getDiff(t, r, Unstaged, f)
			if strings.Contains(staged.Patch, "+first change") || !strings.Contains(staged.Patch, "+second change") || !strings.Contains(unstaged.Patch, "+first change") {
				t.Fatal("incorrect split diff")
			}
			must(t, r.StageHunk(ctx, unstaged.Hunks[0]))
			d = getDiff(t, r, Staged, f)
			if len(d.Hunks) != 2 {
				t.Fatal("expected two staged hunks")
			}
			must(t, r.UnstageHunk(ctx, d.Hunks[0]))
			if index(t, r, path) != strings.ReplaceAll(base, "line 33\n", "second change\n") || worktree(t, r, path) != changed {
				t.Fatal("hunk unstage damaged content")
			}
		})
	}
}

func TestHunkLineShiftsAndNoNewline(t *testing.T) {
	r, base, _ := twoHunks(t, "f")
	ctx := context.Background()
	changed := strings.Replace(base, "line 03\n", "extra a\nextra b\nextra c\n", 1)
	changed = strings.TrimSuffix(strings.Replace(changed, "line 39\n", "last change\n", 1), "\n")
	write(t, r, "f", changed)
	d := getDiff(t, r, Unstaged, File{Path: "f"})
	if len(d.Hunks) != 2 {
		t.Fatalf("expected two hunks: %s", d.Patch)
	}
	must(t, r.StageHunk(ctx, d.Hunks[1]))
	d = getDiff(t, r, Unstaged, File{Path: "f"})
	must(t, r.StageHunk(ctx, d.Hunks[0]))
	if index(t, r, "f") != changed {
		t.Fatal("line shift or no-newline damaged staging")
	}
	d = getDiff(t, r, Staged, File{Path: "f"})
	must(t, r.UnstageHunk(ctx, d.Hunks[1]))
	d = getDiff(t, r, Staged, File{Path: "f"})
	must(t, r.UnstageHunk(ctx, d.Hunks[0]))
	if index(t, r, "f") != base || worktree(t, r, "f") != changed {
		t.Fatal("line shift damaged unstage")
	}
}

func TestHunksNewAndDeletedFiles(t *testing.T) {
	for _, mode := range []string{"new", "deleted", "unborn"} {
		t.Run(mode, func(t *testing.T) {
			r := fixture(t)
			ctx := context.Background()
			section := Untracked
			write(t, r, "f", "content without newline")
			if mode == "deleted" {
				commit(t, r)
				must(t, os.Remove(filepath.Join(r.Root, "f")))
				section = Unstaged
			} else if mode == "new" {
				gitCmd(t, r.Root, "commit", "--allow-empty", "-m", "base")
			}
			d := getDiff(t, r, section, File{Path: "f"})
			if len(d.Hunks) != 1 {
				t.Fatalf("%+v", d)
			}
			must(t, r.StageHunk(ctx, d.Hunks[0]))
			d = getDiff(t, r, Staged, File{Path: "f"})
			if len(d.Hunks) != 1 {
				t.Fatalf("%+v", d)
			}
			must(t, r.UnstageHunk(ctx, d.Hunks[0]))
			if len(status(t, r).Groups[Staged]) != 0 {
				t.Fatal("hunk remained staged")
			}
			if mode == "deleted" {
				if _, err := os.Stat(filepath.Join(r.Root, "f")); !os.IsNotExist(err) {
					t.Fatal("working file restored")
				}
			} else if worktree(t, r, "f") != "content without newline" {
				t.Fatal("content changed")
			}
		})
	}
}

func TestRenamedFileHunkPreservesRename(t *testing.T) {
	r, _, changed := twoHunks(t, "old name")
	ctx := context.Background()
	gitCmd(t, r.Root, "mv", "old name", "new name")
	must(t, r.StageFile(ctx, File{Path: "new name"}))
	s := status(t, r)
	f := s.Groups[Staged][0]
	if f.OriginalPath != "old name" {
		t.Fatalf("expected rename: %+v", s)
	}
	d := getDiff(t, r, Staged, f)
	if len(d.Hunks) != 2 {
		t.Fatalf("rename hunks: %s %s", d.Patch, d.HunkUnavailable)
	}
	must(t, r.UnstageHunk(ctx, d.Hunks[0]))
	if status(t, r).Groups[Staged][0].OriginalPath != "old name" {
		t.Fatal("text unstage undid rename")
	}
	write(t, r, "old name", "unrelated recreated source\n")
	d = getDiff(t, r, Unstaged, f)
	if strings.Contains(d.Patch, "unrelated recreated") {
		t.Fatal("unstaged diff includes unrelated old path")
	}
	must(t, r.StageHunk(ctx, d.Hunks[0]))
	if worktree(t, r, "new name") != changed || worktree(t, r, "old name") != "unrelated recreated source\n" {
		t.Fatal("rename hunk changed worktree")
	}
	must(t, r.UnstageFile(ctx, f))
	if len(status(t, r).Groups[Staged]) != 0 {
		t.Fatal("rename not fully unstaged")
	}
}

func TestMalformedStaleAndRejectedPatchPreserveRepository(t *testing.T) {
	r, base, changed := twoHunks(t, "f")
	ctx := context.Background()
	d := getDiff(t, r, Unstaged, File{Path: "f"})
	bad := d.Hunks[0]
	bad.Body = []string{"+malformed\n"}
	err := r.StageHunk(ctx, bad)
	if err == nil || !strings.Contains(err.Error(), "malformed") {
		t.Fatalf("malformed error: %v", err)
	}
	write(t, r, "f", strings.Replace(changed, "first change", "different change", 1))
	err = r.StageHunk(ctx, d.Hunks[0])
	if err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale error: %v", err)
	}
	if index(t, r, "f") != base || !strings.Contains(worktree(t, r, "f"), "different change") {
		t.Fatal("failed operation changed content")
	}
	// Force Git itself to reject a valid current hunk through its index lock.
	d = getDiff(t, r, Unstaged, File{Path: "f"})
	must(t, os.WriteFile(filepath.Join(r.Root, ".git", "index.lock"), nil, 0600))
	err = r.StageHunk(ctx, d.Hunks[0])
	if err == nil || !strings.Contains(err.Error(), "index.lock") || !strings.Contains(err.Error(), "could not stage hunk") {
		t.Fatalf("Git diagnostic lost: %v", err)
	}
	if index(t, r, "f") != base {
		t.Fatal("Git failure corrupted index")
	}
}

func TestHunkRespectsMetadataAndDiffConfig(t *testing.T) {
	r, _, changed := twoHunks(t, "f")
	ctx := context.Background()
	gitCmd(t, r.Root, "config", "diff.noprefix", "true")
	gitCmd(t, r.Root, "config", "diff.mnemonicPrefix", "true")
	gitCmd(t, r.Root, "config", "diff.interHunkContext", "100")
	must(t, os.Chmod(filepath.Join(r.Root, "f"), 0755))
	d := getDiff(t, r, Unstaged, File{Path: "f"})
	if len(d.Hunks) != 2 {
		t.Fatalf("config merged hunks: %s", d.Patch)
	}
	must(t, r.StageHunk(ctx, d.Hunks[0]))
	if strings.Contains(getDiff(t, r, Staged, File{Path: "f"}).Patch, "new mode") {
		t.Fatal("hunk staged unrelated permission change")
	}
	if worktree(t, r, "f") != changed {
		t.Fatal("worktree changed")
	}
}

func TestEmptyFileHunkEdges(t *testing.T) {
	for _, remove := range []bool{false, true} {
		t.Run(fmt.Sprint(remove), func(t *testing.T) {
			r := fixture(t)
			before, after := "", "new content\n"
			if remove {
				before, after = after, before
			}
			write(t, r, "f", before)
			commit(t, r)
			write(t, r, "f", after)
			d := getDiff(t, r, Unstaged, File{Path: "f"})
			must(t, r.StageHunk(context.Background(), d.Hunks[0]))
			if index(t, r, "f") != after {
				t.Fatal("zero-range stage failed")
			}
			d = getDiff(t, r, Staged, File{Path: "f"})
			must(t, r.UnstageHunk(context.Background(), d.Hunks[0]))
			if index(t, r, "f") != before || worktree(t, r, "f") != after {
				t.Fatal("zero-range unstage failed")
			}
		})
	}
}

func TestCopiedFileUnstageDoesNotTouchSource(t *testing.T) {
	r := fixture(t)
	ctx := context.Background()
	write(t, r, "source", "base\n")
	commit(t, r)
	write(t, r, "source", "modified source\n")
	write(t, r, "copy", "base\n")
	must(t, r.StageFile(ctx, File{Path: "source"}))
	must(t, r.StageFile(ctx, File{Path: "copy", OriginalPath: "source", XY: "C."}))
	must(t, r.UnstageFile(ctx, File{Path: "copy", OriginalPath: "source", XY: "C."}))
	if index(t, r, "source") != "modified source\n" || worktree(t, r, "copy") != "base\n" {
		t.Fatal("copy unstage changed source or worktree")
	}
}

func TestConcurrentMutationsAndDirectoryGuard(t *testing.T) {
	r := fixture(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for i := 0; i < 10; i++ {
		path := fmt.Sprint(i)
		write(t, r, path, path)
		wg.Add(1)
		go func() { defer wg.Done(); errs <- r.StageFile(ctx, File{Path: path}) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		must(t, err)
	}
	if len(status(t, r).Groups[Staged]) != 10 {
		t.Fatal("concurrent mutations lost changes")
	}
	must(t, os.Mkdir(filepath.Join(r.Root, "dir"), 0700))
	write(t, r, "dir/unrelated", "no\n")
	if err := r.StageFile(ctx, File{Path: "dir"}); err == nil {
		t.Fatal("directory recursively staged")
	}
	if len(status(t, r).Groups[Staged]) != 10 {
		t.Fatal("directory error changed index")
	}
}

func TestBinaryFileUsesFileOperationsOnly(t *testing.T) {
	r := fixture(t)
	ctx := context.Background()
	write(t, r, "binary", "a\x00b")
	d := getDiff(t, r, Untracked, File{Path: "binary"})
	if len(d.Hunks) != 0 || d.HunkUnavailable == "" {
		t.Fatal("binary exposed text hunks")
	}
	must(t, r.StageFile(ctx, File{Path: "binary"}))
	must(t, r.UnstageFile(ctx, File{Path: "binary"}))
	if worktree(t, r, "binary") != "a\x00b" {
		t.Fatal("binary changed")
	}
}

func TestHunkHonorsLineEndingNormalization(t *testing.T) {
	r := fixture(t)
	gitCmd(t, r.Root, "config", "core.autocrlf", "true")
	write(t, r, "new.txt", "one\r\ntwo\r\n")
	d := getDiff(t, r, Untracked, File{Path: "new.txt"})
	must(t, r.StageHunk(context.Background(), d.Hunks[0]))
	if index(t, r, "new.txt") != "one\ntwo\n" {
		t.Fatal("new-file hunk bypassed Git line-ending normalization")
	}
	if worktree(t, r, "new.txt") != "one\r\ntwo\r\n" {
		t.Fatal("new-file hunk modified working bytes")
	}
}
