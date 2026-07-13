package menu

import (
	"bufio"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"mycalendar/calendar"
	"mycalendar/color"
	"mycalendar/config"
	"mycalendar/models"
	"mycalendar/parser"
)

const (
	cancelWord = "отмена"

	hdrSep   = "═══"
	sep      = "────────────────────────────────────────────────"
	okMark   = "[✓]"
	errMark  = "[✗]"
	warnMark = "[!]"
	askMark  = "[?]"

	listFmt = "  %2d. %-6s %-18s %-20s %-8s [%s]"
	statFmt = "  %-24s %s"
)

var splitModeLabels = map[string]string{
	models.SplitNone:  "В одном файле",
	models.SplitYear:  "По годам",
	models.SplitMonth: "По месяцам",
}

var dateCheckLabels = map[string]string{
	models.DateCheckOff:   "Выключена",
	models.DateCheckAsk:   "Спрашивать",
	models.DateCheckFix:   "Исправлять авто",
	models.DateCheckReask: "Переспрашивать",
}

var dateCheckOrder = []string{
	models.DateCheckOff,
	models.DateCheckAsk,
	models.DateCheckFix,
	models.DateCheckReask,
}

// App runs the interactive console menu.
type App struct {
	svc     *calendar.Service
	cfg     *models.Config
	cfgPath string
	scanner *bufio.Scanner
}

// NewApp creates a new App instance.
func NewApp(svc *calendar.Service, cfg *models.Config, cfgPath string) *App {
	a := &App{
		svc:     svc,
		cfg:     cfg,
		cfgPath: cfgPath,
		scanner: bufio.NewScanner(os.Stdin),
	}
	a.initDate()
	return a
}

// resolveDate returns the current effective date.
func (a *App) resolveDate() time.Time {
	if !a.cfg.UseSystemDate && a.cfg.CustomDate != "" {
		if d, err := time.Parse("2006-01-02", a.cfg.CustomDate); err == nil {
			return d
		}
	}
	return time.Now()
}

// initDate validates system time at startup.
func (a *App) initDate() {
	now := time.Now()
	if now.Year() >= 2024 {
		return
	}
	fmt.Println()
	fmt.Println(color.Yellow(warnMark + " Системная дата некорректна (" + parser.FormatDate(now) + ")"))
	fmt.Println(color.Yellow("  Пожалуйста, введите текущую дату."))
	date, ok := a.dialogDate("Текущая дата:", "DD-MM-YYYY, DD.MM.YYYY, YYYY-MM-DD")
	if ok {
		a.cfg.CustomDate = date.Format("2006-01-02")
		a.cfg.UseSystemDate = false
		if err := config.Save(context.Background(), a.cfgPath, a.cfg); err != nil {
			fmt.Println(color.Red(errMark + " Ошибка сохранения даты: " + err.Error()))
		} else {
			fmt.Println(color.Green(okMark + " Дата сохранена в настройках"))
		}
	}
}

// Run starts the main menu loop.
func (a *App) Run() {
	for {
		fmt.Println()
		fmt.Println(color.Yellow(hdrSep + " Мои записи " + hdrSep))
		fmt.Println(" a | 1. Добавить запись")
		fmt.Println(" e | 2. Редактировать запись")
		fmt.Println(" v | 3. Посмотреть записи")
		fmt.Println(" d | 4. Удалить запись")
		fmt.Println(" t | 5. Сегодня")
		fmt.Println(" x | 6. Экспорт в CSV")
		fmt.Println(" s | 7. Настройки")
		fmt.Println(" q | 8. Выход")

		input := a.prompt("")
		choice := a.resolveHotkey(strings.TrimSpace(input))

		switch choice {
		case "1":
			a.addEntry()
		case "2":
			a.editEntry()
		case "3":
			a.viewEntries("")
		case "4":
			a.deleteEntry()
		case "5":
			a.todayView()
		case "6":
			a.exportCSV()
		case "7":
			a.settingsMenu()
		case "8":
			fmt.Println()
			return
		default:
			fmt.Println(color.Red(errMark + " Некорректный выбор (1–8)"))
		}
	}
}

func (a *App) resolveHotkey(input string) string {
	lower := strings.ToLower(input)
	switch lower {
	case "a", "д":
		return "1"
	case "e", "р":
		return "2"
	case "v", "п":
		return "3"
	case "d", "у":
		return "4"
	case "t", "с":
		return "5"
	case "x", "э":
		return "6"
	case "s", "ы":
		return "7"
	case "q", "в":
		return "8"
	}
	return input
}

// ----------------------------------- 1. Добавление -----------------------------------

func (a *App) addEntry() {
	a.addEntryLoop(false)
}

