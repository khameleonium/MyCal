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

// editSession edits the session with the given ID. For a member of a repeating
// series it first asks whether to change just that occurrence or the whole
// series (the same change is then applied to every occurrence).
func (a *App) editSession(id string) {
	session, ok := a.svc.ByID(id)
	if !ok {
		fmt.Println(color.Red(errMark + " Запись не найдена"))
		return
	}

	seriesScope := calendar.SeriesScopeSingle
	if session.IsRepeat() {
		total, followingCount := a.svc.SeriesCounts(session.SeriesID, session.ID)

		fmt.Println()
		fmt.Println(color.Yellow(warnMark + " Это повторяющаяся запись."))
		fmt.Println("  1. Изменить только эту запись")
		fmt.Printf("  2. Изменить эту и все последующие (%d записей)\n", followingCount)
		fmt.Printf("  3. Изменить всю серию целиком (%d записей)\n", total)
		fmt.Println("  0. Отмена")
		switch a.prompt("") {
		case "1":
			seriesScope = calendar.SeriesScopeSingle
		case "2":
			seriesScope = calendar.SeriesScopeFollowing
		case "3":
			seriesScope = calendar.SeriesScopeAll
		default:
			fmt.Println(color.Yellow(warnMark + " Редактирование отменено"))
			return
		}
	}

	a.showSessionDetail(session)
	fmt.Println()
	fmt.Println("Что редактируем?")
	fmt.Println("1. Наименование   4. Продолжительность   7. Кэш         10. Дублировать")
	fmt.Println("2. Тип            5. Комментарий          8. Полностью   11. В др. календарь")
	fmt.Println("3. Время          6. Статус              9. Дата         0. Отмена")
	if a.cfg.ShowHints {
		fmt.Println(color.Cyan("  → Номер поля (1–11) для действия, Enter без ввода — оставить, '-' очистить, 0 отмена"))
	}

	choice := a.prompt("")
	if choice == "0" || isCancel(choice) {
		fmt.Println(color.Yellow(warnMark + " Редактирование отменено"))
		return
	}

	var patch calendar.Patch
	var newDate *time.Time
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
		if t, ok := a.askType(session.Type); set(ok) && t != session.Type {
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
		if v, ok, changed := a.askNotesChange(session.Notes); set(ok) && changed {
			patch.Notes = &v
		}
	case "6":
		if st, ok := a.askStatus(session.Status); set(ok) && st != session.Status {
			patch.Status = &st
		}
	case "7", "к", "k", "c", "cash", "кэш":
		if c, ok, changed := a.askCashChange(session.Cash); set(ok) && changed {
			patch.Cash = &c
		}
	case "8", "full", "все", "всё", "all", "f", "п":
		a.fullEdit(session, &patch, &cancelled)
	case "9", "d", "д", "date", "дата":
		if d, ok, changed := a.askDateChange(session.Date()); set(ok) && changed {
			newDate = &d
		}
	case "10", "dup", "clone", "дубль":
		a.duplicateSessionCLI(session)
		return
	case "11", "move", "m", "м":
		a.moveSessionCLI(session)
		return
	default:
		fmt.Println(color.Red(errMark + " Некорректный выбор (0–11)"))
		return
	}

	if cancelled {
		fmt.Println(color.Yellow(warnMark + " Редактирование отменено"))
		return
	}
	if patch.Empty() && newDate == nil {
		fmt.Println(color.Yellow(warnMark + " Изменений нет"))
		return
	}

	_, n, err := a.svc.UpdateEntry(a.ctx, a.cfg, id, calendar.UpdateInput{
		NewDate:      newDate,
		Patch:        patch,
		SeriesScope:  seriesScope,
		UpdateSeries: seriesScope == calendar.SeriesScopeAll,
	})
	if err != nil {
		fmt.Println(color.Red(errMark + " " + err.Error()))
		return
	}
	a.saveConfig()
	if seriesScope == calendar.SeriesScopeFollowing {
		fmt.Println(color.Green(okMark + fmt.Sprintf(" Обновлено записей серии (начиная с текущей): %d", n)))
		return
	}
	if seriesScope == calendar.SeriesScopeAll {
		fmt.Println(color.Green(okMark + fmt.Sprintf(" Обновлено записей в серии: %d", n)))
		return
	}
	fmt.Println(color.Green(okMark + " Запись обновлена"))
}

