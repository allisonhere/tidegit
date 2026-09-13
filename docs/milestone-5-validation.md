# Milestone 5 validation

Baseline inspection ran the existing suite first (`go test ./...`), built the
Milestone 4 executable and launched it against a repository, and re-read the
TideUI source for what already exists. TideUI has no progress, operation-detail
or option-list component, so the operation panel and the choice dialog were
written here on top of `SoftPanelOverlay`/`RenderSoftRow`/`RenderSoftHints`;
nothing it already provides was reimplemented. The existing per-repository
mutation semaphore, the background-command lifecycle, the diff renderer and the
`CommandError` shape were extended rather than duplicated.

## Architecture notes

- The subprocess layer gained one streaming runner (`runStream`). Ordinary
  commands are unchanged.
- `--literal-pathspecs` is skipped only for `stash push` (see defects).
- Remote and stash operations reuse the mutation semaphore in `internal/git`,
  so they serialize with staging, branch work and commits.
- The operation panel is generic and reusable; no screen owns it.
- Stash patches flow through the existing `Diff`/`diffLines`/`renderDiff`
  pipeline.

## Automated coverage

All fixtures are temporary repositories with isolated Git configuration,
repository-local identity, local bare remotes and second clones. Nothing
contacts a network. Remote-tracking state is created and diverged entirely
between local bare repositories.

Git layer — remotes: a single remote's name, fetch URL, push URL, default flag
and tracking branches; a pushurl differing from the fetch URL; branch-remote
resolution; fetching updating `refs/remotes` while leaving the local branch
untouched and recalculating ahead/behind; fast-forward and already-up-to-date
pulls; push and first-push upstream setup with the remote actually receiving the
commit; a non-fast-forward push classified as such with the local branch
unchanged; an unreachable host classified without being mistaken for
authentication; an ambiguous two-remote repository with no default; a pull
conflict classified and left in the working tree; and a dirty working tree
refused with the local edit preserved.

Git layer — stashes: listing with selector, branch, message, author and time;
Git's default WIP subject parsed as well as a named one; include-untracked
capture with both files listed and a patch returned; apply keeping the entry;
pop removing it only on success; drop removing only the selected entry; and a
conflicted apply typed as `StashConflictError` with the conflict visible in
status and the stash preserved.

UI layer: the Remotes screen rendering URLs, tracking branches and last fetch;
fetch by key and by explicit remote; fetch-all; a fast-forward pull; first-push
upstream setup; a rejected push opening the operation panel with a
plain-language summary and the raw Git output reachable behind `e`; a pull with
no upstream offering a deliberate remote choice instead of guessing; the Stash
screen listing, inspecting and showing a labelled patch; apply, pop, create with
message and include-untracked; drop requiring confirmation, naming the entry,
and removing only the right one after cancel then confirm; a conflicted apply
surfaced with the stash kept; `p` popping rather than pulling on the Stash
screen; palette commands including and excluding the right contextual entries;
help for the new screens; the running operation panel showing a progress line;
and both new screens rendering within bounds at 160, 132, 100, 80, 60, 54, 40
and 1 column. Background-continuity tests now also cover the Stash screen, the
Remotes screen, the choice dialog and the operation panel.

There are 29 new top-level tests; the suite is 177 top-level tests.

## Defects found and fixed during this milestone

Each was found by a test or a rendered-view review and then fixed:

- `--literal-pathspecs` disabled `git stash push -u`'s pathspec magic, so the
  untracked file was reported as stashed but remained in the working tree. A
  no-literal runner is now used for stash creation only.
- The post-operation working-tree refresh cleared the notice bar and discarded
  the success summary the operation had just produced. The refresh now restores
  it.
- Pressing `o` while an operation was running cancelled it instead of hiding
  the panel.
- The remote cache was written from a background goroutine. It is now assigned
  in the result handler, on the model goroutine.
- Fetching from the Remotes screen refreshed the remote list but not the
  working-tree snapshot, so the header and ahead/behind looked stale.
- `git stash show -p <ref> -- <path>` does not accept a pathspec and errored, so
  a single file's stash patch is now taken by splitting the full patch.
- The missing-upstream choice clipped its explanation instead of wrapping it.
- A stash created from Status had no stash state loaded, so the list was empty
  until the screen was reopened.

## Manual verification

The executable was built and launched against a fixture repository under a
pseudo-terminal; it emitted the expected terminal setup and stayed responsive,
and the fixture was left untouched.

The interactive matrix is otherwise exercised through the model and rendering
harness rather than a scripted terminal session: one remote, several remotes, no
remote, a branch with and without an upstream, ahead, behind and diverged
branches, fetch, fast-forward pull, rejected push, push, authentication/network
failure classification, create/named/include-untracked stash, inspect, apply,
pop, drop, a stash-application conflict, and narrow and wide terminals in
Catppuccin Mocha, Catppuccin Latte and VT52. Every rendered view was checked to
stay within its requested width and height and to keep a continuous theme
background.

## Final checks

`gofmt`, `go vet ./...`, `go test -race ./...`, an executable build, and a
launch of that executable under a pseudo-terminal.

No commits were made and nothing was staged in the TideGit source repository
during these checks; every Git mutation ran in a temporary or disposable
directory.
