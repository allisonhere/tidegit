# Milestone 4 validation

Baseline inspection ran the existing suite, launched the Milestone 3 executable
against a repository and confirmed staging, committing and the hook-failure path
still worked before anything was changed. The TideUI source was re-read to find
what already exists — `Renderer`, `Layout` with five modes, `Pane`, `Row`,
`Block`, `SoftPanel`, `SoftRow`, `PaneScroller`, `PaneRatio`, the `Styles` set
and the built-in themes. TideUI has no graph, tree or badge component, so those
were written here; nothing it already provides was reimplemented.

## Automated coverage

All tests build temporary repositories with isolated Git configuration and
repository-local identity, and never touch the developer's own repositories.
Remote-tracking refs are created directly with `update-ref` against a configured
but unreachable remote, so nothing contacts a network.

Git layer: linear history and commit metadata; merges with first-parent order;
diverged branches visible only when every ref is walked; lightweight and
annotated tags landing on the right commit, with annotated tags dereferenced to
their commit; remote-tracking refs; pagination across page boundaries with no
duplicates or reordering; literal, case-insensitive subject search; unborn HEAD;
subjects containing quotes, tabs and non-ASCII text surviving intact; changed-file
parsing for modify, add, delete and rename; root and merge commits; commit diffs
carrying their commit and refusing to offer hunk staging; HEAD resolution;
`ResolveRef` for names, tags and short hashes; detached HEAD including branch
creation from it; and graph topology — node, merge, root and tip marking, lane
bounds and a linear history never leaving lane zero.

Branch layer: local and remote listing with current-branch, upstream, ahead,
behind, merged state and the local branches tracking a remote; a diverged branch
reporting both counts; a deleted upstream reported as gone; divergence and merge
base including unrelated histories; switching; a switch Git refuses because of
local modifications, with HEAD and the working tree left alone; creation from
HEAD, a commit and a tag, with and without checkout; invalid names refused
without creating anything; renaming including the current branch; safe deletion
of a merged branch leaving other branches untouched; a safe delete refused for
an unmerged branch and typed as `ErrUnmergedBranch`; forced deletion only when
explicitly asked; and refusal to delete the checked-out branch.

UI layer: the History screen rendering graph, badges and metadata; the inspector
showing the full hash, identity, parents, refs and changed files; opening one
file's patch labelled as a historical commit and escaping back; a merge
presented as a merge; filtering by ref and by search; incremental paging keeping
the selection and the graph consistent; the inspection cache avoiding reloads;
detached HEAD announced in the header with branch creation still working; the
Branches screen listing, inspecting, switching, creating, renaming and deleting;
the refused switch surfaced on screen with the working tree intact; the two-step
unmerged deletion; refusal to switch to, rename or delete a remote branch; the
command palette including screen-specific commands and jump-to-ref; screen
switching preserving state; contextual help per screen; graph glyphs against
topology; the ASCII fallback for plain themes; badge ordering, degradation and
text sigils; and rendering at 160, 132, 100, 80, 60, 54, 40 and 1 column in
Catppuccin Mocha, Catppuccin Latte and VT52.

There are 53 new top-level tests, 110 in total.

## Defects found and fixed during this milestone

The visual review and the tests found real problems, each fixed and then
covered:

- Commit rows were padded to the full width and then had the right-hand
  metadata appended, so every row wrapped onto a second line.
- `CommitDetail` appended a field separator to an already complete record and
  failed to parse every commit.
- Refs were allocated space last despite outranking the hash and author, so
  badges vanished entirely on rows that had them; a badge too wide for the room
  was dropped rather than shortened.
- Hash and age columns were decided per row, so they moved from row to row.
- `wrapText` clipped tokens with no spaces, truncating the full commit hash it
  was supposed to wrap.
- The two branch-detail loads ran concurrently under one context, so whichever
  finished first cancelled the other and the inspector's comparison never
  arrived.
- Three-column mode gave the inspector a height of one line.
- Returning to a loaded screen reloaded it and discarded the selection.
- The "beginning of history" marker was drawn whenever the history was fully
  loaded, even with commits still below the fold.
- Graph lanes on the selected row were drawn in the dimmed colour, which is
  close to the selection background: selection hid the topology.
- A background worker wrote the pending force-delete target straight onto the
  model; it now travels in the message.

## Manual visual review

A fixture repository was built with linear history, two merged feature branches,
an abandoned experiment branch, a branch that was never merged, lightweight and
annotated tags, three remote-tracking refs with the current branch behind one of
them, two authors, a very long subject, a multi-paragraph commit message and
uncommitted working-tree changes.

The built executable was driven through a pseudo-terminal and every screen was
read: History at 132x36, the inspector, a commit's file patch, the Branches
screen, the delete confirmation for an unmerged branch, and the detached-HEAD
header. The graph was checked against `git log --graph --oneline --all` on the
same repository and matches its topology commit for commit. The rendered
colours were inspected directly from the captured escape sequences to confirm
the palette is restrained: quiet dimmed lanes, one accent for nodes, a second
for HEAD, and three badge fills — accent for HEAD and local branches, neutral
grey for remotes, and the theme's highlight colour for tags.

Both destructive paths were exercised against a disposable repository with the
real binary. Deleting an unmerged branch showed the warning, then Git's refusal
as a separate panel with different wording and a different key, and declining
left the branch in place. Pressing delete on a remote branch was refused with an
explanation and deleted nothing.

Rendering was reviewed in Catppuccin Mocha, Catppuccin Latte and VT52, and at
132, 80 and 60 columns. The medium-width tier was added during review: three
columns at 80 were too cramped, and TideUI's `StackedRight` keeps the ref
sidebar while stacking the commit list above the inspector.

## Final checks

`gofmt`, `go vet ./...`, `go test -race ./...`, `git diff --check`, an
executable build, and a live run of that executable against a fixture
repository.

No commits were made and nothing was staged in the TideGit source repository
during these checks; every Git mutation ran in a temporary or disposable
directory.
