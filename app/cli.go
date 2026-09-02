package app

import (
	"fmt"
	"os"
	"strings"

	"mycalendar/calendar"
	"mycalendar/color"
	"mycalendar/parser"
)

// RunCLI dispatches a non-interactive subcommand. It returns false (and prints
// help) for an unknown command.
func (a *App) RunCLI(cmd string, args []string) bool {
	a.reportWarnings()
	switch strings.ToLower(cmd) {
	case "add", "a":
		a.addByArgs(args)
	case "view", "v":
		a.viewByArgs(args)
	case "delete", "d", "del":
		a.deleteByArgs(args)
	case "export", "e":
		a.exportByArgs(args)
	case "today", "t":
		a.todayView()
	case "week", "w":
		a.renderList(a.svc.Week(a.today()), weekLabel(a.today()))
	case "month", "m":
		a.renderList(a.svc.Month(a.today()), monthLabel(a.today()))
	case "help", "-h", "--help", "/?":
		PrintHelp()
	default:
		fmt.Fprintf(os.Stderr, "Неизвестная команда: %s\n\n", cmd)
		PrintHelp()
		return false
	}
	return true
}

func (a *App) addByArgs(args []string) {
	if len(args) == 0 {
		a.addLoop(true, nil)
		return
	}
	date, err := parser.ParseDate(strings.Join(args, " "), a.today())
	if err != nil {
		fmt.Println(color.Yellow(warnMark + " Неверный формат даты, переход в интерактивный режим."))
		a.addLoop(true, nil)
		return
	}
	a.addLoop(true, &date)
}

func (a *App) viewByArgs(args []string) {
	if len(args) == 0 {
		a.viewMenu()
		return
	}
	arg := strings.Join(args, " ")
	start, end, err := parser.ParsePeriod(arg)
	if err != nil {
		fmt.Println(color.Red(errMark + " Неверный формат периода: " + arg))
		return
	}
	a.renderList(a.svc.InRange(start, end),
		parser.FormatDate(start)+" — "+parser.FormatDate(end))
}

func (a *App) deleteByArgs(args []string) {
	if len(args) == 0 {
		a.deleteMenu()
		return
	}
	arg := strings.Join(args, " ")
	if _, ok := a.svc.ByID(arg); ok {
		a.deleteByID(arg)
		return
	}
	start, end, err := parser.ParsePeriod(arg)
	if err != nil {
		fmt.Println(color.Red(errMark + " Не распознаны ни ID, ни период: " + arg))
		return
	}
	start, end = calendar.DayStart(start), calendar.DayStart(end)
	a.deleteSpan(a.svc.InRange(start, end), start, end,
		parser.FormatDate(start)+" — "+parser.FormatDate(end))
}

func (a *App) exportByArgs(args []string) {
	if len(args) == 0 {
		a.exportMenu()
		return
	}
	arg := strings.Join(args, " ")
	start, end, err := parser.ParsePeriod(arg)
	if err != nil {
		fmt.Println(color.Red(errMark + " Неверный формат периода: " + arg))
		return
	}
	a.exportCSV(a.svc.InRange(start, end))
}
