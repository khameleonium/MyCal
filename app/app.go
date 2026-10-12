// Package app implements the interactive console UI and the CLI subcommands.
package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"

	"mycalendar/calendar"
	"mycalendar/color"
	"mycalendar/config"
	"mycalendar/models"
	"mycalendar/parser"
)

const (
	hdrSep   = "═══"
	sep      = "────────────────────────────────────────────────"
	okMark   = "[✓]"
	errMark  = "[✗]"
	warnMark = "[!]"
	askMark  = "[?]"

	listFmt = "  %2d. %-8s %-18s %-20s %-8s [%s]"
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
	models.DateCheckOff, models.DateCheckAsk, models.DateCheckFix, models.DateCheckReask,
}

// App runs the interactive console menu.
type App struct {
	svc     *calendar.Service
	cfg     *models.Config
	cfgPath string
	ctx     context.Context

	in          *bufio.Reader
	stdinClosed bool

	todayCache time.Time
}

// NewApp creates an App. ctx is used for persistence so a cancelled context
// (SIGINT) aborts in-flight disk writes.
func NewApp(ctx context.Context, svc *calendar.Service, cfg *models.Config, cfgPath string) *App {
	return &App{
		svc:     svc,
		cfg:     cfg,
		cfgPath: cfgPath,
		ctx:     ctx,
		in:      bufio.NewReader(os.Stdin),
	}
}

// Start runs an interactive session: report load warnings, validate the clock,
// run the integrity check, then enter the main menu.
func (a *App) Start() {
	a.reportWarnings()
	a.checkClock()
	a.checkIntegrity()
	a.mainMenu()
}

// reportWarnings surfaces recoverable problems noticed while loading data.
func (a *App) reportWarnings() {
	for _, w := range a.svc.Warnings() {
		fmt.Println(color.Yellow(warnMark + " " + w))
	}
}

// today returns the effective current date (system, or the pinned custom date
// parsed once and cached).
func (a *App) today() time.Time {
	if a.cfg.UseSystemDate || a.cfg.CustomDate == "" {
		return time.Now()
	}
	if a.todayCache.IsZero() {
		d, err := time.Parse("2006-01-02", a.cfg.CustomDate)
		if err != nil {
			return time.Now()
		}
		a.todayCache = d
	}
	return a.todayCache
}

func (a *App) forgetToday() { a.todayCache = time.Time{} }

// checkClock prompts for the date only when the system clock is obviously wrong.
func (a *App) checkClock() {
	y := time.Now().Year()
	if y >= 2000 && y <= 2100 {
		return
	}
	fmt.Println()
	fmt.Println(color.Yellow(warnMark + " Системные дата/время выглядят неверно (" + parser.FormatDate(time.Now()) + ")."))
	fmt.Println(color.Yellow("  Укажите сегодняшнюю дату — она сохранится в настройках."))
	date, ok := a.askDate("Сегодняшняя дата:", "DD-MM-YYYY, DD.MM.YYYY, YYYY-MM-DD")
	if !ok {
		return
	}
	a.cfg.CustomDate = date.Format("2006-01-02")
	a.cfg.UseSystemDate = false
	a.forgetToday()
	if err := config.Save(a.ctx, a.cfgPath, a.cfg); err != nil {
		fmt.Println(color.Red(errMark + " Не удалось сохранить дату: " + err.Error()))
	} else {
		fmt.Println(color.Green(okMark + " Дата сохранена"))
	}
}

// save persists the calendar and prints a clear error on failure.
func (a *App) save() error {
	if err := a.svc.Save(a.ctx); err != nil {
		fmt.Println(color.Red(errMark + " Не удалось сохранить данные: " + err.Error()))
		return err
	}
	return nil
}

