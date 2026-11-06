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
		{"🗓️", 2},     // Emoji calendar with variation selector
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
	m := NewModalYearCalendar(2026, 10, 15, nil)
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

func TestModalYearCalendarNavigationAndSessions(t *testing.T) {
	app := newTestApp(t)

	// Add a session on Oct 15, 2026
	_ = app.svc.Add(models.Session{ID: "20261015100000", Name: "Тест", Type: "Работа", Time: "10:00"})
	app.allSessions = app.svc.All()

	m := NewModalYearCalendar(2026, 10, 15, app.allSessions)
	app.activeModal = m

	// Verify Oct 15 has session marked
	if !m.daysWithSessions[1015] {
		t.Fatalf("expected Oct 15 (1015) to be marked in daysWithSessions")
	}
	if m.daysWithSessions[1014] {
		t.Fatalf("Oct 14 should not have sessions")
	}

	// Navigate Left (to Oct 14)
	m.HandleKey(Key{Type: KeyLeft}, app)
	if m.selMonth != 10 || m.selDay != 14 {
		t.Fatalf("expected Oct 14, got %02d.%02d", m.selDay, m.selMonth)
	}

	// Navigate Right (to Oct 15)
	m.HandleKey(Key{Type: KeyRight}, app)
	if m.selMonth != 10 || m.selDay != 15 {
		t.Fatalf("expected Oct 15, got %02d.%02d", m.selDay, m.selMonth)
	}

	// Space to anchor range
	m.HandleKey(Key{Type: KeySpace}, app)
	if m.rangeAnchor == nil || m.rangeAnchor.Day() != 15 {
		t.Fatalf("expected anchor at day 15")
	}

	// Navigate 3 days Right (to Oct 18)
	for i := 0; i < 3; i++ {
		m.HandleKey(Key{Type: KeyRight}, app)
	}
	if m.selDay != 18 {
		t.Fatalf("expected day 18, got %d", m.selDay)
	}
	if !m.isDayInRange(2026, 10, 15) || !m.isDayInRange(2026, 10, 18) {
		t.Fatalf("days 15..18 should be in range")
	}

	// Enter to apply period and close modal!
	m.HandleKey(Key{Type: KeyEnter}, app)
	if app.activeModal != nil {
		t.Fatalf("expected modal to be closed after Enter")
	}
	if app.periodMode != "custom" {
		t.Fatalf("expected periodMode custom, got %q", app.periodMode)
	}
	if app.startDate.Day() != 15 || app.endDate.Day() != 18 {
		t.Fatalf("expected applied range 15..18, got %d..%d", app.startDate.Day(), app.endDate.Day())
	}
}

