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

// mainMenu is the top-level loop.
func (a *App) mainMenu() {
	for {
		fmt.Println()
		fmt.Println(color.Yellow(hdrSep + " Мои записи " + hdrSep))
		fmt.Println(" a | 1. Добавить запись")
		fmt.Println(" v | 2. Посмотреть записи")
		fmt.Println(" d | 3. Удалить запись")
		fmt.Println(" t | 4. Сегодня")
		fmt.Println(" x | 5. Экспорт в CSV")
		fmt.Println(" s | 6. Настройки")
		fmt.Println(" h | 7. Справка")
		fmt.Println(" q | 8. Выход")

		choice, ok := a.menuChoice()
		if !ok {
			fmt.Println()
			return
		}
		switch {
		case match(choice, "1", "a", "д"):
			a.addEntry()
		case match(choice, "2", "v", "п"):
			a.viewMenu()
		case match(choice, "3", "d", "у"):
			a.deleteMenu()
		case match(choice, "4", "t", "с"):
			a.todayView()
		case match(choice, "5", "x", "э"):
			a.exportMenu()
		case match(choice, "6", "s", "н"):
			a.settingsMenu()
		case match(choice, "7", "h", "?", "м"):
			PrintHelp()
		case match(choice, "8", "q", "в"):
			fmt.Println()
			return
		default:
			fmt.Println(color.Red(errMark + " Некорректный выбор (1–8)"))
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
