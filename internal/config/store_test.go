package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func storePaths(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "config.toml"), filepath.Join(dir, "overrides.toml")
}

func TestStorePersistsOverridesSeparately(t *testing.T) {
	primary, overrides := storePaths(t)
	if err := os.WriteFile(primary, []byte("# keep me\nversion = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := Open(primary, overrides)
	if store.Config().Appearance.Theme != "catppuccin-mocha" {
		t.Fatalf("defaults: %+v", store.Config())
	}
	if err := store.Set("appearance.theme", "nord"); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("diff.context_lines", 8); err != nil {
		t.Fatal(err)
	}
	if got := store.Config(); got.Appearance.Theme != "nord" || got.Diff.ContextLines != 8 {
		t.Fatalf("effective config: %+v", got)
	}
	if !store.IsOverridden("appearance.theme") || store.Source("appearance.theme") != "overrides.toml" {
		t.Fatalf("override tracking: %+v", store.Overrides())
	}
	if store.Source("appearance.compact") != "default" {
		t.Fatalf("untouched setting source: %q", store.Source("appearance.compact"))
	}

	// config.toml is never rewritten, so its comment survives.
	data, err := os.ReadFile(primary)
	if err != nil || !strings.Contains(string(data), "# keep me") {
		t.Fatalf("config.toml was disturbed: %q %v", data, err)
	}
	// The app-managed file holds only the changes.
	raw, err := os.ReadFile(overrides)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "nord") || strings.Contains(string(raw), "subject_guide") {
		t.Fatalf("overrides file should be minimal: %q", raw)
	}

	// A fresh store on the same paths sees the persisted overrides.
	again := Open(primary, overrides)
	if got := again.Config(); got.Appearance.Theme != "nord" || got.Diff.ContextLines != 8 {
		t.Fatalf("overrides did not persist: %+v", got)
	}
}

func TestStoreResetAndResetAll(t *testing.T) {
	primary, overrides := storePaths(t)
	store := Open(primary, overrides)
	if err := store.Set("appearance.theme", "nord"); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("git.show_tags", false); err != nil {
		t.Fatal(err)
	}
	if err := store.Reset("appearance.theme"); err != nil {
		t.Fatal(err)
	}
	if store.Config().Appearance.Theme != "catppuccin-mocha" || store.IsOverridden("appearance.theme") {
		t.Fatalf("reset did not restore the default: %+v", store.Config())
	}
	if err := store.ResetAll(); err != nil {
		t.Fatal(err)
	}
	if len(store.Overrides()) != 0 || !store.Config().Git.ShowTags {
		t.Fatalf("reset all should return to defaults: %+v", store.Overrides())
	}
}

func TestStoreConfigLayerAndPrecedence(t *testing.T) {
	primary, overrides := storePaths(t)
	if err := os.WriteFile(primary, []byte("version = 1\n[appearance]\ntheme = \"gruvbox-dark\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := Open(primary, overrides)
	if store.Source("appearance.theme") != "config.toml" {
		t.Fatalf("config layer not recognised: %q", store.Source("appearance.theme"))
	}
	if err := store.Set("appearance.theme", "dracula"); err != nil {
		t.Fatal(err)
	}
	if store.Config().Appearance.Theme != "dracula" {
		t.Fatal("in-app override should win over config.toml")
	}
	if err := store.Reset("appearance.theme"); err != nil {
		t.Fatal(err)
	}
	if store.Config().Appearance.Theme != "gruvbox-dark" {
		t.Fatal("resetting the override should fall back to config.toml")
	}
}

func TestStoreKeybindingOverrides(t *testing.T) {
	primary, overrides := storePaths(t)
	store := Open(primary, overrides)
	if err := store.Set("keybindings.app.quit", "ctrl+q"); err != nil {
		t.Fatal(err)
	}
	if got := store.Config().Keybindings["app.quit"]; got != "ctrl+q" {
		t.Fatalf("keybinding override: %q", got)
	}
	again := Open(primary, overrides)
	if again.Config().Keybindings["app.quit"] != "ctrl+q" {
		t.Fatal("keybinding override did not persist")
	}
	if err := again.Reset("keybindings.app.quit"); err != nil {
		t.Fatal(err)
	}
	if _, ok := again.Config().Keybindings["app.quit"]; ok {
		t.Fatal("keybinding reset did not clear")
	}
}

func TestStoreRejectsInvalidValue(t *testing.T) {
	primary, overrides := storePaths(t)
	store := Open(primary, overrides)
	if err := store.Set("diff.context_lines", 999); err == nil {
		t.Fatal("out-of-range value was accepted")
	}
	if store.IsOverridden("diff.context_lines") {
		t.Fatal("invalid value reached the overrides layer")
	}
}

func TestStoreToleratesCorruptOverrides(t *testing.T) {
	primary, overrides := storePaths(t)
	if err := os.WriteFile(overrides, []byte("[appearance\ntheme = "), 0o600); err != nil {
		t.Fatal(err)
	}
	store := Open(primary, overrides)
	if store.Config().Appearance.Theme != "catppuccin-mocha" {
		t.Fatalf("corrupt overrides should fall back to defaults: %+v", store.Config())
	}
	if len(store.Errors()) == 0 {
		t.Fatal("corrupt overrides not reported")
	}
	// The store remains usable and can overwrite the corrupt file.
	if err := store.Set("appearance.theme", "nord"); err != nil {
		t.Fatal(err)
	}
	if got := Open(primary, overrides).Config().Appearance.Theme; got != "nord" {
		t.Fatalf("overrides not recoverable: %q", got)
	}
}

func TestStoreReloadPicksUpManualEdits(t *testing.T) {
	primary, overrides := storePaths(t)
	if err := os.WriteFile(primary, []byte("version = 1\n[appearance]\ntheme = \"nord\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := Open(primary, overrides)
	if store.Config().Appearance.Theme != "nord" {
		t.Fatal("initial load")
	}
	if err := os.WriteFile(primary, []byte("version = 1\n[appearance]\ntheme = \"dracula\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Reload(); err != nil {
		t.Fatal(err)
	}
	if store.Config().Appearance.Theme != "dracula" {
		t.Fatalf("reload did not apply edits: %+v", store.Config())
	}
}

func TestStoreKeepsLastValidOnBadReload(t *testing.T) {
	primary, overrides := storePaths(t)
	if err := os.WriteFile(primary, []byte("version = 1\n[appearance]\ntheme = \"nord\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := Open(primary, overrides)
	if err := os.WriteFile(primary, []byte("version = 1\n[appearance\ntheme = "), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Reload(); err == nil {
		t.Fatal("bad reload did not report an error")
	}
	if store.Config().Appearance.Theme != "nord" {
		t.Fatalf("bad reload replaced the valid config: %+v", store.Config())
	}
}
