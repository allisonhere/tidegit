package git

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestStashListModel(t *testing.T) {
	r := fixture(t)
	write(t, r, "f", "base\n")
	commit(t, r)
	write(t, r, "f", "changed\n")
	if _, err := r.StashCreate(context.Background(), "refactor parser", false); err != nil {
		t.Fatal(err)
	}
	if s := status(t, r); len(s.Groups[Unstaged]) != 0 || len(s.Groups[Staged]) != 0 {
		t.Fatalf("working tree not clean after stash: %+v", s)
	}
	stashes, err := r.Stashes(context.Background())
	if err != nil || len(stashes) != 1 {
		t.Fatalf("stashes: %+v %v", stashes, err)
	}
	stash := stashes[0]
	if stash.Ref != "stash@{0}" || stash.Branch != "main" || stash.Message != "refactor parser" {
		t.Fatalf("stash model: %+v", stash)
	}
	if stash.OID == "" || stash.Short == "" || stash.Time.IsZero() {
		t.Fatalf("stash missing identity: %+v", stash)
	}

	// A default stash keeps Git's WIP wording, which parses the same way.
	write(t, r, "f", "again\n")
	if _, err := r.StashCreate(context.Background(), "", false); err != nil {
		t.Fatal(err)
	}
	stashes, err = r.Stashes(context.Background())
	if err != nil || len(stashes) != 2 {
		t.Fatalf("stashes after default: %+v %v", stashes, err)
	}
	if stashes[0].Branch != "main" || stashes[0].Message == "" {
		t.Fatalf("default stash wording: %+v", stashes[0])
	}
}

func TestStashIncludeUntracked(t *testing.T) {
	r := fixture(t)
	write(t, r, "tracked", "base\n")
	commit(t, r)
	write(t, r, "tracked", "changed\n")
	write(t, r, "untracked", "new\n")
	if _, err := r.StashCreate(context.Background(), "with untracked", true); err != nil {
		t.Fatal(err)
	}
	s := status(t, r)
	for i, group := range s.Groups {
		if len(group) != 0 {
			t.Fatalf("group %d not clean: %+v", i, group)
		}
	}
	files, err := r.StashFiles(context.Background(), "stash@{0}")
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	if !contains(paths, "tracked") || !contains(paths, "untracked") {
		t.Fatalf("stash files: %+v", files)
	}
	d, err := r.StashDiff(context.Background(), "stash@{0}", FileChange{Path: "untracked"})
	if err != nil || !strings.Contains(d.Patch, "+new") {
		t.Fatalf("untracked stash diff: %+v %v", d, err)
	}
}

func TestStashApplyKeepsEntryAndPopRemoves(t *testing.T) {
	r := fixture(t)
	write(t, r, "f", "base\n")
	commit(t, r)
	write(t, r, "f", "stashed\n")
	if _, err := r.StashCreate(context.Background(), "work", false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.StashApply(context.Background(), "stash@{0}"); err != nil {
		t.Fatal(err)
	}
	if s := status(t, r); len(s.Groups[Unstaged]) != 1 {
		t.Fatalf("apply did not restore changes: %+v", s)
	}
	if stashes, _ := r.Stashes(context.Background()); len(stashes) != 1 {
		t.Fatalf("apply removed the stash: %+v", stashes)
	}
	// Reset the working tree, then pop: changes return and the entry is gone.
	gitCmd(t, r.Root, "checkout", "--", "f")
	if _, err := r.StashPop(context.Background(), "stash@{0}"); err != nil {
		t.Fatal(err)
	}
	if s := status(t, r); len(s.Groups[Unstaged]) != 1 {
		t.Fatalf("pop did not restore changes: %+v", s)
	}
	if stashes, _ := r.Stashes(context.Background()); len(stashes) != 0 {
		t.Fatalf("pop left the stash: %+v", stashes)
	}
}

func TestStashDropRemovesOnlySelected(t *testing.T) {
	r := fixture(t)
	write(t, r, "f", "base\n")
	commit(t, r)
	for _, message := range []string{"first", "second", "third"} {
		write(t, r, "f", message+"\n")
		if _, err := r.StashCreate(context.Background(), message, false); err != nil {
			t.Fatal(err)
		}
	}
	stashes, err := r.Stashes(context.Background())
	if err != nil || len(stashes) != 3 {
		t.Fatalf("setup: %+v %v", stashes, err)
	}
	// Capture the identity of the middle entry, then drop index 1.
	target := stashes[1]
	if _, err := r.StashDrop(context.Background(), target.Ref); err != nil {
		t.Fatal(err)
	}
	remaining, err := r.Stashes(context.Background())
	if err != nil || len(remaining) != 2 {
		t.Fatalf("after drop: %+v %v", remaining, err)
	}
	for _, stash := range remaining {
		if stash.OID == target.OID {
			t.Fatalf("dropped the wrong stash: %+v", stash)
		}
	}
}

func TestStashConflictIsSurfaced(t *testing.T) {
	r := fixture(t)
	write(t, r, "f", "base\n")
	commit(t, r)
	write(t, r, "f", "stashed\n")
	if _, err := r.StashCreate(context.Background(), "conflicting", false); err != nil {
		t.Fatal(err)
	}
	write(t, r, "f", "local\n")
	commit(t, r)

	_, err := r.StashApply(context.Background(), "stash@{0}")
	var conflict *StashConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("want conflict error, got %v", err)
	}
	s := status(t, r)
	if len(s.Groups[Conflicted]) != 1 {
		t.Fatalf("conflict not visible in status: %+v", s)
	}
	// Git keeps the stash on a conflicted apply; TideGit must not have dropped it.
	if stashes, _ := r.Stashes(context.Background()); len(stashes) != 1 {
		t.Fatalf("stash was lost: %+v", stashes)
	}
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}