func (a *App) askDateChange(currentISO string) (time.Time, bool, bool) {
	curFmt := parser.FormatDate(mustISO(currentISO))
	v := a.dialog("Дата ["+color.Magenta(curFmt)+"]:", "Enter — без изменений, 0 — отмена")
	if isCancel(v) || a.stdinClosed {
		return time.Time{}, false, false
	}
	if v == "" {
		return time.Time{}, true, false
	}
	d, err := parser.ParseDate(v, a.today())
	if err != nil {
		fmt.Println(color.Red(errMark + " " + err.Error()))
		return time.Time{}, false, false
	}
	return d, true, d.Format("2006-01-02") != currentISO
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

	if t, ok := a.askType(s.Type); !ok {
		*cancelled = true
		return
	} else if t != s.Type {
		patch.Type = &t
	}

	if d, okd, changed := a.askDurationChange(s.Duration); !okd {
		*cancelled = true
		return
	} else if changed {
		patch.Duration = &d
	}

	if c, okc, changed := a.askCashChange(s.Cash); !okc {
		*cancelled = true
		return
	} else if changed {
		patch.Cash = &c
	}

	if st, ok := a.askStatus(s.Status); !ok {
		*cancelled = true
		return
	} else if st != s.Status {
		patch.Status = &st
	}

	if v, ok, changed := a.askNotesChange(s.Notes); !ok {
		*cancelled = true
		return
	} else if changed {
		patch.Notes = &v
	}
}

// askCashChange returns (value, ok, changed).
func (a *App) askCashChange(current float64) (float64, bool, bool) {
	curStr := calendar.FormatCash(current)
	v := a.dialog("Кэш ["+color.Yellow(curStr)+"]:",
		"Enter — без изменений, '-' — сбросить в 0, 0 — отмена")
	switch {
	case isCancel(v) || a.stdinClosed:
		return 0, false, false
	case v == "":
		return current, true, false
	case v == "-" || v == "0.0" || v == "0,0":
		return 0, true, current != 0
	default:
		c, err := calendar.ParseCash(v)
		if err != nil {
			fmt.Println(color.Red(errMark + " Некорректная сумма (введите число, напр. 1500 или -300)"))
			return a.askCashChange(current)
		}
		return c, true, c != current
	}
}

