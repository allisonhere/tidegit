# Architecture

## Inspected APIs

Before implementation, the local TideUI source, its README, and TideMail's Ripple
adapter were inspected, along with the cached Ripple v0.3.0 API documentation.
The build pins a published TideUI containing the soft-panel API, the background
continuity helpers (`StyleOver`) and the exported contrast correction
(`CorrectThemeContrast`, `ShiftHue`). There is no local `replace`: the pin is a
version every machine can resolve.

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
- `internal/git/commit.go`: commit review metadata, index identity and the
  `git commit` invocation.
- `internal/ui/model.go`: focus, selection, filter, refresh generation and
  background command lifecycle.
- `internal/ui/view.go`: TideUI shell and context rendering.
- `internal/ui/diff.go`: terminal-safe diff presentation, line numbers and colors.
- `internal/ui/actions.go`: typed actions, mutation lifecycle, selection following
  and hunk navigation; suitable for a future palette dispatcher.
- `internal/ui/commit.go`: commit state, Ripple lifecycle and message routing.
- `internal/ui/commit_view.go`: commit screen rendering and Ripple styling.
- `internal/ui/clipboard.go`: platform clipboard adapter for Ripple.
- `internal/git/history.go`: commit records, paging, changed files, commit diffs.
- `internal/git/refs.go`: ref listing, decoration parsing, HEAD resolution.
- `internal/git/branch.go`: branch listing, tracking data and branch mutations.
- `internal/git/graph.go`: lane assignment — topology only, no presentation.
- `internal/ui/screen.go`: screen routing, shared keys and responsive layout.
- `internal/ui/history.go` / `history_view.go`: History state and rendering.
- `internal/ui/branches.go` / `branches_view.go`: Branches state and rendering.
- `internal/ui/graph_view.go`: graph glyphs and ref badges.
- `internal/ui/palette.go`, `internal/ui/prompt.go`, `internal/ui/choice.go`:
  command palette, one-line prompts, confirmations and short option lists.
- `internal/ui/visual.go`: shared chrome, help panels and activity indicator.
- `internal/git/remote.go`: remote model, fetch/pull/push and the typed
  classification of network failures. No UI imports.
- `internal/git/stash.go`: stash listing, files, per-file patches and the
  create/apply/pop/drop mutations.
- `internal/ui/operation.go`: the reusable operation-details component, the
  goroutine-safe progress buffer and post-operation refresh.
- `internal/ui/remote.go` / `remote_view.go`: Remotes screen state and rendering.
- `internal/ui/stash.go` / `stash_view.go`: Stash screen state and rendering.
- `internal/git/state.go`: repository-operation detection and conflict kinds,
  read from Git's state files rather than process memory.
- `internal/git/conflict.go`: unmerged index stages, conflict-marker parsing and
  the file-level resolution actions.
- `internal/git/continue.go`, `recover.go`, `reflog.go`: continue/skip/abort,
  reset/revert/switch-detached, and the recovery timeline.
- `internal/ui/conflicts.go` / `conflicts_view.go`: Conflicts screen and logic.
- `internal/ui/reflog.go` / `reflog_view.go`: Reflog screen and logic.
- `internal/ui/recovery.go`, `reset.go`: the shared recovery runner, destructive
  confirmations and the reset/restore workflows.
- `internal/config`: typed configuration, XDG paths, layering, validation,
  migration, atomic persistence and persistent state. No UI imports.
- `internal/ui/settings.go` / `settings_view.go`: the Settings screen. It reads
  the setting catalog and resolved values; it never parses TOML itself.
- `internal/ui/keys.go`: the stable action catalog and the customizable keymap.

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
this working-tree screen. Operation state is read only where a commit depends on
it (`MERGE_HEAD`); presenting and continuing merge, rebase or cherry-pick, and
conflict resolution, remain future work.

## Commit (Milestone 3)

`commitState` embeds `ripple.Model` from `ripple.New`, wired with `SetClipboard`,
`SetValue`, `SetPlaceholder`, `Focus`/`Blur`, `SetSize`, `Update` and
`View(ripple.Options{...})`. Ripple owns cursor movement, wrapping, selection,
undo/redo and the buffer; TideGit never rewraps its rendered output and clips it
to exactly the allocated cells. `StyleKey`/`Style` mark the subject line and
comment lines without changing the text Git receives. `SubmitMsg` and `CancelMsg`
route to the host's submit and cancel actions, and Ripple's `CopiedMsg`/`PasteMsg`
errors surface as an inline clipboard notice rather than a failed edit. The
clipboard adapter runs fixed argv (`pbcopy`/`pbpaste`, `wl-copy`/`wl-paste`,
`xclip`) with text passed only over stdin, and its absence is reported, not fatal.

