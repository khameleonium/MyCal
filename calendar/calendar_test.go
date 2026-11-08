package calendar

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

func TestDeleteSeriesFromAndBranching(t *testing.T) {
	s := newSvc(t)
	// Create series of 4 entries
	a := add(t, s, "2026-05-01", "10:00", "Day1", "Yoga", 45)
	b := add(t, s, "2026-05-08", "10:00", "Day2", "Yoga", 45)
	c := add(t, s, "2026-05-15", "10:00", "Day3", "Yoga", 45)
	d := add(t, s, "2026-05-22", "10:00", "Day4", "Yoga", 45)
	for i := range s.sessions {
		s.sessions[i].SeriesID = a
	}

	// 1. UpdateSeriesFrom: from c (Day 3) onwards, change duration to 60 and name to "Yoga Pro"
	newName := "Yoga Pro"
	newDur := 60
	nUp := s.UpdateSeriesFrom(a, c, Patch{Name: &newName, Duration: &newDur})
	if nUp != 2 {
		t.Fatalf("expected 2 updated, got %d", nUp)
	}

	// a and b must remain Yoga / 45 min and retain series a
	sessA, _ := s.ByID(a)
	sessB, _ := s.ByID(b)
	if sessA.Name != "Day1" || sessA.Duration != 45 || sessA.SeriesID != a {
		t.Errorf("sessA altered unexpectedly: %+v", sessA)
	}
	if sessB.Name != "Day2" || sessB.Duration != 45 || sessB.SeriesID != a {
		t.Errorf("sessB altered unexpectedly: %+v", sessB)
	}

	// c and d must be Yoga Pro / 60 min and branched into series c
	sessC, _ := s.ByID(c)
	sessD, _ := s.ByID(d)
	if sessC.Name != "Yoga Pro" || sessC.Duration != 60 || sessC.SeriesID != c {
		t.Errorf("sessC not branched correctly: %+v", sessC)
	}
	if sessD.Name != "Yoga Pro" || sessD.Duration != 60 || sessD.SeriesID != c {
		t.Errorf("sessD not branched correctly: %+v", sessD)
	}

	// 2. DeleteSeriesFrom: delete from c onwards (c and d)
	nDel := s.DeleteSeriesFrom(c, c)
	if nDel != 2 {
		t.Fatalf("expected 2 deleted, got %d", nDel)
	}
	if _, ok := s.ByID(c); ok {
		t.Errorf("session c should be deleted")
	}
	if _, ok := s.ByID(d); ok {
		t.Errorf("session d should be deleted")
	}
	if _, ok := s.ByID(a); !ok {
		t.Errorf("session a should still exist")
	}
	if _, ok := s.ByID(b); !ok {
		t.Errorf("session b should still exist")
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

func TestSortSessions(t *testing.T) {
	// Sample data spanning multiple dates and times
	s1 := models.Session{ID: "20261105140000", Time: "14:00", Name: "Яблоко", Type: "Спорт", Duration: 30, Cash: 100, Status: "Активно"}
	s2 := models.Session{ID: "20261105090000", Time: "09:00", Name: "Апельсин", Type: "Работа", Duration: 90, Cash: 500, Status: "Завершено"}
	s3 := models.Session{ID: "20261104180000", Time: "18:00", Name: "Банан", Type: "Отдых", Duration: 60, Cash: -50, Status: "В процессе"}
	s4 := models.Session{ID: "20261104100000", Time: "10:00", Name: "Груша", Type: "Учёба", Duration: 120, Cash: 0, Status: "Отменено"}

	// 1. Sort by Date Descending:
	// Dates should be 2026-11-05 before 2026-11-04.
	// CRITICAL REQUIREMENT: inside 2026-11-05, time must be ascending (09:00 before 14:00).
	// inside 2026-11-04, time must be ascending (10:00 before 18:00).
	list := []models.Session{s1, s2, s3, s4}
	SortSessions(list, "date", false)
	if list[0].ID != s2.ID || list[1].ID != s1.ID || list[2].ID != s4.ID || list[3].ID != s3.ID {
		t.Fatalf("date desc sort failed: got %s(%s), %s(%s), %s(%s), %s(%s)",
			list[0].Date(), list[0].Time, list[1].Date(), list[1].Time, list[2].Date(), list[2].Time, list[3].Date(), list[3].Time)
	}

	// 2. Sort by Date Ascending:
	// Dates: 2026-11-04 before 2026-11-05.
	// Inside each date: time still ascending!
	SortSessions(list, "date", true)
	if list[0].ID != s4.ID || list[1].ID != s3.ID || list[2].ID != s2.ID || list[3].ID != s1.ID {
		t.Fatalf("date asc sort failed: got %s(%s), %s(%s), %s(%s), %s(%s)",
			list[0].Date(), list[0].Time, list[1].Date(), list[1].Time, list[2].Date(), list[2].Time, list[3].Date(), list[3].Time)
	}

	// 3. Sort by Name Ascending:
	// Апельсин, Банан, Груша, Яблоко
	SortSessions(list, "name", true)
	if list[0].Name != "Апельсин" || list[1].Name != "Банан" || list[2].Name != "Груша" || list[3].Name != "Яблоко" {
		t.Fatalf("name asc sort failed: %v, %v, %v, %v", list[0].Name, list[1].Name, list[2].Name, list[3].Name)
	}

	// 4. Sort by Name Descending:
	// Яблоко, Груша, Банан, Апельсин
	SortSessions(list, "name", false)
	if list[0].Name != "Яблоко" || list[1].Name != "Груша" || list[2].Name != "Банан" || list[3].Name != "Апельсин" {
		t.Fatalf("name desc sort failed: %v, %v, %v, %v", list[0].Name, list[1].Name, list[2].Name, list[3].Name)
	}

	// 5. Sort by Duration Descending:
	// 120, 90, 60, 30
	SortSessions(list, "duration", false)
	if list[0].Duration != 120 || list[1].Duration != 90 || list[2].Duration != 60 || list[3].Duration != 30 {
		t.Fatalf("duration desc sort failed: %d, %d, %d, %d", list[0].Duration, list[1].Duration, list[2].Duration, list[3].Duration)
	}

	// 6. Sort by Cash Descending:
	// 500, 100, 0, -50
	SortSessions(list, "cash", false)
	if list[0].Cash != 500 || list[1].Cash != 100 || list[2].Cash != 0 || list[3].Cash != -50 {
		t.Fatalf("cash desc sort failed: %v, %v, %v, %v", list[0].Cash, list[1].Cash, list[2].Cash, list[3].Cash)
	}
}

func TestFormatDuration(t *testing.T) {
	// Minutes (default)
	if got := FormatDuration(60, "min"); got != "60 мин" {
		t.Errorf("FormatDuration(60, min) = %q, want '60 мин'", got)
	}
	if got := FormatDuration(45, "min"); got != "45 мин" {
		t.Errorf("FormatDuration(45, min) = %q, want '45 мин'", got)
	}
	if got := FormatDuration(90, ""); got != "90 мин" {
		t.Errorf("FormatDuration(90, '') = %q, want '90 мин'", got)
	}

	// Hours
	if got := FormatDuration(60, "hours"); got != "1 ч" {
		t.Errorf("FormatDuration(60, hours) = %q, want '1 ч'", got)
	}
	if got := FormatDuration(120, "hours"); got != "2 ч" {
		t.Errorf("FormatDuration(120, hours) = %q, want '2 ч'", got)
	}
	if got := FormatDuration(90, "hours"); got != "1.5 ч" {
		t.Errorf("FormatDuration(90, hours) = %q, want '1.5 ч'", got)
	}
	if got := FormatDuration(45, "hours"); got != "0.8 ч" {
		t.Errorf("FormatDuration(45, hours) = %q, want '0.8 ч'", got)
	}
}

func TestUnifiedCreateUpdateStatsAndCSVExport(t *testing.T) {
	s := newSvc(t)
	cfg := &models.Config{DefaultDuration: 60, DefaultType: "Стрельба из лука"}
	cfg.Normalize()

	d := time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)
	tm := time.Date(0, 1, 1, 10, 30, 0, 0, time.UTC)

	// 1. Create multiple entries via semicolon/newline split in CreateEntries
	res, err := s.CreateEntries(context.Background(), cfg, EntryInput{
		Date:     d,
		Time:     tm,
		RawName:  "Иван; Мария",
		Type:     "Стрельба из лука",
		Duration: 60,
		Cash:     1500.5,
		Status:   "Активно",
		Notes:    "Индивидуальное занятие",
	})
	if err != nil {
		t.Fatalf("CreateEntries: %v", err)
	}
	if res.AddedCount != 2 || len(res.Sessions) != 2 {
		t.Fatalf("expected 2 sessions created, got %d", res.AddedCount)
	}

	// 2. Update first entry including date relocation and cash update
	newDate := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	newCash := 2000.0
	newName := "Иван Петров"
	updated, n, err := s.UpdateEntry(context.Background(), cfg, res.Sessions[0].ID, UpdateInput{
		Patch: Patch{
			Name: &newName,
			Cash: &newCash,
		},
		NewDate: &newDate,
	})
	if err != nil {
		t.Fatalf("UpdateEntry: %v", err)
	}
	if n != 1 || updated.Date() != "2026-11-08" || updated.Cash != 2000.0 || updated.Name != "Иван Петров" {
		t.Fatalf("unexpected updated session: %+v", updated)
	}

	// 3. ComputeStats
	st := ComputeStats(s.All())
	if st.TotalEntries != 2 || st.TotalMinutes != 120 || st.TotalCash != 3500.5 {
		t.Fatalf("unexpected stats: %+v", st)
	}

	// 4. ExportCSV
	csvPath, err := s.ExportCSV(s.All())
	if err != nil || csvPath == "" {
		t.Fatalf("ExportCSV failed: %v", err)
	}
}

func TestMultiCalendarCreateDiscoverAndSwitch(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfg := &models.Config{DataFileName: "mycal", SplitMode: models.SplitNone}
	cfg.Normalize()

	svc, err := NewService(ctx, dir, cfg.ActiveEffectiveBaseName(), cfg.ActiveSplitMode())
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	// Add 1 session to default calendar ("mycal.json")
	d := time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)
	tm := time.Date(0, 1, 1, 9, 0, 0, 0, time.UTC)
	_, err = svc.CreateEntries(ctx, cfg, EntryInput{
		Date:     d,
		Time:     tm,
		RawName:  "Запись в основном",
		Type:     "Стрельба из лука",
		Duration: 60,
	})
	if err != nil {
		t.Fatalf("CreateEntries default: %v", err)
	}
	if err := svc.Save(ctx); err != nil {
		t.Fatalf("Save default: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "mycal.json")); err != nil {
		t.Fatalf("expected mycal.json on disk: %v", err)
	}

	// Create a second calendar "work" with SplitNone ("mycal--work.json")
	if err := svc.CreateCalendar(ctx, cfg, "work", models.SplitNone); err != nil {
		t.Fatalf("CreateCalendar work: %v", err)
	}
	if cfg.ActiveCalendar != "work" || svc.BaseName() != "mycal--work" {
		t.Fatalf("unexpected active calendar after create: cfg=%q svc=%q", cfg.ActiveCalendar, svc.BaseName())
	}
	if _, err := os.Stat(filepath.Join(dir, "mycal--work.json")); err != nil {
		t.Fatalf("expected mycal--work.json on disk: %v", err)
	}
	if svc.Count() != 0 {
		t.Fatalf("expected new calendar to be empty, got %d", svc.Count())
	}

	// Add 2 sessions to "work"
	_, err = svc.CreateEntries(ctx, cfg, EntryInput{
		Date:     d,
		Time:     tm,
		RawName:  "Созвон; Отчёт",
		Type:     "Работа",
		Duration: 45,
	})
	if err != nil {
		t.Fatalf("CreateEntries work: %v", err)
	}
	if err := svc.Save(ctx); err != nil {
		t.Fatalf("Save work: %v", err)
	}

	// Create a third calendar "sport" with SplitYear ("YYYY_mycal--sport.json")
	if err := svc.CreateCalendar(ctx, cfg, "sport", models.SplitYear); err != nil {
		t.Fatalf("CreateCalendar sport: %v", err)
	}
	yearFile := filepath.Join(dir, time.Now().Format("2006")+"_mycal--sport.json")
	if _, err := os.Stat(yearFile); err != nil {
		t.Fatalf("expected yearly file %s on disk: %v", yearFile, err)
	}

	// Discover calendars: should find "", "sport", "work"
	cals := svc.ListCalendars(cfg)
	if len(cals) != 3 {
		t.Fatalf("expected 3 calendars, got %d (%+v)", len(cals), cals)
	}
	if cals[0].Key != "" || cals[1].Key != "sport" || cals[1].SplitMode != models.SplitYear || cals[2].Key != "work" || cals[2].SplitMode != models.SplitNone {
		t.Fatalf("unexpected discovered calendars: %+v", cals)
	}

	// Switch back to "work" -> should have 2 sessions
	if err := svc.SwitchCalendar(ctx, cfg, "work"); err != nil {
		t.Fatalf("SwitchCalendar work: %v", err)
	}
	if svc.Count() != 2 {
		t.Fatalf("expected 2 sessions in work, got %d", svc.Count())
	}

	// Switch back to default "" -> should have 1 session
	if err := svc.SwitchCalendar(ctx, cfg, ""); err != nil {
		t.Fatalf("SwitchCalendar default: %v", err)
	}
	if svc.Count() != 1 || svc.All()[0].Name != "Запись в основном" {
		t.Fatalf("expected 1 session in default calendar, got %+v", svc.All())
	}
}

func TestUndoMoveImportAndEmptyStatus(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfg := &models.Config{DataFileName: "mycal", SplitMode: models.SplitNone}
	cfg.Normalize()

	svc, err := NewService(ctx, dir, cfg.ActiveEffectiveBaseName(), cfg.ActiveSplitMode())
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	// 1. Status normalization & cycling
	if NormalizeStatus("~пусто~") != "" || NormalizeStatus("<пусто>") != "" || NormalizeStatus("Активно") != "Активно" {
		t.Fatalf("unexpected NormalizeStatus result")
	}
	if FormatStatusDisplay("") != EmptyStatusLabel {
		t.Fatalf("expected %q for empty status display, got %q", EmptyStatusLabel, FormatStatusDisplay(""))
	}
	avail := []string{"Активно", "Завершено"}
	s1 := NextStatusInCycle("Активно", avail)
	s2 := NextStatusInCycle(s1, avail)
	s3 := NextStatusInCycle(s2, avail)
	if s1 != "Завершено" || s2 != "" || s3 != "Активно" {
		t.Fatalf("unexpected status cycle: %q -> %q -> %q", s1, s2, s3)
	}

	// 2. Add & Undo
	sess := models.Session{
		ID:       "20261107100000",
		Time:     "10:00",
		Name:     "Тест Undo",
		Type:     "Работа",
		Duration: 60,
		Status:   "",
	}
	if err := svc.Add(sess); err != nil {
		t.Fatalf("Add: %v", err)
	}
	_ = svc.Save(ctx)
	if ok, _ := svc.CanUndo(); !ok {
		t.Fatalf("expected CanUndo() == true after Add")
	}

	// Delete & Undo
	if !svc.Delete(sess.ID) {
		t.Fatalf("Delete failed")
	}
	_ = svc.Save(ctx)
	if svc.Count() != 0 {
		t.Fatalf("expected 0 sessions after delete, got %d", svc.Count())
	}
	desc, err := svc.Undo(ctx)
	if err != nil || desc == "" {
		t.Fatalf("Undo delete failed: %v", err)
	}
	if svc.Count() != 1 {
		t.Fatalf("expected 1 session restored after Undo, got %d", svc.Count())
	}

	// 3. MoveEntriesToCalendar
	moved, err := svc.MoveEntriesToCalendar(ctx, cfg, []string{sess.ID}, "work")
	if err != nil || moved != 1 {
		t.Fatalf("MoveEntriesToCalendar: moved=%d, err=%v", moved, err)
	}
	if svc.Count() != 0 {
		t.Fatalf("expected 0 sessions in default calendar after move, got %d", svc.Count())
	}
	// Verify target calendar has the moved session
	if err := svc.SwitchCalendar(ctx, cfg, "work"); err != nil {
		t.Fatalf("SwitchCalendar work: %v", err)
	}
	if svc.Count() != 1 {
		t.Fatalf("expected 1 session in work calendar after move, got %d", svc.Count())
	}

	// 4. ImportCSV and ImportJSON with deduplication
	csvData := "Дата;Время;Наименование;Тип;Длительность_мин;Кэш;Статус;Повторение;Комментарии\n" +
		"07.11.2026;10:00;Тест Undo;Работа;60;0;;Одиночная;\n" + // duplicate by Date+Time+Name
		"08.11.2026;14:00;Новая из CSV;Спорт;45;500;~пусто~;Одиночная;Заметка\n"
	resCSV, err := svc.ImportCSV(ctx, cfg, strings.NewReader(csvData))
	if err != nil {
		t.Fatalf("ImportCSV: %v", err)
	}
	if resCSV.Added != 1 || resCSV.Skipped != 1 {
		t.Fatalf("expected Added=1, Skipped=1 from CSV, got %+v", resCSV)
	}
	// Verify imported session has empty status ("") when CSV had "~пусто~"
	all := svc.All()
	var csvSess *models.Session
	for i := range all {
		if all[i].Name == "Новая из CSV" {
			csvSess = &all[i]
			break
		}
	}
	if csvSess == nil || csvSess.Status != "" {
		t.Fatalf("expected imported CSV session with empty status, got %+v", csvSess)
	}
}

func TestCalendarSettingsAndEffectiveConfig(t *testing.T) {
	globalCfg := &models.Config{
		DefaultDuration: 60,
		DefaultType:     "Общее",
		Theme:           "tokyo",
		VisibleFields:   DefaultVisibleFields,
	}

	// 1. Calendar with no overrides inherits global config
	svc, err := NewService(context.Background(), t.TempDir(), "testcal", models.SplitNone)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	eff := svc.EffectiveConfig(globalCfg)
	if eff.DefaultDuration != 60 || eff.DefaultType != "Общее" || eff.Theme != "tokyo" {
		t.Fatalf("expected inherited defaults, got duration=%d type=%s theme=%s", eff.DefaultDuration, eff.DefaultType, eff.Theme)
	}

	// 2. Calendar with custom Diary overrides
	zeroDur := 0
	diaryType := "Дневник"
	diaryTheme := "paper"
	diaryFields := "Дата; Наименование; Заметка"
	svc.SetSettings(&models.CalendarSettings{
		DefaultDuration: &zeroDur,
		DefaultType:     &diaryType,
		Theme:           &diaryTheme,
		VisibleFields:   &diaryFields,
	})

	eff2 := svc.EffectiveConfig(globalCfg)
	if eff2.DefaultDuration != 0 {
		t.Errorf("expected overridden duration 0, got %d", eff2.DefaultDuration)
	}
	if eff2.DefaultType != "Дневник" {
		t.Errorf("expected overridden type Дневник, got %s", eff2.DefaultType)
	}
	if eff2.Theme != "paper" {
		t.Errorf("expected overridden theme paper, got %s", eff2.Theme)
	}
	if eff2.VisibleFields != diaryFields {
		t.Errorf("expected overridden visible_fields, got %s", eff2.VisibleFields)
	}

	// 3. Test field visibility helper
	if !IsFieldVisible(diaryFields, FieldDate) {
		t.Errorf("expected Date to be visible in Diary")
	}
	if !IsFieldVisible(diaryFields, FieldName) {
		t.Errorf("expected Name to be visible in Diary")
	}
	if !IsFieldVisible(diaryFields, FieldNotes) {
		t.Errorf("expected Notes to be visible in Diary")
	}
	if IsFieldVisible(diaryFields, FieldDuration) {
		t.Errorf("expected Duration to NOT be visible in Diary")
	}
	if IsFieldVisible(diaryFields, FieldStatus) {
		t.Errorf("expected Status to NOT be visible in Diary")
	}
	if IsFieldVisible(diaryFields, FieldCash) {
		t.Errorf("expected Cash to NOT be visible in Diary")
	}

	// 4. Persistence test: Save and reload
	if err := svc.Save(context.Background()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	svcReloaded, err := NewService(context.Background(), svc.DataDir(), svc.BaseName(), svc.Mode())
	if err != nil {
		t.Fatalf("Reload NewService: %v", err)
	}
	if svcReloaded.Settings() == nil {
		t.Fatalf("expected settings to persist on disk, got nil")
	}
	if svcReloaded.Settings().DefaultType == nil || *svcReloaded.Settings().DefaultType != "Дневник" {
		t.Fatalf("expected persisted DefaultType 'Дневник'")
	}
}
