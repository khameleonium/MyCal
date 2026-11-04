package tui

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"mycalendar/calendar"
	"mycalendar/config"
	"mycalendar/models"
	"mycalendar/parser"
)

const (
	PanelSidebar   = 0
	PanelFeed      = 1
	PanelInspector = 2
)

// App manages the full terminal user interface.
type App struct {
	ctx     context.Context
	svc     *calendar.Service
	cfg     *models.Config
	cfgPath string
	version string

	term   *Terminal
	width  int
	height int

	focusPanel  int // PanelSidebar, PanelFeed, PanelInspector
	periodMode  string
	startDate   time.Time
	endDate     time.Time
	typeFilter  string
	searchQuery string

	allSessions []models.Session
	sessions    []models.Session

	cursorIdx    int
	scrollOffset int
	selectedIDs  map[string]bool

	// Calendar state
	calYear  int
	calMonth int
	calDay   int

	sidebarIdx int // 0: cal, 1: all, 2: today, 3: week, 4: month, 5: period

	activeModal Modal
	statusMsg   string
	statusTime  time.Time
}

// NewApp creates a new TUI application instance.
func NewApp(svc *calendar.Service, cfg *models.Config, cfgPath, version string) *App {
	now := time.Now()
	app := &App{
		svc:         svc,
		cfg:         cfg,
		cfgPath:     cfgPath,
		version:     version,
		focusPanel:  PanelFeed,
		periodMode:  "all",
		selectedIDs: make(map[string]bool),
		calYear:     now.Year(),
		calMonth:    int(now.Month()),
		calDay:      now.Day(),
		sidebarIdx:  1, // "Все записи"
	}
	return app
}

// Run starts the TUI main loop.
func (a *App) Run(ctx context.Context) error {
	a.ctx = ctx

	// Enter full-screen raw terminal mode
	term, err := Enter(func() {
		// on resize callback
		a.width, a.height = Size()
		a.render()
	})
	if err != nil {
		return err
	}
	a.term = term
	defer a.term.Exit()

	a.width, a.height = Size()
	a.reloadSessions()

	keyReader := NewKeyReader()
	defer keyReader.Close()

	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	a.render()

	for {
		select {
		case <-ctx.Done():
			return nil

		case <-ticker.C:
			// Check if dimensions changed (for Windows or polling)
			curW, curH := Size()
			if curW != a.width || curH != a.height {
				a.width = curW
				a.height = curH
				a.render()
			} else if a.statusMsg != "" && time.Since(a.statusTime) > 4*time.Second {
				a.statusMsg = ""
				a.render()
			}

		case k, ok := <-keyReader.C():
			if !ok {
				return nil
			}

			if a.handleKey(k) {
				return nil // Exit requested
			}
			a.render()
		}
	}
}

func (a *App) setStatusMsg(msg string) {
	a.statusMsg = msg
	a.statusTime = time.Now()
}

func (a *App) persistConfig() {
	if a.cfgPath != "" {
		_ = config.Save(a.ctx, a.cfgPath, a.cfg)
	}
}

