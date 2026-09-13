package git

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Branch is one local or remote-tracking branch with the metadata the branch
// inspector shows. Ahead and Behind are counts against Upstream and are only
// meaningful when Upstream is set.
type Branch struct {
	Name, Full      string
	OID, Short      string
	Remote          bool
	Current         bool
	Upstream        string
	UpstreamGone    bool // the configured upstream no longer exists
	Ahead, Behind   int
	Subject         string
	Author          string
	CommitTime      time.Time
	Merged          bool // reachable from HEAD
	RemoteName      string
	TrackedByLocals []string // local branches tracking this remote branch
}

// branchFormat and its field order must stay in step with parseBranches.
const branchFormat = "%(refname)" + fieldSep + "%(refname:short)" + fieldSep +
	"%(objectname)" + fieldSep + "%(objectname:short)" + fieldSep +
	"%(HEAD)" + fieldSep + "%(upstream:short)" + fieldSep +
	"%(upstream:track,nobracket)" + fieldSep + "%(contents:subject)" + fieldSep +
	"%(authorname)" + fieldSep + "%(committerdate:unix)" + recordSep

// Branches lists local and remote-tracking branches. Ahead/behind comes from
// for-each-ref's own tracking data, so the whole list costs two Git commands
// rather than one per branch.
func (r Repository) Branches(ctx context.Context) (local, remote []Branch, err error) {
	res, err := run(ctx, r.Root, "for-each-ref", "--format="+branchFormat,
		"--sort=-committerdate", "refs/heads", "refs/remotes")
	if err != nil {
		return nil, nil, err
	}
	if res.Truncated {
		return nil, nil, fmt.Errorf("branch list exceeds 4 MiB")
	}
	all, err := parseBranches(res.Stdout)
	if err != nil {
		return nil, nil, err
	}
	merged, err := r.mergedRefs(ctx)
	if err != nil {
		return nil, nil, err
	}
	tracked := map[string][]string{}
	for _, b := range all {
		if !b.Remote && b.Upstream != "" {
			tracked[b.Upstream] = append(tracked[b.Upstream], b.Name)
		}
	}
	for _, b := range all {
		b.Merged = merged[b.Full]
		if b.Remote {
			b.RemoteName, _, _ = strings.Cut(b.Name, "/")
			b.TrackedByLocals = tracked[b.Name]
			remote = append(remote, b)
			continue
		}
		local = append(local, b)
	}
	return local, remote, nil
}

func parseBranches(raw string) ([]Branch, error) {
	var out []Branch
	for _, record := range strings.Split(raw, recordSep) {
		record = strings.TrimLeft(record, "\n")
		if strings.TrimSpace(record) == "" {
			continue
		}
		f := strings.Split(record, fieldSep)
		if len(f) != 10 {
			return nil, fmt.Errorf("malformed branch record with %d fields", len(f))
		}
		b := Branch{Full: f[0], Name: f[1], OID: f[2], Short: f[3], Current: f[4] == "*",
			Upstream: f[5], Subject: f[7], Author: f[8]}
		b.Remote = strings.HasPrefix(b.Full, "refs/remotes/")
		// A remote's symbolic HEAD is an alias for another branch, not a branch.
		if b.Remote && strings.HasSuffix(b.Name, "/HEAD") {
			continue
		}
		b.Ahead, b.Behind, b.UpstreamGone = parseTrack(f[6])
		if f[9] != "" {
			n, err := strconv.ParseInt(f[9], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("malformed branch timestamp %q", f[9])
			}
			b.CommitTime = time.Unix(n, 0)
		}
		out = append(out, b)
	}
	return out, nil
}

// parseTrack reads for-each-ref's tracking summary, which with nobracket is
// "ahead 3", "behind 1", "ahead 3, behind 1", "gone", or empty.
func parseTrack(field string) (ahead, behind int, gone bool) {
	field = strings.TrimSpace(field)
	if field == "" {
		return 0, 0, false
	}
	if field == "gone" {
		return 0, 0, true
	}
	for _, part := range strings.Split(field, ", ") {
		kind, value, ok := strings.Cut(strings.TrimSpace(part), " ")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(value)
		if err != nil {
			continue
		}
		switch kind {
		case "ahead":
			ahead = n
		case "behind":
			behind = n
		}
	}
	return ahead, behind, false
}

// mergedRefs reports which refs are reachable from HEAD.
func (r Repository) mergedRefs(ctx context.Context) (map[string]bool, error) {
	merged := map[string]bool{}
	res, err := run(ctx, r.Root, "for-each-ref", "--merged=HEAD", "--format=%(refname)",
		"refs/heads", "refs/remotes")
	if err != nil {
		// An unborn HEAD has nothing to be merged into, which is not an error.
		if isUnbornRevision(err) {
			return merged, nil
		}
		return nil, err
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			merged[line] = true
		}
	}
	return merged, nil
}

