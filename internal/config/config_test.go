package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPathsFollowXDG(t *testing.T) {
	configHome := t.TempDir()
	stateHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_STATE_HOME", stateHome)
	if got := Dir(); got != filepath.Join(configHome, App) {
		t.Fatalf("config dir: %q", got)
	}
	if got := Path(); got != filepath.Join(configHome, App, "config.toml") {
		t.Fatalf("config path: %q", got)
	}
	if got := StateDir(); got != filepath.Join(stateHome, App) {
		t.Fatalf("state dir: %q", got)
	}
	if got := StatePath(); got != filepath.Join(stateHome, App, "state.toml") {
		t.Fatalf("state path: %q", got)
	}
}

func TestPathsFallBackToHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", home)
	if got := Path(); got != filepath.Join(home, ".config", App, "config.toml") {
		t.Fatalf("fallback config path: %q", got)
	}
	if got := StatePath(); got != filepath.Join(home, ".local", "state", App, "state.toml") {
		t.Fatalf("fallback state path: %q", got)
	}
}

func TestMissingFileLoadsDefaults(t *testing.T) {
	cfg, report, err := Load(filepath.Join(t.TempDir(), "does-not-exist.toml"))
	if err != nil || !report.UsedDefaults {
		t.Fatalf("missing file: %v %+v", err, report)
	}
	if cfg.Version != CurrentVersion || cfg.Appearance.Theme != "catppuccin-mocha" || !cfg.Git.ShowTags {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
}

func TestPartialConfigMergesWithDefaults(t *testing.T) {
	path := writeConfig(t, "version = 1\n[appearance]\ntheme = \"nord\"\n[diff]\ncontext_lines = 7\n")
	cfg, report, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Appearance.Theme != "nord" || cfg.Diff.ContextLines != 7 {
		t.Fatalf("overrides not applied: %+v", cfg)
	}
	// Untouched values keep their defaults.
	if !cfg.Appearance.RelativeTime || cfg.Git.HistoryPageSize != 120 {
		t.Fatalf("defaults lost after merge: %+v", cfg)
	}
	if report.HasErrors() {
		t.Fatalf("unexpected errors: %+v", report)
	}
}

func TestInvalidTOMLIsGraceful(t *testing.T) {
	path := writeConfig(t, "version = 1\n[appearance\ntheme = ")
	cfg, report, err := Load(path)
	if err == nil {
		t.Fatal("malformed TOML did not error")
	}
	if !report.HasErrors() || !report.UsedDefaults {
		t.Fatalf("report: %+v", report)
	}
	if cfg.Appearance.Theme != "catppuccin-mocha" {
		t.Fatalf("did not fall back to defaults: %+v", cfg)
	}
}

func TestValidationRejectsBadValues(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"enum", "version = 1\n[appearance]\nborder_style = \"wavy\"\n", "border_style"},
		{"range", "version = 1\n[diff]\ncontext_lines = 999\n", "context_lines"},
		{"screen", "version = 1\n[layout]\ndefault_screen = \"nowhere\"\n", "default_screen"},
		{"keybinding", "version = 1\n[keybindings]\n\"app.quit\" = \"\"\n", "app.quit"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, report, err := Load(writeConfig(t, tc.body))
			if err == nil || !report.HasErrors() {
				t.Fatalf("expected validation error, got %v %+v", err, report)
			}
			if !strings.Contains(strings.Join(report.Errors, " "), tc.want) {
				t.Fatalf("error %v does not mention %q", report.Errors, tc.want)
			}
			if cfg.Appearance.Theme != "catppuccin-mocha" {
				t.Fatalf("invalid config did not fall back: %+v", cfg)
			}
		})
	}
}

func TestUnknownSettingWarnsWithSuggestion(t *testing.T) {
	path := writeConfig(t, "version = 1\n[appearance]\nanimatons = false\n")
	cfg, report, err := Load(path)
	if err != nil {
		t.Fatalf("unknown key must not fail a load: %v", err)
	}
	if cfg.Appearance.Theme == "" {
		t.Fatal("known settings did not load")
	}
	joined := strings.Join(report.Warnings, " ")
	if !strings.Contains(joined, "appearance.animatons") {
		t.Fatalf("unknown key not reported: %v", report.Warnings)
	}
	if !strings.Contains(joined, "animations") {
		t.Fatalf("no suggestion offered: %v", report.Warnings)
	}
}

func TestFutureVersionIsRefusedGracefully(t *testing.T) {
	path := writeConfig(t, "version = 99\n[appearance]\ntheme = \"nord\"\n")
	cfg, report, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("future version: %v", err)
	}
	if report.Version != 99 || cfg.Appearance.Theme != "catppuccin-mocha" {
		t.Fatalf("future version did not fall back to defaults: %+v %+v", report, cfg)
	}
}

func TestMissingVersionIsCurrent(t *testing.T) {
	path := writeConfig(t, "[appearance]\ntheme = \"nord\"\n")
	cfg, _, err := Load(path)
	if err != nil || cfg.Appearance.Theme != "nord" || cfg.Version != CurrentVersion {
		t.Fatalf("missing version: %v %+v", err, cfg)
	}
}

// The migration seam must exist even though version 1 has no migrations.
func TestMigrationSeam(t *testing.T) {
	if len(migrations) != 0 {
		t.Fatalf("version 1 should have no migrations yet: %v", migrations)
	}
	cfg := Default()
	if err := migrate(cfg, CurrentVersion); err != nil {
		t.Fatalf("no-op migration failed: %v", err)
	}
}

func TestEffectiveListsResolvedValues(t *testing.T) {
	cfg := Default()
	cfg.Keybindings["app.quit"] = "ctrl+q"
	got := map[string]string{}
	for _, pair := range Effective(cfg) {
		got[pair[0]] = pair[1]
	}
	if got["appearance.theme"] != "catppuccin-mocha" || got["keybindings.app.quit"] != "ctrl+q" {
		t.Fatalf("effective config: %+v", got)
	}
}
