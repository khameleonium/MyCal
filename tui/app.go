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

	// Tabs for Compact / Vertical mode
	TabFeed      = 0
	TabCalendar  = 1
	TabInspector = 2
	TabStats     = 3
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

	activeTab int // TabFeed, TabCalendar, TabInspector, TabStats (for compact mode)

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
		sidebarIdx:  0, // "Все записи" (0-indexed)
		activeTab:   TabFeed,
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
		if a.width < 115 {
			a.activeTab = (a.activeTab + 1) % 4
			if a.activeTab == TabCalendar {
				a.syncSidebarIdx()
			}
		} else {
			a.focusPanel = (a.focusPanel + 1) % 3
			if a.focusPanel == PanelSidebar {
				a.syncSidebarIdx()
			}
		}
		return false

	case k.Type == KeyBackTab:
		if a.width < 115 {
			a.activeTab = (a.activeTab + 3) % 4
			if a.activeTab == TabCalendar {
				a.syncSidebarIdx()
			}
		} else {
			a.focusPanel = (a.focusPanel + 2) % 3
			if a.focusPanel == PanelSidebar {
				a.syncSidebarIdx()
			}
		}
		return false

	case k.Matches('1', '1'):
		if a.width < 115 {
			a.activeTab = TabFeed
		} else {
			a.periodMode = "all"
			a.sidebarIdx = 0
			a.reloadSessions()
			a.setStatusMsg("Выбран период: Все записи")
		}
		return false

	case k.Matches('2', '2'):
		if a.width < 115 {
			a.activeTab = TabCalendar
			a.syncSidebarIdx()
		} else {
			a.periodMode = "today"
			a.sidebarIdx = 1
			a.reloadSessions()
			a.setStatusMsg("Выбран период: Сегодня")
		}
		return false

	case k.Matches('3', '3'):
		if a.width < 115 {
			a.activeTab = TabInspector
		} else {
			a.periodMode = "week"
			a.sidebarIdx = 2
			a.reloadSessions()
			a.setStatusMsg("Выбран период: Неделя")
		}
		return false

	case k.Matches('4', '4'):
		if a.width < 115 {
			a.activeTab = TabStats
		} else {
			a.periodMode = "month"
			a.sidebarIdx = 3
			a.reloadSessions()
			a.setStatusMsg("Выбран период: Месяц")
		}
		return false

	case k.Matches('l', 'д'):
		if a.width < 115 {
			a.activeTab = TabFeed
		} else {
			a.focusPanel = PanelFeed
		}
		return false

	case k.Matches('c', 'к'):
		if a.width < 115 {
			a.activeTab = TabCalendar
			a.syncSidebarIdx()
		} else {
			a.focusPanel = PanelSidebar
			a.syncSidebarIdx()
		}
		return false

	case k.Matches('i', 'ш'):
		if a.width < 115 {
			a.activeTab = TabInspector
		} else {
			a.focusPanel = PanelInspector
		}
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
		if a.width < 115 {
			if a.activeTab == TabCalendar {
				a.handleSidebarKey(k)
				return false
			}
		} else {
			if a.focusPanel == PanelSidebar {
				a.handleSidebarKey(k)
				return false
			}
		}
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
		if a.width < 115 && a.activeTab != TabFeed {
			a.activeTab = TabFeed
			return false
		}
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

	// Panel/Tab-specific navigation
	if a.width < 115 {
		switch a.activeTab {
		case TabFeed:
			a.handleFeedKey(k)
		case TabCalendar:
			a.handleSidebarKey(k)
		case TabInspector:
			a.handleInspectorKey(k)
		case TabStats:
			if k.Type == KeyEnter || k.Type == KeyEsc {
				a.activeTab = TabFeed
			}
		}
		return false
	}

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

func (a *App) syncSidebarIdx() {
	switch a.periodMode {
	case "all":
		a.sidebarIdx = 0
	case "today":
		a.sidebarIdx = 1
	case "week":
		a.sidebarIdx = 2
	case "month":
		a.sidebarIdx = 3
	}
}

