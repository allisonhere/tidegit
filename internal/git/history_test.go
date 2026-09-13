package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// topologyFixture builds a repository with a merge, a diverged branch, tags and
// a remote-tracking ref:
//
//	main      A---B---------M---E      M merges D into B
//	             \         /
//	feature       C-------D
//	other     A---F                    diverged, never merged
//
// v1.0 is a lightweight tag on B, v2.0 an annotated tag on E, and
// refs/remotes/origin/main points at B so main is ahead of its upstream.
func topologyFixture(t *testing.T) (Repository, map[string]string) {
	t.Helper()
	r := fixture(t)
	oid := map[string]string{}
	record := func(name string) {
		oid[name] = strings.TrimSpace(gitCmd(t, r.Root, "rev-parse", "HEAD"))
	}
	write(t, r, "a.txt", "a\n")
	gitCmd(t, r.Root, "add", ".")
	gitCmd(t, r.Root, "commit", "-m", "Add a")
	record("A")
	write(t, r, "b.txt", "b\n")
	gitCmd(t, r.Root, "add", ".")
	gitCmd(t, r.Root, "commit", "-m", "Add b")
	record("B")
	gitCmd(t, r.Root, "tag", "v1.0")

	gitCmd(t, r.Root, "switch", "-c", "feature", "HEAD")
	write(t, r, "c.txt", "c\n")
	gitCmd(t, r.Root, "add", ".")
	gitCmd(t, r.Root, "commit", "-m", "Add c on feature")
	record("C")
	write(t, r, "d.txt", "d\n")
	gitCmd(t, r.Root, "add", ".")
	gitCmd(t, r.Root, "commit", "-m", "Add d on feature")
	record("D")

	gitCmd(t, r.Root, "switch", "main")
	gitCmd(t, r.Root, "merge", "--no-ff", "-m", "Merge feature into main", "feature")
	record("M")
	write(t, r, "e.txt", "e\n")
	gitCmd(t, r.Root, "add", ".")
	gitCmd(t, r.Root, "commit", "-m", "Add e")
	record("E")
	gitCmd(t, r.Root, "tag", "-a", "v2.0", "-m", "Release two")

	gitCmd(t, r.Root, "switch", "-c", "other", oid["A"])
	write(t, r, "f.txt", "f\n")
	gitCmd(t, r.Root, "add", ".")
	gitCmd(t, r.Root, "commit", "-m", "Add f on other")
	record("F")
	gitCmd(t, r.Root, "switch", "main")

	// A remote-tracking ref is an ordinary ref, so the fixture creates one
	// directly and never talks to a network. The remote still has to be
	// configured for Git to resolve an upstream through its fetch refspec.
	gitCmd(t, r.Root, "remote", "add", "origin", r.Root+"/../unreachable-remote.git")
	gitCmd(t, r.Root, "update-ref", "refs/remotes/origin/main", oid["B"])
	gitCmd(t, r.Root, "update-ref", "refs/remotes/origin/feature", oid["D"])
	gitCmd(t, r.Root, "branch", "--set-upstream-to=origin/main", "main")
	return r, oid
}

func subjects(commits []Commit) []string {
	out := make([]string, len(commits))
	for i, c := range commits {
		out[i] = c.Subject
	}
	return out
}

func findCommit(t *testing.T, commits []Commit, subject string) Commit {
	t.Helper()
	for _, c := range commits {
		if c.Subject == subject {
			return c
		}
	}
	t.Fatalf("no commit with subject %q in %v", subject, subjects(commits))
	return Commit{}
}

func TestHistoryLinearAndMetadata(t *testing.T) {
	r := fixture(t)
	ctx := context.Background()
	for _, s := range []string{"first", "second", "third"} {
		write(t, r, s+".txt", s+"\n")
		gitCmd(t, r.Root, "add", ".")
		gitCmd(t, r.Root, "commit", "-m", s)
	}
	commits, err := r.History(ctx, HistoryOptions{})
	must(t, err)
	if got := subjects(commits); len(got) != 3 || got[0] != "third" || got[2] != "first" {
		t.Fatalf("newest-first order: %v", got)
	}
	head := commits[0]
	if head.Author != "TideGit Test" || head.AuthorEmail != "test@example.invalid" {
		t.Fatalf("author metadata: %+v", head)
	}
	if head.Short == "" || !strings.HasPrefix(head.OID, head.Short) {
		t.Fatalf("hash metadata: %+v", head)
	}
	if head.AuthorTime.IsZero() || head.CommitTime.IsZero() {
		t.Fatal("missing timestamps")
	}
	if len(head.Parents) != 1 || head.Parents[0] != commits[1].OID {
		t.Fatalf("parent link: %+v", head)
	}
	if len(commits[2].Parents) != 0 {
		t.Fatal("root commit reported a parent")
	}
	if head.Merge() || !strings.Contains(headDecorations(head), "main") {
		t.Fatalf("decorations: %+v", head.Decorations)
	}
}

