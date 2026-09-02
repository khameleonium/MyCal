package app

import (
	"fmt"
	"strconv"

	"mycalendar/color"
	"mycalendar/models"
	"mycalendar/parser"
)

// doInteractiveEdit edits the session with the given ID. For a dependent
// occurrence of a repeating series it first asks whether to change just that
// occurrence or the whole series.
func (a *App) doInteractiveEdit(id string) {
	found := a.svc.FindByID(id)
	if len(found) == 0 {
		fmt.Println(color.Red(errMark + " Запись не найдена"))
		return
	}
	session := a.svc.HydrateSession(found[0])

	editWholeSeries := false
	if session.IsRepeat && session.OriginalID != "" && session.OriginalID != id {
		fmt.Println()
		fmt.Println(color.Yellow(warnMark + " Это зависимая повторяющаяся запись."))
		fmt.Println("  1. Изменить только эту запись")
		fmt.Println("  2. Изменить всю серию (оригинал)")
		fmt.Println("  0. Отмена")
		switch a.prompt("") {
		case "1":
		case "2":
			id = session.OriginalID
			editWholeSeries = true
			if o := a.svc.FindByID(id); len(o) > 0 {
				session = a.svc.HydrateSession(o[0])
			}
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
	if choice == "0" || isCancelled(choice) {
		fmt.Println(color.Yellow(warnMark + " Редактирование отменено"))
		return
	}

	var updated models.Session
	var newSeriesTime string
	abort := func() { fmt.Println(color.Yellow(warnMark + " Редактирование отменено")) }

	switch choice {
	case "1":
		name := a.askName("Имя ["+color.Green(session.Name)+"]:", session.Name)
		if name == "" {
			abort()
			return
		}
		updated.Name = name
	case "2":
		t, ok := a.askType()
		if !ok {
			abort()
			return
		}
		updated.Type = t
	case "3":
		t, ok := a.editTime(session.Time)
		if !ok {
			abort()
			return
		}
		if editWholeSeries {
			newSeriesTime = t
		} else {
			updated.Time = t
		}
	case "4":
		d, ok := a.editDuration(session.Duration)
		if !ok {
			abort()
			return
		}
		updated.Duration = d
	case "5":
		v := a.dialogPrompt("Комментарий ["+session.Notes+"]:", "Enter — без изменений, 0 — отмена")
		if isCancelled(v) {
			abort()
			return
		}
		updated.Notes = v
	case "6":
		st, ok := a.askStatus()
		if !ok {
			abort()
			return
		}
		updated.Status = st
	case "7":
		fmt.Println("Введите новые значения (Enter — без изменений, 0 — отмена)")
		var ok bool
		if updated.Time, ok = a.editTime(session.Time); !ok {
			abort()
			return
		}
		if editWholeSeries {
			newSeriesTime, updated.Time = updated.Time, ""
		}
		if name := a.askName("Имя ["+color.Green(session.Name)+"]:", session.Name); name == "" {
			abort()
			return
		} else if name != session.Name {
			updated.Name = name
		}
		newType := a.dialogPrompt("Тип ["+session.Type+"]:", "Enter — без изменений, 0 — отмена")
		if isCancelled(newType) {
			abort()
			return
		}
		updated.Type = newType
		if updated.Duration, ok = a.editDurationOptional(session.Duration); !ok {
			abort()
			return
		}
		newNotes := a.dialogPrompt("Комментарий ["+session.Notes+"]:", "Enter — без изменений, 0 — отмена")
		if isCancelled(newNotes) {
			abort()
			return
		}
		updated.Notes = newNotes
		newStatus := a.dialogPrompt("Статус ["+session.Status+"]:", "Enter — без изменений, 0 — отмена")
		if isCancelled(newStatus) {
			abort()
			return
		}
		updated.Status = newStatus
	default:
		fmt.Println(color.Red(errMark + " Некорректный выбор (0–7)"))
		return
	}

	if newSeriesTime != "" {
		a.svc.EditSeriesTime(id, newSeriesTime)
	}
	if hasEdits(updated) {
		if err := a.svc.EditEntry(id, updated); err != nil {
			fmt.Println(color.Red(errMark + " Ошибка: " + err.Error()))
			return
		}
	}
	if err := a.svc.Save(a.ctx); err != nil {
		fmt.Println(color.Red(errMark + " Ошибка сохранения: " + err.Error()))
		return
	}
	fmt.Println(color.Green(okMark + " Запись обновлена"))
}

func hasEdits(s models.Session) bool {
	return s.Time != "" || s.Name != "" || s.Type != "" || s.Duration != 0 || s.Notes != "" || s.Status != ""
}

// editTime returns the normalised HH:MM, "" for "no change", ok=false to abort.
func (a *App) editTime(current string) (string, bool) {
	v := a.dialogPrompt("Время ["+current+"]:", "Enter — без изменений, 0 — отмена")
	if isCancelled(v) {
		return "", false
	}
	if v == "" {
		return "", true
	}
	tm, err := parser.ParseTime(v)
	if err != nil {
		fmt.Println(color.Red(errMark + " Некорректное время"))
		return "", false
	}
	return tm.Format("15:04"), true
}

func (a *App) editDuration(current int) (int, bool) {
	d, ok := a.editDurationOptional(current)
	if ok && d == 0 {
		return 0, true // unchanged
	}
	return d, ok
}

func (a *App) editDurationOptional(current int) (int, bool) {
	v := a.dialogPrompt("Продолжительность ["+color.Orange(strconv.Itoa(current))+"]:",
		"Enter — без изменений, 0 — отмена")
	if isCancelled(v) {
		return 0, false
	}
	if v == "" {
		return 0, true
	}
	d, err := strconv.Atoi(v)
	if err != nil || d <= 0 {
		fmt.Println(color.Red(errMark + " Некорректная продолжительность"))
		return 0, false
	}
	return d, true
}
