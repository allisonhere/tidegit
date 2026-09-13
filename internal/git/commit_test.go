package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func stagedCommitRepo(t *testing.T) Repository {
	t.Helper()
	r := fixture(t)
	write(t, r, "f", "baseline\n")
	commit(t, r)
	write(t, r, "f", "staged\n")
	must(t, r.StageFile(context.Background(), File{Path: "f"}))
	return r
}
func hook(t *testing.T, r Repository, name, body string) {
	t.Helper()
	must(t, os.WriteFile(filepath.Join(r.Root, ".git", "hooks", name), []byte("#!/bin/sh\n"+body+"\n"), 0700))
}
func TestCommitMessageAndState(t *testing.T) {
	r := stagedCommitRepo(t)
	ctx := context.Background()
	message := "Keep quotes \" and ' and $HOME `touch nope` — 海\n\nBody with two paragraphs.\n\nLast paragraph.\n"
	info, err := r.PrepareCommit(ctx, false)
	must(t, err)
	if info.Additions != 1 || info.Deletions != 1 {
		t.Fatalf("bad summary: %+v", info)
	}
	res, err := r.Commit(ctx, CommitOptions{Message: message, ExpectedHead: info.Status.OID, ExpectedIndex: info.IndexToken})
	must(t, err)
	if res.OID == "" || !strings.HasPrefix(res.Subject, "Keep quotes") {
		t.Fatal("missing commit summary")
	}
	if got := gitCmd(t, r.Root, "show", "-s", "--format=format:%B", "HEAD"); got != message {
		t.Fatalf("message mismatch: %q", got)
	}
	if len(status(t, r).Groups[Staged]) != 0 || len(status(t, r).Groups[Unstaged]) != 0 {
		t.Fatal("commit did not refresh index")
	}
	if _, err := os.Stat(filepath.Join(r.Root, "nope")); !os.IsNotExist(err) {
		t.Fatal("message executed as shell")
	}
}
func TestNothingStagedAndEmptyMessage(t *testing.T) {
	r := fixture(t)
	ctx := context.Background()
	_, err := r.PrepareCommit(ctx, false)
	if err == nil || !strings.Contains(err.Error(), "nothing staged") {
		t.Fatalf("%v", err)
	}
	_, err = r.Commit(ctx, CommitOptions{Message: "no changes"})
	if err == nil {
		t.Fatal("empty repo committed")
	}
	write(t, r, "f", "new\n")
	must(t, r.StageFile(ctx, File{Path: "f"}))
	_, err = r.Commit(ctx, CommitOptions{Message: "# only a comment\n"})
	if err == nil {
		t.Fatal("empty cleaned message committed")
	}
	if status(t, r).OID != "(initial)" || worktree(t, r, "f") != "new\n" {
		t.Fatal("failed commit changed state")
	}
}
func TestCommitHooksRunNormally(t *testing.T) {
	r := stagedCommitRepo(t)
	ctx := context.Background()
	for _, name := range []string{"pre-commit", "prepare-commit-msg", "commit-msg", "post-commit"} {
		hook(t, r, name, "echo "+name+" >> .git/hook-trace")
	}
	_, err := r.Commit(ctx, CommitOptions{Message: "through hooks"})
	must(t, err)
	b, err := os.ReadFile(filepath.Join(r.Root, ".git", "hook-trace"))
	must(t, err)
	if string(b) != "pre-commit\nprepare-commit-msg\ncommit-msg\npost-commit\n" {
		t.Fatalf("hook trace: %s", b)
	}
}
func TestCommitHookFailures(t *testing.T) {
	for _, name := range []string{"pre-commit", "prepare-commit-msg", "commit-msg"} {
		t.Run(name, func(t *testing.T) {
			r := stagedCommitRepo(t)
			old := status(t, r).OID
			hook(t, r, name, "echo 'hook stdout context'; echo 'message rejected by "+name+"' >&2; exit 1")
			res, err := r.Commit(context.Background(), CommitOptions{Message: "recoverable draft"})
			if err == nil || !strings.Contains(err.Error(), name) || !strings.Contains(res.Output.Stdout+res.Output.Stderr, "hook stdout") {
				t.Fatalf("missing hook output: %+v %v", res, err)
			}
			if status(t, r).OID != old || len(status(t, r).Groups[Staged]) != 1 || worktree(t, r, "f") != "staged\n" {
				t.Fatal("rejected commit changed repository")
			}
		})
	}
}
func TestAmendWithAndWithoutStagedChanges(t *testing.T) {
	r := stagedCommitRepo(t)
	ctx := context.Background()
	info, err := r.PrepareCommit(ctx, true)
	must(t, err)
	if info.Message != "fixture\n" {
		t.Fatalf("amend draft: %q", info.Message)
	}
	old := info.Status.OID
	res, err := r.Commit(ctx, CommitOptions{Message: "amended subject", Amend: true})
	must(t, err)
	if res.OID == old || res.Subject != "amended subject" || gitCmd(t, r.Root, "show", "HEAD:f") != "staged\n" {
		t.Fatal("amend incorrect")
	}
	_, err = r.PrepareCommit(ctx, true)
	must(t, err)
	res, err = r.Commit(ctx, CommitOptions{Message: "message-only amendment", Amend: true})
	must(t, err)
	if res.Subject != "message-only amendment" || strings.TrimSpace(gitCmd(t, r.Root, "rev-list", "--count", "HEAD")) != "1" {
		t.Fatal("message-only amend made extra commit")
	}
}
func TestTemplateCleanupAndSignoff(t *testing.T) {
	r := stagedCommitRepo(t)
	ctx := context.Background()
	template := "# subject guide\n\n# body guide\n"
	write(t, r, "template", template)
	gitCmd(t, r.Root, "config", "commit.template", "template")
	info, err := r.PrepareCommit(ctx, false)
	must(t, err)
	if info.Message != template || info.Template != template {
		t.Fatal("template not loaded verbatim")
	}
	_, err = r.Commit(ctx, CommitOptions{Message: "Subject\n\n# help text\nBody\n", Signoff: true})
	must(t, err)
	msg := gitCmd(t, r.Root, "show", "-s", "--format=format:%B", "HEAD")
	if strings.Contains(msg, "# help") || !strings.Contains(msg, "Signed-off-by: TideGit Test <test@example.invalid>") {
		t.Fatalf("cleanup/signoff: %q", msg)
	}
	gitCmd(t, r.Root, "config", "commit.cleanup", "verbatim")
	_, err = r.Commit(ctx, CommitOptions{Message: "Subject\n\n# intentionally retained\n", Amend: true})
	must(t, err)
	if !strings.Contains(gitCmd(t, r.Root, "show", "-s", "--format=format:%B", "HEAD"), "# intentionally retained") {
		t.Fatal("overrode configured cleanup")
	}
}
func TestSigningConfigurationIsNotDisabled(t *testing.T) {
	r := stagedCommitRepo(t)
	gitCmd(t, r.Root, "config", "commit.gpgsign", "true")
	program := filepath.Join(r.Root, "fake-gpg")
	must(t, os.WriteFile(program, []byte("#!/bin/sh\necho 'isolated signing failure' >&2\nexit 1\n"), 0700))
	gitCmd(t, r.Root, "config", "gpg.program", program)
	_, err := r.Commit(context.Background(), CommitOptions{Message: "must sign"})
	if err == nil || !strings.Contains(err.Error(), "sign") {
		t.Fatalf("signing bypassed: %v", err)
	}
}
func TestCommitRejectsChangedReview(t *testing.T) {
	r := stagedCommitRepo(t)
	ctx := context.Background()
	info, err := r.PrepareCommit(ctx, false)
	must(t, err)
	write(t, r, "f", "external stage\n")
	must(t, r.StageFile(ctx, File{Path: "f"}))
	_, err = r.Commit(ctx, CommitOptions{Message: "stale review", ExpectedIndex: info.IndexToken, ExpectedHead: info.Status.OID})
	if err == nil || !strings.Contains(err.Error(), "staged content changed") {
		t.Fatalf("stale review: %v", err)
	}
}
func TestConflictsBlockCommit(t *testing.T) {
	r := fixture(t)
	ctx := context.Background()
	write(t, r, "f", "base\n")
	commit(t, r)
	gitCmd(t, r.Root, "checkout", "-b", "side")
	write(t, r, "f", "side\n")
	commit(t, r)
	gitCmd(t, r.Root, "checkout", "main")
	write(t, r, "f", "main\n")
	commit(t, r)
	// git merge exits non-zero on conflict, which is the state under test.
	merge := exec.Command("git", "merge", "side")
	merge.Dir = r.Root
	if out, err := merge.CombinedOutput(); err == nil || !strings.Contains(string(out), "CONFLICT") {
		t.Fatalf("expected a conflicting merge: %v\n%s", err, out)
	}
	if len(status(t, r).Groups[Conflicted]) == 0 {
		t.Fatal("no conflicted entry")
	}
	if _, err := r.PrepareCommit(ctx, false); err == nil || !strings.Contains(err.Error(), "resolve conflicts") {
		t.Fatalf("conflicted review: %v", err)
	}
	if _, err := r.Commit(ctx, CommitOptions{Message: "merge anyway"}); err == nil || !strings.Contains(err.Error(), "resolve conflicts") {
		t.Fatalf("conflicted commit: %v", err)
	}
	// Resolving without changing the tree still leaves a legitimate merge commit.
	write(t, r, "f", "main\n")
	must(t, r.StageFile(ctx, File{Path: "f"}))
	info, err := r.PrepareCommit(ctx, false)
	must(t, err)
	if _, err := r.Commit(ctx, CommitOptions{Message: "merge side", ExpectedHead: info.Status.OID, ExpectedIndex: info.IndexToken}); err != nil {
		t.Fatalf("merge commit refused: %v", err)
	}
	if strings.TrimSpace(gitCmd(t, r.Root, "rev-list", "--count", "--merges", "HEAD")) != "1" {
		t.Fatal("merge commit not recorded")
	}
}
func TestUnbornHeadCommitAndAmend(t *testing.T) {
	r := fixture(t)
	ctx := context.Background()
	if _, err := r.PrepareCommit(ctx, true); err == nil || !strings.Contains(err.Error(), "no HEAD commit to amend") {
		t.Fatalf("unborn amend: %v", err)
	}
	write(t, r, "f", "first\n")
	must(t, r.StageFile(ctx, File{Path: "f"}))
	info, err := r.PrepareCommit(ctx, false)
	must(t, err)
	if info.Status.OID != "(initial)" || info.Additions != 1 {
		t.Fatalf("unborn review: %+v", info)
	}
	res, err := r.Commit(ctx, CommitOptions{Message: "First commit", ExpectedHead: info.Status.OID, ExpectedIndex: info.IndexToken})
	must(t, err)
	if res.Subject != "First commit" || strings.TrimSpace(gitCmd(t, r.Root, "rev-list", "--count", "HEAD")) != "1" {
		t.Fatal("initial commit incorrect")
	}
}
func TestCommitRejectsChangedHead(t *testing.T) {
	r := stagedCommitRepo(t)
	ctx := context.Background()
	info, err := r.PrepareCommit(ctx, false)
	must(t, err)
	write(t, r, "other", "external\n")
	gitCmd(t, r.Root, "add", "other")
	gitCmd(t, r.Root, "commit", "-m", "external commit")
	_, err = r.Commit(ctx, CommitOptions{Message: "stale head", ExpectedHead: info.Status.OID})
	if err == nil || !strings.Contains(err.Error(), "HEAD changed") {
		t.Fatalf("stale HEAD: %v", err)
	}
}
func TestConfiguredCommentPrefix(t *testing.T) {
	r := stagedCommitRepo(t)
	ctx := context.Background()
	gitCmd(t, r.Root, "config", "core.commentChar", ";")
	info, err := r.PrepareCommit(ctx, false)
	must(t, err)
	if info.CommentPrefix != ";" {
		t.Fatalf("comment prefix: %q", info.CommentPrefix)
	}
	_, err = r.Commit(ctx, CommitOptions{Message: "Subject\n\n; dropped guidance\nBody\n"})
	must(t, err)
	msg := gitCmd(t, r.Root, "show", "-s", "--format=format:%B", "HEAD")
	if strings.Contains(msg, "dropped guidance") || !strings.Contains(msg, "Body") {
		t.Fatalf("configured comment prefix ignored: %q", msg)
	}
}