func headDecorations(c Commit) string {
	var names []string
	for _, ref := range c.Decorations {
		names = append(names, ref.Name)
	}
	return strings.Join(names, ",")
}

func TestHistoryMergeDivergenceAndTags(t *testing.T) {
	r, oid := topologyFixture(t)
	ctx := context.Background()
	commits, err := r.History(ctx, HistoryOptions{All: true})
	must(t, err)
	merge := findCommit(t, commits, "Merge feature into main")
	if !merge.Merge() || len(merge.Parents) != 2 {
		t.Fatalf("merge parents: %+v", merge)
	}
	if merge.Parents[0] != oid["B"] || merge.Parents[1] != oid["D"] {
		t.Fatal("merge parent order is not first-parent first")
	}
	// A diverged branch only appears when every ref is walked.
	if findCommit(t, commits, "Add f on other").OID != oid["F"] {
		t.Fatal("diverged branch commit mismatched")
	}
	headOnly, err := r.History(ctx, HistoryOptions{})
	must(t, err)
	for _, c := range headOnly {
		if c.Subject == "Add f on other" {
			t.Fatal("HEAD history included a diverged branch")
		}
	}

	tagged := findCommit(t, commits, "Add b")
	if !hasRef(tagged.Decorations, RefTag, "v1.0") {
		t.Fatalf("lightweight tag missing: %+v", tagged.Decorations)
	}
	if !hasRef(tagged.Decorations, RefRemote, "origin/main") {
		t.Fatalf("remote-tracking ref missing: %+v", tagged.Decorations)
	}
	newest := findCommit(t, commits, "Add e")
	if !hasRef(newest.Decorations, RefTag, "v2.0") {
		t.Fatalf("annotated tag missing: %+v", newest.Decorations)
	}
	if !hasRef(newest.Decorations, RefLocal, "main") {
		t.Fatalf("branch decoration missing: %+v", newest.Decorations)
	}
	for _, ref := range newest.Decorations {
		if ref.Name == "main" && !ref.Head {
			t.Fatal("HEAD branch not marked")
		}
	}
}

func hasRef(refs []Ref, kind RefKind, name string) bool {
	for _, ref := range refs {
		if ref.Kind == kind && ref.Name == name {
			return true
		}
	}
	return false
}

func TestHistoryPaginationAndSearch(t *testing.T) {
	r := fixture(t)
	ctx := context.Background()
	for _, s := range []string{"alpha", "beta", "gamma", "delta", "epsilon"} {
		write(t, r, s+".txt", s+"\n")
		gitCmd(t, r.Root, "add", ".")
		gitCmd(t, r.Root, "commit", "-m", "commit "+s)
	}
	first, err := r.History(ctx, HistoryOptions{Limit: 2})
	must(t, err)
	second, err := r.History(ctx, HistoryOptions{Limit: 2, Skip: 2})
	must(t, err)
	third, err := r.History(ctx, HistoryOptions{Limit: 2, Skip: 4})
	must(t, err)
	if len(first) != 2 || len(second) != 2 || len(third) != 1 {
		t.Fatalf("page sizes: %d %d %d", len(first), len(second), len(third))
	}
	whole, err := r.History(ctx, HistoryOptions{})
	must(t, err)
	var paged []Commit
	paged = append(append(append(paged, first...), second...), third...)
	if len(paged) != len(whole) {
		t.Fatalf("paged %d, whole %d", len(paged), len(whole))
	}
	for i := range whole {
		if paged[i].OID != whole[i].OID {
			t.Fatalf("page boundary reordered commit %d", i)
		}
	}
	beyond, err := r.History(ctx, HistoryOptions{Limit: 2, Skip: 99})
	must(t, err)
	if len(beyond) != 0 {
		t.Fatal("skipping past the end returned commits")
	}

	found, err := r.History(ctx, HistoryOptions{Search: "GAMMA"})
	must(t, err)
	if len(found) != 1 || found[0].Subject != "commit gamma" {
		t.Fatalf("case-insensitive search: %v", subjects(found))
	}
	// Search text is literal: regular expression syntax matches nothing.
	none, err := r.History(ctx, HistoryOptions{Search: "commit .*"})
	must(t, err)
	if len(none) != 0 {
		t.Fatalf("search was not literal: %v", subjects(none))
	}
}

