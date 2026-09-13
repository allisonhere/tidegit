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

// CurrentVersion is the config schema version this build writes and understands.
const CurrentVersion = 1

// Config is TideGit's typed configuration. Every field has a default; a config
// file is a set of overrides on top of those defaults.
type Config struct {
	Version     int               `toml:"version"`
	Appearance  Appearance        `toml:"appearance"`
	Layout      Layout            `toml:"layout"`
	Diff        Diff              `toml:"diff"`
	Editor      Editor            `toml:"editor"`
	Behavior    Behavior          `toml:"behavior"`
	Git         Git               `toml:"git"`
	Remote      Remote            `toml:"remote"`
	Keybindings map[string]string `toml:"keybindings"`
	// AI is reserved for a later milestone. It is parsed and validated so a
	// hand-written [ai] section does not read as an unknown key, but it is not
	// surfaced in Settings and no AI behavior is implemented.
	AI AI `toml:"ai"`
}

type Appearance struct {
	Theme        string `toml:"theme"`
	Compact      bool   `toml:"compact"`
	BorderStyle  string `toml:"border_style"`
	RelativeTime bool   `toml:"relative_time"`
	Icons        bool   `toml:"icons"`
	Animations   bool   `toml:"animations"`
}

type Layout struct {
	DefaultScreen string `toml:"default_screen"`
}

type Diff struct {
	ContextLines   int    `toml:"context_lines"`
	LineNumbers    bool   `toml:"line_numbers"`
	Mode           string `toml:"mode"`
	Syntax         bool   `toml:"syntax"`
	WordHighlight  bool   `toml:"word_highlight"`
	ShowWhitespace bool   `toml:"show_whitespace"`
	Whitespace     string `toml:"whitespace"`
	Collapse       bool   `toml:"collapse"`
}

type Editor struct {
	SubjectGuide   int    `toml:"subject_guide"`
	BodyGuide      int    `toml:"body_guide"`
	ExternalEditor string `toml:"external_editor"`
}

type Behavior struct {
	Mouse                 bool `toml:"mouse"`
	AutoRefresh           bool `toml:"auto_refresh"`
	RestoreLastRepository bool `toml:"restore_last_repository"`
	RecentRepositoryCount int  `toml:"recent_repository_count"`
}

type Git struct {
	HistoryPageSize    int  `toml:"history_page_size"`
	ShowRemoteBranches bool `toml:"show_remote_branches"`
	ShowTags           bool `toml:"show_tags"`
}

type Remote struct {
	VerboseProgress          bool `toml:"verbose_progress"`
	OperationOutputRetention int  `toml:"operation_output_retention"`
}

type AI struct {
	Enabled  bool   `toml:"enabled"`
	Provider string `toml:"provider"`
	Model    string `toml:"model"`
	Privacy  string `toml:"privacy"`
}

// Default returns a fully populated configuration. The zero value of Config is
// not a valid configuration; always start from Default.
func Default() *Config {
	return &Config{
		Version: CurrentVersion,
		Appearance: Appearance{
			Theme:        "catppuccin-mocha",
			Compact:      true,
			BorderStyle:  "round",
			RelativeTime: true,
			Icons:        true,
			Animations:   true,
		},
		Layout: Layout{DefaultScreen: "status"},
		Diff: Diff{
			ContextLines: 3, LineNumbers: true,
			Mode: "unified", Syntax: true, WordHighlight: true,
			ShowWhitespace: false, Whitespace: "normal", Collapse: true,
		},
		Editor: Editor{SubjectGuide: 50, BodyGuide: 72},
		Behavior: Behavior{
			RestoreLastRepository: true,
			RecentRepositoryCount: 10,
		},
		Git:         Git{HistoryPageSize: 120, ShowRemoteBranches: true, ShowTags: true},
		Remote:      Remote{OperationOutputRetention: 2000},
		Keybindings: map[string]string{},
		AI:          AI{Provider: "openai-compatible", Privacy: "ask"},
	}
}

// clone copies a config, including its map, so callers can mutate a copy.
func (c *Config) clone() *Config {
	clone := *c
	clone.Keybindings = make(map[string]string, len(c.Keybindings))
	for k, v := range c.Keybindings {
		clone.Keybindings[k] = v
	}
	return &clone
}

// Report describes how a load went: where the file was, what version it
// claimed, and any warnings or errors. Warnings never stop the load; errors do.
type Report struct {
	Path         string
	Version      int
	UsedDefaults bool
	Warnings     []string
	Errors       []string
}

