package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Remote is one configured Git remote: where it fetches from, where it pushes,
// which remote-tracking branches belong to it, and whether TideGit resolved it
// as the repository's default. URLs are kept verbatim; presentation decides how
// they are shown, never this layer.
type Remote struct {
	Name     string
	FetchURL string
	PushURL  string
	Default  bool
	Branches []string // remote-tracking branch short names, e.g. "origin/main"
}

// noRemoteEnv turns off Git's own terminal credential prompt for network
// commands. TideGit owns the terminal, so Git must not stop on an invisible
// prompt waiting for input that can never arrive. SSH agents, SSH config and
// credential helpers continue to work; only the interactive fallback is denied.
var noRemoteEnv = []string{"GIT_TERMINAL_PROMPT=0"}

// Remotes lists configured remotes with their URLs, tracking branches and the
// resolved default. Remote names and URLs come from Git configuration, never
// from human-oriented output.
func (r Repository) Remotes(ctx context.Context) ([]Remote, error) {
	namesRes, err := run(ctx, r.Root, "remote")
	if err != nil {
		return nil, err
	}
	if namesRes.Truncated {
		return nil, fmt.Errorf("remote list exceeds 4 MiB")
	}
	fetchURLs, pushURLs, err := r.remoteURLs(ctx)
	if err != nil {
		return nil, err
	}
	refsRes, err := run(ctx, r.Root, "for-each-ref", "--format=%(refname:short)", "refs/remotes")
	if err != nil {
		return nil, err
	}
	if refsRes.Truncated {
		return nil, fmt.Errorf("remote-tracking branch list exceeds 4 MiB")
	}
	var tracking []string
	for _, line := range strings.Split(refsRes.Stdout, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			tracking = append(tracking, line)
		}
	}
	var remotes []Remote
	for _, name := range strings.Split(namesRes.Stdout, "\n") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		remote := Remote{Name: name, FetchURL: fetchURLs[name], PushURL: pushURLs[name]}
		if remote.PushURL == "" {
			remote.PushURL = remote.FetchURL
		}
		for _, ref := range tracking {
			if ref == name || strings.HasPrefix(ref, name+"/") {
				// A remote's symbolic HEAD is an alias, not a branch.
				if strings.HasSuffix(ref, "/HEAD") {
					continue
				}
				remote.Branches = append(remote.Branches, ref)
			}
		}
		remotes = append(remotes, remote)
	}
	defaultRemote, err := r.DefaultRemote(ctx, remotes)
	if err != nil {
		return nil, err
	}
	for i := range remotes {
		remotes[i].Default = remotes[i].Name == defaultRemote
	}
	return remotes, nil
}

// remoteURLs reads remote.*.url and remote.*.pushurl from Git configuration.
// The --null framing keeps a URL containing spaces or newlines unambiguous.
func (r Repository) remoteURLs(ctx context.Context) (fetch, push map[string]string, err error) {
	fetch, push = map[string]string{}, map[string]string{}
	res, err := run(ctx, r.Root, "config", "--null", "--get-regexp", `^remote\..*\.(url|pushurl)$`)
	if err != nil {
		var ce *CommandError
		// Exit 1 means no remote URLs at all, which is an empty result.
		if asCommandError(err, &ce) && ce.Result.ExitCode == 1 {
			return fetch, push, nil
		}
		return nil, nil, err
	}
	if res.Truncated {
		return nil, nil, fmt.Errorf("remote configuration exceeds 4 MiB")
	}
	for _, record := range strings.Split(res.Stdout, "\x00") {
		if record == "" {
			continue
		}
		key, value, ok := strings.Cut(record, "\n")
		if !ok {
			continue
		}
		name, kind := "", ""
		switch {
		case strings.HasSuffix(key, ".pushurl") && strings.HasPrefix(key, "remote."):
			name, kind = strings.TrimSuffix(strings.TrimPrefix(key, "remote."), ".pushurl"), "push"
		case strings.HasSuffix(key, ".url") && strings.HasPrefix(key, "remote."):
			name, kind = strings.TrimSuffix(strings.TrimPrefix(key, "remote."), ".url"), "fetch"
		}
		if name == "" {
			continue
		}
		if kind == "push" {
			if _, seen := push[name]; !seen {
				push[name] = value
			}
		} else if _, seen := fetch[name]; !seen {
			fetch[name] = value
		}
	}
	return fetch, push, nil
}

