package app

import (
	"fmt"
	"time"

	"mycalendar/calendar"
	"mycalendar/color"
	"mycalendar/models"
	"mycalendar/parser"
)

func (a *App) viewEntries() {
	fmt.Println()
	fmt.Println(color.Yellow(hdrSep + " Просмотр записей " + hdrSep))
	fmt.Println(" w | 1. Записи за эту неделю")
	fmt.Println(" m | 2. Записи за этот месяц")
	fmt.Println(" p | 3. Указать период вручную")
	fmt.Println(" a | 4. Все записи")
	fmt.Println(" q | 0. Назад")

	choice, ok := a.promptChoice("")
	if !ok {
		return
	}

	var entries []models.DateEntry
	var label string

	switch {
	case match(choice, "1", "w"):
		entries = a.svc.GetWeekEntries(a.resolveDate())
		label = weekLabel(a.resolveDate())
	case match(choice, "2", "m"):
		entries = a.svc.GetMonthEntries(a.resolveDate())
		label = monthLabel(a.resolveDate())
	case match(choice, "3", "p"):
		start, end, ok := a.dialogPeriod("Введите период:",
			"Например: 2025, 12.2025, 10-12, 01.12-15.12; 0 — отмена")
		if !ok {
			return
		}
		entries = a.svc.FindByPeriod(start, end)
		label = parser.FormatDate(start) + " — " + parser.FormatDate(end)
	case match(choice, "4", "a"):
		entries = a.svc.GetAllEntries()
		label = "Все записи"
	case match(choice, "0", "q"):
		return
	default:
		fmt.Println(color.Red(errMark + " Некорректный выбор"))
		return
	}

	a.renderEntries(entries, label)
}

func (a *App) todayView() {
	ref := a.resolveDate()
	weekdayNames := []string{"вс", "пн", "вт", "ср", "чт", "пт", "сб"}
	label := "Сегодня (" + weekdayNames[ref.Weekday()] + ", " + parser.FormatDate(ref) + ")"
	a.renderEntries(a.svc.GetTodayEntries(ref), label)
}

func weekLabel(ref time.Time) string {
	monday, sunday := calendar.WeekBounds(ref)
	return parser.FormatDate(monday) + " — " + parser.FormatDate(sunday)
}

func monthLabel(ref time.Time) string {
	first, last := calendar.MonthBounds(ref)
	return parser.FormatDate(first) + " — " + parser.FormatDate(last)
}
