package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mycalendar/models"
)

func sample() *models.Calendar {
	return &models.Calendar{Entries: []models.DateEntry{
		{Date: "2025-01-10", Sessions: []models.Session{{ID: "20250110090000", Time: "09:00", Name: "A", Duration: 30}}},
		{Date: "2025-03-14", Sessions: []models.Session{{ID: "20250314120000", Time: "12:00", Name: "B", Duration: 60}}},
		{Date: "2026-02-02", Sessions: []models.Session{{ID: "20260202080000", Time: "08:00", Name: "C", Duration: 45}}},
	}}
}

func countSessions(cal *models.Calendar) int {
	n := 0
	for _, de := range cal.Entries {
		n += len(de.Sessions)
	}
	return n
}

func TestSaveLoadRoundTrip(t *testing.T) {
	for _, mode := range []string{models.SplitNone, models.SplitYear, models.SplitMonth} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			ctx := context.Background()
			if err := Save(ctx, dir, "mycal", sample(), mode); err != nil {
				t.Fatalf("Save: %v", err)
			}
			got, err := Load(ctx, dir, "mycal", mode)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if countSessions(got) != 3 {
				t.Fatalf("expected 3 sessions, got %d", countSessions(got))
			}
			if got.Entries[0].Date != "2025-01-10" {
				t.Errorf("entries not sorted by date: %s first", got.Entries[0].Date)
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
	monthFiles, _ := filepath.Glob(filepath.Join(dir, "????-??_mycal.json"))
	if len(monthFiles) == 0 {
		t.Fatal("expected month files to be written")
	}

	if err := Save(ctx, dir, "mycal", sample(), models.SplitNone); err != nil {
		t.Fatalf("Save none: %v", err)
	}
	if leftover, _ := filepath.Glob(filepath.Join(dir, "????-??_mycal.json")); len(leftover) != 0 {
		t.Errorf("month files not cleaned up after switching to none: %v", leftover)
	}
	if _, err := os.Stat(filepath.Join(dir, "mycal.json")); err != nil {
		t.Errorf("single file missing after migration: %v", err)
	}

	got, err := Load(ctx, dir, "mycal", models.SplitNone)
	if err != nil || countSessions(got) != 3 {
		t.Fatalf("data lost across migration: err=%v count=%d", err, countSessions(got))
	}
}

func TestLoadRestoresFromBackupOnCorruption(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	if err := Save(ctx, dir, "mycal", sample(), models.SplitNone); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// Second save creates mycal.json.bak from the first save's contents.
	if err := Save(ctx, dir, "mycal", sample(), models.SplitNone); err != nil {
		t.Fatalf("Save 2: %v", err)
	}

	path := filepath.Join(dir, "mycal.json")
	if err := os.WriteFile(path, []byte("{ this is not json"), 0o644); err != nil {
		t.Fatalf("corrupt: %v", err)
	}

	got, err := Load(ctx, dir, "mycal", models.SplitNone)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if countSessions(got) != 3 {
		t.Errorf("expected backup to restore 3 sessions, got %d", countSessions(got))
	}
}

func TestLoadMissingReturnsEmpty(t *testing.T) {
	got, err := Load(context.Background(), t.TempDir(), "mycal", models.SplitNone)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if countSessions(got) != 0 {
		t.Errorf("expected empty calendar, got %d sessions", countSessions(got))
	}
}
