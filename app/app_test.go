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
	// add: date, time, name, type(1), duration, notes(empty), status(0), no repeat
	// then main menu: 8 to quit
	script := lines(
		"1", "15.06.2026", "14:30", "Совещание", "1", "45", "", "0", "",
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
	// daily repeat 06->08 Jan (3 sessions), all defaults
	script := lines(
		"1", "06.01.2026", "09:00", "Зарядка", "1", "15", "", "0",
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

	// Edit whole series duration to 20 via view/all/\2/series/field4.
	a2, _ := newTestAppInDir(t, dir, lines(
		"2", "4", "\\2", "2", "4", "20", "", "0", "8",
	))
	a2.mainMenu()
	for _, s := range a2.svc.All() {
		if s.Duration != 20 {
			t.Errorf("series duration not propagated: %s has %d", s.ID, s.Duration)
		}
	}

	// Delete whole series.
	a3, _ := newTestAppInDir(t, dir, lines("3", "1", a2.svc.All()[1].ID, "2", "8"))
	a3.mainMenu()
	if a3.svc.Count() != 0 {
		t.Fatalf("series not fully deleted, %d left", a3.svc.Count())
	}
}

func TestRunCLIView(t *testing.T) {
	a, dir := newTestApp(t, lines(
		"1", "10.03.2026", "10:00", "X", "1", "30", "", "0", "", "8",
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