func TestHistoryOnUnbornHeadIsEmpty(t *testing.T) {
	r := fixture(t)
	commits, err := r.History(context.Background(), HistoryOptions{})
	must(t, err)
	if len(commits) != 0 {
		t.Fatalf("unborn HEAD returned %d commits", len(commits))
	}
}

func TestHistoryPreservesUnusualSubjects(t *testing.T) {
	r := fixture(t)
	ctx := context.Background()
	subject := "Fix \"quoted\" tabs\tand — unicode 海 in one subject"
	write(t, r, "f", "x\n")
	gitCmd(t, r.Root, "add", ".")
	gitCmd(t, r.Root, "commit", "-m", subject+"\n\nBody paragraph one.\n\nBody paragraph two.\n")
	commits, err := r.History(ctx, HistoryOptions{})
	must(t, err)
	if len(commits) != 1 || commits[0].Subject != subject {
		t.Fatalf("subject mangled: %q", commits[0].Subject)
	}
	detail, err := r.CommitDetail(ctx, commits[0].OID)
	must(t, err)
	if !strings.Contains(detail.Body, "paragraph one") || !strings.Contains(detail.Body, "paragraph two") {
		t.Fatalf("body lost: %q", detail.Body)
	}
	if detail.Subject != subject || detail.OID != commits[0].OID {
		t.Fatal("detail disagrees with history")
	}
}

func TestCommitFilesAndDiff(t *testing.T) {
	r := fixture(t)
	ctx := context.Background()
	write(t, r, "keep.txt", "one\ntwo\n")
	write(t, r, "drop.txt", "gone\n")
	write(t, r, "move.txt", "some content worth renaming\nsecond line\nthird line\n")
	gitCmd(t, r.Root, "add", ".")
	gitCmd(t, r.Root, "commit", "-m", "baseline")

	write(t, r, "keep.txt", "one\ntwo\nthree\n")
	write(t, r, "added.txt", "brand new\n")
	must(t, os.Remove(filepath.Join(r.Root, "drop.txt")))
	gitCmd(t, r.Root, "mv", "move.txt", "moved.txt")
	gitCmd(t, r.Root, "add", "-A")
	gitCmd(t, r.Root, "commit", "-m", "change everything")

	commits, err := r.History(ctx, HistoryOptions{Limit: 1})
	must(t, err)
	files, err := r.CommitFiles(ctx, commits[0])
	must(t, err)
	byPath := map[string]FileChange{}
	for _, f := range files {
		byPath[f.Path] = f
	}
	if len(files) != 4 {
		t.Fatalf("expected four changed paths, got %d: %+v", len(files), files)
	}
	if f := byPath["keep.txt"]; f.Status != 'M' || f.Additions != 1 || f.Deletions != 0 {
		t.Fatalf("modified file: %+v", f)
	}
	if f := byPath["added.txt"]; f.Status != 'A' || f.Additions != 1 {
		t.Fatalf("added file: %+v", f)
	}
	if f := byPath["drop.txt"]; f.Status != 'D' || f.Deletions != 1 {
		t.Fatalf("deleted file: %+v", f)
	}
	if f := byPath["moved.txt"]; f.Status != 'R' || f.OriginalPath != "move.txt" {
		t.Fatalf("renamed file: %+v", f)
	}

	diff, err := r.CommitDiff(ctx, commits[0], byPath["keep.txt"])
	must(t, err)
	if !strings.Contains(diff.Patch, "+three") || !strings.Contains(diff.Patch, "keep.txt") {
		t.Fatalf("commit diff: %q", diff.Patch)
	}
	if strings.Contains(diff.Patch, "added.txt") {
		t.Fatal("commit diff leaked another path")
	}
	if diff.Commit != commits[0].Short {
		t.Fatalf("diff is not labelled with its commit: %q", diff.Commit)
	}
	if len(diff.Hunks) != 0 || diff.HunkUnavailable == "" {
		t.Fatal("historical diff offered working-tree hunk staging")
	}
}

