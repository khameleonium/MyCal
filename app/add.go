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
	typ, ok := a.askType("")
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
	status, ok := a.askStatus("")
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
		occurrences, truncated := repeatDates(date, until, mode)
		if truncated {
			fmt.Println(color.Yellow(fmt.Sprintf(
				"%s Достигнут предел в %d повторов — серия обрезана.", warnMark, maxOccurrences)))
		}
		dates = append(dates, occurrences...)
	}

	first := base
	first.ID = a.svc.GenerateID(date, tm)
	seriesID := ""
	if len(dates) > 1 {
		seriesID = first.ID
		first.SeriesID = seriesID
	}

	toAdd := []models.Session{first}
	for _, d := range dates[1:] {
		occ := base
		occ.ID = a.svc.GenerateID(d, tm)
		occ.SeriesID = seriesID
		toAdd = append(toAdd, occ)
	}

	added, err := a.svc.AddBatch(toAdd)
	if err != nil {
		if added == 0 {
			fmt.Println(color.Red(errMark + " Не удалось добавить запись: " + err.Error()))
			return false
		}
		fmt.Println(color.Red(errMark + " Не удалось добавить повтор: " + err.Error()))
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

// askRepeat asks whether to repeat and until when. An empty mode means "no
// repeat"; ok=false means the user backed out of adding altogether.
func (a *App) askRepeat(start time.Time) (string, time.Time, bool) {
	var mode string
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

// maxOccurrences caps one repeating series, so an accidental "every day until
// 2099" cannot fill the calendar.
const maxOccurrences = 5000

// repeatDates returns the occurrence dates strictly after start, up to and
// including until. Every date is computed from start rather than from the
// previous occurrence, so a series never drifts. truncated reports whether the
// cap cut the series short.
func repeatDates(start, until time.Time, mode string) (dates []time.Time, truncated bool) {
	for i := 1; ; i++ {
		var cur time.Time
		switch mode {
		case "d":
			cur = start.AddDate(0, 0, i)
		case "w":
			cur = start.AddDate(0, 0, 7*i)
		case "m":
			cur = addMonths(start, i)
		default:
			return nil, false
		}
		if cur.After(until) {
			return dates, false
		}
		dates = append(dates, cur)
		if len(dates) >= maxOccurrences {
			return dates, true
		}
	}
}

// addMonths adds n months to t, clamping the day to the end of the target
// month. Go's AddDate would roll 31 January into 3 March and shift every later
// occurrence with it; clamping keeps a monthly series on its own day-of-month
// and lands on the last day of shorter months.
func addMonths(t time.Time, n int) time.Time {
	first := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location()).AddDate(0, n, 0)
	day := t.Day()
	if last := daysInMonth(first.Year(), first.Month()); day > last {
		day = last
	}
	return time.Date(first.Year(), first.Month(), day, 0, 0, 0, 0, t.Location())
}

func daysInMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}
