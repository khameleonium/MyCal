package app

import (
	"fmt"
	"time"

	"mycalendar/color"
	"mycalendar/models"
	"mycalendar/parser"
)

func (a *App) addEntry() { a.addEntryLoop(false, nil) }

// addEntryLoop drives the "add entry" wizard. When quick is true it keeps
// prompting for another entry until the user backs out (used by the CLI `add`).
// presetDate, when non-nil, pre-fills the date question (used by `add <date>`).
func (a *App) addEntryLoop(quick bool, presetDate *time.Time) {
	first := true
	for {
		if !first {
			if !quick {
				return
			}
			fmt.Println()
			fmt.Println(color.Yellow("  Enter — ещё запись  |  0 — в меню"))
			if isCancelled(a.prompt("")) || a.stdinClosed {
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

// addOne runs one pass of the wizard. It returns true only when an entry was
// saved; any cancellation returns false and ends the (quick) loop.
func (a *App) addOne(presetDate *time.Time) bool {
	var date time.Time
	if presetDate != nil {
		date = *presetDate
	} else {
		var ok bool
		date, ok = a.dialogDate("Дата записи:",
			"DD-MM-YYYY, DD.MM.YYYY, YYYY-MM-DD; Enter — сегодня, 0 — отмена")
		if !ok {
			return false
		}
	}

	tm, ok := a.askTimeLooped()
	if !ok {
		return false
	}
	name := a.askName("Наименование записи:", "")
	if name == "" {
		return false
	}
	sessionType, ok := a.askType()
	if !ok {
		return false
	}
	duration, ok := a.dialogDuration(a.cfg.DefaultDuration)
	if !ok {
		return false
	}

	notes := a.dialogPrompt("Комментарий (Enter — пропустить, 0 — отмена):", "")
	if isCancelled(notes) || a.stdinClosed {
		fmt.Println(color.Yellow(warnMark + " Добавление отменено"))
		return false
	}
	status, ok := a.askStatus()
	if !ok {
		fmt.Println(color.Yellow(warnMark + " Добавление отменено"))
		return false
	}

	dateStr := date.Format("2006-01-02")
	timeStr := tm.Format("15:04")

	if !a.resolveConflicts(dateStr, timeStr) {
		return false
	}

	repeatMode, repeatUntil, ok := a.askRepeat(date)
	if !ok {
		return false
	}

	id := a.svc.GenerateID(date, tm)
	if repeatMode != "" {
		id += models.RepeatSuffix
	}
	if err := a.svc.AddEntry(models.Session{
		ID: id, Time: timeStr, Name: name, Type: sessionType,
		Duration: duration, Notes: notes, Status: status,
	}); err != nil {
		fmt.Println(color.Red(errMark + " Ошибка: " + err.Error()))
		return false
	}

	count := 1 + a.materializeRepeats(id, date, tm, timeStr, repeatMode, repeatUntil)

	if err := a.svc.Save(a.ctx); err != nil {
		fmt.Println(color.Red(errMark + " Ошибка сохранения: " + err.Error()))
		return false
	}
	if count > 1 {
		fmt.Println(color.Green(okMark + fmt.Sprintf(" Добавлено %d записей. Оригинал ID: ", count) + color.Yellow(id)))
	} else {
		fmt.Println(color.Green(okMark + " Запись добавлена  ID: " + color.Yellow(id)))
	}
	return true
}

// resolveConflicts shows any existing sessions at the same date/time and lets
// the user edit/delete one or proceed. Returns false if the user backed out.
func (a *App) resolveConflicts(dateStr, timeStr string) bool {
	conflicts := a.svc.FindConflicts(dateStr, timeStr)
	if len(conflicts) == 0 {
		return true
	}
	fmt.Println(color.Yellow("\n" + warnMark + " На эту дату и время уже есть записи:"))
	for i, c := range conflicts {
		t := c.Time
		if c.IsRepeat {
			t = "* " + t
		}
		fmt.Printf(listFmt+"\n", i+1, t, color.Green(c.Name), c.Type,
			color.Orange(fmt.Sprintf("%d мин", c.Duration)), color.Yellow(c.ID))
	}
	fmt.Println()
	fmt.Println("  1. Редактировать существующую")
	fmt.Println("  2. Удалить существующую")
	fmt.Println("  3. Всё равно добавить")
	fmt.Println("  4. Вернуться в меню")

	switch a.prompt("") {
	case "1":
		if id := a.askID(); id != "" {
			a.doInteractiveEdit(id)
		}
		return false
	case "2":
		if id := a.askID(); id != "" {
			a.doDelete(id)
		}
		return false
	case "3":
		return true
	default:
		return false
	}
}

// askRepeat asks whether to repeat the entry and until when.
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
		return "", time.Time{}, true
	}

	for {
		untilStr := a.prompt("До какой даты повторять? (DD.MM.YYYY, 0 — отмена)")
		if isCancelled(untilStr) || a.stdinClosed {
			return "", time.Time{}, true // treat as "no repeat"
		}
		untilDate, err := parser.ParseDate(untilStr, a.resolveDate())
		if err != nil {
			fmt.Println(color.Red(errMark + " Неверный формат даты."))
			continue
		}
		if untilDate.Before(start) {
			fmt.Println(color.Red(errMark + " Дата окончания не может быть раньше начальной."))
			continue
		}
		return mode, untilDate, true
	}
}

// materializeRepeats creates child occurrences that reference the original by
// ID (in their Name field); the calendar service hydrates their fields on read.
func (a *App) materializeRepeats(origID string, start, tm time.Time, timeStr, mode string, until time.Time) int {
	if mode == "" {
		return 0
	}
	step := func(t time.Time) time.Time {
		switch mode {
		case "d":
			return t.AddDate(0, 0, 1)
		case "w":
			return t.AddDate(0, 0, 7)
		default:
			return t.AddDate(0, 1, 0)
		}
	}
	count := 0
	for cur := step(start); !cur.After(until); cur = step(cur) {
		child := models.Session{
			ID:   a.svc.GenerateID(cur, tm),
			Time: timeStr,
			Name: origID,
		}
		if err := a.svc.AddEntry(child); err != nil {
			fmt.Println(color.Red(errMark + " Ошибка при создании повтора: " + err.Error()))
			break
		}
		count++
	}
	return count
}
