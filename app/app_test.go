package app

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mycalendar/calendar"
	"mycalendar/color"
	"mycalendar/config"
	"mycalendar/models"
)

func init() { color.SetEnabled(false) }

// newTestApp builds an App whose stdin is the given script and whose data lives
// in a temp dir. stdout is left alone (tests assert on state, not output).
func newTestApp(t *testing.T, script string) (*App, string) {
	t.Helper()
	dir := t.TempDir()
	svc, err := calendar.NewService(context.Background(), dir, "mycal", models.SplitNone)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	cfg := config.DefaultConfig()
	a := &App{
		svc:     svc,
		cfg:     cfg,
		cfgPath: filepath.Join(dir, config.ConfigFile),
		ctx:     context.Background(),
		in:      bufio.NewReader(strings.NewReader(script)),
	}
	return a, dir
}

func lines(ss ...string) string { return strings.Join(ss, "\n") + "\n" }

func TestMainMenuExitsOnEOF(t *testing.T) {
	a, _ := newTestApp(t, "") // empty stdin -> immediate EOF
	done := make(chan struct{})
	go func() { a.mainMenu(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("mainMenu did not exit on EOF (busy loop?)")
	}
}

func TestAddThenDeleteInteractive(t *testing.T) {
	// add: date, time, name, type(1), duration, cash(empty), notes(empty), status(0), no repeat
	// then main menu: 8 to quit
	script := lines(
		"1", "15.06.2026", "14:30", "Совещание", "1", "45", "", "", "0", "",
		"8",
	)
	a, dir := newTestApp(t, script)
	a.mainMenu()

	if a.svc.Count() != 1 {
		t.Fatalf("expected 1 session, got %d", a.svc.Count())
	}
	all := a.svc.All()
	if all[0].Name != "Совещание" || all[0].Time != "14:30" || all[0].Duration != 45 {
		t.Fatalf("bad session: %+v", all[0])
	}
	if _, err := os.Stat(filepath.Join(dir, "mycal.json")); err != nil {
		t.Fatalf("data file not written: %v", err)
	}

	// New app over the same dir: delete the entry by ID.
	id := all[0].ID
	a2, _ := newTestAppInDir(t, dir, lines("3", "1", id, "да", "8"))
	a2.mainMenu()
	if a2.svc.Count() != 0 {
		t.Fatalf("expected 0 after delete, got %d", a2.svc.Count())
	}
}

func TestRepeatSeriesAddEditDelete(t *testing.T) {
	// daily repeat 06->08 Jan (3 sessions), all defaults (cash empty)
	script := lines(
		"1", "06.01.2026", "09:00", "Зарядка", "1", "15", "", "", "0",
		"d", "08.01.2026", "",
		"8",
	)
	a, dir := newTestApp(t, script)
	a.mainMenu()

	all := a.svc.All()
	if len(all) != 3 {
		t.Fatalf("expected 3 sessions in series, got %d", len(all))
	}
	series := all[0].SeriesID
	if series == "" {
		t.Fatal("SeriesID not set")
	}
	for _, s := range all {
		if s.SeriesID != series {
			t.Errorf("session %s not in series: %q", s.ID, s.SeriesID)
		}
	}

	// 1. Edit "this and following" (option 2) on session 2 (07.01).
	// Changes duration to 25 for sessions 2 & 3, session 1 remains 15.
	a2, _ := newTestAppInDir(t, dir, lines(
		"2", "4", "\\2", "2", "4", "25", "", "0", "8",
	))
	a2.mainMenu()
	all2 := a2.svc.All()
	if len(all2) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(all2))
	}
	if all2[0].Duration != 15 || all2[0].SeriesID != series {
		t.Errorf("session 1 should remain 15 min & original series, got dur=%d series=%s", all2[0].Duration, all2[0].SeriesID)
	}
	if all2[1].Duration != 25 || all2[1].SeriesID != all2[1].ID {
		t.Errorf("session 2 should be 25 min & branched series, got dur=%d series=%s", all2[1].Duration, all2[1].SeriesID)
	}
	if all2[2].Duration != 25 || all2[2].SeriesID != all2[1].ID {
		t.Errorf("session 3 should be 25 min & branched series, got dur=%d series=%s", all2[2].Duration, all2[2].SeriesID)
	}

	// 2. Edit "whole series" (option 3) on branched series (session 2 & 3).
	// Changes duration to 30 for both.
	a3, _ := newTestAppInDir(t, dir, lines(
		"2", "4", "\\2", "3", "4", "30", "", "0", "8",
	))
	a3.mainMenu()
	all3 := a3.svc.All()
	if all3[0].Duration != 15 {
		t.Errorf("session 1 should still be 15 min, got %d", all3[0].Duration)
	}
	if all3[1].Duration != 30 || all3[2].Duration != 30 {
		t.Errorf("branched series should be 30 min, got %d and %d", all3[1].Duration, all3[2].Duration)
	}

	// 3. Delete "this and following" (option 2) from session 2: deletes sessions 2 and 3.
	a4, _ := newTestAppInDir(t, dir, lines("3", "1", all3[1].ID, "2", "8"))
	a4.mainMenu()
	if a4.svc.Count() != 1 {
		t.Fatalf("expected 1 session remaining (session 1), got %d", a4.svc.Count())
	}
	if a4.svc.All()[0].ID != all3[0].ID {
		t.Fatalf("expected session 1 to remain, got %s", a4.svc.All()[0].ID)
	}

	// 4. Delete the remaining single session (not a series anymore, or delete as single).
	a5, _ := newTestAppInDir(t, dir, lines("3", "1", all3[0].ID, "1", "8"))
	a5.mainMenu()
	if a5.svc.Count() != 0 {
		t.Fatalf("series not fully deleted, %d left", a5.svc.Count())
	}
}