// DefaultRemote resolves the remote a bare fetch or push should use: Git's own
// pushDefault, then the current branch's pushRemote or remote, then origin,
// then the only remote when there is exactly one. An ambiguous repository
// returns "", which the caller must resolve with an explicit choice.
func (r Repository) DefaultRemote(ctx context.Context, remotes []Remote) (string, error) {
	names := map[string]bool{}
	for _, remote := range remotes {
		names[remote.Name] = true
	}
	if pushDefault, err := r.config(ctx, "remote.pushDefault", false); err != nil {
		return "", err
	} else if pushDefault != "" && names[pushDefault] {
		return pushDefault, nil
	}
	head, err := r.ResolveHead(ctx)
	if err != nil {
		return "", err
	}
	if head.Branch != "" {
		if pushRemote, err := r.config(ctx, "branch."+head.Branch+".pushRemote", false); err != nil {
			return "", err
		} else if pushRemote != "" && names[pushRemote] {
			return pushRemote, nil
		}
		if branchRemote, err := r.config(ctx, "branch."+head.Branch+".remote", false); err != nil {
			return "", err
		} else if branchRemote != "" && branchRemote != "." && names[branchRemote] {
			return branchRemote, nil
		}
	}
	if names["origin"] {
		return "origin", nil
	}
	if len(remotes) == 1 {
		return remotes[0].Name, nil
	}
	return "", nil
}

// RemoteForBranch resolves the remote a branch pushes to, following the same
// precedence Git does.
func (r Repository) RemoteForBranch(ctx context.Context, branch string) (string, error) {
	if branch == "" {
		return "", nil
	}
	if pushRemote, err := r.config(ctx, "branch."+branch+".pushRemote", false); err != nil {
		return "", err
	} else if pushRemote != "" {
		return pushRemote, nil
	}
	if remote, err := r.config(ctx, "branch."+branch+".remote", false); err != nil {
		return "", err
	} else if remote != "" && remote != "." {
		return remote, nil
	}
	return r.config(ctx, "remote.pushDefault", false)
}

// LastFetchTime reports when FETCH_HEAD was last written, if it exists. It is a
// best-effort timestamp for the remote inspector, not an error when absent.
func (r Repository) LastFetchTime(ctx context.Context) (time.Time, bool) {
	res, err := run(ctx, r.Root, "rev-parse", "--git-path", "FETCH_HEAD")
	if err != nil {
		return time.Time{}, false
	}
	path := strings.TrimSuffix(res.Stdout, "\n")
	if path == "" {
		return time.Time{}, false
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(r.Root, path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}, false
	}
	return info.ModTime(), true
}

// validRemoteName rejects input shaped like a Git option. The name itself is
// owned by Git configuration; this only stops a typo from becoming a flag.
func validRemoteName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("enter a remote name")
	}
	if strings.HasPrefix(name, "-") || strings.ContainsAny(name, "\x00\r\n") {
		return fmt.Errorf("%q is not a valid remote name", name)
	}
	return nil
}

// Fetch updates one remote, or every remote when all is set. A single remote is
// never turned into --all on the caller's behalf.
func (r Repository) Fetch(ctx context.Context, remote string, all bool, progress ProgressFunc) (Result, error) {
	var result Result
	err := r.mutate(ctx, func() error {
		args := []string{"fetch", "--progress"}
		if all {
			args = append(args, "--all")
		} else {
			if err := validRemoteName(remote); err != nil {
				return err
			}
			args = append(args, "--", remote)
		}
		var runErr error
		result, runErr = runStream(ctx, r.Root, noRemoteEnv, progress, args...)
		if runErr != nil {
			return classifyRemoteError("fetch", runErr)
		}
		return nil
	})
	return result, err
}

// Pull integrates the current branch's upstream using the user's own Git
// configuration. It never chooses a merge or rebase strategy itself: if Git
// refuses because the configuration is ambiguous, that refusal is returned
// unchanged for the caller to explain.
func (r Repository) Pull(ctx context.Context, remote, branch string, progress ProgressFunc) (Result, error) {
	var result Result
	err := r.mutate(ctx, func() error {
		args := []string{"pull", "--progress"}
		if remote != "" || branch != "" {
			if err := validRemoteName(remote); err != nil {
				return err
			}
			if branch != "" {
				if err := validRevision(branch); err != nil {
					return err
				}
			}
			args = append(args, "--")
			args = append(args, remote)
			if branch != "" {
				args = append(args, branch)
			}
		}
		var runErr error
		result, runErr = runStream(ctx, r.Root, noRemoteEnv, progress, args...)
		if runErr != nil {
			return classifyRemoteError("pull", runErr)
		}
		return nil
	})
	return result, err
}