func (a *App) handleFeedKey(k Key) {
	switch {
	case k.Type == KeyUp || k.Matches('k', 'л'):
		if a.cursorIdx > 0 {
			a.cursorIdx--
			if a.cursorIdx < a.scrollOffset {
				a.scrollOffset = a.cursorIdx
			}
		}
	case k.Type == KeyDown || k.Matches('j', 'о'):
		if a.cursorIdx < len(a.sessions)-1 {
			a.cursorIdx++
		}
	case k.Type == KeyHome:
		a.cursorIdx = 0
		a.scrollOffset = 0
	case k.Type == KeyEnd:
		if len(a.sessions) > 0 {
			a.cursorIdx = len(a.sessions) - 1
		}
	case k.Type == KeyPgUp:
		a.cursorIdx -= 10
		if a.cursorIdx < 0 {
			a.cursorIdx = 0
		}
		a.scrollOffset = a.cursorIdx
	case k.Type == KeyPgDn:
		a.cursorIdx += 10
		if a.cursorIdx >= len(a.sessions) {
			a.cursorIdx = len(a.sessions) - 1
		}
	case k.Type == KeyEnter:
		if len(a.sessions) > 0 && a.cursorIdx < len(a.sessions) {
			if a.width < 115 {
				a.activeTab = TabInspector
			} else {
				sess := a.sessions[a.cursorIdx]
				a.activeModal = NewModalSessionForm(&sess, "")
			}
		}
	}
}

func (a *App) handleSidebarKey(k Key) {
	types := a.getAvailableTypes()
	maxIdx := 3
	if len(types) > 0 {
		maxIdx = 3 + len(types)
	}

	switch {
	case k.Type == KeyUp || k.Matches('k', 'л'):
		if a.sidebarIdx > 0 {
			a.sidebarIdx--
			a.applySidebarNav(false)
		}
	case k.Type == KeyDown || k.Matches('j', 'о'):
		if a.sidebarIdx < maxIdx {
			a.sidebarIdx++
			a.applySidebarNav(false)
		}
	case k.Type == KeyLeft || k.Matches('h', 'р'):
		a.changeMonth(-1)
		if a.periodMode == "month" {
			a.reloadSessions()
		}
	case k.Type == KeyRight:
		a.changeMonth(1)
		if a.periodMode == "month" {
			a.reloadSessions()
		}
	case k.Type == KeyEnter:
		a.applySidebarNav(true)
	case k.Type == KeySpace:
		catIdx := a.sidebarIdx - 4
		if catIdx >= 0 && catIdx < len(types) {
			t := types[catIdx]
			if strings.EqualFold(a.typeFilter, t) {
				a.typeFilter = ""
				a.setStatusMsg("Фильтр категорий сброшен: Все категории")
			} else {
				a.typeFilter = t
				a.setStatusMsg("Фильтр категорий: " + t)
			}
			a.reloadSessions()
		}
	case k.Type == KeyEsc:
		if a.width < 115 {
			a.activeTab = TabFeed
		} else {
			a.focusPanel = PanelFeed
		}
	}
}

func (a *App) applySidebarNav(enterPressed bool) {
	presets := []struct {
		id   string
		name string
	}{
		{"all", "Все записи"},
		{"today", "Сегодня"},
		{"week", "Неделя"},
		{"month", "Месяц"},
	}

	if a.sidebarIdx >= 0 && a.sidebarIdx < len(presets) {
		p := presets[a.sidebarIdx]
		a.periodMode = p.id
		a.reloadSessions()
		a.setStatusMsg("Выбран период: " + p.name)
		if enterPressed {
			if a.width < 115 {
				a.activeTab = TabFeed
			} else {
				a.focusPanel = PanelFeed
			}
		}
		return
	}

	types := a.getAvailableTypes()
	catIdx := a.sidebarIdx - len(presets)
	if catIdx >= 0 && catIdx < len(types) {
		if enterPressed {
			t := types[catIdx]
			if strings.EqualFold(a.typeFilter, t) {
				a.typeFilter = ""
				a.setStatusMsg("Фильтр категорий сброшен: Все категории")
			} else {
				a.typeFilter = t
				a.setStatusMsg("Фильтр категорий: " + t)
			}
			a.reloadSessions()
			if a.width < 115 {
				a.activeTab = TabFeed
			} else {
				a.focusPanel = PanelFeed
			}
		}
	}
}