// HasErrors reports whether the load produced validation or parse errors.
func (r Report) HasErrors() bool { return len(r.Errors) > 0 }

// LoadError carries a config problem with the file path attached.
type LoadError struct {
	Path string
	Err  error
}

func (e *LoadError) Error() string { return fmt.Sprintf("%s: %v", e.Path, e.Err) }
func (e *LoadError) Unwrap() error { return e.Err }

// Load reads one config file over the defaults. A missing path or file is not
// an error. A malformed or invalid file returns the defaults with an error, so
// the caller can keep running on something safe.
func Load(path string) (*Config, Report, error) {
	cfg := Default()
	report := Report{Path: path, Version: CurrentVersion, UsedDefaults: true}
	if path == "" {
		return cfg, report, nil
	}
	tree, err := readTree(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, report, nil
		}
		report.Errors = append(report.Errors, err.Error())
		return cfg, report, &LoadError{Path: path, Err: err}
	}
	report.UsedDefaults = false
	if errs := applyTree(cfg, tree, &report); len(errs) > 0 {
		report.Errors = append(report.Errors, errs...)
		return Default(), report, &LoadError{Path: path, Err: fmt.Errorf("%s", errs[0])}
	}
	if errs := Validate(cfg); len(errs) > 0 {
		report.Errors = append(report.Errors, errs...)
		return Default(), report, &LoadError{Path: path, Err: fmt.Errorf("%s", errs[0])}
	}
	report.Warnings = append(report.Warnings, unknownKeys(tree, cfg)...)
	return cfg, report, nil
}

// applyTree checks the version, unmarshals present values onto cfg and applies
// migrations. It returns any errors rather than failing outright.
func applyTree(cfg *Config, tree *toml.Tree, report *Report) []string {
	version := CurrentVersion
	if raw, ok := tree.GetPath([]string{"version"}).(int64); ok {
		version = int(raw)
	}
	report.Version = version
	if version > CurrentVersion {
		return []string{fmt.Sprintf("config version %d is newer than this build supports (version %d); using defaults", version, CurrentVersion)}
	}
	if version < 1 {
		version = CurrentVersion
	}
	if err := tree.Unmarshal(cfg); err != nil {
		return []string{err.Error()}
	}
	cfg.Version = CurrentVersion
	if err := migrate(cfg, version); err != nil {
		return []string{err.Error()}
	}
	return nil
}

func readTree(path string) (*toml.Tree, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	tree, err := toml.LoadBytes(data)
	if err != nil {
		return nil, err
	}
	return tree, nil
}

// migrate applies schema migrations in order. Version 1 has none, but the seam
// exists so later versions add a single entry each.
func migrate(cfg *Config, from int) error {
	for v := from + 1; v <= CurrentVersion; v++ {
		fn, ok := migrations[v]
		if !ok {
			continue
		}
		if err := fn(cfg); err != nil {
			return fmt.Errorf("migrating config to version %d: %w", v, err)
		}
	}
	return nil
}

// migrations maps a target version to the change that reaches it. A later
// milestone adds one function per version; nothing else needs to change.
var migrations = map[int]func(*Config) error{}

