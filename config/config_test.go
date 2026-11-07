package config

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"mycalendar/models"
)

func TestLoadMissingReturnsDefaults(t *testing.T) {
	cfg, err := Load(context.Background(), filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DefaultDuration != 60 || cfg.SplitMode != models.SplitNone || cfg.DataFileName != "mycal" || !cfg.UseSystemDate || cfg.DefaultMode != models.ModeCLI || cfg.WebPort != 8080 {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadOverlaysDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.json")
	// Only two keys present; the rest must fall back to defaults, and an
	// invalid duration must be repaired.
	if err := os.WriteFile(path, []byte(`{"default_type":"Йога","default_duration":0}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(context.Background(), path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DefaultType != "Йога" {
		t.Errorf("default_type not read: %q", cfg.DefaultType)
	}
	if cfg.DefaultDuration != 60 {
		t.Errorf("invalid duration not repaired: %d", cfg.DefaultDuration)
	}
	if cfg.SplitMode != models.SplitNone || cfg.DataFileName != "mycal" || !cfg.UseSystemDate || cfg.DefaultMode != models.ModeCLI || cfg.WebPort != 8080 {
		t.Errorf("missing keys not defaulted: %+v", cfg)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ConfigFile)
	want := DefaultConfig()
	want.DefaultType = "Бег"
	want.CustomNames = []string{"a", "b"}
	if err := Save(context.Background(), path, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(context.Background(), path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.DefaultType != "Бег" || len(got.CustomNames) != 2 {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	// Saved file must be valid JSON.
	raw, _ := os.ReadFile(path)
	if !json.Valid(raw) {
		t.Errorf("saved config is not valid JSON")
	}
}

func TestLoadRewritesOldConfigAndPreservesKnownFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ConfigFile)

	// Simulate an old config file that lacks new fields (e.g. tui_default_period, setting_modes)
	// and has an obsolete/unknown field.
	oldJSON := `{"default_type":"Стрельба из лука","default_duration":45,"web_port":9090,"legacy_unknown_field":"ignored"}`
	if err := os.WriteFile(path, []byte(oldJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(context.Background(), path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DefaultType != "Стрельба из лука" || cfg.DefaultDuration != 45 || cfg.WebPort != 9090 {
		t.Fatalf("known fields not preserved: %+v", cfg)
	}
	if cfg.TUIDefaultPeriod != "today" {
		t.Fatalf("new field TUIDefaultPeriod not initialized to 'today': %q", cfg.TUIDefaultPeriod)
	}
	if len(cfg.SettingModes) == 0 {
		t.Fatalf("expected SettingModes to be populated")
	}

	// Verify that the file on disk was replaced with the new complete schema
	newBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile after Load migration: %v", err)
	}
	var diskMap map[string]interface{}
	if err := json.Unmarshal(newBytes, &diskMap); err != nil {
		t.Fatalf("Unmarshal migrated config: %v", err)
	}
	if _, hasLegacy := diskMap["legacy_unknown_field"]; hasLegacy {
		t.Errorf("expected legacy_unknown_field to be removed in rewritten config")
	}
	if _, hasModes := diskMap["setting_modes"]; !hasModes {
		t.Errorf("expected setting_modes in rewritten config")
	}
	if _, hasTUIPeriod := diskMap["tui_default_period"]; !hasTUIPeriod {
		t.Errorf("expected tui_default_period in rewritten config")
	}
}

func TestSettingModesFiltering(t *testing.T) {
	cfg := DefaultConfig()

	// date_check_mode -> cli, tui (NOT web)
	if !cfg.IsSettingForMode("date_check_mode", models.ModeCLI) || !cfg.IsSettingForMode("date_check_mode", models.ModeTUI) {
		t.Errorf("expected date_check_mode enabled for CLI and TUI")
	}
	if cfg.IsSettingForMode("date_check_mode", models.ModeWeb) {
		t.Errorf("expected date_check_mode disabled for Web")
	}

	// use_system_date, custom_date, web_port -> cli, tui, web
	for _, key := range []string{"use_system_date", "custom_date", "web_port"} {
		for _, mode := range []string{models.ModeCLI, models.ModeTUI, models.ModeWeb} {
			if !cfg.IsSettingForMode(key, mode) {
				t.Errorf("expected %s enabled for mode %s", key, mode)
			}
		}
	}

	// tui_default_period -> tui only
	if !cfg.IsSettingForMode("tui_default_period", models.ModeTUI) {
		t.Errorf("expected tui_default_period enabled for TUI")
	}
	if cfg.IsSettingForMode("tui_default_period", models.ModeCLI) || cfg.IsSettingForMode("tui_default_period", models.ModeWeb) {
		t.Errorf("expected tui_default_period disabled for CLI and Web")
	}

	// show_hints, silent_add_names -> cli only
	for _, key := range []string{"show_hints", "silent_add_names"} {
		if !cfg.IsSettingForMode(key, models.ModeCLI) {
			t.Errorf("expected %s enabled for CLI", key)
		}
		if cfg.IsSettingForMode(key, models.ModeTUI) || cfg.IsSettingForMode(key, models.ModeWeb) {
			t.Errorf("expected %s disabled for TUI and Web", key)
		}
	}
}