func TestCommitFilesForRootAndMergeCommits(t *testing.T) {
	r, _ := topologyFixture(t)
	ctx := context.Background()
	commits, err := r.History(ctx, HistoryOptions{All: true})
	must(t, err)

	root := findCommit(t, commits, "Add a")
	files, err := r.CommitFiles(ctx, root)
	must(t, err)
	if len(files) != 1 || files[0].Path != "a.txt" || files[0].Status != 'A' {
		t.Fatalf("root commit files: %+v", files)
	}
	diff, err := r.CommitDiff(ctx, root, files[0])
	must(t, err)
	if !strings.Contains(diff.Patch, "+a") {
		t.Fatalf("root commit diff: %q", diff.Patch)
	}

	// A merge is summarised against its first parent, the way `git show` does.
	merge := findCommit(t, commits, "Merge feature into main")
	mergeFiles, err := r.CommitFiles(ctx, merge)
	must(t, err)
	paths := map[string]bool{}
	for _, f := range mergeFiles {
		paths[f.Path] = true
	}
	if !paths["c.txt"] || !paths["d.txt"] {
		t.Fatalf("merge should show what it brought in: %+v", mergeFiles)
	}
}

func TestResolveHeadAndRefs(t *testing.T) {
	r, oid := topologyFixture(t)
	ctx := context.Background()
	head, err := r.ResolveHead(ctx)
	must(t, err)
	if head.Branch != "main" || head.Detached || head.Unborn || head.OID != oid["E"] {
		t.Fatalf("attached HEAD: %+v", head)
	}

	refs, err := r.Refs(ctx)
	must(t, err)
	kinds := map[string]Ref{}
	for _, ref := range refs {
		kinds[ref.Name] = ref
	}
	if ref := kinds["main"]; ref.Kind != RefLocal || !ref.Head || ref.OID != oid["E"] {
		t.Fatalf("local ref: %+v", ref)
	}
	if ref := kinds["other"]; ref.Kind != RefLocal || ref.Head {
		t.Fatalf("non-HEAD local ref: %+v", ref)
	}
	if ref := kinds["origin/main"]; ref.Kind != RefRemote || ref.OID != oid["B"] {
		t.Fatalf("remote ref: %+v", ref)
	}
	if ref := kinds["v1.0"]; ref.Kind != RefTag || ref.OID != oid["B"] {
		t.Fatalf("lightweight tag: %+v", ref)
	}
	// An annotated tag must report the commit, not the tag object.
	if ref := kinds["v2.0"]; ref.Kind != RefTag || ref.OID != oid["E"] {
		t.Fatalf("annotated tag did not dereference to its commit: %+v", ref)
	}

	resolved, err := r.ResolveRef(ctx, "v2.0")
	must(t, err)
	if resolved != oid["E"] {
		t.Fatalf("ResolveRef(tag): %s", resolved)
	}
	if short, err := r.ResolveRef(ctx, oid["B"][:7]); err != nil || short != oid["B"] {
		t.Fatalf("ResolveRef(short hash): %s %v", short, err)
	}
	if _, err := r.ResolveRef(ctx, "no-such-ref"); err == nil {
		t.Fatal("unknown ref resolved")
	}
	if _, err := r.ResolveRef(ctx, "--all"); err == nil {
		t.Fatal("option-shaped revision accepted")
	}
}

func TestDetachedHead(t *testing.T) {
	r, oid := topologyFixture(t)
	ctx := context.Background()
	gitCmd(t, r.Root, "switch", "--detach", oid["B"])

	head, err := r.ResolveHead(ctx)
	must(t, err)
	if !head.Detached || head.Branch != "" || head.OID != oid["B"] || head.Short != oid["B"][:7] {
		t.Fatalf("detached HEAD: %+v", head)
	}
	commits, err := r.History(ctx, HistoryOptions{})
	must(t, err)
	if len(commits) != 2 || commits[0].OID != oid["B"] {
		t.Fatalf("detached history: %v", subjects(commits))
	}
	if !hasRef(commits[0].Decorations, RefHead, "HEAD") {
		t.Fatalf("detached HEAD not decorated: %+v", commits[0].Decorations)
	}
	// Branch creation must still work while detached.
	must(t, r.CreateBranch(ctx, "from-detached", "HEAD", false))
	resolved, err := r.ResolveRef(ctx, "from-detached")
	must(t, err)
	if resolved != oid["B"] {
		t.Fatalf("branch from detached HEAD: %s", resolved)
	}
	if again, err := r.ResolveHead(ctx); err != nil || !again.Detached {
		t.Fatalf("creating a branch left detached HEAD: %+v %v", again, err)
	}
}