// reloadSessions filters and sorts sessions according to the current period and type.
func (a *App) reloadSessions() {
	a.allSessions = a.svc.All()

	var filtered []models.Session

	now := time.Now()
	var start, end time.Time

	switch a.periodMode {
	case "today":
		start = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
		end = start.Add(24*time.Hour - time.Nanosecond)
	case "week":
		weekday := int(now.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		mon := now.AddDate(0, 0, -(weekday - 1))
		start = time.Date(mon.Year(), mon.Month(), mon.Day(), 0, 0, 0, 0, time.Local)
		end = start.AddDate(0, 0, 7).Add(-time.Nanosecond)
	case "month":
		start = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)
		end = start.AddDate(0, 1, 0).Add(-time.Nanosecond)
	case "custom":
		start = a.startDate
		end = a.endDate
	default: // "all"
		// no date limits
	}

	for _, s := range a.allSessions {
		// Period check
		if a.periodMode != "all" {
			d, err := parser.ParseDate(s.Date(), now)
			if err != nil || d.Before(start) || d.After(end) {
				continue
			}
		}

		// Type filter check
		if a.typeFilter != "" && !strings.EqualFold(s.Type, a.typeFilter) {
			continue
		}

		// Search query check
		if a.searchQuery != "" {
			q := strings.ToLower(a.searchQuery)
			if !strings.Contains(strings.ToLower(s.Name), q) &&
				!strings.Contains(strings.ToLower(s.Type), q) &&
				!strings.Contains(strings.ToLower(s.Notes), q) {
				continue
			}
		}

		filtered = append(filtered, s)
	}

	// Sort chronologically (newest first for quick glance)
	sort.Slice(filtered, func(i, j int) bool {
		d1, _ := parser.ParseDate(filtered[i].Date(), now)
		d2, _ := parser.ParseDate(filtered[j].Date(), now)
		if d1.Equal(d2) {
			return filtered[i].Time > filtered[j].Time
		}
		return d1.After(d2)
	})

	a.sessions = filtered

	if a.cursorIdx >= len(a.sessions) {
		a.cursorIdx = len(a.sessions) - 1
	}
	if a.cursorIdx < 0 {
		a.cursorIdx = 0
	}
}

func (a *App) handleKey(k Key) bool {
	// If a modal is active, delegate entirely to it
	if a.activeModal != nil {
		a.activeModal.HandleKey(k, a)
		return false
	}

	// Global navigation & actions
	switch {
	case k.Type == KeyCtrlC || k.Matches('q', 'й'):
		return true // Exit TUI

	case k.Matches('?', '?') || k.Matches('h', 'р') || k.Type == KeyF1:
		a.activeModal = NewModalHelp()
		return false

	case k.Matches('o', 'щ'):
		a.activeModal = NewModalSettings(a.cfg)
		return false

	case k.Matches('y', 'н'):
		a.activeModal = NewModalYearCalendar(a.calYear)
		return false

	case k.Type == KeyTab:
		a.focusPanel = (a.focusPanel + 1) % 3
		return false

	case k.Type == KeyBackTab:
		a.focusPanel = (a.focusPanel + 2) % 3
		return false

	case k.Matches('1', '1'):
		a.periodMode = "all"
		a.sidebarIdx = 1
		a.reloadSessions()
		a.setStatusMsg("Выбран период: Все записи")
		return false

	case k.Matches('2', '2'):
		a.periodMode = "today"
		a.sidebarIdx = 2
		a.reloadSessions()
		a.setStatusMsg("Выбран период: Сегодня")
		return false

	case k.Matches('3', '3'):
		a.periodMode = "week"
		a.sidebarIdx = 3
		a.reloadSessions()
		a.setStatusMsg("Выбран период: Неделя")
		return false

	case k.Matches('4', '4'):
		a.periodMode = "month"
		a.sidebarIdx = 4
		a.reloadSessions()
		a.setStatusMsg("Выбран период: Месяц")
		return false

	case k.Matches('t', 'е'):
		a.cycleTypeFilter()
		return false

	case k.Matches('a', 'ф'):
		// Add new session
		var curDate string
		if len(a.sessions) > 0 && a.cursorIdx < len(a.sessions) {
			curDate = a.sessions[a.cursorIdx].Date()
		}
		a.activeModal = NewModalSessionForm(nil, curDate)
		return false

	case k.Matches('e', 'у'):
		// Edit focused session
		if len(a.sessions) > 0 && a.cursorIdx < len(a.sessions) {
			sess := a.sessions[a.cursorIdx]
			a.activeModal = NewModalSessionForm(&sess, "")
		}
		return false

	case k.Matches('s', 'ы'):
		// Change status popup
		if len(a.selectedIDs) > 0 {
			a.activeModal = NewModalStatusPicker(a.cfg.CustomStatuses, "Активно", "")
		} else if len(a.sessions) > 0 && a.cursorIdx < len(a.sessions) {
			sess := a.sessions[a.cursorIdx]
			a.activeModal = NewModalStatusPicker(a.cfg.CustomStatuses, sess.Status, sess.ID)
		}
		return false

	case k.Matches('f', 'а'):
		// Quick field edit popup
		if len(a.selectedIDs) > 0 {
			a.activeModal = NewModalQuickEditField("")
		} else if len(a.sessions) > 0 && a.cursorIdx < len(a.sessions) {
			sess := a.sessions[a.cursorIdx]
			a.activeModal = NewModalQuickEditField(sess.ID)
		}
		return false

	case k.Matches('d', 'в') || k.Type == KeyDelete:
		// Delete session(s)
		if len(a.selectedIDs) > 0 {
			a.activeModal = NewModalConfirmDelete("", len(a.selectedIDs))
		} else if len(a.sessions) > 0 && a.cursorIdx < len(a.sessions) {
			sess := a.sessions[a.cursorIdx]
			a.activeModal = NewModalConfirmDelete(sess.ID, 1)
		}
		return false

	case k.Type == KeySpace:
		// Toggle selection
		if len(a.sessions) > 0 && a.cursorIdx < len(a.sessions) {
			id := a.sessions[a.cursorIdx].ID
			if a.selectedIDs[id] {
				delete(a.selectedIDs, id)
			} else {
				a.selectedIDs[id] = true
			}
		}
		return false

	case k.Type == KeyEsc:
		// Clear selection or type filter
		if len(a.selectedIDs) > 0 {
			a.selectedIDs = make(map[string]bool)
			a.setStatusMsg("Выделение снято")
		} else if a.typeFilter != "" {
			a.typeFilter = ""
			a.reloadSessions()
			a.setStatusMsg("Фильтр типов сброшен")
		}
		return false
	}

	// Panel-specific navigation
	switch a.focusPanel {
	case PanelSidebar:
		a.handleSidebarKey(k)
	case PanelFeed:
		a.handleFeedKey(k)
	case PanelInspector:
		a.handleInspectorKey(k)
	}

	return false
}

