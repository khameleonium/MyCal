// Package app implements the interactive console UI and the CLI subcommands.
package app

import (
	"bufio"
	"context"
	"fmt"
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
	cancelWord = "отмена"

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
	ctx     context.Context

	scanner     *bufio.Scanner
	stdinClosed bool

	nowCache time.Time // cached effective "today", see resolveDate
}

// NewApp creates a new App instance. ctx is used for persistence calls so a
// cancelled context (e.g. SIGINT) aborts in-flight disk writes.
func NewApp(ctx context.Context, svc *calendar.Service, cfg *models.Config, cfgPath string) *App {
	return &App{
		svc:     svc,
		cfg:     cfg,
		cfgPath: cfgPath,
		ctx:     ctx,
		scanner: bufio.NewScanner(os.Stdin),
	}
}

// Start runs the interactive session: validate the clock, run the Data Doctor,
// then enter the main menu loop.
func (a *App) Start() {
	a.initDate()
	a.CheckIntegrity()
	a.Run()
}

// resolveDate returns the current effective date. When the user has pinned a
// custom date it is parsed once and cached.
func (a *App) resolveDate() time.Time {
	if a.cfg.UseSystemDate || a.cfg.CustomDate == "" {
		return time.Now()
	}
	if a.nowCache.IsZero() {
		d, err := time.Parse("2006-01-02", a.cfg.CustomDate)
		if err != nil {
			return time.Now()
		}
		a.nowCache = d
	}
	return a.nowCache
}

// invalidateDateCache must be called whenever CustomDate/UseSystemDate change.
func (a *App) invalidateDateCache() { a.nowCache = time.Time{} }

// initDate validates the system clock at startup.
func (a *App) initDate() {
	now := time.Now()
	if now.Year() >= 2024 {
		return
	}
	fmt.Println()
	fmt.Println(color.Yellow(warnMark + " Системная дата некорректна (" + parser.FormatDate(now) + ")"))
	fmt.Println(color.Yellow("  Пожалуйста, введите текущую дату."))
	date, ok := a.dialogDate("Текущая дата:", "DD-MM-YYYY, DD.MM.YYYY, YYYY-MM-DD")
	if !ok {
		return
	}
	a.cfg.CustomDate = date.Format("2006-01-02")
	a.cfg.UseSystemDate = false
	a.invalidateDateCache()
	if err := config.Save(a.ctx, a.cfgPath, a.cfg); err != nil {
		fmt.Println(color.Red(errMark + " Ошибка сохранения даты: " + err.Error()))
	} else {
		fmt.Println(color.Green(okMark + " Дата сохранена в настройках"))
	}
}

// Run starts the main menu loop.
func (a *App) Run() {
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

		choice, ok := a.promptChoice("")
		if !ok {
			fmt.Println()
			return
		}

		// The main menu accepts digits, the Latin mnemonic, and the mnemonic
		// first letter of the Russian label.
		switch {
		case match(choice, "1", "a", "д"):
			a.addEntry()
		case match(choice, "2", "v", "п"):
			a.viewEntries()
		case match(choice, "3", "d", "у"):
			a.deleteEntry()
		case match(choice, "4", "t", "с"):
			a.todayView()
		case match(choice, "5", "x", "э"):
			a.exportCSV()
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

func isConfirmWord(s string) bool {
	switch strings.TrimSpace(strings.ToLower(s)) {
	case "да", "д", "yes", "y":
		return true
	}
	return false
}

func isCancelled(s string) bool {
	t := strings.TrimSpace(strings.ToLower(s))
	return t == "0" || t == cancelWord
}