// PushOptions selects the destination and whether to record an upstream. Force
// is deliberately absent: plain push is the only default, and no key binding
// reaches this with force set.
type PushOptions struct {
	Remote, Branch string
	SetUpstream    bool
}

// Push publishes a branch with ordinary Git semantics. push.default,
// remote.pushDefault, branch configuration and signing all stay Git's. An empty
// Remote or Branch leaves that choice to Git, which is how push.default is
// honoured.
func (r Repository) Push(ctx context.Context, opts PushOptions, progress ProgressFunc) (Result, error) {
	var result Result
	err := r.mutate(ctx, func() error {
		args := []string{"push", "--progress"}
		if opts.SetUpstream {
			args = append(args, "--set-upstream")
		}
		if opts.Remote != "" {
			if err := validRemoteName(opts.Remote); err != nil {
				return err
			}
			args = append(args, "--", opts.Remote)
		}
		if opts.Branch != "" {
			if err := validRevision(opts.Branch); err != nil {
				return err
			}
			args = append(args, opts.Branch)
		}
		var runErr error
		result, runErr = runStream(ctx, r.Root, noRemoteEnv, progress, args...)
		if runErr != nil {
			return classifyRemoteError("push", runErr)
		}
		return nil
	})
	return result, err
}

// SetUpstream points a local branch at a remote-tracking branch. It requires
// that ref to exist, which a fetch or push establishes.
func (r Repository) SetUpstream(ctx context.Context, branch, remote, remoteBranch string) error {
	return r.mutate(ctx, func() error {
		if err := r.ValidateBranchName(ctx, branch); err != nil {
			return err
		}
		if err := validRemoteName(remote); err != nil {
			return err
		}
		if err := validRevision(remoteBranch); err != nil {
			return err
		}
		upstream := remote + "/" + remoteBranch
		if _, err := run(ctx, r.Root, "branch", "--set-upstream-to="+upstream, "--", branch); err != nil {
			return fmt.Errorf("could not set upstream of %q to %s: %w", branch, upstream, err)
		}
		return nil
	})
}

// RemoteErrorKind names the failure a person needs to act on. It never replaces
// Git's own stderr, which is retained in the wrapped error.
type RemoteErrorKind int

const (
	RemoteErrorOther RemoteErrorKind = iota
	RemoteAuth
	RemoteHostUnreachable
	RemoteDNS
	RemotePermission
	RemoteNotFound
	RemoteNonFastForward
	RemoteProtected
	RemoteNoUpstream
	RemotePullStrategy
	RemoteConflict
	RemoteDirtyWorktree
)

// RemoteError pairs a plain-language explanation with the underlying Git
// failure, so a screen can show both the meaning and the detail.
type RemoteError struct {
	Op      string
	Kind    RemoteErrorKind
	Message string
	Err     error
}

func (e *RemoteError) Error() string { return e.Message + " · " + e.Err.Error() }
func (e *RemoteError) Unwrap() error { return e.Err }