func (a *App) handleFeedKey(k Key) {
	switch k.Type {
	case KeyUp, KeyRune:
		if k.Type == KeyUp || k.Matches('k', 'л') {
			if a.cursorIdx > 0 {
				a.cursorIdx--
				if a.cursorIdx < a.scrollOffset {
					a.scrollOffset = a.cursorIdx
				}
			}
		} else if k.Type == KeyDown || k.Matches('j', 'о') {
			if a.cursorIdx < len(a.sessions)-1 {
				a.cursorIdx++
			}
		}
	case KeyDown:
		if a.cursorIdx < len(a.sessions)-1 {
			a.cursorIdx++
		}
	case KeyHome:
		a.cursorIdx = 0
		a.scrollOffset = 0
	case KeyEnd:
		if len(a.sessions) > 0 {
			a.cursorIdx = len(a.sessions) - 1
		}
	case KeyPgUp:
		a.cursorIdx -= 10
		if a.cursorIdx < 0 {
			a.cursorIdx = 0
		}
		a.scrollOffset = a.cursorIdx
	case KeyPgDn:
		a.cursorIdx += 10
		if a.cursorIdx >= len(a.sessions) {
			a.cursorIdx = len(a.sessions) - 1
		}
	case KeyEnter:
		if len(a.sessions) > 0 && a.cursorIdx < len(a.sessions) {
			sess := a.sessions[a.cursorIdx]
			a.activeModal = NewModalSessionForm(&sess, "")
		}
	}
}