// Validate checks enums and numeric ranges. It returns every problem it finds,
// not just the first, so a hand-edited file can be fixed in one pass.
func Validate(cfg *Config) []string {
	var errs []string
	if strings.TrimSpace(cfg.Appearance.Theme) == "" {
		errs = append(errs, "appearance.theme cannot be empty")
	}
	if cfg.Appearance.BorderStyle != "round" && cfg.Appearance.BorderStyle != "square" {
		errs = append(errs, fmt.Sprintf("appearance.border_style %q is not valid; use \"round\" or \"square\"", cfg.Appearance.BorderStyle))
	}
	if !containsString(screenNames, cfg.Layout.DefaultScreen) {
		errs = append(errs, fmt.Sprintf("layout.default_screen %q is not a known screen", cfg.Layout.DefaultScreen))
	}
	if cfg.Diff.ContextLines < 0 || cfg.Diff.ContextLines > 50 {
		errs = append(errs, fmt.Sprintf("diff.context_lines %d is out of range; use 0-50", cfg.Diff.ContextLines))
	}
	if cfg.Diff.Mode != "unified" && cfg.Diff.Mode != "split" {
		errs = append(errs, fmt.Sprintf("diff.mode %q is not valid; use \"unified\" or \"split\"", cfg.Diff.Mode))
	}
	switch cfg.Diff.Whitespace {
	case "normal", "ignore-trailing", "ignore-change", "ignore-all":
	default:
		errs = append(errs, fmt.Sprintf("diff.whitespace %q is not valid; use normal, ignore-trailing, ignore-change or ignore-all", cfg.Diff.Whitespace))
	}
	if cfg.Editor.SubjectGuide < 10 || cfg.Editor.SubjectGuide > 200 {
		errs = append(errs, fmt.Sprintf("editor.subject_guide %d is out of range; use 10-200", cfg.Editor.SubjectGuide))
	}
	if cfg.Editor.BodyGuide < 10 || cfg.Editor.BodyGuide > 300 {
		errs = append(errs, fmt.Sprintf("editor.body_guide %d is out of range; use 10-300", cfg.Editor.BodyGuide))
	}
	if cfg.Behavior.RecentRepositoryCount < 0 || cfg.Behavior.RecentRepositoryCount > 50 {
		errs = append(errs, fmt.Sprintf("behavior.recent_repository_count %d is out of range; use 0-50", cfg.Behavior.RecentRepositoryCount))
	}
	if cfg.Git.HistoryPageSize < 20 || cfg.Git.HistoryPageSize > 2000 {
		errs = append(errs, fmt.Sprintf("git.history_page_size %d is out of range; use 20-2000", cfg.Git.HistoryPageSize))
	}
	if cfg.Remote.OperationOutputRetention < 100 || cfg.Remote.OperationOutputRetention > 100000 {
		errs = append(errs, fmt.Sprintf("remote.operation_output_retention %d is out of range; use 100-100000", cfg.Remote.OperationOutputRetention))
	}
	for action, key := range cfg.Keybindings {
		if strings.TrimSpace(action) == "" {
			errs = append(errs, "keybindings contains an empty action id")
			continue
		}
		if err := validateKey(key); err != nil {
			errs = append(errs, fmt.Sprintf("keybindings.%s: %v", action, err))
		}
	}
	return errs
}

// screenNames is the set layout.default_screen accepts.
var screenNames = []string{"status", "history", "branches", "stash", "remotes", "conflicts", "reflog", "settings"}

// validateKey accepts the key notation Bubble Tea reports: a single printable
// rune, or a dash-joined list of modifiers and a base key.
func validateKey(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("a key cannot be empty")
	}
	if strings.ContainsAny(key, " \t\n") || len(key) > 32 {
		return fmt.Errorf("%q is not a valid key", key)
	}
	return nil
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// unknownKeys walks the parsed tree and warns about keys the schema does not
// know, suggesting the closest known path. It never fails a load.
func unknownKeys(tree *toml.Tree, cfg *Config) []string {
	known := knownPaths(cfg)
	var warnings []string
	var walk func(prefix string, node *toml.Tree)
	walk = func(prefix string, node *toml.Tree) {
		for _, key := range node.Keys() {
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			if child, ok := node.GetPath([]string{key}).(*toml.Tree); ok {
				if _, isKnown := known[path]; !isKnown && !hasKnownPrefix(known, path) {
					warnings = append(warnings, fmt.Sprintf("Unknown setting: %s%s", path, suggest(path, known)))
					continue
				}
				walk(path, child)
				continue
			}
			if !known[path] {
				warnings = append(warnings, fmt.Sprintf("Unknown setting: %s%s", path, suggest(path, known)))
			}
		}
	}
	walk("", tree)
	sort.Strings(warnings)
	return warnings
}

func hasKnownPrefix(known map[string]bool, path string) bool {
	for k := range known {
		if strings.HasPrefix(k, path+".") {
			return true
		}
	}
	return false
}

// suggest offers a nearest known path for a typo, when one is close.
func suggest(path string, known map[string]bool) string {
	best, bestDist := "", 4
	for k := range known {
		if d := levenshtein(path, k); d < bestDist {
			best, bestDist = k, d
		}
	}
	if best == "" {
		return ""
	}
	return fmt.Sprintf(" (did you mean %s?)", best)
}

