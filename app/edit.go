package app

import (
	"fmt"
	"strconv"

	"mycalendar/calendar"
	"mycalendar/color"
	"mycalendar/models"
	"mycalendar/parser"
)

// editSession edits the session with the given ID. For a member of a repeating
// series it first asks whether to change just that occurrence or the whole
// series (the same change is then applied to every occurrence).
func (a *App) editSession(id string) {
	session, ok := a.svc.ByID(id)
	if !ok {
		fmt.Println(color.Red(errMark + " Запись не найдена"))
		return
	}

	wholeSeries := false
	if session.IsRepeat() {
		fmt.Println()
		fmt.Println(color.Yellow(warnMark + " Это повторяющаяся запись."))
		fmt.Println("  1. Изменить только эту запись")
		fmt.Println("  2. Изменить всю серию")
		fmt.Println("  0. Отмена")
		switch a.prompt("") {
		case "1":
		case "2":
			wholeSeries = true
		default:
			fmt.Println(color.Yellow(warnMark + " Редактирование отменено"))
			return
		}
	}

	a.showSessionDetail(session)
	fmt.Println()
	fmt.Println("Что редактируем?")
	fmt.Println("1. Наименование   4. Продолжительность   7. Полностью")
	fmt.Println("2. Тип            5. Комментарий          0. Отмена")
	fmt.Println("3. Время          6. Статус")

	choice := a.prompt("")
	if choice == "0" || isCancel(choice) {
		fmt.Println(color.Yellow(warnMark + " Редактирование отменено"))
		return
	}

	var patch calendar.Patch
	cancelled := false
	set := func(ok bool) bool {
		if !ok {
			cancelled = true
		}
		return ok
	}

	switch choice {
	case "1":
		if name := a.askName("Имя ["+color.Green(session.Name)+"]:", session.Name); name == "" {
			cancelled = true
		} else if name != session.Name {
			patch.Name = &name
		}
	case "2":
		if t, ok := a.askType(); set(ok) {
			patch.Type = &t
		}
	case "3":
		if t, ok := a.askTimeChange(session.Time); set(ok) && t != "" {
			patch.Time = &t
		}
	case "4":
		if d, ok, changed := a.askDurationChange(session.Duration); set(ok) && changed {
			patch.Duration = &d
		}
	case "5":
		v := a.dialog("Комментарий ["+session.Notes+"]:", "Enter — без изменений, 0 — отмена")
		if isCancel(v) {
			cancelled = true
		} else if v != "" {
			patch.Notes = &v
		}
	case "6":
		if st, ok := a.askStatus(); set(ok) {
			patch.Status = &st
		}
	case "7":
		a.fullEdit(session, &patch, &cancelled)
	default:
		fmt.Println(color.Red(errMark + " Некорректный выбор (0–7)"))
		return
	}

	if cancelled {
		fmt.Println(color.Yellow(warnMark + " Редактирование отменено"))
		return
	}
	if patch.Empty() {
		fmt.Println(color.Yellow(warnMark + " Изменений нет"))
		return
	}

	if wholeSeries {
		n := a.svc.UpdateSeries(session.SeriesID, patch)
		if err := a.save(); err != nil {
			return
		}
		fmt.Println(color.Green(okMark + fmt.Sprintf(" Обновлено записей в серии: %d", n)))
		return
	}
	if err := a.svc.Update(id, patch); err != nil {
		fmt.Println(color.Red(errMark + " " + err.Error()))
		return
	}
	if err := a.save(); err != nil {
		return
	}
	fmt.Println(color.Green(okMark + " Запись обновлена"))
}

func (a *App) fullEdit(s models.Session, patch *calendar.Patch, cancelled *bool) {
	fmt.Println("Введите новые значения (Enter — без изменений, 0 — отмена)")

	t, ok := a.askTimeChange(s.Time)
	if !ok {
		*cancelled = true
		return
	}
	if t != "" {
		patch.Time = &t
	}

	if name := a.askName("Имя ["+color.Green(s.Name)+"]:", s.Name); name == "" {
		*cancelled = true
		return
	} else if name != s.Name {
		patch.Name = &name
	}

	if v := a.dialog("Тип ["+s.Type+"]:", "Enter — без изменений, 0 — отмена"); isCancel(v) {
		*cancelled = true
		return
	} else if v != "" {
		patch.Type = &v
	}

	if d, okd, changed := a.askDurationChange(s.Duration); !okd {
		*cancelled = true
		return
	} else if changed {
		patch.Duration = &d
	}

	if v := a.dialog("Комментарий ["+s.Notes+"]:", "Enter — без изменений, 0 — отмена"); isCancel(v) {
		*cancelled = true
		return
	} else if v != "" {
		patch.Notes = &v
	}

	if v := a.dialog("Статус ["+s.Status+"]:", "Enter — без изменений, 0 — отмена"); isCancel(v) {
		*cancelled = true
		return
	} else if v != "" {
		patch.Status = &v
	}
}

// askTimeChange returns normalised HH:MM ("" = unchanged); ok=false to abort.
func (a *App) askTimeChange(current string) (string, bool) {
	v := a.dialog("Время ["+current+"]:", "Enter — без изменений, 0 — отмена")
	if isCancel(v) {
		return "", false
	}
	if v == "" {
		return "", true
	}
	tm, err := parser.ParseTime(v)
	if err != nil {
		fmt.Println(color.Red(errMark + " " + err.Error()))
		return "", false
	}
	return tm.Format("15:04"), true
}

// askDurationChange returns (value, ok, changed).
func (a *App) askDurationChange(current int) (int, bool, bool) {
	v := a.dialog("Продолжительность ["+color.Orange(strconv.Itoa(current))+"]:",
		"Enter — без изменений, 0 — отмена")
	if isCancel(v) {
		return 0, false, false
	}
	if v == "" {
		return 0, true, false
	}
	d, err := strconv.Atoi(v)
	if err != nil || d <= 0 {
		fmt.Println(color.Red(errMark + " Некорректная продолжительность"))
		return 0, false, false
	}
	return d, true, true
}
