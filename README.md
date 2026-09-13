# TideGit

A calm, keyboard-first terminal Git client built with TideUI.
**Milestone 4: history, commit inspection and branch management.**

![The History screen: ref filters, the commit graph with each branch in its own
colour, and a commit's patch in the inspector](images/screen1.png)

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

TideGit has three screens: **Status** (`1`), **History** (`2`) and **Branches**
(`3`). `Ctrl-P` opens the command palette, `?` shows the bindings that act on
the screen you are looking at, and `q` steps back to Status before it quits.

Press `c` to compose a commit or `A` to amend HEAD. The commit screen pairs a
Ripple editor with a staged review: file list, additions/deletions and a staged
diff preview. Git owns the message. Templates, `core.commentChar`,
`commit.cleanup`, hooks, signing and sign-off all behave as they do on the
command line. TideGit records what you reviewed: if the index or HEAD changes
between review and submission, the commit is refused until you refresh with
`Ctrl-R`. A rejected commit keeps your draft, and `F6` shows Git's own output.

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
| c / A | Compose a commit / amend HEAD |
| 1 / 2 / 3 | Status / History / Branches |
| Ctrl-P | Command palette |
| t | Theme picker |
| r / Ctrl-R | Refresh status and selected diff |
| z | Expand / restore focused pane |
| ? | Contextual help |
| q | Close help/expanded view, otherwise quit |
| Ctrl-C | Quit |

### Commit screen

| Key | Action |
| --- | --- |
| Ctrl-S | Commit (runs hooks and signing) |
| Ctrl-O | Toggle `Signed-off-by` |
| Ctrl-R | Re-read the staged review, keeping your draft |
| Tab | Switch between the editor and the staged review |
| j / k | Choose a staged file to preview (review pane) |
| F6 | Show Git's output from the last failure |
| F1 | Commit and editor guide |
| Esc / Ctrl-Q | Return to Status; an edited draft asks first (Ctrl-D discards) |

Ripple owns editing: arrows move, Shift-arrows select, Ctrl-Z / Ctrl-Y undo and
redo, Ctrl-C / Ctrl-X / Ctrl-V copy, cut and paste through the system clipboard
(`pbcopy`, `wl-copy` or `xclip`). Because Ctrl-C is copy here, it does not quit;
leave with Esc first. The one exception is while Git is running: a commit has no
deadline, so Ctrl-C during hooks or signing leaves TideGit rather than trapping
you behind an unattended prompt.

Small terminals use TideUI's tabbed layout. Mouse input is not enabled.

## Themes

Press `t` on any screen for the theme picker: moving the cursor previews the
whole app in that palette, `Enter` keeps it, `Esc` puts back the one you had.
`-theme NAME` picks one at launch, and the command palette has the same two
entries.

Besides TideUI's built-in palettes there is **`match-omarchy`**, which follows
the desktop's current [Omarchy](https://omarchy.org) theme instead of carrying
colours of its own:

```sh
./bin/tidegit -theme match-omarchy
```

It reads the active theme from `~/.local/state/omarchy/current/`, preferring
Omarchy's own `omarchy-theme-color` resolver and falling back to parsing the
theme's `colors.toml` / `alacritty.toml`. While it is selected TideGit polls for
desktop theme changes every two seconds and follows them, so changing your
desktop theme recolours the running app. Where Omarchy is not installed the
name still works and falls back to a built-in palette.

A desktop palette is chosen for a desktop, not for a dense terminal UI, so it is
run through TideUI's contrast correction before use: text, accents and the
status bar are each lifted until they clear a readability floor against the
theme's own background. Colours that already pass are left exactly as they are.

The chosen theme lasts for the session — TideGit has no configuration file yet,
so use `-theme` for a lasting default.

## History

The History screen walks refs on the left, draws the commit graph in the middle
and inspects the selected commit on the right.

The graph is drawn from real topology, not decoration: lanes are assigned from
each commit's parents, a lane keeps its column for as long as a commit is still
expected in it, and merges and forks are drawn where they actually happen.

| Glyph | Meaning |
| --- | --- |
| `●` | commit |
| `◆` | merge commit |
| `○` | first commit — no parents |
| `◉` | the commit HEAD points at |
| `┃ ━ ╭ ╮ ╰ ╯ ╋ ┳ ┻` | lanes, forks, merges, crossings and tees |

Lines use Unicode's heavy box-drawing set with light arcs for the corners. The
weight is what makes a lane read as a continuous line rather than a column of
dots, which is what the colours below need in order to say anything; the arcs
keep the turns soft against the rounded panes. Unicode has no heavy arc, so a
corner is lighter than the lines it joins — at a terminal's cell size that reads
as a taper into the turn. A plain theme falls back to ASCII (`| - + * %`).

Each line of development is drawn in its own colour, and keeps it for as long as
that line exists — across every row it passes through, even where it changes
column — so a branch can be followed down the page at a glance. A column reused
by a later branch gets a new colour rather than inheriting the finished one.
Where a commit reaches sideways, the horizontal run takes that commit's colour
while a lane it crosses keeps its own, so the two readings stay separable.

The colours are rotations of the theme's own accent, corrected until each clears
a contrast floor against whatever it is drawn on — including the selection
background, so selecting a row never hides the topology under it. A theme with
no hue variety of its own, like the amber VT52 or the green VT100, is left
monochrome: inventing colour for it would destroy the thing that makes it that
theme.

Refs are compact badges that carry a text signal as well as a colour, so they
stay readable in a plain terminal: `@ main` is where HEAD is, `# v1.0` is a tag,
`origin/main` is a remote-tracking branch, and a bare name is a local branch.
Badges that do not fit become `+2`.

The inspector shows the full hash, author and committer with their dates, the
parents, the message, the refs, and the changed files with per-file counts.
Enter on a changed file opens that commit's patch in the working-tree diff
viewer, labelled `Commit <hash>` so it is never mistaken for staged state.

History loads 120 commits at a time and asks for more as the selection nears the
end of the list. Filtering by ref, searching subjects and inspecting commits all
run through Git; nothing is filtered by loading the whole history into memory.

| Key | Action |
| --- | --- |
| j / k | Move through refs, commits or changed files |
| Tab | Refs → commits → inspector |
| Enter | Focus the next pane, or open the selected file's patch |
| / | Search commit subjects |
| n | Create a branch at the selected commit |
| y | Copy the full commit hash |
| Esc | Leave the patch and return to the commit |

## Branches

The Branches screen lists local and remote-tracking branches with the current
branch marked `@`, ahead/behind counts, and `✓` for a branch level with its
upstream. The middle pane shows the selected branch's own history; the inspector
gives its target, tip commit, upstream, merge state against HEAD and merge base.
Ahead/behind appears as `↑ 3  ↓ 1` **and** in words, so the arrows are never the
only cue.

| Key | Action |
| --- | --- |
| Enter / s | Switch to the selected local branch |
| n | Create a branch from the selection (Ctrl-S also switches to it) |
| R | Rename a local branch |
| D | Delete a local branch, after confirming |
| / | Filter the branch list |
| y | Copy the branch's target hash |

Switching uses `git switch` and nothing else: if you have local work that the
switch would overwrite, Git refuses and TideGit shows why, leaving your working
tree untouched. Deleting asks first and uses Git's safe delete. If Git refuses
because the branch holds unmerged commits, that refusal becomes a second,
differently worded question with a different key — nothing escalates to a forced
delete on its own.

Remote-tracking branches are read-only here: they can be inspected and branched
from, but not switched to, renamed or deleted.

## A repository to try it on

TideGit is most legible against real history, so the repository it is pointed at
matters. `scripts/demo-repo.sh` builds a throwaway one with enough shape to
exercise every screen:

```sh
scripts/demo-repo.sh /tmp/tidegit-demo
./bin/tidegit /tmp/tidegit-demo
```

It produces roughly 300 commits over about 15 months from six authors, with
concurrent branch lanes, merges that criss-cross, an octopus merge with four
parents, a long-running `develop`, a release branch and a hotfix, branches left
unmerged, an orphan history with no merge base, lightweight and annotated tags,
two remotes at different distances (including one upstream that has been
deleted), a rename, a deletion, a binary file, a very long subject, a
multi-paragraph message and uncommitted working-tree changes.

The destination is deleted and rebuilt on every run, so point it somewhere
disposable. `TRUNK_COMMITS=600 scripts/demo-repo.sh …` makes a larger one.

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
Commit tests cover message fidelity, hooks running in order, each hook rejection
keeping the repository and draft unchanged, amend with and without staged
changes, unborn HEAD, conflicts, merge commits with an unchanged tree, templates,
configured comment prefixes and cleanup, sign-off, signing remaining enabled,
stale index/HEAD refusals, the Git output panel, staged review navigation and
leaving a running commit.
History and branch tests cover linear history, merges, diverged branches, tags
on the right commits, remote-tracking refs, pagination, commit metadata and
changed-file parsing, the commit inspector and commit diff, graph topology and
lane stability, branch listing with upstream and ahead/behind, switching
(including a switch Git refuses), creation from HEAD, a commit and a tag,
renaming including the current branch, safe deletion, refused deletion of an
unmerged branch, detached HEAD, the command palette, and rendering at widths
from 160 down to 1 column in three themes.

## Scope and limits

Remote operations (push, pull, fetch), stash, conflict resolution, rebase,
cherry-pick, worktree and submodule management, forge integration, the
repository picker, persisted configuration and watching are not implemented.
Milestone 5 has not started.

The theme picker changes the running session only; there is no configuration
file to remember it in yet.

Remote refs are read as they are: nothing contacts a network. Tags are rendered
in history but there is no tag management. Branch work covers local branches
only — switch, create, rename and delete.

Committing covers `git commit` and `--amend` of HEAD. Interactive rebase, fixup
and squash helpers, commit splitting, editing older commits, and resuming an
interrupted merge, rebase or cherry-pick are not exposed. Conflicted files block
a commit until they are resolved with Git. The commit screen reviews the index
TideGit read; it does not poll for external changes, so refresh with Ctrl-R if
another process stages something while you write.

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

Status, diff and commit-review commands run asynchronously with a 30-second
deadline. The commit itself has none: hooks, signing and pinentry may
legitimately wait, so it runs until Git returns or you leave with Ctrl-C. Captured
output is bounded to 4 MiB per stream. Oversized status results fail explicitly;
diffs show a limited preview (also capped at 20,000 displayed lines). Scrollable
diff data is prepared once in the background. Truncated previews cannot be used
for hunk staging. Git itself may still use substantial
resources calculating very large diffs before cancellation. Preview limits are
not a substitute for future large-repository profiling.

See [architecture](docs/architecture.md) for API reuse, decisions and future
seams, and the validation records for what was checked and how:
[Milestone 3](docs/milestone-3-validation.md),
[Milestone 4](docs/milestone-4-validation.md).

---

<p align="center">
  <img src="images/TIDE-small.png"
       alt="TIDE — Terminal Information Delivery Engine"
       width="520">
</p>
