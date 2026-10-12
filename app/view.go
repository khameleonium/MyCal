package app

import (
	"fmt"
	"time"

	"mycalendar/calendar"
	"mycalendar/color"
	"mycalendar/models"
	"mycalendar/parser"
)

func (a *App) viewMenu() {
	fmt.Println()
	fmt.Println(color.Yellow(hdrSep + " Просмотр записей " + hdrSep))
	fmt.Println(" w | 1. За эту неделю")
	a.hint("События с понедельника по воскресенье текущей недели")
	fmt.Println(" m | 2. За этот месяц")
	a.hint("Все события текущего календарного месяца")
	fmt.Println(" p | 3. Указать период")
	a.hint("Выборка дат (примеры: \"15-20\", \"2026-10\", \"01.10-15.10\")")
	fmt.Println(" a | 4. Все записи")
	a.hint("Полный список всех сохранённых записей")
	fmt.Println(" q | 0. Назад")
	a.hint("Возврат в главное меню")

	choice, ok := a.menuChoice()
	if !ok {
		return
	}

	var sessions []models.Session
	var label string
	switch {
	case match(choice, "1", "w"):
		sessions, label = a.svc.Week(a.today()), weekLabel(a.today())
	case match(choice, "2", "m"):
		sessions, label = a.svc.Month(a.today()), monthLabel(a.today())
	case match(choice, "3", "p"):
		start, end, ok := a.askPeriod("Введите период:",
			"Например: 2025, 12.2025, 10-12, 01.12-15.12; 0 — отмена")
		if !ok {
			return
		}
		sessions = a.svc.InRange(start, end)
		label = parser.FormatDate(start) + " — " + parser.FormatDate(end)
	case match(choice, "4", "a"):
		sessions, label = a.svc.All(), "Все записи"
	case match(choice, "0", "q"):
		return
	default:
		fmt.Println(color.Red(errMark + " Некорректный выбор"))
		return
	}
	a.renderList(sessions, label)
}

func (a *App) todayView() {
	ref := a.today()
	names := []string{"вс", "пн", "вт", "ср", "чт", "пт", "сб"}
	label := "Сегодня (" + names[ref.Weekday()] + ", " + parser.FormatDate(ref) + ")"
	a.renderList(a.svc.Day(ref), label)
}

func weekLabel(ref time.Time) string {
	mon, sun := calendar.WeekBounds(ref)
	return parser.FormatDate(mon) + " — " + parser.FormatDate(sun)
}

func monthLabel(ref time.Time) string {
	first, last := calendar.MonthBounds(ref)
	return parser.FormatDate(first) + " — " + parser.FormatDate(last)
}