func (a *App) handleSidebarKey(k Key) {
	switch k.Type {
	case KeyUp:
		if a.sidebarIdx > 0 {
			a.sidebarIdx--
		}
	case KeyDown:
		if a.sidebarIdx < 5 {
			a.sidebarIdx++
		}
	case KeyLeft:
		a.changeMonth(-1)
	case KeyRight:
		a.changeMonth(1)
	case KeyEnter:
		switch a.sidebarIdx {
		case 1:
			a.periodMode = "all"
		case 2:
			a.periodMode = "today"
		case 3:
			a.periodMode = "week"
		case 4:
			a.periodMode = "month"
		case 5:
			a.cycleTypeFilter()
		}
		a.reloadSessions()
	}
}

func (a *App) handleInspectorKey(k Key) {
	switch k.Type {
	case KeyEnter:
		if len(a.sessions) > 0 && a.cursorIdx < len(a.sessions) {
			sess := a.sessions[a.cursorIdx]
			a.activeModal = NewModalSessionForm(&sess, "")
		}
	}
}

func (a *App) changeMonth(delta int) {
	a.calMonth += delta
	if a.calMonth > 12 {
		a.calMonth = 1
		a.calYear++
	} else if a.calMonth < 1 {
		a.calMonth = 12
		a.calYear--
	}
}

func (a *App) cycleTypeFilter() {
	types := a.getAvailableTypes()
	if len(types) == 0 {
		return
	}
	if a.typeFilter == "" {
		a.typeFilter = types[0]
	} else {
		found := false
		for i, t := range types {
			if strings.EqualFold(t, a.typeFilter) {
				if i+1 < len(types) {
					a.typeFilter = types[i+1]
				} else {
					a.typeFilter = "" // reset to all
				}
				found = true
				break
			}
		}
		if !found {
			a.typeFilter = ""
		}
	}
	a.reloadSessions()
	if a.typeFilter == "" {
		a.setStatusMsg("Фильтр типов: Все категории")
	} else {
		a.setStatusMsg("Фильтр типов: " + a.typeFilter)
	}
}

func (a *App) getAvailableTypes() []string {
	seen := map[string]bool{}
	var res []string
	for _, s := range a.allSessions {
		t := strings.TrimSpace(s.Type)
		if t != "" && !seen[t] {
			seen[t] = true
			res = append(res, t)
		}
	}
	sort.Strings(res)
	return res
}

// ----------------- RENDERING -----------------

func (a *App) render() {
	if a.width < 40 || a.height < 10 {
		return
	}

	screen := NewScreen(a.width, a.height)
	screen.Clear(BgPrimary)

	// 1. Top Header Bar (row 0)
	a.renderHeader(screen)

	// 2. Main Workspace Layout
	workspaceY := 1
	workspaceH := a.height - 3

	wSidebar := 26
	wInspector := 34

	if a.width >= 115 {
		// Full 3 panels with subtle gap
		gap := 1
		wFeed := a.width - wSidebar - wInspector - (gap * 2)
		xSidebar := 0
		xFeed := wSidebar + gap
		xInspector := xFeed + wFeed + gap

		a.renderSidebar(screen, xSidebar, workspaceY, wSidebar, workspaceH)
		a.renderFeed(screen, xFeed, workspaceY, wFeed, workspaceH)
		a.renderInspector(screen, xInspector, workspaceY, wInspector, workspaceH)
	} else if a.width >= 85 {
		// 2 panels: Sidebar + Feed
		gap := 1
		wFeed := a.width - wSidebar - gap
		xSidebar := 0
		xFeed := wSidebar + gap

		a.renderSidebar(screen, xSidebar, workspaceY, wSidebar, workspaceH)
		a.renderFeed(screen, xFeed, workspaceY, wFeed, workspaceH)
	} else {
		// Single Feed panel
		a.renderFeed(screen, 0, workspaceY, a.width, workspaceH)
	}

	// 3. Footer Bar (Bottom 2 rows)
	a.renderFooter(screen)

	// 4. Modal Dialog Overlay (if active)
	if a.activeModal != nil {
		a.activeModal.Draw(screen)
	}

	_ = screen.Render(os.Stdout)
}

