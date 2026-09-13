package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// remoteFixture builds a working repository, a local bare remote and a second
// clone. Divergence is created between the two clones, entirely offline.
func remoteFixture(t *testing.T) (Repository, string, string) {
	t.Helper()
	r := fixture(t)
	write(t, r, "f", "base\n")
	commit(t, r)
	bare := t.TempDir()
	gitCmd(t, bare, "init", "--bare", "-b", "main")
	gitCmd(t, r.Root, "remote", "add", "origin", bare)
	gitCmd(t, r.Root, "push", "-u", "origin", "main")
	clone := filepath.Join(t.TempDir(), "clone")
	gitCmd(t, r.Root, "clone", bare, clone)
	gitCmd(t, clone, "config", "user.name", "TideGit Test")
	gitCmd(t, clone, "config", "user.email", "test@example.invalid")
	return r, bare, clone
}

func TestRemotesModel(t *testing.T) {
	r, bare, _ := remoteFixture(t)
	remotes, err := r.Remotes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(remotes) != 1 {
		t.Fatalf("want one remote, got %+v", remotes)
	}
	remote := remotes[0]
	if remote.Name != "origin" || remote.FetchURL != bare || remote.PushURL != bare {
		t.Fatalf("urls: %+v", remote)
	}
	if !remote.Default {
		t.Fatal("sole remote should be the default")
	}
	if len(remote.Branches) != 1 || remote.Branches[0] != "origin/main" {
		t.Fatalf("tracking branches: %+v", remote.Branches)
	}
	// A pushurl that differs from the fetch URL must be reported separately.
	gitCmd(t, r.Root, "config", "remote.origin.pushurl", bare+"-push")
	remotes, err = r.Remotes(context.Background())
	if err != nil || remotes[0].PushURL != bare+"-push" {
		t.Fatalf("pushurl: %+v %v", remotes, err)
	}
	// The current branch's remote outranks the only-remote fallback.
	if got, err := r.RemoteForBranch(context.Background(), "main"); err != nil || got != "origin" {
		t.Fatalf("remote for branch: %q %v", got, err)
	}
}

func TestFetchUpdatesTrackingWithoutTouchingLocal(t *testing.T) {
	r, _, clone := remoteFixture(t)
	before := status(t, r).OID
	write(t, Repository{clone}, "f", "clone change\n")
	gitCmd(t, clone, "add", "f")
	gitCmd(t, clone, "commit", "-m", "clone change")
	gitCmd(t, clone, "push", "origin", "main")

	if _, err := r.Fetch(context.Background(), "origin", false, nil); err != nil {
		t.Fatal(err)
	}
	if after := status(t, r).OID; after != before {
		t.Fatalf("fetch moved the local branch: %s -> %s", before, after)
	}
	s := status(t, r)
	if s.Ahead != 0 || s.Behind != 1 {
		t.Fatalf("ahead/behind after fetch: %+v", s)
	}
}

func TestPullFastForward(t *testing.T) {
	r, _, clone := remoteFixture(t)
	write(t, Repository{clone}, "f", "clone change\n")
	gitCmd(t, clone, "add", "f")
	gitCmd(t, clone, "commit", "-m", "clone change")
	gitCmd(t, clone, "push", "origin", "main")
	before := status(t, r).OID

	res, err := r.Pull(context.Background(), "", "", nil)
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if !strings.Contains(res.Stdout+res.Stderr, "Fast-forward") {
		t.Fatalf("unexpected pull output: %q", res.Stdout+res.Stderr)
	}
	if after := status(t, r).OID; after == before {
		t.Fatal("pull did not advance the branch")
	}
	if s := status(t, r); s.Ahead != 0 || s.Behind != 0 {
		t.Fatalf("not level after fast-forward: %+v", s)
	}
	// A second pull has nothing to do and says so rather than failing.
	res, err = r.Pull(context.Background(), "", "", nil)
	if err != nil || !strings.Contains(res.Stdout+res.Stderr, "Already up to date") {
		t.Fatalf("up-to-date pull: %q %v", res.Stdout+res.Stderr, err)
	}
}

func TestPushAndUpstreamSetup(t *testing.T) {
	r := fixture(t)
	write(t, r, "f", "one\n")
	commit(t, r)
	bare := t.TempDir()
	gitCmd(t, bare, "init", "--bare", "-b", "main")
	gitCmd(t, r.Root, "remote", "add", "origin", bare)
	if s := status(t, r); s.Upstream != "" {
		t.Fatalf("unexpected upstream before push: %+v", s)
	}
	res, err := r.Push(context.Background(), PushOptions{Remote: "origin", Branch: "main", SetUpstream: true}, nil)
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if describePushResult(res) == "" {
		t.Fatal("push produced no output")
	}
	if s := status(t, r); s.Upstream != "origin/main" || s.Ahead != 0 {
		t.Fatalf("upstream not recorded: %+v", s)
	}
	// The remote actually received the commit.
	if got := gitCmd(t, bare, "rev-parse", "main"); !strings.Contains(got, status(t, r).OID) {
		t.Fatalf("remote tip %q does not match %s", got, status(t, r).OID)
	}
}

