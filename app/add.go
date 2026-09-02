package app

import (
	"fmt"
	"time"

	"mycalendar/color"
	"mycalendar/models"
	"mycalendar/parser"
)

func (a *App) addEntry() { a.addLoop(false, nil) }

// addLoop drives the "add entry" wizard. When quick is true it keeps offering
// another entry until the user backs out (CLI `add`). presetDate, when non-nil,
// pre-fills the date question (CLI `add <date>`).
func (a *App) addLoop(quick bool, presetDate *time.Time) {
	first := true
	for {
		if !first {
			if !quick {
				return
			}
			fmt.Println()
			fmt.Println(color.Yellow("  Enter — ещё запись  |  0 — в меню"))
			if isCancel(a.prompt("")) || a.stdinClosed {
				return
			}
		}
		if first && !quick {
			fmt.Println()
			fmt.Println(color.Yellow(hdrSep + " Добавление записи " + hdrSep))
		}
		first = false

		if !a.addOne(presetDate) {
			return
		}
	}
}

// addOne runs one pass of the wizard. Returns true only when an entry was saved.
func (a *App) addOne(presetDate *time.Time) bool {
	var date time.Time
	if presetDate != nil {
		date = *presetDate
	} else {
		var ok bool
		date, ok = a.askDate("Дата записи:",
			"DD-MM-YYYY, DD.MM.YYYY, YYYY-MM-DD; Enter — сегодня, 0 — отмена")
		if !ok {
			return false
		}
	}

	tm, ok := a.askTime()
	if !ok {
		return false
	}
	name := a.askName("Наименование записи:", "")
	if name == "" {
		return false
	}
	typ, ok := a.askType()
	if !ok {
		return false
	}
	duration, ok := a.askDuration(a.cfg.DefaultDuration)
	if !ok {
		return false
	}
	notes := a.dialog("Комментарий (Enter — пропустить, 0 — отмена):", "")
	if isCancel(notes) || a.stdinClosed {
		fmt.Println(color.Yellow(warnMark + " Добавление отменено"))
		return false
	}
	status, ok := a.askStatus()
	if !ok {
		fmt.Println(color.Yellow(warnMark + " Добавление отменено"))
		return false
	}

	iso := date.Format("2006-01-02")
	hhmm := tm.Format("15:04")
	if !a.resolveConflicts(iso, hhmm) {
		return false
	}

	mode, until, ok := a.askRepeat(date)
	if !ok {
		return false
	}

	base := models.Session{
		Time: hhmm, Name: name, Type: typ,
		Duration: duration, Notes: notes, Status: status,
	}

	dates := []time.Time{date}
	if mode != "" {
		dates = append(dates, repeatDates(date, until, mode)...)
	}

	first := base
	first.ID = a.svc.GenerateID(date, tm)
	seriesID := ""
	if len(dates) > 1 {
		seriesID = first.ID
		first.SeriesID = seriesID
	}
	if err := a.svc.Add(first); err != nil {
		fmt.Println(color.Red(errMark + " Не удалось добавить запись: " + err.Error()))
		return false
	}

	added := 1
	for _, d := range dates[1:] {
		occ := base
		occ.ID = a.svc.GenerateID(d, tm)
		occ.SeriesID = seriesID
		if err := a.svc.Add(occ); err != nil {
			fmt.Println(color.Red(errMark + " Не удалось добавить повтор: " + err.Error()))
			break
		}
		added++
	}

	if err := a.save(); err != nil {
		return false
	}
	if added > 1 {
		fmt.Println(color.Green(okMark + fmt.Sprintf(" Добавлено записей: %d (серия %s)", added, color.Yellow(seriesID))))
	} else {
		fmt.Println(color.Green(okMark + " Запись добавлена  ID: " + color.Yellow(first.ID)))
	}
	return true
}

// resolveConflicts shows existing sessions at the same date/time and lets the
// user edit/delete one or proceed. Returns false if the user backed out.
func (a *App) resolveConflicts(iso, hhmm string) bool {
	conflicts := a.svc.Conflicts(iso, hhmm)
	if len(conflicts) == 0 {
		return true
	}
	fmt.Println(color.Yellow("\n" + warnMark + " На эту дату и время уже есть записи:"))
	for i, c := range conflicts {
		t := c.Time
		if c.IsRepeat() {
			t = "* " + t
		}
		fmt.Printf(listFmt+"\n", i+1, t, color.Green(c.Name), c.Type,
			color.Orange(fmt.Sprintf("%d мин", c.Duration)), color.Yellow(c.ID))
	}
	fmt.Println()
	fmt.Println("  1. Редактировать существующую")
	fmt.Println("  2. Удалить существующую")
	fmt.Println("  3. Всё равно добавить")
	fmt.Println("  0. Вернуться в меню")

	switch a.prompt("") {
	case "1":
		if s, ok := a.pickSession(conflicts); ok {
			a.editSession(s.ID)
		}
		return false
	case "2":
		if s, ok := a.pickSession(conflicts); ok {
			a.deleteByID(s.ID)
		}
		return false
	case "3":
		return true
	default:
		return false
	}
}

// askRepeat asks whether to repeat and until when.
func (a *App) askRepeat(start time.Time) (mode string, until time.Time, ok bool) {
	raw := a.prompt("\nПовторять запись? (d — каждый день, w — каждую неделю, m — каждый месяц, Enter — нет)")
	switch {
	case raw == "":
		return "", time.Time{}, true
	case match(raw, "d"):
		mode = "d"
	case match(raw, "w"):
		mode = "w"
	case match(raw, "m"):
		mode = "m"
	default:
		fmt.Println(color.Yellow(warnMark + " Не понял режим повтора, запись добавлена без повторов"))
		return "", time.Time{}, true
	}

	for {
		s := a.prompt("До какой даты повторять включительно? (DD.MM.YYYY, 0 — без повторов)")
		if isCancel(s) || a.stdinClosed {
			return "", time.Time{}, true
		}
		until, err := parser.ParseDate(s, a.today())
		if err != nil {
			fmt.Println(color.Red(errMark + " Не удалось распознать дату"))
			continue
		}
		if until.Before(start) {
			fmt.Println(color.Red(errMark + " Дата окончания раньше начальной"))
			continue
		}
		return mode, until, true
	}
}

// repeatDates returns the occurrence dates strictly after start, up to and
// including until.
func repeatDates(start, until time.Time, mode string) []time.Time {
	step := map[string]func(time.Time) time.Time{
		"d": func(t time.Time) time.Time { return t.AddDate(0, 0, 1) },
		"w": func(t time.Time) time.Time { return t.AddDate(0, 0, 7) },
		"m": func(t time.Time) time.Time { return t.AddDate(0, 1, 0) },
	}[mode]

	var out []time.Time
	for cur := step(start); !cur.After(until); cur = step(cur) {
		out = append(out, cur)
		if len(out) > 5000 { // hard safety cap
			break
		}
	}
	return out
}
