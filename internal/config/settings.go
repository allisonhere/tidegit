package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	toml "github.com/pelletier/go-toml"
)

// Kind selects the control a setting is edited with.
type Kind int

const (
	KindBool Kind = iota
	KindEnum
	KindInt
	KindText
	KindTheme
)

// Setting is one editable configuration entry. The catalog is the single place
// that maps a dotted path to its type, range, defaults accessor and setter, so
// the Settings screen never knows field names and the loader never knows
// widgets.
type Setting struct {
	Path        string
	Category    string
	Title       string
	Description string
	Kind        Kind
	Enum        []string
	Min, Max    int
	Restart     bool
	Get         func(*Config) any
	Set         func(*Config, any) error
}

// Settings is every fixed setting, grouped by category in display order.
func Settings() []Setting {
	return []Setting{
		{Path: "appearance.theme", Category: "Appearance", Title: "Theme", Kind: KindTheme,
			Description: "TideUI palette for every screen. Choose match-omarchy to follow the desktop.",
			Get:         func(c *Config) any { return c.Appearance.Theme },
			Set:         func(c *Config, v any) error { c.Appearance.Theme = asString(v); return nil }},
		{Path: "appearance.compact", Category: "Appearance", Title: "Compact density", Kind: KindBool,
			Description: "Remove optional padding from rows and modals.",
			Get:         func(c *Config) any { return c.Appearance.Compact },
			Set:         func(c *Config, v any) error { c.Appearance.Compact = asBool(v); return nil }},
		{Path: "appearance.border_style", Category: "Appearance", Title: "Pane borders", Kind: KindEnum,
			Description: "Corner style for panes and modals.", Enum: []string{"round", "square"},
			Get: func(c *Config) any { return c.Appearance.BorderStyle },
			Set: func(c *Config, v any) error { c.Appearance.BorderStyle = asString(v); return nil }},
		{Path: "appearance.relative_time", Category: "Appearance", Title: "Relative timestamps", Kind: KindBool,
			Description: "Show ages like 4m ago instead of absolute dates.",
			Get:         func(c *Config) any { return c.Appearance.RelativeTime },
			Set:         func(c *Config, v any) error { c.Appearance.RelativeTime = asBool(v); return nil }},
		{Path: "appearance.icons", Category: "Appearance", Title: "Unicode icons", Kind: KindBool,
			Description: "Use box-drawing and glyphs; disable for plain ASCII.",
			Get:         func(c *Config) any { return c.Appearance.Icons },
			Set:         func(c *Config, v any) error { c.Appearance.Icons = asBool(v); return nil }},
		{Path: "appearance.animations", Category: "Appearance", Title: "Animations", Kind: KindBool,
			Description: "Animate the activity indicator while work runs.",
			Get:         func(c *Config) any { return c.Appearance.Animations },
			Set:         func(c *Config, v any) error { c.Appearance.Animations = asBool(v); return nil }},

		{Path: "layout.default_screen", Category: "Layout", Title: "Startup screen", Kind: KindEnum,
			Description: "The screen TideGit opens on.", Enum: screenNames,
			Get: func(c *Config) any { return c.Layout.DefaultScreen },
			Set: func(c *Config, v any) error { c.Layout.DefaultScreen = asString(v); return nil }},

		{Path: "diff.context_lines", Category: "Diff", Title: "Context lines", Kind: KindInt, Min: 0, Max: 50,
			Description: "Lines of unchanged context around each change.",
			Get:         func(c *Config) any { return c.Diff.ContextLines },
			Set:         func(c *Config, v any) error { return setInt(&c.Diff.ContextLines, v) }},
		{Path: "diff.line_numbers", Category: "Diff", Title: "Line numbers", Kind: KindBool,
			Description: "Show old and new line numbers in the diff gutter.",
			Get:         func(c *Config) any { return c.Diff.LineNumbers },
			Set:         func(c *Config, v any) error { c.Diff.LineNumbers = asBool(v); return nil }},
		{Path: "diff.mode", Category: "Diff", Title: "Default mode", Kind: KindEnum,
			Description: "Unified is one column; split shows old and new side by side.",
			Enum:        []string{"unified", "split"},
			Get:         func(c *Config) any { return c.Diff.Mode },
			Set:         func(c *Config, v any) error { c.Diff.Mode = asString(v); return nil }},
		{Path: "diff.syntax", Category: "Diff", Title: "Syntax highlighting", Kind: KindBool,
			Description: "Colour source code by language. Disabled automatically for very large diffs.",
			Get:         func(c *Config) any { return c.Diff.Syntax },
			Set:         func(c *Config, v any) error { c.Diff.Syntax = asBool(v); return nil }},
		{Path: "diff.word_highlight", Category: "Diff", Title: "Word-level changes", Kind: KindBool,
			Description: "Emphasise the words that changed within replaced lines.",
			Get:         func(c *Config) any { return c.Diff.WordHighlight },
			Set:         func(c *Config, v any) error { c.Diff.WordHighlight = asBool(v); return nil }},
		{Path: "diff.show_whitespace", Category: "Diff", Title: "Show whitespace", Kind: KindBool,
			Description: "Mark tabs, spaces and trailing whitespace in the diff.",
			Get:         func(c *Config) any { return c.Diff.ShowWhitespace },
			Set:         func(c *Config, v any) error { c.Diff.ShowWhitespace = asBool(v); return nil }},
		{Path: "diff.whitespace", Category: "Diff", Title: "Ignore whitespace", Kind: KindEnum,
			Description: "Git-native whitespace comparison for every diff.",
			Enum:        []string{"normal", "ignore-trailing", "ignore-change", "ignore-all"},
			Get:         func(c *Config) any { return c.Diff.Whitespace },
			Set:         func(c *Config, v any) error { c.Diff.Whitespace = asString(v); return nil }},
		{Path: "diff.collapse", Category: "Diff", Title: "Collapse context", Kind: KindBool,
			Description: "Collapse long runs of unchanged lines with an expandable marker.",
			Get:         func(c *Config) any { return c.Diff.Collapse },
			Set:         func(c *Config, v any) error { c.Diff.Collapse = asBool(v); return nil }},

		{Path: "editor.subject_guide", Category: "Editor", Title: "Subject guide", Kind: KindInt, Min: 10, Max: 200,
			Description: "Target width for a commit subject; the commit screen warns past it.",
			Get:         func(c *Config) any { return c.Editor.SubjectGuide },
			Set:         func(c *Config, v any) error { return setInt(&c.Editor.SubjectGuide, v) }},
		{Path: "editor.body_guide", Category: "Editor", Title: "Body guide", Kind: KindInt, Min: 10, Max: 300,
			Description: "Target width for commit body lines.",
			Get:         func(c *Config) any { return c.Editor.BodyGuide },
			Set:         func(c *Config, v any) error { return setInt(&c.Editor.BodyGuide, v) }},
		{Path: "editor.external_editor", Category: "Editor", Title: "External editor", Kind: KindText,
			Description: "Overrides $VISUAL and $EDITOR for external edits. Leave empty to use the environment.",
			Get:         func(c *Config) any { return c.Editor.ExternalEditor },
			Set:         func(c *Config, v any) error { c.Editor.ExternalEditor = asString(v); return nil }},

		{Path: "behavior.mouse", Category: "Behavior", Title: "Mouse", Kind: KindBool, Restart: true,
			Description: "Enable mouse input. Takes effect after a restart.",
			Get:         func(c *Config) any { return c.Behavior.Mouse },
			Set:         func(c *Config, v any) error { c.Behavior.Mouse = asBool(v); return nil }},
		{Path: "behavior.auto_refresh", Category: "Behavior", Title: "Auto refresh", Kind: KindBool,
			Description: "Periodically re-read the repository while the Status screen is idle.",
			Get:         func(c *Config) any { return c.Behavior.AutoRefresh },
			Set:         func(c *Config, v any) error { c.Behavior.AutoRefresh = asBool(v); return nil }},
		{Path: "behavior.restore_last_repository", Category: "Behavior", Title: "Restore last repository", Kind: KindBool,
			Description: "Open the last repository when no path is given.",
			Get:         func(c *Config) any { return c.Behavior.RestoreLastRepository },
			Set:         func(c *Config, v any) error { c.Behavior.RestoreLastRepository = asBool(v); return nil }},
		{Path: "behavior.recent_repository_count", Category: "Behavior", Title: "Recent repositories", Kind: KindInt, Min: 0, Max: 50,
			Description: "How many recent repositories to remember.",
			Get:         func(c *Config) any { return c.Behavior.RecentRepositoryCount },
			Set:         func(c *Config, v any) error { return setInt(&c.Behavior.RecentRepositoryCount, v) }},

		{Path: "git.history_page_size", Category: "Git", Title: "History page size", Kind: KindInt, Min: 20, Max: 2000,
			Description: "Commits loaded per page in History.",
			Get:         func(c *Config) any { return c.Git.HistoryPageSize },
			Set:         func(c *Config, v any) error { return setInt(&c.Git.HistoryPageSize, v) }},
		{Path: "git.show_remote_branches", Category: "Git", Title: "Remote branches", Kind: KindBool,
			Description: "Show remote-tracking branches in ref filters and the Branches list.",
			Get:         func(c *Config) any { return c.Git.ShowRemoteBranches },
			Set:         func(c *Config, v any) error { c.Git.ShowRemoteBranches = asBool(v); return nil }},
		{Path: "git.show_tags", Category: "Git", Title: "Tags", Kind: KindBool,
			Description: "Show tags in ref filters.",
			Get:         func(c *Config) any { return c.Git.ShowTags },
			Set:         func(c *Config, v any) error { c.Git.ShowTags = asBool(v); return nil }},

		{Path: "remote.verbose_progress", Category: "Remote", Title: "Verbose progress", Kind: KindBool,
			Description: "Show the latest Git progress line in the header while an operation runs.",
			Get:         func(c *Config) any { return c.Remote.VerboseProgress },
			Set:         func(c *Config, v any) error { c.Remote.VerboseProgress = asBool(v); return nil }},
		{Path: "remote.operation_output_retention", Category: "Remote", Title: "Output retention", Kind: KindInt, Min: 100, Max: 100000,
			Description: "Lines of operation output kept for the details panel.",
			Get:         func(c *Config) any { return c.Remote.OperationOutputRetention },
			Set:         func(c *Config, v any) error { return setInt(&c.Remote.OperationOutputRetention, v) }},
	}
}