func describePushResult(res Result) string {
	if upToDate, updates := DescribePush(res); upToDate || updates > 0 {
		return "ok"
	}
	return ""
}

func TestPushRejectedNonFastForward(t *testing.T) {
	r, _, clone := remoteFixture(t)
	write(t, r, "f", "local only\n")
	commit(t, r)
	write(t, Repository{clone}, "f", "remote change\n")
	gitCmd(t, clone, "add", "f")
	gitCmd(t, clone, "commit", "-m", "remote change")
	gitCmd(t, clone, "push", "origin", "main")

	_, err := r.Push(context.Background(), PushOptions{Remote: "origin", Branch: "main"}, nil)
	var remoteErr *RemoteError
	if !errors.As(err, &remoteErr) || remoteErr.Kind != RemoteNonFastForward {
		t.Fatalf("want non-fast-forward, got %v", err)
	}
	// The local branch is untouched and still holds the rejected commit.
	if s := status(t, r); s.Ahead != 1 {
		t.Fatalf("local state changed after rejection: %+v", s)
	}
}

func TestFetchClassifiesNetworkFailure(t *testing.T) {
	r := fixture(t)
	write(t, r, "f", "one\n")
	commit(t, r)
	gitCmd(t, r.Root, "remote", "add", "origin", "https://127.0.0.1:1/none.invalid/repo.git")
	_, err := r.Fetch(context.Background(), "origin", false, nil)
	var remoteErr *RemoteError
	if !errors.As(err, &remoteErr) {
		t.Fatalf("failure was not classified: %v", err)
	}
	if remoteErr.Kind != RemoteHostUnreachable && remoteErr.Kind != RemoteDNS && remoteErr.Kind != RemoteNotFound {
		t.Fatalf("unexpected classification: %+v", remoteErr)
	}
	if remoteErr.Message == "" {
		t.Fatal("no plain-language message")
	}
}

func TestDefaultRemoteIsNotChosenWhenAmbiguous(t *testing.T) {
	r := fixture(t)
	write(t, r, "f", "one\n")
	commit(t, r)
	gitCmd(t, r.Root, "remote", "add", "one", "https://example.invalid/one.git")
	gitCmd(t, r.Root, "remote", "add", "two", "https://example.invalid/two.git")
	remotes, err := r.Remotes(context.Background())
	if err != nil || len(remotes) != 2 {
		t.Fatalf("remotes: %+v %v", remotes, err)
	}
	for _, remote := range remotes {
		if remote.Default {
			t.Fatalf("ambiguous remote was marked default: %+v", remote)
		}
	}
}

func TestPullConflictClassified(t *testing.T) {
	r, _, clone := remoteFixture(t)
	// Force Git to merge rather than refuse an ambiguous divergence, so the
	// conflict itself is what the classification has to recognise.
	gitCmd(t, r.Root, "config", "pull.rebase", "false")
	write(t, r, "f", "local commit\n")
	commit(t, r)
	write(t, Repository{clone}, "f", "remote commit\n")
	gitCmd(t, clone, "add", "f")
	gitCmd(t, clone, "commit", "-m", "remote commit")
	gitCmd(t, clone, "push", "origin", "main")

	_, err := r.Pull(context.Background(), "", "", nil)
	var remoteErr *RemoteError
	if !errors.As(err, &remoteErr) || remoteErr.Kind != RemoteConflict {
		t.Fatalf("want conflict, got %v", err)
	}
	if s := status(t, r); len(s.Groups[Conflicted]) != 1 {
		t.Fatalf("conflict not left in the working tree: %+v", s)
	}
}

func TestPullRefusesDirtyWorktree(t *testing.T) {
	r, _, clone := remoteFixture(t)
	gitCmd(t, r.Root, "config", "pull.rebase", "false")
	write(t, Repository{clone}, "f", "remote commit\n")
	gitCmd(t, clone, "add", "f")
	gitCmd(t, clone, "commit", "-m", "remote commit")
	gitCmd(t, clone, "push", "origin", "main")
	write(t, r, "f", "uncommitted local work\n")

	_, err := r.Pull(context.Background(), "", "", nil)
	var remoteErr *RemoteError
	if !errors.As(err, &remoteErr) || remoteErr.Kind != RemoteDirtyWorktree {
		t.Fatalf("want dirty-worktree refusal, got %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(r.Root, "f")); string(got) != "uncommitted local work\n" {
		t.Fatalf("local work was disturbed: %q", got)
	}
}