func (a *App) handleInspectorKey(k Key) {
	switch {
	case k.Type == KeyEsc || k.Type == KeyEnter || k.Matches('1', '1') || k.Matches('l', 'д'):
		if a.width < 115 {
			a.activeTab = TabFeed
		}
	case k.Matches('e', 'у'):
		if len(a.sessions) > 0 && a.cursorIdx < len(a.sessions) {
			sess := a.sessions[a.cursorIdx]
			a.activeModal = NewModalSessionForm(&sess, "")
		}
	case k.Matches('s', 'ы'):
		if len(a.sessions) > 0 && a.cursorIdx < len(a.sessions) {
			sess := a.sessions[a.cursorIdx]
			a.activeModal = NewModalStatusPicker(a.cfg.CustomStatuses, sess.Status, sess.ID)
		}
	case k.Matches('f', 'а'):
		if len(a.sessions) > 0 && a.cursorIdx < len(a.sessions) {
			sess := a.sessions[a.cursorIdx]
			a.activeModal = NewModalQuickEditField(sess.ID)
		}
	case k.Matches('d', 'в') || k.Type == KeyDelete:
		if len(a.sessions) > 0 && a.cursorIdx < len(a.sessions) {
			sess := a.sessions[a.cursorIdx]
			a.activeModal = NewModalConfirmDelete(sess.ID, 1)
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

	if a.width < 115 {
		// Responsive Tab bar at row 1 for vertical/compact proportions
		a.renderTabBar(screen)
		workspaceY = 2
		workspaceH = a.height - 4

		switch a.activeTab {
		case TabFeed:
			a.renderFeed(screen, 0, workspaceY, a.width, workspaceH)
		case TabCalendar:
			a.renderSidebar(screen, 0, workspaceY, a.width, workspaceH)
		case TabInspector:
			a.renderInspector(screen, 0, workspaceY, a.width, workspaceH)
		case TabStats:
			a.renderStats(screen, 0, workspaceY, a.width, workspaceH)
		default:
			a.renderFeed(screen, 0, workspaceY, a.width, workspaceH)
		}
	} else {
		// Full 3 panels side-by-side with subtle gap
		wSidebar := 26
		wInspector := 34
		gap := 1
		wFeed := a.width - wSidebar - wInspector - (gap * 2)
		xSidebar := 0
		xFeed := wSidebar + gap
		xInspector := xFeed + wFeed + gap

		a.renderSidebar(screen, xSidebar, workspaceY, wSidebar, workspaceH)
		a.renderFeed(screen, xFeed, workspaceY, wFeed, workspaceH)
		a.renderInspector(screen, xInspector, workspaceY, wInspector, workspaceH)
	}

	// 3. Footer Bar (Bottom 2 rows)
	a.renderFooter(screen)

	// 4. Modal Dialog Overlay (if active)
	if a.activeModal != nil {
		a.activeModal.Draw(screen)
	}

	_ = screen.Render(os.Stdout)
}

func (a *App) renderTabBar(s *Screen) {
	s.Fill(0, 1, a.width, 1, ' ', FgMain, BgPanelAlt)

	tabs := []struct {
		id   int
		num  string
		name string
	}{
		{TabFeed, "1", "Записи"},
		{TabCalendar, "2", "Календарь"},
		{TabInspector, "3", "Карточка"},
		{TabStats, "4", "Статистика"},
	}

	cx := 2
	for _, t := range tabs {
		isActive := a.activeTab == t.id
		fg := FgMuted
		bg := BgPanelAlt
		bold := false
		label := fmt.Sprintf(" [%s] %s ", t.num, t.name)
		if isActive {
			fg = FgWhite
			bg = BgHighlight
			bold = true
			label = fmt.Sprintf(" ► %s: %s ", t.num, t.name)
		}
		lblW := StringVisualWidth(label)
		if cx+lblW > a.width-1 {
			break
		}
		s.Fill(cx, 1, lblW, 1, ' ', fg, bg)
		s.DrawText(cx, 1, label, fg, bg, lblW, bold, false, false)
		cx += lblW + 1
	}

	hint := "[Tab / 1-4]"
	if a.width-len(hint)-2 > cx+1 {
		s.DrawText(a.width-len(hint)-2, 1, hint, FgDim, BgPanelAlt, len(hint), false, false, false)
	}
}

func (a *App) renderHeader(s *Screen) {
	s.Fill(0, 0, a.width, 1, ' ', FgMain, BgPanelAlt)

	title := fmt.Sprintf("🗓️ MyCal TUI %s", a.version)
	titleW := StringVisualWidth(title)
	s.DrawText(2, 0, title, FgCyan, BgPanelAlt, titleW, true, false, false)

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
		pLabel += fmt.Sprintf(" [%s]", a.typeFilter)
	}

	totalMin := 0
	totalCash := 0.0
	for _, sess := range a.sessions {
		totalMin += sess.Duration
		totalCash += sess.Cash
	}
	totalHours := float64(totalMin) / 60.0

	startX := 2 + titleW + 2
	if a.width >= 90 {
		cashStr := strconv.FormatFloat(totalCash, 'f', -1, 64)
		infoStr := fmt.Sprintf("│ Период: %s (%d зап.) │ Часов: %.1f ч │ Кэш: %s",
			pLabel, len(a.sessions), totalHours, cashStr)
		s.DrawText(startX, 0, infoStr, FgMuted, BgPanelAlt, a.width-startX-10, false, false, false)
	} else if a.width >= 62 {
		infoStr := fmt.Sprintf("│ %s (%d) │ %.1fч", pLabel, len(a.sessions), totalHours)
		s.DrawText(startX, 0, infoStr, FgMuted, BgPanelAlt, a.width-startX-8, false, false, false)
	}

	// Clock
	nowStr := time.Now().Format("15:04")
	s.DrawText(a.width-7, 0, nowStr, FgWhite, BgPanelAlt, 5, true, false, false)
}

func (a *App) renderSidebar(s *Screen, x, y, w, h int) {
	borderCol := BorderDim
	isFocused := a.focusPanel == PanelSidebar || a.width < 115
	if isFocused {
		borderCol = BorderFocus
	}
	s.DrawBox(x, y, w, h, "Календарь и периоды", FgCyan, borderCol, BgSurface, isFocused)

	// Month header
	monthsRu := []string{
		"", "Январь", "Февраль", "Март", "Апрель", "Май", "Июнь",
		"Июль", "Август", "Сентябрь", "Октябрь", "Ноябрь", "Декабрь",
	}
	mTitle := fmt.Sprintf("◄  %s %d  ►", monthsRu[a.calMonth], a.calYear)

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

	types := a.getAvailableTypes()

	if w >= 54 {
		// Wide mode (Full screen Calendar tab): 2 columns
		calW := 23
		s.DrawText(x+2, y+1, CenterString(mTitle, calW), FgYellow, BgSurface, calW, true, false, false)
		s.DrawText(x+2, y+2, "Пн Вт Ср Чт Пт Сб Вс", FgMuted, BgSurface, calW, false, false, false)

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

		// Vertical divider
		divX := x + calW + 3
		s.DrawVLine(divX, y+1, y+h-2, BoxV, BorderDim, BgSurface)

		// Right column: Presets and Categories
		rightX := divX + 2
		rightW := w - calW - 7

		s.DrawText(rightX, y+1, "┤ Периоды (фильтр) ├", FgDim, BgSurface, rightW, false, false, false)
		for i, p := range presets {
			rowY := y + 2 + i
			isCursor := isFocused && a.sidebarIdx == i
			isActive := a.periodMode == p.id
			fg := FgMuted
			bg := BgSurface
			prefix := "  "
			if isCursor {
				fg = FgWhite
				bg = BgHighlight
				prefix = "► "
			} else if isActive {
				fg = FgCyan
				prefix = "• "
			}
			s.Fill(rightX, rowY, rightW, 1, ' ', fg, bg)
			line := fmt.Sprintf("%s[%s] %s", prefix, p.num, p.name)
			s.DrawText(rightX, rowY, line, fg, bg, rightW, isCursor || isActive, false, false)
		}

		typesSepY := y + 7
		s.DrawHLine(rightX, x+w-2, typesSepY, BoxH, BorderDim, BgSurface)
		s.DrawText(rightX, typesSepY, "┤ Категории ├", FgDim, BgSurface, rightW, false, false, false)
		for i, t := range types {
			if i >= 6 || typesSepY+1+i >= y+h-3 {
				break
			}
			rowY := typesSepY + 1 + i
			isCursor := isFocused && a.sidebarIdx == 4+i
			isSelected := strings.EqualFold(a.typeFilter, t)
			fg := FgMuted
			bg := BgSurface
			prefix := "  "
			if isCursor {
				fg = FgWhite
				bg = BgHighlight
				prefix = "► "
			}
			check := "[ ] "
			if isSelected {
				check = "[✓] "
				if !isCursor {
					fg = FgCyan
				}
			}
			s.Fill(rightX, rowY, rightW, 1, ' ', fg, bg)
			s.DrawText(rightX, rowY, prefix+check+t, fg, bg, rightW, isCursor || isSelected, false, false)
		}

		s.DrawText(x+2, y+h-2, "[↑↓] Выбор периода/категории  [Enter] К записям  [←/→] Месяц", FgDim, BgSurface, w-4, false, false, false)
	} else {
		// Narrow mode (Stacked layout in 3-panel sidebar)
		s.DrawText(x+2, y+1, CenterString(mTitle, w-4), FgYellow, BgSurface, w-4, true, false, false)
		s.DrawText(x+2, y+2, "Пн Вт Ср Чт Пт Сб Вс", FgMuted, BgSurface, w-4, false, false, false)

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

		sepY := y + 10
		if sepY < y+h-7 {
			s.DrawHLine(x+1, x+w-2, sepY, BoxH, BorderDim, BgSurface)
			s.DrawText(x+2, sepY, "┤ Периоды ├", FgDim, BgSurface, w-4, false, false, false)

			for i, p := range presets {
				rowY := sepY + 1 + i
				if rowY >= y+h-5 {
					break
				}
				isCursor := isFocused && a.sidebarIdx == i
				isActive := a.periodMode == p.id
				fg := FgMuted
				bg := BgSurface
				prefix := "  "
				if isCursor {
					fg = FgWhite
					bg = BgHighlight
					prefix = "► "
				} else if isActive {
					fg = FgCyan
					prefix = "• "
				}
				s.Fill(x+2, rowY, w-4, 1, ' ', fg, bg)
				line := fmt.Sprintf("%s[%s] %s", prefix, p.num, p.name)
				s.DrawText(x+2, rowY, line, fg, bg, w-4, isCursor || isActive, false, false)
			}

			typesSepY := sepY + 6
			if typesSepY < y+h-3 {
				s.DrawHLine(x+1, x+w-2, typesSepY, BoxH, BorderDim, BgSurface)
				s.DrawText(x+2, typesSepY, "┤ Категории ├", FgDim, BgSurface, w-4, false, false, false)

				for i, t := range types {
					if i >= 4 || typesSepY+1+i >= y+h-2 {
						break
					}
					rowY := typesSepY + 1 + i
					isCursor := isFocused && a.sidebarIdx == 4+i
					isSelected := strings.EqualFold(a.typeFilter, t)
					fg := FgMuted
					bg := BgSurface
					prefix := "  "
					if isCursor {
						fg = FgWhite
						bg = BgHighlight
						prefix = "► "
					}
					check := "[ ] "
					if isSelected {
						check = "[✓] "
						if !isCursor {
							fg = FgCyan
						}
					}
					s.Fill(x+2, rowY, w-4, 1, ' ', fg, bg)
					s.DrawText(x+2, rowY, prefix+check+t, fg, bg, w-4, isCursor || isSelected, false, false)
				}
			}
		}

		if a.width < 115 {
			s.DrawText(x+2, y+h-2, "[Enter] Выбрать  [1/Esc] К списку", FgDim, BgSurface, w-4, false, false, false)
		}
	}
}

func (a *App) renderFeed(s *Screen, x, y, w, h int) {
	borderCol := BorderDim
	isFocused := a.focusPanel == PanelFeed || a.width < 115
	if isFocused {
		borderCol = BorderFocus
	}

	title := fmt.Sprintf("Записи (%d)", len(a.sessions))
	if len(a.selectedIDs) > 0 {
		title += fmt.Sprintf(" [Выбрано: %d]", len(a.selectedIDs))
	}
	s.DrawBox(x, y, w, h, title, FgBlue, borderCol, BgSurface, isFocused)

	// Adaptive column layouts based on width w
	showTime := w >= 65
	showDur := w >= 65
	showCash := w >= 65
	showType := w >= 90

	colCheck := x + 2
	colDate := x + 6
	colTime := x + 17

	colStatus := x + w - 16
	colCash := colStatus - 9
	colDur := colCash - 7

	var colName, wName, colType, wType int

	if showTime && showDur && showCash {
		colName = colTime + 6
		avail := colDur - colName - 1
		if showType && avail > 24 {
			wType = avail * 35 / 100
			if wType > 16 {
				wType = 16
			}
			wName = avail - wType - 1
			colType = colName + wName + 1
		} else {
			wName = avail
			wType = 0
		}
	} else {
		// Narrow layout (e.g. w < 65)
		colName = colDate + 11
		wName = colStatus - colName - 1
		if wName < 10 {
			wName = 10
		}
	}

	// Column Headers
	colY := y + 1
	s.Fill(x+1, colY, w-2, 1, ' ', FgDim, BgCard)
	s.DrawText(colCheck, colY, "[✓]", FgDim, BgCard, 4, false, false, false)
	s.DrawText(colDate, colY, "Дата", FgMuted, BgCard, 10, true, false, false)
	if showTime {
		s.DrawText(colTime, colY, "Время", FgMuted, BgCard, 5, true, false, false)
	}
	s.DrawText(colName, colY, "Название", FgMuted, BgCard, wName, true, false, false)
	if wType > 0 {
		s.DrawText(colType, colY, "Категория", FgMuted, BgCard, wType, true, false, false)
	}
	if showDur {
		s.DrawText(colDur, colY, "Длит.", FgMuted, BgCard, 6, true, false, false)
	}
	if showCash {
		s.DrawText(colCash, colY, "Кэш", FgMuted, BgCard, 8, true, false, false)
	}
	s.DrawText(colStatus, colY, "Статус", FgMuted, BgCard, 14, true, false, false)

	// Rows viewport
	viewportH := h - 3
	if viewportH <= 0 {
		return
	}

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

		isCursor := sessIdx == a.cursorIdx && isFocused
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

		// Date
		s.DrawText(colDate, rowY, sess.Date(), FgDim, rowBg, 10, false, false, false)

		// Time
		if showTime {
			s.DrawText(colTime, rowY, sess.Time, FgWhite, rowBg, 5, true, false, false)
		}

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
		if showDur {
			durStr := fmt.Sprintf("%02d:%02d", sess.Duration/60, sess.Duration%60)
			s.DrawText(colDur, rowY, durStr, FgDim, rowBg, 6, false, false, false)
		}

		// Cash (pure number!)
		if showCash {
			cashStr := ""
			if sess.Cash > 0 {
				cashStr = strconv.FormatFloat(sess.Cash, 'f', -1, 64)
			}
			s.DrawText(colCash, rowY, cashStr, FgWhite, rowBg, 8, false, false, false)
		}

		// Status pill
		badgeText, bFg, _ := StatusBadge(sess.Status)
		s.DrawText(colStatus, rowY, badgeText, bFg, rowBg, 14, true, false, false)
	}
}

