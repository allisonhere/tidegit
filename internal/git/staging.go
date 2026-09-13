package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Serialize TideGit mutations per repository. Git's own index.lock remains the
// authority for coordination with other processes. Waiting is cancellable.
var mutationLocks sync.Map

func (r Repository) mutate(ctx context.Context, fn func() error) error {
	root, err := filepath.Abs(r.Root)
	if err != nil {
		return err
	}
	key, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	v, _ := mutationLocks.LoadOrStore(key, make(chan struct{}, 1))
	lock := v.(chan struct{})
	select {
	case lock <- struct{}{}:
		defer func() { <-lock }()
		return fn()
	case <-ctx.Done():
		return ctx.Err()
	}
}

func validPath(path string) error {
	if path == "" || filepath.IsAbs(path) || filepath.Clean(path) == "." || filepath.Clean(path) == ".." || strings.HasPrefix(filepath.Clean(path), ".."+string(filepath.Separator)) {
		return fmt.Errorf("expected a repository-relative file path, got %q", path)
	}
	return nil
}

func (r Repository) StageFile(ctx context.Context, f File) error {
	return r.mutate(ctx, func() error {
		if err := validPath(f.Path); err != nil {
			return err
		}
		info, statErr := os.Lstat(filepath.Join(r.Root, f.Path))
		if statErr != nil && !os.IsNotExist(statErr) {
			return fmt.Errorf("could not stage %q: %w", f.Path, statErr)
		}
		if info != nil && info.IsDir() {
			return fmt.Errorf("could not stage %q: selected path is a directory; recursive staging is not supported", f.Path)
		}
		args := []string{"add", "--", f.Path}
		// Only a worktree-side rename consumes the old path. A staged rename's
		// old name may already have been recreated as a different untracked file.
		if len(f.XY) == 2 && f.XY[1] == 'R' && f.OriginalPath != "" {
			if err := validPath(f.OriginalPath); err != nil {
				return err
			}
			args = append(args, f.OriginalPath)
		}
		_, err := run(ctx, r.Root, args...)
		if err != nil {
			return fmt.Errorf("could not stage file %q: %w", f.Path, err)
		}
		return nil
	})
}

func (r Repository) UnstageFile(ctx context.Context, f File) error {
	return r.mutate(ctx, func() error {
		if err := validPath(f.Path); err != nil {
			return err
		}
		s, err := r.RepositoryStatus(ctx)
		if err != nil {
			return err
		}
		args := []string{"restore", "--staged", "--source=HEAD", "--", f.Path}
		if s.OID == "(initial)" {
			args = []string{"rm", "--cached", "-f", "--", f.Path}
		}
		if len(f.XY) == 2 && f.XY[0] == 'R' && f.OriginalPath != "" {
			if err := validPath(f.OriginalPath); err != nil {
				return err
			}
			args = append(args, f.OriginalPath)
		}
		_, err = run(ctx, r.Root, args...)
		if err != nil {
			return fmt.Errorf("could not unstage file %q (working tree preserved): %w", f.Path, err)
		}
		return nil
	})
}

func (r Repository) StageHunk(ctx context.Context, h Hunk) error   { return r.applyHunk(ctx, h, false) }
func (r Repository) UnstageHunk(ctx context.Context, h Hunk) error { return r.applyHunk(ctx, h, true) }

func (r Repository) applyHunk(ctx context.Context, h Hunk, reverse bool) error {
	return r.mutate(ctx, func() error {
		action := "stage"
		if reverse {
			action = "unstage"
		}
		fail := func(err error) error {
			return fmt.Errorf("could not %s hunk of %q from %s: %w", action, h.File.Path, sectionName(h.Source), err)
		}
		if err := validPath(h.File.Path); err != nil {
			return fail(err)
		}
		if reverse && h.Source != Staged || !reverse && h.Source != Unstaged && h.Source != Untracked {
			return fail(fmt.Errorf("wrong diff source"))
		}
		// Re-read and compare the semantic target before applying. This rejects
		// stale or caller-modified patches, including truncated and binary input.
		d, err := r.Diff(ctx, h.Source, h.File)
		if err != nil {
			return fail(err)
		}
		var target *Hunk
		for i := range d.Hunks {
			candidate := &d.Hunks[i]
			if candidate.ID == h.ID && candidate.patch() == h.patch() {
				target = candidate
				break
			}
		}
		if target == nil {
			return fail(fmt.Errorf("patch is stale or malformed; refresh and select the hunk again"))
		}
		args := []string{"apply", "--cached", "--whitespace=nowarn"}
		if reverse {
			args = append(args, "--reverse")
		}
		args = append(args, "-")
		_, err = runInput(ctx, r.Root, target.patch(), args...)
		if err != nil {
			return fail(err)
		}
		return nil
	})
}

func sectionName(s Section) string {
	if s < Staged || s > Conflicted {
		return "unknown source"
	}
	return SectionNames[s]
}
