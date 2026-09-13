package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mergeConflict builds a repository sitting in a two-sided merge conflict on
// "f" and returns it. Both sides modified the same line from a common base.
func mergeConflict(t *testing.T) Repository {
	t.Helper()
	r := fixture(t)
	write(t, r, "f", "base\n")
	commit(t, r)
	gitCmd(t, r.Root, "config", "merge.conflictStyle", "diff3")
	gitCmd(t, r.Root, "checkout", "-b", "other")
	write(t, r, "f", "theirs\n")
	commit(t, r)
	gitCmd(t, r.Root, "checkout", "main")
	write(t, r, "f", "ours\n")
	commit(t, r)
	if _, err := run(context.Background(), r.Root, "merge", "other"); err == nil {
		t.Fatal("expected a merge conflict")
	}
	return r
}

func TestMergeStateDetection(t *testing.T) {
	r := mergeConflict(t)
	state, err := r.State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if state.Operation != OpMerge || !state.InProgress() {
		t.Fatalf("state: %+v", state)
	}
	conflicts, err := r.Conflicts(context.Background())
	if err != nil || len(conflicts) != 1 {
		t.Fatalf("conflicts: %+v %v", conflicts, err)
	}
	if conflicts[0].Kind != ConflictBothModified || conflicts[0].File.Path != "f" {
		t.Fatalf("conflict kind: %+v", conflicts[0])
	}

	// A fresh repository handle rediscovers the state, which is the restart
	// guarantee: nothing was remembered in process.
	fresh := Repository{Root: r.Root}
	again, err := fresh.State(context.Background())
	if err != nil || again.Operation != OpMerge {
		t.Fatalf("restart detection: %+v %v", again, err)
	}
}

