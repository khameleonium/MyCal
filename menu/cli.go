package menu

import (
	"fmt"
	"strings"
	"mycalendar/color"
	"mycalendar/parser"
)

// ViewByArgs handles 'mycal view [period]'
func (a *App) ViewByArgs(args []string) {
	if len(args) == 0 {
		a.viewEntries("")
		return
	}

	arg := strings.Join(args, " ")
	start, end, err := parser.ParsePeriod(arg)
	if err != nil {
		fmt.Println(color.Red(errMark + " Неверный формат периода: " + arg))
		return
	}
	
	entries := a.svc.FindByPeriod(start, end)
	if len(entries) == 0 {
		fmt.Println(color.Yellow(warnMark + " Записей не найдено за указанный период"))
		return
	}
	
	allSessions := a.printEntries(entries)
	hours := 0.0
	totalSessions := 0
	for _, de := range entries {
		totalSessions += len(de.Sessions)
		for _, s := range de.Sessions {
			hours += float64(s.Duration) / 60.0
		}
	}
	
	fmt.Printf("\nОбщее время: %s | Всего записей: %s\n", color.Orange(fmt.Sprintf("%.1f ч", hours)), color.Green(fmt.Sprintf("%d", totalSessions)))
	a.printStats(allSessions)
	a.detailLoop(allSessions, "Номер или ID для подробностей (Enter — назад) > ")
}

// DeleteByArgs handles 'mycal delete [period|id]'
func (a *App) DeleteByArgs(args []string) {
	if len(args) == 0 {
		a.deleteEntry()
		return
	}
	
	arg := strings.Join(args, " ")
	
	// First check if it's an ID
	sessions := a.svc.FindByID(arg)
	if len(sessions) > 0 {
		a.doDelete(arg)
		return
	}
	
	// If not ID, try as period
	start, end, err := parser.ParsePeriod(arg)
	if err == nil {
		entries := a.svc.FindByPeriod(start, end)
		label := parser.FormatDate(start) + " — " + parser.FormatDate(end)
		a.deletePeriod(entries, start, end, label)
		return
	}
	
	fmt.Println(color.Red(errMark + " Неверный формат периода или ID не найден: " + arg))
}

// ExportByArgs handles 'mycal export [period]'
func (a *App) ExportByArgs(args []string) {
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
	
	entries := a.svc.FindByPeriod(start, end)
	if len(entries) == 0 {
		fmt.Println(color.Yellow(warnMark + " Нет записей для экспорта"))
		return
	}
	
	a.exportEntries(entries)
}

// AddByArgs handles 'mycal add [date]'
func (a *App) AddByArgs(args []string) {
	if len(args) == 0 {
		a.AddEntryQuick()
		return
	}
	
	// For now, if arguments are passed, try to parse the first argument as date
	// and interactive for the rest.
	dateStr := args[0]
	parsedDate, err := parser.ParseDate(dateStr, a.resolveDate())
	if err != nil {
		fmt.Println(color.Yellow(warnMark + " Неверный формат даты. Переход в интерактивный режим."))
		a.AddEntryQuick()
		return
	}
	
	// Predetermine custom date for adding just this time
	oldMode := a.cfg.UseSystemDate
	oldCustom := a.cfg.CustomDate
	
	a.cfg.UseSystemDate = false
	a.cfg.CustomDate = parsedDate.Format("2006-01-02")
	
	a.AddEntryQuick()
	
	// Restore
	a.cfg.UseSystemDate = oldMode
	a.cfg.CustomDate = oldCustom
}