// Divergence counts commits on each side of two revisions. It is used for a
// remote branch the inspector is showing, where no stored tracking data exists.
func (r Repository) Divergence(ctx context.Context, left, right string) (ahead, behind int, err error) {
	if err := validRevision(left); err != nil {
		return 0, 0, err
	}
	if err := validRevision(right); err != nil {
		return 0, 0, err
	}
	res, err := run(ctx, r.Root, "rev-list", "--left-right", "--count", left+"..."+right, "--")
	if err != nil {
		return 0, 0, err
	}
	f := strings.Fields(res.Stdout)
	if len(f) != 2 {
		return 0, 0, fmt.Errorf("malformed divergence count %q", res.Stdout)
	}
	if ahead, err = strconv.Atoi(f[0]); err != nil {
		return 0, 0, err
	}
	if behind, err = strconv.Atoi(f[1]); err != nil {
		return 0, 0, err
	}
	return ahead, behind, nil
}

// MergeBase returns the best common ancestor of two revisions, or "" when the
// histories are unrelated.
func (r Repository) MergeBase(ctx context.Context, left, right string) (string, error) {
	if err := validRevision(left); err != nil {
		return "", err
	}
	if err := validRevision(right); err != nil {
		return "", err
	}
	res, err := run(ctx, r.Root, "merge-base", left, right)
	if err != nil {
		var ce *CommandError
		// Exit code 1 means no common ancestor, which is a fact, not a failure.
		if asCommandError(err, &ce) && ce.Result.ExitCode == 1 {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSuffix(res.Stdout, "\n"), nil
}

// ValidateBranchName asks Git whether a name is usable before any mutation, so
// an obvious typo is refused without touching the repository. Git remains the
// authority: creation can still fail for reasons only Git knows.
func (r Repository) ValidateBranchName(ctx context.Context, name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("enter a branch name")
	}
	if strings.HasPrefix(name, "-") {
		return fmt.Errorf("a branch name cannot start with %q", "-")
	}
	if _, err := run(ctx, r.Root, "check-ref-format", "--branch", name); err != nil {
		return fmt.Errorf("%q is not a valid branch name", name)
	}
	return nil
}

// SwitchBranch checks out an existing local branch with Git's own switch, so
// checkout semantics, including refusing to discard local modifications, stay
// Git's. Nothing is stashed, reset or force-checked-out on the caller's behalf.
func (r Repository) SwitchBranch(ctx context.Context, name string) error {
	return r.mutate(ctx, func() error {
		if err := r.ValidateBranchName(ctx, name); err != nil {
			return err
		}
		if _, err := run(ctx, r.Root, "switch", "--", name); err != nil {
			return fmt.Errorf("could not switch to %q (working tree unchanged): %w", name, err)
		}
		return nil
	})
}

// CreateBranch creates a local branch at start, which may be a branch, tag or
// commit. An empty start uses HEAD. The branch is only checked out when the
// caller explicitly asks for it.
func (r Repository) CreateBranch(ctx context.Context, name, start string, checkout bool) error {
	return r.mutate(ctx, func() error {
		if err := r.ValidateBranchName(ctx, name); err != nil {
			return err
		}
		if start == "" {
			start = "HEAD"
		}
		if err := validRevision(start); err != nil {
			return err
		}
		args := []string{"branch", "--", name, start}
		if checkout {
			args = []string{"switch", "--create", name, start, "--"}
		}
		if _, err := run(ctx, r.Root, args...); err != nil {
			return fmt.Errorf("could not create branch %q at %s: %w", name, start, err)
		}
		return nil
	})
}

// RenameBranch renames a local branch. Renaming the current branch is Git's
// own supported behaviour and leaves HEAD pointing at the new name.
func (r Repository) RenameBranch(ctx context.Context, oldName, newName string) error {
	return r.mutate(ctx, func() error {
		if err := r.ValidateBranchName(ctx, oldName); err != nil {
			return err
		}
		if err := r.ValidateBranchName(ctx, newName); err != nil {
			return err
		}
		if _, err := run(ctx, r.Root, "branch", "--move", "--", oldName, newName); err != nil {
			return fmt.Errorf("could not rename %q to %q: %w", oldName, newName, err)
		}
		return nil
	})
}

// ErrUnmergedBranch reports a safe delete that Git refused because the branch
// holds commits no other branch contains. The caller must decide, explicitly,
// whether to ask for a forced delete; nothing escalates on its own.
var ErrUnmergedBranch = errors.New("branch has unmerged commits")

// DeleteBranch deletes a local branch. Safe deletion is the default and refuses
// to discard unmerged work; force is only ever used when the caller passes it.
func (r Repository) DeleteBranch(ctx context.Context, name string, force bool) error {
	return r.mutate(ctx, func() error {
		if err := r.ValidateBranchName(ctx, name); err != nil {
			return err
		}
		flag := "--delete"
		if force {
			flag = "-D"
		}
		_, err := run(ctx, r.Root, "branch", flag, "--", name)
		if err == nil {
			return nil
		}
		var ce *CommandError
		if !force && asCommandError(err, &ce) && strings.Contains(ce.Result.Stderr, "not fully merged") {
			return fmt.Errorf("%q holds commits that are not merged into HEAD: %w", name, ErrUnmergedBranch)
		}
		return fmt.Errorf("could not delete branch %q: %w", name, err)
	})
}

// asCommandError unwraps to the structured Git command failure, if any.
func asCommandError(err error, target **CommandError) bool { return errors.As(err, target) }
