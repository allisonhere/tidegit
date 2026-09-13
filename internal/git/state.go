package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// OperationKind is the multi-step Git operation the repository is part-way
// through, if any. It is derived from Git's own state files, never from UI
// history, so a fresh process discovers it after a restart.
type OperationKind int

const (
	OpNone OperationKind = iota
	OpMerge
	OpRebase
	OpCherryPick
	OpRevert
	OpBisect
)

// Label is the lowercase name used in sentences.
func (k OperationKind) Label() string {
	switch k {
	case OpMerge:
		return "merge"
	case OpRebase:
		return "rebase"
	case OpCherryPick:
		return "cherry-pick"
	case OpRevert:
		return "revert"
	case OpBisect:
		return "bisect"
	default:
		return "operation"
	}
}

// Banner is the uppercase global state indicator.
func (k OperationKind) Banner() string {
	if k == OpNone {
		return ""
	}
	return strings.ToUpper(k.Label()) + " IN PROGRESS"
}

// RepoState describes a special repository state: the operation in progress,
// its progress where Git records it, and the HEAD facts the UI needs even
// before a status scan completes.
type RepoState struct {
	Operation OperationKind
	// Step and Total are 1-based progress for a rebase or a pick/revert
	// sequence. Zero means Git did not record a position.
	Step, Total int
	// Detail names the moving parts, e.g. the branch being rebased or the
	// branch's starting point, in plain language.
	Detail string
	// Branch is the branch being operated on, when known.
	Branch    string
	Detached  bool
	Unborn    bool
	Conflicts int
}

// InProgress reports whether a multi-step operation is active.
func (s RepoState) InProgress() bool { return s.Operation != OpNone }

// State detects the repository's current operation from Git's state files. It
// never mutates anything and never guesses from process memory.
func (r Repository) State(ctx context.Context) (RepoState, error) {
	var state RepoState
	head, err := r.ResolveHead(ctx)
	if err != nil {
		return state, err
	}
	state.Detached, state.Unborn = head.Detached, head.Unborn
	state.Branch = head.Branch

	rebaseDir, hasRebase := r.resolveGitPath(ctx, "rebase-merge")
	if !hasRebase {
		rebaseDir, hasRebase = r.resolveGitPath(ctx, "rebase-apply")
	}
	_, hasCherry := r.resolveGitPath(ctx, "CHERRY_PICK_HEAD")
	_, hasRevert := r.resolveGitPath(ctx, "REVERT_HEAD")
	_, hasMerge := r.resolveGitPath(ctx, "MERGE_HEAD")
	_, hasBisect := r.resolveGitPath(ctx, "BISECT_LOG")

	switch {
	case hasRebase:
		state.Operation = OpRebase
		state.Step, state.Total = readProgress(filepath.Join(rebaseDir, "msgnum"), filepath.Join(rebaseDir, "end"))
		if state.Step == 0 {
			state.Step, state.Total = readProgress(filepath.Join(rebaseDir, "next"), filepath.Join(rebaseDir, "last"))
		}
		if name := readTrimmed(filepath.Join(rebaseDir, "head-name")); name != "" {
			state.Branch = strings.TrimPrefix(name, "refs/heads/")
		}
		onto := readTrimmed(filepath.Join(rebaseDir, "onto"))
		state.Detail = rebaseDetail(state.Branch, onto)
	case hasCherry:
		state.Operation = OpCherryPick
		state.Total = r.sequenceRemaining(ctx, "pick")
		state.Detail = "a cherry-pick is paused waiting for conflict resolution"
		if subject := r.revSubject(ctx, "CHERRY_PICK_HEAD"); subject != "" {
			state.Detail = "picking " + subject
		}
	case hasRevert:
		state.Operation = OpRevert
		state.Total = r.sequenceRemaining(ctx, "revert")
		state.Detail = "a revert is paused waiting for conflict resolution"
		if subject := r.revSubject(ctx, "REVERT_HEAD"); subject != "" {
			state.Detail = "reverting " + subject
		}
	case hasMerge:
		state.Operation = OpMerge
		if subject := r.revSubject(ctx, "MERGE_HEAD"); subject != "" {
			state.Detail = "merging " + subject
		} else {
			state.Detail = "a merge is paused waiting for conflict resolution"
		}
	case hasBisect:
		state.Operation = OpBisect
		state.Detail = "bisect in progress"
	}
	return state, nil
}