// classifyRemoteError turns Git's stderr into a typed failure without hiding
// it. Unknown failures keep Git's wording rather than being relabelled.
func classifyRemoteError(op string, err error) error {
	var ce *CommandError
	if !errors.As(err, &ce) {
		return err
	}
	stderr := strings.ToLower(ce.Result.Stderr + "\n" + ce.Result.Stdout)
	failure := func(kind RemoteErrorKind, message string) error {
		return &RemoteError{Op: op, Kind: kind, Message: message, Err: err}
	}
	switch {
	case strings.Contains(stderr, "could not resolve host") ||
		strings.Contains(stderr, "name or service not known") ||
		strings.Contains(stderr, "temporary failure in name resolution") ||
		strings.Contains(stderr, "no address associated with hostname"):
		return failure(RemoteDNS, "Could not resolve the remote host · check your network and DNS")
	case strings.Contains(stderr, "authentication failed") ||
		strings.Contains(stderr, "could not read username") ||
		strings.Contains(stderr, "could not read password") ||
		strings.Contains(stderr, "permission denied (publickey)") ||
		strings.Contains(stderr, "terminal prompts disabled") ||
		strings.Contains(stderr, "invalid username or password") ||
		strings.Contains(stderr, "support for password authentication was removed"):
		return failure(RemoteAuth, "Authentication failed · check your SSH agent, keys or credential helper")
	case strings.Contains(stderr, "connection timed out") ||
		strings.Contains(stderr, "connection refused") ||
		strings.Contains(stderr, "network is unreachable") ||
		strings.Contains(stderr, "could not connect") ||
		strings.Contains(stderr, "connection reset by peer") ||
		strings.Contains(stderr, "no route to host"):
		return failure(RemoteHostUnreachable, "Could not reach the remote host · check your network connection")
	case strings.Contains(stderr, "repository not found") ||
		strings.Contains(stderr, "does not appear to be a git repository") ||
		strings.Contains(stderr, "does not exist") && strings.Contains(stderr, "remote"):
		return failure(RemoteNotFound, "The remote repository could not be found")
	case strings.Contains(stderr, "permission to") && strings.Contains(stderr, "denied"):
		return failure(RemotePermission, "The remote denied access")
	case strings.Contains(stderr, "protected branch") ||
		strings.Contains(stderr, "pre-receive hook declined") ||
		strings.Contains(stderr, "remote rejected"):
		return failure(RemoteProtected, "The remote refused the update · protected branch or rejection hook")
	case strings.Contains(stderr, "non-fast-forward") ||
		strings.Contains(stderr, "fetch first") ||
		strings.Contains(stderr, "updates were rejected"):
		return failure(RemoteNonFastForward, "Push rejected · the remote has commits you do not have; fetch and integrate first")
	case strings.Contains(stderr, "no upstream branch") ||
		strings.Contains(stderr, "has no upstream branch"):
		return failure(RemoteNoUpstream, "The branch has no upstream · push and set one first")
	case strings.Contains(stderr, "divergent branches") ||
		strings.Contains(stderr, "need to specify how to reconcile"):
		return failure(RemotePullStrategy, "Git needs a pull strategy · configure pull.rebase or pull.ff, then retry")
	case strings.Contains(stderr, "automatic merge failed") ||
		strings.Contains(stderr, "conflict (content)") ||
		strings.Contains(stderr, "conflict (modify/delete)") ||
		strings.Contains(stderr, "fix conflicts"):
		return failure(RemoteConflict, "Pull produced conflicts · resolve them on the Status screen")
	case strings.Contains(stderr, "your local changes") && strings.Contains(stderr, "would be overwritten") ||
		strings.Contains(stderr, "cannot pull with rebase") ||
		strings.Contains(stderr, "please commit your changes or stash them"):
		return failure(RemoteDirtyWorktree, "Local changes would be overwritten · commit or stash them first")
	default:
		return failure(RemoteErrorOther, "Git could not complete the "+op)
	}
}

// DescribeFetch summarises a completed fetch. It is presentation-independent:
// the caller owns the wording around it.
func DescribeFetch(remote string, res Result) (upToDate bool, updates int) {
	if remote == "" {
		remote = "remote"
	}
	combined := res.Stdout + "\n" + res.Stderr
	if strings.Contains(combined, "Everything up-to-date") || strings.Contains(combined, "Already up to date") {
		return true, 0
	}
	updates = countRefUpdates(combined)
	return updates == 0, updates
}

// DescribePush summarises a completed push.
func DescribePush(res Result) (upToDate bool, updates int) {
	combined := res.Stdout + "\n" + res.Stderr
	if strings.Contains(combined, "Everything up-to-date") {
		return true, 0
	}
	updates = countRefUpdates(combined)
	return updates == 0, updates
}

// describePull words the three ordinary pull outcomes. Git only prints these on
// a successful, non-conflicted integration.
func describePull(res Result) string {
	switch {
	case strings.Contains(res.Stdout+res.Stderr, "Already up to date"):
		return "Already up to date"
	case strings.Contains(res.Stdout+res.Stderr, "Fast-forward"):
		return "Fast-forwarded"
	case strings.Contains(res.Stdout+res.Stderr, "Merge made by"):
		return "Merged"
	default:
		return "Pulled"
	}
}

// countRefUpdates counts fetch/push ref lines. Rejections are not success.
func countRefUpdates(text string) int {
	n := 0
	for _, line := range strings.Split(text, "\n") {
		if !strings.Contains(line, " -> ") && !strings.Contains(line, "..") {
			continue
		}
		if strings.Contains(line, "[rejected]") || strings.Contains(line, "[remote rejected]") {
			continue
		}
		if strings.Contains(line, "Everything up-to-date") {
			continue
		}
		n++
	}
	return n
}

// PullOutcome is the concise success wording for a pull.
func PullOutcome(res Result) string { return describePull(res) }