// SettingByPath finds a fixed setting.
func SettingByPath(path string) (Setting, bool) {
	for _, s := range Settings() {
		if s.Path == path {
			return s, true
		}
	}
	return Setting{}, false
}

// Categories lists the fixed categories in display order, with Keybindings
// appended by the UI.
func Categories() []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range Settings() {
		if !seen[s.Category] {
			seen[s.Category] = true
			out = append(out, s.Category)
		}
	}
	return out
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func asBool(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		b, _ := strconv.ParseBool(strings.TrimSpace(x))
		return b
	default:
		return false
	}
}

func setInt(dst *int, v any) error {
	switch x := v.(type) {
	case int:
		*dst = x
	case int64:
		*dst = int(x)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(x))
		if err != nil {
			return fmt.Errorf("expected a number, got %q", x)
		}
		*dst = n
	default:
		return fmt.Errorf("expected a number, got %v", v)
	}
	return nil
}

// Store owns configuration precedence and persistence. It merges compiled
// defaults, the hand-edited config.toml, and the app-managed overrides file,
// and never rewrites config.toml.
type Store struct {
	primaryPath, overridesPath string
	base                       *Config
	overrides                  map[string]any
	warnings, errors           []string
}

// Open builds a store from the two config paths. Whether the files exist is
// irrelevant; missing files simply leave their layer empty.
func Open(primaryPath, overridesPath string) *Store {
	store := &Store{primaryPath: primaryPath, overridesPath: overridesPath, overrides: map[string]any{}}
	store.reload(true)
	return store
}

