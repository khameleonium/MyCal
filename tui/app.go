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
	PanelCalendar  = 0
	PanelSidebar   = 1
	PanelFeed      = 2
	PanelInspector = 3

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

	focusPanel    int // PanelCalendar, PanelSidebar, PanelFeed, PanelInspector
	lastLeftPanel int // PanelCalendar or PanelSidebar (for natural Left arrow restore)
	periodMode    string
	startDate     time.Time
	endDate       time.Time
	typeFilters   map[string]bool
	searchQuery   string

	allSessions []models.Session
	sessions    []models.Session

	cursorIdx    int
	scrollOffset int
	selectedIDs  map[string]bool

	// Calendar state
	calYear        int
	calMonth       int
	calDay         int
	calHeaderFocus bool
	calHeaderBtn   int // 0: ◄, 1: ►, 2: [Год]
	rangeAnchor    *time.Time

	sidebarIdx int // in PanelSidebar: 0..4 (presets), 5.. (categories)

	activeModal Modal
	statusMsg   string
	statusTime  time.Time

	lastDataMod time.Time
	lastCfgMod  time.Time

	sortField string
	sortAsc   bool

	// Cached render state updated on reloadSessions
	cachedStats calendar.StatsSummary
	screenBuf   *Screen
}

// NewApp creates a new TUI application instance.
func NewApp(svc *calendar.Service, cfg *models.Config, cfgPath, version string) *App {
	eff := cfg
	if svc != nil {
		eff = svc.EffectiveConfig(cfg)
	}
	if eff != nil && eff.Theme != "" {
		ApplyTheme(eff.Theme)
	} else {
		ApplyTheme("tokyo")
	}
	now := config.EffectiveToday(cfg)
	var dataMod, cfgMod time.Time
	if svc != nil {
		dataMod = svc.DataFilesModTime()
	}
	if cfgPath != "" {
		if fi, err := os.Stat(cfgPath); err == nil {
			cfgMod = fi.ModTime()
		}
	}
	sortField := "date"
	sortAsc := false
	if eff != nil {
		if eff.SortField != "" {
			sortField = eff.SortField
		}
		sortAsc = eff.SortAsc
	}
	initialPeriod := "today"
	initialSidebarIdx := 1
	if eff != nil && eff.TUIDefaultPeriod != "" {
		switch eff.TUIDefaultPeriod {
		case "all":
			initialPeriod = "all"
			initialSidebarIdx = 0
		case "week":
			initialPeriod = "week"
			initialSidebarIdx = 2
		case "month":
			initialPeriod = "month"
			initialSidebarIdx = 3
		case "today", "day":
			initialPeriod = "today"
			initialSidebarIdx = 1
		default:
			initialPeriod = "today"
			initialSidebarIdx = 1
		}
	}

	app := &App{
		svc:           svc,
		cfg:           cfg,
		cfgPath:       cfgPath,
		version:       version,
		ctx:           context.Background(),
		focusPanel:    PanelFeed,
		lastLeftPanel: PanelCalendar,
		periodMode:    initialPeriod,
		selectedIDs:   make(map[string]bool),
		typeFilters:   make(map[string]bool),
		calYear:       now.Year(),
		calMonth:      int(now.Month()),
		calDay:        now.Day(),
		sidebarIdx:    initialSidebarIdx,
		activeTab:     TabFeed,
		lastDataMod:   dataMod,
		lastCfgMod:    cfgMod,
		sortField:     sortField,
		sortAsc:       sortAsc,
	}
	return app
}

