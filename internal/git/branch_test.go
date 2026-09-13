package git

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func branchNames(branches []Branch) []string {
	out := make([]string, len(branches))
	for i, b := range branches {
		out[i] = b.Name
	}
	return out
}

func findBranch(t *testing.T, branches []Branch, name string) Branch {
	t.Helper()
	for _, b := range branches {
		if b.Name == name {
			return b
		}
	}
	t.Fatalf("no branch %q in %v", name, branchNames(branches))
	return Branch{}
}

func TestBranchListingMetadata(t *testing.T) {
	r, oid := topologyFixture(t)
	ctx := context.Background()
	local, remote, err := r.Branches(ctx)
	must(t, err)

	if len(local) != 3 {
		t.Fatalf("local branches: %v", branchNames(local))
	}
	main := findBranch(t, local, "main")
	if !main.Current || main.Remote || main.OID != oid["E"] {
		t.Fatalf("current branch: %+v", main)
	}
	if main.Subject != "Add e" || main.Author != "TideGit Test" || main.CommitTime.IsZero() {
		t.Fatalf("branch tip metadata: %+v", main)
	}
	// origin/main sits at B. main reaches four commits B cannot: the two
	// feature commits the merge brought in, the merge itself, and "Add e".
	if main.Upstream != "origin/main" || main.Ahead != 4 || main.Behind != 0 {
		t.Fatalf("upstream tracking: %+v", main)
	}
	if feature := findBranch(t, local, "feature"); !feature.Merged || feature.Current {
		t.Fatalf("merged branch: %+v", feature)
	}
	if other := findBranch(t, local, "other"); other.Merged {
		t.Fatal("diverged branch reported as merged")
	}
	if findBranch(t, local, "feature").Upstream != "" {
		t.Fatal("branch without configured tracking reported an upstream")
	}

	if len(remote) != 2 {
		t.Fatalf("remote branches: %v", branchNames(remote))
	}
	originMain := findBranch(t, remote, "origin/main")
	if !originMain.Remote || originMain.RemoteName != "origin" || originMain.OID != oid["B"] {
		t.Fatalf("remote branch: %+v", originMain)
	}
	if len(originMain.TrackedByLocals) != 1 || originMain.TrackedByLocals[0] != "main" {
		t.Fatalf("remote branch did not report its local tracker: %+v", originMain)
	}
	if findBranch(t, remote, "origin/feature").TrackedByLocals != nil {
		t.Fatal("untracked remote branch reported a tracker")
	}
}

func TestBranchBehindAndGoneUpstream(t *testing.T) {
	r, oid := topologyFixture(t)
	ctx := context.Background()
	// Move the remote ref ahead of main so main is both ahead and behind.
	gitCmd(t, r.Root, "update-ref", "refs/remotes/origin/main", oid["F"])
	local, _, err := r.Branches(ctx)
	must(t, err)
	main := findBranch(t, local, "main")
	if main.Ahead == 0 || main.Behind == 0 {
		t.Fatalf("diverged tracking: ahead %d behind %d", main.Ahead, main.Behind)
	}

	gitCmd(t, r.Root, "update-ref", "-d", "refs/remotes/origin/main")
	local, _, err = r.Branches(ctx)
	must(t, err)
	if gone := findBranch(t, local, "main"); !gone.UpstreamGone {
		t.Fatalf("deleted upstream not reported as gone: %+v", gone)
	}
}

func TestDivergenceAndMergeBase(t *testing.T) {
	r, oid := topologyFixture(t)
	ctx := context.Background()
	ahead, behind, err := r.Divergence(ctx, "main", "other")
	must(t, err)
	// main holds B, C, D, M and E that other lacks; other holds only F.
	if ahead != 5 || behind != 1 {
		t.Fatalf("divergence: %d ahead, %d behind", ahead, behind)
	}
	base, err := r.MergeBase(ctx, "main", "other")
	must(t, err)
	if base != oid["A"] {
		t.Fatalf("merge base: %s", base)
	}
	// An unrelated history has no common ancestor, which is a fact, not an error.
	gitCmd(t, r.Root, "checkout", "--orphan", "unrelated")
	gitCmd(t, r.Root, "rm", "-rf", "--cached", ".")
	write(t, r, "solo.txt", "solo\n")
	gitCmd(t, r.Root, "add", "solo.txt")
	gitCmd(t, r.Root, "commit", "-m", "unrelated root")
	base, err = r.MergeBase(ctx, "main", "unrelated")
	must(t, err)
	if base != "" {
		t.Fatalf("unrelated histories reported merge base %q", base)
	}
}

