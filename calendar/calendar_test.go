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
	s.AddEntry(models.Session{ID: id, Time: tm, Name: name, Type: typ, Duration: duration})
	return id
}

func firstByID(s *Service, id string) *models.Session {
	got := s.FindByID(id)
	if len(got) == 0 {
		return nil
	}
	return &got[0]
}

func TestAddEntryKeepsSortOrder(t *testing.T) {
	svc := newTestService(t)
	addTestSession(svc, "2025-12-29", "12:00", "noon", "T", 30)
	addTestSession(svc, "2025-12-01", "09:00", "early", "T", 30)
	addTestSession(svc, "2025-12-29", "08:00", "morning", "T", 30)

	if len(svc.data.Entries) != 2 {
		t.Fatalf("expected 2 date entries, got %d", len(svc.data.Entries))
	}
	if svc.data.Entries[0].Date != "2025-12-01" || svc.data.Entries[1].Date != "2025-12-29" {
		t.Fatalf("date entries not sorted: %+v", svc.data.Entries)
	}
	sessions := svc.data.Entries[1].Sessions
	if sessions[0].Time != "08:00" || sessions[1].Time != "12:00" {
		t.Errorf("sessions within day not sorted by time: %+v", sessions)
	}
}

func TestFindConflicts(t *testing.T) {
	svc := newTestService(t)
	addTestSession(svc, "2025-12-29", "10:51", "Иван Иваныч", "Стрельба из лука", 90)
	addTestSession(svc, "2025-12-29", "10:51", "Пётр Петрович", "Метание ножей", 60)

	if got := svc.FindConflicts("2025-12-29", "10:51"); len(got) != 2 {
		t.Errorf("expected 2 conflicts, got %d", len(got))
	}
	if got := svc.FindConflicts("2025-12-29", "12:00"); len(got) != 0 {
		t.Errorf("expected 0 conflicts, got %d", len(got))
	}
}

func TestGenerateIDUniquePerMinute(t *testing.T) {
	svc := newTestService(t)
	date := time.Date(2025, 12, 29, 0, 0, 0, 0, time.UTC)
	tm := time.Date(0, 1, 1, 10, 51, 0, 0, time.UTC)

	id1 := svc.generateID(date, tm, 5)
	svc.AddEntry(models.Session{ID: id1, Time: "10:51", Name: "a"})
	id2 := svc.generateID(date, tm, 5)

	if len(id1) != 14 || len(id2) != 14 {
		t.Fatalf("bad ID length: %q %q", id1, id2)
	}
	if id1[:12] != "202512291051" {
		t.Errorf("unexpected prefix: %s", id1)
	}
	if id1 == id2 {
		t.Errorf("generateID returned a colliding ID: %s", id2)
	}
}

func TestEditEntryPartial(t *testing.T) {
	svc := newTestService(t)
	id := addTestSession(svc, "2025-12-29", "10:51", "Иван Иваныч", "Стрельба из лука", 90)

	if err := svc.EditEntry(id, models.Session{Name: "Пётр Петрович"}); err != nil {
		t.Fatalf("EditEntry: %v", err)
	}
	got := firstByID(svc, id)
	if got.Name != "Пётр Петрович" {
		t.Errorf("name not updated: %s", got.Name)
	}
	if got.Type != "Стрельба из лука" || got.Duration != 90 {
		t.Errorf("untouched fields changed: %+v", got)
	}
	if err := svc.EditEntry("nope", models.Session{}); err == nil {
		t.Error("expected error for missing ID")
	}
}

func TestDeleteEntry(t *testing.T) {
	svc := newTestService(t)
	id := addTestSession(svc, "2025-12-29", "10:51", "Иван Иваныч", "Стрельба из лука", 90)

	if n := svc.DeleteEntry(id); n != 1 {
		t.Fatalf("expected 1 deleted, got %d", n)
	}
	if firstByID(svc, id) != nil {
		t.Error("session still present after delete")
	}
	if len(svc.data.Entries) != 0 {
		t.Errorf("empty DateEntry not pruned, got %d", len(svc.data.Entries))
	}
}

func TestRepeatSeriesHydrationAndDeletion(t *testing.T) {
	svc := newTestService(t)
	d, _ := time.Parse("2006-01-02", "2025-01-06")
	tm, _ := time.Parse("15:04", "18:00")
	origID := svc.generateID(d, tm, 0) + models.RepeatSuffix
	svc.AddEntry(models.Session{ID: origID, Time: "18:00", Name: "Тренировка", Type: "Бег", Duration: 45, Status: "Активно"})

	// three weekly children referencing the original by Name
	for i := 1; i <= 3; i++ {
		cd := d.AddDate(0, 0, 7*i)
		cid := svc.generateID(cd, tm, i)
		svc.AddEntry(models.Session{ID: cid, Time: "18:00", Name: origID})
	}

	all := svc.GetAllEntries()
	got := 0
	for _, de := range all {
		for _, s := range de.Sessions {
			got++
			if s.Name != "Тренировка" {
				t.Errorf("child not hydrated with series name: %q", s.Name)
			}
			if s.Type != "Бег" || s.Duration != 45 || s.Status != "Активно" {
				t.Errorf("child not hydrated with series fields: %+v", s)
			}
			if !s.IsRepeat {
				t.Errorf("IsRepeat not set: %+v", s)
			}
		}
	}
	if got != 4 {
		t.Fatalf("expected 4 sessions (1 original + 3 repeats), got %d", got)
	}

	if n := svc.DeleteRepeats(origID); n != 3 {
		t.Errorf("expected 3 repeats deleted, got %d", n)
	}
	if len(svc.OrphanRepeats()) != 0 {
		t.Errorf("orphans remain after cascade delete")
	}
}

