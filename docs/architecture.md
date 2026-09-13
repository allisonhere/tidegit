# Architecture

## Inspected APIs

Before implementation, the local TideUI source, its README, and TideMail's Ripple
adapter were inspected, along with the cached Ripple v0.3.0 API documentation.
The build pins TideUI `v0.2.3-0.20260823151826-81535e78c84e`, which contains
the soft-panel API, without a machine-specific local replacement.

TideUI is a view-oriented toolkit. It does **not** own the application model,
focus routing, mouse hit testing or arbitrary list navigation. TideGit owns
that state and reuses `Renderer`, `Layout`, `ThreeColumn`, `Tabbed`, `Pane`,
`RenderRow`, `PaneScroller`, `StatusBar`, `SoftPanelOverlay`, shared `Styles`,
and built-in `Theme` palettes. No duplicate pane chrome or theme system exists.
No built-in Shift-Space binding was found; `z` provides discoverable expansion.

## Boundaries

- `cmd/tidegit`: launch flags, program lifecycle and cancellation.
- `internal/git`: repository discovery, porcelain parsing, typed status/diff
  operations, command execution and structured errors. No UI imports.
- `internal/git/patch.go`: hunk parsing, source metadata and patch generation.
- `internal/git/staging.go`: serialized file and index-only hunk operations.
- `internal/ui/model.go`: focus, selection, filter, refresh generation and
  background command lifecycle.
- `internal/ui/view.go`: TideUI shell and context rendering.
- `internal/ui/diff.go`: terminal-safe diff presentation, line numbers and colors.
- `internal/ui/actions.go`: typed actions, mutation lifecycle, selection following
  and hunk navigation; suitable for a future palette dispatcher.

Git runs through argument arrays, never a shell. Pathspecs are literal and
separated with `--`. Status uses `--porcelain=v2 --branch -z` and retains paths
verbatim, including rename source records. Display sanitization never changes
the paths sent back to Git. `GIT_OPTIONAL_LOCKS=0` avoids status index refresh
writes. User config is otherwise inherited. Preview commands disable external
diff and textconv programs to get predictable Git patches; Git attributes still
influence native diff behavior. stdout, stderr and exit status are captured.
Only no-index diff exit code 1 is accepted as a successful difference result.

Refresh cancels old scan/diff work. Selection cancels the preceding diff; message
generation IDs reject stale completions. The current repository snapshot and
prepared diff lines stay in memory until deliberately invalidated. Only visible
diff rows are styled during rendering. There is no multi-file diff cache yet.
Refresh retains selection by path when possible.

The Git `Diff` result keeps the original patch and typed hunks, separate from
presentation. Each `Hunk` includes file identity, source section, original file
headers, raw old/new header paths, hunk header/body, old/new ranges, a SHA-256
identity derived from original patch bytes, and a row hint used only for
navigation. Hunk identity never depends on displayed line numbers. Body lines
and ranges leave room for future reduced patches and split diff rendering.
The current viewer recognizes patch syntax and Git's function headers;
source-language token highlighting is not included.

## Staging mechanisms and safety

Whole-file staging uses `git add -- <literal path>`. Whole-file unstaging uses
`git restore --staged --source=HEAD -- <literal path>`. On unborn HEAD it uses
`git rm --cached -f -- <literal path>`: `--cached` is essential; the force flag
only allows removing the index entry when index and working file differ.
No command writes the working tree. Staged renames include both paths when
unstaging; copies include only the destination. An unstaged diff for a staged
rename includes only its destination, so a recreated old path stays unrelated.
Directory staging is rejected to prevent accidentally staging descendants.

Hunk operations pipe a generated patch directly into `git apply --cached
--whitespace=nowarn -`, adding `--reverse` for unstaging. Git performs the
atomic index update and patch validation. No interactive Git process, manual
index editing, temporary patch file or retry is used. Original no-newline
markers and hunk ranges are preserved. Existing-file patches contain content
headers only, so reversing text changes preserves staged rename/copy/mode
metadata; new/deleted files retain their required file headers.

Before applying, the service re-reads the source diff and matches the hunk's
identity and generated patch. Modified or stale requests fail visibly. This
revalidation does not lock out external tools; Git's index lock and patch
validation remain authoritative if another process changes the index.
Truncated previews, combined conflicts, submodules, binary and multi-file
patches do not expose hunk actions. The UI also disables hunk actions when its
20,000-line preview limit is exceeded.

Diffs explicitly set a/b prefixes, output indicators, three context lines,
zero inter-hunk context and non-relative output. These minimal changes were
necessary because user-configured preview prefixes/indicators are not a stable
patch transport format. Ordinary Git configuration, attributes and filters
still apply to whole-file staging; partial staging applies Git's native diff
content directly to the index.

A cancellable semaphore serializes mutations per canonical repository root.
Git's own `index.lock` coordinates with external processes. The UI keeps a
mutation gate closed through the ensuing status scan, cancels and invalidates
old read requests, and rejects repeated mutation/refresh keys while busy. It
rescans even after failures, then loads the relevant diff. Whole-file actions
prefer the destination group; hunk actions prefer remaining source hunks.
Focus and approximate hunk/scroll position survive refresh. Read-only scans
and diffs otherwise retain the existing background command architecture.

`Repository` currently records the discovered root. It can grow explicit Git
directory/common-directory identity for worktrees and submodules; there is no
assumption that `.git` is a directory. Bare repositories are not supported by
this working-tree screen. Operation-state detection (merge/rebase/cherry-pick)
and conflict continuation remain future work.

## Ripple integration planned for Milestone 3

A dedicated commit state will embed `ripple.Model`, initialized with `ripple.New`.
Load Git's commit template through the Git service, then use `SetValue`, `Focus`,
`SetSize`, `Update`, `Value` and `View(ripple.Options{...})`. Ripple owns cursor
movement, wrapping, selection, undo/redo and the message buffer. TideUI owns
the surrounding view and styles; do not rewrap Ripple's rendered output.
Ripple's clipboard interface can be adapted later. Route its `SubmitMsg` and
`CancelMsg` to host actions, preserving the buffer on Git failure. Comments can
use Ripple v0.3.0's `StyleKey`/`Style` hooks.

The future commit service must invoke normal `git commit`, preserve hook and
signing behavior, and leave cleanup/comment semantics to Git. No Ripple runtime
dependency or placeholder editor is added to this staging milestone.

## Remaining concerns

TideUI does not export pane geometry; the host currently mirrors the 2:3:5
column calculation for row widths. Shared geometry would help later mouse
hit testing and resizing. Narrow layouts use tabbed rendering.

Output limits bound retained bytes but do not prevent Git's own resource usage.
Very large status lists fail visibly rather than silently dropping paths. Later
performance work should profile scan latency, add debounced watching and assess
incremental status strategies without compromising Git semantics.

Before Milestone 3, extend the existing mutation lifecycle for hook/signing
processes and retain the Ripple buffer across failures. The current 30-second
staging deadline is unsuitable as a blanket deadline for interactive signing
or long-running commit hooks. There is no need to replace the shell or Git
service architecture. Worktree-aware index identity will be needed when
worktree support enters scope; the current semaphore keys ordinary roots.