func (a *App) addEntryLoop(quickAdd bool) {
	first := true
	for {
		if !quickAdd && !first {
			return
		}
		if quickAdd && !first {
			fmt.Println()
			fmt.Println(color.Yellow("  Enter — ещё запись  |  0 — в меню"))
			fmt.Print("> ")
			if isCancelled(a.readLine()) {
				return
			}
		}

		if first || quickAdd {
			fmt.Println()
			if first && !quickAdd {
				fmt.Println(color.Yellow(hdrSep + " Добавление записи " + hdrSep))
			}
		}
		first = false

		date, ok := a.dialogDate("Дата записи:",
			"DD-MM-YYYY, DD.MM.YYYY, YYYY-MM-DD; Enter — сегодня, 0 — отмена")
		if !ok {
			return
		}

		tm, ok := a.askTimeLooped()
		if !ok {
			return
		}

		name := a.askRequired("Наименование записи:")
		if name == "" {
			return
		}

		sessionType, ok := a.askType()
		if !ok {
			return
		}

		duration, ok := a.dialogDuration(a.cfg.DefaultDuration)
		if !ok {
			return
		}

		fmt.Print("Комментарий (Enter — пропустить, 0 — отмена) > ")
		notes := a.readLine()
		if isCancelled(notes) {
			fmt.Println(color.Yellow(warnMark + " Добавление отменено"))
			if quickAdd {
				continue
			}
			return
		}

		status, ok := a.askStatus()
		if !ok {
			fmt.Println(color.Yellow(warnMark + " Добавление отменено"))
			if quickAdd {
				continue
			}
			return
		}

		dateStr := date.Format("2006-01-02")
		timeStr := tm.Format("15:04")

		conflicts := a.svc.FindConflicts(dateStr, timeStr)
		if len(conflicts) > 0 {
			fmt.Println(color.Yellow("\n" + warnMark + " На эту дату и время уже есть записи:"))
			for i := range conflicts {
				fmt.Printf(listFmt+"\n",
					i+1, conflicts[i].Time, color.Green(conflicts[i].Name), conflicts[i].Type,
					color.Orange(fmt.Sprintf("%d мин", conflicts[i].Duration)),
					color.Yellow(conflicts[i].ID))
			}
			a.printConflictMenu()
			action := a.prompt("")
			switch strings.TrimSpace(action) {
			case "1":
				id := a.askID()
				if id != "" {
					a.doEdit(id)
				}
				if quickAdd {
					continue
				}
				return
			case "2":
				id := a.askID()
				if id != "" {
					a.doDelete(id)
				}
				if quickAdd {
					continue
				}
				return
			case "3":
			default:
				return
			}
		}

		id := a.svc.GenerateID(date, tm)
		session := models.Session{
			ID:       id,
			Time:     timeStr,
			Name:     name,
			Type:     sessionType,
			Duration: duration,
			Notes:    notes,
			Status:   status,
		}

		if err := a.svc.AddEntry(session); err != nil {
			fmt.Println(color.Red(errMark + " Ошибка: " + err.Error()))
			if quickAdd {
				continue
			}
			return
		}
		if err := a.svc.Save(context.Background()); err != nil {
			fmt.Println(color.Red(errMark + " Ошибка сохранения: " + err.Error()))
			if quickAdd {
				continue
			}
			return
		}
		fmt.Println(color.Green(okMark + " Запись добавлена  ID: " + color.Yellow(id)))
	}
}

func (a *App) printConflictMenu() {
	fmt.Println()
	fmt.Println("  1. Редактировать существующую")
	fmt.Println("  2. Удалить существующую")
	fmt.Println("  3. Всё равно добавить")
	fmt.Println("  4. Вернуться в меню")
}

// ----------------------------------- 2. Редактирование -----------------------------------

func (a *App) editEntry() {
	fmt.Println()
	fmt.Println(color.Yellow(hdrSep + " Редактирование записи " + hdrSep))

	input := a.dialogPrompt("Дата и время или ID записи:",
		"Например: 29-12-2025 10:51 или 20251229105142; 0 — отмена")
	if isCancelled(input) {
		return
	}
	sessions := a.searchSessions(input)
	if len(sessions) == 0 {
		return
	}

	var target *models.Session
	if len(sessions) == 1 {
		target = &sessions[0]
	} else {
		target = a.pickSession(sessions)
		if target == nil {
			return
		}
	}

	a.doEdit(target.ID)
}

func (a *App) doEdit(id string) {
	session, _, _ := a.svc.FindByID(id)
	if session == nil {
		fmt.Println(color.Red(errMark + " Запись не найдена"))
		return
	}

	a.showSessionDetail(*session)
	fmt.Println("Введите новые значения (Enter — без изменений, 0 — отмена)")
	fmt.Println()

	newTime := a.dialogPrompt("Время ["+session.Time+"]:", "")
	if isCancelled(newTime) {
		fmt.Println(color.Yellow(warnMark + " Редактирование отменено"))
		return
	}

	newName := a.dialogPrompt("Имя ["+color.Green(session.Name)+"]:", "")
	if isCancelled(newName) {
		fmt.Println(color.Yellow(warnMark + " Редактирование отменено"))
		return
	}

	newType := a.dialogPrompt("Тип ["+session.Type+"]:", "")
	if isCancelled(newType) {
		fmt.Println(color.Yellow(warnMark + " Редактирование отменено"))
		return
	}

	durStr := a.dialogPrompt("Продолжительность ["+
		color.Orange(fmt.Sprintf("%d", session.Duration))+"]:", "")
	if isCancelled(durStr) {
		fmt.Println(color.Yellow(warnMark + " Редактирование отменено"))
		return
	}

	newNotes := a.dialogPrompt("Комментарий ["+session.Notes+"]:", "")
	if isCancelled(newNotes) {
		fmt.Println(color.Yellow(warnMark + " Редактирование отменено"))
		return
	}

	newStatus := a.dialogPrompt("Статус ["+session.Status+"]:", "")
	if isCancelled(newStatus) {
		fmt.Println(color.Yellow(warnMark + " Редактирование отменено"))
		return
	}

	if newTime != "" {
		tm, err := parser.ParseTime(newTime)
		if err != nil {
			fmt.Println(color.Red(errMark + " Некорректное время"))
			return
		}
		newTime = tm.Format("15:04")
	}

	var newDuration int
	if durStr != "" {
		var err error
		newDuration, err = strconv.Atoi(strings.TrimSpace(durStr))
		if err != nil || newDuration <= 0 {
			fmt.Println(color.Red(errMark + " Некорректная продолжительность"))
			return
		}
	}

	updated := models.Session{
		Time:     newTime,
		Name:     newName,
		Type:     newType,
		Duration: newDuration,
		Notes:    newNotes,
		Status:   newStatus,
	}

	if err := a.svc.EditEntry(id, updated); err != nil {
		fmt.Println(color.Red(errMark + " Ошибка: " + err.Error()))
		return
	}
	if err := a.svc.Save(context.Background()); err != nil {
		fmt.Println(color.Red(errMark + " Ошибка сохранения: " + err.Error()))
		return
	}
	fmt.Println(color.Green(okMark + " Запись обновлена"))
}

// ----------------------------------- 3. Просмотр -----------------------------------

