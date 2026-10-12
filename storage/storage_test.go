package storage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

// A session whose ID carries no usable date cannot be filed under a year or a
// month. It used to be dropped silently on save; it must survive a round-trip.
func TestSplitSaveKeepsSessionsWithUnusableID(t *testing.T) {
	for _, mode := range []string{models.SplitYear, models.SplitMonth} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			cal := &models.Calendar{Sessions: []models.Session{
				{ID: "20260101120000", Time: "12:00", Name: "обычная"},
				{ID: "xx", Time: "09:00", Name: "битый ID"},
			}}
			if err := Save(context.Background(), dir, "mycal", cal, mode); err != nil {
				t.Fatalf("save: %v", err)
			}
			got, _, err := Load(context.Background(), dir, "mycal", mode)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if len(got.Sessions) != 2 {
				t.Fatalf("round-trip lost data: %d of 2 sessions survived (%+v)", len(got.Sessions), got.Sessions)
			}
		})
	}
}

// Once the bad records are gone the bucket file must not linger, or its stale
// contents would reappear on the next load.
func TestSplitSaveRemovesEmptyOrphanBucket(t *testing.T) {
	dir := t.TempDir()
	withOrphan := &models.Calendar{Sessions: []models.Session{
		{ID: "20260101120000", Time: "12:00", Name: "обычная"},
		{ID: "xx", Time: "09:00", Name: "битый ID"},
	}}
	if err := Save(context.Background(), dir, "mycal", withOrphan, models.SplitYear); err != nil {
		t.Fatalf("save: %v", err)
	}
	clean := &models.Calendar{Sessions: withOrphan.Sessions[:1]}
	if err := Save(context.Background(), dir, "mycal", clean, models.SplitYear); err != nil {
		t.Fatalf("second save: %v", err)
	}
	got, _, err := Load(context.Background(), dir, "mycal", models.SplitYear)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got.Sessions) != 1 {
		t.Fatalf("stale bucket resurrected data: got %d sessions, want 1 (%+v)", len(got.Sessions), got.Sessions)
	}
}

func TestMissingMainFileRestoredFromBackup(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	if err := Save(ctx, dir, "mycal", sample(), models.SplitNone); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := Save(ctx, dir, "mycal", sample(), models.SplitNone); err != nil {
		t.Fatalf("Save 2: %v", err)
	}
	path := filepath.Join(dir, "mycal.json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	got, warns, err := Load(ctx, dir, "mycal", models.SplitNone)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Sessions) != 3 {
		t.Errorf("backup not used: %d sessions", len(got.Sessions))
	}
	if len(warns) == 0 || !strings.Contains(warns[0], "отсутствовал и был восстановлен") {
		t.Errorf("expected warning about missing file restored, got: %v", warns)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("main file was not recreated on disk: %v", err)
	}
}

func TestEmptyMainFileRestoredFromBackup(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	if err := Save(ctx, dir, "mycal", sample(), models.SplitNone); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := Save(ctx, dir, "mycal", sample(), models.SplitNone); err != nil {
		t.Fatalf("Save 2: %v", err)
	}
	path := filepath.Join(dir, "mycal.json")
	if err := os.WriteFile(path, []byte("   \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, warns, err := Load(ctx, dir, "mycal", models.SplitNone)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Sessions) != 3 {
		t.Errorf("backup not used: %d sessions", len(got.Sessions))
	}
	if len(warns) == 0 || !strings.Contains(warns[0], "был пуст и восстановлен") {
		t.Errorf("expected warning about empty file restored, got: %v", warns)
	}
}

func TestEmptyMainFileWithoutBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mycal.json")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	got, warns, err := Load(context.Background(), dir, "mycal", models.SplitNone)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Sessions) != 0 {
		t.Errorf("expected empty calendar, got %d", len(got.Sessions))
	}
	if len(warns) == 0 || !strings.Contains(warns[0], "пуст") {
		t.Errorf("expected warning about empty file, got %v", warns)
	}
}

func TestCorruptMainFilePreservedWhenRestoredFromBackup(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	if err := Save(ctx, dir, "mycal", sample(), models.SplitNone); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := Save(ctx, dir, "mycal", sample(), models.SplitNone); err != nil {
		t.Fatalf("Save 2: %v", err)
	}
	path := filepath.Join(dir, "mycal.json")
	corruptContent := "{ this is corrupted json"
	if err := os.WriteFile(path, []byte(corruptContent), 0o644); err != nil {
		t.Fatal(err)
	}
	got, warns, err := Load(ctx, dir, "mycal", models.SplitNone)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Sessions) != 3 {
		t.Errorf("backup not used: %d sessions", len(got.Sessions))
	}
	if len(warns) == 0 || !strings.Contains(warns[0], "был повреждён и восстановлен") {
		t.Errorf("expected warning about corrupt file restored, got: %v", warns)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "mycal.json.corrupt-*"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("expected 1 corrupt file, got %v", matches)
	}
	savedCorrupt, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read corrupt file: %v", err)
	}
	if string(savedCorrupt) != corruptContent {
		t.Errorf("corrupt content not preserved: got %q, want %q", string(savedCorrupt), corruptContent)
	}
}

func TestSplitModeMissingFileRestoredFromBackup(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	if err := Save(ctx, dir, "mycal", sample(), models.SplitYear); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := Save(ctx, dir, "mycal", sample(), models.SplitYear); err != nil {
		t.Fatalf("Save 2: %v", err)
	}
	// Delete 2025_mycal.json
	missing := filepath.Join(dir, "2025_mycal.json")
	if err := os.Remove(missing); err != nil {
		t.Fatal(err)
	}
	got, warns, err := Load(ctx, dir, "mycal", models.SplitYear)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Sessions) != 3 {
		t.Fatalf("expected 3 sessions restored, got %d", len(got.Sessions))
	}
	if len(warns) == 0 || !strings.Contains(warns[0], "отсутствовал и был восстановлен") {
		t.Errorf("expected warning, got %v", warns)
	}
	if _, err := os.Stat(missing); err != nil {
		t.Errorf("missing year file was not recreated on disk: %v", err)
	}
}