func TestSwitchBranch(t *testing.T) {
	r, _ := topologyFixture(t)
	ctx := context.Background()
	must(t, r.SwitchBranch(ctx, "feature"))
	head, err := r.ResolveHead(ctx)
	must(t, err)
	if head.Branch != "feature" || head.Detached {
		t.Fatalf("after switch: %+v", head)
	}
	if worktree(t, r, "c.txt") != "c\n" {
		t.Fatal("switch did not update the working tree")
	}
	local, _, err := r.Branches(ctx)
	must(t, err)
	if !findBranch(t, local, "feature").Current || findBranch(t, local, "main").Current {
		t.Fatal("branch listing did not follow HEAD")
	}
}

func TestSwitchBlockedByLocalChanges(t *testing.T) {
	r, _ := topologyFixture(t)
	ctx := context.Background()
	// e.txt exists on main but not on feature, so an uncommitted edit to it
	// cannot survive the switch and Git must refuse.
	write(t, r, "e.txt", "local work that must not be lost\n")
	err := r.SwitchBranch(ctx, "feature")
	if err == nil {
		t.Fatal("switch discarded uncommitted work")
	}
	if !strings.Contains(err.Error(), "feature") || !strings.Contains(err.Error(), "working tree unchanged") {
		t.Fatalf("unhelpful error: %v", err)
	}
	if !strings.Contains(err.Error(), "overwritten") && !strings.Contains(err.Error(), "e.txt") {
		t.Fatalf("error does not carry Git's explanation: %v", err)
	}
	head, err2 := r.ResolveHead(ctx)
	must(t, err2)
	if head.Branch != "main" {
		t.Fatalf("refused switch still moved HEAD: %+v", head)
	}
	if worktree(t, r, "e.txt") != "local work that must not be lost\n" {
		t.Fatal("refused switch changed the working tree")
	}
}

func TestCreateBranch(t *testing.T) {
	r, oid := topologyFixture(t)
	ctx := context.Background()

	must(t, r.CreateBranch(ctx, "from-head", "", false))
	resolved, err := r.ResolveRef(ctx, "from-head")
	must(t, err)
	if resolved != oid["E"] {
		t.Fatalf("branch from HEAD: %s", resolved)
	}
	// Creating a branch must not move HEAD unless asked.
	head, err := r.ResolveHead(ctx)
	must(t, err)
	if head.Branch != "main" {
		t.Fatalf("creation switched branches: %+v", head)
	}

	must(t, r.CreateBranch(ctx, "from-commit", oid["C"], false))
	if resolved, err = r.ResolveRef(ctx, "from-commit"); err != nil || resolved != oid["C"] {
		t.Fatalf("branch from commit: %s %v", resolved, err)
	}
	must(t, r.CreateBranch(ctx, "from-tag", "v1.0", false))
	if resolved, err = r.ResolveRef(ctx, "from-tag"); err != nil || resolved != oid["B"] {
		t.Fatalf("branch from tag: %s %v", resolved, err)
	}

	must(t, r.CreateBranch(ctx, "checked-out", oid["D"], true))
	head, err = r.ResolveHead(ctx)
	must(t, err)
	if head.Branch != "checked-out" || head.OID != oid["D"] {
		t.Fatalf("explicit checkout: %+v", head)
	}

	if err := r.CreateBranch(ctx, "main", "", false); err == nil {
		t.Fatal("duplicate branch name accepted")
	}
	for _, bad := range []string{"", "  ", "-force", "bad name", "bad..name", "refs/heads/x/"} {
		if err := r.CreateBranch(ctx, bad, "", false); err == nil {
			t.Fatalf("invalid branch name accepted: %q", bad)
		}
	}
	// A rejected name must not have created anything.
	local, _, err := r.Branches(ctx)
	must(t, err)
	for _, b := range local {
		if strings.Contains(b.Name, "force") || strings.Contains(b.Name, " ") {
			t.Fatalf("invalid branch created: %q", b.Name)
		}
	}
}