`PrepareCommit` reads only: index digest, status, comment prefix, HEAD message or
`commit.template`, and a staged numstat summary. It never stages or alters the
index, and it re-reads the index digest afterwards so a review assembled while
another process wrote the index is rejected instead of displayed. `meaningfulCommit`
refuses conflicted trees and empty commits, while deliberately allowing a merge
commit with an unchanged tree by checking `MERGE_HEAD` through `rev-parse
--git-path`.

`Commit` runs ordinary `git commit --file=-` under the same per-repository
mutation semaphore as staging, with the message on stdin so no path, quoting or
temporary file is involved. Hooks, signing, identity and cleanup stay Git's.
Only the implicit `-F` cleanup default is overridden — to `strip`, matching a
composed message — and an explicit `commit.cleanup` is always preserved.
`--amend` and `--signoff` are passed through as flags. The recorded `ExpectedHead`
and `ExpectedIndex` from the review are re-checked inside the lock, so a commit
can never record content the author did not see. A failure returns Git's captured
output with the draft intact; the buffer is only cleared on success.

Unlike every other command, the commit has no deadline: hooks, signing and
pinentry may legitimately wait for a person. That makes the UI's usual busy gate
a trap, so while Git runs, Ctrl-C quits TideGit. Everything else stays gated, and
the draft is the only thing lost. Ripple keeps Ctrl-C as copy while editing.

The commit screen's staged preview reuses the diff pipeline and generation IDs,
so its reads cancel and reject stale completions like any other diff.

## History and branches (Milestone 4)

History is read through `git log --topo-order` with a `--pretty=format` built
from `%`-placeholders separated by ASCII unit and record separators. Git never
emits those bytes from a placeholder, so a subject, author name or ref
decoration containing tabs, newlines or quotes cannot make the stream
ambiguous. No human-oriented Git output is parsed anywhere. Refs come from
`for-each-ref` with the same separators, dereferencing annotated tags to their
commit so a tag matches the commit it marks. Branch tracking uses
`%(upstream:track,nobracket)`, which costs one command for the whole list
rather than one `rev-list` per branch.

Topological order is deliberate: date order can interleave branches between
pages and make lanes jump when the next page arrives. `--topo-order` keeps a
page boundary from reordering what is already drawn.

A commit's changed files are read as two `-z` diffs against the first parent —
`--numstat` for counts and `--name-status` for change kinds — merged by path. A
merge is therefore summarised the way `git show` summarises it, and a root
commit is compared against the empty tree by asking `git show` instead of
inventing an empty-tree hash. One file's patch is fetched with the same flags
the working-tree viewer uses, so `internal/ui/diff.go` renders history and the
working tree through one code path. A historical `Diff` carries its commit and
an explicit `HunkUnavailable`: staging actions apply to the working tree, and a
patch from history can never become a staging target.

`GraphLanes` is a pure function from commits to per-row lane geometry. It holds
no glyphs, colours or widths, so the renderer chooses box characters or ASCII,
and a later screen — rebase, cherry-pick, a compare view — can reuse the
topology without the History screen. Each lane remembers the commit it is
waiting for; a commit claims the leftmost lane waiting for it, other lanes
waiting for the same commit collapse into it, and parents are handed back out
with the first keeping the commit's own lane so straight history never drifts
sideways. A lane released by a merge stays reserved until the row is finished,
because reusing it immediately would draw one line both ending and starting in
a single cell. Rendering caps the drawn lanes and marks the overflow rather than
pushing the subject off the row.

Lane colour is why the model carries `Tracks`. A colour keyed to the column
would change meaning whenever a branch moved sideways or a column was reused,
so each line is given an identity when it starts and keeps it until it ends;
the renderer maps that identity to a colour. The identity is topology, not
presentation, which is why it belongs in the model — the palette does not.

Lines are drawn with the heavy box-drawing set rather than the light one. The
weight is not decoration: at the stroke width of the light set a coloured lane
reads as a column of dots rather than a line, which defeats the point of
colouring it. Corners stay light, because Unicode has no heavy arc and the
curve was worth more than uniform weight — the mismatch reads as a taper into
the turn at the size a terminal actually draws it.

