# Milestone 6 validation

Baseline inspection ran the full Milestone 5 suite, built and launched the
executable against a repository, and re-read the state, status, diff, async
mutation, dialog and palette code before changing anything. The existing
per-repository mutation semaphore, background-command lifecycle, `CommandError`,
diff renderer and commit inspector were extended rather than duplicated.
Conflict parsing, operation detection and reflog reading live in `internal/git`;
no repository state is inferred from the UI's memory.

## Automated coverage

Every fixture is a temporary repository with isolated Git configuration and
repository-local identity. Conflicts are created between real branches; no test
touches the developer's repositories or the network.

Git layer — operation state: a merge is detected from Git's state files, the
kind is reported, and an independent repository handle rediscovers the same
state. Conflict stages: the base, ours and theirs blobs are read back and their
contents checked. Region parsing: 1-based ranges are correct for diff3 and
two-way conflicts, and a lone separator line in ordinary content is not treated
as a conflict. Resolution: ours and theirs are written correctly and writing a
side does not resolve the index; keep-both concatenates the sides and removes
markers; mark-resolved clears the conflict and stages the result. Controls:
merge continue creates a merge commit and abort restores HEAD; rebase continue,
skip and abort each finish or restore; cherry-pick abort restores; revert
aborts, and a clean revert creates an inverse commit. Conflict kinds: both
modified and both added are classified from Git's codes. Reset: soft keeps the
index and working tree, mixed keeps the working tree but resets the index, hard
discards tracked changes. Reflog: selectors, actions, timestamps and object ids
parse. Restore: working-tree, index and from-HEAD variants each do exactly what
they say. A clean repository reports no operation and refuses continue/abort.

UI layer: the merge is detected on load and the ribbon appears; the Conflicts
screen groups the file, lists its one region, shows `OURS`/`THEIRS`/`BASE` with
a base stage, writes ours without resolving, marks resolved, shows the calm
success state, and continues to a merge commit. The theirs-and-resolve action
resolves; keep-both leaves the file unmerged; abort confirms with its own key
and restores HEAD; a second, independent model rediscovers the operation and
conflict on "restart". A rebase conflict adapts the side labels to
`UPSTREAM`/`YOUR COMMIT` and shows progress, and skip finishes it. The reflog
screen lists entries, and a recovery branch is created at the selected entry's
exact object id. Undo-last-commit offers only the two safe shapes and the soft
path leaves changes staged; hard reset confirms as destructive and explains
what it discards. Revert from History creates the inverse commit. Palette
commands appear and conflict-only commands are hidden off the Conflicts screen.
Both new screens render within bounds at ten sizes.

The suite is 203 top-level tests.

## Defects found and fixed during this milestone

- The conflict list and conflict detail shared one generation counter, so the
  detail load invalidated the list result and a resolved file stayed on screen.
  They now use separate counters.
- Conflict-region line numbers mixed 0-based and 1-based indices; the parser now
  stores 1-based ranges and a test pins them.
- The region inspector dropped its mode label when rendering the default view.
- The reflog selector and inspector columns were one cell too narrow, so
  `HEAD@{10}` ran into the hash and labels ran into their values.

## Manual verification

The executable builds and launches under a pseudo-terminal against a fixture
repository, emitting the expected terminal setup and staying responsive.

The interactive matrix is exercised through the model and rendering harness:
simple and multi-file merge conflicts, multiple regions in one file, add/add,
modify/delete, rebase, cherry-pick and revert conflicts; accept ours, accept
theirs, keep both, mark resolved, continue, skip and abort; restart during a
conflict; reflog browsing and recovery-branch creation; soft, mixed and hard
reset; revert; undo-last-commit keeping changes staged and unstaged; and the
Conflicts and Reflog screens at narrow and wide sizes in three themes. Rendered
views are checked to stay within their requested dimensions and to keep a
continuous theme background.

## Final checks

`gofmt`, `go vet ./...`, `go test -race ./...`, an executable build, and a
launch under a pseudo-terminal.

No commits were made and nothing was staged in the TideGit source repository
during these checks; every Git mutation ran in a temporary or disposable
directory.
