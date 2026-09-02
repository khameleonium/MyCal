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
	if cfg.DefaultDuration != 60 || cfg.SplitMode != models.SplitNone || cfg.DataFileName != "mycal" || !cfg.UseSystemDate {
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
	if cfg.SplitMode != models.SplitNone || cfg.DataFileName != "mycal" || !cfg.UseSystemDate {
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