func TestConflictStagesAndRegions(t *testing.T) {
	r := mergeConflict(t)
	stages, err := r.Unmerged(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := stages["f"]
	if !ok || entry.Base == "" || entry.Ours == "" || entry.Theirs == "" {
		t.Fatalf("stages: %+v", entry)
	}
	for oid, want := range map[string]string{entry.Base: "base", entry.Ours: "ours", entry.Theirs: "theirs"} {
		blob, err := r.BlobContent(context.Background(), oid)
		if err != nil || strings.TrimSpace(blob.Text) != want {
			t.Fatalf("blob %s = %q (%v), want %q", oid[:7], blob.Text, err, want)
		}
	}
	content, ok, err := r.WorkingFile("f")
	if err != nil || !ok {
		t.Fatalf("working file: %v", err)
	}
	regions := ParseConflictRegions(content)
	if len(regions) != 1 {
		t.Fatalf("regions: %+v", regions)
	}
	region := regions[0]
	if !region.HasBase || !region.HasOurs || !region.HasTheirs {
		t.Fatalf("region sides: %+v", region)
	}
	if strings.Join(region.OursLines, "\n") != "ours" || strings.Join(region.TheirsLines, "\n") != "theirs" {
		t.Fatalf("region content: %+v", region)
	}
}

func TestResolveOursTheirsAndKeepBoth(t *testing.T) {
	r := mergeConflict(t)
	if err := r.ResolveOurs(context.Background(), "f", false); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := r.WorkingFile("f"); strings.TrimSpace(got) != "ours" {
		t.Fatalf("ours not written: %q", got)
	}
	// Writing a side does not mark it resolved on its own.
	if s := status(t, r); len(s.Groups[Conflicted]) != 1 {
		t.Fatalf("resolving a side must not resolve the index: %+v", s)
	}

	if err := r.ResolveTheirs(context.Background(), "f", false); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := r.WorkingFile("f"); strings.TrimSpace(got) != "theirs" {
		t.Fatalf("theirs not written: %q", got)
	}

	// Put the markers back and combine both sides predictably.
	write(t, r, "f", "<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> other\n")
	if err := r.KeepBoth(context.Background(), "f"); err != nil {
		t.Fatal(err)
	}
	got, _, _ := r.WorkingFile("f")
	if !strings.Contains(got, "ours") || !strings.Contains(got, "theirs") ||
		strings.Contains(got, "<<<<<<<") || strings.Contains(got, "=======") {
		t.Fatalf("keep both: %q", got)
	}
}

func TestMarkResolvedClearsConflict(t *testing.T) {
	r := mergeConflict(t)
	// Write a resolution that differs from HEAD so staging it is observable,
	// then mark it resolved the way the UI does.
	write(t, r, "f", "resolved\n")
	if err := r.MarkResolved(context.Background(), "f"); err != nil {
		t.Fatal(err)
	}
	if s := status(t, r); len(s.Groups[Conflicted]) != 0 {
		t.Fatalf("conflict not cleared: %+v", s)
	}
	if s := status(t, r); len(s.Groups[Staged]) != 1 {
		t.Fatalf("resolution not staged: %+v", s)
	}
}

func TestMergeContinueAndAbort(t *testing.T) {
	r := mergeConflict(t)
	if err := r.ResolveOurs(context.Background(), "f", true); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Continue(context.Background()); err != nil {
		t.Fatalf("continue: %v", err)
	}
	if state, _ := r.State(context.Background()); state.InProgress() {
		t.Fatalf("merge still in progress: %+v", state)
	}
	if parents := strings.Fields(gitCmd(t, r.Root, "rev-list", "--parents", "-n", "1", "HEAD")); len(parents) != 3 {
		t.Fatalf("continue did not create a merge commit: %v", parents)
	}

	// A fresh conflict can be aborted, returning to the pre-merge commit.
	r2 := mergeConflict(t)
	before := status(t, r2).OID
	if _, err := r2.Abort(context.Background()); err != nil {
		t.Fatalf("abort: %v", err)
	}
	if state, _ := r2.State(context.Background()); state.InProgress() {
		t.Fatalf("merge not aborted: %+v", state)
	}
	if after := status(t, r2).OID; after != before {
		t.Fatalf("abort moved HEAD: %s -> %s", before, after)
	}
}

func TestRebaseConflictContinueSkipAbort(t *testing.T) {
	r := fixture(t)
	write(t, r, "f", "base\n")
	commit(t, r)
	gitCmd(t, r.Root, "checkout", "-b", "feature")
	write(t, r, "g", "feature\n")
	commit(t, r)
	write(t, r, "f", "feature change\n")
	commit(t, r)
	gitCmd(t, r.Root, "checkout", "main")
	write(t, r, "f", "main change\n")
	commit(t, r)
	gitCmd(t, r.Root, "checkout", "feature")
	if _, err := run(context.Background(), r.Root, "rebase", "main"); err == nil {
		t.Fatal("expected a rebase conflict")
	}
	state, err := r.State(context.Background())
	if err != nil || state.Operation != OpRebase {
		t.Fatalf("rebase state: %+v %v", state, err)
	}
	if state.Step == 0 || state.Total == 0 {
		t.Fatalf("rebase progress not recorded: %+v", state)
	}
	if !state.CanSkip() || !state.CanAbort() {
		t.Fatalf("rebase controls: %+v", state)
	}

	// Resolve and continue.
	if err := r.ResolveTheirs(context.Background(), "f", true); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Continue(context.Background()); err != nil {
		t.Fatalf("rebase continue: %v", err)
	}
	if state, _ := r.State(context.Background()); state.InProgress() {
		t.Fatalf("rebase not finished: %+v", state)
	}

	// Skip: a fresh conflict, skipped, ends the rebase.
	r2 := fixture(t)
	write(t, r2, "f", "base\n")
	commit(t, r2)
	gitCmd(t, r2.Root, "checkout", "-b", "feature")
	write(t, r2, "f", "feature change\n")
	commit(t, r2)
	gitCmd(t, r2.Root, "checkout", "main")
	write(t, r2, "f", "main change\n")
	commit(t, r2)
	gitCmd(t, r2.Root, "checkout", "feature")
	if _, err := run(context.Background(), r2.Root, "rebase", "main"); err == nil {
		t.Fatal("expected a rebase conflict")
	}
	if _, err := r2.Skip(context.Background()); err != nil {
		t.Fatalf("rebase skip: %v", err)
	}
	if state, _ := r2.State(context.Background()); state.InProgress() {
		t.Fatalf("rebase not skipped to completion: %+v", state)
	}
}

func TestCherryPickConflictContinueAbort(t *testing.T) {
	r := fixture(t)
	write(t, r, "f", "base\n")
	commit(t, r)
	gitCmd(t, r.Root, "checkout", "-b", "feature")
	write(t, r, "f", "feature\n")
	commit(t, r)
	gitCmd(t, r.Root, "checkout", "main")
	write(t, r, "f", "main\n")
	commit(t, r)
	pick := strings.TrimSpace(gitCmd(t, r.Root, "rev-parse", "feature"))
	if _, err := r.CherryPick(context.Background(), pick); err == nil {
		t.Fatal("expected a cherry-pick conflict")
	}
	state, err := r.State(context.Background())
	if err != nil || state.Operation != OpCherryPick {
		t.Fatalf("cherry-pick state: %+v %v", state, err)
	}
	if _, err := r.Abort(context.Background()); err != nil {
		t.Fatalf("cherry-pick abort: %v", err)
	}
	if state, _ := r.State(context.Background()); state.InProgress() {
		t.Fatalf("cherry-pick not aborted: %+v", state)
	}
}

func TestRevertConflictAndSuccess(t *testing.T) {
	r := fixture(t)
	write(t, r, "f", "a\n")
	commit(t, r)
	write(t, r, "f", "b\n")
	commit(t, r)
	target := strings.TrimSpace(gitCmd(t, r.Root, "rev-parse", "HEAD~1"))
	if _, err := r.Revert(context.Background(), target); err == nil {
		t.Fatal("expected a revert conflict")
	}
	state, err := r.State(context.Background())
	if err != nil || state.Operation != OpRevert {
		t.Fatalf("revert state: %+v %v", state, err)
	}
	if _, err := r.Abort(context.Background()); err != nil {
		t.Fatalf("revert abort: %v", err)
	}

	// A revert that applies cleanly creates an inverse commit.
	r2 := fixture(t)
	write(t, r2, "f", "one\n")
	commit(t, r2)
	target = strings.TrimSpace(gitCmd(t, r2.Root, "rev-parse", "HEAD"))
	write(t, r2, "g", "two\n")
	commit(t, r2)
	if _, err := r2.Revert(context.Background(), target); err != nil {
		t.Fatalf("revert: %v", err)
	}
	subject := strings.TrimSpace(gitCmd(t, r2.Root, "log", "-1", "--format=%s"))
	if !strings.HasPrefix(subject, "Revert") {
		t.Fatalf("no inverse commit: %q", subject)
	}
	if state, _ := r2.State(context.Background()); state.InProgress() {
		t.Fatalf("revert left state: %+v", state)
	}
}

func TestConflictKinds(t *testing.T) {
	// Add/add: both branches add the same path with different content.
	r := fixture(t)
	write(t, r, "f", "base\n")
	commit(t, r)
	gitCmd(t, r.Root, "checkout", "-b", "other")
	write(t, r, "shared", "theirs\n")
	gitCmd(t, r.Root, "add", "shared")
	gitCmd(t, r.Root, "commit", "-m", "theirs")
	gitCmd(t, r.Root, "checkout", "main")
	write(t, r, "shared", "ours\n")
	gitCmd(t, r.Root, "add", "shared")
	gitCmd(t, r.Root, "commit", "-m", "ours")
	if _, err := run(context.Background(), r.Root, "merge", "other"); err == nil {
		t.Fatal("expected conflict")
	}
	if conflicts, _ := r.Conflicts(context.Background()); len(conflicts) != 1 || conflicts[0].Kind != ConflictBothAdded {
		t.Fatalf("both added: %+v", conflicts)
	}

	// Deleted by us: we removed the file, they changed it.
	r2 := fixture(t)
	write(t, r2, "f", "base\n")
	commit(t, r2)
	gitCmd(t, r2.Root, "checkout", "-b", "other")
	write(t, r2, "f", "theirs\n")
	commit(t, r2)
	gitCmd(t, r2.Root, "checkout", "main")
	if err := os.Remove(filepath.Join(r2.Root, "f")); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, r2.Root, "rm", "--", "f")
	commit(t, r2)
	if _, err := run(context.Background(), r2.Root, "merge", "other"); err == nil {
		t.Fatal("expected conflict")
	}
	conflicts, _ := r2.Conflicts(context.Background())
	if len(conflicts) != 1 || conflicts[0].Kind != ConflictDeletedByUs {
		t.Fatalf("deleted by us: %+v", conflicts)
	}
}