The palette is built from the theme's own accent by hue rotation, so it belongs
to the theme rather than being a fixed set of colours, and every entry is run
through the contrast correction before use. It is rebuilt against the selection
background for a selected row, because the one thing selection must not do is
hide the topology underneath it. A theme whose own colours share a single hue
is detected and left alone: a green phosphor terminal that suddenly grew six
hues would no longer be one.

## Screens, loading and responsiveness

`screen.go` routes one keystroke: global gates, then whichever overlay is open,
then the active screen. Screens keep their own state, so leaving and returning
preserves a selection; `r` reloads on demand. Returning to Status always
rescans, because a branch switch made elsewhere changes the working tree.

History loads `HistoryBatch` commits at a time and requests the next page as the
selection approaches the end. Lanes are recomputed over the whole list when a
page arrives so the new page continues the lanes the earlier pages established.
Commit inspection is debounced and cached: holding `j` schedules one load after
the cursor settles, a commit already inspected is shown without touching Git,
and the cache is bounded so walking a long history does not retain every commit
it passed. Generation counters reject stale results, and each concurrent load
owns its own context — two commands sharing one would let whichever finished
first cancel the other.

Layout has three tiers rather than a cliff: three columns when there is room,
TideUI's `StackedRight` at medium widths so the sidebar survives and the other
two panes stack, and `Tabbed` when the terminal is genuinely small. Commit rows
give up metadata in a fixed order — author, age, hash, refs — and the columns
are decided once per render so the hash and the right-hand gutter line up down
the whole pane instead of moving from row to row. The graph and the subject are
never sacrificed.

## Branch mutations

Switching, creating, renaming and deleting run through the same per-repository
mutation semaphore as staging and committing, and each is ordinary Git
porcelain: `switch`, `branch`, `branch --move`, `branch --delete`. Checkout
semantics stay Git's — nothing is stashed, reset or force-checked-out on a
caller's behalf, so a switch that would overwrite local work is refused by Git
and reported. Names are checked with `check-ref-format --branch` before any
mutation, which catches a typo without touching the repository while leaving
Git the authority on what is valid.

Deletion is safe by default. `DeleteBranch` only passes `-D` when the caller
explicitly asks, and a refusal for unmerged commits is returned as a typed
`ErrUnmergedBranch`. The UI turns that into a second confirmation with different
wording and a different key; nothing escalates on its own. The background worker
only builds a message — model state is never written from a command goroutine —
and every mutation is followed by a rescan, so the screen never shows state from
before the change.

## Themes

The picker is TideUI's own `ThemePicker`; TideGit supplies only what the toolkit
leaves to the host — when it opens, which theme the view renders with while a
preview is live, and what happens to the confirmed choice. Previewing works by
having `renderer()` build from `activeTheme()` rather than the stored theme, so
the entire app repaints in the highlighted palette without anything being
committed to.

`internal/omarchy` reads the desktop's active theme, preferring Omarchy's own
`omarchy-theme-color` resolver (which applies its alias and shade cascade) and
falling back to parsing the staged theme files. It is the same reader the other
Tide applications use, copied rather than shared because there is no common
module to hold it yet.

`match-omarchy` is not a stored palette: it is resolved when selected and
re-resolved whenever the desktop changes. A cheap signature — the theme name
plus the state file's mtime — is polled every two seconds, and the palette is
only re-read when that token changes. The poll starts when the theme is chosen
or at launch, refuses to stack a second loop, and stops on the first tick after
the theme changes to something else.

Contrast correction lives in TideUI (`CorrectThemeContrast`) rather than here.
An arbitrary desktop palette cannot be trusted to be readable in a dense
terminal UI, but the maths for fixing that is the toolkit's concern, not the
application's — and each of the other Tide applications had grown its own copy.
The floors are the ones the built-in themes already meet, so correction is close
to a no-op on a hand-tuned palette and only moves a colour that genuinely fails.
The focused pane border is deliberately not corrected at theme level, because
`BuildStyles` already lifts that one where it renders.

## Remotes and stashes (Milestone 5)

Remote names, URLs and tracking branches come from Git configuration and
`for-each-ref`, never from `git remote -v` or any human-oriented output.
`config --null --get-regexp` keeps a URL containing spaces unambiguous, and
`remote.pushDefault`, `branch.<name>.pushRemote`/`.remote`, then `origin`, then a
sole remote resolve the default in that order. When that resolution is
ambiguous the model returns "", and the UI asks through a small choice panel
rather than guessing.

