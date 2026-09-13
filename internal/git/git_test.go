package git

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}
func fixture(t *testing.T) Repository {
	t.Helper()
	// Isolate fixtures from developer identity, signing, hooks and Git config.
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "TideGit Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "TideGit Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.invalid")
	dir := t.TempDir()
	gitCmd(t, dir, "init", "-b", "main")
	gitCmd(t, dir, "config", "user.name", "TideGit Test")
	gitCmd(t, dir, "config", "user.email", "test@example.invalid")
	return Repository{dir}
}
func write(t *testing.T, r Repository, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(r.Root, name), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}
func commit(t *testing.T, r Repository) {
	t.Helper()
	gitCmd(t, r.Root, "add", ".")
	gitCmd(t, r.Root, "commit", "-m", "fixture")
}
func status(t *testing.T, r Repository) Status {
	t.Helper()
	s, err := r.RepositoryStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func TestStatusLifecycle(t *testing.T) {
	r := fixture(t)
	s := status(t, r)
	if s.OID != "(initial)" || s.Branch != "main" {
		t.Fatalf("unborn: %+v", s)
	}
	name := "space tab\tline\nfile.txt"
	write(t, r, name, "one\n")
	s = status(t, r)
	if len(s.Groups[Untracked]) != 1 || s.Groups[Untracked][0].Path != name {
		t.Fatalf("untracked: %+v", s)
	}
	gitCmd(t, r.Root, "add", "--", name)
	write(t, r, name, "two\n")
	s = status(t, r)
	if len(s.Groups[Staged]) != 1 || len(s.Groups[Unstaged]) != 1 {
		t.Fatalf("both: %+v", s)
	}
	commit(t, r)
	s = status(t, r)
	for _, g := range s.Groups {
		if len(g) != 0 {
			t.Fatalf("dirty after commit: %+v", s)
		}
	}
	gitCmd(t, r.Root, "mv", "--", name, "renamed.txt")
	s = status(t, r)
	if len(s.Groups[Staged]) != 1 || s.Groups[Staged][0].OriginalPath != name {
		t.Fatalf("rename: %+v", s)
	}
	commit(t, r)
	if err := os.Remove(filepath.Join(r.Root, "renamed.txt")); err != nil {
		t.Fatal(err)
	}
	s = status(t, r)
	if len(s.Groups[Unstaged]) != 1 || s.Groups[Unstaged][0].XY != ".D" {
		t.Fatalf("deleted: %+v", s)
	}
	gitCmd(t, r.Root, "checkout", "--detach")
	s = status(t, r)
	if s.Branch != "(detached)" {
		t.Fatalf("detached: %+v", s)
	}
}
func TestDiscovery(t *testing.T) {
	r := fixture(t)
	sub := filepath.Join(r.Root, "a")
	if err := os.Mkdir(sub, 0700); err != nil {
		t.Fatal(err)
	}
	found, err := Discover(context.Background(), sub)
	if err != nil || found.Root != r.Root {
		t.Fatalf("%+v %v", found, err)
	}
	_, err = Discover(context.Background(), t.TempDir())
	var ce *CommandError
	if !errors.As(err, &ce) || ce.Result.ExitCode == 0 || ce.Result.Stderr == "" {
		t.Fatalf("missing Git failure detail: %v", err)
	}
}
func TestDiffStates(t *testing.T) {
	r := fixture(t)
	write(t, r, "f", "original\n")
	commit(t, r)
	write(t, r, "f", "staged\n")
	gitCmd(t, r.Root, "add", "f")
	write(t, r, "f", "working\n")
	for _, tc := range []struct {
		section      Section
		want, absent string
	}{{Staged, "+staged", "+working"}, {Unstaged, "+working", "+staged"}} {
		d, err := r.Diff(context.Background(), tc.section, File{Path: "f"})
		if err != nil || !strings.Contains(d.Patch, tc.want) || strings.Contains(d.Patch, tc.absent) {
			t.Fatalf("%+v %v", d, err)
		}
	}
	write(t, r, "new", "new file\n")
	d, err := r.Diff(context.Background(), Untracked, File{Path: "new"})
	if err != nil || !strings.Contains(d.Patch, "+new file") {
		t.Fatalf("%+v %v", d, err)
	}
	write(t, r, "binary", "a\x00b")
	d, err = r.Diff(context.Background(), Untracked, File{Path: "binary"})
	if err != nil || !strings.Contains(d.Patch, "Binary files") {
		t.Fatalf("%+v %v", d, err)
	}
	write(t, r, "[special]*", "literal\n")
	gitCmd(t, r.Root, "add", "--", "[special]*")
	d, err = r.Diff(context.Background(), Staged, File{Path: "[special]*"})
	if err != nil || !strings.Contains(d.Patch, "+literal") || strings.Contains(d.Patch, "+staged") {
		t.Fatalf("literal path: %+v %v", d, err)
	}
}
func TestConflict(t *testing.T) {
	r := fixture(t)
	write(t, r, "f", "base\n")
	commit(t, r)
	gitCmd(t, r.Root, "checkout", "-b", "other")
	write(t, r, "f", "other\n")
	commit(t, r)
	gitCmd(t, r.Root, "checkout", "main")
	write(t, r, "f", "main\n")
	commit(t, r)
	_, err := run(context.Background(), r.Root, "merge", "other")
	if err == nil {
		t.Fatal("expected conflict")
	}
	s := status(t, r)
	if len(s.Groups[Conflicted]) != 1 || len(s.Groups[Staged]) != 0 || len(s.Groups[Unstaged]) != 0 {
		t.Fatalf("conflict groups: %+v", s)
	}
	d, err := r.Diff(context.Background(), Conflicted, s.Groups[Conflicted][0])
	if err != nil || !d.Conflict || !strings.Contains(d.Patch, "++<<<<<<<") {
		t.Fatalf("conflict diff: %+v %v", d, err)
	}
}
func TestUpstreamCounts(t *testing.T) {
	r := fixture(t)
	write(t, r, "f", "base\n")
	commit(t, r)
	if status(t, r).Upstream != "" {
		t.Fatal("unexpected upstream")
	}
	remote := t.TempDir()
	gitCmd(t, remote, "init", "--bare", "-b", "main")
	gitCmd(t, r.Root, "remote", "add", "origin", remote)
	gitCmd(t, r.Root, "push", "-u", "origin", "main")
	write(t, r, "f", "ahead\n")
	commit(t, r)
	s := status(t, r)
	if s.Upstream != "origin/main" || s.Ahead != 1 || s.Behind != 0 {
		t.Fatalf("ahead: %+v", s)
	}
	gitCmd(t, r.Root, "push")
	gitCmd(t, r.Root, "reset", "--hard", "HEAD~1")
	s = status(t, r)
	if s.Ahead != 0 || s.Behind != 1 {
		t.Fatalf("behind: %+v", s)
	}
}
func TestBoundedOutputAndCancellation(t *testing.T) {
	var b boundedBuffer
	n, err := b.Write([]byte(strings.Repeat("x", outputLimit+100)))
	if err != nil || n != outputLimit+100 || b.Len() != outputLimit || !b.truncated {
		t.Fatal("output bound failed")
	}
	r := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = r.RepositoryStatus(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}
func TestParserRejectsMalformedRecords(t *testing.T) {
	for _, input := range []string{"1 broken\x00", "2 R. N... 100644 100644 100644 abc def R100 dest\x00", "u broken\x00", "# branch.ab bad\x00", "x nope\x00"} {
		if _, err := ParseStatus(input); err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
}

func TestRenameAndDeletedDiff(t *testing.T) {
	r := fixture(t)
	write(t, r, "before", "original\n")
	commit(t, r)
	gitCmd(t, r.Root, "mv", "before", "after")
	s := status(t, r)
	d, err := r.Diff(context.Background(), Staged, s.Groups[Staged][0])
	if err != nil || !strings.Contains(d.Patch, "rename from before") || !strings.Contains(d.Patch, "rename to after") {
		t.Fatalf("rename: %+v %v", d, err)
	}
	commit(t, r)
	if err := os.Remove(filepath.Join(r.Root, "after")); err != nil {
		t.Fatal(err)
	}
	d, err = r.Diff(context.Background(), Unstaged, File{Path: "after"})
	if err != nil || !strings.Contains(d.Patch, "deleted file") || !strings.Contains(d.Patch, "-original") {
		t.Fatalf("deleted: %+v %v", d, err)
	}
}

func TestLargeDiffIsExplicitlyTruncated(t *testing.T) {
	r := fixture(t)
	write(t, r, "large", strings.Repeat("long text line\n", 400000))
	d, err := r.Diff(context.Background(), Untracked, File{Path: "large"})
	if err != nil || !d.Truncated || len(d.Patch) > outputLimit {
		t.Fatalf("large diff: length=%d truncated=%t error=%v", len(d.Patch), d.Truncated, err)
	}
}