func TestRenameBranch(t *testing.T) {
	r, oid := topologyFixture(t)
	ctx := context.Background()

	must(t, r.RenameBranch(ctx, "other", "renamed-other"))
	if _, err := r.ResolveRef(ctx, "other"); err == nil {
		t.Fatal("old branch name still resolves")
	}
	resolved, err := r.ResolveRef(ctx, "renamed-other")
	must(t, err)
	if resolved != oid["F"] {
		t.Fatalf("rename moved the branch: %s", resolved)
	}

	// Renaming the current branch is Git's own behaviour: HEAD follows it.
	must(t, r.RenameBranch(ctx, "main", "trunk"))
	head, err := r.ResolveHead(ctx)
	must(t, err)
	if head.Branch != "trunk" || head.Detached || head.OID != oid["E"] {
		t.Fatalf("renaming the current branch: %+v", head)
	}
	local, _, err := r.Branches(ctx)
	must(t, err)
	if !findBranch(t, local, "trunk").Current {
		t.Fatal("renamed current branch lost its marker")
	}

	if err := r.RenameBranch(ctx, "trunk", "feature"); err == nil {
		t.Fatal("rename onto an existing branch accepted")
	}
	if err := r.RenameBranch(ctx, "trunk", "bad name"); err == nil {
		t.Fatal("invalid new name accepted")
	}
	if head, err = r.ResolveHead(ctx); err != nil || head.Branch != "trunk" {
		t.Fatalf("failed rename disturbed HEAD: %+v %v", head, err)
	}
}

func TestDeleteBranchSafety(t *testing.T) {
	r, oid := topologyFixture(t)
	ctx := context.Background()

	// feature is merged into main, so a safe delete is allowed.
	must(t, r.DeleteBranch(ctx, "feature", false))
	if _, err := r.ResolveRef(ctx, "feature"); err == nil {
		t.Fatal("merged branch still resolves after delete")
	}
	// Deleting a branch must not touch the commits or any other branch.
	if resolved, err := r.ResolveRef(ctx, oid["D"]); err != nil || resolved != oid["D"] {
		t.Fatal("delete removed the branch's commits")
	}
	local, _, err := r.Branches(ctx)
	must(t, err)
	if len(local) != 2 || !findBranch(t, local, "main").Current {
		t.Fatalf("delete disturbed other branches: %v", branchNames(local))
	}

	// other holds a commit no other branch contains: a safe delete must refuse
	// and must not escalate to a forced delete on its own.
	err = r.DeleteBranch(ctx, "other", false)
	if err == nil {
		t.Fatal("unmerged branch was deleted by a safe delete")
	}
	if !errors.Is(err, ErrUnmergedBranch) {
		t.Fatalf("unmerged refusal not typed: %v", err)
	}
	if !strings.Contains(err.Error(), "other") || !strings.Contains(err.Error(), "not merged") {
		t.Fatalf("unexplained refusal: %v", err)
	}
	if resolved, err := r.ResolveRef(ctx, "other"); err != nil || resolved != oid["F"] {
		t.Fatal("refused delete removed the branch anyway")
	}

	// Forcing is available, but only when the caller explicitly asks for it.
	must(t, r.DeleteBranch(ctx, "other", true))
	if _, err := r.ResolveRef(ctx, "other"); err == nil {
		t.Fatal("forced delete left the branch")
	}

	if err := r.DeleteBranch(ctx, "main", false); err == nil {
		t.Fatal("deleted the checked-out branch")
	}
	if head, err := r.ResolveHead(ctx); err != nil || head.Branch != "main" {
		t.Fatalf("failed delete disturbed HEAD: %+v %v", head, err)
	}
}

func TestBranchNameValidation(t *testing.T) {
	r, _ := topologyFixture(t)
	ctx := context.Background()
	for _, good := range []string{"feature/login", "release-1.2", "a"} {
		if err := r.ValidateBranchName(ctx, good); err != nil {
			t.Fatalf("rejected valid name %q: %v", good, err)
		}
	}
	for _, bad := range []string{"", "   ", "-x", "has space", "double..dot", "trailing.lock", "end/"} {
		if err := r.ValidateBranchName(ctx, bad); err == nil {
			t.Fatalf("accepted invalid name %q", bad)
		}
	}
}