func TestRunCLIView(t *testing.T) {
	a, dir := newTestApp(t, lines(
		"1", "10.03.2026", "10:00", "X", "1", "30", "", "", "0", "", "8",
	))
	a.mainMenu()

	a2, _ := newTestAppInDir(t, dir, "")
	if !a2.RunCLI("view", []string{"2026"}) {
		t.Fatal("RunCLI view returned false")
	}
	if !a2.RunCLI("today", nil) {
		t.Fatal("RunCLI today returned false")
	}
	if a2.RunCLI("bogus", nil) {
		t.Fatal("RunCLI bogus should return false")
	}
}

func newTestAppInDir(t *testing.T, dir, script string) (*App, string) {
	t.Helper()
	svc, err := calendar.NewService(context.Background(), dir, "mycal", models.SplitNone)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	a := &App{
		svc:     svc,
		cfg:     config.DefaultConfig(),
		cfgPath: filepath.Join(dir, config.ConfigFile),
		ctx:     context.Background(),
		in:      bufio.NewReader(strings.NewReader(script)),
	}
	return a, dir
}

func TestAddWithSkip8(t *testing.T) {
	// add: date, time, name, then "8" to skip all remaining optional questions and save
	// then main menu: 8 to quit
	script := lines(
		"1", "15.06.2026", "14:30", "Быстрая встреча", "8",
		"8",
	)
	a, dir := newTestApp(t, script)
	a.mainMenu()

	if a.svc.Count() != 1 {
		t.Fatalf("expected 1 session, got %d", a.svc.Count())
	}
	s := a.svc.All()[0]
	if s.Name != "Быстрая встреча" || s.Time != "14:30" || s.Duration != 60 || s.Notes != "" || s.Status != "" {
		t.Fatalf("unexpected session: %+v", s)
	}
	if _, err := os.Stat(filepath.Join(dir, "mycal.json")); err != nil {
		t.Fatalf("data file not written: %v", err)
	}
}

func TestConflictDefaultsAndEnterAdd(t *testing.T) {
	// 1. AskOnConflict is false by default: second entry at same time added without any prompt
	script := lines(
		"1", "15.06.2026", "14:30", "Первая", "8",
		"1", "15.06.2026", "14:30", "Вторая", "8",
		"8",
	)
	a, dir := newTestApp(t, script)
	a.mainMenu()
	if a.svc.Count() != 2 {
		t.Fatalf("expected 2 sessions, got %d", a.svc.Count())
	}

	// 2. Enable AskOnConflict: adding third entry at same time prompts conflict, Enter adds it anyway
	a.cfg.AskOnConflict = true
	a.feed(lines(
		"1", "15.06.2026", "14:30", "Третья", "", "8", // "" presses Enter at conflict resolution
		"8",
	))
	a.mainMenu()
	if a.svc.Count() != 3 {
		t.Fatalf("expected 3 sessions after Enter conflict resolution, got %d", a.svc.Count())
	}
	_ = dir
}

func TestSettingsToggleAskConflictAndShowHints(t *testing.T) {
	// 6 (settings), 10 (toggle conflict), 11 (toggle hints), 0 (save & exit), 8 (main menu quit)
	script := lines(
		"6", "10", "11", "0",
		"8",
	)
	a, dir := newTestApp(t, script)
	a.mainMenu()

	if !a.cfg.AskOnConflict {
		t.Errorf("expected AskOnConflict true")
	}
	if !a.cfg.ShowHints {
		t.Errorf("expected ShowHints true")
	}

	saved, err := config.Load(context.Background(), filepath.Join(dir, config.ConfigFile))
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if !saved.AskOnConflict {
		t.Errorf("saved config AskOnConflict is false, want true")
	}
	if !saved.ShowHints {
		t.Errorf("saved config ShowHints is false, want true")
	}
}