func (a *App) renderInspector(s *Screen, x, y, w, h int) {
	borderCol := BorderDim
	isFocused := a.focusPanel == PanelInspector || a.width < 115
	if isFocused {
		borderCol = BorderFocus
	}
	s.DrawBox(x, y, w, h, "Карточка записи", FgGreen, borderCol, BgSurface, isFocused)

	if len(a.sessions) == 0 || a.cursorIdx >= len(a.sessions) {
		s.DrawText(x+2, y+2, "(Запись не выбрана)", FgDim, BgSurface, w-4, false, false, false)
		return
	}

	sess := a.sessions[a.cursorIdx]
	maxContentW := w - 4
	if maxContentW < 12 {
		maxContentW = 12
	}

	actionH := 6
	actionTop := y + h - actionH
	if actionTop < y+8 {
		actionTop = y + 8
	}

	curY := y + 1

	// Title wrapping: neatly wrapped across lines
	titleLines := WrapText(sess.Name, maxContentW)
	if len(titleLines) == 0 {
		titleLines = []string{"(Без названия)"}
	}
	for _, tl := range titleLines {
		if curY >= actionTop-6 {
			break
		}
		s.DrawText(x+2, curY, tl, FgWhite, BgSurface, maxContentW, true, false, false)
		curY++
	}

	// Status badge
	badgeText, bFg, _ := StatusBadge(sess.Status)
	s.DrawText(x+2, curY, badgeText, bFg, BgSurface, maxContentW, true, false, false)
	curY += 2

	// Details list
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

	valW := maxContentW - 10
	if valW < 6 {
		valW = 6
	}

	for _, d := range details {
		if curY >= actionTop-4 {
			break
		}
		vLines := WrapText(d.val, valW)
		if len(vLines) == 0 {
			vLines = []string{"—"}
		}
		s.DrawText(x+2, curY, d.lbl, FgDim, BgSurface, 10, false, false, false)
		s.DrawText(x+12, curY, vLines[0], FgMain, BgSurface, valW, false, false, false)
		curY++
		for _, vl := range vLines[1:] {
			if curY >= actionTop-4 {
				break
			}
			s.DrawText(x+12, curY, vl, FgMain, BgSurface, valW, false, false, false)
			curY++
		}
	}

	// Notes wrapping
	if curY < actionTop-1 {
		s.DrawText(x+2, curY, "Заметки:", FgDim, BgSurface, maxContentW, true, false, false)
		curY++
		notes := sess.Notes
		if notes == "" {
			notes = "(нет заметок)"
		}
		nLines := WrapText(notes, maxContentW)
		for _, nl := range nLines {
			if curY >= actionTop-1 {
				break
			}
			s.DrawText(x+2, curY, nl, FgMuted, BgSurface, maxContentW, false, false, false)
			curY++
		}
	}

	// Quick hotkeys hint pinned to bottom
	if actionTop > y+3 && actionTop < y+h-1 {
		s.DrawHLine(x+1, x+w-2, actionTop, BoxH, BorderDim, BgSurface)
		s.DrawText(x+2, actionTop, "┤ Действия ├", FgDim, BgSurface, maxContentW, false, false, false)
		s.DrawText(x+2, actionTop+1, "[e] Редактировать", FgCyan, BgSurface, maxContentW, false, false, false)
		s.DrawText(x+2, actionTop+2, "[s] Сменить статус", FgBlue, BgSurface, maxContentW, false, false, false)
		s.DrawText(x+2, actionTop+3, "[f] Изменить поле", FgYellow, BgSurface, maxContentW, false, false, false)
		delHint := "[d] Удалить запись"
		if a.width < 115 {
			delHint += "    [1/Esc] К списку"
		}
		s.DrawText(x+2, actionTop+4, delHint, FgRed, BgSurface, maxContentW, false, false, false)
	}
}