func (a *App) viewEntries(filter string) {
	fmt.Println()
	fmt.Println(color.Yellow(hdrSep + " Просмотр записей " + hdrSep))
	fmt.Println(" w | 1. Записи за эту неделю")
	fmt.Println(" m | 2. Записи за этот месяц")
	fmt.Println(" p | 3. Указать период вручную")
	fmt.Println(" a | 4. Все записи")
	fmt.Println(" q | 0. Назад")

	choice := strings.TrimSpace(a.prompt(""))
	choice = strings.ToLower(choice)

	var entries []models.DateEntry
	var periodLabel string

	switch choice {
	case "1", "w", "ц":
		entries = a.svc.GetWeekEntries(a.resolveDate())
		monday, sunday := weekBounds(a.resolveDate())
		periodLabel = color.Magenta(parser.FormatDate(monday) + " — " + parser.FormatDate(sunday))
	case "2", "m", "ь":
		entries = a.svc.GetMonthEntries(a.resolveDate())
		firstDay := time.Date(a.resolveDate().Year(), a.resolveDate().Month(), 1, 0, 0, 0, 0, a.resolveDate().Location())
		lastDay := firstDay.AddDate(0, 1, -1)
		periodLabel = color.Magenta(parser.FormatDate(firstDay) + " — " + parser.FormatDate(lastDay))
	case "3", "p", "з":
		startDate, endDate, ok := a.dialogPeriod("Введите период:", "Например: 2025, 12.2025, 10-12, 01.12-15.12; 0 — отмена")
		if !ok {
			return
		}
		entries = a.svc.FindByPeriod(startDate, endDate)
		periodLabel = color.Magenta(parser.FormatDate(startDate) + " — " + parser.FormatDate(endDate))
	case "4", "a", "ф":
		entries = a.svc.GetAllEntries()
		periodLabel = color.Magenta("Все записи")
	case "0", "q", "й":
		return
	default:
		fmt.Println(color.Red(errMark + " Некорректный выбор"))
		return
	}

	fmt.Println()

	if filter != "" {
		fmt.Println(color.Yellow("Фильтр: " + filter))
		matching := a.svc.SearchByNameOrType(filter)
		if len(matching) == 0 {
			fmt.Println(color.Yellow(warnMark + " Ничего не найдено по запросу: " + filter))
			return
		}
		entries = groupByDate(matching)
		periodLabel = color.Magenta("Результаты поиска")
	}

	fmt.Println(color.Magenta(hdrSep + " " + periodLabel + " " + hdrSep))

	if len(entries) == 0 {
		fmt.Println(color.Yellow(warnMark + " Записей за выбранный период не найдено"))
		return
	}

	allSessions := a.printEntries(entries)

	hours := calendar.TotalHours(entries)
	fmt.Printf("\nОбщее время: %s\n", color.Orange(fmt.Sprintf("%.1f ч", hours)))

	a.printStats(allSessions)

	for {
		fmt.Println()
		fmt.Print("Номер или ID для подробностей  |  /текст — фильтр  |  Enter — назад > ")
		idInput := strings.TrimSpace(a.readLine())
		if idInput == "" {
			return
		}

		if strings.HasPrefix(idInput, "/") {
			q := strings.TrimSpace(idInput[1:])
			if q != "" {
				a.viewEntries(q)
				return
			}
			continue
		}

		idx, err := strconv.Atoi(idInput)
		if err == nil && idx >= 1 && idx <= len(allSessions) {
			a.showSessionDetail(allSessions[idx-1])
			continue
		}

		sess, _, _ := a.svc.FindByID(idInput)
		if sess != nil {
			a.showSessionDetail(*sess)
			continue
		}

		fmt.Println(color.Yellow(warnMark + " Запись не найдена"))
	}
}

// ----------------------------------- 4. Удаление -----------------------------------

func (a *App) deleteEntry() {
	for {
		fmt.Println()
		fmt.Println(color.Yellow(hdrSep + " Удаление записи " + hdrSep))
		fmt.Println(" i | 1. По ID / дате и времени")
		fmt.Println(" d | 2. Все записи за день")
		fmt.Println(" p | 3. За период")
		fmt.Println(" a | 4. Удалить все записи")
		fmt.Println(" q | 0. Назад")

		choice := strings.TrimSpace(a.prompt(""))
		choice = strings.ToLower(choice)
		switch choice {
		case "1", "i", "ш":
			a.deleteBySearch()
			return
		case "2", "d", "в":
			a.deleteByDate()
			return
		case "3", "p", "з":
			a.deleteByPeriod()
			return
		case "4", "a", "ф":
			a.deleteAll()
			return
		case "0", "q", "й":
			return
		default:
			fmt.Println(color.Red(errMark + " Некорректный выбор (0–3)"))
		}
	}
}

func (a *App) deleteBySearch() {
	input := a.dialogPrompt("Дата и время или ID записи:",
		"Например: 29-12-2025 10:51 или 20251229105142; 0 — отмена")
	if isCancelled(input) {
		return
	}
	sessions := a.searchSessions(input)
	if len(sessions) == 0 {
		return
	}

	var target *models.Session
	if len(sessions) == 1 {
		target = &sessions[0]
	} else {
		target = a.pickSession(sessions)
		if target == nil {
			return
		}
	}

	a.doDelete(target.ID)
}

func (a *App) deleteByDate() {
	date, ok := a.dialogDate("Дата:", "DD-MM-YYYY, DD.MM.YYYY, YYYY-MM-DD; 0 — отмена")
	if !ok {
		return
	}
	start := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	end := start
	entries := a.svc.FindByPeriod(start, end)
	a.deletePeriod(entries, start, end, parser.FormatDate(date))
}

func (a *App) deleteByPeriod() {
	startDate, endDate, ok := a.dialogPeriod("Введите период для удаления:", "Например: 2025, 12.2025, 10-12, 01.12-15.12; 0 — отмена")
	if !ok {
		return
	}
	start := time.Date(startDate.Year(), startDate.Month(), startDate.Day(), 0, 0, 0, 0, startDate.Location())
	end := time.Date(endDate.Year(), endDate.Month(), endDate.Day(), 0, 0, 0, 0, endDate.Location())
	entries := a.svc.FindByPeriod(start, end)
	label := parser.FormatDate(startDate) + " — " + parser.FormatDate(endDate)
	a.deletePeriod(entries, start, end, label)
}

