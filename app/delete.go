package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"mycalendar/calendar"
	"mycalendar/color"
	"mycalendar/models"
	"mycalendar/parser"
)

func (a *App) deleteMenu() {
	fmt.Println()
	fmt.Println(color.Yellow(hdrSep + " Удаление записи " + hdrSep))
	fmt.Println(" i | 1. По ID / дате и времени")
	fmt.Println(" d | 2. Все записи за день")
	fmt.Println(" p | 3. За период")
	fmt.Println(" a | 4. Удалить все записи")
	fmt.Println(" q | 0. Назад")

	choice, ok := a.menuChoice()
	if !ok {
		return
	}
	switch {
	case match(choice, "1", "i"):
		a.deleteBySearch()
	case match(choice, "2", "d"):
		a.deleteByDay()
	case match(choice, "3", "p"):
		a.deleteByPeriod()
	case match(choice, "4", "a"):
		a.deleteAll()
	case match(choice, "0", "q"):
		return
	default:
		fmt.Println(color.Red(errMark + " Некорректный выбор (0–4)"))
	}
}

func (a *App) deleteBySearch() {
	in := a.dialog("Дата и время или ID записи:",
		"Например: 29-12-2025 10:51 или 20251229105142; 0 — отмена")
	if isCancel(in) || a.stdinClosed {
		return
	}
	sessions := a.lookup(in)
	if len(sessions) == 0 {
		return
	}
	if s, ok := a.pickSession(sessions); ok {
		a.deleteByID(s.ID)
	}
}

func (a *App) deleteByDay() {
	date, ok := a.askDate("Дата:", "DD-MM-YYYY, DD.MM.YYYY, YYYY-MM-DD; 0 — отмена")
	if !ok {
		return
	}
	d := calendar.DayStart(date)
	a.deleteSpan(a.svc.InRange(d, d), d, d, parser.FormatDate(date))
}

func (a *App) deleteByPeriod() {
	start, end, ok := a.askPeriod("Введите период для удаления:",
		"Например: 2025, 12.2025, 10-12, 01.12-15.12; 0 — отмена")
	if !ok {
		return
	}
	start, end = calendar.DayStart(start), calendar.DayStart(end)
	label := parser.FormatDate(start) + " — " + parser.FormatDate(end)
	a.deleteSpan(a.svc.InRange(start, end), start, end, label)
}

func (a *App) deleteSpan(sessions []models.Session, start, end time.Time, label string) {
	if len(sessions) == 0 {
		fmt.Println(color.Yellow(warnMark + " Нет записей за " + label))
		return
	}
	if seriesHit(sessions) {
		fmt.Println(color.Yellow(warnMark + " В диапазон попадают повторяющиеся записи — серия может стать неполной."))
	}
	fmt.Println(color.Yellow("\n" + warnMark + " Будет удалено записей: " + strconv.Itoa(len(sessions)) + " за " + label))
	if !a.confirm(color.Yellow(askMark + " Удалить всё за период?")) {
		fmt.Println(color.Yellow(warnMark + " Удаление отменено"))
		return
	}
	n := a.svc.DeleteRange(start, end)
	if err := a.save(); err != nil {
		return
	}
	fmt.Println(color.Green(okMark + " Удалено записей: " + strconv.Itoa(n)))
}

func (a *App) deleteAll() {
	total := a.svc.Count()
	if total == 0 {
		fmt.Println(color.Yellow(warnMark + " Записей нет"))
		return
	}
	fmt.Println(color.Yellow("\n" + warnMark + " Будет удалено ВСЕ записи: " + strconv.Itoa(total)))
	if !a.confirm(color.Yellow(askMark + " Вы уверены?")) {
		fmt.Println(color.Yellow(warnMark + " Удаление отменено"))
		return
	}
	a.svc.DeleteAll()
	if err := a.save(); err != nil {
		return
	}
	fmt.Println(color.Green(okMark + " Все записи удалены"))
}

// deleteByID removes one session; for a series member it offers to remove just
// that occurrence or the whole series.
func (a *App) deleteByID(id string) {
	s, ok := a.svc.ByID(id)
	if !ok {
		fmt.Println(color.Red(errMark + " Запись не найдена"))
		return
	}
	fmt.Printf("Запись: %s | %s | %s\n",
		color.Green(s.Name), color.Magenta(parser.FormatDate(mustISO(s.Date()))), s.Time)

	if s.IsRepeat() {
		n := len(a.svc.BySeries(s.SeriesID))
		fmt.Println(color.Yellow(warnMark + " Это повторяющаяся запись."))
		fmt.Println("  1. Удалить только эту")
		fmt.Printf("  2. Удалить всю серию (%d записей)\n", n)
		fmt.Println("  0. Отмена")
		switch a.prompt("") {
		case "1":
			a.svc.Delete(id)
			if a.save() == nil {
				fmt.Println(color.Green(okMark + " Запись удалена"))
			}
		case "2":
			removed := a.svc.DeleteSeries(s.SeriesID)
			if a.save() == nil {
				fmt.Println(color.Green(okMark + fmt.Sprintf(" Удалено записей серии: %d", removed)))
			}
		default:
			fmt.Println(color.Yellow(warnMark + " Удаление отменено"))
		}
		return
	}

	if !a.confirm(color.Yellow(askMark + " Удалить?")) {
		fmt.Println(color.Yellow(warnMark + " Удаление отменено"))
		return
	}
	a.svc.Delete(id)
	if a.save() == nil {
		fmt.Println(color.Green(okMark + " Запись удалена"))
	}
}

func seriesHit(sessions []models.Session) bool {
	for _, s := range sessions {
		if s.IsRepeat() {
			return true
		}
	}
	return false
}

// lookup resolves free-form "ID" or "date [time]" input to a list of sessions,
// printing a clear message when nothing matches.
func (a *App) lookup(in string) []models.Session {
	in = strings.TrimSpace(in)
	if in == "" {
		fmt.Println(color.Yellow(warnMark + " Пустой ввод"))
		return nil
	}

	if s, ok := a.svc.ByID(in); ok {
		return []models.Session{s}
	}

	datePart, timePart := in, ""
	if d, tm, err := parser.ParseDateTime(in); err == nil {
		datePart = d.Format("2006-01-02")
		timePart = tm.Format("15:04")
	}

	date, err := parser.ParseDate(datePart, time.Time{})
	if err != nil {
		fmt.Println(color.Red(errMark + " Не распознаны ни ID, ни дата: " + in))
		return nil
	}
	iso := date.Format("2006-01-02")

	if timePart == "" {
		on := a.svc.OnDate(iso)
		if len(on) == 0 {
			fmt.Println(color.Yellow(warnMark + " На " + color.Magenta(parser.FormatDate(date)) + " записей нет"))
		}
		return on
	}
	at := a.svc.Conflicts(iso, timePart)
	if len(at) == 0 {
		fmt.Println(color.Yellow(warnMark + " На " + color.Magenta(parser.FormatDate(date)) + " " + timePart + " записей нет"))
	}
	return at
}