func (a *App) renderStats(s *Screen, x, y, w, h int) {
	borderCol := BorderFocus
	title := "Статистика за период"
	s.DrawBox(x, y, w, h, title, FgYellow, borderCol, BgSurface, true)

	// Calculate statistics
	totalEntries := len(a.sessions)
	totalMin := 0
	totalCash := 0.0
	statusCounts := map[string]int{}
	typeCounts := map[string]int{}

	for _, sess := range a.sessions {
		totalMin += sess.Duration
		totalCash += sess.Cash
		statusCounts[sess.Status]++
		t := sess.Type
		if t == "" {
			t = "(без типа)"
		}
		typeCounts[t]++
	}

	pLabel := "Все записи"
	switch a.periodMode {
	case "today":
		pLabel = "Сегодня"
	case "week":
		pLabel = "Эта неделя"
	case "month":
		pLabel = "Этот месяц"
	case "custom":
		pLabel = "Выбранный период"
	}

	totalHours := float64(totalMin) / 60.0
	avgMin := 0
	if totalEntries > 0 {
		avgMin = totalMin / totalEntries
	}

	curY := y + 2
	s.DrawText(x+2, curY, fmt.Sprintf("Период:            %s", pLabel), FgWhite, BgSurface, w-4, true, false, false)
	curY++
	s.DrawText(x+2, curY, fmt.Sprintf("Всего записей:     %d шт.", totalEntries), FgWhite, BgSurface, w-4, true, false, false)
	curY++
	s.DrawText(x+2, curY, fmt.Sprintf("Общее время:       %.1f ч (%d мин)", totalHours, totalMin), FgCyan, BgSurface, w-4, true, false, false)
	curY++
	s.DrawText(x+2, curY, fmt.Sprintf("Среднее на запись: %d мин (%02d:%02d)", avgMin, avgMin/60, avgMin%60), FgDim, BgSurface, w-4, false, false, false)
	curY++
	// Cash is strictly a pure number!
	s.DrawText(x+2, curY, fmt.Sprintf("Суммарный кэш:     %s", strconv.FormatFloat(totalCash, 'f', -1, 64)), FgGreen, BgSurface, w-4, true, false, false)
	curY += 2

	// Status breakdown
	s.DrawHLine(x+1, x+w-2, curY, BoxH, BorderDim, BgSurface)
	s.DrawText(x+2, curY, "┤ Распределение по статусам ├", FgDim, BgSurface, w-4, false, false, false)
	curY++

	statusOrder := []string{"Активно", "Завершено", "Запланировано", "В процессе", "Отменено"}
	for _, st := range statusOrder {
		c := statusCounts[st]
		if c > 0 || totalEntries == 0 {
			bText, bFg, _ := StatusBadge(st)
			s.DrawText(x+2, curY, bText, bFg, BgSurface, 16, true, false, false)
			s.DrawText(x+18, curY, fmt.Sprintf("%d записей", c), FgWhite, BgSurface, w-20, false, false, false)
			curY++
		}
	}
	for st, c := range statusCounts {
		isDefault := false
		for _, def := range statusOrder {
			if def == st {
				isDefault = true
				break
			}
		}
		if !isDefault && c > 0 {
			bText, bFg, _ := StatusBadge(st)
			s.DrawText(x+2, curY, bText, bFg, BgSurface, 16, true, false, false)
			s.DrawText(x+18, curY, fmt.Sprintf("%d записей", c), FgWhite, BgSurface, w-20, false, false, false)
			curY++
		}
	}

	curY++
	// Category breakdown
	if len(typeCounts) > 0 && curY < y+h-4 {
		s.DrawHLine(x+1, x+w-2, curY, BoxH, BorderDim, BgSurface)
		s.DrawText(x+2, curY, "┤ Распределение по категориям ├", FgDim, BgSurface, w-4, false, false, false)
		curY++
		for tName, c := range typeCounts {
			if curY >= y+h-3 {
				break
			}
			s.DrawText(x+2, curY, fmt.Sprintf("• %s:", tName), FgMuted, BgSurface, 20, false, false, false)
			s.DrawText(x+24, curY, fmt.Sprintf("%d записей", c), FgCyan, BgSurface, w-26, true, false, false)
			curY++
		}
	}

	// Hint at bottom
	s.DrawText(x+2, y+h-2, "[1 / Esc] Вернуться к списку записей  [Tab] Следующая вкладка", FgDim, BgSurface, w-4, false, false, false)
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

	var hints []struct {
		key  string
		desc string
	}

	if a.width < 115 {
		hints = []struct {
			key  string
			desc string
		}{
			{"[Tab/1-4]", "Вкладки"},
			{"[↑↓]", "Навигация"},
			{"[Enter]", "Карточка"},
			{"[Space]", "Выбор"},
			{"[s]", "Статус"},
			{"[a]", "Создать"},
			{"[e]", "Правка"},
			{"[f]", "Поле"},
			{"[d]", "Удалить"},
			{"[?]", "Помощь"},
			{"[q]", "Выход"},
		}
	} else {
		hints = []struct {
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
			{"[1-4]", "Период"},
			{"[?]", "Помощь"},
			{"[q]", "Выход"},
		}
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