// askNotesChange returns (value, ok, changed). Enter keeps the current note and
// "-" clears it — without an explicit clear token an existing comment could
// never be removed, since an empty reply means "unchanged" and "0" cancels.
func (a *App) askNotesChange(current string) (string, bool, bool) {
	v := a.dialog("Комментарий ["+dash(current)+"]:",
		"Enter — без изменений, '-' — очистить, 0 — отмена")
	switch {
	case isCancel(v):
		return "", false, false
	case v == "":
		return "", true, false
	case v == "-":
		return "", true, current != ""
	default:
		return v, true, v != current
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

func (a *App) duplicateSessionCLI(s models.Session) {
	curDate := mustISO(s.Date())
	curFmt := parser.FormatDate(curDate)
	fmt.Println()
	fmt.Println(color.Yellow(hdrSep + " Дублирование записи " + hdrSep))
	dateVal := a.dialog("Дата для копии ["+color.Magenta(curFmt)+"]:", "Enter — та же дата, или укажите новую дату; 0 — отмена")
	if isCancel(dateVal) || a.stdinClosed {
		fmt.Println(color.Yellow(warnMark + " Дублирование отменено"))
		return
	}
	targetDate := curDate
	if dateVal != "" {
		d, err := parser.ParseDate(dateVal, a.today())
		if err != nil {
			fmt.Println(color.Red(errMark + " " + err.Error()))
			return
		}
		targetDate = d
	}

	timeVal := a.dialog("Время для копии ["+color.Green(s.Time)+"]:", "Enter — то же время, или укажите новое (ЧЧ:ММ); 0 — отмена")
	if isCancel(timeVal) || a.stdinClosed {
		fmt.Println(color.Yellow(warnMark + " Дублирование отменено"))
		return
	}
	targetTime, _ := time.Parse("15:04", s.Time)
	if timeVal != "" {
		tm, err := parser.ParseTime(timeVal)
		if err != nil {
			fmt.Println(color.Red(errMark + " " + err.Error()))
			return
		}
		targetTime = tm
	}

	res, err := a.svc.CreateEntries(a.ctx, a.cfg, calendar.EntryInput{
		Date:     targetDate,
		Time:     targetTime,
		RawName:  s.Name,
		Type:     s.Type,
		Duration: s.Duration,
		Cash:     s.Cash,
		Status:   s.Status,
		Notes:    s.Notes,
	})
	if err != nil {
		fmt.Println(color.Red(errMark + " Не удалось дублировать запись: " + err.Error()))
		return
	}
	if res.ConfigMod {
		a.saveConfig()
	}
	fmt.Println(color.Green(okMark + " Запись продублирована  ID: " + color.Yellow(res.First.ID)))
}

func (a *App) moveSessionCLI(s models.Session) {
	cals := a.svc.ListCalendars(a.cfg)
	var other []models.CalendarInfo
	for _, c := range cals {
		if !c.Active {
			other = append(other, c)
		}
	}
	fmt.Println()
	fmt.Println(color.Yellow(hdrSep + " Перенос записи в другой календарь " + hdrSep))
	for i, c := range other {
		fmt.Printf("  %d. %s\n", i+1, c.Label)
	}
	fmt.Printf("  %d. Указать префикс календаря вручную\n", len(other)+1)
	fmt.Println("  0. Отмена")

	choice := a.prompt("")
	if choice == "" || choice == "0" || isCancel(choice) {
		return
	}
	targetKey := ""
	idx, err := strconv.Atoi(choice)
	if err == nil && idx >= 1 && idx <= len(other) {
		targetKey = other[idx-1].Key
	} else if err == nil && idx == len(other)+1 {
		prefix := a.dialog("Префикс целевого календаря (Enter — default):", "Напр. work, sport; 0 — отмена")
		if isCancel(prefix) || a.stdinClosed {
			return
		}
		prefix = strings.TrimSpace(prefix)
		if prefix == "" || strings.EqualFold(prefix, "default") || strings.EqualFold(prefix, "mycal") {
			targetKey = ""
		} else {
			base := a.cfg.DataFileName
			if base == "" {
				base = "mycal"
			}
			targetKey = strings.TrimPrefix(prefix, base+"--")
		}
	} else {
		fmt.Println(color.Red(errMark + " Некорректный выбор"))
		return
	}

	moved, err := a.svc.MoveEntriesToCalendar(a.ctx, a.cfg, []string{s.ID}, targetKey)
	if err != nil {
		fmt.Println(color.Red(errMark + " " + err.Error()))
		return
	}
	a.saveConfig()
	targetDisp := targetKey
	if targetDisp == "" {
		base := a.cfg.DataFileName
		if base == "" {
			base = "mycal"
		}
		targetDisp = base + " (default)"
	}
	fmt.Println(color.Green(fmt.Sprintf("%s Перенесено записей: %d → %s", okMark, moved, targetDisp)))
}

func (a *App) undoCLI() {
	desc, err := a.svc.Undo(a.ctx)
	if err != nil {
		fmt.Println(color.Yellow(warnMark + " " + err.Error()))
		return
	}
	fmt.Println(color.Green(okMark + " Отменено действие: " + desc))
}