func rebaseDetail(branch, onto string) string {
	ontoShort := onto
	if len(ontoShort) > 8 {
		ontoShort = ontoShort[:8]
	}
	switch {
	case branch != "" && ontoShort != "":
		return fmt.Sprintf("rebasing %s onto %s", branch, ontoShort)
	case branch != "":
		return "rebasing " + branch
	default:
		return "a rebase is paused waiting for conflict resolution"
	}
}

// readProgress reads two integer state files, returning zeros when either is
// missing or malformed. Git writes them as plain decimal lines.
func readProgress(currentPath, totalPath string) (step, total int) {
	step, _ = strconv.Atoi(readTrimmed(currentPath))
	total, _ = strconv.Atoi(readTrimmed(totalPath))
	return step, total
}

func readTrimmed(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// sequenceRemaining counts the pick/revert instructions Git still has queued.
// The sequencer rewrites its todo file as it goes, so this is what is left, not
// the total the sequence began with.
func (r Repository) sequenceRemaining(ctx context.Context, verb string) int {
	dir, ok := r.resolveGitPath(ctx, "sequencer")
	if !ok {
		return 0
	}
	data, err := os.ReadFile(filepath.Join(dir, "todo"))
	if err != nil {
		return 0
	}
	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, verb) {
			n++
		}
	}
	return n
}

// revSubject reads the subject line of a pseudo-ref such as MERGE_HEAD.
func (r Repository) revSubject(ctx context.Context, rev string) string {
	res, err := run(ctx, r.Root, "log", "-1", "--format=%s", rev, "--")
	if err != nil || res.Truncated {
		return ""
	}
	return strings.TrimSpace(res.Stdout)
}

// resolveGitPath asks Git where a state file lives, then reports whether it
// exists. Git owns the layout, so worktrees and unusual Git directories work.
func (r Repository) resolveGitPath(ctx context.Context, name string) (string, bool) {
	res, err := run(ctx, r.Root, "rev-parse", "--git-path", name)
	if err != nil {
		return "", false
	}
	path := strings.TrimSuffix(res.Stdout, "\n")
	if path == "" {
		return "", false
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(r.Root, path)
	}
	_, statErr := os.Stat(path)
	return path, statErr == nil
}

// ConflictKind classifies an unmerged index entry from the porcelain XY code,
// which is structured Git data rather than a scan of the file's text.
type ConflictKind int

const (
	ConflictBothModified ConflictKind = iota
	ConflictBothAdded
	ConflictDeletedByUs
	ConflictDeletedByThem
	ConflictAddedByUs
	ConflictAddedByThem
	ConflictBothDeleted
	ConflictOther
)

// Label is the human phrase for a conflict kind.
func (k ConflictKind) Label() string {
	switch k {
	case ConflictBothModified:
		return "both modified"
	case ConflictBothAdded:
		return "both added"
	case ConflictDeletedByUs:
		return "deleted by us"
	case ConflictDeletedByThem:
		return "deleted by them"
	case ConflictAddedByUs:
		return "added by us"
	case ConflictAddedByThem:
		return "added by them"
	case ConflictBothDeleted:
		return "both deleted"
	default:
		return "other conflict"
	}
}

// Group is the heading conflicts are listed under.
func (k ConflictKind) Group() string { return strings.ToUpper(k.Label()) }

// ConflictKindFor maps porcelain v2's unmerged XY code to a kind.
func ConflictKindFor(xy string) ConflictKind {
	switch xy {
	case "UU":
		return ConflictBothModified
	case "AA":
		return ConflictBothAdded
	case "DU":
		return ConflictDeletedByUs
	case "UD":
		return ConflictDeletedByThem
	case "AU":
		return ConflictAddedByUs
	case "UA":
		return ConflictAddedByThem
	case "DD":
		return ConflictBothDeleted
	default:
		return ConflictOther
	}
}

// Conflict is one unmerged path with its structured kind.
type Conflict struct {
	File File
	Kind ConflictKind
}

// Conflicts returns the repository's unmerged paths classified by Git's own
// status output. It is separate from Status so a caller can ask for the
// conflict set without building the whole working-tree view.
func (r Repository) Conflicts(ctx context.Context) ([]Conflict, error) {
	status, err := r.RepositoryStatus(ctx)
	if err != nil {
		return nil, err
	}
	var out []Conflict
	for _, f := range status.Groups[Conflicted] {
		out = append(out, Conflict{File: f, Kind: ConflictKindFor(f.XY)})
	}
	return out, nil
}