func TestDateCheckHint(t *testing.T) {
	modes := []string{models.DateCheckOff, models.DateCheckAsk, models.DateCheckFix, models.DateCheckReask}
	for _, m := range modes {
		h := config.DateCheckHint(m)
		if h == "" || !strings.Contains(h, "32.01") {
			t.Errorf("DateCheckHint(%q) = %q, expected non-empty hint containing 32.01", m, h)
		}
	}
}

func TestCLICashAddEditViewExport(t *testing.T) {
	// 1. Add session with cash "1500,50" (comma decimal)
	scriptAdd := lines(
		"1", "17.10.2026", "10:00", "Платное занятие", "1", "60", "1500,50", "Заметка", "0", "",
		"8",
	)
	a, dir := newTestApp(t, scriptAdd)
	a.mainMenu()

	if a.svc.Count() != 1 {
		t.Fatalf("expected 1 session, got %d", a.svc.Count())
	}
	s := a.svc.All()[0]
	if s.Cash != 1500.5 {
		t.Fatalf("expected cash 1500.5, got %v", s.Cash)
	}

	// 2. Edit cash to 2000 via field 7
	scriptEdit := lines(
		"2", "4", "\\1", "7", "2000", "", "8",
	)
	a2, _ := newTestAppInDir(t, dir, scriptEdit)
	a2.mainMenu()
	s = a2.svc.All()[0]
	if s.Cash != 2000 {
		t.Fatalf("expected cash 2000 after edit, got %v", s.Cash)
	}

	// 3. Clear cash to 0 via "-"
	scriptClear := lines(
		"2", "4", "\\1", "7", "-", "", "8",
	)
	a3, _ := newTestAppInDir(t, dir, scriptClear)
	a3.mainMenu()
	s = a3.svc.All()[0]
	if s.Cash != 0 {
		t.Fatalf("expected cash 0 after '-' clear, got %v", s.Cash)
	}

	// 4. Edit cash via fullEdit (choice 8) to negative cash -350
	scriptFullEdit := lines(
		"2", "4", "\\1", "8", "", "", "", "", "-350", "", "", "", "8",
	)
	a4, _ := newTestAppInDir(t, dir, scriptFullEdit)
	a4.mainMenu()
	s = a4.svc.All()[0]
	if s.Cash != -350 {
		t.Fatalf("expected cash -350 after fullEdit, got %v", s.Cash)
	}

	// 5. Verify showSessionDetail, printSummary, printStats
	a4.showSessionDetail(s)
	a4.printSummary(a4.svc.All())
	a4.printStats(a4.svc.All())

	// 6. Verify CSV export includes Cash column and value
	a4.exportCSV(a4.svc.All())
	matches, _ := filepath.Glob(filepath.Join(dir, "export_*.csv"))
	if len(matches) == 0 {
		t.Fatal("expected CSV export")
	}
	content, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	csvStr := string(content)
	if !strings.Contains(csvStr, "Кэш") {
		t.Fatalf("expected 'Кэш' in CSV header, got:\n%s", csvStr)
	}
	if !strings.Contains(csvStr, "-350") {
		t.Fatalf("expected '-350' in CSV content, got:\n%s", csvStr)
	}
}

func TestCLICreateAndSelectCalendar(t *testing.T) {
	// 1. Add entry to default calendar ("1"), then create calendar "work" with yearly mode ("10", "work", "2"),
	// add an entry to "work" ("1"), then switch back to default calendar ("9", "1"), and exit ("8").
	script := lines(
		"1", "17.10.2026", "10:00", "Запись в основном", "8",
		"10", "work", "2",
		"1", "17.10.2026", "11:00", "Запись в рабочем", "8",
		"9", "1",
		"8",
	)
	a, dir := newTestApp(t, script)
	a.mainMenu()

	// Should now be on default calendar with 1 entry
	if a.cfg.ActiveCalendar != "" || a.svc.Count() != 1 || a.svc.All()[0].Name != "Запись в основном" {
		t.Fatalf("expected default calendar with 1 entry, got active=%q count=%d", a.cfg.ActiveCalendar, a.svc.Count())
	}

	// Verify 2026_mycal--work.json was created on disk
	if _, err := os.Stat(filepath.Join(dir, "2026_mycal--work.json")); err != nil {
		t.Fatalf("expected 2026_mycal--work.json on disk: %v", err)
	}

	// Switch back to "work" via CLI dialog ("9", "2")
	a.feed(lines("9", "2", "8"))
	a.mainMenu()
	if a.cfg.ActiveCalendar != "work" || a.svc.Count() != 1 || a.svc.All()[0].Name != "Запись в рабочем" {
		t.Fatalf("expected work calendar with 1 entry, got active=%q count=%d", a.cfg.ActiveCalendar, a.svc.Count())
	}
}
