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
	fmt.Println(" t | 5. По типам (фильтр)")
	a.hint("Выборка по одному или нескольким типам занятий")
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
	case match(choice, "5", "t", "т"):
		a.viewByTypes()
		return
	case match(choice, "0", "q"):
		return
	default:
		fmt.Println(color.Red(errMark + " Некорректный выбор"))
		return
	}
	a.renderList(sessions, label)
}

func (a *App) viewByTypes() {
	allTypes := a.calendarTypes(true)
	if len(allTypes) == 0 {
		fmt.Println(color.Yellow(warnMark + " В календаре пока нет типов записей."))
		return
	}
	selected, ok := a.pickTypesMulti(allTypes, "Выбор типов записей")
	if !ok || len(selected) == 0 {
		return
	}

	fmt.Println()
	fmt.Println(color.Yellow(hdrSep + " Период выборки " + hdrSep))
	fmt.Println(" w | 1. За эту неделю")
	fmt.Println(" m | 2. За этот месяц")
	fmt.Println(" p | 3. Указать период")
	fmt.Println(" a | 4. Все записи")
	fmt.Println(" q | 0. Отмена")
	fmt.Println(" Enter / 4 — Все записи")

	choice, ok := a.menuChoice()
	if !ok || match(choice, "0", "q") {
		return
	}

	var sessions []models.Session
	var baseLabel string
	switch {
	case match(choice, "1", "w"):
		sessions, baseLabel = a.svc.Week(a.today()), weekLabel(a.today())
	case match(choice, "2", "m"):
		sessions, baseLabel = a.svc.Month(a.today()), monthLabel(a.today())
	case match(choice, "3", "p"):
		start, end, ok := a.askPeriod("Введите период:",
			"Например: 2025, 12.2025, 10-12, 01.12-15.12; 0 — отмена")
		if !ok {
			return
		}
		sessions = a.svc.InRange(start, end)
		baseLabel = parser.FormatDate(start) + " — " + parser.FormatDate(end)
	default:
		sessions, baseLabel = a.svc.All(), "Все записи"
	}

	filtered := filterSessionsByTypes(sessions, selected)
	label := formatTypesLabel(baseLabel, selected)
	a.renderList(filtered, label)
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
