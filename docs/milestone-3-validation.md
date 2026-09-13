# Milestone 3 validation

Baseline inspection covered the Git runner, staging service, application state,
TideUI shell and the Milestone 2 diff/action layers, together with Ripple v0.3.0's
editor API and TideMail's adapter. The existing suite passed before any commit
work, and the Milestone 2 executable opened a repository and staged a hunk.

Automated tests use temporary repositories with isolated Git configuration and
repository-local identity. Commit coverage includes message fidelity through
stdin (quotes, `$HOME`, backticks and non-ASCII text reach Git unchanged and are
never shell-interpreted), `pre-commit`/`prepare-commit-msg`/`commit-msg`/`post-commit`
running in order, each hook rejection leaving HEAD, the index and the working tree
untouched with Git's own output preserved, amend with and without staged changes,
message-only amend making no extra commit, unborn HEAD review and first commit,
amend refused on unborn HEAD, conflicted trees refused, merge commits with an
unchanged tree permitted, `commit.template` loaded verbatim, configured
`core.commentChar` honoured, `commit.cleanup` preserved when set and defaulted to
`strip` otherwise, sign-off, signing configuration remaining in force, and both
stale-index and stale-HEAD refusals.

UI coverage includes the whole compose-to-status workflow, the Ripple buffer
surviving a hook failure and a successful retry, undo/redo routing, cancel
confirmation and discard, amend loading HEAD's message, sign-off toggling without
staging unrelated working changes, refusal to open with nothing staged, staged
review navigation with a following preview, the F6 Git output panel, refresh
preserving the draft while picking up external staging, rendering in three themes
from 140x40 down to 1x1, and Ctrl-C leaving while a commit runs but not while
editing. There are 22 new top-level tests, 57 in total.

Interactive checks drove the built executable through a pseudo-terminal in a
disposable repository. A staged modification was composed and committed: the
status screen returned with `Committed <oid> <subject>`, the untracked file beside
it was untouched, and Git confirmed the recorded tree. A rejecting `commit-msg`
hook was then installed: the commit screen showed the activity line, then the hook
message with the draft intact; F6 displayed Git's stderr; no commit was created
and the file stayed staged. Amend with Ctrl-O replaced HEAD in place, folding in
the newly staged change and appending `Signed-off-by`, without adding a commit.
Rendering was reviewed from actual `View` output captured as text, ANSI and SVG
in both light and dark themes, at full width, narrow width, and with the hook
error and Git output panels open.

Final checks passed: `gofmt`, `go vet ./...`, `go test -race ./...`,
`git diff --check`, and an executable build.

No commits were made and nothing was staged in the TideGit source repository
during these checks; all Git mutations ran in temporary directories.