Network commands run through `runStream`, a sibling of the ordinary runner that
reads Git's stderr as it arrives while still capturing it in full. It is the
only place a subprocess is streamed, and it appends `GIT_TERMINAL_PROMPT=0` so
Git cannot stop on a credential prompt the alt-screen UI cannot show; SSH
agents, SSH config and credential helpers are untouched. Git is asked for
progress explicitly (`--progress`) because a pipe suppresses it by default.
`stash push -u` is the one command that runs without `--literal-pathspecs`,
because Git implements include-untracked through pathspec magic that the flag
disables; the stash argument list contains no caller paths, so nothing is left
ambiguous.

Fetch, pull and push are ordinary `git fetch`, `git pull` and `git push`. Pull
passes no strategy flag, so `pull.rebase`/`pull.ff` and Git's own refusal for an
ambiguous divergence are preserved. Push passes no refspec when an upstream
exists, so `push.default` still decides; a first push alone adds
`--set-upstream`. Failures are classified by inspecting Git's stderr into
authentication, DNS, unreachable host, permission, missing repository,
non-fast-forward, protected branch, missing upstream, pull strategy, conflict
and dirty working tree. The classification adds a plain sentence and never
discards the underlying `CommandError`, which the operation panel shows under
`e`.

The operation-details component is deliberately generic: a title, a target, a
running/done/failed state, a concise summary, a bounded raw-output buffer and a
retry closure. Fetch, pull, push and stash all report through it, and the same
shape is what rebase, cherry-pick and clone will use. Progress is written from
the command goroutine into a mutex-guarded buffer; the model is never written
from a goroutine. A finished operation invalidates history, branch, stash and
remote caches and re-reads the working-tree snapshot, so no screen shows state
from before the change.

Stash entries are parsed from `git stash list --format` with the same separator
scheme history uses; the source branch and message are split out of the stash
commit's own generated subject in the Git layer, not the UI. Files come from
`stash show --numstat`/`--name-status` and one file's patch from `stash show -p`,
split per file and rendered through the shared diff renderer. Because
`stash@{n}` renumbers after a drop or pop, a reload restores the selection by
the stash's commit id, and no action ever runs against a stale index. Apply,
pop and drop go through the same per-repository mutation semaphore as staging,
branch work and commits; a conflicted apply or pop is detected and the stash is
left exactly where Git left it.

## Conflicts and recovery (Milestone 6)

Repository operation state is read from Git's own state files, never from UI
memory. `State` resolves `MERGE_HEAD`, `CHERRY_PICK_HEAD`, `REVERT_HEAD`,
`rebase-merge`/`rebase-apply` and `BISECT_LOG` through `rev-parse --git-path`,
so a fresh process rediscovers a half-finished rebase before its first scan
completes. Rebase progress comes from `msgnum`/`end` (or `next`/`last`), the
branch from `head-name`, and the remaining cherry-pick/revert steps from the
sequencer's `todo`. The status scan carries the state alongside the file list,
and the header rule becomes a state ribbon while an operation is active. A
restart therefore needs no special handling: the same scan finds the state.

Conflict kinds come from porcelain v2's unmerged `XY` codes (`UU`, `AA`, `DU`,
`UD`, `AU`, `UA`, `DD`), not from scanning text. Stages come from one
`ls-files --unmerged` call that yields each side's blob id and mode; blob
contents are then read by object id, so no path ever enters revision syntax.
Conflict markers are parsed into structured regions only for files Git already
reports as unmerged, and only complete `<<<<<<<`/`=======`/`>>>>>>>` blocks
(with an optional diff3 `|||||||` base) count; a stray separator line in
ordinary content is ignored. Regions are stored with 1-based line ranges so the
working-file view can colour each side without re-parsing.

Resolution is deliberately split: `ResolveOurs`/`ResolveTheirs` write one stage
over the working file and stop, while the uppercase UI actions call the same
functions with `mark` set. Marking resolved is `git add -A`, so the index stays
the single source of truth and a deletion resolution is recorded too. Keep-both
is a literal concatenation of the two sides, left unstaged; it does not pretend
to merge. An external editor is launched with `tea.ExecProcess`, which suspends
and restores the alt screen, and its return only refreshes the file.

Continue, skip and abort are Git's own porcelain commands with `GIT_EDITOR=true`
so a prepared message is accepted instead of opening an editor. Git performs
every safety check itself; TideGit adds wording, never bypasses. Abort always
confirms and names the operation and what will be restored.

