package git

import (
	"context"
	"fmt"
	"strings"
)

// RefKind separates the ref namespaces the UI badges differently.
type RefKind int

const (
	RefLocal RefKind = iota
	RefRemote
	RefTag
	RefHead // HEAD itself, only ever produced as a decoration
)

// Ref is one name pointing at a commit. Name is the short form Git prints;
// Full is the complete refname where one is known.
type Ref struct {
	Kind       RefKind
	Name, Full string
	OID        string
	// Head marks the ref HEAD currently points at, so the UI can render one
	// badge rather than repeating "HEAD" beside the branch name.
	Head bool
}

// parseDecorations reads Git's %D field, for example
// "HEAD -> main, origin/main, tag: v1.2.0".
func parseDecorations(field string) []Ref {
	field = strings.TrimSpace(field)
	if field == "" {
		return nil
	}
	var refs []Ref
	for _, part := range strings.Split(field, ", ") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		switch {
		case part == "HEAD":
			// Detached HEAD decorates the commit on its own.
			refs = append(refs, Ref{Kind: RefHead, Name: "HEAD", Head: true})
		case strings.HasPrefix(part, "HEAD -> "):
			name := strings.TrimPrefix(part, "HEAD -> ")
			refs = append(refs, Ref{Kind: RefLocal, Name: name, Full: "refs/heads/" + name, Head: true})
		case strings.HasPrefix(part, "tag: "):
			name := strings.TrimPrefix(part, "tag: ")
			refs = append(refs, Ref{Kind: RefTag, Name: name, Full: "refs/tags/" + name})
		case strings.Contains(part, "/"):
			refs = append(refs, Ref{Kind: RefRemote, Name: part, Full: "refs/remotes/" + part})
		default:
			refs = append(refs, Ref{Kind: RefLocal, Name: part, Full: "refs/heads/" + part})
		}
	}
	return refs
}

// refFormat and its field order must stay in step with Refs.
const refFormat = "%(refname)" + fieldSep + "%(refname:short)" + fieldSep +
	"%(objectname)" + fieldSep + "%(HEAD)" + fieldSep + "%(objecttype)" + fieldSep +
	"%(*objectname)" + recordSep

// Refs lists local branches, remote-tracking branches and tags. An annotated
// tag reports the commit it points at, not the tag object, so history can match
// it against a commit.
func (r Repository) Refs(ctx context.Context) ([]Ref, error) {
	res, err := run(ctx, r.Root, "for-each-ref", "--format="+refFormat,
		"refs/heads", "refs/remotes", "refs/tags")
	if err != nil {
		return nil, err
	}
	if res.Truncated {
		return nil, fmt.Errorf("reference list exceeds 4 MiB")
	}
	var refs []Ref
	for _, record := range strings.Split(res.Stdout, recordSep) {
		record = strings.TrimLeft(record, "\n")
		if strings.TrimSpace(record) == "" {
			continue
		}
		f := strings.Split(record, fieldSep)
		if len(f) != 6 {
			return nil, fmt.Errorf("malformed ref record with %d fields", len(f))
		}
		ref := Ref{Full: f[0], Name: f[1], OID: f[2], Head: f[3] == "*"}
		// An annotated tag's own object id is not a commit; %(*objectname)
		// dereferences it to one.
		if f[4] == "tag" && f[5] != "" {
			ref.OID = f[5]
		}
		switch {
		case strings.HasPrefix(ref.Full, "refs/heads/"):
			ref.Kind = RefLocal
		case strings.HasPrefix(ref.Full, "refs/remotes/"):
			ref.Kind = RefRemote
		case strings.HasPrefix(ref.Full, "refs/tags/"):
			ref.Kind = RefTag
		}
		// A remote's symbolic HEAD (origin/HEAD) is an alias, not a branch.
		if ref.Kind == RefRemote && strings.HasSuffix(ref.Name, "/HEAD") {
			continue
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

// Head describes what HEAD currently points at.
type Head struct {
	Branch   string // empty when detached
	OID      string
	Short    string
	Detached bool
	Unborn   bool // a branch exists but has no commit yet
}

// ResolveHead reports the current branch or detached commit.
func (r Repository) ResolveHead(ctx context.Context) (Head, error) {
	var h Head
	res, err := run(ctx, r.Root, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err == nil {
		h.Branch = strings.TrimSuffix(res.Stdout, "\n")
	} else {
		var ce *CommandError
		// Exit code 1 from symbolic-ref means HEAD is not a symbolic ref, which
		// is exactly a detached HEAD rather than a failure.
		if !asCommandError(err, &ce) || ce.Result.ExitCode != 1 {
			return h, err
		}
		h.Detached = true
	}
	oid, err := run(ctx, r.Root, "rev-parse", "--verify", "--quiet", "HEAD")
	if err != nil {
		h.Unborn = true
		return h, nil
	}
	h.OID = strings.TrimSuffix(oid.Stdout, "\n")
	if len(h.OID) >= 7 {
		h.Short = h.OID[:7]
	}
	return h, nil
}

// ResolveRef turns a user-supplied name, hash prefix or commit-ish into a full
// commit id. Git validates it; TideGit only refuses input shaped like a flag.
func (r Repository) ResolveRef(ctx context.Context, rev string) (string, error) {
	if err := validRevision(rev); err != nil {
		return "", err
	}
	res, err := run(ctx, r.Root, "rev-parse", "--verify", "--quiet", rev+"^{commit}")
	if err != nil || strings.TrimSpace(res.Stdout) == "" {
		return "", fmt.Errorf("no commit matches %q", rev)
	}
	return strings.TrimSuffix(res.Stdout, "\n"), nil
}
