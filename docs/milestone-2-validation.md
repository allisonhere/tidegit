# Milestone 2 validation

Baseline inspection included the Git runner/status parser, application state,
TideUI shell/list/scroll/help components, diff renderer and architecture notes.
The existing suite passed and the Milestone 1 executable opened a clean repository.

Automated tests use temporary repositories with isolated Git configuration and
local identity. Coverage includes whole-file add/modify/delete staging, unrelated
file isolation, unborn unstaging with different index/working bytes, two-hunk
stage/unstage, mixed-state diffs, shifted line numbers, missing trailing newline,
empty-file ranges, quoted/non-ASCII paths, staged renames, copied paths, permission
metadata, binary files, stale/malformed requests, Git index lock errors, serialized
concurrent mutations, output limits and UI action/refresh/selection behavior.

Interactive terminal checks used disposable repositories. The two-hunk workflow
selected the second hunk, staged it, inspected both source views, staged the
remaining hunk, unstaged one hunk, then unstaged/staged the complete file. Git
diffs independently confirmed the split. Additional checks exercised untracked
and deleted files, whole-file rename unstaging and selection following. Renames
unstaged into deletion plus untracked paths, matching Git's native status.

An unborn repository was tested by staging a new file, editing it again, and
unstaging it: the index entry disappeared and the newer working content remained.
Help showed the file/hunk bindings. A live terminal resize from 120x30 to 60x12
and back switched to TideUI's tabbed layout and restored the three-pane layout.
The harness explicitly delivered SIGWINCH after changing the pseudo-terminal
size; the UI retained its selected file. Automated rendering checks also cover
very small sizes down to 1x1.

Final checks passed: `gofmt`, `go vet ./...`, `go test -race ./...`,
`git diff --check`, and an executable build. There are 19 new top-level tests,
plus their edge-case subtests, across the Git service and UI action layer.

No changes were staged in the TideGit source repository during these checks.
