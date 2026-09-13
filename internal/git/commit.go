package git

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type CommitInfo struct {
	Status                                       Status
	Message, Template, CommentPrefix, IndexToken string
	Additions, Deletions, Binary                 int
}
type CommitOptions struct {
	Message                     string
	Amend, Signoff              bool
	ExpectedHead, ExpectedIndex string
}
type CommitResult struct {
	OID, Subject, Warning string
	Output                Result
}

func (r Repository) config(ctx context.Context, key string, path bool) (string, error) {
	args := []string{"config"}
	if path {
		args = append(args, "--path")
	}
	args = append(args, "--get", key)
	res, err := run(ctx, r.Root, args...)
	var ce *CommandError
	if errors.As(err, &ce) && ce.Result.ExitCode == 1 {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if res.Truncated {
		return "", fmt.Errorf("Git configuration value too large: %s", key)
	}
	return strings.TrimSuffix(res.Stdout, "\n"), nil
}

func (r Repository) indexToken(ctx context.Context) (string, error) {
	res, err := run(ctx, r.Root, "ls-files", "--stage", "-z")
	if err != nil {
		return "", err
	}
	if res.Truncated {
		return "", fmt.Errorf("index exceeds review limit; commit from Git externally")
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(res.Stdout))), nil
}

func (r Repository) meaningfulCommit(ctx context.Context, s Status, amend bool) error {
	if len(s.Groups[Conflicted]) > 0 {
		return fmt.Errorf("resolve conflicts before committing")
	}
	if amend {
		if s.OID == "(initial)" || s.OID == "" {
			return fmt.Errorf("there is no HEAD commit to amend")
		}
		return nil
	}
	res, err := run(ctx, r.Root, "diff", "--cached", "--quiet", "--exit-code")
	var ce *CommandError
	if errors.As(err, &ce) && res.ExitCode == 1 {
		return nil
	}
	if err != nil {
		return err
	}
	// Git permits a merge commit with an unchanged tree.
	merge, err := run(ctx, r.Root, "rev-parse", "--git-path", "MERGE_HEAD")
	if err != nil {
		return err
	}
	p := strings.TrimSuffix(merge.Stdout, "\n")
	if !filepath.IsAbs(p) {
		p = filepath.Join(r.Root, p)
	}
	if _, err := os.Stat(p); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return fmt.Errorf("nothing staged: stage a file or hunk before committing")
}

// PrepareCommit loads Git-owned metadata; it never stages or changes the index.
func (r Repository) PrepareCommit(ctx context.Context, amend bool) (CommitInfo, error) {
	var info CommitInfo
	before, err := r.indexToken(ctx)
	if err != nil {
		return info, err
	}
	info.Status, err = r.RepositoryStatus(ctx)
	if err != nil {
		return info, err
	}
	if err = r.meaningfulCommit(ctx, info.Status, amend); err != nil {
		return info, err
	}
	info.CommentPrefix, err = r.config(ctx, "core.commentString", false)
	if err != nil {
		return info, err
	}
	if info.CommentPrefix == "" {
		info.CommentPrefix, err = r.config(ctx, "core.commentChar", false)
		if err != nil {
			return info, err
		}
	}
	if info.CommentPrefix == "" || info.CommentPrefix == "auto" {
		info.CommentPrefix = "#"
	}
	if amend {
		res, e := run(ctx, r.Root, "show", "-s", "--format=format:%B", "HEAD")
		if e != nil {
			return info, e
		}
		if res.Truncated {
			return info, fmt.Errorf("HEAD message exceeds editor limit")
		}
		info.Message = res.Stdout
	} else {
		path, e := r.config(ctx, "commit.template", true)
		if e != nil {
			return info, e
		}
		if path != "" {
			if !filepath.IsAbs(path) {
				path = filepath.Join(r.Root, path)
			}
			f, e := os.Open(path)
			if e != nil {
				return info, fmt.Errorf("load commit template: %w", e)
			}
			b, e := io.ReadAll(io.LimitReader(f, outputLimit+1))
			f.Close()
			if e != nil {
				return info, e
			}
			if len(b) > outputLimit {
				return info, fmt.Errorf("commit template exceeds 4 MiB")
			}
			info.Template = string(b)
			info.Message = info.Template
		}
	}
	res, err := run(ctx, r.Root, "diff", "--cached", "--numstat", "-z", "--find-renames", "--no-ext-diff", "--no-textconv")
	if err != nil {
		return info, err
	}
	if res.Truncated {
		return info, fmt.Errorf("staged summary exceeds review limit")
	}
	records := strings.Split(res.Stdout, "\x00")
	for i := 0; i < len(records); i++ {
		if records[i] == "" {
			continue
		}
		p := strings.SplitN(records[i], "\t", 3)
		if len(p) != 3 {
			return info, fmt.Errorf("malformed staged numstat")
		}
		if p[0] == "-" {
			info.Binary++
		} else {
			a, e := strconv.Atoi(p[0])
			if e != nil {
				return info, e
			}
			d, e := strconv.Atoi(p[1])
			if e != nil {
				return info, e
			}
			info.Additions += a
			info.Deletions += d
		}
		if p[2] == "" {
			i += 2
		}
	}
	info.IndexToken, err = r.indexToken(ctx)
	if err != nil {
		return info, err
	}
	if before != info.IndexToken {
		return info, fmt.Errorf("index changed during review; refresh and try again")
	}
	return info, nil
}

// Commit invokes normal Git porcelain. Hooks, signing and identity remain Git's
// responsibility. No fixed timeout: hooks and pinentry may legitimately wait.
func (r Repository) Commit(ctx context.Context, options CommitOptions) (CommitResult, error) {
	var result CommitResult
	err := r.mutate(ctx, func() error {
		s, err := r.RepositoryStatus(ctx)
		if err != nil {
			return err
		}
		if options.ExpectedHead != "" && options.ExpectedHead != s.OID {
			return fmt.Errorf("HEAD changed since review; refresh the commit inspector before retrying")
		}
		if err = r.meaningfulCommit(ctx, s, options.Amend); err != nil {
			return err
		}
		if options.ExpectedIndex != "" {
			token, e := r.indexToken(ctx)
			if e != nil {
				return e
			}
			if token != options.ExpectedIndex {
				return fmt.Errorf("staged content changed since review; press Ctrl-R to review it before retrying")
			}
		}
		args := []string{"commit", "--file=-"}
		cleanup, err := r.config(ctx, "commit.cleanup", false)
		if err != nil {
			return err
		}
		// -F's implicit default is whitespace, whereas a composed message uses
		// strip. Preserve an explicit user choice, including verbatim/whitespace.
		if cleanup == "" || cleanup == "default" {
			args = append(args, "--cleanup=strip")
		}
		if options.Amend {
			args = append(args, "--amend")
		}
		if options.Signoff {
			args = append(args, "--signoff")
		}
		result.Output, err = runInput(ctx, r.Root, options.Message, args...)
		if err != nil {
			return fmt.Errorf("commit was not completed: %w", err)
		}
		res, e := run(ctx, r.Root, "log", "-1", "--format=%H%x00%s")
		if e != nil {
			result.Warning = "Commit completed, but its summary could not be loaded: " + e.Error()
			return nil
		}
		result.OID, result.Subject, _ = strings.Cut(strings.TrimSuffix(res.Stdout, "\n"), "\x00")
		return nil
	})
	return result, err
}
