# Milestone 8 validation

Baseline inspection ran the full Milestone 7 suite, built and launched the
executable, and mapped every place the diff renderer was reused: the Status
preview, the commit staged review, the History commit diff, the Stash diff, the
Reflog commit diff, and the conflict stage comparisons. The parsed model and the
viewer are new packages; the Git layer and the staging service were extended,
not duplicated.

## Automated coverage

The `internal/diff` parser is tested for additions, deletions, context, multiple
hunks, hunk section headings, old/new line numbers, no-newline markers, new and
deleted files, renames with similarity, copies, quoted paths containing spaces,
Unicode content and binary notices. Side-by-side alignment is tested for
replacement, pure insertion and pure deletion. Word-level spans are tested for
one changed word, an inserted word, a deleted word, punctuation and Unicode.
Context collapse is tested for collapsing a long run, expanding it, and leaving
a short run alone. Highlighting is tested for keyword, string, number and
comment tokens, empty input and extension detection.

The UI viewer is tested for loading a two-hunk diff, navigating hunks and
keeping the model and viewer in step, split rendering with a separator,
searching and stepping matches, collapsing and expanding a gap, cycling the
whitespace mode, toggling syntax and whitespace settings, clamping context,
staging the selected hunk through the shared viewer, and a 30,000-line diff that
stays windowed with the degradation notice. Git tests verify that the
whitespace modes reach Git as its own flags and that a content change still
appears under `ignore-all`.

The suite is 266 top-level tests.

## Defects found and fixed during this milestone

- The LCS word diff returned one mask and applied it to both lines, so a longer
  new line indexed past the mask and panicked; it now marks each side
  separately.
- Side-by-side treated any non-deletion line as context, so a standalone
  insertion appeared on both sides; additions now render on the right only.
- The help guide grew past a 30-row terminal with the new Diff section, so it
  fell back to the compact form and a test caught the missing full table; the
  trailing blank is trimmed before measuring and the section was trimmed.
- The viewer's selected hunk lost the textual `>` marker when it gained a
  background; the marker is back so selection is not colour-only.

## Manual verification

The executable builds and launches under a pseudo-terminal. Rendered unified and
split views of a real two-hunk Go diff were inspected: the gutters align, the
selected hunk header is marked, `+`/`-` markers are present, the split columns
line up, and syntax and word emphasis sit under the change semantics.

The interactive matrix is exercised through the model and rendering harness:
small unified and split diffs, a large diff, multi-hunk files, staged and
unstaged diffs, a file with both, commit diffs, stash diffs, renames, deleted
and new files, whitespace-only changes, long lines, syntax-highlighted source,
plain text, binary files, search, hunk navigation, context collapse and expand,
hunk staging, and narrow, wide and very wide terminals in light and dark themes.
Views stay within their dimensions and keep a continuous theme background.

## Final checks

`gofmt`, `go vet ./...`, `go test -race ./...`, an executable build, and a
launch under a pseudo-terminal.

No commits were made and nothing was staged in the TideGit source repository
during these checks; every Git mutation ran in a temporary or disposable
directory.
