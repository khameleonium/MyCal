package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mycalendar/models"
)

func sample() *models.Calendar {
	return &models.Calendar{Sessions: []models.Session{
		{ID: "20250110090000", Time: "09:00", Name: "A", Duration: 30},
		{ID: "20250314120000", Time: "12:00", Name: "B", Duration: 60},
		{ID: "20260202080000", Time: "08:00", Name: "C", Duration: 45},
	}}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	for _, mode := range []string{models.SplitNone, models.SplitYear, models.SplitMonth} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			ctx := context.Background()
			if err := Save(ctx, dir, "mycal", sample(), mode); err != nil {
				t.Fatalf("Save: %v", err)
			}
			got, warns, err := Load(ctx, dir, "mycal", mode)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if len(warns) != 0 {
				t.Errorf("unexpected warnings: %v", warns)
			}
			if len(got.Sessions) != 3 {
				t.Fatalf("expected 3 sessions, got %d", len(got.Sessions))
			}
			if got.Sessions[0].Date() != "2025-01-10" {
				t.Errorf("not sorted: first is %s", got.Sessions[0].Date())
			}
		})
	}
}

func TestModeMigrationRemovesStaleFiles(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	if err := Save(ctx, dir, "mycal", sample(), models.SplitMonth); err != nil {
		t.Fatalf("Save month: %v", err)
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "????-??_mycal.json")); len(m) == 0 {
		t.Fatal("month files not written")
	}

	if err := Save(ctx, dir, "mycal", sample(), models.SplitNone); err != nil {
		t.Fatalf("Save none: %v", err)
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "????-??_mycal.json")); len(m) != 0 {
		t.Errorf("month files left behind: %v", m)
	}
	if _, err := os.Stat(filepath.Join(dir, "mycal.json")); err != nil {
		t.Errorf("single file missing: %v", err)
	}
	got, _, err := Load(ctx, dir, "mycal", models.SplitNone)
	if err != nil || len(got.Sessions) != 3 {
		t.Fatalf("data lost across migration: err=%v n=%d", err, len(got.Sessions))
	}
}

func TestCorruptFileRestoredFromBackup(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	if err := Save(ctx, dir, "mycal", sample(), models.SplitNone); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := Save(ctx, dir, "mycal", sample(), models.SplitNone); err != nil {
		t.Fatalf("Save 2: %v", err) // creates mycal.json.bak
	}
	path := filepath.Join(dir, "mycal.json")
	if err := os.WriteFile(path, []byte("{ broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, warns, err := Load(ctx, dir, "mycal", models.SplitNone)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Sessions) != 3 {
		t.Errorf("backup not used: %d sessions", len(got.Sessions))
	}
	if len(warns) == 0 {
		t.Errorf("expected a warning about the corrupt file")
	}
}

func TestCorruptFileWithoutBackupSetAside(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mycal.json")
	if err := os.WriteFile(path, []byte("not json at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, warns, err := Load(context.Background(), dir, "mycal", models.SplitNone)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Sessions) != 0 {
		t.Errorf("expected empty calendar, got %d", len(got.Sessions))
	}
	if len(warns) == 0 {
		t.Fatal("expected a warning")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("corrupt file should have been moved aside, still at %s", path)
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "mycal.json.corrupt-*")); len(m) != 1 {
		t.Errorf("expected one .corrupt- file, got %v", m)
	}
}

func TestLoadMissingIsEmpty(t *testing.T) {
	got, warns, err := Load(context.Background(), t.TempDir(), "mycal", models.SplitNone)
	if err != nil || len(warns) != 0 || len(got.Sessions) != 0 {
		t.Fatalf("want empty/no-warn/no-err, got n=%d warns=%v err=%v", len(got.Sessions), warns, err)
	}
}

func TestLoadTrimsBOM(t *testing.T) {
	dir := t.TempDir()
	body := append([]byte{0xEF, 0xBB, 0xBF}, []byte(`{"sessions":[{"id":"20250101090000","time":"09:00","name":"x"}]}`)...)
	if err := os.WriteFile(filepath.Join(dir, "mycal.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	got, _, err := Load(context.Background(), dir, "mycal", models.SplitNone)
	if err != nil || len(got.Sessions) != 1 {
		t.Fatalf("BOM not handled: n=%d err=%v", len(got.Sessions), err)
	}
}
