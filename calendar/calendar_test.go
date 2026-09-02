package calendar

import (
	"context"
	"testing"
	"time"

	"mycalendar/models"
)

func newSvc(t *testing.T) *Service {
	t.Helper()
	svc, err := NewService(context.Background(), t.TempDir(), "test", models.SplitNone)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc
}

func add(t *testing.T, s *Service, date, tm, name, typ string, dur int) string {
	t.Helper()
	d, _ := time.Parse("2006-01-02", date)
	c, _ := time.Parse("15:04", tm)
	id := s.GenerateID(d, c)
	if err := s.Add(models.Session{ID: id, Time: tm, Name: name, Type: typ, Duration: dur}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	return id
}

func TestAddKeepsOrderAndRejectsDupID(t *testing.T) {
	s := newSvc(t)
	add(t, s, "2025-12-29", "12:00", "noon", "T", 30)
	add(t, s, "2025-12-01", "09:00", "early", "T", 30)
	id := add(t, s, "2025-12-29", "08:00", "morning", "T", 30)

	all := s.All()
	if all[0].Date() != "2025-12-01" || all[1].Time != "08:00" {
		t.Fatalf("not sorted: %+v", all)
	}
	if err := s.Add(models.Session{ID: id, Time: "08:00", Name: "dup"}); err == nil {
		t.Error("expected duplicate-ID error")
	}
}

func TestGenerateIDUnique(t *testing.T) {
	s := newSvc(t)
	d, _ := time.Parse("2006-01-02", "2025-12-29")
	c, _ := time.Parse("15:04", "10:51")
	var ids []string
	for i := 0; i < 5; i++ {
		id := s.GenerateID(d, c)
		for _, prev := range ids {
			if prev == id {
				t.Fatalf("collision: %s", id)
			}
		}
		if len(id) != 14 || id[:12] != "202512291051" {
			t.Fatalf("bad ID %q", id)
		}
		_ = s.Add(models.Session{ID: id, Time: "10:51", Name: "x"})
		ids = append(ids, id)
	}
}

func TestConflictsAndSearch(t *testing.T) {
	s := newSvc(t)
	add(t, s, "2025-12-29", "10:51", "Иван", "Лук", 90)
	add(t, s, "2025-12-29", "10:51", "Пётр", "Ножи", 60)
	add(t, s, "2025-12-29", "12:00", "둘", "Лук", 30)

	if got := s.Conflicts("2025-12-29", "10:51"); len(got) != 2 {
		t.Errorf("conflicts: want 2, got %d", len(got))
	}
	if got := s.Search("иван"); len(got) != 1 {
		t.Errorf("search name: want 1, got %d", len(got))
	}
	if got := s.Search("лук"); len(got) != 2 {
		t.Errorf("search type: want 2, got %d", len(got))
	}
}

func TestUpdateAndUpdateSeries(t *testing.T) {
	s := newSvc(t)
	a := add(t, s, "2025-01-06", "18:00", "Trn", "Run", 30)
	b := add(t, s, "2025-01-13", "18:00", "Trn", "Run", 30)
	// mark them a series
	s.sessions[0].SeriesID, s.sessions[1].SeriesID = a, a
	_ = b

	name := "Renamed"
	if err := s.Update(a, Patch{Name: &name}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if g, _ := s.ByID(a); g.Name != "Renamed" {
		t.Errorf("update failed: %s", g.Name)
	}

	newTime := "19:30"
	if n := s.UpdateSeries(a, Patch{Time: &newTime}); n != 2 {
		t.Fatalf("UpdateSeries changed %d, want 2", n)
	}
	for _, id := range []string{a, b} {
		if g, _ := s.ByID(id); g.Time != "19:30" {
			t.Errorf("series time not propagated to %s: %s", id, g.Time)
		}
	}
}

func TestDeleteVariants(t *testing.T) {
	s := newSvc(t)
	a := add(t, s, "2025-01-06", "18:00", "S", "T", 30)
	b := add(t, s, "2025-01-13", "18:00", "S", "T", 30)
	c := add(t, s, "2025-02-01", "10:00", "Solo", "T", 30)
	s.sessions[0].SeriesID, s.sessions[1].SeriesID = a, a

	if s.DeleteSeries(a) != 2 {
		t.Fatal("DeleteSeries count")
	}
	if _, ok := s.ByID(b); ok {
		t.Error("series member survived")
	}
	if !s.Delete(c) || s.Delete(c) {
		t.Error("Delete should return true then false")
	}
}

func TestPatchEmptyAndClear(t *testing.T) {
	if !(Patch{}).Empty() {
		t.Error("zero Patch should be Empty")
	}
	s := newSvc(t)
	id := add(t, s, "2025-01-06", "18:00", "S", "T", 30)
	empty := ""
	if err := s.Update(id, Patch{Notes: &empty, Status: &empty}); err != nil {
		t.Fatal(err)
	}
	if g, _ := s.ByID(id); g.Notes != "" || g.Status != "" {
		t.Error("explicit empty patch should clear fields")
	}
}

func TestValidateAndRepair(t *testing.T) {
	s := newSvc(t)
	good := add(t, s, "2025-01-06", "18:00", "ok", "T", 30)
	s.sessions = append(s.sessions,
		models.Session{ID: "", Name: "no id"},
		models.Session{ID: good, Name: "dup", Time: "18:00"},
		models.Session{ID: "20250107180000", Name: "neg", Time: "18:00", Duration: -5},
		models.Session{ID: "20250108180000", Name: "badtime", Time: "99:99"},
	)

	if len(s.Validate()) < 4 {
		t.Errorf("expected >=4 issues, got %d", len(s.Validate()))
	}
	changed := s.Repair()
	if changed == 0 {
		t.Fatal("Repair changed nothing")
	}
	if len(s.Validate()) != 0 {
		t.Errorf("issues remain after repair: %v", s.Validate())
	}
	// the empty-ID row is dropped; the rest survive with fixes
	if s.Count() != 4 {
		t.Errorf("want 4 sessions after repair, got %d", s.Count())
	}
}

func TestTypesStatusesDefaultsFirst(t *testing.T) {
	s := newSvc(t)
	add(t, s, "2025-01-06", "18:00", "x", "Бокс", 30)
	types := s.Types("Йога")
	if types[0] != "Стрельба из лука" || types[1] != "Метание ножей" {
		t.Errorf("defaults not first: %v", types)
	}
	if !contains(types, "Бокс") || !contains(types, "Йога") {
		t.Errorf("seed/used type missing: %v", types)
	}
}

func TestPersistRoundTrip(t *testing.T) {
	s := newSvc(t)
	id := add(t, s, "2025-12-29", "10:51", "Иван", "Лук", 90)
	if err := s.Save(context.Background()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	s2, err := NewService(context.Background(), s.dir, s.baseName, s.mode)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if g, ok := s2.ByID(id); !ok || g.Name != "Иван" {
		t.Fatalf("mismatch after reload: %+v", g)
	}
}

func TestWeekMonth(t *testing.T) {
	s := newSvc(t)
	add(t, s, "2025-12-29", "10:00", "Mon", "T", 30)
	add(t, s, "2025-12-31", "10:00", "Wed", "T", 30)
	add(t, s, "2026-01-05", "10:00", "Next", "T", 30)
	ref := time.Date(2025, 12, 29, 0, 0, 0, 0, time.UTC)
	if len(s.Week(ref)) != 2 {
		t.Errorf("week count")
	}
	if len(s.Month(ref)) != 2 {
		t.Errorf("month count")
	}
}

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}