The reflog is read with `git reflog --date=unix`, which makes the selector carry
the reflog entry's own timestamp; the entry's normalised `HEAD@{n}` selector is
recomputed from position. Recovery reuses the existing commit inspector
(`CommitDetail`, `CommitFiles`, `CommitDiff`) and the shared diff renderer.
Creating a branch at an entry is the prominent action; reset and switch-detach
are behind the reusable destructive confirmation, which states the exact target,
what changes, and whether the reflog keeps it recoverable. Reset's three modes
each carry their own description, and hard reset is the only one marked
destructive.

## Configuration and state (Milestone 7)

`internal/config` is the only package that reads or writes configuration. It
resolves XDG paths with `os.UserHomeDir` rather than string-building `~`, and it
keeps configuration and state in separate files with separate version numbers.
The typed `Config` is a set of defaults plus overrides; `Default` is the single
source of truth, and loading unmarshals only the keys a file actually contains,
so a partial file is safe.

The config is layered deliberately: compiled defaults, then `config.toml`, then
an app-managed `overrides.toml`, then session overrides such as `-theme`. The
`Store` owns that merge and is the only writer. It never rewrites `config.toml`;
in-app changes go to `overrides.toml`, which is why a hand-edited file with
comments survives the Settings screen byte for byte. Writes are atomic: a temp
file in the same directory is written, fsynced and renamed. This is the spec's
"store app-managed overrides separately" option, chosen because the available
TOML writer cannot preserve comments.

Reading is tolerant by design. Malformed TOML, an invalid enum or range, an
unsupported future version and a conflicting keybinding are reported but never
fatal: the loader returns the defaults with an error, and a reload keeps the
last valid configuration. Unknown keys become warnings, with a nearest known
path suggested through a small Levenshtein pass. The migration seam is a map
from target version to a function; version 1 has none, and a later milestone
adds one entry per version without touching the loader.

The Settings screen is driven by a catalog: every fixed setting is a `Setting`
with a path, category, kind, enum or range, default accessor and a typed setter.
The screen renders controls from that metadata and never knows field names; the
config package never knows widgets. Keybindings are the one dynamic map, edited
by stable action id. The action catalog in `ui/keys.go` holds each id, its
description and its default key; `globalKey` reverse-looks-up a pressed key and
dispatches through the same handlers the original hardcoded switches used, so
rebinding changes the key and not the behaviour. Resolving the keymap reports
unknown ids and duplicate keys instead of silently accepting them.

Persistent state is its own versioned file. It carries the last repository, a
bounded recent list, the last screen and dismissed hints, and corruption falls
back to a fresh state rather than blocking startup. Configuration reset clears
only `overrides.toml`; it never touches state.

## Remaining concerns

TideUI does not export pane geometry; the host currently mirrors the 2:3:5
column calculation for row widths. Shared geometry would help later mouse
hit testing and resizing. Narrow layouts use tabbed rendering.

Output limits bound retained bytes but do not prevent Git's own resource usage.
Very large status lists fail visibly rather than silently dropping paths. Later
performance work should profile scan latency, add debounced watching and assess
incremental status strategies without compromising Git semantics.

The index digest is a whole-index SHA-256 of `ls-files --stage`, which is exact
but O(index). A large repository will want a cheaper identity, and the digest
already fails loudly rather than silently on an oversized index. Operation-state
detection (merge/rebase/cherry-pick) is read only far enough to permit a merge
commit; presenting and continuing those operations remains future work, as does
amending anything other than HEAD. Worktree-aware index identity will be needed
when worktree support enters scope; the current semaphore keys ordinary roots.

Killing TideGit during a commit leaves Git to be killed with it, exactly as
Ctrl-C at a shell prompt would; Git's own lock files remain the recovery
mechanism. A future watching layer should treat the commit review as a consumer
of index change notifications instead of requiring a manual Ctrl-R.

History paging uses `--skip`, which Git satisfies by walking from the start each
time; it is fine for the pages a person scrolls through but is not the right
mechanism for walking deep into a very large repository. A commit-id cursor
would replace it without changing the UI. Ref and branch lists are read whole:
a repository with thousands of refs will want filtering pushed into
`for-each-ref`. The graph is computed over the loaded page rather than the whole
history, which is what keeps it cheap, and is correct because lanes are derived
from the commits actually shown.