func (a *App) deletePeriod(entries []models.DateEntry, start, end time.Time, label string) {
	totalSessions := 0
	for _, de := range entries {
		totalSessions += len(de.Sessions)
	}
	if totalSessions == 0 {
		fmt.Println(color.Yellow(warnMark + " Нет записей за " + label))
		return
	}

	fmt.Println(color.Yellow("\n" + warnMark + " Будет удалено " + strconv.Itoa(totalSessions) + " записей за " + label))
	confirm := strings.TrimSpace(a.dialogPrompt("",
		color.Yellow(askMark+" Удалить ВСЕ записи? (Да/Нет)")))
	if !isConfirm(confirm) {
		fmt.Println(color.Yellow(warnMark + " Удаление отменено"))
		return
	}

	count := a.svc.DeleteByPeriod(start, end)
	if count == 0 {
		fmt.Println(color.Yellow(warnMark + " Нечего удалять"))
		return
	}
	if err := a.svc.Save(context.Background()); err != nil {
		fmt.Println(color.Red(errMark + " Ошибка сохранения: " + err.Error()))
		return
	}
	fmt.Println(color.Green(okMark + " Удалено " + strconv.Itoa(count) + " записей"))
}

func (a *App) deleteAll() {
	entries := a.svc.GetAllEntries()
	totalSessions := 0
	for _, de := range entries {
		totalSessions += len(de.Sessions)
	}
	if totalSessions == 0 {
		fmt.Println(color.Yellow(warnMark + " Нет записей для удаления"))
		return
	}

	fmt.Println(color.Yellow("\n" + warnMark + " Будет удалено " + strconv.Itoa(totalSessions) + " записей (ВСЕ записи)"))
	confirm := strings.TrimSpace(a.dialogPrompt("",
		color.Yellow(askMark+" Вы уверены, что хотите удалить ВСЕ записи? (Да/Нет)")))
	if !isConfirm(confirm) {
		fmt.Println(color.Yellow(warnMark + " Удаление отменено"))
		return
	}

	a.svc.DeleteAll()
	if err := a.svc.Save(context.Background()); err != nil {
		fmt.Println(color.Red(errMark + " Ошибка сохранения: " + err.Error()))
		return
	}
	fmt.Println(color.Green(okMark + " Все записи успешно удалены"))
}

func (a *App) doDelete(id string) {
	session, _, _ := a.svc.FindByID(id)
	if session == nil {
		fmt.Println(color.Red(errMark + " Запись не найдена"))
		return
	}

	confirm := strings.TrimSpace(a.dialogPrompt(
		fmt.Sprintf("Запись: %s | %s | %s",
			color.Green(session.Name), color.Magenta(session.Date()), session.Time),
		color.Yellow(askMark+" Удалить? (Да/Нет)")))

	confirmLower := strings.ToLower(confirm)

	if confirmLower == "да" || confirmLower == "д" || confirmLower == "yes" || confirmLower == "y" {
		if err := a.svc.DeleteEntry(id); err != nil {
			fmt.Println(color.Red(errMark + " Ошибка: " + err.Error()))
			return
		}
		if err := a.svc.Save(context.Background()); err != nil {
			fmt.Println(color.Red(errMark + " Ошибка сохранения: " + err.Error()))
			return
		}
		fmt.Println(color.Green(okMark + " Запись удалена"))
	} else {
		fmt.Println(color.Yellow(warnMark + " Удаление отменено"))
	}
}

// ----------------------------------- 5. Сегодня -----------------------------------

func (a *App) todayView() {
	fmt.Println()
	weekdayNames := []string{"вс", "пн", "вт", "ср", "чт", "пт", "сб"}
	wd := weekdayNames[a.resolveDate().Weekday()]
	fmt.Println(color.Yellow(hdrSep + " Сегодня (" + wd + ", " + parser.FormatDate(a.resolveDate()) + ") " + hdrSep))

	entries := a.svc.GetTodayEntries(a.resolveDate())
	if len(entries) == 0 {
		fmt.Println(color.Yellow(warnMark + " На сегодня записей нет"))
		return
	}

	allSessions := a.printEntries(entries)
	hours := calendar.TotalHours(entries)
	fmt.Printf("\nОбщее время: %s\n", color.Orange(fmt.Sprintf("%.1f ч", hours)))

	a.printStats(allSessions)
	a.detailLoop(allSessions, "Номер или ID для подробностей (Enter — назад) > ")
}

// ----------------------------------- 6. Экспорт -----------------------------------