// PrimaryPath is the hand-editable config file.
func (s *Store) PrimaryPath() string { return s.primaryPath }

// OverridesPath is the app-managed overrides file.
func (s *Store) OverridesPath() string { return s.overridesPath }

// Warnings returns the accumulated non-fatal load warnings.
func (s *Store) Warnings() []string { return append([]string(nil), s.warnings...) }

// Errors returns the accumulated load errors from the last reload.
func (s *Store) Errors() []string { return append([]string(nil), s.errors...) }

// Base is the config.toml layer merged over defaults, without overrides.
func (s *Store) Base() *Config { return s.base.clone() }

// Overrides returns a copy of the app-managed override map.
func (s *Store) Overrides() map[string]any {
	out := make(map[string]any, len(s.overrides))
	for k, v := range s.overrides {
		out[k] = v
	}
	return out
}

// Config is the effective configuration: defaults, then config.toml, then
// in-app overrides.
func (s *Store) Config() *Config {
	cfg := s.base.clone()
	paths := make([]string, 0, len(s.overrides))
	for path := range s.overrides {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		_ = s.applyOverride(cfg, path, s.overrides[path])
	}
	return cfg
}

// applyOverride sets one dotted path. Keybindings are the only dynamic map.
func (s *Store) applyOverride(cfg *Config, path string, value any) error {
	if id, ok := strings.CutPrefix(path, "keybindings."); ok {
		if cfg.Keybindings == nil {
			cfg.Keybindings = map[string]string{}
		}
		cfg.Keybindings[id] = asString(value)
		return nil
	}
	setting, ok := SettingByPath(path)
	if !ok {
		return fmt.Errorf("unknown setting %q", path)
	}
	return setting.Set(cfg, value)
}

// Set records an in-app change and persists it. The value is validated through
// the setting's own setter, so a bad value never reaches the overrides file.
func (s *Store) Set(path string, value any) error {
	cfg := s.Config()
	if id, ok := strings.CutPrefix(path, "keybindings."); ok {
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("a keybinding action id cannot be empty")
		}
	} else {
		setting, ok := SettingByPath(path)
		if !ok {
			return fmt.Errorf("unknown setting %q", path)
		}
		if err := setting.Set(cfg, value); err != nil {
			return err
		}
		if errs := Validate(cfg); len(errs) > 0 {
			return fmt.Errorf("%s", errs[0])
		}
	}
	s.overrides[path] = normalizeValue(value)
	return s.Save()
}

