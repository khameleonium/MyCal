package app

import (
	"fmt"
	"os"
	"strings"

	"mycalendar/calendar"
	"mycalendar/color"
	"mycalendar/parser"
)

// RunCLI dispatches a non-interactive subcommand. Unknown commands print help
// and report failure.
func (a *App) RunCLI(cmd string, args []string) (ok bool) {
	switch strings.ToLower(cmd) {
	case "add", "a":
		a.addByArgs(args)
	case "view", "v":
		a.viewByArgs(args)
	case "delete", "d":
		a.deleteByArgs(args)
	case "export", "e":
		a.exportByArgs(args)
	case "today", "t":
		a.todayView()
	case "week", "w":
		a.renderEntries(a.svc.GetWeekEntries(a.resolveDate()), weekLabel(a.resolveDate()))
	case "month", "m":
		a.renderEntries(a.svc.GetMonthEntries(a.resolveDate()), monthLabel(a.resolveDate()))
	case "help", "-h", "--help":
		PrintHelp()
	default:
		fmt.Fprintf(os.Stderr, "Неизвестная команда: %s\n", cmd)
		PrintHelp()
		return false
	}
	return true
}

func (a *App) addByArgs(args []string) {
	if len(args) == 0 {
		a.addEntryLoop(true, nil)
		return
	}
	date, err := parser.ParseDate(args[0], a.resolveDate())
	if err != nil {
		fmt.Println(color.Yellow(warnMark + " Неверный формат даты. Переход в интерактивный режим."))
		a.addEntryLoop(true, nil)
		return
	}
	a.addEntryLoop(true, &date)
}

func (a *App) viewByArgs(args []string) {
	if len(args) == 0 {
		a.viewEntries()
		return
	}
	arg := strings.Join(args, " ")
	start, end, err := parser.ParsePeriod(arg)
	if err != nil {
		fmt.Println(color.Red(errMark + " Неверный формат периода: " + arg))
		return
	}
	a.renderEntries(a.svc.FindByPeriod(start, end),
		parser.FormatDate(start)+" — "+parser.FormatDate(end))
}

func (a *App) deleteByArgs(args []string) {
	if len(args) == 0 {
		a.deleteEntry()
		return
	}
	arg := strings.Join(args, " ")
	if len(a.svc.FindByID(arg)) > 0 {
		a.doDelete(arg)
		return
	}
	start, end, err := parser.ParsePeriod(arg)
	if err != nil {
		fmt.Println(color.Red(errMark + " Неверный формат периода или ID не найден: " + arg))
		return
	}
	start, end = calendar.DayStart(start), calendar.DayStart(end)
	a.deletePeriod(a.svc.FindByPeriod(start, end), start, end,
		parser.FormatDate(start)+" — "+parser.FormatDate(end))
}

func (a *App) exportByArgs(args []string) {
	if len(args) == 0 {
		a.exportCSV()
		return
	}
	arg := strings.Join(args, " ")
	start, end, err := parser.ParsePeriod(arg)
	if err != nil {
		fmt.Println(color.Red(errMark + " Неверный формат периода: " + arg))
		return
	}
	a.exportEntries(a.svc.FindByPeriod(start, end))
}