func (a *App) exportCSV() {
	fmt.Println()
	fmt.Println(color.Yellow(hdrSep + " Экспорт в CSV " + hdrSep))
	fmt.Println(" w | 1. Записи за эту неделю")
	fmt.Println(" m | 2. Записи за этот месяц")
	fmt.Println(" p | 3. Указать период вручную")
	fmt.Println(" a | 4. Экспортировать все записи")
	fmt.Println(" q | 0. Назад")

	choice := strings.TrimSpace(a.prompt(""))
	choice = strings.ToLower(choice)

	var entries []models.DateEntry
	switch choice {
	case "1", "w", "ц":
		entries = a.svc.GetWeekEntries(a.resolveDate())
	case "2", "m", "ь":
		entries = a.svc.GetMonthEntries(a.resolveDate())
	case "3", "p", "з":
		startDate, endDate, ok := a.dialogPeriod("Введите период для экспорта:", "Например: 2025, 12.2025, 10-12; 0 — отмена")
		if !ok {
			return
		}
		entries = a.svc.FindByPeriod(startDate, endDate)
	case "4", "a", "ф":
		entries = a.svc.GetAllEntries()
	case "0", "q", "й":
		return
	default:
		fmt.Println(color.Red(errMark + " Некорректный выбор"))
		return
	}

	if len(entries) == 0 {
		fmt.Println(color.Yellow(warnMark + " Нет записей для экспорта"))
		return
	}

	fileName := fmt.Sprintf("export_%s.csv", time.Now().Format("20060102_150405"))
	file, err := os.Create(fileName)
	if err != nil {
		fmt.Println(color.Red(errMark + " Ошибка создания файла: " + err.Error()))
		return
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	writer.Write([]string{"ID", "Дата", "Время", "Имя", "Тип", "Продолжительность (мин)", "Комментарий", "Статус"})

	for _, de := range entries {
		for _, s := range de.Sessions {
			writer.Write([]string{
				s.ID,
				s.Date(),
				s.Time,
				s.Name,
				s.Type,
				strconv.Itoa(s.Duration),
				s.Notes,
				s.Status,
			})
		}
	}

	fmt.Println(color.Green(okMark + " Экспортировано: " + color.Green(fileName)))
}

// ----------------------------------- 7. Настройки -----------------------------------

func (a *App) settingsMenu() {
	oldMode := a.cfg.SplitMode
	for {
		fmt.Println()
		fmt.Println(color.Yellow(hdrSep + " Настройки " + hdrSep))
		fmt.Printf(" m | 1. Режим хранения         [%s]\n", splitModeLabels[a.cfg.SplitMode])
		fmt.Printf(" n | 2. Имя файла данных       [%s]\n", a.cfg.DataFileName)
		fmt.Printf(" l | 3. Продолж. по умолчанию  [%d мин]\n", a.cfg.DefaultDuration)
		fmt.Printf(" t | 4. Тип по умолчанию       [%s]\n", a.cfg.DefaultType)
		fmt.Printf(" p | 5. Путь к данным          [%s]\n", dataPathDisplay(a.cfg.DataPath))
		fmt.Printf(" c | 6. Проверка даты          [%s]\n", dateCheckLabels[a.cfg.DateCheckMode])
		fmt.Printf(" d | 7. Дата                   [%s]\n", a.dateDisplay())
		fmt.Println(" q | 0. Сохранить и выйти")

		input := strings.TrimSpace(a.prompt(""))
		input = strings.ToLower(input)
		switch input {
		case "1", "m", "ь":
			a.cfg.SplitMode = nextSplitMode(a.cfg.SplitMode)
		case "2", "n", "т":
			fmt.Print("Имя файла данных > ")
			name := strings.TrimSpace(a.readLine())
			if name != "" && !isCancelled(name) {
				a.cfg.DataFileName = name
			}
		case "3", "l", "д":
			fmt.Printf("Продолжительность по умолчанию (мин) [%d] > ", a.cfg.DefaultDuration)
			durStr := strings.TrimSpace(a.readLine())
			if durStr != "" && !isCancelled(durStr) {
				if d, err := strconv.Atoi(durStr); err == nil && d > 0 {
					a.cfg.DefaultDuration = d
				} else {
					fmt.Println(color.Yellow(warnMark + " Некорректное значение"))
				}
			}
		case "4", "t", "е":
			fmt.Printf("Тип по умолчанию [%s] > ", a.cfg.DefaultType)
			typ := strings.TrimSpace(a.readLine())
			if typ != "" && !isCancelled(typ) {
				a.cfg.DefaultType = typ
			}
		case "5", "p", "з":
			fmt.Printf("Путь к данным [%s] > ", dataPathDisplay(a.cfg.DataPath))
			path := strings.TrimSpace(a.readLine())
			if path != "" && !isCancelled(path) {
				a.cfg.DataPath = path
			}
		case "6", "c", "с":
			a.cfg.DateCheckMode = nextDateCheckMode(a.cfg.DateCheckMode)
		case "7", "d", "в":
			a.dateSettings()
		case "0", "q", "й":
			if a.cfg.SplitMode != oldMode {
				if err := a.svc.Save(context.Background()); err != nil {
					fmt.Println(color.Red(errMark + " Ошибка: " + err.Error()))
				}
				a.doMigration(oldMode)
			}
			oldMode = a.cfg.SplitMode
			a.svc.UpdateMode(a.cfg.SplitMode)
			if err := config.Save(context.Background(), a.cfgPath, a.cfg); err != nil {
				fmt.Println(color.Red(errMark + " Ошибка сохранения настроек: " + err.Error()))
			} else {
				fmt.Println(color.Green(okMark + " Настройки сохранены"))
			}
			return
		default:
			fmt.Println(color.Red(errMark + " Некорректный выбор (0–7)"))
		}
	}
}

func (a *App) doMigration(oldMode string) {
	if oldMode == models.SplitNone && a.cfg.SplitMode != models.SplitNone {
		fmt.Println(color.Yellow(warnMark + " Данные будут разделены на отдельные файлы."))
		fmt.Print(color.Yellow(askMark + " Удалить старый единый файл? (Да/Нет) > "))
		if isConfirm(a.readLine()) {
			fmt.Println(color.Green(okMark + " Старый файл удалён при следующем сохранении"))
		}
	} else if oldMode != models.SplitNone && a.cfg.SplitMode == models.SplitNone {
		fmt.Println(color.Yellow(warnMark + " Данные будут объединены в один файл."))
		fmt.Print(color.Yellow(askMark + " Удалить старые раздельные файлы? (Да/Нет) > "))
		if isConfirm(a.readLine()) {
			fmt.Println(color.Green(okMark + " Старые файлы удалены при следующем сохранении"))
		}
	} else if oldMode != models.SplitNone && a.cfg.SplitMode != models.SplitNone && oldMode != a.cfg.SplitMode {
		fmt.Println(color.Yellow(warnMark + " Режим разделения изменён. Данные перегруппируются."))
	}
}

func nextSplitMode(current string) string {
	order := []string{models.SplitNone, models.SplitYear, models.SplitMonth}
	for i, m := range order {
		if m == current {
			return order[(i+1)%len(order)]
		}
	}
	return models.SplitNone
}

func nextDateCheckMode(current string) string {
	for i, m := range dateCheckOrder {
		if m == current {
			return dateCheckOrder[(i+1)%len(dateCheckOrder)]
		}
	}
	return models.DateCheckAsk
}

func dataPathDisplay(p string) string {
	if p == "" || p == "." {
		return "./"
	}
	return p
}

func (a *App) dateDisplay() string {
	if a.cfg.UseSystemDate {
		return "Системная"
	}
	if a.cfg.CustomDate != "" {
		return a.cfg.CustomDate
	}
	return "Системная"
}

func (a *App) dateSettings() {
	for {
		fmt.Println()
		fmt.Println(color.Yellow("  Дата"))
		fmt.Printf("  Текущая: %s\n", color.Magenta(parser.FormatDate(a.resolveDate())))
		fmt.Println(" s | 1. Системное время")
		fmt.Println(" m | 2. Ввести дату вручную")
		fmt.Println(" q | 0. Назад")

		input := strings.TrimSpace(a.prompt(""))
		input = strings.ToLower(input)
		switch input {
		case "1", "s", "ы":
			a.cfg.UseSystemDate = true
			fmt.Println(color.Green(okMark + " Используется системное время"))
			return
		case "2", "m", "ь":
			date, ok := a.dialogDate("Текущая дата:", "DD-MM-YYYY, DD.MM.YYYY, YYYY-MM-DD; 0 — отмена")
			if ok {
				a.cfg.CustomDate = date.Format("2006-01-02")
				a.cfg.UseSystemDate = false
				fmt.Println(color.Green(okMark + " Установлено: " + color.Magenta(parser.FormatDate(date))))
			}
			return
		case "0", "q", "й":
			return
		default:
			fmt.Println(color.Red(errMark + " Некорректный выбор (0–2)"))
		}
	}
}

func isConfirm(s string) bool {
	t := strings.TrimSpace(strings.ToLower(s))
	return t == "да" || t == "д" || t == "yes" || t == "y"
}

// ================================= helpers =================================

func isCancelled(s string) bool {
	t := strings.TrimSpace(strings.ToLower(s))
	return t == "0" || t == cancelWord
}

func (a *App) dialogPrompt(title, help string) string {
	fmt.Println(title)
	if help != "" {
		fmt.Println(help)
	}
	fmt.Print("> ")
	return a.readLine()
}

func (a *App) dialogPeriod(title, help string) (time.Time, time.Time, bool) {
	for {
		input := a.dialogPrompt(title, help)
		if isCancelled(input) {
			return time.Time{}, time.Time{}, false
		}
		start, end, err := parser.ParsePeriod(input)
		if err != nil {
			fmt.Println(color.Red(errMark + " " + err.Error()))
			continue
		}
		return start, end, true
	}
}

func (a *App) dialogDate(title, help string) (time.Time, bool) {
	for {
		input := a.dialogPrompt(title, help)
		if isCancelled(input) {
			return time.Time{}, false
		}
		date, err := parser.ParseDate(input, a.resolveDate())
		if err != nil {
			fmt.Println(color.Red(errMark + " Не удалось распознать дату"))
			continue
		}

		if a.cfg.DateCheckMode == "" || a.cfg.DateCheckMode == models.DateCheckOff {
			return date, true
		}

		if !parser.ValidateDate(input, date) {
			date, ok := a.handleBadDate(input, date)
			if !ok {
				continue
			}
			return date, true
		}
		return date, true
	}
}

func (a *App) handleBadDate(raw string, corrected time.Time) (time.Time, bool) {
	switch a.cfg.DateCheckMode {
	case models.DateCheckFix:
		fmt.Println(color.Yellow(warnMark + " \"" + raw + "\" скорректирована на " + parser.FormatDate(corrected)))
		return corrected, true
	case models.DateCheckReask:
		fmt.Println(color.Yellow(warnMark + " Некорректная дата, введите заново"))
		return time.Time{}, false
	case models.DateCheckAsk:
		correctedStr := parser.FormatDate(corrected)
		fmt.Printf(color.Yellow("\n"+warnMark+" \"%s\" не является корректной датой.\n"), raw)
		fmt.Printf("    Будет преобразовано в %s\n", correctedStr)
		fmt.Println()
		fmt.Println("  1. Принять (" + correctedStr + ")")
		fmt.Println("  2. Ввести другую дату")

		choice := strings.TrimSpace(a.prompt(""))
		switch choice {
		case "1":
			fmt.Print(color.Yellow(askMark + " Запомнить выбор? (Да/Нет) > "))
			if isConfirm(a.readLine()) {
				a.cfg.DateCheckMode = models.DateCheckFix
			}
			return corrected, true
		default:
			return time.Time{}, false
		}
	}
	return corrected, true
}

func (a *App) dialogDuration(defaultDur int) (int, bool) {
	input := a.dialogPrompt(
		fmt.Sprintf("Продолжительность (мин) [%d]:", defaultDur),
		"Enter — "+strconv.Itoa(defaultDur)+"; 0 — отмена")
	if isCancelled(input) {
		return 0, false
	}
	if strings.TrimSpace(input) == "" {
		return defaultDur, true
	}
	d, err := strconv.Atoi(strings.TrimSpace(input))
	if err != nil || d <= 0 {
		fmt.Println(color.Yellow(warnMark + " Некорректно, используется " + strconv.Itoa(defaultDur)))
		return defaultDur, true
	}
	return d, true
}

func (a *App) askTimeLooped() (time.Time, bool) {
	for {
		input := a.dialogPrompt("Время записи:", "HH:MM, HH MM — обязательно; 0 — отмена")
		if isCancelled(input) {
			return time.Time{}, false
		}
		tm, err := parser.ParseTime(input)
		if errors.Is(err, parser.ErrEmptyTime) {
			fmt.Println(color.Yellow(warnMark + " Время не указано"))
			continue
		}
		if err != nil {
			fmt.Println(color.Red(errMark + " Некорректное время"))
			continue
		}
		return tm, true
	}
}

func (a *App) askRequired(title string) string {
	for {
		input := a.dialogPrompt(title, "Обязательное поле; 0 — отмена")
		if isCancelled(input) {
			return ""
		}
		if strings.TrimSpace(input) != "" {
			return strings.TrimSpace(input)
		}
		fmt.Println(color.Yellow(warnMark + " Поле не может быть пустым"))
	}
}

func (a *App) askType() (string, bool) {
	types := a.svc.AllTypes()
	fmt.Println()
	fmt.Println(color.Yellow("Тип записи:"))
	for i, t := range types {
		fmt.Printf("  %d. %s\n", i+1, t)
	}
	fmt.Printf("  %d. Ручной ввод\n", len(types)+1)

	defaultIdx := 1
	if a.cfg.DefaultType != "" {
		for i, t := range types {
			if strings.EqualFold(t, a.cfg.DefaultType) {
				defaultIdx = i + 1
				break
			}
		}
	}

	for {
		fmt.Printf("> ")
		input := strings.TrimSpace(a.readLine())
		if isCancelled(input) {
			return "", false
		}
		if input == "" {
			return types[defaultIdx-1], true
		}

		idx, err := strconv.Atoi(input)
		if err != nil || idx < 1 || idx > len(types)+1 {
			fmt.Println(color.Yellow(warnMark + " Введите номер из списка"))
			continue
		}

		if idx == len(types)+1 {
			custom := strings.TrimSpace(a.dialogPrompt("Название нового типа:", "0 — отмена"))
			if isCancelled(custom) {
				return "", false
			}
			if custom == "" {
				return types[defaultIdx-1], true
			}
			return custom, true
		}

		return types[idx-1], true
	}
}

func (a *App) askStatus() (string, bool) {
	statuses := a.svc.AllStatuses()
	for _, st := range a.cfg.CustomStatuses {
		if st != "" && !slices.Contains(statuses, st) {
			statuses = append(statuses, st)
		}
	}

	fmt.Println()
	fmt.Println(color.Yellow("Статус:"))
	fmt.Println("  0. <пусто>")
	for i, st := range statuses {
		fmt.Printf("  %d. %s\n", i+1, st)
	}
	fmt.Printf("  %d. Ручной ввод\n", len(statuses)+1)

	for {
		fmt.Printf("> ")
		input := strings.TrimSpace(a.readLine())
		if isCancelled(input) {
			return "", false
		}
		if input == "" || input == "0" {
			return "", true
		}

		idx, err := strconv.Atoi(input)
		if err != nil || idx < 1 || idx > len(statuses)+1 {
			fmt.Println(color.Yellow(warnMark + " Введите номер из списка"))
			continue
		}

		if idx == len(statuses)+1 {
			custom := strings.TrimSpace(a.dialogPrompt("Название статуса:", "0 — отмена"))
			if isCancelled(custom) {
				return "", false
			}
			if custom == "" {
				return "", true
			}
			fmt.Print(color.Yellow(askMark + " Добавить в список? (Да/Нет) > "))
			if isConfirm(a.readLine()) {
				a.cfg.CustomStatuses = append(a.cfg.CustomStatuses, custom)
			}
			return custom, true
		}

		return statuses[idx-1], true
	}
}

func (a *App) askID() string {
	for {
		input := a.dialogPrompt("ID записи:", "14 цифр; 0 — отмена")
		if isCancelled(input) {
			return ""
		}
		id := strings.TrimSpace(input)
		if len(id) != models.IDLen {
			fmt.Println(color.Red(errMark + " Некорректный ID (должно быть 14 цифр)"))
			continue
		}
		sess, _, _ := a.svc.FindByID(id)
		if sess != nil {
			return id
		}
		fmt.Println(color.Yellow(warnMark + " Запись с таким ID не найдена"))
	}
}

func (a *App) searchSessions(input string) []models.Session {
	input = strings.TrimSpace(input)
	if input == "" {
		fmt.Println(color.Yellow(warnMark + " Пустой ввод"))
		return nil
	}

	if len(input) == models.IDLen && isDigits(input) {
		sess, _, _ := a.svc.FindByID(input)
		if sess != nil {
			return []models.Session{*sess}
		}
		fmt.Println(color.Yellow(warnMark + " Запись с таким ID не найдена"))
		return nil
	}

	var datePart, timePart string

	if strings.Contains(input, ":") {
		datePart, timePart = splitByLastToken(input, ":")
	}

	if datePart == "" && timePart == "" {
		d, tm, err := parser.ParseDateTime(input)
		if err == nil {
			datePart = d.Format("2006-01-02")
			timePart = tm.Format("15:04")
		}
	}

	if datePart == "" {
		datePart = input
	}

	date, err := parser.ParseDate(datePart, time.Time{})
	if err != nil {
		fmt.Println(color.Red(errMark + " Некорректная дата"))
		return nil
	}
	dateStr := date.Format("2006-01-02")

	if timePart == "" {
		de := a.svc.FindByDate(dateStr)
		if de == nil || len(de.Sessions) == 0 {
			fmt.Println(color.Yellow(warnMark + " Записей на " + color.Magenta(parser.FormatDate(date)) + " не найдено"))
			return nil
		}
		return de.Sessions
	}

	tm, err := parser.ParseTime(timePart)
	if err != nil {
		fmt.Println(color.Red(errMark + " Некорректное время"))
		return nil
	}
	timeStr := tm.Format("15:04")
	sessions := a.svc.FindByDateTime(dateStr, timeStr)
	if len(sessions) == 0 {
		fmt.Println(color.Yellow(warnMark + " Записей на " +
			color.Magenta(parser.FormatDate(date)) + " " + timeStr + " не найдено"))
		return nil
	}
	return sessions
}

func (a *App) pickSession(sessions []models.Session) *models.Session {
	fmt.Println("\nНайдено несколько записей:")
	for i, s := range sessions {
		fmt.Printf(listFmt+"\n",
			i+1, s.Time, color.Green(s.Name), s.Type,
			color.Orange(fmt.Sprintf("%d мин", s.Duration)),
			color.Yellow(s.ID))
	}
	fmt.Print("\n> ")
	input := strings.TrimSpace(a.readLine())
	idx, err := strconv.Atoi(input)
	if err != nil || idx < 1 || idx > len(sessions) {
		fmt.Println(color.Red(errMark + " Некорректный выбор"))
		return nil
	}
	return &sessions[idx-1]
}

func (a *App) showSessionDetail(s models.Session) {
	fmt.Println()
	fmt.Println(sep)
	fmt.Printf("  ИД:              %s\n", color.Yellow(s.ID))
	fmt.Printf("  Дата:            %s\n", color.Magenta(formatDateFromID(s.Date())))
	fmt.Printf("  Время:           %s\n", s.Time)
	fmt.Printf("  Имя:             %s\n", color.Green(s.Name))
	fmt.Printf("  Тип:             %s\n", s.Type)
	fmt.Printf("  Продолжительность: %s\n", color.Orange(fmt.Sprintf("%d мин", s.Duration)))
	if s.Notes != "" {
		fmt.Printf("  Комментарий:      %s\n", s.Notes)
	}
	if s.Status != "" {
		fmt.Printf("  Статус:           %s\n", s.Status)
	}
	fmt.Println(sep)
}

func (a *App) printEntries(entries []models.DateEntry) []models.Session {
	var all []models.Session

	for _, de := range entries {
		d, _ := time.Parse("2006-01-02", de.Date)
		fmt.Println(color.Magenta(d.Format("02-01-2006")))

		for _, s := range de.Sessions {
			num := len(all) + 1
			fmt.Printf(listFmt+"\n",
				num, s.Time, color.Green(s.Name), s.Type,
				color.Orange(fmt.Sprintf("%d мин", s.Duration)),
				color.Yellow(s.ID))
			all = append(all, s)
		}
	}

	return all
}

func (a *App) printStats(sessions []models.Session) {
	if len(sessions) == 0 {
		return
	}

	totalMin := 0
	typeMin := make(map[string]int)

	for _, s := range sessions {
		totalMin += s.Duration
		typeMin[s.Type] += s.Duration
	}

	fmt.Println()
	fmt.Println(sep)

	fmt.Printf(statFmt+"\n", "Всего записей:", color.Orange(fmt.Sprintf("%d", len(sessions))))
	fmt.Printf(statFmt+"\n", "Средняя продолж.:", color.Orange(fmt.Sprintf("%.1f мин", float64(totalMin)/float64(len(sessions)))))

	fmt.Println()
	fmt.Println(color.Yellow("  По типам записей:"))
	typeKeys := make([]string, 0, len(typeMin))
	for t := range typeMin {
		typeKeys = append(typeKeys, t)
	}
	sort.Strings(typeKeys)
	for _, t := range typeKeys {
		fmt.Printf(statFmt+"\n", "    "+t+":",
			color.Orange(fmt.Sprintf("%.1f ч (%d мин)", float64(typeMin[t])/60.0, typeMin[t])))
	}

	fmt.Println("  " + sep[:36])
	fmt.Printf(statFmt+"\n", "  Общее время:",
		color.Orange(fmt.Sprintf("%.1f ч (%d мин)", float64(totalMin)/60.0, totalMin)))

	fmt.Println(sep)
}

func (a *App) prompt(msg string) string {
	if msg != "" {
		fmt.Println(msg)
	}
	fmt.Print("> ")
	return a.readLine()
}

func (a *App) readLine() string {
	if !a.scanner.Scan() {
		if err := a.scanner.Err(); err != nil {
			return ""
		}
		return ""
	}
	return a.scanner.Text()
}

func isDigits(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func (a *App) detailLoop(allSessions []models.Session, prompt string) {
	for {
		fmt.Println()
		fmt.Print(prompt)
		idInput := strings.TrimSpace(a.readLine())
		if idInput == "" {
			return
		}

		idx, err := strconv.Atoi(idInput)
		if err == nil && idx >= 1 && idx <= len(allSessions) {
			a.showSessionDetail(allSessions[idx-1])
			continue
		}

		sess, _, _ := a.svc.FindByID(idInput)
		if sess != nil {
			a.showSessionDetail(*sess)
			continue
		}

		fmt.Println(color.Yellow(warnMark + " Запись не найдена"))
	}
}

func weekBounds(ref time.Time) (monday, sunday time.Time) {
	wd := ref.Weekday()
	if wd == 0 {
		wd = 7
	}
	monday = ref.AddDate(0, 0, -int(wd-1))
	sunday = monday.AddDate(0, 0, 6)
	return
}

func formatDateFromID(d string) string {
	if len(d) == 10 {
		return d[8:10] + "-" + d[5:7] + "-" + d[0:4]
	}
	return d
}

func splitByLastToken(input, sep string) (before, after string) {
	fields := strings.Fields(input)
	for i := len(fields) - 1; i >= 0; i-- {
		if strings.Contains(fields[i], sep) {
			after = fields[i]
			before = strings.Join(fields[:i], " ")
			return
		}
	}
	return input, ""
}

func groupByDate(sessions []models.Session) []models.DateEntry {
	dateMap := make(map[string][]models.Session)
	for _, s := range sessions {
		d := s.Date()
		dateMap[d] = append(dateMap[d], s)
	}

	var result []models.DateEntry
	for date, sessList := range dateMap {
		sort.Slice(sessList, func(i, j int) bool {
			return sessList[i].Time < sessList[j].Time
		})
		result = append(result, models.DateEntry{
			Date:     date,
			Sessions: sessList,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Date < result[j].Date
	})
	return result
}

// --- Public entry points (CLI and internal) ---

func (a *App) AddEntryQuick() {
	a.addEntryLoop(true)
}

func (a *App) TodayView() {
	a.todayView()
}

func (a *App) WeekView() {
	entries := a.svc.GetWeekEntries(a.resolveDate())
	monday, sunday := weekBounds(a.resolveDate())
	a.showPeriod(entries, parser.FormatDate(monday)+" — "+parser.FormatDate(sunday))
}

func (a *App) MonthView() {
	entries := a.svc.GetMonthEntries(a.resolveDate())
	firstDay := time.Date(a.resolveDate().Year(), a.resolveDate().Month(), 1, 0, 0, 0, 0, a.resolveDate().Location())
	lastDay := firstDay.AddDate(0, 1, -1)
	a.showPeriod(entries, parser.FormatDate(firstDay)+" — "+parser.FormatDate(lastDay))
}

func (a *App) showPeriod(entries []models.DateEntry, label string) {
	fmt.Println()
	fmt.Println(color.Magenta(hdrSep + " " + label + " " + hdrSep))

	if len(entries) == 0 {
		fmt.Println(color.Yellow(warnMark + " Записей не найдено"))
		return
	}

	allSessions := a.printEntries(entries)
	hours := calendar.TotalHours(entries)
	fmt.Printf("\nОбщее время: %s\n", color.Orange(fmt.Sprintf("%.1f ч", hours)))
	a.printStats(allSessions)
	a.detailLoop(allSessions, "Номер или ID для подробностей (Enter — назад) > ")
}
