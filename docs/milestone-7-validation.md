# Milestone 7 validation

Baseline inspection ran the full Milestone 6 suite, built and launched the
executable, and re-read the entry point, theme handling, editor resolution,
palette, dialogs and the existing state fields before changing anything. The
configuration package is new; the UI consumes resolved values through one
`Store`, and views never touch TOML.

## Automated coverage

Path tests set `XDG_CONFIG_HOME` and `XDG_STATE_HOME` to temporary directories
and check both the XDG paths and the `~/.config`, `~/.local/state` fallbacks,
with `HOME` redirected so the developer's real home is never used.

Config loader: a missing file returns defaults; a partial file merges over
defaults leaving untouched values alone; malformed TOML returns defaults with an
error; invalid enums, ranges and keybindings are rejected with every problem
listed; unknown keys warn with a nearest-path suggestion; a future `version` is
refused with an explanatory error and defaults; a missing version is treated as
current; the migration seam exists and is a no-op for version 1; `Effective`
lists resolved values.

Store: overrides persist to `overrides.toml` and are minimal; `config.toml` is
never rewritten, so a comment placed in it survives an in-app change; a fresh
store sees the persisted overrides; `Source` distinguishes default, config and
overrides; reset and reset-all return to the correct fallback; an out-of-range
value is rejected and never reaches the overrides file; corrupt overrides fall
back to defaults, are reported and are recoverable; a manual edit is picked up
by reload; a bad reload keeps the last valid configuration.

State: recent repositories keep most-recent order and stay bounded, the last
repository is tracked, paths are pruned when they disappear, removal works,
round-trips through the file, and corrupt or future-versioned state falls back
to a fresh state without failing.

UI: the Settings screen renders every category; a toggle changes the value,
persists it, marks the row and the inspector modified, and reset clears it;
search filters across categories; integer and enum controls adjust correctly;
reset-all confirms and clears; keybinding overrides resolve and a duplicate key
is reported as a conflict; key capture records and persists a new binding and
the new key then triggers the action; reload after a manual edit applies the new
theme; the effective-configuration overlay opens and closes; an invalid config
keeps the app on defaults and recovers after the file is fixed; and the screen
renders within bounds at eight sizes. The background-continuity test now covers
the Settings screen.

The suite is 241 top-level tests.

## Defects found and fixed during this milestone

- `github.com/pelletier/go-toml` v1 silently serialises a bare Go `int` to an
  empty document. Integer overrides are normalised to `int64` before writing.
- The same library drops comments even on an unchanged round trip, so the
  original "rewrite config.toml" plan would have destroyed hand edits. The
  design moved to a separate app-managed overrides file, which the spec lists as
  the best option when comment preservation is impractical.
- Dotted action ids such as `app.quit` are nested tables in TOML unless quoted;
  the write path quotes them, and the starter config documents the quoting.
- Status hardcoded `c`/`A`/`s`/`u` alongside the new keymap, so a rebind would
  not have taken effect; those keys now flow only through the action catalog.
- The diff loaders read the line-number and context settings inside background
  goroutines, which would race a live settings change; the values are captured
  on the model goroutine before the command starts.

## Manual verification

The executable builds and launches under a pseudo-terminal against a fixture
repository with `XDG_CONFIG_HOME` and `XDG_STATE_HOME` pointed at temporary
directories. Launching with no config directory present created no config file,
as intended, and the terminal setup was emitted normally.

The interactive matrix is exercised through the model and rendering harness:
no config, an empty config, a partial config, an invalid config and a
future-versioned config; changing the theme, compact mode, border style, icons,
animations, diff context lines, line numbers, editor guides, external editor,
mouse, auto-refresh, history page size, remote/tag visibility and remote output
retention; overriding a keybinding, creating a conflict and resetting it;
opening the config in an editor, editing it by hand and reloading; resetting one
setting and a whole category; recovery from invalid TOML; showing the effective
configuration; Settings search; and narrow and wide terminals in three themes.
Rendered views stay within their requested dimensions and keep a continuous
theme background.

## Final checks

`gofmt`, `go vet ./...`, `go test -race ./...`, an executable build, and a
launch under a pseudo-terminal.

No commits were made and nothing was staged in the TideGit source repository
during these checks; every Git mutation and every state/config write ran in a
temporary directory.
