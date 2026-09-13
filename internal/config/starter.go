package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// starter is a clean, commented template. It is written only when the user
// explicitly asks to create a config file, so it stays readable rather than
// dumping every default.
const starter = `# TideGit configuration
#
# This file is optional. Every setting has a default, so leave anything you do
# not want to change commented out. Settings changed inside TideGit are stored
# separately in overrides.toml, so this file is never rewritten and your
# comments are safe.

version = 1

[appearance]
# theme = "catppuccin-mocha"    # any TideUI theme, or "match-omarchy"
# compact = true                # remove optional padding
# border_style = "round"        # round | square
# relative_time = true          # "4m ago" instead of a date
# icons = true                  # Unicode glyphs; false for plain ASCII
# animations = true

[layout]
# default_screen = "status"     # status | history | branches | stash | remotes | conflicts | reflog

[diff]
# context_lines = 3             # 0-50
# line_numbers = true
# mode = "unified"             # unified | split
# syntax = true                # colour by language
# word_highlight = true        # emphasise changed words
# show_whitespace = false      # mark tabs, spaces and trailing space
# whitespace = "normal"        # normal | ignore-trailing | ignore-change | ignore-all
# collapse = true              # collapse long unchanged runs

[editor]
# subject_guide = 50            # commit subject width hint
# body_guide = 72               # commit body width hint
# external_editor = ""          # overrides $VISUAL and $EDITOR

[behavior]
# mouse = false                 # requires a restart
# auto_refresh = false
# restore_last_repository = true
# recent_repository_count = 10

[git]
# history_page_size = 120
# show_remote_branches = true
# show_tags = true

[remote]
# verbose_progress = false
# operation_output_retention = 2000

# Keybindings are action ids. Keys containing a dot must be quoted.
# [keybindings]
# "app.quit" = "q"
# "git.commit" = "c"
`

// Starter returns the commented starter configuration.
func Starter() string { return starter }

// WriteStarter creates the starter config at path. It refuses to overwrite an
// existing file, so a hand-edited config is never clobbered.
func WriteStarter(path string) error {
	if path == "" {
		return fmt.Errorf("no config path is available")
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeAtomic(path, []byte(starter))
}