// Run starts the TUI main loop.
func (a *App) Run(ctx context.Context) error {
	a.ctx = ctx

	resizeCh := make(chan struct{}, 1)

	// Enter full-screen raw terminal mode
	term, err := Enter(func() {
		// Signal main loop to handle resize without racing with render
		select {
		case resizeCh <- struct{}{}:
		default:
		}
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

	tickCount := 0
	for {
		select {
		case <-ctx.Done():
			return nil

		case <-resizeCh:
			curW, curH := Size()
			if curW != a.width || curH != a.height {
				a.width = curW
				a.height = curH
			}
			a.render()

		case <-ticker.C:
			tickCount++
			needRender := false

			// Check if file on disk was modified by an external process once per second (every 4th tick)
			if tickCount%4 == 0 {
				if a.svc != nil {
					dataMod := a.svc.DataFilesModTime()
					if !dataMod.IsZero() && dataMod.After(a.lastDataMod) {
						if err := a.svc.Reload(a.ctx); err == nil {
							a.lastDataMod = dataMod
							a.reloadSessions()
							needRender = true
						}
					}
				}
				if a.cfgPath != "" {
					if fi, err := os.Stat(a.cfgPath); err == nil {
						if fi.ModTime().After(a.lastCfgMod) {
							if newCfg, err := config.Load(a.ctx, a.cfgPath); err == nil {
								a.cfg = newCfg
								a.lastCfgMod = fi.ModTime()
								if a.svc != nil {
									newDir := config.EffectiveDataDir(a.cfg.DataPath, a.cfgPath)
									if newDir != a.svc.DataDir() {
										if newSvc, err := calendar.NewService(a.ctx, newDir, a.cfg.ActiveEffectiveBaseName(), a.cfg.ActiveSplitMode()); err == nil {
											a.svc = newSvc
											a.lastDataMod = a.svc.DataFilesModTime()
											a.reloadSessions()
										}
									} else if a.svc.BaseName() != a.cfg.ActiveEffectiveBaseName() || a.svc.Mode() != a.cfg.ActiveSplitMode() {
										_ = a.svc.SwitchCalendar(a.ctx, a.cfg, a.cfg.ActiveCalendar)
										a.lastDataMod = a.svc.DataFilesModTime()
										a.reloadSessions()
									}
								}
								needRender = true
							}
						}
					}
				}
			}

			// Check if dimensions changed (for Windows or polling)
			curW, curH := Size()
			if curW != a.width || curH != a.height {
				a.width = curW
				a.height = curH
				a.render()
			} else if a.statusMsg != "" && time.Since(a.statusTime) > 4*time.Second {
				a.statusMsg = ""
				a.render()
			} else if needRender && a.activeModal == nil {
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

func (a *App) today() time.Time {
	return config.EffectiveToday(a.cfg)
}

func (a *App) setStatusMsg(msg string) {
	a.statusMsg = msg
	a.statusTime = time.Now()
}

func (a *App) persistConfig() {
	if a.cfgPath != "" {
		_ = config.Save(a.ctx, a.cfgPath, a.cfg)
		if fi, err := os.Stat(a.cfgPath); err == nil {
			a.lastCfgMod = fi.ModTime()
		}
	}
}

func (a *App) isTypeFilterActive(t string) bool {
	if a.typeFilters == nil {
		return false
	}
	return a.typeFilters[strings.ToLower(strings.TrimSpace(t))]
}

func (a *App) toggleTypeFilter(t string) {
	if a.typeFilters == nil {
		a.typeFilters = make(map[string]bool)
	}
	key := strings.ToLower(strings.TrimSpace(t))
	if a.typeFilters[key] {
		delete(a.typeFilters, key)
	} else {
		a.typeFilters[key] = true
	}
	a.reloadSessions()
}

func (a *App) hasActiveTypeFilters() bool {
	for _, active := range a.typeFilters {
		if active {
			return true
		}
	}
	return false
}

func (a *App) clearTypeFilters() {
	a.typeFilters = make(map[string]bool)
	a.reloadSessions()
}

func (a *App) isDayInFilter(year, month, day int) bool {
	if a.periodMode == "all" {
		return false // No date highlighted for "Все записи"!
	}
	d := time.Date(year, time.Month(month), day, 12, 0, 0, 0, time.Local)
	switch a.periodMode {
	case "day", "today":
		return year == a.calYear && month == a.calMonth && day == a.calDay
	case "week":
		selDate := time.Date(a.calYear, time.Month(a.calMonth), a.calDay, 12, 0, 0, 0, time.Local)
		weekday := int(selDate.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		mon := selDate.AddDate(0, 0, -(weekday - 1))
		sun := mon.AddDate(0, 0, 6)
		monDate := time.Date(mon.Year(), mon.Month(), mon.Day(), 0, 0, 0, 0, time.Local)
		sunDate := time.Date(sun.Year(), sun.Month(), sun.Day(), 23, 59, 59, 0, time.Local)
		return (d.Equal(monDate) || d.After(monDate)) && (d.Equal(sunDate) || d.Before(sunDate))
	case "month":
		return year == a.calYear && month == a.calMonth
	case "custom":
		if a.startDate.IsZero() || a.endDate.IsZero() {
			return false
		}
		start := time.Date(a.startDate.Year(), a.startDate.Month(), a.startDate.Day(), 0, 0, 0, 0, time.Local)
		end := time.Date(a.endDate.Year(), a.endDate.Month(), a.endDate.Day(), 23, 59, 59, 0, time.Local)
		return (d.Equal(start) || d.After(start)) && (d.Equal(end) || d.Before(end))
	default:
		return false
	}
}

// reloadSessions filters and sorts sessions according to the current period and type.
func (a *App) reloadSessions() {
	a.allSessions = a.svc.All()

	var filtered []models.Session

	var start, end time.Time

	// Preset bounds computed relative to selected calendar date
	selDate := time.Date(a.calYear, time.Month(a.calMonth), a.calDay, 0, 0, 0, 0, time.Local)

	switch a.periodMode {
	case "today", "day":
		start = selDate
		end = start.Add(24*time.Hour - time.Nanosecond)
	case "week":
		weekday := int(selDate.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		mon := selDate.AddDate(0, 0, -(weekday - 1))
		start = time.Date(mon.Year(), mon.Month(), mon.Day(), 0, 0, 0, 0, time.Local)
		end = start.AddDate(0, 0, 7).Add(-time.Nanosecond)
	case "month":
		start = time.Date(a.calYear, time.Month(a.calMonth), 1, 0, 0, 0, 0, time.Local)
		end = start.AddDate(0, 1, 0).Add(-time.Nanosecond)
	case "custom":
		start = a.startDate
		end = a.endDate
	default: // "all"
		// no date limits
	}

	var startKey, endKey string
	if a.periodMode != "all" {
		startKey = start.Format("20060102")
		endKey = end.Format("20060102")
	}

	hasTypeFilter := a.hasActiveTypeFilters()
	hasSearch := a.searchQuery != ""
	var searchLow string
	if hasSearch {
		searchLow = strings.ToLower(a.searchQuery)
	}

	for _, s := range a.allSessions {
		// Period check using zero-allocation YYYYMMDD date key comparison
		if a.periodMode != "all" {
			d := s.DateKey()
			if d == "" || d < startKey || d > endKey {
				continue
			}
		}

		// Type filter check (multi-category)
		if hasTypeFilter {
			sessType := strings.ToLower(strings.TrimSpace(s.Type))
			if !a.typeFilters[sessType] {
				continue
			}
		}

		// Search query check
		if hasSearch {
			if !strings.Contains(strings.ToLower(s.Name), searchLow) &&
				!strings.Contains(strings.ToLower(s.Type), searchLow) &&
				!strings.Contains(strings.ToLower(s.Notes), searchLow) {
				continue
			}
		}

		filtered = append(filtered, s)
	}

	// Sort sessions according to sortField and sortAsc
	calendar.SortSessions(filtered, a.sortField, a.sortAsc)

	a.sessions = filtered
	a.cachedStats = calendar.ComputeStats(filtered)

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

	case k.Matches('?', '?'):
		a.activeModal = NewModalHelp()
		return false

	case k.Matches('o', 'щ'):
		a.activeModal = NewModalSettings(a.cfg)
		return false

	case k.Matches('T', 'Е'):
		a.activeModal = NewModalThemePicker(a)
		return false

	case k.Matches('b', 'и') || k.Matches('B', 'И'):
		a.activeModal = NewModalCalendarManager(a)
		return false

	case k.Matches('y', 'н'):
		a.activeModal = NewModalYearCalendar(a.calYear, a.calMonth, a.calDay, a.allSessions)
		return false

	case k.Matches('p', 'з') || k.Matches('P', 'З'):
		curDate := time.Date(a.calYear, time.Month(a.calMonth), a.calDay, 0, 0, 0, 0, time.Local)
		initFrom := curDate.Format("02.01.2006")
		initTo := curDate.Format("02.01.2006")
		if a.periodMode == "custom" && !a.startDate.IsZero() && !a.endDate.IsZero() {
			initFrom = a.startDate.Format("02.01.2006")
			initTo = a.endDate.Format("02.01.2006")
		}
		a.activeModal = NewModalDateRange(initFrom, initTo)
		return false

	case k.Matches('<', 'б') || k.Rune == '<' || k.Rune == ',' || k.Rune == 'б' || k.Rune == 'Б':
		a.changeMonth(-1)
		a.onMonthChanged()
		return false

	case k.Matches('>', 'ю') || k.Rune == '>' || k.Rune == '.' || k.Rune == 'ю' || k.Rune == 'Ю':
		a.changeMonth(1)
		a.onMonthChanged()
		return false

	case k.Type == KeyTab:
		if a.width < 115 {
			if a.activeTab == TabCalendar {
				if a.focusPanel == PanelCalendar {
					a.focusPanel = PanelSidebar
					a.lastLeftPanel = PanelSidebar
				} else {
					a.focusPanel = PanelCalendar
					a.lastLeftPanel = PanelCalendar
				}
			} else {
				a.activeTab = (a.activeTab + 1) % 4
				if a.activeTab == TabCalendar {
					a.focusPanel = PanelCalendar
					a.lastLeftPanel = PanelCalendar
				} else if a.activeTab == TabFeed {
					a.focusPanel = PanelFeed
				} else if a.activeTab == TabInspector {
					a.focusPanel = PanelInspector
				}
			}
		} else {
			a.focusPanel = (a.focusPanel + 1) % 4
			if a.focusPanel == PanelCalendar {
				a.lastLeftPanel = PanelCalendar
			} else if a.focusPanel == PanelSidebar {
				a.lastLeftPanel = PanelSidebar
			}
		}
		return false

	case k.Type == KeyBackTab:
		if a.width < 115 {
			if a.activeTab == TabCalendar {
				if a.focusPanel == PanelCalendar {
					a.focusPanel = PanelSidebar
					a.lastLeftPanel = PanelSidebar
				} else {
					a.focusPanel = PanelCalendar
					a.lastLeftPanel = PanelCalendar
				}
			} else {
				a.activeTab = (a.activeTab + 3) % 4
				if a.activeTab == TabCalendar {
					a.focusPanel = PanelCalendar
					a.lastLeftPanel = PanelCalendar
				} else if a.activeTab == TabFeed {
					a.focusPanel = PanelFeed
				} else if a.activeTab == TabInspector {
					a.focusPanel = PanelInspector
				}
			}
		} else {
			a.focusPanel = (a.focusPanel + 3) % 4
			if a.focusPanel == PanelCalendar {
				a.lastLeftPanel = PanelCalendar
			} else if a.focusPanel == PanelSidebar {
				a.lastLeftPanel = PanelSidebar
			}
		}
		return false

	case (k.Matches('[', 'х') || k.Matches('{', 'Х')) && a.width < 115:
		a.setCompactTab((a.activeTab + 3) % 4)
		return false

	case (k.Matches(']', 'ъ') || k.Matches('}', 'Ъ')) && a.width < 115:
		a.setCompactTab((a.activeTab + 1) % 4)
		return false

	case k.Type == KeyF1:
		if a.width < 115 {
			a.setCompactTab(TabFeed)
		} else {
			a.focusPanel = PanelCalendar
			a.lastLeftPanel = PanelCalendar
		}
		return false

	case k.Type == KeyF2:
		if a.width < 115 {
			a.setCompactTab(TabCalendar)
		} else {
			a.focusPanel = PanelSidebar
			a.lastLeftPanel = PanelSidebar
		}
		return false

	case k.Type == KeyF3:
		if a.width < 115 {
			a.setCompactTab(TabInspector)
		} else {
			a.focusPanel = PanelFeed
		}
		return false

	case k.Type == KeyF4:
		if a.width < 115 {
			a.setCompactTab(TabStats)
		} else {
			a.activeModal = NewModalStats(a)
		}
		return false

	case k.Matches('1', '1'):
		a.periodMode = "all"
		a.sidebarIdx = 0
		a.reloadSessions()
		a.setStatusMsg("Выбран период: Все записи")
		return false

	case k.Matches('2', '2'):
		a.periodMode = "day"
		a.sidebarIdx = 1
		a.reloadSessions()
		a.setStatusMsg(fmt.Sprintf("Выбран период: День (%02d.%02d.%d)", a.calDay, a.calMonth, a.calYear))
		return false

	case k.Matches('3', '3'):
		a.periodMode = "week"
		a.sidebarIdx = 2
		a.reloadSessions()
		a.setStatusMsg("Выбран период: Неделя")
		return false

	case k.Matches('4', '4'):
		a.periodMode = "month"
		a.sidebarIdx = 3
		a.reloadSessions()
		a.setStatusMsg(fmt.Sprintf("Выбран период: Месяц (%02d.%d)", a.calMonth, a.calYear))
		return false

	case k.Matches('l', 'д') || k.Matches('L', 'Д'):
		a.activeModal = NewModalSort(a)
		return false

	case k.Matches('c', 'с'):
		// Clone / duplicate focused session
		if len(a.sessions) > 0 && a.cursorIdx < len(a.sessions) {
			sess := a.sessions[a.cursorIdx]
			a.activeModal = NewModalCloneSessionForm(&sess, "")
		}
		return false

	case k.Matches('v', 'м'):
		// Quick status cycling (including ~пусто~)
		a.cycleSessionStatus()
		return false

	case k.Matches('m', 'ь'):
		// Move session(s) to another calendar
		a.openMoveToCalendarModal()
		return false

	case k.Matches('u', 'г'):
		// Undo last action
		a.undoLastAction()
		return false

	case k.Type == KeyRune && (k.Rune == 'X' || k.Rune == 'Ч'):
		// Import from CSV / JSON
		a.activeModal = NewModalImport(a)
		return false

	case k.Matches('x', 'ч'):
		// Export current view to CSV
		a.exportCurrentCSV()
		return false

	case k.Matches('i', 'ш'):
		if a.width < 115 {
			a.activeTab = TabInspector
			a.focusPanel = PanelInspector
		} else {
			a.focusPanel = PanelInspector
		}
		return false

	case k.Matches('t', 'е'):
		types := a.getAvailableTypes()
		catStart := 5
		isCategoriesFocused := a.focusPanel == PanelSidebar && a.sidebarIdx >= catStart

		if len(types) == 0 {
			a.focusPanel = PanelSidebar
			a.lastLeftPanel = PanelSidebar
			if a.width < 115 {
				a.activeTab = TabCalendar
			}
			a.setStatusMsg("Список категорий пуст")
			return false
		}

		if !isCategoriesFocused {
			a.focusPanel = PanelSidebar
			a.lastLeftPanel = PanelSidebar
			if a.width < 115 {
				a.activeTab = TabCalendar
			}
			targetIdx := catStart
			for i, t := range types {
				if a.isTypeFilterActive(t) {
					targetIdx = catStart + i
					break
				}
			}
			a.sidebarIdx = targetIdx
			a.setStatusMsg("Блок «Категории»: навигация [↑↓], выбор [Space], повтор [t] для перебора")
		} else {
			a.cycleTypeFilter()
			found := false
			for i, t := range types {
				if a.isTypeFilterActive(t) {
					a.sidebarIdx = catStart + i
					found = true
					break
				}
			}
			if !found {
				a.sidebarIdx = catStart
			}
		}
		return false

	case k.Matches('a', 'ф'):
		// Add new session
		curDate := a.getAddSessionDate()
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
			a.activeModal = NewModalStatusPicker(a.getAvailableStatuses(), "Активно", "")
		} else if len(a.sessions) > 0 && a.cursorIdx < len(a.sessions) {
			sess := a.sessions[a.cursorIdx]
			a.activeModal = NewModalStatusPicker(a.getAvailableStatuses(), sess.Status, sess.ID)
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
			if sess.IsRepeat() {
				total, following := a.svc.SeriesCounts(sess.SeriesID, sess.ID)
				a.activeModal = NewModalConfirmDeleteSeries(sess.ID, sess.SeriesID, total, following)
			} else {
				a.activeModal = NewModalConfirmDelete(sess.ID, 1)
			}
		}
		return false

	case k.Type == KeySpace:
		if a.width < 115 {
			if a.activeTab == TabCalendar {
				if a.focusPanel == PanelCalendar {
					a.handleCalendarKey(k)
				} else {
					a.handleSidebarKey(k)
				}
				return false
			}
		} else {
			if a.focusPanel == PanelCalendar {
				a.handleCalendarKey(k)
				return false
			} else if a.focusPanel == PanelSidebar {
				a.handleSidebarKey(k)
				return false
			}
		}
		// Toggle selection in Feed
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
		if a.rangeAnchor != nil {
			a.rangeAnchor = nil
			a.syncCalendarSelection()
			a.setStatusMsg("Выделение периода отменено")
			return false
		}
		if len(a.selectedIDs) > 0 {
			a.selectedIDs = make(map[string]bool)
			a.setStatusMsg("Выделение снято")
			return false
		}
		if a.hasActiveTypeFilters() {
			a.clearTypeFilters()
			a.setStatusMsg("Фильтры категорий сброшены: Все категории")
			return false
		}
		if a.width < 115 && a.activeTab != TabFeed {
			a.activeTab = TabFeed
			a.focusPanel = PanelFeed
			return false
		}
		if a.focusPanel != PanelFeed {
			a.focusPanel = PanelFeed
			return false
		}
		return false
	}

	// Panel/Tab-specific navigation
	if a.width < 115 {
		switch a.activeTab {
		case TabFeed:
			a.handleFeedKey(k)
		case TabCalendar:
			if a.focusPanel == PanelCalendar {
				a.handleCalendarKey(k)
			} else {
				a.handleSidebarKey(k)
			}
		case TabInspector:
			a.handleInspectorKey(k)
		case TabStats:
			if k.Type == KeyEnter || k.Type == KeyEsc {
				a.activeTab = TabFeed
				a.focusPanel = PanelFeed
			}
		}
		return false
	}

	switch a.focusPanel {
	case PanelCalendar:
		a.handleCalendarKey(k)
	case PanelSidebar:
		a.handleSidebarKey(k)
	case PanelFeed:
		a.handleFeedKey(k)
	case PanelInspector:
		a.handleInspectorKey(k)
	}

	return false
}

func (a *App) onMonthChanged() {
	daysInMonth := time.Date(a.calYear, time.Month(a.calMonth+1), 0, 0, 0, 0, 0, time.Local).Day()
	if a.calDay > daysInMonth {
		a.calDay = daysInMonth
	}
	if a.calDay < 1 {
		a.calDay = 1
	}
	monthsRu := []string{
		"", "Январь", "Февраль", "Март", "Апрель", "Май", "Июнь",
		"Июль", "Август", "Сентябрь", "Октябрь", "Ноябрь", "Декабрь",
	}
	if a.periodMode == "month" {
		a.reloadSessions()
		a.setStatusMsg(fmt.Sprintf("Выбран месяц: %s %d", monthsRu[a.calMonth], a.calYear))
	} else if a.focusPanel == PanelCalendar {
		a.syncCalendarSelection()
	} else {
		a.setStatusMsg(fmt.Sprintf("Календарь: %s %d", monthsRu[a.calMonth], a.calYear))
	}
}

func (a *App) syncCalendarSelection() {
	curDate := time.Date(a.calYear, time.Month(a.calMonth), a.calDay, 0, 0, 0, 0, time.Local)
	if a.rangeAnchor != nil {
		anchor := *a.rangeAnchor
		if curDate.Before(anchor) {
			a.startDate = curDate
			a.endDate = anchor.Add(24*time.Hour - time.Nanosecond)
		} else {
			a.startDate = anchor
			a.endDate = curDate.Add(24*time.Hour - time.Nanosecond)
		}
		a.periodMode = "custom"
		a.setStatusMsg(fmt.Sprintf("Выделен период: %s - %s", a.startDate.Format("02.01.2006"), a.endDate.Format("02.01.2006")))
	} else {
		a.startDate = curDate
		a.endDate = curDate.Add(24*time.Hour - time.Nanosecond)
		a.periodMode = "day"
		a.setStatusMsg(fmt.Sprintf("Выбран день: %02d.%02d.%d", a.calDay, a.calMonth, a.calYear))
	}
	a.reloadSessions()
}

// getAddSessionDate determines the pre-filled date (DD.MM.YYYY) for creating a new session.
// If the user navigated or focused on a day in the calendar (or is filtering by day),
// that day is used automatically, consistent with web-interface behavior.
func (a *App) getAddSessionDate() string {
	// 1. If currently interacting with the calendar panel or tab:
	if a.focusPanel == PanelCalendar || (a.width < 115 && a.activeTab == TabCalendar) {
		if a.calDay > 0 && a.calMonth > 0 && a.calYear > 0 {
			return fmt.Sprintf("%02d.%02d.%04d", a.calDay, a.calMonth, a.calYear)
		}
	}

	// 2. If filtering by day or today (including after calendar day navigation):
	if a.periodMode == "day" || a.periodMode == "today" {
		if a.calDay > 0 && a.calMonth > 0 && a.calYear > 0 {
			return fmt.Sprintf("%02d.%02d.%04d", a.calDay, a.calMonth, a.calYear)
		}
	}

	// 3. If in Feed or Inspector panel with a selected session, inherit that session's date:
	if (a.focusPanel == PanelFeed || a.focusPanel == PanelInspector || (a.width < 115 && a.activeTab == TabInspector)) &&
		len(a.sessions) > 0 && a.cursorIdx < len(a.sessions) {
		sDate := a.sessions[a.cursorIdx].Date()
		if parsed, err := parser.ParseDate(sDate, a.today()); err == nil {
			return fmt.Sprintf("%02d.%02d.%04d", parsed.Day(), parsed.Month(), parsed.Year())
		}
		return sDate
	}

	// 4. Default to current calendar position if valid:
	if a.calDay > 0 && a.calMonth > 0 && a.calYear > 0 {
		return fmt.Sprintf("%02d.%02d.%04d", a.calDay, a.calMonth, a.calYear)
	}

	// 5. Fallback to today:
	now := a.today()
	return fmt.Sprintf("%02d.%02d.%04d", now.Day(), now.Month(), now.Year())
}

func (a *App) handleFeedKey(k Key) {
	switch {
	case k.Type == KeyLeft || k.Matches('h', 'р'):
		if a.width < 115 {
			a.activeTab = TabCalendar
			a.focusPanel = PanelCalendar
		} else {
			if a.lastLeftPanel == PanelSidebar {
				a.focusPanel = PanelSidebar
			} else {
				a.focusPanel = PanelCalendar
			}
		}
	case k.Type == KeyRight:
		if a.width < 115 {
			a.activeTab = TabInspector
			a.focusPanel = PanelInspector
		} else {
			a.focusPanel = PanelInspector
		}
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

func (a *App) handleCalendarKey(k Key) {
	switch {
	case k.Type == KeyHome || (k.Matches('t', 'е') && !a.calHeaderFocus):
		now := a.today()
		a.calYear = now.Year()
		a.calMonth = int(now.Month())
		a.calDay = now.Day()
		a.calHeaderFocus = false
		a.rangeAnchor = nil
		a.syncCalendarSelection()
		a.setStatusMsg("Календарь возвращён на Сегодня")

	case a.calHeaderFocus:
		switch {
		case k.Matches('y', 'н'):
			a.activeModal = NewModalYearCalendar(a.calYear, a.calMonth, a.calDay, a.allSessions)
			return
		case k.Type == KeyLeft || k.Matches('h', 'р'):
			if a.calHeaderBtn > 0 {
				a.calHeaderBtn--
			} else {
				a.changeMonth(-1)
				a.onMonthChanged()
			}
		case k.Type == KeyRight || k.Matches('l', 'д'):
			if a.calHeaderBtn < 1 {
				a.calHeaderBtn++
			} else {
				a.changeMonth(1)
				a.onMonthChanged()
			}
		case k.Type == KeyEnter || k.Type == KeySpace:
			if a.calHeaderBtn == 0 {
				a.changeMonth(-1)
				a.onMonthChanged()
			} else if a.calHeaderBtn == 1 {
				a.changeMonth(1)
				a.onMonthChanged()
			}
		case k.Type == KeyDown || k.Matches('j', 'о'):
			a.calHeaderFocus = false
			if a.calDay < 1 {
				a.calDay = 1
			}
		case k.Type == KeyEsc:
			a.calHeaderFocus = false
		}

	default:
		switch {
		case k.Type == KeyLeft || k.Matches('h', 'р'):
			cur := time.Date(a.calYear, time.Month(a.calMonth), a.calDay, 12, 0, 0, 0, time.Local)
			prev := cur.AddDate(0, 0, -1)
			a.calYear = prev.Year()
			a.calMonth = int(prev.Month())
			a.calDay = prev.Day()
			a.syncCalendarSelection()

		case k.Type == KeyRight:
			cur := time.Date(a.calYear, time.Month(a.calMonth), a.calDay, 12, 0, 0, 0, time.Local)
			next := cur.AddDate(0, 0, 1)
			a.calYear = next.Year()
			a.calMonth = int(next.Month())
			a.calDay = next.Day()
			a.syncCalendarSelection()

		case k.Type == KeyUp || k.Matches('k', 'л'):
			if a.calDay <= 7 {
				a.calHeaderFocus = true
				a.calHeaderBtn = 0
			} else {
				cur := time.Date(a.calYear, time.Month(a.calMonth), a.calDay, 12, 0, 0, 0, time.Local)
				prevWeek := cur.AddDate(0, 0, -7)
				a.calYear = prevWeek.Year()
				a.calMonth = int(prevWeek.Month())
				a.calDay = prevWeek.Day()
				a.syncCalendarSelection()
			}

		case k.Type == KeyDown || k.Matches('j', 'о'):
			cur := time.Date(a.calYear, time.Month(a.calMonth), a.calDay, 12, 0, 0, 0, time.Local)
			nextWeek := cur.AddDate(0, 0, 7)
			if int(nextWeek.Month()) != a.calMonth {
				// Switch focus to Presets list
				a.focusPanel = PanelSidebar
				a.lastLeftPanel = PanelSidebar
				a.sidebarIdx = 0
				a.applySidebarNav(false)
			} else {
				a.calYear = nextWeek.Year()
				a.calMonth = int(nextWeek.Month())
				a.calDay = nextWeek.Day()
				a.syncCalendarSelection()
			}

		case k.Type == KeySpace:
			if a.rangeAnchor == nil {
				anchor := time.Date(a.calYear, time.Month(a.calMonth), a.calDay, 0, 0, 0, 0, time.Local)
				a.rangeAnchor = &anchor
				a.syncCalendarSelection()
				a.setStatusMsg(fmt.Sprintf("Начало периода: %02d.%02d.%d. Стрелками выберите конец, [Space] зафиксировать", a.calDay, a.calMonth, a.calYear))
			} else {
				a.rangeAnchor = nil
				a.setStatusMsg(fmt.Sprintf("Период зафиксирован: %s - %s", a.startDate.Format("02.01.2006"), a.endDate.Format("02.01.2006")))
			}

		case k.Type == KeyEnter:
			if a.rangeAnchor != nil {
				a.rangeAnchor = nil
			}
			a.focusPanel = PanelFeed
			if a.width < 115 {
				a.activeTab = TabFeed
			}
			a.setStatusMsg("Переход к записям")

		case k.Type == KeyEsc:
			if a.rangeAnchor != nil {
				a.rangeAnchor = nil
				a.syncCalendarSelection()
				a.setStatusMsg("Выделение периода отменено")
			} else {
				a.focusPanel = PanelFeed
				if a.width < 115 {
					a.activeTab = TabFeed
				}
			}

		case k.Type == KeyPgUp:
			a.changeMonth(-1)
			a.onMonthChanged()

		case k.Type == KeyPgDn:
			a.changeMonth(1)
			a.onMonthChanged()
		}
	}
}

func (a *App) handleSidebarKey(k Key) {
	types := a.getAvailableTypes()
	numPresets := 5
	maxIdx := numPresets - 1
	if len(types) > 0 {
		maxIdx = numPresets + len(types) - 1
	}

	switch {
	case k.Type == KeyUp || k.Matches('k', 'л'):
		if a.sidebarIdx > 0 {
			a.sidebarIdx--
			a.applySidebarNav(false)
		} else {
			// Move up into Calendar Grid!
			a.focusPanel = PanelCalendar
			a.lastLeftPanel = PanelCalendar
			a.calHeaderFocus = false
			a.setStatusMsg("Переход в сетку календаря")
		}

	case k.Type == KeyRight:
		a.focusPanel = PanelFeed
		if a.width < 115 {
			a.activeTab = TabFeed
		}

	case k.Type == KeyDown || k.Matches('j', 'о'):
		if a.sidebarIdx < maxIdx {
			a.sidebarIdx++
			a.applySidebarNav(false)
		}

	case k.Type == KeyEnter:
		a.applySidebarNav(true)

	case k.Type == KeySpace:
		if a.sidebarIdx == 4 {
			// "Период..."
			a.applySidebarNav(true)
		} else if a.sidebarIdx >= numPresets {
			catIdx := a.sidebarIdx - numPresets
			if catIdx >= 0 && catIdx < len(types) {
				t := types[catIdx]
				a.toggleTypeFilter(t)
				if a.isTypeFilterActive(t) {
					a.setStatusMsg("Включена категория: " + t)
				} else {
					a.setStatusMsg("Отключена категория: " + t)
				}
			}
		}

	case k.Type == KeyEsc:
		if a.hasActiveTypeFilters() {
			a.clearTypeFilters()
			a.setStatusMsg("Фильтры категорий очищены")
		} else {
			a.focusPanel = PanelFeed
			if a.width < 115 {
				a.activeTab = TabFeed
			}
		}
	}
}

func (a *App) applySidebarNav(enterPressed bool) {
	presets := []struct {
		id   string
		name string
	}{
		{"all", "Все записи"},
		{"day", "День"},
		{"week", "Неделя"},
		{"month", "Месяц"},
		{"custom", "Период..."},
	}

	if a.sidebarIdx >= 0 && a.sidebarIdx < len(presets) {
		p := presets[a.sidebarIdx]
		if p.id == "custom" {
			if enterPressed {
				curDate := time.Date(a.calYear, time.Month(a.calMonth), a.calDay, 0, 0, 0, 0, time.Local)
				initFrom := curDate.Format("02.01.2006")
				initTo := curDate.Format("02.01.2006")
				if a.periodMode == "custom" && !a.startDate.IsZero() && !a.endDate.IsZero() {
					initFrom = a.startDate.Format("02.01.2006")
					initTo = a.endDate.Format("02.01.2006")
				}
				a.activeModal = NewModalDateRange(initFrom, initTo)
			}
			return
		}

		a.periodMode = p.id
		a.rangeAnchor = nil
		a.reloadSessions()

		switch p.id {
		case "all":
			a.setStatusMsg("Выбран период: Все записи")
		case "day":
			a.setStatusMsg(fmt.Sprintf("Выбран период: День (%02d.%02d.%d)", a.calDay, a.calMonth, a.calYear))
		case "week":
			a.setStatusMsg("Выбран период: Неделя")
		case "month":
			a.setStatusMsg(fmt.Sprintf("Выбран период: Месяц (%02d.%d)", a.calMonth, a.calYear))
		}

		if enterPressed {
			a.focusPanel = PanelFeed
			if a.width < 115 {
				a.activeTab = TabFeed
			}
		}
		return
	}

	types := a.getAvailableTypes()
	catIdx := a.sidebarIdx - len(presets)
	if catIdx >= 0 && catIdx < len(types) {
		if enterPressed {
			t := types[catIdx]
			a.toggleTypeFilter(t)
			if a.isTypeFilterActive(t) {
				a.setStatusMsg("Включена категория: " + t)
			} else {
				a.setStatusMsg("Отключена категория: " + t)
			}
			a.focusPanel = PanelFeed
			if a.width < 115 {
				a.activeTab = TabFeed
			}
		}
	}
}
func (a *App) setCompactTab(tab int) {
	a.activeTab = tab
	switch tab {
	case TabFeed:
		a.focusPanel = PanelFeed
	case TabCalendar:
		a.focusPanel = PanelCalendar
		a.lastLeftPanel = PanelCalendar
	case TabInspector:
		a.focusPanel = PanelInspector
	}
}

func (a *App) handleInspectorKey(k Key) {
	switch {
	case k.Type == KeyEsc || k.Type == KeyEnter || k.Type == KeyLeft || k.Matches('h', 'р'):
		a.focusPanel = PanelFeed
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
			a.activeModal = NewModalStatusPicker(a.getAvailableStatuses(), sess.Status, sess.ID)
		}
	case k.Matches('f', 'а'):
		if len(a.sessions) > 0 && a.cursorIdx < len(a.sessions) {
			sess := a.sessions[a.cursorIdx]
			a.activeModal = NewModalQuickEditField(sess.ID)
		}
	case k.Matches('d', 'в') || k.Type == KeyDelete:
		if len(a.sessions) > 0 && a.cursorIdx < len(a.sessions) {
			sess := a.sessions[a.cursorIdx]
			if sess.IsRepeat() {
				total, following := a.svc.SeriesCounts(sess.SeriesID, sess.ID)
				a.activeModal = NewModalConfirmDeleteSeries(sess.ID, sess.SeriesID, total, following)
			} else {
				a.activeModal = NewModalConfirmDelete(sess.ID, 1)
			}
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

func (a *App) cycleSessionStatus() {
	if a == nil || a.svc == nil {
		return
	}
	avail := a.getAvailableStatuses()
	if len(a.selectedIDs) > 0 {
		a.svc.BeginUndoTx("быстрая смена статуса")
		var next string
		first := true
		count := 0
		for id := range a.selectedIDs {
			sess, ok := a.svc.ByID(id)
			if !ok {
				continue
			}
			if first {
				next = calendar.NextStatusInCycle(sess.Status, avail)
				first = false
			}
			patch := calendar.Patch{Status: &next}
			if err := a.svc.Update(id, patch); err == nil {
				count++
			}
		}
		a.svc.EndUndoTx()
		if count > 0 {
			if err := a.svc.Save(a.ctx); err != nil {
				a.setStatusMsg("⚠ Ошибка сохранения: " + err.Error())
			} else {
				a.lastDataMod = a.svc.DataFilesModTime()
				a.setStatusMsg(fmt.Sprintf("Статус обновлён для %d записей → %s", count, calendar.FormatStatusDisplay(next)))
			}
			a.reloadSessions()
		}
		return
	}
	if len(a.sessions) > 0 && a.cursorIdx < len(a.sessions) {
		sess := a.sessions[a.cursorIdx]
		next := calendar.NextStatusInCycle(sess.Status, avail)
		patch := calendar.Patch{Status: &next}
		if err := a.svc.Update(sess.ID, patch); err == nil {
			if err := a.svc.Save(a.ctx); err != nil {
				a.setStatusMsg("⚠ Ошибка сохранения: " + err.Error())
			} else {
				a.lastDataMod = a.svc.DataFilesModTime()
				a.setStatusMsg("Статус записи → " + calendar.FormatStatusDisplay(next))
			}
			a.reloadSessions()
		}
	}
}

func (a *App) openMoveToCalendarModal() {
	var ids []string
	if len(a.selectedIDs) > 0 {
		for id := range a.selectedIDs {
			ids = append(ids, id)
		}
		sort.Strings(ids)
	} else if len(a.sessions) > 0 && a.cursorIdx < len(a.sessions) {
		ids = []string{a.sessions[a.cursorIdx].ID}
	}
	if len(ids) == 0 {
		a.setStatusMsg("Нет выбранной записи для переноса")
		return
	}
	a.activeModal = NewModalMoveToCalendar(a, ids)
}

func (a *App) undoLastAction() {
	if a == nil || a.svc == nil {
		return
	}
	desc, err := a.svc.Undo(a.ctx)
	if err != nil {
		a.setStatusMsg("⚠ " + err.Error())
		return
	}
	a.lastDataMod = a.svc.DataFilesModTime()
	a.reloadSessions()
	a.setStatusMsg("↶ Отменено: " + desc)
}

func (a *App) exportCurrentCSV() {
	if a == nil || a.svc == nil {
		return
	}
	if len(a.sessions) == 0 {
		a.setStatusMsg("Нет записей в текущем списке для экспорта")
		return
	}
	path, err := a.svc.ExportCSV(a.sessions)
	if err != nil {
		a.setStatusMsg("⚠ Ошибка экспорта CSV: " + err.Error())
		return
	}
	a.setStatusMsg("Экспортировано в CSV: " + path)
}

func (a *App) cycleTypeFilter() {
	types := a.getAvailableTypes()
	if len(types) == 0 {
		return
	}
	if !a.hasActiveTypeFilters() {
		a.clearTypeFilters()
		a.toggleTypeFilter(types[0])
		a.setStatusMsg("Фильтр категорий: " + types[0])
		return
	}
	var activeList []string
	for _, t := range types {
		if a.isTypeFilterActive(t) {
			activeList = append(activeList, t)
		}
	}
	if len(activeList) == 1 {
		cur := activeList[0]
		for i, t := range types {
			if strings.EqualFold(t, cur) {
				a.clearTypeFilters()
				if i+1 < len(types) {
					a.toggleTypeFilter(types[i+1])
					a.setStatusMsg("Фильтр категорий: " + types[i+1])
				} else {
					a.setStatusMsg("Фильтр категорий сброшен: Все категории")
				}
				return
			}
		}
	}
	a.clearTypeFilters()
	a.setStatusMsg("Фильтр категорий сброшен: Все категории")
}

func (a *App) getAvailableTypes() []string {
	if a == nil || a.svc == nil {
		return nil
	}
	var customTypes, excludedTypes []string
	if a.cfg != nil {
		customTypes = a.cfg.CustomTypes
		excludedTypes = a.cfg.ExcludedTypes
	}
	return a.svc.AvailableTypes(customTypes, excludedTypes)
}

func (a *App) getAvailableStatuses() []string {
	if a == nil || a.svc == nil {
		return []string{"Активно", "Завершено", "В процессе", "Запланировано", "Пропущено", "Отменено"}
	}
	var customStatuses, excludedStatuses []string
	if a.cfg != nil {
		customStatuses = a.cfg.CustomStatuses
		excludedStatuses = a.cfg.ExcludedStatuses
	}
	return a.svc.AvailableStatuses(customStatuses, excludedStatuses)
}

func (a *App) getAvailableNames() []string {
	seen := map[string]bool{}
	var list []string
	if a != nil {
		if a.cfg != nil {
			for _, n := range a.cfg.CustomNames {
				n = strings.TrimSpace(n)
				if n != "" && !seen[n] {
					seen[n] = true
					list = append(list, n)
				}
			}
		}
		for _, s := range a.allSessions {
			n := strings.TrimSpace(s.Name)
			if n != "" && !seen[n] {
				seen[n] = true
				list = append(list, n)
			}
		}
	}
	return list
}

// ----------------- RENDERING -----------------

func (a *App) render() {
	if a.width < 40 || a.height < 10 {
		return
	}

	if a.screenBuf == nil || a.screenBuf.Width != a.width || a.screenBuf.Height != a.height {
		a.screenBuf = NewScreen(a.width, a.height)
	}
	screen := a.screenBuf
	screen.Clear(BgPrimary)

	// 1. Top Header Bar (row 0)
	a.renderHeader(screen)

	// 2. Dynamic footer layout to determine workspace height
	statusLines, hintLines := a.layoutFooter()
	footerH := len(statusLines) + len(hintLines)
	if footerH < 1 {
		footerH = 1
	}

	workspaceY := 1
	if a.width < 115 {
		// Responsive Tab bar at row 1 for vertical/compact proportions
		a.renderTabBar(screen)
		workspaceY = 2
	}
	workspaceH := a.height - workspaceY - footerH
	if workspaceH < 4 {
		workspaceH = 4
	}

	if a.width < 115 {
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

	// 3. Footer Bar (Dynamic multi-line wrapped hotkeys and status)
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
		{TabFeed, "F1", "Записи"},
		{TabCalendar, "F2", "Календарь"},
		{TabInspector, "F3", "Карточка"},
		{TabStats, "F4", "Статистика"},
	}

	cx := 2
	for _, t := range tabs {
		isActive := a.activeTab == t.id
		fg := FgMuted
		bg := BgPanelAlt
		bold := false
		tName := t.name
		if a.width < 52 {
			switch t.id {
			case TabFeed:
				tName = "Зап"
			case TabCalendar:
				tName = "Кал"
			case TabInspector:
				tName = "Карт"
			case TabStats:
				tName = "Стат"
			}
		} else if a.width < 68 {
			switch t.id {
			case TabFeed:
				tName = "Записи"
			case TabCalendar:
				tName = "Календ"
			case TabInspector:
				tName = "Карта"
			case TabStats:
				tName = "Стат"
			}
		} else if a.width < 82 {
			switch t.id {
			case TabFeed:
				tName = "Записи"
			case TabCalendar:
				tName = "Календарь"
			case TabInspector:
				tName = "Карточка"
			case TabStats:
				tName = "Статы"
			}
		}
		label := fmt.Sprintf(" [%s] %s ", t.num, tName)
		if isActive {
			fg = FgWhite
			bg = BgHighlight
			bold = true
			label = fmt.Sprintf(" ► %s: %s ", t.num, tName)
		}
		lblW := StringVisualWidth(label)
		if cx+lblW > a.width-1 {
			break
		}
		s.Fill(cx, 1, lblW, 1, ' ', fg, bg)
		s.DrawText(cx, 1, label, fg, bg, lblW, bold, false, false)
		cx += lblW + 1
	}

	hint := "[[ / ] • F1-F4]"
	if a.width-len(hint)-2 > cx+1 {
		s.DrawText(a.width-len(hint)-2, 1, hint, FgDim, BgPanelAlt, len(hint), false, false, false)
	} else if a.width-9 > cx+1 {
		s.DrawText(a.width-9, 1, "[[ / ]]", FgDim, BgPanelAlt, 7, false, false, false)
	}
}

func (a *App) renderHeader(s *Screen) {
	s.Fill(0, 0, a.width, 1, ' ', FgMain, BgPanelAlt)

	calBadge := "Основной"
	if a.cfg != nil && strings.TrimSpace(a.cfg.ActiveCalendar) != "" {
		calBadge = strings.TrimSpace(a.cfg.ActiveCalendar)
	}
	title := fmt.Sprintf("🗓️ MyCal %s [%s]", a.version, calBadge)
	if a.width < 50 {
		title = fmt.Sprintf("🗓️ MyCal [%s]", calBadge)
	}
	maxTitleW := a.width - 8
	if maxTitleW < 6 {
		maxTitleW = 6
	}
	titleW := StringVisualWidth(title)
	if titleW > maxTitleW {
		title = TruncateString(title, maxTitleW, "…")
		titleW = StringVisualWidth(title)
	}
	s.DrawText(2, 0, title, FgCyan, BgPanelAlt, maxTitleW, true, false, false)

	// Period & filter breadcrumbs
	monthsRu := []string{
		"", "Январь", "Февраль", "Март", "Апрель", "Май", "Июнь",
		"Июль", "Август", "Сентябрь", "Октябрь", "Ноябрь", "Декабрь",
	}
	pLabel := "Все записи"
	now := a.today()
	switch a.periodMode {
	case "today", "day":
		if now.Year() == a.calYear && int(now.Month()) == a.calMonth && now.Day() == a.calDay {
			pLabel = "Сегодня"
		} else {
			pLabel = fmt.Sprintf("День (%02d.%02d.%d)", a.calDay, a.calMonth, a.calYear)
		}
	case "week":
		selDate := time.Date(a.calYear, time.Month(a.calMonth), a.calDay, 12, 0, 0, 0, time.Local)
		weekday := int(selDate.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		mon := selDate.AddDate(0, 0, -(weekday - 1))
		sun := mon.AddDate(0, 0, 6)
		pLabel = fmt.Sprintf("Неделя (%s-%s)", mon.Format("02.01"), sun.Format("02.01"))
	case "month":
		pLabel = fmt.Sprintf("%s %d", monthsRu[a.calMonth], a.calYear)
	case "custom":
		if !a.startDate.IsZero() && !a.endDate.IsZero() {
			pLabel = fmt.Sprintf("%s - %s", a.startDate.Format("02.01"), a.endDate.Format("02.01"))
		} else {
			pLabel = "Период"
		}
	}

	var activeCatNames []string
	for catName, active := range a.typeFilters {
		if active {
			activeCatNames = append(activeCatNames, catName)
		}
	}
	sort.Strings(activeCatNames)
	if len(activeCatNames) > 0 {
		if len(activeCatNames) <= 2 {
			pLabel += fmt.Sprintf(" [%s]", strings.Join(activeCatNames, ", "))
		} else {
			pLabel += fmt.Sprintf(" [%d катег.]", len(activeCatNames))
		}
	}

	stSummary := a.cachedStats
	if stSummary.TotalEntries != len(a.sessions) {
		stSummary = calendar.ComputeStats(a.sessions)
		a.cachedStats = stSummary
	}
	totalHours := stSummary.TotalHours

	startX := 2 + titleW + 2
	clockW := 7
	availInfo := a.width - startX - clockW - 1
	if availInfo > 6 {
		if a.width >= 90 {
			cashStr := calendar.FormatCash(stSummary.TotalCash)
			infoStr := fmt.Sprintf("│ Период: %s (%d зап.) │ Часов: %.1f ч │ Кэш: %s",
				pLabel, stSummary.TotalEntries, totalHours, cashStr)
			s.DrawText(startX, 0, infoStr, FgMuted, BgPanelAlt, availInfo, false, false, false)
		} else if a.width >= 62 {
			infoStr := fmt.Sprintf("│ %s (%d) │ %.1fч", pLabel, stSummary.TotalEntries, totalHours)
			s.DrawText(startX, 0, infoStr, FgMuted, BgPanelAlt, availInfo, false, false, false)
		} else if a.width >= 46 {
			infoStr := fmt.Sprintf("│ %s (%d)", pLabel, stSummary.TotalEntries)
			s.DrawText(startX, 0, infoStr, FgMuted, BgPanelAlt, availInfo, false, false, false)
		} else {
			infoStr := fmt.Sprintf("(%d)", stSummary.TotalEntries)
			s.DrawText(startX, 0, infoStr, FgDim, BgPanelAlt, availInfo, false, false, false)
		}
	}

	// Clock
	if a.width >= 35 {
		nowStr := time.Now().Format("15:04")
		s.DrawText(a.width-6, 0, nowStr, FgWhite, BgPanelAlt, 5, true, false, false)
	}
}

func (a *App) renderCalendarBox(s *Screen, x, y, w, h int) {
	isCalFocused := a.focusPanel == PanelCalendar
	borderCol := BorderDim
	if isCalFocused {
		borderCol = BorderFocus
	}
	s.DrawBox(x, y, w, h, "Календарь [Y]", FgCyan, borderCol, BgSurface, isCalFocused)

	monthsRu := []string{
		"", "Январь", "Февраль", "Март", "Апрель", "Май", "Июнь",
		"Июль", "Август", "Сентябрь", "Октябрь", "Ноябрь", "Декабрь",
	}
	mName := fmt.Sprintf("%s %d", monthsRu[a.calMonth], a.calYear)

	if isCalFocused && a.calHeaderFocus {
		btnPrev := "◄"
		btnNext := "►"
		prevBg := BgSurface
		prevFg := FgYellow
		if a.calHeaderBtn == 0 {
			prevBg = BgHighlight
			prevFg = FgWhite
		}
		nextBg := BgSurface
		nextFg := FgYellow
		if a.calHeaderBtn == 1 {
			nextBg = BgHighlight
			nextFg = FgWhite
		}

		s.DrawText(x+2, y+1, btnPrev, prevFg, prevBg, 1, a.calHeaderBtn == 0, false, false)
		availCenter := w - 8
		if availCenter < 4 {
			availCenter = 4
		}
		s.DrawText(x+4, y+1, CenterString(mName, availCenter), FgWhite, BgSurface, availCenter, true, false, false)
		s.DrawText(x+w-3, y+1, btnNext, nextFg, nextBg, 1, a.calHeaderBtn == 1, false, false)
	} else {
		mTitle := fmt.Sprintf("◄  %s  ►", mName)
		s.DrawText(x+2, y+1, CenterString(mTitle, w-4), FgYellow, BgSurface, w-4, true, false, false)
	}

	wide := w >= 32
	stepX := 3
	availNum := 2
	calLeftMargin := 2
	if wide {
		stepX = 4
		availNum = 3
		calLeftMargin = (w - (7 * 4)) / 2
		if calLeftMargin < 2 {
			calLeftMargin = 2
		}
	}

	// Weekdays
	if wide {
		s.DrawText(x+calLeftMargin, y+2, " Пн  Вт  Ср  Чт  Пт  Сб  Вс", FgMuted, BgSurface, w-4, false, false, false)
	} else {
		s.DrawText(x+2, y+2, "Пн Вт Ср Чт Пт Сб Вс", FgMuted, BgSurface, w-4, false, false, false)
	}

	firstDay := time.Date(a.calYear, time.Month(a.calMonth), 1, 0, 0, 0, 0, time.UTC)
	weekday := int(firstDay.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	weekday-- // 0..6 (Mon..Sun)

	daysInMonth := time.Date(a.calYear, time.Month(a.calMonth+1), 0, 0, 0, 0, 0, time.UTC).Day()
	now := a.today()
	isCurrentMonth := now.Year() == a.calYear && int(now.Month()) == a.calMonth

	daysWithSessions := map[int]bool{}
	ymPrefix := fmt.Sprintf("%04d%02d", a.calYear, a.calMonth)
	for _, sess := range a.allSessions {
		if dStr := sess.DateKey(); len(dStr) == 8 && strings.HasPrefix(dStr, ymPrefix) {
			day := int(dStr[6]-'0')*10 + int(dStr[7]-'0')
			if day >= 1 && day <= 31 {
				daysWithSessions[day] = true
			}
		}
	}

	curX := x + calLeftMargin + (weekday * stepX)
	curY := y + 3

	for d := 1; d <= daysInMonth; d++ {
		if curY >= y+h-1 {
			break
		}
		fg := FgMain
		bg := BgSurface
		bold := false
		under := false

		hasSess := daysWithSessions[d]
		if hasSess {
			fg = FgGreen
			bold = true
		}

		inFilter := a.isDayInFilter(a.calYear, a.calMonth, d)
		if inFilter {
			bg = BgCard
			if !hasSess {
				fg = FgCyan
			}
		}

		isAnchor := (a.rangeAnchor != nil && a.rangeAnchor.Year() == a.calYear && int(a.rangeAnchor.Month()) == a.calMonth && a.rangeAnchor.Day() == d)
		if isAnchor {
			bg = BgHighlight
			fg = FgYellow
			bold = true
		}

		isCursor := isCalFocused && !a.calHeaderFocus && (d == a.calDay)
		if isCursor {
			bg = BgSelected
			fg = FgWhite
			bold = true
		}

		if isCurrentMonth && d == now.Day() {
			under = true
			if !isCursor && !isAnchor {
				bold = true
				if !inFilter && !hasSess {
					fg = FgCyan
				}
			}
		}

		maxDrawX := x + w - 2
		if curX <= maxDrawX {
			avail := maxDrawX - curX + 1
			if avail > availNum {
				avail = availNum
			}
			numStr := fmt.Sprintf("%2d", d)
			if wide {
				numStr = fmt.Sprintf(" %2d", d)
			}
			s.Fill(curX, curY, avail, 1, ' ', fg, bg)
			s.DrawText(curX, curY, numStr, fg, bg, avail, bold, false, under)
		}

		curX += stepX
		weekday++
		if weekday == 7 {
			weekday = 0
			curX = x + calLeftMargin
			curY++
		}
	}

	if h >= 10 {
		hintText := "[Space] Период"
		hintFg := FgDim
		if a.rangeAnchor != nil {
			hintText = "[Space] Закрепить"
			hintFg = FgYellow
		} else if w >= 22 {
			hintText = "[Space] Период  [y] Год"
		}
		s.DrawBottomWrappedHint(x+2, y+h-2, w-4, 2, hintText, hintFg, BgSurface)
	}
}

func (a *App) renderPresetsBox(s *Screen, x, y, w, h int) {
	isSidebarFocused := a.focusPanel == PanelSidebar
	borderCol := BorderDim
	if isSidebarFocused {
		borderCol = BorderFocus
	}
	s.DrawBox(x, y, w, h, "Периоды и категории", FgCyan, borderCol, BgSurface, isSidebarFocused)

	presets := []struct {
		id   string
		name string
		num  string
	}{
		{"all", "Все записи", "1"},
		{"day", "День", "2"},
		{"week", "Неделя", "3"},
		{"month", "Месяц", "4"},
		{"custom", "Период...", "p"},
	}

	now := a.today()
	if now.Year() == a.calYear && int(now.Month()) == a.calMonth && now.Day() == a.calDay {
		presets[1].name = "Сегодня"
	} else {
		presets[1].name = fmt.Sprintf("День (%02d.%02d)", a.calDay, a.calMonth)
	}

	curY := y + 1
	for i, p := range presets {
		if curY >= y+h-1 {
			break
		}
		isCursor := isSidebarFocused && a.sidebarIdx == i
		isActive := a.periodMode == p.id || (p.id == "day" && a.periodMode == "today")
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

		s.Fill(x+2, curY, w-4, 1, ' ', fg, bg)
		line := fmt.Sprintf("%s[%s] %s", prefix, p.num, p.name)
		s.DrawText(x+2, curY, line, fg, bg, w-4, isCursor || isActive, false, false)
		curY++
	}

	types := a.getAvailableTypes()
	if len(types) > 0 && curY < y+h-2 {
		s.DrawSectionDivider(x, curY, w, "Категории", FgCyan, BorderDim, BgSurface)
		curY++

		for i, t := range types {
			if curY >= y+h-1 {
				break
			}
			isCursor := isSidebarFocused && a.sidebarIdx == len(presets)+i
			isSelected := a.isTypeFilterActive(t)

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

			s.Fill(x+2, curY, w-4, 1, ' ', fg, bg)
			line := prefix + check + t
			s.DrawText(x+2, curY, line, fg, bg, w-4, isCursor || isSelected, false, false)
			curY++
		}
	}
}

func (a *App) renderSidebar(s *Screen, x, y, w, h int) {
	if w >= 54 {
		// Wide 2-column layout (Full screen Calendar tab)
		calW := 26
		if w >= 66 {
			calW = 32
		}
		presetsW := w - calW - 1
		a.renderCalendarBox(s, x, y, calW, h)
		a.renderPresetsBox(s, x+calW+1, y, presetsW, h)
	} else {
		// Stacked layout (Narrow / 3-panel sidebar)
		calH := 12
		if calH > h-5 {
			calH = h / 2
		}
		if calH < 8 {
			calH = 8
		}
		a.renderCalendarBox(s, x, y, w, calH)
		presetsH := h - calH
		if presetsH > 2 {
			a.renderPresetsBox(s, x, y+calH, w, presetsH)
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

	// Adaptive column layouts based on width w and visible_fields
	eff := a.svc.EffectiveConfig(a.cfg)
	fieldDateVis := calendar.IsFieldVisible(eff.VisibleFields, calendar.FieldDate)
	fieldTimeVis := calendar.IsFieldVisible(eff.VisibleFields, calendar.FieldTime)
	fieldDurVis := calendar.IsFieldVisible(eff.VisibleFields, calendar.FieldDuration)
	fieldCashVis := calendar.IsFieldVisible(eff.VisibleFields, calendar.FieldCash)
	fieldTypeVis := calendar.IsFieldVisible(eff.VisibleFields, calendar.FieldType)
	fieldStatusVis := calendar.IsFieldVisible(eff.VisibleFields, calendar.FieldStatus)

	showTime := (w >= 50) && fieldTimeVis
	showDur := (w >= 56) && fieldDurVis
	showCash := (w >= 68) && fieldCashVis
	showType := (w >= 88) && fieldTypeVis

	colCheck := x + 2
	colDate := x + 6
	colTime := x + 17
	if !fieldDateVis {
		colTime = x + 6
	}

	wStatus := 0
	colStatus := x + w - 1
	if fieldStatusVis {
		wStatus = 14
		if w < 54 {
			wStatus = 11
		}
		colStatus = x + w - 1 - wStatus
		if colStatus < x+2 {
			colStatus = x + 2
		}
	}

	rightBound := colStatus - 1
	var colCash, colDur int
	if showCash {
		colCash = rightBound - 8
		rightBound = colCash - 1
	}
	if showDur {
		colDur = rightBound - 8
		rightBound = colDur - 1
	}

	var colName, wName, colType, wType int
	if showTime {
		colName = colTime + 6
	} else if fieldDateVis {
		colName = colDate + 11
	} else {
		colName = x + 6
	}

	avail := rightBound - colName
	if avail < 4 {
		avail = 4
	}

	if showType && avail > 24 {
		wType = avail * 32 / 100
		if wType > 16 {
			wType = 16
		}
		wName = avail - wType - 1
		colType = colName + wName + 1
	} else {
		wName = avail
		wType = 0
	}

	// Column Headers
	colY := y + 1
	s.Fill(x+1, colY, w-2, 1, ' ', FgDim, BgCard)
	s.DrawText(colCheck, colY, "[✓]", FgDim, BgCard, 4, false, false, false)

	sortArrow := "▼"
	if a.sortAsc {
		sortArrow = "▲"
	}

	if fieldDateVis {
		dateHdr := "Дата"
		dateFg := FgMuted
		if a.sortField == "date" {
			dateHdr += " " + sortArrow
			dateFg = FgCyan
		}
		s.DrawText(colDate, colY, dateHdr, dateFg, BgCard, 10, true, false, false)
	}

	if showTime {
		timeHdr := "Время"
		timeFg := FgMuted
		if a.sortField == "time" {
			timeHdr += " " + sortArrow
			timeFg = FgCyan
		}
		s.DrawText(colTime, colY, timeHdr, timeFg, BgCard, 7, true, false, false)
	}

	nameHdr := "Название"
	nameFg := FgMuted
	if a.sortField == "name" {
		nameHdr += " " + sortArrow
		nameFg = FgCyan
	}
	s.DrawText(colName, colY, nameHdr, nameFg, BgCard, wName, true, false, false)

	if wType > 0 {
		typeHdr := "Категория"
		typeFg := FgMuted
		if a.sortField == "type" {
			typeHdr += " " + sortArrow
			typeFg = FgCyan
		}
		s.DrawText(colType, colY, typeHdr, typeFg, BgCard, wType, true, false, false)
	}

	if showDur {
		durHdr := "Длит."
		durFg := FgMuted
		if a.sortField == "duration" {
			durHdr += " " + sortArrow
			durFg = FgCyan
		}
		s.DrawText(colDur, colY, PadLeft(durHdr, 8), durFg, BgCard, 8, true, false, false)
	}

	if showCash {
		cashHdr := "Кэш"
		cashFg := FgMuted
		if a.sortField == "cash" {
			cashHdr += " " + sortArrow
			cashFg = FgCyan
		}
		s.DrawText(colCash, colY, PadLeft(cashHdr, 8), cashFg, BgCard, 8, true, false, false)
	}

	if wStatus > 0 {
		statusHdr := "Статус"
		statusFg := FgMuted
		if a.sortField == "status" {
			statusHdr += " " + sortArrow
			statusFg = FgCyan
		}
		s.DrawText(colStatus, colY, statusHdr, statusFg, BgCard, wStatus, true, false, false)
	}

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
		if fieldDateVis {
			s.DrawText(colDate, rowY, sess.Date(), FgDim, rowBg, 10, false, false, false)
		}

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
			unit := "min"
			if eff != nil && eff.DurationUnit == "hours" {
				unit = "hours"
			}
			durStr := calendar.FormatDuration(sess.Duration, unit)
			s.DrawText(colDur, rowY, PadLeft(durStr, 8), FgDim, rowBg, 8, false, false, false)
		}

		// Cash (pure number!)
		if showCash {
			cashStr := "0"
			cashFg := FgMuted
			if sess.Cash != 0 {
				cashStr = strconv.FormatFloat(sess.Cash, 'f', -1, 64)
				cashFg = FgWhite
				if sess.Cash < 0 {
					cashFg = FgRed
				}
			}
			s.DrawText(colCash, rowY, PadLeft(cashStr, 8), cashFg, rowBg, 8, false, false, false)
		}

		// Status pill
		if wStatus > 0 && fieldStatusVis {
			badgeText, bFg, _ := StatusBadge(sess.Status)
			s.DrawText(colStatus, rowY, badgeText, bFg, rowBg, wStatus, true, false, false)
		}
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
	if h < 18 {
		actionH = 5
	}
	if h < 14 {
		actionH = 4
	}
	actionTop := y + h - actionH
	if actionTop < y+5 {
		actionTop = y + 5
	}

	curY := y + 1

	// Title wrapping: neatly wrapped across lines
	titleLines := WrapText(sess.Name, maxContentW)
	if len(titleLines) == 0 {
		titleLines = []string{"(Без названия)"}
	}
	for _, tl := range titleLines {
		if curY >= actionTop-4 {
			break
		}
		s.DrawText(x+2, curY, tl, FgWhite, BgSurface, maxContentW, true, false, false)
		curY++
	}

	eff := a.svc.EffectiveConfig(a.cfg)

	// Status badge
	if calendar.IsFieldVisible(eff.VisibleFields, calendar.FieldStatus) {
		badgeText, bFg, _ := StatusBadge(sess.Status)
		s.DrawText(x+2, curY, badgeText, bFg, BgSurface, maxContentW, true, false, false)
		curY++
		if h >= 18 {
			curY++ // extra space on taller terminals
		}
	}

	// Details list
	durDisp := fmt.Sprintf("%d мин (%.1f ч)", sess.Duration, float64(sess.Duration)/60.0)
	if eff != nil && eff.DurationUnit == "hours" {
		durDisp = fmt.Sprintf("%.1f ч (%d мин)", float64(sess.Duration)/60.0, sess.Duration)
	}

	var details []struct {
		lbl string
		val string
	}
	if calendar.IsFieldVisible(eff.VisibleFields, calendar.FieldDate) {
		details = append(details, struct{ lbl, val string }{"Дата:     ", sess.Date()})
	}
	if calendar.IsFieldVisible(eff.VisibleFields, calendar.FieldTime) {
		details = append(details, struct{ lbl, val string }{"Время:    ", sess.Time})
	}
	if calendar.IsFieldVisible(eff.VisibleFields, calendar.FieldDuration) {
		details = append(details, struct{ lbl, val string }{"Длит.:    ", durDisp})
	}
	if calendar.IsFieldVisible(eff.VisibleFields, calendar.FieldCash) {
		details = append(details, struct{ lbl, val string }{"Кэш:      ", strconv.FormatFloat(sess.Cash, 'f', -1, 64)})
	}
	if calendar.IsFieldVisible(eff.VisibleFields, calendar.FieldType) {
		details = append(details, struct{ lbl, val string }{"Категория:", sess.Type})
	}
	details = append(details, struct{ lbl, val string }{"ID:       ", sess.ID})

	valW := maxContentW - 10
	if valW < 6 {
		valW = 6
	}

	for _, d := range details {
		if curY >= actionTop {
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
			if curY >= actionTop {
				break
			}
			s.DrawText(x+12, curY, vl, FgMain, BgSurface, valW, false, false, false)
			curY++
		}
	}

	// Notes wrapping
	if calendar.IsFieldVisible(eff.VisibleFields, calendar.FieldNotes) && curY < actionTop {
		s.DrawText(x+2, curY, "Заметки:", FgDim, BgSurface, maxContentW, true, false, false)
		curY++
		notes := sess.Notes
		if notes == "" {
			notes = "(нет заметок)"
		}
		nLines := WrapText(notes, maxContentW)
		for _, nl := range nLines {
			if curY >= actionTop {
				break
			}
			s.DrawText(x+2, curY, nl, FgMuted, BgSurface, maxContentW, false, false, false)
			curY++
		}
	}

	// Quick hotkeys hint pinned to bottom
	if actionTop > y+2 && actionTop < y+h-1 {
		s.DrawSectionDivider(x, actionTop, w, "Действия", FgCyan, BorderDim, BgSurface)
		s.DrawText(x+2, actionTop+1, "[e] Редактировать  [c] Дубль", FgCyan, BgSurface, maxContentW, false, false, false)
		s.DrawText(x+2, actionTop+2, "[v/s] Статус       [m] В кал.", FgBlue, BgSurface, maxContentW, false, false, false)
		s.DrawText(x+2, actionTop+3, "[f] Изменить поле  [u] Отмена", FgYellow, BgSurface, maxContentW, false, false, false)
		delHint := "[d] Удалить запись"
		if a.width < 115 {
			delHint += "  [Esc] К списку"
		}
		if actionTop+4 < y+h-1 {
			s.DrawText(x+2, actionTop+4, delHint, FgRed, BgSurface, maxContentW, false, false, false)
		}
	}
}

func (a *App) renderStats(s *Screen, x, y, w, h int) {
	borderCol := BorderFocus
	title := "Статистика за период"
	s.DrawBox(x, y, w, h, title, FgYellow, borderCol, BgSurface, true)

	// Calculate statistics via unified core
	stSummary := a.cachedStats
	if stSummary.TotalEntries != len(a.sessions) {
		stSummary = calendar.ComputeStats(a.sessions)
		a.cachedStats = stSummary
	}
	totalEntries := stSummary.TotalEntries
	totalMin := stSummary.TotalMinutes
	totalCash := stSummary.TotalCash
	statusCounts := stSummary.StatusCounts
	typeCounts := stSummary.TypeCounts
	totalHours := stSummary.TotalHours
	avgMin := int(stSummary.AvgMinutes)

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
	cashFg := FgGreen
	if totalCash < 0 {
		cashFg = FgRed
	}
	s.DrawText(x+2, curY, fmt.Sprintf("Суммарный кэш:     %s", strconv.FormatFloat(totalCash, 'f', -1, 64)), cashFg, BgSurface, w-4, true, false, false)
	curY += 2

	// Status breakdown
	s.DrawSectionDivider(x, curY, w, "Распределение по статусам", FgCyan, BorderDim, BgSurface)
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
		s.DrawSectionDivider(x, curY, w, "Распределение по категориям", FgCyan, BorderDim, BgSurface)
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
	s.DrawBottomWrappedHint(x+2, y+h-2, w-4, 2, "[Esc] Вернуться к списку записей  [[ / ]] Листать разделы", FgDim, BgSurface)
}

type footerHintItem struct {
	key  string
	desc string
	kW   int
	dW   int
}

func (a *App) layoutFooter() (statusLines []string, hintLines [][]footerHintItem) {
	if a.statusMsg != "" {
		statusLines = WrapText(a.statusMsg, a.width-4)
		if len(statusLines) > 2 {
			statusLines = statusLines[:2]
		}
	}

	var hints []struct {
		key  string
		desc string
	}

	if a.width < 115 {
		hints = []struct {
			key  string
			desc string
		}{
			{"[[/]]", "Вкладки"},
			{"[↑↓←→]", "Навигация"},
			{"[Space]", "Выбор"},
			{"[Enter]", "Карточка"},
			{"[v/s]", "Статус"},
			{"[a/c]", "Нов/Дуб"},
			{"[e/f]", "Правка"},
			{"[d]", "Удалить"},
			{"[m]", "Перенос"},
			{"[u]", "Отмена"},
			{"[x/X]", "CSV"},
			{"[1-4/p]", "Период"},
			{"[t]", "Категории"},
			{"[l]", "Сорт."},
			{"[b]", "Календари"},
			{"[o]", "Настройки"},
			{"[?]", "Помощь"},
			{"[q]", "Выход"},
		}
	} else {
		hints = []struct {
			key  string
			desc string
		}{
			{"[↑↓←→]", "Навигация"},
			{"[Space]", "Выбор/Период"},
			{"[Enter]", "Карточка"},
			{"[v/s]", "Статус"},
			{"[a]", "Создать"},
			{"[c]", "Дубль"},
			{"[e/f]", "Правка"},
			{"[d]", "Удалить"},
			{"[m]", "Перенос"},
			{"[u]", "Отмена"},
			{"[x/X]", "Эксп/Имп"},
			{"[l]", "Сортировка"},
			{"[Tab]", "Блок"},
			{"[< >]", "Месяц"},
			{"[1-4/p]", "Период"},
			{"[t]", "Категории"},
			{"[y]", "Год"},
			{"[F4]", "Статистика"},
			{"[b]", "Календари"},
			{"[o]", "Настройки"},
			{"[?]", "Помощь"},
			{"[q]", "Выход"},
		}
	}

	availW := a.width - 4
	if availW < 10 {
		availW = 10
	}

	maxHintLines := 4
	if a.height < 28 {
		maxHintLines = 3
	}
	if a.height < 22 {
		maxHintLines = 2
	}
	if a.height < 14 {
		maxHintLines = 1
	}

	var currentLine []footerHintItem
	curW := 0

	for _, h := range hints {
		kW := StringVisualWidth(h.key)
		dW := StringVisualWidth(h.desc)
		itemW := kW + 1 + dW
		spacing := 2

		if len(currentLine) == 0 {
			currentLine = append(currentLine, footerHintItem{h.key, h.desc, kW, dW})
			curW = itemW
		} else if curW+spacing+itemW <= availW {
			currentLine = append(currentLine, footerHintItem{h.key, h.desc, kW, dW})
			curW += spacing + itemW
		} else {
			hintLines = append(hintLines, currentLine)
			if len(hintLines) >= maxHintLines {
				currentLine = nil
				break
			}
			currentLine = []footerHintItem{{h.key, h.desc, kW, dW}}
			curW = itemW
		}
	}
	if len(currentLine) > 0 && len(hintLines) < maxHintLines {
		hintLines = append(hintLines, currentLine)
	}

	return statusLines, hintLines
}

func (a *App) renderFooter(s *Screen) {
	statusLines, hintLines := a.layoutFooter()
	footerH := len(statusLines) + len(hintLines)
	if footerH < 1 {
		footerH = 1
	}
	curY := a.height - footerH

	// 1. Status notification rows
	for _, sLine := range statusLines {
		if curY >= a.height {
			break
		}
		s.Fill(0, curY, a.width, 1, ' ', FgYellow, BgPanelAlt)
		s.DrawText(2, curY, sLine, FgYellow, BgPanelAlt, a.width-4, true, false, false)
		curY++
	}

	// 2. Hotkey hint rows
	for _, lineItems := range hintLines {
		if curY >= a.height {
			break
		}
		s.Fill(0, curY, a.width, 1, ' ', FgMain, BgPrimary)
		cx := 2
		for _, item := range lineItems {
			if cx+item.kW+item.dW+2 > a.width {
				break
			}
			s.DrawText(cx, curY, item.key, FgCyan, BgPrimary, item.kW, true, false, false)
			cx += item.kW + 1
			s.DrawText(cx, curY, item.desc, FgMuted, BgPrimary, item.dW, false, false, false)
			cx += item.dW + 2
		}
		curY++
	}
}