func TestGraphLanesTopology(t *testing.T) {
	r, oid := topologyFixture(t)
	commits, err := r.History(context.Background(), HistoryOptions{All: true})
	must(t, err)
	rows := GraphLanes(commits)
	if len(rows) != len(commits) {
		t.Fatalf("%d rows for %d commits", len(rows), len(commits))
	}
	byOID := map[string]GraphRow{}
	for i, c := range commits {
		byOID[c.OID] = rows[i]
		if rows[i].Lane >= rows[i].Width() {
			t.Fatalf("node lane %d outside width %d", rows[i].Lane, rows[i].Width())
		}
		if rows[i].Glyphs[rows[i].Lane] != GraphNode {
			t.Fatal("node lane does not hold the node glyph")
		}
	}
	if !byOID[oid["M"]].Merge {
		t.Fatal("merge commit not marked")
	}
	if !byOID[oid["A"]].Root {
		t.Fatal("root commit not marked")
	}
	if byOID[oid["A"]].Merge {
		t.Fatal("root commit marked as a merge")
	}
	// The merge must open a second lane for its non-first parent.
	if byOID[oid["M"]].Width() < 2 {
		t.Fatalf("merge row is one lane wide: %+v", byOID[oid["M"]])
	}
	// A tip nobody is waiting for starts its own lane.
	if !byOID[oid["E"]].Tip || !byOID[oid["F"]].Tip {
		t.Fatal("branch tips not marked")
	}
	if byOID[oid["B"]].Tip {
		t.Fatal("an interior commit was marked as a tip")
	}
	// Lanes must never exceed the number of simultaneously open branches.
	for _, row := range rows {
		if row.Width() > 3 {
			t.Fatalf("graph used %d lanes for a three-branch history", row.Width())
		}
	}
}

func TestGraphLanesLinearHistoryStaysInOneLane(t *testing.T) {
	r := fixture(t)
	for _, s := range []string{"one", "two", "three", "four"} {
		write(t, r, s, s+"\n")
		gitCmd(t, r.Root, "add", ".")
		gitCmd(t, r.Root, "commit", "-m", s)
	}
	commits, err := r.History(context.Background(), HistoryOptions{})
	must(t, err)
	rows := GraphLanes(commits)
	for i, row := range rows {
		if row.Lane != 0 || row.Width() != 1 {
			t.Fatalf("row %d drifted: %+v", i, row)
		}
		if i > 0 && row.Tip {
			t.Fatalf("row %d is not a tip", i)
		}
	}
	if len(GraphLanes(nil)) != 0 {
		t.Fatal("empty history produced rows")
	}
}

func TestGraphLanesOctopusMerge(t *testing.T) {
	r := fixture(t)
	ctx := context.Background()
	write(t, r, "base", "base\n")
	gitCmd(t, r.Root, "add", ".")
	gitCmd(t, r.Root, "commit", "-m", "base")
	for _, side := range []string{"alpha", "beta", "gamma"} {
		gitCmd(t, r.Root, "switch", "-c", side, "main")
		write(t, r, side, side+"\n")
		gitCmd(t, r.Root, "add", ".")
		gitCmd(t, r.Root, "commit", "-m", "explore "+side)
		gitCmd(t, r.Root, "switch", "main")
	}
	gitCmd(t, r.Root, "merge", "--no-ff", "-m", "merge three topics", "alpha", "beta", "gamma")

	commits, err := r.History(ctx, HistoryOptions{All: true})
	must(t, err)
	rows := GraphLanes(commits)
	merge := rows[0]
	if len(commits[0].Parents) != 4 {
		t.Fatalf("expected a four-parent merge, got %d", len(commits[0].Parents))
	}
	if !merge.Merge || merge.Glyphs[merge.Lane] != GraphNode {
		t.Fatalf("octopus row: %+v", merge)
	}
	// Three extra parents open three lanes. The run reaches the furthest of
	// them, so the corners it passes through are tees, not closing corners.
	tees, corners := 0, 0
	for _, g := range merge.Glyphs {
		switch g {
		case GraphForkTee:
			tees++
		case GraphForkRight, GraphForkLeft:
			corners++
		}
	}
	if tees != 2 || corners != 1 {
		t.Fatalf("octopus connectors: %d tees, %d corners in %+v", tees, corners, merge.Glyphs)
	}
	// Every opened lane must be reachable, never left dangling.
	if merge.Width() < 4 {
		t.Fatalf("octopus row is only %d lanes wide", merge.Width())
	}
}
