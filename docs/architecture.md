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
- `internal/ui/model.go`: focus, selection, filter, refresh generation and
  background command lifecycle.
- `internal/ui/view.go`: TideUI shell and context rendering.
- `internal/ui/diff.go`: terminal-safe diff presentation, line numbers and colors.

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

The Git `Diff` result keeps the original patch, separate from presentation.
Partial staging should introduce a parsed patch/hunk model here and apply
Git-compatible patches through Git, never by editing working files. Truncated
previews must never be used for patch application. Split view can consume the
same patch model later. The current viewer recognizes patch syntax and Git's
function headers; source-language token highlighting is not included.

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
dependency or placeholder editor is added to this read-only milestone.

## Remaining concerns

TideUI does not export pane geometry; the host currently mirrors the 2:3:5
column calculation for row widths. Shared geometry would help later mouse
hit testing and resizing. Narrow layouts use tabbed rendering.

Output limits bound retained bytes but do not prevent Git's own resource usage.
Very large status lists fail visibly rather than silently dropping paths. Later
performance work should profile scan latency, add debounced watching and assess
incremental status strategies without compromising Git semantics.

The workspace initially contained no application files and no usable Git
metadata (only a restricted `.git` placeholder). No Git repository was created
and no commit was made as part of implementation.
