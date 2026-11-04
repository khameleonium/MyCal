package tui

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"mycalendar/calendar"
	"mycalendar/models"
)

func TestVisualWidth(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"Hello", 5},
		{"Привет", 6}, // 6 Cyrillic characters = 6 visual columns
		{"🗓️", 2},      // Emoji calendar with variation selector
		{"[● Активно]", 11},
		{"\033[31mКрасный\033[0m", 7}, // ANSI stripped
	}

	for _, tt := range tests {
		got := StringVisualWidth(tt.input)
		if got != tt.want {
			t.Errorf("StringVisualWidth(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestTruncateString(t *testing.T) {
	s := "Очень длинное наименование события"
	trunc := TruncateString(s, 10, "…")
	w := StringVisualWidth(trunc)
	if w > 10 {
		t.Errorf("TruncateString width %d exceeds max 10: %q", w, trunc)
	}
	if !strings.HasSuffix(trunc, "…") {
		t.Errorf("expected suffix '…', got %q", trunc)
	}
}

func TestPadRightAndLeft(t *testing.T) {
	s := "Тест"
	right := PadRight(s, 10)
	if StringVisualWidth(right) != 10 {
		t.Errorf("PadRight width = %d, want 10", StringVisualWidth(right))
	}

	left := PadLeft(s, 10)
	if StringVisualWidth(left) != 10 {
		t.Errorf("PadLeft width = %d, want 10", StringVisualWidth(left))
	}
}

func TestScreenDrawBox(t *testing.T) {
	screen := NewScreen(20, 5)
	screen.DrawBox(0, 0, 20, 5, "Заголовок", FgWhite, BorderBright, BgSurface, true)

	var buf bytes.Buffer
	if err := screen.Render(&buf); err != nil {
		t.Fatalf("screen.Render error: %v", err)
	}

	rendered := buf.String()
	if !strings.Contains(rendered, "Заголовок") {
		t.Errorf("expected box title to be rendered, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, string(BoxTL)) {
		t.Errorf("expected box corner %c to be rendered", BoxTL)
	}
}

func TestStatusBadge(t *testing.T) {
	text, fg, bg := StatusBadge("Активно")
	if !strings.Contains(text, "Активно") || fg == "" || bg == "" {
		t.Errorf("invalid StatusBadge for 'Активно': text=%q, fg=%q, bg=%q", text, fg, bg)
	}

	textDone, _, _ := StatusBadge("Завершено")
	if !strings.Contains(textDone, "Завершено") {
		t.Errorf("invalid StatusBadge for 'Завершено': text=%q", textDone)
	}
}

func TestKeyMatching(t *testing.T) {
	kEn := Key{Type: KeyRune, Rune: 'a'}
	if !kEn.Matches('a', 'ф') {
		t.Errorf("expected key 'a' to match ('a', 'ф')")
	}

	kRu := Key{Type: KeyRune, Rune: 'ф'}
	if !kRu.Matches('a', 'ф') {
		t.Errorf("expected key 'ф' to match ('a', 'ф')")
	}

	kUpper := Key{Type: KeyRune, Rune: 'A'}
	if !kUpper.Matches('a', 'ф') {
		t.Errorf("expected uppercase key 'A' to match ('a', 'ф')")
	}
}

func TestWrapText(t *testing.T) {
	text := "Очень длинное наименование события для проверки автоматического переноса строк"
	maxWidth := 20
	lines := WrapText(text, maxWidth)

	if len(lines) < 2 {
		t.Fatalf("expected multiple lines, got %d", len(lines))
	}

	for i, line := range lines {
		w := StringVisualWidth(line)
		if w > maxWidth {
			t.Errorf("line %d exceeds maxWidth %d: got %d (%q)", i, maxWidth, w, line)
		}
	}

	joined := strings.Join(lines, " ")
	for _, word := range strings.Fields(text) {
		if !strings.Contains(joined, word) {
			t.Errorf("word %q lost during wrapping", word)
		}
	}
}

func TestWrapTextLongWord(t *testing.T) {
	longWord := "СупермегадлинноеСловоБезПробеловКотороеНеПомещается"
	maxWidth := 15
	lines := WrapText(longWord, maxWidth)

	if len(lines) < 2 {
		t.Fatalf("expected chunked lines for long word, got %d", len(lines))
	}

	for i, line := range lines {
		w := StringVisualWidth(line)
		if w > maxWidth {
			t.Errorf("chunk %d exceeds maxWidth %d: got %d (%q)", i, maxWidth, w, line)
		}
	}
}

func TestScreenDrawWrappedText(t *testing.T) {
	screen := NewScreen(30, 10)
	text := "Первая строка и продолжение текста на второй и третьей строках"
	linesDrawn := screen.DrawWrappedText(2, 2, 18, 5, text, FgWhite, BgSurface, false, false, false)

	if linesDrawn < 2 {
		t.Errorf("expected at least 2 lines drawn, got %d", linesDrawn)
	}

	// Verify characters are placed in screen cells
	if screen.Cells[2][2].Ch == 0 {
		t.Errorf("expected cell (2,2) to have character")
	}
}

func TestModalYearCalendarInput(t *testing.T) {
	m := NewModalYearCalendar(2026)
	if m.year != 2026 {
		t.Fatalf("expected year 2026, got %d", m.year)
	}

	// Step 1: press 'y' to enter input mode
	m.HandleKey(Key{Type: KeyRune, Rune: 'y'}, nil)
	if !m.inputMode {
		t.Fatalf("expected inputMode to be true after pressing 'y'")
	}

	// Step 2: enter '2', '0', '3', '0'
	m.HandleKey(Key{Type: KeyRune, Rune: '2'}, nil)
	m.HandleKey(Key{Type: KeyRune, Rune: '0'}, nil)
	m.HandleKey(Key{Type: KeyRune, Rune: '3'}, nil)
	m.HandleKey(Key{Type: KeyRune, Rune: '0'}, nil)
	if m.inputYear != "2030" {
		t.Fatalf("expected inputYear '2030', got %q", m.inputYear)
	}

	// Step 3: Enter confirms and updates year
	m.HandleKey(Key{Type: KeyEnter}, nil)
	if m.inputMode {
		t.Fatalf("expected inputMode to be false after Enter")
	}
	if m.year != 2030 {
		t.Fatalf("expected year 2030, got %d", m.year)
	}
}

func TestModalSettingsEditNumber(t *testing.T) {
	cfg := &models.Config{
		DefaultMode:     "tui",
		WebPort:         8080,
		DefaultDuration: 60,
	}
	m := NewModalSettings(cfg)

	// Move down to port (selectedIdx = 1)
	m.HandleKey(Key{Type: KeyDown}, nil)
	if m.selectedIdx != 1 {
		t.Fatalf("expected selectedIdx 1, got %d", m.selectedIdx)
	}

	// Backspace and change port to 9090
	for i := 0; i < 4; i++ {
		m.HandleKey(Key{Type: KeyBackspace}, nil)
	}
	for _, r := range "9090" {
		m.HandleKey(Key{Type: KeyRune, Rune: r}, nil)
	}
	if m.portStr != "9090" {
		t.Fatalf("expected portStr '9090', got %q", m.portStr)
	}
}

func newTestApp(t *testing.T) *App {
	dir := t.TempDir()
	svc, err := calendar.NewService(context.Background(), dir, "mycal", models.SplitNone)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	cfg := &models.Config{
		DefaultMode: "tui",
		WebPort:     8080,
	}
	app := NewApp(svc, cfg, "", "vTest")
	return app
}

func TestModalDateRange(t *testing.T) {
	app := newTestApp(t)
	m := NewModalDateRange("01.10.2026", "10.10.2026")
	app.activeModal = m

	// Verify initial state
	if m.fromStr != "01.10.2026" || m.toStr != "10.10.2026" {
		t.Fatalf("unexpected initial dates: from=%q to=%q", m.fromStr, m.toStr)
	}

	// Press Tab to move to field 1 (toStr)
	m.HandleKey(Key{Type: KeyTab}, app)
	if m.focused != 1 {
		t.Fatalf("expected focus on field 1, got %d", m.focused)
	}

	// Press Tab to move to field 2 (Apply button)
	m.HandleKey(Key{Type: KeyTab}, app)
	if m.focused != 2 {
		t.Fatalf("expected focus on button 2, got %d", m.focused)
	}

	// Enter on Apply button
	m.HandleKey(Key{Type: KeyEnter}, app)
	if app.activeModal != nil {
		t.Fatalf("expected modal to close after Apply")
	}
	if app.periodMode != "custom" {
		t.Fatalf("expected periodMode custom, got %q", app.periodMode)
	}
	if app.startDate.Day() != 1 || app.startDate.Month() != 10 || app.startDate.Year() != 2026 {
		t.Fatalf("unexpected startDate: %v", app.startDate)
	}
	if app.endDate.Day() != 10 || app.endDate.Month() != 10 || app.endDate.Year() != 2026 {
		t.Fatalf("unexpected endDate: %v", app.endDate)
	}

	// Test error on invalid date
	m2 := NewModalDateRange("invalid", "10.10.2026")
	app.activeModal = m2
	m2.HandleKey(Key{Type: KeyEnter}, app) // from field 0 -> field 1
	m2.HandleKey(Key{Type: KeyEnter}, app) // submit from field 1
	if m2.errMsg == "" {
		t.Fatalf("expected error message on invalid date")
	}
	if app.activeModal != m2 {
		t.Fatalf("modal should remain open on error")
	}
}

func TestCalendarGridNavigationAndMonthWrap(t *testing.T) {
	app := newTestApp(t)
	app.focusPanel = PanelCalendar
	app.calYear = 2026
	app.calMonth = 10
	app.calDay = 1

	// Left from day 1 should wrap to previous month's last day (30 September)
	app.handleCalendarKey(Key{Type: KeyLeft})
	if app.calMonth != 9 || app.calDay != 30 || app.calYear != 2026 {
		t.Fatalf("expected 30.09.2026 after Left from 01.10, got %02d.%02d.%d", app.calDay, app.calMonth, app.calYear)
	}

	// Right from 30 September should wrap to 1 October
	app.handleCalendarKey(Key{Type: KeyRight})
	if app.calMonth != 10 || app.calDay != 1 {
		t.Fatalf("expected 01.10.2026 after Right from 30.09, got %02d.%02d.%d", app.calDay, app.calMonth, app.calYear)
	}

	// Up from day 1 should activate month header controls
	app.handleCalendarKey(Key{Type: KeyUp})
	if !app.calHeaderFocus {
		t.Fatalf("expected calHeaderFocus to be true after Up from day 1")
	}
	if app.calHeaderBtn != 0 {
		t.Fatalf("expected calHeaderBtn 0 (◄), got %d", app.calHeaderBtn)
	}

	// Space on ◄ changes month to 9
	app.handleCalendarKey(Key{Type: KeySpace})
	if app.calMonth != 9 {
		t.Fatalf("expected month 9 after clicking ◄, got %d", app.calMonth)
	}

	// Down moves back into day grid
	app.handleCalendarKey(Key{Type: KeyDown})
	if app.calHeaderFocus {
		t.Fatalf("expected calHeaderFocus false after Down")
	}

	// Global month change keys: '>' moves to month 10, '<' moves to month 9
	app.handleKey(Key{Type: KeyRune, Rune: '>'})
	if app.calMonth != 10 {
		t.Fatalf("expected month 10 after global '>', got %d", app.calMonth)
	}
	app.handleKey(Key{Type: KeyRune, Rune: '<'})
	if app.calMonth != 9 {
		t.Fatalf("expected month 9 after global '<', got %d", app.calMonth)
	}

	// Return to today via 't'
	app.handleCalendarKey(Key{Type: KeyRune, Rune: 't'})
	now := time.Now()
	if app.calYear != now.Year() || app.calMonth != int(now.Month()) || app.calDay != now.Day() {
		t.Fatalf("expected calendar reset to today, got %02d.%02d.%d", app.calDay, app.calMonth, app.calYear)
	}
}

func TestCalendarRangeSelectionWithSpace(t *testing.T) {
	app := newTestApp(t)
	app.focusPanel = PanelCalendar
	app.calYear = 2026
	app.calMonth = 10
	app.calDay = 10

	// 1. Press Space to anchor start date
	app.handleCalendarKey(Key{Type: KeySpace})
	if app.rangeAnchor == nil {
		t.Fatalf("expected rangeAnchor to be non-nil after Space")
	}
	if app.rangeAnchor.Day() != 10 {
		t.Fatalf("expected anchor day 10, got %d", app.rangeAnchor.Day())
	}

	// 2. Navigate 5 days to the right (to 15.10)
	for i := 0; i < 5; i++ {
		app.handleCalendarKey(Key{Type: KeyRight})
	}
	if app.calDay != 15 {
		t.Fatalf("expected day 15, got %d", app.calDay)
	}
	if app.periodMode != "custom" {
		t.Fatalf("expected periodMode custom during range selection, got %q", app.periodMode)
	}
	if app.startDate.Day() != 10 || app.endDate.Day() != 15 {
		t.Fatalf("expected range 10..15, got %d..%d", app.startDate.Day(), app.endDate.Day())
	}

	// 3. Press Space again to confirm selection
	app.handleCalendarKey(Key{Type: KeySpace})
	if app.rangeAnchor != nil {
		t.Fatalf("expected rangeAnchor to be nil after confirming range")
	}
	if app.startDate.Day() != 10 || app.endDate.Day() != 15 {
		t.Fatalf("expected confirmed range 10..15, got %d..%d", app.startDate.Day(), app.endDate.Day())
	}

	// 4. Press Esc to reset to single day
	app.handleCalendarKey(Key{Type: KeyEsc})
	if app.periodMode != "day" && app.periodMode != "all" {
		// single day mode
	}
}

func TestMultiCategoryFiltering(t *testing.T) {
	app := newTestApp(t)

	// Add 3 sessions with different categories
	_ = app.svc.Add(models.Session{ID: "20261015100001", Name: "Дело 1", Type: "Работа", Time: "10:00"})
	_ = app.svc.Add(models.Session{ID: "20261015110002", Name: "Дело 2", Type: "Отдых", Time: "11:00"})
	_ = app.svc.Add(models.Session{ID: "20261015120003", Name: "Дело 3", Type: "Спорт", Time: "12:00"})
	app.periodMode = "all"
	app.reloadSessions()

	if len(app.sessions) != 3 {
		t.Fatalf("expected 3 sessions initially, got %d", len(app.sessions))
	}

	// 1. Filter by "Работа"
	app.toggleTypeFilter("Работа")
	if len(app.sessions) != 1 || app.sessions[0].Type != "Работа" {
		t.Fatalf("expected 1 'Работа' session, got %d", len(app.sessions))
	}

	// 2. Add "Спорт" to filter (multi-category!)
	app.toggleTypeFilter("Спорт")
	if len(app.sessions) != 2 {
		t.Fatalf("expected 2 sessions ('Работа' and 'Спорт'), got %d", len(app.sessions))
	}
	for _, s := range app.sessions {
		if s.Type != "Работа" && s.Type != "Спорт" {
			t.Fatalf("unexpected session type %q in multi-filter", s.Type)
		}
	}

	// 3. Toggle off "Работа"
	app.toggleTypeFilter("Работа")
	if len(app.sessions) != 1 || app.sessions[0].Type != "Спорт" {
		t.Fatalf("expected 1 'Спорт' session, got %d", len(app.sessions))
	}

	// 4. Clear all category filters
	app.clearTypeFilters()
	if len(app.sessions) != 3 {
		t.Fatalf("expected 3 sessions after clearing filter, got %d", len(app.sessions))
	}
}

func TestRelativePresetPeriods(t *testing.T) {
	app := newTestApp(t)

	// Oct 15, 2026 is Thursday (week is Mon Oct 12 .. Sun Oct 18)
	_ = app.svc.Add(models.Session{ID: "20261015100000", Name: "Четверг", Type: "Работа", Time: "10:00"})
	_ = app.svc.Add(models.Session{ID: "20261012100000", Name: "Понедельник", Type: "Работа", Time: "10:00"})
	_ = app.svc.Add(models.Session{ID: "20261020100000", Name: "След. Неделя", Type: "Работа", Time: "10:00"})
	_ = app.svc.Add(models.Session{ID: "20261105100000", Name: "Ноябрь", Type: "Работа", Time: "10:00"})

	// Focus calendar on 15.10.2026
	app.calYear = 2026
	app.calMonth = 10
	app.calDay = 15

	// 1. Preset "day": should only match Oct 15
	app.periodMode = "day"
	app.reloadSessions()
	if len(app.sessions) != 1 || app.sessions[0].ID != "20261015100000" {
		t.Fatalf("expected only 15.10 session, got %d", len(app.sessions))
	}

	// 2. Preset "week": should match Mon 12.10 and Thu 15.10
	app.periodMode = "week"
	app.reloadSessions()
	if len(app.sessions) != 2 {
		t.Fatalf("expected 2 sessions for week of 15.10, got %d", len(app.sessions))
	}

	// 3. Preset "month": should match 12.10, 15.10, 20.10 (not Nov 05)
	app.periodMode = "month"
	app.reloadSessions()
	if len(app.sessions) != 3 {
		t.Fatalf("expected 3 sessions for October 2026, got %d", len(app.sessions))
	}

	// 4. Preset "all": should match all 4 sessions
	app.periodMode = "all"
	app.reloadSessions()
	if len(app.sessions) != 4 {
		t.Fatalf("expected 4 sessions for 'all', got %d", len(app.sessions))
	}
}

func TestIsDayInFilter(t *testing.T) {
	app := newTestApp(t)
	app.calYear = 2026
	app.calMonth = 10
	app.calDay = 15

	// When periodMode is "all", no day should be highlighted
	app.periodMode = "all"
	if app.isDayInFilter(2026, 10, 15) {
		t.Fatalf("isDayInFilter must return false for all days when periodMode == 'all'")
	}

	// When periodMode is "week", week of 15.10 (days 12..18) must return true
	app.periodMode = "week"
	if !app.isDayInFilter(2026, 10, 12) || !app.isDayInFilter(2026, 10, 15) || !app.isDayInFilter(2026, 10, 18) {
		t.Fatalf("expected days 12, 15, 18 to be in week filter")
	}
	if app.isDayInFilter(2026, 10, 11) || app.isDayInFilter(2026, 10, 19) {
		t.Fatalf("days 11 and 19 should not be in week filter")
	}

	// When periodMode is "month", all days of October must return true
	app.periodMode = "month"
	if !app.isDayInFilter(2026, 10, 1) || !app.isDayInFilter(2026, 10, 31) {
		t.Fatalf("expected days 1 and 31 to be in month filter")
	}
	if app.isDayInFilter(2026, 11, 1) {
		t.Fatalf("November 1 should not be in October month filter")
	}
}
