package calendar

import (
	"context"
	"testing"
	"time"

	"mycalendar/models"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()
	svc, err := NewService(context.Background(), dir, "test", models.SplitNone)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc
}

func addTestSession(s *Service, date, tm, name, typ string, duration int) string {
	d, _ := time.Parse("2006-01-02", date)
	t, _ := time.Parse("15:04", tm)
	id := s.GenerateID(d, t)
	s.AddEntry(models.Session{
		ID:       id,
		Time:     tm,
		Name:     name,
		Type:     typ,
		Duration: duration,
	})
	return id
}

func TestAddEntry(t *testing.T) {
	svc := newTestService(t)

	addTestSession(svc, "2025-12-29", "10:51", "Иван Иваныч", "Стрельба из лука", 90)

	if len(svc.data.Entries) != 1 {
		t.Fatalf("expected 1 DateEntry, got %d", len(svc.data.Entries))
	}
	de := svc.data.Entries[0]
	if de.Date != "2025-12-29" {
		t.Errorf("expected date 2025-12-29, got %s", de.Date)
	}
	if len(de.Sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(de.Sessions))
	}
	s := de.Sessions[0]
	if s.Name != "Иван Иваныч" {
		t.Errorf("expected name Иван Иваныч, got %s", s.Name)
	}
	if s.Duration != 90 {
		t.Errorf("expected duration 90, got %d", s.Duration)
	}
}

func TestFindConflicts(t *testing.T) {
	svc := newTestService(t)

	addTestSession(svc, "2025-12-29", "10:51", "Иван Иваныч", "Стрельба из лука", 90)
	addTestSession(svc, "2025-12-29", "10:51", "Пётр Петрович", "Метание ножей", 60)

	conflicts := svc.FindConflicts("2025-12-29", "10:51")
	if len(conflicts) != 2 {
		t.Errorf("expected 2 conflicts, got %d", len(conflicts))
	}

	noConflicts := svc.FindConflicts("2025-12-29", "12:00")
	if len(noConflicts) != 0 {
		t.Errorf("expected 0 conflicts, got %d", len(noConflicts))
	}
}

func TestGenerateID(t *testing.T) {
	svc := newTestService(t)

	date := time.Date(2025, 12, 29, 0, 0, 0, 0, time.UTC)
	tm := time.Date(0, 1, 1, 10, 51, 0, 0, time.UTC)

	id := svc.GenerateID(date, tm)
	if len(id) != 14 {
		t.Errorf("expected ID length 14, got %d", len(id))
	}
	if id[:12] != "202512291051" {
		t.Errorf("expected prefix 202512291051, got %s", id[:12])
	}
}

func TestFindByID(t *testing.T) {
	svc := newTestService(t)

	id := addTestSession(svc, "2025-12-29", "10:51", "Иван Иваныч", "Стрельба из лука", 90)

	sess, _, _ := svc.FindByID(id)
	if sess == nil {
		t.Fatal("expected to find session")
	}
	if sess.Name != "Иван Иваныч" {
		t.Errorf("expected Иван Иваныч, got %s", sess.Name)
	}

	sess, _, _ = svc.FindByID("00000000000000")
	if sess != nil {
		t.Error("expected nil for nonexistent ID")
	}
}

func TestEditEntry(t *testing.T) {
	svc := newTestService(t)

	id := addTestSession(svc, "2025-12-29", "10:51", "Иван Иваныч", "Стрельба из лука", 90)

	err := svc.EditEntry(id, models.Session{
		Name: "Пётр Петрович",
	})
	if err != nil {
		t.Fatalf("EditEntry: %v", err)
	}

	sess, _, _ := svc.FindByID(id)
	if sess.Name != "Пётр Петрович" {
		t.Errorf("expected Пётр Петрович, got %s", sess.Name)
	}
	if sess.Type != "Стрельба из лука" {
		t.Errorf("type should remain unchanged, got %s", sess.Type)
	}
}

