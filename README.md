# TideGit

A calm, keyboard-first terminal Git client built with TideUI.
**Milestone 1: read-only repository inspection.**

Requires Git with porcelain v2 support and Go 1.26.1 or newer.

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
No staging or other repository mutations are exposed.

| Key | Action |
| --- | --- |
| Tab / Shift-Tab | Next / previous pane |
| j / k, Up / Down | Navigate sections/files or scroll the diff |
| h / l, Left / Right | Scroll the diff horizontally |
| PageUp / PageDown, Home / End | Page or jump within the focused pane |
| Enter | Focus next pane |
| / | Filter the current file section; Enter keeps, Escape clears |
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
fixtures, and never stage or commit in your own repositories. They cover status
transitions, special filenames, discovery failures, detached/unborn HEAD,
ahead/behind state, conflicts, binary and state-specific diffs, cancellation,
output bounds, stale UI responses, filtering, line numbers and terminal sizing.

## Scope and limits

Staging, Ripple commit editing, log, branches, remote operations, stash,
command palette, repository picker, persisted configuration, watching and
external-tool actions are not implemented. Milestone 2 has not started.

Status and diff commands run asynchronously with a 30-second deadline. Captured
output is bounded to 4 MiB per stream. Oversized status results fail explicitly;
diffs show a limited preview (also capped at 20,000 displayed lines). Scrollable
diff data is prepared once in the background. Git itself may still use substantial
resources calculating very large diffs before cancellation. Preview limits are
not a substitute for future large-repository profiling.

See [architecture](docs/architecture.md) for API reuse, decisions and future seams.