func (a *App) renderHeader(s *Screen) {
	s.Fill(0, 0, a.width, 1, ' ', FgMain, BgPanelAlt)

	title := fmt.Sprintf("🗓️ MyCal TUI %s", a.version)
	s.DrawText(2, 0, title, FgCyan, BgPanelAlt, 24, true, false, false)

	// Period & filter breadcrumbs
	pLabel := "Все записи"
	switch a.periodMode {
	case "today":
		pLabel = "Сегодня"
	case "week":
		pLabel = "Эта неделя"
	case "month":
		pLabel = "Этот месяц"
	case "custom":
		pLabel = "Период"
	}
	if a.typeFilter != "" {
		pLabel += fmt.Sprintf(" [Тип: %s]", a.typeFilter)
	}

	totalMin := 0
	totalCash := 0.0
	for _, sess := range a.sessions {
		totalMin += sess.Duration
		totalCash += sess.Cash
	}
	totalHours := float64(totalMin) / 60.0

	infoStr := fmt.Sprintf("│ Период: %s (%d зап.) │ Часов: %.1f ч │ Кэш: %.1f",
		pLabel, len(a.sessions), totalHours, totalCash)
	s.DrawText(26, 0, infoStr, FgMuted, BgPanelAlt, a.width-44, false, false, false)

	// Clock
	nowStr := time.Now().Format("15:04")
	s.DrawText(a.width-8, 0, nowStr, FgWhite, BgPanelAlt, 6, true, false, false)
}