// saveConfig persists the configuration. Every setting the user changes outside
// the settings menu (a remembered date-check choice, a new custom status or
// frequent name) goes through here, so a choice the program promised to
// remember actually survives the session.
func (a *App) saveConfig() {
	if err := config.Save(a.ctx, a.cfgPath, a.cfg); err != nil {
		fmt.Println(color.Red(errMark + " Не удалось сохранить настройки: " + err.Error()))
	}
}

// hint prints an explanatory line when ShowHints is enabled.
func (a *App) hint(text string) {
	if a.cfg.ShowHints {
		fmt.Println(color.Cyan("     → " + text))
	}
}

// mainMenuItems is the single source of truth for the top-level menu: the keys
// each entry accepts (digit, Latin hotkey, Russian mnemonic), its label, and
// what it does. run returns false to leave the menu.
var mainMenuItems = []struct {
	keys  []string
	label string
	hint  string
	run   func(*App) bool
}{
	{[]string{"1", "a", "д"}, " a | 1. Добавить запись", "Создание события (напр. 17.10 19:00, тренировка)", func(a *App) bool { a.addEntry(); return true }},
	{[]string{"2", "v", "п"}, " v | 2. Посмотреть записи", "Список за неделю, месяц, период (напр. 15-20) или все", func(a *App) bool { a.viewMenu(); return true }},
	{[]string{"3", "d", "у"}, " d | 3. Удалить запись", "Удаление по номеру/ID, за дату, диапазон или всех", func(a *App) bool { a.deleteMenu(); return true }},
	{[]string{"4", "t", "с"}, " t | 4. Сегодня", "Быстрый просмотр расписания на текущий день", func(a *App) bool { a.todayView(); return true }},
	{[]string{"5", "x", "э"}, " x | 5. Экспорт в CSV", "Выгрузка записей в таблицу для Excel / Calc (.csv)", func(a *App) bool { a.exportMenu(); return true }},
	{[]string{"6", "s", "н"}, " s | 6. Настройки", "Параметры хранения, автоисправление дат, подсказки", func(a *App) bool { a.settingsMenu(); return true }},
	{[]string{"7", "h", "?", "м"}, " h | 7. Справка", "Руководство пользователя, горячие клавиши и параметры CLI", func(a *App) bool { PrintHelp(); return true }},
	{[]string{"8", "q", "в"}, " q | 8. Выход", "Завершение работы программы (также работает Ctrl+C)", func(a *App) bool { return false }},
}

func mainMenuKeys() [][]string {
	keys := make([][]string, len(mainMenuItems))
	for i, it := range mainMenuItems {
		keys[i] = it.keys
	}
	return keys
}

// mainMenu is the top-level loop.
func (a *App) mainMenu() {
	keys := mainMenuKeys()
	for {
		fmt.Println()
		fmt.Println(color.Yellow(hdrSep + " Мои записи " + hdrSep))
		for _, it := range mainMenuItems {
			fmt.Println(it.label)
			if a.cfg.ShowHints {
				fmt.Println(color.Cyan("     → " + it.hint))
			}
		}

		choice, ok := a.menuChoice()
		if !ok {
			fmt.Println()
			return
		}
		idx := resolve(choice, keys)
		if idx < 0 {
			fmt.Println(color.Red(errMark + " Некорректный выбор (1–8)"))
			continue
		}
		if !mainMenuItems[idx].run(a) {
			fmt.Println()
			return
		}
	}
}

func isYes(s string) bool {
	switch strings.TrimSpace(strings.ToLower(s)) {
	case "да", "д", "yes", "y":
		return true
	}
	return false
}

func isCancel(s string) bool {
	t := strings.TrimSpace(strings.ToLower(s))
	return t == "0" || t == "отмена"
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// explainFS turns a filesystem error into a short Russian phrase.
func explainFS(err error) string {
	switch {
	case errors.Is(err, fs.ErrPermission):
		return "нет прав доступа"
	case errors.Is(err, fs.ErrNotExist):
		return "путь не найден"
	default:
		return err.Error()
	}
}
