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
	case "export", "e", "x":
		a.exportByArgs(args)
	case "import", "i":
		a.importByArgs(args)
	case "undo", "u":
		a.undoCLI()
	case "today", "t":
		a.todayView()
	case "type", "types", "тип", "типы":
		if len(args) == 0 {
			a.viewByTypes()
		} else {
			typeFilter := strings.Join(args, " ")
			filtered := filterSessionsByTypes(a.svc.All(), []string{typeFilter})
			a.renderList(filtered, formatTypesLabel("Все записи", []string{typeFilter}))
		}
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
	typeFilter, remaining := extractTypeArg(args)
	if len(remaining) == 0 {
		if typeFilter != "" {
			filtered := filterSessionsByTypes(a.svc.All(), []string{typeFilter})
			a.renderList(filtered, formatTypesLabel("Все записи", []string{typeFilter}))
			return
		}
		a.viewMenu()
		return
	}
	if len(remaining) == 1 && (remaining[0] == "type" || remaining[0] == "types" || remaining[0] == "тип" || remaining[0] == "типы") {
		a.viewByTypes()
		return
	}
	arg := strings.Join(remaining, " ")
	start, end, err := parser.ParsePeriod(arg, a.today())
	if err != nil {
		fmt.Println(color.Red(errMark + " Неверный формат периода: " + arg))
		return
	}
	sessions := a.svc.InRange(start, end)
	label := parser.FormatDate(start) + " — " + parser.FormatDate(end)
	if typeFilter != "" {
		sessions = filterSessionsByTypes(sessions, []string{typeFilter})
		label = formatTypesLabel(label, []string{typeFilter})
	}
	a.renderList(sessions, label)
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
	start, end, err := parser.ParsePeriod(arg, a.today())
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
	typeFilter, remaining := extractTypeArg(args)
	if len(remaining) == 0 {
		if typeFilter != "" {
			filtered := filterSessionsByTypes(a.svc.All(), []string{typeFilter})
			a.exportCSV(filtered)
			return
		}
		a.exportMenu()
		return
	}
	if len(remaining) == 1 && (remaining[0] == "type" || remaining[0] == "types" || remaining[0] == "тип" || remaining[0] == "типы") {
		a.exportByTypes()
		return
	}
	arg := strings.Join(remaining, " ")
	start, end, err := parser.ParsePeriod(arg, a.today())
	if err != nil {
		fmt.Println(color.Red(errMark + " Неверный формат периода: " + arg))
		return
	}
	sessions := a.svc.InRange(start, end)
	if typeFilter != "" {
		sessions = filterSessionsByTypes(sessions, []string{typeFilter})
	}
	a.exportCSV(sessions)
}
