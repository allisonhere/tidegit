// Package config owns TideGit's human-editable configuration and its separate
// persistent state. It never creates a config file implicitly, and it has no
// knowledge of terminal widgets: callers give it paths and consume resolved
// values.
package config

import (
	"os"
	"path/filepath"
)

// App is the directory name used under the XDG roots.
const App = "tidegit"

// configHome resolves $XDG_CONFIG_HOME, falling back to ~/.config. It returns
// "" when neither is available, which callers treat as "no config file".
func configHome() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return dir
	}
	if home := homeDir(); home != "" {
		return filepath.Join(home, ".config")
	}
	return ""
}

// stateHome resolves $XDG_STATE_HOME, falling back to ~/.local/state.
func stateHome() string {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return dir
	}
	if home := homeDir(); home != "" {
		return filepath.Join(home, ".local", "state")
	}
	return ""
}

// homeDir resolves the user's home directory without string-concatenating "~".
func homeDir() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return home
	}
	return ""
}

// Dir is the config directory, or "" when no home can be resolved.
func Dir() string {
	base := configHome()
	if base == "" {
		return ""
	}
	return filepath.Join(base, App)
}

// Path is the primary config file path, or "" when no home can be resolved.
func Path() string {
	dir := Dir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "config.toml")
}

// OverridesPath is where in-app Settings changes are persisted. It is separate
// from config.toml so a carefully commented hand-edited file is never rewritten.
func OverridesPath() string {
	dir := Dir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "overrides.toml")
}

// StateDir is the state directory, or "" when no home can be resolved.
func StateDir() string {
	base := stateHome()
	if base == "" {
		return ""
	}
	return filepath.Join(base, App)
}

// StatePath is the persistent state file path, or "" when no home can be
// resolved.
func StatePath() string {
	dir := StateDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "state.toml")
}