func TestDeleteEntry(t *testing.T) {
	svc := newTestService(t)

	id := addTestSession(svc, "2025-12-29", "10:51", "Иван Иваныч", "Стрельба из лука", 90)

	err := svc.DeleteEntry(id)
	if err != nil {
		t.Fatalf("DeleteEntry: %v", err)
	}

	sess, _, _ := svc.FindByID(id)
	if sess != nil {
		t.Error("expected nil after deletion")
	}

	if len(svc.data.Entries) != 0 {
		t.Errorf("expected 0 DateEntries after deleting last session, got %d", len(svc.data.Entries))
	}
}

func TestFindByPeriod(t *testing.T) {
	svc := newTestService(t)

	addTestSession(svc, "2025-12-01", "10:00", "A", "Type", 30)
	addTestSession(svc, "2025-12-15", "12:00", "B", "Type", 45)
	addTestSession(svc, "2025-12-31", "14:00", "C", "Type", 60)

	start := time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 12, 15, 0, 0, 0, 0, time.UTC)

	entries := svc.FindByPeriod(start, end)
	if len(entries) != 2 {
		t.Errorf("expected 2 entries in period, got %d", len(entries))
	}
}

func TestTotalHours(t *testing.T) {
	svc := newTestService(t)

	addTestSession(svc, "2025-12-29", "10:00", "A", "Type", 60)
	addTestSession(svc, "2025-12-29", "11:00", "B", "Type", 90)

	entries := svc.FindByPeriod(
		time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC),
	)

	hours := TotalHours(entries)
	if hours != 2.5 {
		t.Errorf("expected 2.5 hours, got %.1f", hours)
	}
}

func TestAllTypes(t *testing.T) {
	svc := newTestService(t)

	addTestSession(svc, "2025-12-29", "10:00", "A", "Стрельба из лука", 60)
	addTestSession(svc, "2025-12-29", "11:00", "B", "Метание ножей", 60)
	addTestSession(svc, "2025-12-30", "12:00", "C", "Бокс", 60)

	types := svc.AllTypes()
	if len(types) < 3 {
		t.Errorf("expected at least 3 types, got %d: %v", len(types), types)
	}
	if types[0] != "Стрельба из лука" {
		t.Errorf("expected first type Стрельба из лука, got %s", types[0])
	}
	if types[1] != "Метание ножей" {
		t.Errorf("expected second type Метание ножей, got %s", types[1])
	}
}

func TestSaveLoad(t *testing.T) {
	svc := newTestService(t)

	id := addTestSession(svc, "2025-12-29", "10:51", "Иван Иваныч", "Стрельба из лука", 90)

	if err := svc.Save(context.Background()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	svc2, err := NewService(context.Background(), svc.dir, svc.baseName, svc.mode)
	if err != nil {
		t.Fatalf("NewService reload: %v", err)
	}

	sess, _, _ := svc2.FindByID(id)
	if sess == nil {
		t.Fatal("session not found after reload")
	}
	if sess.Name != "Иван Иваныч" {
		t.Errorf("data mismatch after reload")
	}
}

func TestGetWeekEntries(t *testing.T) {
	svc := newTestService(t)

	addTestSession(svc, "2025-12-29", "10:00", "Monday", "T", 30)
	addTestSession(svc, "2025-12-31", "10:00", "Wednesday", "T", 30)

	refDate := time.Date(2025, 12, 29, 0, 0, 0, 0, time.UTC)
	entries := svc.GetWeekEntries(refDate)
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}
}

func TestGetMonthEntries(t *testing.T) {
	svc := newTestService(t)

	addTestSession(svc, "2025-12-01", "10:00", "First day", "T", 30)
	addTestSession(svc, "2025-12-31", "10:00", "Last day", "T", 30)

	refDate := time.Date(2025, 12, 15, 0, 0, 0, 0, time.UTC)
	entries := svc.GetMonthEntries(refDate)
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}
}
