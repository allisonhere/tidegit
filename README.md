# TideGit

A calm, keyboard-first terminal Git client built with TideUI.
**Milestone 2: repository inspection and file/hunk staging.**

Requires Git 2.23 or newer and Go 1.26.1 or newer.

```sh
go run ./cmd/tidegit /path/to/repository
go build -o bin/tidegit ./cmd/tidegit
./bin/tidegit
./bin/tidegit -theme nord /path/to/repository
```

Without a path, TideGit discovers the repository containing the current directory.
Outside a repository it shows the Git discovery error and instructions to launch
with a path; the recent-repository picker belongs to a later milestone.

The Status screen groups staged, unstaged, untracked and conflicted files. A file
with both index and working-tree changes appears in both appropriate groups.
The right pane previews the selected state, including unified diff line numbers,
Git's hunk/function headers, binary notices, renames, deletions and combined
conflict diffs. Conflict diffs intentionally retain Git's multi-parent columns.
Stage or unstage complete files or individual text hunks. Working-tree content
is preserved by all staging actions. A whole-file operation follows the selected
path into its destination group; a hunk operation stays with the remaining hunks
in the current group when possible.

| Key | Action |
| --- | --- |
| Tab / Shift-Tab | Next / previous pane |
| j / k, Up / Down | Navigate sections/files or scroll the diff |
| h / l, Left / Right | Scroll the diff horizontally |
| PageUp / PageDown, Home / End | Page or jump within the focused pane |
| Enter | Focus next pane |
| / | Filter the current file section; Enter keeps, Escape clears |
| s / u | Stage / unstage the selected **file** |
| S / U | Stage / unstage the selected **hunk** |
| [ / ] | Previous / next hunk; the active header is marked `>` |
| r / Ctrl-R | Refresh status and selected diff |
| z | Expand / restore focused pane |
| ? | Contextual help |
| q | Close help/expanded view, otherwise quit |
| Ctrl-C | Quit |

Small terminals use TideUI's tabbed layout. Themes come directly from TideUI;
`-theme` selects a built-in palette. Mouse input is not enabled in this milestone.

## Validation

```sh
gofmt -w cmd internal
go vet ./...
go test -race ./...
go build -o bin/tidegit ./cmd/tidegit
```

Tests create isolated temporary repositories, disable user Git configuration for
fixtures, configure repository-local identity, and never stage or commit in your
own repositories. They cover status
transitions, special filenames, discovery failures, detached/unborn HEAD,
ahead/behind state, conflicts, binary and state-specific diffs, cancellation,
output bounds, stale UI responses, filtering, line numbers and terminal sizing.
Staging tests additionally cover file isolation, unborn HEAD, partial stage and
unstage, mixed state, newline/line-shift/empty-file edge cases, rename and copy
handling, metadata preservation, stale/malformed patches, index lock errors,
concurrent mutations and the complete UI action/refresh/selection workflow.

## Scope and limits

Ripple commit editing, log, branches, remote operations, stash,
command palette, repository picker, persisted configuration, watching and
external-tool actions are not implemented. Milestone 3 has not started.

Hunk actions require a complete text preview. Binary files and metadata-only
changes use whole-file actions. Conflicted files and submodule staging are not
exposed by the UI. Line-level staging is deferred. A text hunk action does not
stage permission changes or undo a staged rename/copy; use whole-file actions
for that metadata. Git typically reports a rename performed outside the index
as a deleted path plus an untracked path: stage those paths individually to
record the rename, without implicitly staging another file.

Mutations run one at a time. While an operation and its status refresh run,
selection and additional mutations are held; focus, help, expansion and quit
remain available. Errors include operation/path context and Git stderr in the
scrollable right pane. Refresh with `r` after correcting an error.

Status and diff commands run asynchronously with a 30-second deadline. Captured
output is bounded to 4 MiB per stream. Oversized status results fail explicitly;
diffs show a limited preview (also capped at 20,000 displayed lines). Scrollable
diff data is prepared once in the background. Truncated previews cannot be used
for hunk staging. Git itself may still use substantial
resources calculating very large diffs before cancellation. Preview limits are
not a substitute for future large-repository profiling.

See [architecture](docs/architecture.md) for API reuse, decisions and future seams.