// normalizeValue keeps integer overrides in the form the TOML writer handles;
// go-toml v1 silently fails to serialise a bare Go int.
func normalizeValue(v any) any {
	if n, ok := v.(int); ok {
		return int64(n)
	}
	return v
}

// Reset removes an in-app override, returning the setting to config.toml or the
// compiled default.
func (s *Store) Reset(path string) error {
	delete(s.overrides, path)
	return s.Save()
}

// ResetAll clears every in-app override. It deliberately leaves config.toml and
// persistent state untouched.
func (s *Store) ResetAll() error {
	s.overrides = map[string]any{}
	return s.Save()
}

// IsOverridden reports whether a path is set in the app-managed layer.
func (s *Store) IsOverridden(path string) bool {
	_, ok := s.overrides[path]
	return ok
}

// Source describes where a path's effective value comes from.
func (s *Store) Source(path string) string {
	if s.IsOverridden(path) {
		return "overrides.toml"
	}
	if s.differsFromDefault(s.base, path) {
		return "config.toml"
	}
	return "default"
}

func (s *Store) differsFromDefault(base *Config, path string) bool {
	setting, ok := SettingByPath(path)
	if !ok {
		if base.Keybindings[strings.TrimPrefix(path, "keybindings.")] != "" {
			return true
		}
		return false
	}
	return fmt.Sprint(setting.Get(base)) != fmt.Sprint(setting.Get(Default()))
}

// Override records an in-memory change without writing it. It is used by
// callers that supply their own configuration (such as a resolved theme flag)
// and never persist.
func (s *Store) Override(path string, value any) error {
	cfg := s.Config()
	if err := s.applyOverride(cfg, path, normalizeValue(value)); err != nil {
		return err
	}
	if errs := Validate(cfg); len(errs) > 0 {
		return fmt.Errorf("%s", errs[0])
	}
	s.overrides[path] = normalizeValue(value)
	return nil
}

// Save writes the app-managed overrides file atomically.
func (s *Store) Save() error {
	if s.overridesPath == "" {
		return fmt.Errorf("no overrides path is available")
	}
	tree := NewTree()
	for path, value := range s.overrides {
		tree.SetPath(strings.Split(path, "."), normalizeValue(value))
	}
	if err := os.MkdirAll(filepath.Dir(s.overridesPath), 0o755); err != nil {
		return err
	}
	return writeAtomic(s.overridesPath, []byte(tree.String()))
}

// Reload re-reads both layers. If either fails, the previous valid values are
// kept and the problem is reported.
func (s *Store) Reload() error {
	base, report, err := Load(s.primaryPath)
	overrides, overrideWarnings, overrideErr := loadOverrides(s.overridesPath)

	s.warnings = append([]string(nil), report.Warnings...)
	s.warnings = append(s.warnings, overrideWarnings...)
	s.errors = append([]string(nil), report.Errors...)

	if err != nil {
		s.errors = append(s.errors, err.Error())
		return err
	}
	if overrideErr != nil {
		s.errors = append(s.errors, overrideErr.Error())
		return overrideErr
	}
	s.base = base
	s.overrides = overrides
	return nil
}

// reload is the Open path; it tolerates errors so a broken file never blocks
// startup.
func (s *Store) reload(initial bool) {
	if err := s.Reload(); err != nil {
		if s.base == nil {
			s.base = Default()
		}
	}
	if s.base == nil {
		s.base = Default()
	}
	if s.overrides == nil {
		s.overrides = map[string]any{}
	}
}

// loadOverrides reads the app-managed layer into dotted paths.
func loadOverrides(path string) (map[string]any, []string, error) {
	values := map[string]any{}
	if path == "" {
		return values, nil, nil
	}
	tree, err := readTree(path)
	if os.IsNotExist(err) {
		return values, nil, nil
	}
	if err != nil {
		return values, nil, err
	}
	var warnings []string
	var walk func(prefix string, node *toml.Tree)
	walk = func(prefix string, node *toml.Tree) {
		for _, key := range node.Keys() {
			if key == "version" {
				continue
			}
			dotted := key
			if prefix != "" {
				dotted = prefix + "." + key
			}
			if child, ok := node.GetPath([]string{key}).(*toml.Tree); ok {
				walk(dotted, child)
				continue
			}
			values[dotted] = node.GetPath([]string{key})
		}
	}
	walk("", tree)
	for path := range values {
		if _, ok := SettingByPath(path); !ok && !strings.HasPrefix(path, "keybindings.") {
			warnings = append(warnings, fmt.Sprintf("Unknown setting in overrides: %s", path))
		}
	}
	return values, warnings, nil
}