func TestResetModes(t *testing.T) {
	r := fixture(t)
	write(t, r, "f", "one\n")
	commit(t, r)
	write(t, r, "f", "two\n")
	commit(t, r)
	head := status(t, r).OID

	if _, err := r.Reset(context.Background(), ResetSoft, "HEAD~1"); err != nil {
		t.Fatal(err)
	}
	if s := status(t, r); len(s.Groups[Staged]) != 1 || len(s.Groups[Unstaged]) != 0 {
		t.Fatalf("soft reset index: %+v", s)
	}
	if got, _ := os.ReadFile(filepath.Join(r.Root, "f")); string(got) != "two\n" {
		t.Fatalf("soft reset changed the working tree: %q", got)
	}

	// Return to the committed state and try mixed: index resets, work kept.
	gitCmd(t, r.Root, "reset", "--hard", head)
	if _, err := r.Reset(context.Background(), ResetMixed, "HEAD~1"); err != nil {
		t.Fatal(err)
	}
	if s := status(t, r); len(s.Groups[Unstaged]) != 1 || len(s.Groups[Staged]) != 0 {
		t.Fatalf("mixed reset: %+v", s)
	}
	if got, _ := os.ReadFile(filepath.Join(r.Root, "f")); string(got) != "two\n" {
		t.Fatalf("mixed reset changed the working tree: %q", got)
	}

	// Hard reset discards tracked changes.
	if _, err := r.Reset(context.Background(), ResetHard, "HEAD"); err != nil {
		t.Fatal(err)
	}
	if s := status(t, r); len(s.Groups[Unstaged]) != 0 || len(s.Groups[Staged]) != 0 {
		t.Fatalf("hard reset left changes: %+v", s)
	}
	if got, _ := os.ReadFile(filepath.Join(r.Root, "f")); string(got) != "one\n" {
		t.Fatalf("hard reset content: %q", got)
	}
}