func levenshtein(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// knownPaths is every key path the schema understands, including the reserved
// [ai] section and the dynamic keybindings entries.
func knownPaths(cfg *Config) map[string]bool {
	paths := map[string]bool{}
	for _, s := range Settings() {
		paths[s.Path] = true
	}
	for _, p := range []string{
		"version", "appearance", "layout", "diff", "editor", "behavior",
		"git", "remote", "keybindings",
		"ai", "ai.enabled", "ai.provider", "ai.model", "ai.privacy",
	} {
		paths[p] = true
	}
	for action := range cfg.Keybindings {
		paths["keybindings."+action] = true
	}
	return paths
}

// kv is one flattened config value.
type kv struct {
	path  []string
	value any
}

// flatten produces every settable path and value, in a stable order.
func flatten(cfg *Config) []kv {
	pairs := []kv{
		{[]string{"appearance", "theme"}, cfg.Appearance.Theme},
		{[]string{"appearance", "compact"}, cfg.Appearance.Compact},
		{[]string{"appearance", "border_style"}, cfg.Appearance.BorderStyle},
		{[]string{"appearance", "relative_time"}, cfg.Appearance.RelativeTime},
		{[]string{"appearance", "icons"}, cfg.Appearance.Icons},
		{[]string{"appearance", "animations"}, cfg.Appearance.Animations},
		{[]string{"layout", "default_screen"}, cfg.Layout.DefaultScreen},
		{[]string{"diff", "context_lines"}, int64(cfg.Diff.ContextLines)},
		{[]string{"diff", "line_numbers"}, cfg.Diff.LineNumbers},
		{[]string{"diff", "mode"}, cfg.Diff.Mode},
		{[]string{"diff", "syntax"}, cfg.Diff.Syntax},
		{[]string{"diff", "word_highlight"}, cfg.Diff.WordHighlight},
		{[]string{"diff", "show_whitespace"}, cfg.Diff.ShowWhitespace},
		{[]string{"diff", "whitespace"}, cfg.Diff.Whitespace},
		{[]string{"diff", "collapse"}, cfg.Diff.Collapse},
		{[]string{"editor", "subject_guide"}, int64(cfg.Editor.SubjectGuide)},
		{[]string{"editor", "body_guide"}, int64(cfg.Editor.BodyGuide)},
		{[]string{"editor", "external_editor"}, cfg.Editor.ExternalEditor},
		{[]string{"behavior", "mouse"}, cfg.Behavior.Mouse},
		{[]string{"behavior", "auto_refresh"}, cfg.Behavior.AutoRefresh},
		{[]string{"behavior", "restore_last_repository"}, cfg.Behavior.RestoreLastRepository},
		{[]string{"behavior", "recent_repository_count"}, int64(cfg.Behavior.RecentRepositoryCount)},
		{[]string{"git", "history_page_size"}, int64(cfg.Git.HistoryPageSize)},
		{[]string{"git", "show_remote_branches"}, cfg.Git.ShowRemoteBranches},
		{[]string{"git", "show_tags"}, cfg.Git.ShowTags},
		{[]string{"remote", "verbose_progress"}, cfg.Remote.VerboseProgress},
		{[]string{"remote", "operation_output_retention"}, int64(cfg.Remote.OperationOutputRetention)},
		{[]string{"ai", "enabled"}, cfg.AI.Enabled},
		{[]string{"ai", "provider"}, cfg.AI.Provider},
		{[]string{"ai", "model"}, cfg.AI.Model},
		{[]string{"ai", "privacy"}, cfg.AI.Privacy},
	}
	keys := make([]string, 0, len(cfg.Keybindings))
	for action := range cfg.Keybindings {
		keys = append(keys, action)
	}
	sort.Strings(keys)
	for _, action := range keys {
		pairs = append(pairs, kv{[]string{"keybindings", action}, cfg.Keybindings[action]})
	}
	return pairs
}

// Effective returns the resolved configuration as dotted paths, for the
// "show effective configuration" view.
func Effective(cfg *Config) [][2]string {
	pairs := flatten(cfg)
	out := make([][2]string, 0, len(pairs)+1)
	out = append(out, [2]string{"version", strconv.Itoa(cfg.Version)})
	for _, p := range pairs {
		out = append(out, [2]string{strings.Join(p.path, "."), fmt.Sprint(p.value)})
	}
	return out
}

// NewTree returns an empty TOML tree.
func NewTree() *toml.Tree {
	tree, _ := toml.LoadBytes([]byte{})
	return tree
}

// writeAtomic writes to a temp file in the same directory, fsyncs it, then
// renames it over the target, so a crash cannot leave a half-written file.
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tidegit-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// ValueString renders a setting value for display.
func ValueString(v any) string { return fmt.Sprint(v) }