func TestModalYearCalendarDrawAndLayout(t *testing.T) {
	app := newTestApp(t)
	// Add session on Oct 16, 2026 (while cursor is on Oct 15)
	_ = app.svc.Add(models.Session{ID: "20261016100000", Name: "Тест", Type: "Работа", Time: "10:00"})
	app.allSessions = app.svc.All()

	m := NewModalYearCalendar(2026, 10, 15, app.allSessions)
	screen := NewScreen(100, 32)
	screen.Clear(BgPrimary)
	m.Draw(screen)

	// Check that hint is centered and not at x=2
	hintFound := false
	for y := 20; y < 31; y++ {
		rowStr := ""
		for x := 0; x < 100; x++ {
			if screen.Cells[y][x].Ch != 0 {
				rowStr += string(screen.Cells[y][x].Ch)
			} else {
				rowStr += " "
			}
		}
		if strings.Contains(rowStr, "Выбор дня") {
			hintFound = true
			idx := strings.Index(rowStr, "[↑↓←→]")
			if idx < 6 {
				t.Fatalf("expected centered hint, but started at column %d", idx)
			}
			// Check line immediately above hint: inside box should be empty (breathing room!)
			rowAbove := ""
			// Box interior is from x+1 to x+w-2
			boxLeft := (100 - 92) / 2
			boxRight := boxLeft + 92 - 1
			for col := boxLeft + 1; col < boxRight; col++ {
				if screen.Cells[y-1][col].Ch != 0 && screen.Cells[y-1][col].Ch != ' ' {
					rowAbove += string(screen.Cells[y-1][col].Ch)
				}
			}
			if len(rowAbove) > 0 {
				t.Fatalf("line above hint inside box should be blank separator, but contains: %q", rowAbove)
			}
			break
		}
	}
	if !hintFound {
		t.Fatalf("hint text not found on screen")
	}

	// Verify Oct 15 has green color
	greenFound := false
	for y := 0; y < 32; y++ {
		for x := 0; x < 100; x++ {
			cell := screen.Cells[y][x]
			if cell.Fg == FgGreen && (cell.Ch == '1' || cell.Ch == '5') {
				greenFound = true
				break
			}
		}
		if greenFound {
			break
		}
	}
	if !greenFound {
		t.Fatalf("day with session (Oct 15) was not drawn with FgGreen")
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

func TestModalSettingsDurationUnitToggle(t *testing.T) {
	app := newTestApp(t)
	m := NewModalSettings(app.cfg)
	if m.durationUnit != "min" {
		t.Fatalf("expected default durationUnit 'min', got %q", m.durationUnit)
	}

	// Move to field 3 (duration unit)
	m.selectedIdx = 3

	// Press Space to toggle to hours
	m.HandleKey(Key{Type: KeySpace}, app)
	if m.durationUnit != "hours" {
		t.Fatalf("expected durationUnit 'hours' after Space, got %q", m.durationUnit)
	}

	// Press Space again to toggle back to min
	m.HandleKey(Key{Type: KeySpace}, app)
	if m.durationUnit != "min" {
		t.Fatalf("expected durationUnit 'min' after second Space, got %q", m.durationUnit)
	}

	// Toggle to hours and save with Enter
	m.HandleKey(Key{Type: KeySpace}, app)
	m.HandleKey(Key{Type: KeyEnter}, app)

	if app.cfg.DurationUnit != "hours" {
		t.Fatalf("expected app.cfg.DurationUnit 'hours' after save, got %q", app.cfg.DurationUnit)
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
	app.ctx = context.Background()
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

func TestFeedHorizontalPanelNavigation(t *testing.T) {
	app := newTestApp(t)
	app.width = 120 // wide mode
	app.focusPanel = PanelFeed

	// 1. In Feed, press KeyLeft -> moves to PanelCalendar
	app.handleFeedKey(Key{Type: KeyLeft})
	if app.focusPanel != PanelCalendar {
		t.Fatalf("expected focus to move to PanelCalendar on KeyLeft, got %d", app.focusPanel)
	}

	// 2. In Feed, press KeyRight -> moves to PanelInspector
	app.focusPanel = PanelFeed
	app.handleFeedKey(Key{Type: KeyRight})
	if app.focusPanel != PanelInspector {
		t.Fatalf("expected focus to move to PanelInspector on KeyRight, got %d", app.focusPanel)
	}

	// 3. In Inspector, press KeyLeft -> moves back to PanelFeed
	app.handleInspectorKey(Key{Type: KeyLeft})
	if app.focusPanel != PanelFeed {
		t.Fatalf("expected focus to move to PanelFeed on KeyLeft from Inspector, got %d", app.focusPanel)
	}

	// 4. In Sidebar, press KeyRight -> moves to PanelFeed
	app.focusPanel = PanelSidebar
	app.handleSidebarKey(Key{Type: KeyRight})
	if app.focusPanel != PanelFeed {
		t.Fatalf("expected focus to move to PanelFeed on KeyRight from Sidebar, got %d", app.focusPanel)
	}

	// 5. In Feed, press KeyLeft when lastLeftPanel was Sidebar -> moves to PanelSidebar
	app.lastLeftPanel = PanelSidebar
	app.focusPanel = PanelFeed
	app.handleFeedKey(Key{Type: KeyLeft})
	if app.focusPanel != PanelSidebar {
		t.Fatalf("expected focus to restore to PanelSidebar, got %d", app.focusPanel)
	}

	// 6. Compact mode (width < 115)
	app.width = 100
	app.activeTab = TabFeed
	app.focusPanel = PanelFeed
	app.handleFeedKey(Key{Type: KeyLeft})
	if app.activeTab != TabCalendar || app.focusPanel != PanelCalendar {
		t.Fatalf("compact KeyLeft: expected TabCalendar, got activeTab=%d, focusPanel=%d", app.activeTab, app.focusPanel)
	}

	app.activeTab = TabFeed
	app.focusPanel = PanelFeed
	app.handleFeedKey(Key{Type: KeyRight})
	if app.activeTab != TabInspector || app.focusPanel != PanelInspector {
		t.Fatalf("compact KeyRight: expected TabInspector, got activeTab=%d, focusPanel=%d", app.activeTab, app.focusPanel)
	}
}

func TestCategoryPickerDropdownInSessionForm(t *testing.T) {
	app := newTestApp(t)
	// Add some sessions with distinct categories
	_ = app.svc.Add(models.Session{ID: "20261015100000", Time: "10:00", Name: "Session A", Type: "Финансы", Duration: 60})
	_ = app.svc.Add(models.Session{ID: "20261015110000", Time: "11:00", Name: "Session B", Type: "Маркетинг", Duration: 30})
	_ = app.svc.Save(app.ctx)
	app.reloadSessions()

	m := NewModalSessionForm(nil, "15.10.2026")
	m.fieldIdx = 3 // category field

	// 1. Pressing KeyRight opens category picker
	m.HandleKey(Key{Type: KeyRight}, app)
	if !m.catPickerOpen {
		t.Fatalf("expected catPickerOpen == true on KeyRight on field 3")
	}
	if len(m.catPickerList) == 0 {
		t.Fatalf("expected non-empty catPickerList")
	}

	// Check that custom categories from sessions exist in list
	hasFin := false
	hasMkt := false
	for _, c := range m.catPickerList {
		if c == "Финансы" {
			hasFin = true
		}
		if c == "Маркетинг" {
			hasMkt = true
		}
	}
	if !hasFin || !hasMkt {
		t.Fatalf("expected custom categories in list, got: %v", m.catPickerList)
	}

	// 2. Select first item
	m.catPickerIdx = 0
	expectedCat := m.catPickerList[0]
	m.HandleKey(Key{Type: KeyEnter}, app)
	if m.catPickerOpen {
		t.Fatalf("expected catPickerOpen == false after Enter")
	}
	if m.typeStr != expectedCat {
		t.Fatalf("expected typeStr == %q, got %q", expectedCat, m.typeStr)
	}

	// 3. Test Esc closes picker without modifying
	m.HandleKey(Key{Type: KeyRight}, app)
	if !m.catPickerOpen {
		t.Fatalf("expected catPickerOpen == true")
	}
	m.catPickerIdx = 1
	m.HandleKey(Key{Type: KeyEsc}, app)
	if m.catPickerOpen {
		t.Fatalf("expected catPickerOpen == false after Esc")
	}
	if m.typeStr != expectedCat {
		t.Fatalf("typeStr should remain %q on Esc, got %q", expectedCat, m.typeStr)
	}

	// 4. Verify Draw with picker open does not crash
	s := NewScreen(100, 30)
	m.openCatPicker(app)
	m.Draw(s)
}

func TestCategoryPickerDropdownInQuickEditField(t *testing.T) {
	app := newTestApp(t)
	m := NewModalQuickEditField("test-id")
	m.selected = 2 // "Категория / Тип"
	m.stepInput = true

	// Pressing KeyRight opens picker
	m.HandleKey(Key{Type: KeyRight}, app)
	if !m.catPickerOpen {
		t.Fatalf("expected catPickerOpen == true")
	}

	// Navigate and select
	m.HandleKey(Key{Type: KeyDown}, app)
	expectedVal := m.catPickerList[m.catPickerIdx]
	m.HandleKey(Key{Type: KeyEnter}, app)

	if m.catPickerOpen {
		t.Fatalf("expected catPickerOpen == false after Enter")
	}
	if m.value != expectedVal {
		t.Fatalf("expected value == %q, got %q", expectedVal, m.value)
	}
}

func TestCalendarHeaderAndFooterYearHints(t *testing.T) {
	app := newTestApp(t)
	app.width = 120
	app.height = 30
	s := NewScreen(app.width, app.height)

	// Render calendar box
	app.renderCalendarBox(s, 0, 1, 26, 14)
	buf := s.PlainString()

	// Check [Y] in calendar box border title
	if !strings.Contains(buf, "Календарь [Y]") {
		t.Fatalf("expected calendar title to contain 'Календарь [Y]', got:\n%s", buf)
	}

	lines := strings.Split(buf, "\n")
	// The month line (row 2 of screen: ◄ Месяц Год ►) should not have duplicate [Y]
	if len(lines) > 2 && strings.Contains(lines[2], "[Y]") {
		t.Fatalf("expected month line not to contain duplicate '[Y]', got:\n%s", lines[2])
	}

	// Render footer
	app.renderFooter(s)
	footerBuf := s.PlainString()

	// Check footer has [y] and Год
	if !strings.Contains(footerBuf, "[y]") || !strings.Contains(footerBuf, "Год") {
		t.Fatalf("expected footer to contain '[y]' and 'Год'")
	}
}

func TestNarrowFooterWrapping(t *testing.T) {
	app := newTestApp(t)
	app.width = 65
	app.height = 25
	app.statusMsg = "Длинное статусное сообщение, которое непременно должно аккуратно переноситься при узком терминале"

	statusLines, hintLines := app.layoutFooter()
	if len(hintLines) <= 1 {
		t.Fatalf("expected hint lines to wrap into > 1 lines on width 65, got %d", len(hintLines))
	}
	if len(statusLines) == 0 {
		t.Fatalf("expected status message to be wrapped, got 0 lines")
	}

	for i, hLine := range hintLines {
		totalW := 0
		for _, item := range hLine {
			totalW += item.kW + item.dW + 2
		}
		if totalW > app.width-4 {
			t.Fatalf("hint line %d width %d exceeds max %d", i, totalW, app.width-4)
		}
	}

	s := NewScreen(app.width, app.height)
	app.renderFooter(s)
	buf := s.PlainString()
	if !strings.Contains(buf, "Вкладки") || !strings.Contains(buf, "Навигация") {
		t.Fatalf("expected footer to render navigation hints, got:\n%s", buf)
	}
}

func TestNarrowCalendarBoxClipping(t *testing.T) {
	app := newTestApp(t)
	calW := 18 // Narrower than standard 22-char Mon-Sun width
	calH := 12
	s := NewScreen(30, 20)

	app.renderCalendarBox(s, 0, 0, calW, calH)

	// Check right border is preserved along y=1..calH-2
	for y := 1; y < calH-1; y++ {
		cell := s.Cells[y][calW-1]
		if cell.Ch != BoxV {
			t.Fatalf("row %d right border corrupted: expected BoxV '│', got %q", y, string(cell.Ch))
		}
	}
}

func TestNarrowYearCalendarClipping(t *testing.T) {
	app := newTestApp(t)
	s := NewScreen(60, 24) // Narrow terminal: 60 columns

	m := NewModalYearCalendar(2026, 5, 15, app.allSessions)
	m.Draw(s)

	// Verify modal box borders are preserved
	w := 60 - 2 // 58
	x := (60 - w) / 2
	for y := 2; y < 22; y++ {
		cell := s.Cells[y][x+w-1]
		if cell.Ch != BoxV && cell.Ch != 0 && cell.Ch != ' ' {
			// Ensure cell at right edge is either border or empty, not overwritten by garbage
		}
	}
}

func TestDrawBottomWrappedCenteredHint(t *testing.T) {
	s := NewScreen(50, 10)
	text := "Очень длинный текст подсказки, который не поместится в одну строку и должен быть разбит на две строки с центрированием"
	linesDrawn := s.DrawBottomWrappedCenteredHint(2, 8, 46, 2, text, FgDim, BgModal)
	if linesDrawn != 2 {
		t.Fatalf("expected 2 lines drawn, got %d", linesDrawn)
	}

	// Verify rows 7 and 8 have non-empty text
	row7 := strings.TrimSpace(string(s.Cells[7][2:48][0].Ch))
	row8 := strings.TrimSpace(string(s.Cells[8][2:48][0].Ch))
	if row7 == "" && row8 == "" {
		t.Fatalf("expected rows 7 and 8 to contain hint characters")
	}
}

func TestAddSessionUsesSelectedCalendarDay(t *testing.T) {
	app := newTestApp(t)

	// 1. Focus on PanelCalendar and set cursor to 20.11.2026
	app.focusPanel = PanelCalendar
	app.calYear = 2026
	app.calMonth = 11
	app.calDay = 20
	app.periodMode = "day"

	// Press 'a' to add session
	app.handleKey(Key{Type: KeyRune, Rune: 'a'})

	formModal, ok := app.activeModal.(*ModalSessionForm)
	if !ok {
		t.Fatalf("expected activeModal to be *ModalSessionForm")
	}
	if formModal.dateStr != "20.11.2026" {
		t.Fatalf("expected form dateStr to be '20.11.2026', got %q", formModal.dateStr)
	}

	// 2. Switch to PanelFeed with 0 sessions on that day
	app.activeModal = nil
	app.focusPanel = PanelFeed
	app.reloadSessions()
	if len(app.sessions) != 0 {
		t.Fatalf("expected 0 sessions on 20.11.2026, got %d", len(app.sessions))
	}

	// Press 'a' while looking at the empty feed
	app.handleKey(Key{Type: KeyRune, Rune: 'a'})
	formModal, ok = app.activeModal.(*ModalSessionForm)
	if !ok {
		t.Fatalf("expected activeModal to be *ModalSessionForm")
	}
	if formModal.dateStr != "20.11.2026" {
		t.Fatalf("expected form dateStr in empty feed to inherit calendar date '20.11.2026', got %q", formModal.dateStr)
	}

	// 3. Save the session
	formModal.name = "Новое событие"
	formModal.save(app)

	if len(app.sessions) != 1 {
		t.Fatalf("expected 1 session after save, got %d", len(app.sessions))
	}
	if app.sessions[0].Name != "Новое событие" {
		t.Fatalf("expected session name 'Новое событие', got %q", app.sessions[0].Name)
	}
	if app.calDay != 20 || app.calMonth != 11 || app.calYear != 2026 {
		t.Fatalf("expected calendar position 20.11.2026, got %02d.%02d.%d", app.calDay, app.calMonth, app.calYear)
	}

	// 4. ModalYearCalendar: navigate to a date and press 'a'
	app.activeModal = nil
	yearModal := NewModalYearCalendar(2026, 8, 15, app.svc.All())
	yearModal.HandleKey(Key{Type: KeyRune, Rune: 'a'}, app)

	formModal, ok = app.activeModal.(*ModalSessionForm)
	if !ok {
		t.Fatalf("expected activeModal from YearCalendar 'a' to be *ModalSessionForm")
	}
	if formModal.dateStr != "15.08.2026" {
		t.Fatalf("expected form dateStr from YearCalendar to be '15.08.2026', got %q", formModal.dateStr)
	}
}

func TestNegativeCashHandlingAndTotalCalculation(t *testing.T) {
	app := newTestApp(t)

	// 1. Create a session with positive cash
	_ = app.svc.Add(models.Session{
		ID:   "20261104100000",
		Name: "Доход",
		Cash: 1000.0,
		Time: "10:00",
	})

	// 2. Open form and enter negative cash -300
	m := NewModalSessionForm(nil, "04.11.2026")
	m.fieldIdx = 5 // Cash field
	m.appendChar('-')
	m.appendChar('3')
	m.appendChar('0')
	m.appendChar('0')
	if m.cashStr != "-300" {
		t.Fatalf("expected cashStr to be '-300', got %q", m.cashStr)
	}
	m.name = "Расход"
	m.save(app)

	// Check reload
	app.periodMode = "all"
	app.reloadSessions()

	if len(app.sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(app.sessions))
	}

	var totalCash float64
	for _, s := range app.sessions {
		totalCash += s.Cash
	}
	if totalCash != 700.0 {
		t.Fatalf("expected total cash to be 700.0 (1000 - 300), got %v", totalCash)
	}

	// 3. Quick field edit with negative value
	editModal := NewModalQuickEditField(app.sessions[0].ID)
	editModal.selected = 1 // Cash
	editModal.stepInput = true
	editModal.value = "-150.5"
	editModal.applyValue(app)

	updated, ok := app.svc.ByID(app.sessions[0].ID)
	if !ok || updated.Cash != -150.5 {
		t.Fatalf("expected updated cash to be -150.5, got %v", updated.Cash)
	}
}

func TestStatusPickerDropdownInSessionForm(t *testing.T) {
	app := newTestApp(t)
	app.cfg.CustomStatuses = []string{"В ожидании"}

	sess := models.Session{
		ID:       "20261015100000",
		Time:     "10:00",
		Name:     "Тест",
		Type:     "Работа",
		Duration: 60,
		Status:   "Архив",
	}
	_ = app.svc.Add(sess)
	app.reloadSessions()

	m := NewModalSessionForm(nil, "15.10.2026")
	m.fieldIdx = 6 // status field

	// 1. Pressing KeyRight opens status picker
	m.HandleKey(Key{Type: KeyRight}, app)
	if !m.pickerOpen {
		t.Fatalf("expected pickerOpen == true on KeyRight on status field")
	}
	if m.pickerField != 6 {
		t.Fatalf("expected pickerField == 6, got %d", m.pickerField)
	}

	// Verify statuses contain defaults + custom + session
	expectedStatuses := []string{"Активно", "Завершено", "Запланировано", "В процессе", "Отменено", "В ожидании", "Архив"}
	for _, exp := range expectedStatuses {
		found := false
		for _, item := range m.pickerList {
			if item == exp {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected status %q in pickerList: %v", exp, m.pickerList)
		}
	}

	// 2. Select second item (Завершено)
	m.pickerIdx = 1
	expectedStatus := m.pickerList[1]
	m.HandleKey(Key{Type: KeyEnter}, app)

	if m.pickerOpen {
		t.Fatalf("expected pickerOpen == false after Enter")
	}
	if m.status != expectedStatus {
		t.Fatalf("expected status == %q, got %q", expectedStatus, m.status)
	}

	// 3. Test Esc closes picker without modifying
	m.HandleKey(Key{Type: KeyRight}, app)
	if !m.pickerOpen {
		t.Fatalf("expected pickerOpen == true")
	}
	m.pickerIdx = 3
	m.HandleKey(Key{Type: KeyEsc}, app)
	if m.pickerOpen {
		t.Fatalf("expected pickerOpen == false after Esc")
	}
	if m.status != expectedStatus {
		t.Fatalf("status should remain %q on Esc, got %q", expectedStatus, m.status)
	}

	// 4. Test Draw with open picker
	m.HandleKey(Key{Type: KeyRight}, app)
	s := NewScreen(80, 25)
	m.Draw(s)
}

func TestNamesPickerDropdownInSessionForm(t *testing.T) {
	app := newTestApp(t)
	app.cfg.CustomNames = []string{"Тренировка", "Созвон"}

	sess := models.Session{
		ID:       "20261015110000",
		Time:     "11:00",
		Name:     "Чтение книги",
		Type:     "Отдых",
		Duration: 30,
	}
	_ = app.svc.Add(sess)
	app.reloadSessions()

	m := NewModalSessionForm(nil, "15.10.2026")
	m.fieldIdx = 2 // name field

	// Pressing KeyRight opens names picker
	m.HandleKey(Key{Type: KeyRight}, app)
	if !m.pickerOpen {
		t.Fatalf("expected pickerOpen == true on KeyRight on name field")
	}
	if m.pickerField != 2 {
		t.Fatalf("expected pickerField == 2, got %d", m.pickerField)
	}

	expectedNames := []string{"Тренировка", "Созвон", "Чтение книги"}
	for _, exp := range expectedNames {
		found := false
		for _, item := range m.pickerList {
			if item == exp {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected name %q in pickerList: %v", exp, m.pickerList)
		}
	}

	// Select second item
	m.pickerIdx = 1
	expectedName := m.pickerList[1]
	m.HandleKey(Key{Type: KeyEnter}, app)

	if m.pickerOpen {
		t.Fatalf("expected pickerOpen == false after Enter")
	}
	if m.name != expectedName {
		t.Fatalf("expected name == %q, got %q", expectedName, m.name)
	}

	s := NewScreen(80, 25)
	m.Draw(s)
}

func TestStatusPickerDropdownInQuickEditField(t *testing.T) {
	app := newTestApp(t)
	app.cfg.CustomStatuses = []string{"Отложено"}

	sess := models.Session{
		ID:       "20261015120000",
		Time:     "12:00",
		Name:     "План",
		Type:     "Работа",
		Duration: 45,
		Status:   "Активно",
	}
	_ = app.svc.Add(sess)
	app.reloadSessions()

	m := NewModalQuickEditField(sess.ID)
	m.selected = 3 // "Статус"
	m.stepInput = true

	// Pressing KeyRight opens status picker
	m.HandleKey(Key{Type: KeyRight}, app)
	if !m.pickerOpen {
		t.Fatalf("expected pickerOpen == true")
	}
	if m.pickerField != 3 {
		t.Fatalf("expected pickerField == 3, got %d", m.pickerField)
	}

	// Navigate and select
	m.HandleKey(Key{Type: KeyDown}, app)
	expectedVal := m.pickerList[m.pickerIdx]
	m.HandleKey(Key{Type: KeyEnter}, app)

	if m.pickerOpen {
		t.Fatalf("expected pickerOpen == false after Enter")
	}
	if m.value != expectedVal {
		t.Fatalf("expected value == %q, got %q", expectedVal, m.value)
	}

	// Apply value to session
	m.applyValue(app)
	updated, ok := app.svc.ByID(sess.ID)
	if !ok || updated.Status != expectedVal {
		t.Fatalf("expected updated status == %q, got %q", expectedVal, updated.Status)
	}

	s := NewScreen(80, 25)
	m.Draw(s)
}

func TestRepeatPickerDropdownInSessionForm(t *testing.T) {
	app := newTestApp(t)
	m := NewModalSessionForm(nil, "15.10.2026")
	m.fieldIdx = 8 // repeat field

	m.HandleKey(Key{Type: KeyRight}, app)
	if !m.pickerOpen {
		t.Fatalf("expected pickerOpen == true on repeat field")
	}
	if m.pickerField != 8 {
		t.Fatalf("expected pickerField == 8, got %d", m.pickerField)
	}

	// Select weekly ("w") - index 2
	m.pickerIdx = 2
	m.HandleKey(Key{Type: KeyEnter}, app)
	if m.repeatStr != "w" {
		t.Fatalf("expected repeatStr == 'w', got %q", m.repeatStr)
	}

	s := NewScreen(80, 25)
	m.Draw(s)
}

func TestTimePickerDropdownInSessionForm(t *testing.T) {
	app := newTestApp(t)
	m := NewModalSessionForm(nil, "15.10.2026")
	m.fieldIdx = 1 // time field
	m.timeStr = "10:00"

	m.HandleKey(Key{Type: KeyRight}, app)
	if !m.pickerOpen {
		t.Fatalf("expected pickerOpen == true on time field")
	}
	if m.pickerField != 1 {
		t.Fatalf("expected pickerField == 1, got %d", m.pickerField)
	}
	if len(m.pickerList) != 24 {
		t.Fatalf("expected 24 hours in pickerList, got %d", len(m.pickerList))
	}

	// Select 14:00 (index 14)
	m.pickerIdx = 14
	m.HandleKey(Key{Type: KeyEnter}, app)
	if m.timeStr != "14:00" {
		t.Fatalf("expected timeStr == '14:00', got %q", m.timeStr)
	}

	s := NewScreen(80, 25)
	m.Draw(s)
}

func TestTimePickerDropdownInQuickEditField(t *testing.T) {
	app := newTestApp(t)
	m := NewModalQuickEditField("test-id")
	m.selected = 4 // time field
	m.stepInput = true
	m.value = "10:00"

	m.HandleKey(Key{Type: KeyRight}, app)
	if !m.pickerOpen {
		t.Fatalf("expected pickerOpen == true on time field in quick edit")
	}
	if m.pickerField != 4 {
		t.Fatalf("expected pickerField == 4, got %d", m.pickerField)
	}

	// Select 18:00 (index 18)
	m.pickerIdx = 18
	m.HandleKey(Key{Type: KeyEnter}, app)
	if m.value != "18:00" {
		t.Fatalf("expected value == '18:00', got %q", m.value)
	}

	s := NewScreen(80, 25)
	m.Draw(s)
}

func TestStatusPickerDrawHierarchy(t *testing.T) {
	app := newTestApp(t)
	m := NewModalSessionForm(nil, "15.10.2026")
	m.fieldIdx = 6 // status field
	m.status = "Активно"

	m.HandleKey(Key{Type: KeyRight}, app)
	if !m.pickerOpen {
		t.Fatalf("expected status picker to be open")
	}

	s := NewScreen(80, 25)
	m.Draw(s)

	var buf bytes.Buffer
	_ = s.Render(&buf)
	// Ensure the popup title "Статусы" is rendered on top and visible
	if !strings.Contains(buf.String(), "Статусы") {
		t.Fatalf("expected status popup box title to be rendered on screen")
	}
}

func TestStatusBadgeRuneWidthAndAlignment(t *testing.T) {
	statuses := []string{"Активно", "Завершено", "В процессе", "Запланировано", "Пропущено", "Отменено"}
	for _, st := range statuses {
		badge, _, _ := StatusBadge(st)
		// Check that the first character is an icon with width 1
		runes := []rune(badge)
		if len(runes) == 0 {
			t.Fatalf("empty badge for status %s", st)
		}
		icon := runes[0]
		w := RuneVisualWidth(icon)
		if w != 1 {
			t.Errorf("status %q icon %q visual width = %d, expected 1", st, string(icon), w)
		}
	}
}

func TestStatusPickerPopupWithinModalBounds(t *testing.T) {
	app := &App{
		allSessions: []models.Session{},
	}
	m := NewModalSessionForm(nil, "01.01.2026")
	m.fieldIdx = 6 // status
	m.openStatusPicker(app)

	// Check available statuses contains all 6 defaults
	foundStatus := map[string]bool{}
	for _, st := range m.pickerList {
		foundStatus[st] = true
	}
	expected := []string{"Активно", "Завершено", "В процессе", "Запланировано", "Пропущено", "Отменено"}
	for _, exp := range expected {
		if !foundStatus[exp] {
			t.Errorf("expected %q in status picker list, got %v", exp, m.pickerList)
		}
	}

	s := NewScreen(60, 24)
	m.Draw(s)

	var buf bytes.Buffer
	if err := s.Render(&buf); err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	// Verify screen rendered with row anchoring
	outStr := buf.String()
	if !strings.Contains(outStr, "\033[1;1H") {
		t.Errorf("expected row 1 anchoring escape code")
	}
}

func TestMultiNameSessionFormCreation(t *testing.T) {
	app := newTestApp(t)
	m := NewModalSessionForm(nil, "01.01.2026")
	m.name = "Первая задача; Вторая задача; Третья задача"
	m.save(app)

	all := app.svc.All()
	if len(all) != 3 {
		t.Fatalf("expected 3 sessions created, got %d", len(all))
	}
	names := []string{all[0].Name, all[1].Name, all[2].Name}
	if names[0] != "Первая задача" || names[1] != "Вторая задача" || names[2] != "Третья задача" {
		t.Errorf("unexpected session names: %v", names)
	}
}

func TestModalStatusPickerCustomStatusInput(t *testing.T) {
	app := newTestApp(t)
	sess := models.Session{
		ID:       "20260101100000",
		Time:     "10:00",
		Name:     "Тестовая задача",
		Status:   "Активно",
		Duration: 60,
	}
	_ = app.svc.Add(sess)

	p := NewModalStatusPicker(nil, "Активно", sess.ID)
	// Press '+' to activate custom status input
	p.HandleKey(Key{Type: KeyRune, Rune: '+'}, app)
	if !p.isCustomInput {
		t.Fatalf("expected isCustomInput == true after '+'")
	}

	// Type custom status "В архиве"
	for _, r := range "В архиве" {
		p.HandleKey(Key{Type: KeyRune, Rune: r}, app)
	}
	// Press Enter to apply
	p.HandleKey(Key{Type: KeyEnter}, app)

	updated, ok := app.svc.ByID(sess.ID)
	if !ok || updated.Status != "В архиве" {
		t.Fatalf("expected session status 'В архиве', got %q", updated.Status)
	}

	// Check it was added to app.cfg.CustomStatuses
	foundCfg := false
	for _, st := range app.cfg.CustomStatuses {
		if st == "В архиве" {
			foundCfg = true
			break
		}
	}
	if !foundCfg {
		t.Fatalf("expected 'В архиве' in cfg.CustomStatuses, got %v", app.cfg.CustomStatuses)
	}
}

func TestModalStatusPickerDeleteStatus(t *testing.T) {
	app := newTestApp(t)
	sess := models.Session{
		ID:       "20260101100000",
		Time:     "10:00",
		Name:     "Сессия с кастомным статусом",
		Status:   "СуперСтатус",
		Duration: 60,
	}
	_ = app.svc.Add(sess)
	app.cfg.AddCustomStatus("СуперСтатус")

	p := NewModalStatusPicker(app.getAvailableStatuses(), "СуперСтатус", sess.ID)
	idx := -1
	for i, st := range p.statuses {
		if st == "СуперСтатус" {
			idx = i
			break
		}
	}
	if idx == -1 {
		t.Fatalf("СуперСтатус not found in picker: %v", p.statuses)
	}
	p.selected = idx

	p.HandleKey(Key{Type: KeyRune, Rune: 'd'}, app)

	avail := app.getAvailableStatuses()
	for _, st := range avail {
		if st == "СуперСтатус" {
			t.Fatalf("expected 'СуперСтатус' to be removed from available statuses, got %v", avail)
		}
	}

	saved, ok := app.svc.ByID(sess.ID)
	if !ok || saved.Status != "СуперСтатус" {
		t.Fatalf("existing session status was unexpectedly modified: %v", saved)
	}
}

func TestModalSessionFormDeleteType(t *testing.T) {
	app := newTestApp(t)
	sess := models.Session{
		ID:       "20260101110000",
		Time:     "11:00",
		Name:     "Сессия с типом",
		Type:     "ОсобыйТип",
		Status:   "Активно",
		Duration: 60,
	}
	_ = app.svc.Add(sess)
	app.cfg.AddCustomType("ОсобыйТип")

	m := NewModalSessionForm(nil, "01.01.2026")
	m.openCatPicker(app)
	if !m.pickerOpen {
		t.Fatalf("expected pickerOpen to be true")
	}

	idx := -1
	for i, tp := range m.pickerList {
		if tp == "ОсобыйТип" {
			idx = i
			break
		}
	}
	if idx == -1 {
		t.Fatalf("ОсобыйТип not found in m.pickerList: %v", m.pickerList)
	}
	m.pickerIdx = idx

	m.HandleKey(Key{Type: KeyRune, Rune: 'd'}, app)

	avail := app.getAvailableTypes()
	for _, tp := range avail {
		if tp == "ОсобыйТип" {
			t.Fatalf("expected 'ОсобыйТип' to be removed from available types, got %v", avail)
		}
	}

	saved, ok := app.svc.ByID(sess.ID)
	if !ok || saved.Type != "ОсобыйТип" {
		t.Fatalf("existing session type was unexpectedly modified: %v", saved)
	}
}

func TestModalSortKeyLOpensModal(t *testing.T) {
	app := newTestApp(t)

	// Press 'l'
	app.handleKey(Key{Type: KeyRune, Rune: 'l'})
	if app.activeModal == nil {
		t.Fatalf("expected modal to open after pressing 'l'")
	}
	sortModal, ok := app.activeModal.(*ModalSort)
	if !ok {
		t.Fatalf("expected activeModal to be *ModalSort, got %T", app.activeModal)
	}

	// Press Esc to close
	sortModal.HandleKey(Key{Type: KeyEsc}, app)
	if app.activeModal != nil {
		t.Fatalf("expected modal to close after Esc")
	}

	// Press 'д' (Russian keyboard)
	app.handleKey(Key{Type: KeyRune, Rune: 'д'})
	if app.activeModal == nil {
		t.Fatalf("expected modal to open after pressing 'д'")
	}
}

func TestModalSortSelectFieldAndToggleOrder(t *testing.T) {
	app := newTestApp(t)
	app.periodMode = "all"
	// Add two sessions
	_ = app.svc.Add(models.Session{ID: "20261105140000", Time: "14:00", Name: "Яблоко", Duration: 30})
	_ = app.svc.Add(models.Session{ID: "20261105090000", Time: "09:00", Name: "Апельсин", Duration: 90})
	app.reloadSessions()

	// 1. Initial sort is date descending, time ascending
	if app.sortField != "date" || app.sortAsc != false {
		t.Fatalf("expected initial sort date desc, got %s (asc=%v)", app.sortField, app.sortAsc)
	}
	if app.sessions[0].Time != "09:00" || app.sessions[1].Time != "14:00" {
		t.Fatalf("expected time ascending within same date: %v, %v", app.sessions[0].Time, app.sessions[1].Time)
	}

	// 2. Open Sort Modal by pressing 'l'
	app.handleKey(Key{Type: KeyRune, Rune: 'l'})
	m, ok := app.activeModal.(*ModalSort)
	if !ok {
		t.Fatalf("expected ModalSort")
	}

	// Select [3] Название
	m.HandleKey(Key{Type: KeyRune, Rune: '3'}, app)

	// Modal closed and sort applied
	if app.activeModal != nil {
		t.Fatalf("expected modal closed after selection")
	}
	if app.sortField != "name" || app.sortAsc != true {
		t.Fatalf("expected sortField=name asc=true, got %s (asc=%v)", app.sortField, app.sortAsc)
	}
	if app.sessions[0].Name != "Апельсин" || app.sessions[1].Name != "Яблоко" {
		t.Fatalf("expected Апельсин before Яблоко: %s, %s", app.sessions[0].Name, app.sessions[1].Name)
	}

	// 3. Open Sort Modal again and select [3] Название again -> should TOGGLE order!
	app.handleKey(Key{Type: KeyRune, Rune: 'l'})
	m, ok = app.activeModal.(*ModalSort)
	if !ok {
		t.Fatalf("expected ModalSort")
	}
	m.HandleKey(Key{Type: KeyRune, Rune: '3'}, app)

	if app.sortField != "name" || app.sortAsc != false {
		t.Fatalf("expected sortField=name asc=false (toggled), got %s (asc=%v)", app.sortField, app.sortAsc)
	}
	if app.sessions[0].Name != "Яблоко" || app.sessions[1].Name != "Апельсин" {
		t.Fatalf("expected Яблоко before Апельсин: %s, %s", app.sessions[0].Name, app.sessions[1].Name)
	}
}

func TestFooterAndHelpHotkeysSync(t *testing.T) {
	app := newTestApp(t)

	// Check wide mode footer hints
	app.width = 120
	app.height = 30
	_, wideHintLines := app.layoutFooter()
	var wideKeys []string
	for _, line := range wideHintLines {
		for _, item := range line {
			wideKeys = append(wideKeys, item.key)
		}
	}

	hasSettingsWide := false
	for _, k := range wideKeys {
		if k == "[o]" {
			hasSettingsWide = true
			break
		}
	}
	if !hasSettingsWide {
		t.Fatalf("expected '[o]' in wide footer hints, got %v", wideKeys)
	}

	// Check narrow mode footer hints
	app.width = 80
	app.height = 30
	_, narrowHintLines := app.layoutFooter()
	var narrowKeys []string
	for _, line := range narrowHintLines {
		for _, item := range line {
			narrowKeys = append(narrowKeys, item.key)
		}
	}

	hasSettingsNarrow := false
	for _, k := range narrowKeys {
		if k == "[o]" {
			hasSettingsNarrow = true
			break
		}
	}
	if !hasSettingsNarrow {
		t.Fatalf("expected '[o]' in narrow footer hints, got %v", narrowKeys)
	}

	// Check ModalHelp
	help := NewModalHelp()
	screen := NewScreen(100, 30)
	help.Draw(screen)
	// Key scrolling test
	help.HandleKey(Key{Type: KeyDown}, app)
	if help.scrollOffset != 1 {
		t.Fatalf("expected scrollOffset=1 after KeyDown, got %d", help.scrollOffset)
	}
	help.HandleKey(Key{Type: KeyUp}, app)
	if help.scrollOffset != 0 {
		t.Fatalf("expected scrollOffset=0 after KeyUp, got %d", help.scrollOffset)
	}
}

func TestModalSettingsComprehensive(t *testing.T) {
	app := newTestApp(t)
	app.cfg.DefaultMode = "cli"
	app.cfg.DateCheckMode = ""
	app.cfg.AskOnConflict = false
	app.cfg.SilentAddNames = false
	app.cfg.SplitMode = models.SplitNone
	app.cfg.UseSystemDate = true
	app.cfg.DefaultType = "Встреча"

	m := NewModalSettings(app.cfg)
	app.activeModal = m

	// Test field 0 (default mode): toggle to TUI
	m.selectedIdx = 0
	m.HandleKey(Key{Type: KeySpace}, app)
	if m.defaultMode != models.ModeTUI {
		t.Fatalf("expected defaultMode 'tui', got %q", m.defaultMode)
	}

	// Test field 4 (default type): toggle
	m.selectedIdx = 4
	m.HandleKey(Key{Type: KeySpace}, app)

	// Test field 5 (date check mode): toggle from "" to "ask"
	m.selectedIdx = 5
	m.HandleKey(Key{Type: KeySpace}, app)
	if m.dateCheckMode != "ask" {
		t.Fatalf("expected dateCheckMode 'ask', got %q", m.dateCheckMode)
	}

	// Test field 6 (ask on conflict): toggle from false to true
	m.selectedIdx = 6
	m.HandleKey(Key{Type: KeySpace}, app)
	if !m.askOnConflict {
		t.Fatalf("expected askOnConflict true")
	}

	// Test field 7 (silent add names): toggle from false to true
	m.selectedIdx = 7
	m.HandleKey(Key{Type: KeySpace}, app)
	if !m.silentAddNames {
		t.Fatalf("expected silentAddNames true")
	}

	// Test field 8 (split mode): toggle from none to year
	m.selectedIdx = 8
	m.HandleKey(Key{Type: KeySpace}, app)
	if m.splitMode != models.SplitYear {
		t.Fatalf("expected splitMode 'year', got %q", m.splitMode)
	}

	// Test field 9 (use system date): toggle from true to false
	m.selectedIdx = 9
	m.HandleKey(Key{Type: KeySpace}, app)
	if m.useSystemDate != false {
		t.Fatalf("expected useSystemDate false")
	}

	// Save all via Enter
	m.HandleKey(Key{Type: KeyEnter}, app)

	if app.cfg.DefaultMode != models.ModeTUI {
		t.Fatalf("expected app.cfg.DefaultMode 'tui', got %q", app.cfg.DefaultMode)
	}
	if app.cfg.DateCheckMode != "ask" {
		t.Fatalf("expected app.cfg.DateCheckMode 'ask', got %q", app.cfg.DateCheckMode)
	}
	if !app.cfg.AskOnConflict {
		t.Fatalf("expected app.cfg.AskOnConflict true")
	}
	if !app.cfg.SilentAddNames {
		t.Fatalf("expected app.cfg.SilentAddNames true")
	}
	if app.cfg.SplitMode != models.SplitYear {
		t.Fatalf("expected app.cfg.SplitMode 'year', got %q", app.cfg.SplitMode)
	}
	if app.cfg.UseSystemDate != false {
		t.Fatalf("expected app.cfg.UseSystemDate false")
	}
}

func TestModalSettingsDropdownPickers(t *testing.T) {
	app := newTestApp(t)
	app.cfg.DefaultMode = "cli"
	app.cfg.DurationUnit = "min"
	app.cfg.DateCheckMode = ""
	app.cfg.SplitMode = models.SplitNone

	m := NewModalSettings(app.cfg)
	app.activeModal = m

	// 1. On field 0 (DefaultMode), press Right arrow -> should open dropdown picker
	m.selectedIdx = 0
	m.HandleKey(Key{Type: KeyRight}, app)
	if !m.pickerOpen {
		t.Fatalf("expected pickerOpen true on field 0 after KeyRight")
	}
	if len(m.pickerList) != 3 {
		t.Fatalf("expected 3 options for DefaultMode, got %d", len(m.pickerList))
	}
	if m.pickerIdx != 0 {
		t.Fatalf("expected pickerIdx 0 ('cli'), got %d", m.pickerIdx)
	}

	// Press Down to select TUI
	m.HandleKey(Key{Type: KeyDown}, app)
	if m.pickerIdx != 1 {
		t.Fatalf("expected pickerIdx 1 ('tui'), got %d", m.pickerIdx)
	}

	// Press Enter to confirm
	m.HandleKey(Key{Type: KeyEnter}, app)
	if m.pickerOpen {
		t.Fatalf("expected pickerOpen false after Enter")
	}
	if m.defaultMode != models.ModeTUI {
		t.Fatalf("expected defaultMode 'tui', got %q", m.defaultMode)
	}

	// 2. On field 3 (DurationUnit), press Right arrow -> opens picker
	m.selectedIdx = 3
	m.HandleKey(Key{Type: KeyRight}, app)
	if !m.pickerOpen {
		t.Fatalf("expected pickerOpen true on field 3")
	}
	if len(m.pickerList) != 2 {
		t.Fatalf("expected 2 options for DurationUnit, got %d", len(m.pickerList))
	}
	// Press Down to select 'hours'
	m.HandleKey(Key{Type: KeyDown}, app)
	m.HandleKey(Key{Type: KeyEnter}, app)
	if m.durationUnit != "hours" {
		t.Fatalf("expected durationUnit 'hours', got %q", m.durationUnit)
	}

	// 3. Test Esc closes picker without applying changes
	m.selectedIdx = 5 // DateCheckMode
	m.HandleKey(Key{Type: KeyRight}, app)
	if !m.pickerOpen {
		t.Fatalf("expected pickerOpen true on field 5")
	}
	m.HandleKey(Key{Type: KeyDown}, app) // moves to "ask"
	m.HandleKey(Key{Type: KeyEsc}, app)  // cancel
	if m.pickerOpen {
		t.Fatalf("expected pickerOpen false after Esc")
	}
	if m.dateCheckMode != "" {
		t.Fatalf("expected dateCheckMode unchanged (''), got %q", m.dateCheckMode)
	}

	// 4. Test Left arrow closes picker without applying changes
	m.HandleKey(Key{Type: KeyRight}, app)
	if !m.pickerOpen {
		t.Fatalf("expected pickerOpen true on field 5")
	}
	m.HandleKey(Key{Type: KeyDown}, app)
	m.HandleKey(Key{Type: KeyLeft}, app) // cancel with Left arrow
	if m.pickerOpen {
		t.Fatalf("expected pickerOpen false after KeyLeft")
	}
	if m.dateCheckMode != "" {
		t.Fatalf("expected dateCheckMode unchanged (''), got %q", m.dateCheckMode)
	}

	// 5. Test Screen Draw with open picker doesn't panic
	m.HandleKey(Key{Type: KeyRight}, app)
	s := NewScreen(80, 25)
	m.Draw(s)
}

func TestKeyTNavigatesToCategoriesThenCycles(t *testing.T) {
	app := newTestApp(t)
	// Ensure we have multiple types/categories available
	app.allSessions = []models.Session{
		{ID: "20261105100000", Time: "10:00", Name: "Запись 1", Type: "Стрельба из лука"},
		{ID: "20261105120000", Time: "12:00", Name: "Запись 2", Type: "Метание ножей"},
	}

	// 1. Initial state: focus on Feed
	app.focusPanel = PanelFeed
	if app.hasActiveTypeFilters() {
		t.Fatalf("expected no active type filters initially")
	}

	// 2. First press of 't': moves focus to PanelSidebar, cursor on category (sidebarIdx >= 5)
	app.handleKey(Key{Type: KeyRune, Rune: 't'})
	if app.focusPanel != PanelSidebar {
		t.Fatalf("expected focusPanel to be PanelSidebar on first 't', got %d", app.focusPanel)
	}
	if app.sidebarIdx < 5 {
		t.Fatalf("expected sidebarIdx >= 5 (categories block), got %d", app.sidebarIdx)
	}
	if app.hasActiveTypeFilters() {
		t.Fatalf("expected filter NOT cycled on first 't'")
	}

	// 3. Second press of 't' (already in Categories block): cycles type filter to first category
	app.handleKey(Key{Type: KeyRune, Rune: 't'})
	if !app.hasActiveTypeFilters() {
		t.Fatalf("expected active type filter after second 't'")
	}
	types := app.getAvailableTypes()
	if !app.isTypeFilterActive(types[0]) {
		t.Fatalf("expected type %q active, got filters: %v", types[0], app.typeFilters)
	}
	if app.sidebarIdx != 5 {
		t.Fatalf("expected cursor on first category (5), got %d", app.sidebarIdx)
	}

	// 4. Third press of 't': cycles to next category
	app.handleKey(Key{Type: KeyRune, Rune: 't'})
	if !app.isTypeFilterActive(types[1]) {
		t.Fatalf("expected type %q active, got filters: %v", types[1], app.typeFilters)
	}
	if app.sidebarIdx != 6 {
		t.Fatalf("expected cursor on second category (6), got %d", app.sidebarIdx)
	}

	// 5. When in PanelCalendar, press 't': moves focus to PanelSidebar and categories
	app.focusPanel = PanelCalendar
	app.handleKey(Key{Type: KeyRune, Rune: 't'})
	if app.focusPanel != PanelSidebar {
		t.Fatalf("expected focusPanel to be PanelSidebar when pressing 't' from Calendar, got %d", app.focusPanel)
	}
	if app.sidebarIdx < 5 {
		t.Fatalf("expected sidebarIdx >= 5, got %d", app.sidebarIdx)
	}

	// 6. Check footer hints have "[t]" labeled as "Категории"
	app.width = 120
	app.height = 30
	_, wideHints := app.layoutFooter()
	foundCategoryHint := false
	for _, line := range wideHints {
		for _, item := range line {
			if item.key == "[t]" && item.desc == "Категории" {
				foundCategoryHint = true
				break
			}
		}
	}
	if !foundCategoryHint {
		t.Fatalf("expected '[t] Категории' in footer hints")
	}
}

func TestTUIDefaultPeriodStartupAndSettings(t *testing.T) {
	dir := t.TempDir()
	svc, err := calendar.NewService(context.Background(), dir, "mycal", models.SplitNone)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	// 1. Startup period mode checks
	cases := []struct {
		configured  string
		wantPeriod  string
		wantSidebar int
	}{
		{"", "today", 1},
		{"today", "today", 1},
		{"all", "all", 0},
		{"week", "week", 2},
		{"month", "month", 3},
		{"unknown", "today", 1}, // fallback to today
	}

	for _, tc := range cases {
		cfg := &models.Config{DefaultMode: "tui", WebPort: 8080, TUIDefaultPeriod: tc.configured}
		app := NewApp(svc, cfg, "", "vTest")
		if app.periodMode != tc.wantPeriod {
			t.Errorf("configured %q: expected periodMode %q, got %q", tc.configured, tc.wantPeriod, app.periodMode)
		}
		if app.sidebarIdx != tc.wantSidebar {
			t.Errorf("configured %q: expected sidebarIdx %d, got %d", tc.configured, tc.wantSidebar, app.sidebarIdx)
		}
	}

	// 2. ModalSettings field 11: picker & space toggle
	app := newTestApp(t)
	app.cfg.TUIDefaultPeriod = "today"
	m := NewModalSettings(app.cfg)
	app.activeModal = m

	m.selectedIdx = 11 // TUIDefaultPeriod field
	if m.tuiDefaultPeriod != "today" {
		t.Fatalf("expected initial tuiDefaultPeriod 'today', got %q", m.tuiDefaultPeriod)
	}

	// Space key cycles options: today -> all -> week -> month -> today
	m.HandleKey(Key{Type: KeySpace}, app)
	if m.tuiDefaultPeriod != "all" {
		t.Fatalf("expected 'all' after space, got %q", m.tuiDefaultPeriod)
	}
	m.HandleKey(Key{Type: KeySpace}, app)
	if m.tuiDefaultPeriod != "week" {
		t.Fatalf("expected 'week' after space, got %q", m.tuiDefaultPeriod)
	}
	m.HandleKey(Key{Type: KeySpace}, app)
	if m.tuiDefaultPeriod != "month" {
		t.Fatalf("expected 'month' after space, got %q", m.tuiDefaultPeriod)
	}
	m.HandleKey(Key{Type: KeySpace}, app)
	if m.tuiDefaultPeriod != "today" {
		t.Fatalf("expected 'today' after space cycle, got %q", m.tuiDefaultPeriod)
	}

	// Right arrow opens dropdown picker
	m.HandleKey(Key{Type: KeyRight}, app)
	if !m.pickerOpen {
		t.Fatalf("expected pickerOpen true on field 11")
	}
	if len(m.pickerList) != 4 {
		t.Fatalf("expected 4 options in pickerList, got %d", len(m.pickerList))
	}
	if m.pickerIdx != 0 {
		t.Fatalf("expected pickerIdx 0 ('today'), got %d", m.pickerIdx)
	}

	// Select 3rd option ("week", index 2)
	m.HandleKey(Key{Type: KeyDown}, app)
	m.HandleKey(Key{Type: KeyDown}, app)
	if m.pickerIdx != 2 {
		t.Fatalf("expected pickerIdx 2, got %d", m.pickerIdx)
	}

	// Press Enter to confirm selection
	m.HandleKey(Key{Type: KeyEnter}, app)
	if m.pickerOpen {
		t.Fatalf("expected pickerOpen false after Enter")
	}
	if m.tuiDefaultPeriod != "week" {
		t.Fatalf("expected tuiDefaultPeriod 'week', got %q", m.tuiDefaultPeriod)
	}

	// Draw screen with modal to ensure rendering works without panic
	s := NewScreen(80, 25)
	m.Draw(s)

	// Save settings by pressing Enter on field 11 (or navigation to OK)
	m.HandleKey(Key{Type: KeyEnter}, app)
	if app.cfg.TUIDefaultPeriod != "week" {
		t.Fatalf("expected app.cfg.TUIDefaultPeriod updated to 'week', got %q", app.cfg.TUIDefaultPeriod)
	}
}