func TestReflogParsing(t *testing.T) {
	r := fixture(t)
	write(t, r, "f", "one\n")
	commit(t, r)
	write(t, r, "f", "two\n")
	commit(t, r)
	if _, err := r.Reset(context.Background(), ResetMixed, "HEAD~1"); err != nil {
		t.Fatal(err)
	}
	entries, err := r.Reflog(context.Background(), 20)
	if err != nil || len(entries) < 2 {
		t.Fatalf("reflog: %+v %v", entries, err)
	}
	first := entries[0]
	if first.Selector != "HEAD@{0}" || first.Action != "reset" {
		t.Fatalf("first entry: %+v", first)
	}
	if first.OID == "" || first.Short == "" || first.Time.IsZero() {
		t.Fatalf("entry missing facts: %+v", first)
	}
	for i, entry := range entries {
		if entry.Selector != "HEAD@{0}" && entry.Selector != "HEAD@{1}" && entry.Selector != "HEAD@{2}" {
			t.Fatalf("selector %d: %q", i, entry.Selector)
		}
	}
}

func TestRestoreFileVariants(t *testing.T) {
	r := fixture(t)
	write(t, r, "f", "one\n")
	commit(t, r)
	write(t, r, "f", "two\n")
	gitCmd(t, r.Root, "add", "f")
	write(t, r, "f", "three\n")

	// Restore the working tree only: the index still holds "two".
	if err := r.RestoreWorktree(context.Background(), "f"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(r.Root, "f")); string(got) != "two\n" {
		t.Fatalf("worktree restore: %q", got)
	}
	if err := r.RestoreStaged(context.Background(), "f"); err != nil {
		t.Fatal(err)
	}
	if s := status(t, r); len(s.Groups[Staged]) != 0 {
		t.Fatalf("index restore: %+v", s)
	}
	// Restore from a revision writes both index and working tree.
	if err := r.RestoreFrom(context.Background(), "HEAD", "f"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(r.Root, "f")); string(got) != "one\n" {
		t.Fatalf("restore from HEAD: %q", got)
	}
}

func TestConflictRegionLineNumbers(t *testing.T) {
	content := "before\n<<<<<<< HEAD\nours1\nours2\n||||||| base\nbase1\n=======\ntheirs1\n>>>>>>> other\nafter\n"
	regions := ParseConflictRegions(content)
	if len(regions) != 1 {
		t.Fatalf("regions: %+v", regions)
	}
	r := regions[0]
	if r.StartLine != 2 || r.OursStart != 3 || r.OursEnd != 4 {
		t.Fatalf("ours range: %+v", r)
	}
	if !r.HasBase || r.BaseStart != 6 || r.BaseEnd != 6 {
		t.Fatalf("base range: %+v", r)
	}
	if r.TheirsStart != 8 || r.TheirsEnd != 8 || r.EndLine != 9 {
		t.Fatalf("theirs range: %+v", r)
	}
	// A two-way conflict has no base marker; its ranges still line up.
	two := "a\n<<<<<<<\nb\n=======\nc\n>>>>>>>\nd\n"
	regions = ParseConflictRegions(two)
	if len(regions) != 1 || regions[0].HasBase {
		t.Fatalf("two-way: %+v", regions)
	}
	if regions[0].OursStart != 3 || regions[0].OursEnd != 3 || regions[0].TheirsStart != 5 || regions[0].TheirsEnd != 5 || regions[0].EndLine != 6 {
		t.Fatalf("two-way ranges: %+v", regions[0])
	}

	// A lone marker run in ordinary content is not a conflict.
	if regions := ParseConflictRegions("just\n=======\ntext\n"); len(regions) != 0 {
		t.Fatalf("false positive: %+v", regions)
	}
}

func TestNoOperationOnCleanRepo(t *testing.T) {
	r := fixture(t)
	write(t, r, "f", "one\n")
	commit(t, r)
	state, err := r.State(context.Background())
	if err != nil || state.InProgress() || state.Operation != OpNone {
		t.Fatalf("clean state: %+v %v", state, err)
	}
	if _, err := r.Continue(context.Background()); err == nil {
		t.Fatal("continue on a clean repository should fail")
	}
	var ce *CommandError
	if _, err := r.Abort(context.Background()); err == nil {
		t.Fatal("abort on a clean repository should fail")
	} else if !errors.As(err, &ce) && !strings.Contains(err.Error(), "in progress") {
		t.Fatalf("unexpected abort error: %v", err)
	}
}