func (a *App) renderSidebar(s *Screen, x, y, w, h int) {
	borderCol := BorderDim
	if a.focusPanel == PanelSidebar {
		borderCol = BorderFocus
	}
	s.DrawBox(x, y, w, h, "Календарь", FgCyan, borderCol, BgSurface, a.focusPanel == PanelSidebar)

	// Month header
	monthsRu := []string{
		"", "Январь", "Февраль", "Март", "Апрель", "Май", "Июнь",
		"Июль", "Август", "Сентябрь", "Октябрь", "Ноябрь", "Декабрь",
	}
	mTitle := fmt.Sprintf("◄  %s %d  ►", monthsRu[a.calMonth], a.calYear)
	s.DrawText(x+2, y+1, CenterString(mTitle, w-4), FgYellow, BgSurface, w-4, true, false, false)

	// Weekdays
	s.DrawText(x+2, y+2, "Пн Вт Ср Чт Пт Сб Вс", FgMuted, BgSurface, w-4, false, false, false)

	// Mini Calendar Grid
	firstDay := time.Date(a.calYear, time.Month(a.calMonth), 1, 0, 0, 0, 0, time.UTC)
	weekday := int(firstDay.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	weekday-- // 0..6 (Mon..Sun)

	daysInMonth := time.Date(a.calYear, time.Month(a.calMonth+1), 0, 0, 0, 0, 0, time.UTC).Day()
	now := time.Now()
	isCurrentMonth := now.Year() == a.calYear && int(now.Month()) == a.calMonth

	// Days with sessions map
	daysWithSessions := map[int]bool{}
	for _, sess := range a.allSessions {
		if d, err := parser.ParseDate(sess.Date(), now); err == nil {
			if d.Year() == a.calYear && int(d.Month()) == a.calMonth {
				daysWithSessions[d.Day()] = true
			}
		}
	}

	curX := x + 2 + (weekday * 3)
	curY := y + 3

	for d := 1; d <= daysInMonth; d++ {
		fg := FgMain
		bg := BgSurface
		bold := false
		under := false

		hasSess := daysWithSessions[d]
		if hasSess {
			fg = FgGreen
			bold = true
		}
		if isCurrentMonth && d == now.Day() {
			under = true
			fg = FgCyan
			bold = true
		}

		s.DrawText(curX, curY, fmt.Sprintf("%2d", d), fg, bg, 2, bold, false, under)
		curX += 3
		weekday++
		if weekday == 7 {
			weekday = 0
			curX = x + 2
			curY++
		}
	}

	// Separator
	sepY := y + 10
	s.DrawHLine(x+1, x+w-2, sepY, BoxH, BorderDim, BgSurface)
	s.DrawText(x+2, sepY, "┤ Периоды ├", FgDim, BgSurface, w-4, false, false, false)

	// Presets
	presets := []struct {
		id   string
		name string
		num  string
	}{
		{"all", "Все записи", "1"},
		{"today", "Сегодня", "2"},
		{"week", "Неделя", "3"},
		{"month", "Месяц", "4"},
	}

	for i, p := range presets {
		rowY := sepY + 1 + i
		isCur := a.periodMode == p.id
		fg := FgMuted
		bg := BgSurface
		prefix := "  "
		if isCur {
			fg = FgWhite
			bg = BgHighlight
			prefix = "► "
		}
		s.Fill(x+2, rowY, w-4, 1, ' ', fg, bg)
		line := fmt.Sprintf("%s[%s] %s", prefix, p.num, p.name)
		s.DrawText(x+2, rowY, line, fg, bg, w-4, isCur, false, false)
	}

	// Types list
	typesSepY := sepY + 6
	s.DrawHLine(x+1, x+w-2, typesSepY, BoxH, BorderDim, BgSurface)
	s.DrawText(x+2, typesSepY, "┤ Категории ├", FgDim, BgSurface, w-4, false, false, false)

	types := a.getAvailableTypes()
	for i, t := range types {
		if i >= 4 {
			break
		}
		rowY := typesSepY + 1 + i
		isCur := strings.EqualFold(a.typeFilter, t)
		fg := FgMuted
		bg := BgSurface
		check := "[ ] "
		if isCur {
			fg = FgCyan
			bg = BgHighlight
			check = "[✓] "
		}
		s.Fill(x+2, rowY, w-4, 1, ' ', fg, bg)
		s.DrawText(x+2, rowY, check+t, fg, bg, w-4, isCur, false, false)
	}
}

func (a *App) renderFeed(s *Screen, x, y, w, h int) {
	borderCol := BorderDim
	if a.focusPanel == PanelFeed {
		borderCol = BorderFocus
	}

	title := fmt.Sprintf("Записи (%d)", len(a.sessions))
	if len(a.selectedIDs) > 0 {
		title += fmt.Sprintf(" [Выбрано: %d]", len(a.selectedIDs))
	}
	s.DrawBox(x, y, w, h, title, FgBlue, borderCol, BgSurface, a.focusPanel == PanelFeed)

	// Dynamic column calculations
	colCheck := x + 2
	colDate := x + 6
	colTime := x + 17
	colStatus := x + w - 16
	colCash := colStatus - 9
	colDur := colCash - 7

	avail := colDur - (colTime + 6) - 1
	wType := 0
	wName := avail
	colName := colTime + 6
	colType := colName
	if avail > 24 {
		wType = avail * 35 / 100
		if wType > 16 {
			wType = 16
		}
		wName = avail - wType - 1
		colType = colName + wName + 1
	}

	// Column Headers
	colY := y + 1
	s.Fill(x+1, colY, w-2, 1, ' ', FgDim, BgCard)
	s.DrawText(colCheck, colY, "[✓]", FgDim, BgCard, 4, false, false, false)
	s.DrawText(colDate, colY, "Дата", FgMuted, BgCard, 10, true, false, false)
	s.DrawText(colTime, colY, "Время", FgMuted, BgCard, 5, true, false, false)
	s.DrawText(colName, colY, "Название", FgMuted, BgCard, wName, true, false, false)
	if wType > 0 {
		s.DrawText(colType, colY, "Категория", FgMuted, BgCard, wType, true, false, false)
	}
	s.DrawText(colDur, colY, "Длит.", FgMuted, BgCard, 6, true, false, false)
	s.DrawText(colCash, colY, "Кэш", FgMuted, BgCard, 8, true, false, false)
	s.DrawText(colStatus, colY, "Статус", FgMuted, BgCard, 14, true, false, false)

	// Rows viewport
	viewportH := h - 3
	if viewportH <= 0 {
		return
	}

	// Adjust scroll offset so cursor is visible
	if a.cursorIdx < a.scrollOffset {
		a.scrollOffset = a.cursorIdx
	}
	if a.cursorIdx >= a.scrollOffset+viewportH {
		a.scrollOffset = a.cursorIdx - viewportH + 1
	}

	if len(a.sessions) == 0 {
		emptyMsg := "(В выбранном периоде нет записей. Нажмите 'a' для создания)"
		s.DrawText(x+4, y+3, emptyMsg, FgMuted, BgSurface, w-8, false, false, false)
		return
	}

	for i := 0; i < viewportH; i++ {
		sessIdx := a.scrollOffset + i
		if sessIdx >= len(a.sessions) {
			break
		}
		sess := a.sessions[sessIdx]
		rowY := y + 2 + i

		isCursor := sessIdx == a.cursorIdx && a.focusPanel == PanelFeed
		isSelected := a.selectedIDs[sess.ID]

		rowBg := BgSurface
		if sessIdx%2 == 1 {
			rowBg = BgPanelAlt
		}
		if isSelected {
			rowBg = BgCard
		}
		if isCursor {
			rowBg = BgSelected
		}

		s.Fill(x+1, rowY, w-2, 1, ' ', FgMain, rowBg)

		// Checkbox & cursor arrow
		checkStr := "[ ]"
		if isSelected {
			checkStr = "[✓]"
		}
		arrow := " "
		if isCursor {
			arrow = "►"
		}
		s.DrawText(x+1, rowY, arrow, FgCyan, rowBg, 1, true, false, false)
		s.DrawText(colCheck, rowY, checkStr, FgCyan, rowBg, 3, isSelected, false, false)

		// Date & Time
		s.DrawText(colDate, rowY, sess.Date(), FgDim, rowBg, 10, false, false, false)
		s.DrawText(colTime, rowY, sess.Time, FgWhite, rowBg, 5, true, false, false)

		// Name
		nameFg := FgMain
		if isCursor {
			nameFg = FgWhite
		}
		s.DrawText(colName, rowY, TruncateString(sess.Name, wName, "…"), nameFg, rowBg, wName, isCursor, false, false)

		// Type
		if wType > 0 {
			typeStr := sess.Type
			if typeStr == "" {
				typeStr = "—"
			}
			s.DrawText(colType, rowY, TruncateString(typeStr, wType, "…"), FgMuted, rowBg, wType, false, false, false)
		}

		// Duration
		durStr := fmt.Sprintf("%02d:%02d", sess.Duration/60, sess.Duration%60)
		s.DrawText(colDur, rowY, durStr, FgDim, rowBg, 6, false, false, false)

		// Cash (pure number!)
		cashStr := ""
		if sess.Cash > 0 {
			cashStr = strconv.FormatFloat(sess.Cash, 'f', -1, 64)
		}
		s.DrawText(colCash, rowY, cashStr, FgWhite, rowBg, 8, false, false, false)

		// Status pill
		badgeText, bFg, _ := StatusBadge(sess.Status)
		s.DrawText(colStatus, rowY, badgeText, bFg, rowBg, 14, true, false, false)
	}
}

func (a *App) renderInspector(s *Screen, x, y, w, h int) {
	borderCol := BorderDim
	if a.focusPanel == PanelInspector {
		borderCol = BorderFocus
	}
	s.DrawBox(x, y, w, h, "Карточка", FgGreen, borderCol, BgSurface, a.focusPanel == PanelInspector)

	if len(a.sessions) == 0 || a.cursorIdx >= len(a.sessions) {
		s.DrawText(x+2, y+2, "(Запись не выбрана)", FgDim, BgSurface, w-4, false, false, false)
		return
	}

	sess := a.sessions[a.cursorIdx]

	s.DrawText(x+2, y+1, TruncateString(sess.Name, w-4, "…"), FgWhite, BgSurface, w-4, true, false, false)

	badgeText, bFg, _ := StatusBadge(sess.Status)
	s.DrawText(x+2, y+2, badgeText, bFg, BgSurface, w-4, true, false, false)

	details := []struct {
		lbl string
		val string
	}{
		{"Дата:     ", sess.Date()},
		{"Время:    ", sess.Time},
		{"Длит.:    ", fmt.Sprintf("%d мин (%02d:%02d)", sess.Duration, sess.Duration/60, sess.Duration%60)},
		{"Кэш:      ", strconv.FormatFloat(sess.Cash, 'f', -1, 64)},
		{"Категория:", sess.Type},
		{"ID:       ", sess.ID},
	}

	for i, d := range details {
		rowY := y + 4 + i
		s.DrawText(x+2, rowY, d.lbl, FgDim, BgSurface, 10, false, false, false)
		s.DrawText(x+12, rowY, d.val, FgMain, BgSurface, w-14, false, false, false)
	}

	// Notes
	s.DrawText(x+2, y+11, "Заметки:", FgDim, BgSurface, 10, false, false, false)
	notes := sess.Notes
	if notes == "" {
		notes = "(нет заметок)"
	}
	s.DrawText(x+2, y+12, TruncateString(notes, w-4, "…"), FgMuted, BgSurface, w-4, false, false, false)

	// Quick hotkeys hint
	s.DrawHLine(x+1, x+w-2, y+14, BoxH, BorderDim, BgSurface)
	s.DrawText(x+2, y+14, "┤ Действия ├", FgDim, BgSurface, w-4, false, false, false)
	s.DrawText(x+2, y+15, "[e] Редактировать", FgCyan, BgSurface, w-4, false, false, false)
	s.DrawText(x+2, y+16, "[s] Сменить статус", FgBlue, BgSurface, w-4, false, false, false)
	s.DrawText(x+2, y+17, "[f] Изменить поле", FgYellow, BgSurface, w-4, false, false, false)
	s.DrawText(x+2, y+18, "[d] Удалить запись", FgRed, BgSurface, w-4, false, false, false)
}

func (a *App) renderFooter(s *Screen) {
	// Status notification row (H-2)
	notifyY := a.height - 2
	s.Fill(0, notifyY, a.width, 1, ' ', FgMain, BgPanelAlt)
	if a.statusMsg != "" {
		s.DrawText(2, notifyY, a.statusMsg, FgYellow, BgPanelAlt, a.width-4, true, false, false)
	}

	// Hotkeys row (H-1)
	hotkeyY := a.height - 1
	s.Fill(0, hotkeyY, a.width, 1, ' ', FgMain, BgPrimary)

	hints := []struct {
		key  string
		desc string
	}{
		{"[↑↓]", "Навигация"},
		{"[Space]", "Выбор"},
		{"[s]", "Статус"},
		{"[a]", "Создать"},
		{"[e]", "Правка"},
		{"[f]", "Поле"},
		{"[d]", "Удалить"},
		{"[Tab]", "Панель"},
		{"[?]", "Помощь"},
		{"[q]", "Выход"},
	}

	cx := 2
	for _, h := range hints {
		if cx+len(h.key)+len(h.desc)+2 >= a.width {
			break
		}
		s.DrawText(cx, hotkeyY, h.key, FgCyan, BgPrimary, len(h.key), true, false, false)
		cx += len(h.key) + 1
		s.DrawText(cx, hotkeyY, h.desc, FgMuted, BgPrimary, len(h.desc), false, false, false)
		cx += len(h.desc) + 2
	}
}