func TestEditSeriesTimePropagates(t *testing.T) {
	svc := newTestService(t)
	d, _ := time.Parse("2006-01-02", "2025-01-06")
	tm, _ := time.Parse("15:04", "18:00")
	origID := svc.generateID(d, tm, 0) + models.RepeatSuffix
	svc.AddEntry(models.Session{ID: origID, Time: "18:00", Name: "T", Duration: 30})
	child := svc.generateID(d.AddDate(0, 0, 7), tm, 1)
	svc.AddEntry(models.Session{ID: child, Time: "18:00", Name: origID})

	if n := svc.EditSeriesTime(origID, "19:30"); n != 2 {
		t.Fatalf("expected 2 sessions retimed, got %d", n)
	}
	if got := firstByID(svc, origID); got.Time != "19:30" {
		t.Errorf("original time not changed: %s", got.Time)
	}
	if got := firstByID(svc, child); got.Time != "19:30" {
		t.Errorf("child time not propagated: %s", got.Time)
	}
}

func TestOrphanRepeats(t *testing.T) {
	svc := newTestService(t)
	d, _ := time.Parse("2006-01-02", "2025-01-06")
	tm, _ := time.Parse("15:04", "18:00")
	child := svc.generateID(d, tm, 1)
	svc.AddEntry(models.Session{ID: child, Time: "18:00", Name: "20250101120000" + models.RepeatSuffix})

	if got := svc.OrphanRepeats(); len(got) != 1 {
		t.Fatalf("expected 1 orphan, got %d", len(got))
	}
}

func TestFindByPeriod(t *testing.T) {
	svc := newTestService(t)
	addTestSession(svc, "2025-12-01", "10:00", "A", "T", 30)
	addTestSession(svc, "2025-12-15", "12:00", "B", "T", 45)
	addTestSession(svc, "2025-12-31", "14:00", "C", "T", 60)

	entries := svc.FindByPeriod(
		time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 12, 15, 0, 0, 0, 0, time.UTC),
	)
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}
}

func TestTotalHours(t *testing.T) {
	svc := newTestService(t)
	addTestSession(svc, "2025-12-29", "10:00", "A", "T", 60)
	addTestSession(svc, "2025-12-29", "11:00", "B", "T", 90)

	entries := svc.FindByPeriod(
		time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC),
	)
	if h := TotalHours(entries); h != 2.5 {
		t.Errorf("expected 2.5 hours, got %.1f", h)
	}
}

func TestAllTypesDefaultsFirst(t *testing.T) {
	svc := newTestService(t)
	addTestSession(svc, "2025-12-29", "10:00", "A", "Стрельба из лука", 60)
	addTestSession(svc, "2025-12-30", "12:00", "C", "Бокс", 60)

	types := svc.AllTypes()
	if types[0] != "Стрельба из лука" || types[1] != "Метание ножей" {
		t.Errorf("defaults not first: %v", types)
	}
	if types[len(types)-1] != "Бокс" {
		t.Errorf("data-derived type missing/misordered: %v", types)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	svc := newTestService(t)
	id := addTestSession(svc, "2025-12-29", "10:51", "Иван Иваныч", "Стрельба из лука", 90)
	if err := svc.Save(context.Background()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	svc2, err := NewService(context.Background(), svc.dir, svc.baseName, svc.mode)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := firstByID(svc2, id); got == nil || got.Name != "Иван Иваныч" {
		t.Fatalf("data mismatch after reload: %+v", got)
	}
}

func TestGetWeekAndMonthEntries(t *testing.T) {
	svc := newTestService(t)
	addTestSession(svc, "2025-12-29", "10:00", "Mon", "T", 30) // Monday
	addTestSession(svc, "2025-12-31", "10:00", "Wed", "T", 30)
	addTestSession(svc, "2026-01-05", "10:00", "NextWeek", "T", 30)

	ref := time.Date(2025, 12, 29, 0, 0, 0, 0, time.UTC)
	if got := svc.GetWeekEntries(ref); len(got) != 2 {
		t.Errorf("week: expected 2, got %d", len(got))
	}
	if got := svc.GetMonthEntries(ref); len(got) != 2 {
		t.Errorf("month: expected 2, got %d", len(got))
	}
}
