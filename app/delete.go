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

func (a *App) deleteEntry() {
	fmt.Println()
	fmt.Println(color.Yellow(hdrSep + " Удаление записи " + hdrSep))
	fmt.Println(" i | 1. По ID / дате и времени")
	fmt.Println(" d | 2. Все записи за день")
	fmt.Println(" p | 3. За период")
	fmt.Println(" a | 4. Удалить все записи")
	fmt.Println(" q | 0. Назад")

	choice, ok := a.promptChoice("")
	if !ok {
		return
	}
	switch {
	case match(choice, "1", "i"):
		a.deleteBySearch()
	case match(choice, "2", "d"):
		a.deleteByDate()
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
	input := a.dialogPrompt("Дата и время или ID записи:",
		"Например: 29-12-2025 10:51 или 20251229105142; 0 — отмена")
	if isCancelled(input) || a.stdinClosed {
		return
	}
	sessions := a.searchSessions(input)
	if len(sessions) == 0 {
		return
	}
	if target := a.chooseOne(sessions); target != nil {
		a.doDelete(target.ID)
	}
}

func (a *App) deleteByDate() {
	date, ok := a.dialogDate("Дата:", "DD-MM-YYYY, DD.MM.YYYY, YYYY-MM-DD; 0 — отмена")
	if !ok {
		return
	}
	start := calendar.DayStart(date)
	a.deletePeriod(a.svc.FindByPeriod(start, start), start, start, parser.FormatDate(date))
}

func (a *App) deleteByPeriod() {
	start, end, ok := a.dialogPeriod("Введите период для удаления:",
		"Например: 2025, 12.2025, 10-12, 01.12-15.12; 0 — отмена")
	if !ok {
		return
	}
	start, end = calendar.DayStart(start), calendar.DayStart(end)
	label := parser.FormatDate(start) + " — " + parser.FormatDate(end)
	a.deletePeriod(a.svc.FindByPeriod(start, end), start, end, label)
}

func (a *App) deletePeriod(entries []models.DateEntry, start, end time.Time, label string) {
	total := countSessions(entries)
	if total == 0 {
		fmt.Println(color.Yellow(warnMark + " Нет записей за " + label))
		return
	}
	if seriesHit := repeatsInvolved(entries); seriesHit {
		fmt.Println(color.Yellow(warnMark + " В диапазон попадают повторяющиеся записи — связанная серия может стать неполной."))
	}
	fmt.Println(color.Yellow("\n" + warnMark + " Будет удалено " + strconv.Itoa(total) + " записей за " + label))
	if !a.confirm(color.Yellow(askMark + " Удалить ВСЕ записи за период?")) {
		fmt.Println(color.Yellow(warnMark + " Удаление отменено"))
		return
	}
	count := a.svc.DeleteByPeriod(start, end)
	if err := a.svc.Save(a.ctx); err != nil {
		fmt.Println(color.Red(errMark + " Ошибка сохранения: " + err.Error()))
		return
	}
	fmt.Println(color.Green(okMark + " Удалено " + strconv.Itoa(count) + " записей"))
}

func (a *App) deleteAll() {
	entries := a.svc.GetAllEntries()
	total := countSessions(entries)
	if total == 0 {
		fmt.Println(color.Yellow(warnMark + " Нет записей для удаления"))
		return
	}
	fmt.Println(color.Yellow("\n" + warnMark + " Будет удалено " + strconv.Itoa(total) + " записей (ВСЕ записи)"))
	if !a.confirm(color.Yellow(askMark + " Вы уверены, что хотите удалить ВСЕ записи?")) {
		fmt.Println(color.Yellow(warnMark + " Удаление отменено"))
		return
	}
	a.svc.DeleteAll()
	if err := a.svc.Save(a.ctx); err != nil {
		fmt.Println(color.Red(errMark + " Ошибка сохранения: " + err.Error()))
		return
	}
	fmt.Println(color.Green(okMark + " Все записи успешно удалены"))
}

// doDelete removes every session with id. When id is a repeating original it
// cascades to all child occurrences.
func (a *App) doDelete(id string) {
	sessions := a.svc.FindByID(id)
	if len(sessions) == 0 {
		fmt.Println(color.Red(errMark + " Запись не найдена"))
		return
	}
	session := a.svc.HydrateSession(sessions[0])
	isOriginalRepeat := models.HasRepeatSuffix(id)

	fmt.Printf("Запись: %s | %s | %s\n",
		color.Green(session.Name), color.Magenta(session.Date()), session.Time)
	var question string
	switch {
	case isOriginalRepeat:
		question = color.Yellow(warnMark + " Это оригинал повторяющейся записи. Удалить её вместе со всеми повторениями?")
	case len(sessions) > 1:
		question = color.Yellow(fmt.Sprintf(warnMark+" Будет удалено %d записей с этим ID. Продолжить?", len(sessions)))
	default:
		question = color.Yellow(askMark + " Удалить?")
	}
	if !a.confirm(question) {
		fmt.Println(color.Yellow(warnMark + " Удаление отменено"))
		return
	}

	count := a.svc.DeleteEntry(id)
	if isOriginalRepeat {
		count += a.svc.DeleteRepeats(id)
	}
	if err := a.svc.Save(a.ctx); err != nil {
		fmt.Println(color.Red(errMark + " Ошибка сохранения: " + err.Error()))
		return
	}
	switch {
	case isOriginalRepeat:
		fmt.Println(color.Green(okMark + fmt.Sprintf(" Удалено %d записей (включая все повторения)", count)))
	case count > 1:
		fmt.Println(color.Green(okMark + fmt.Sprintf(" Успешно удалено %d записей", count)))
	case count == 1:
		fmt.Println(color.Green(okMark + " Запись успешно удалена"))
	default:
		fmt.Println(color.Yellow(warnMark + " Запись не найдена"))
	}
}

func repeatsInvolved(entries []models.DateEntry) bool {
	for _, de := range entries {
		for _, s := range de.Sessions {
			if s.IsRepeat || s.SeriesRef() != "" || models.HasRepeatSuffix(s.ID) {
				return true
			}
		}
	}
	return false
}

// searchSessions resolves free-form "ID" or "date [time]" input to a session list.
func (a *App) searchSessions(input string) []models.Session {
	input = strings.TrimSpace(input)
	if input == "" {
		fmt.Println(color.Yellow(warnMark + " Пустой ввод"))
		return nil
	}

	if isDigits(strings.TrimSuffix(input, models.RepeatSuffix)) &&
		(len(input) == models.IDLen || models.HasRepeatSuffix(input)) {
		if s := a.svc.FindByID(input); len(s) > 0 {
			return s
		}
		fmt.Println(color.Yellow(warnMark + " Запись с таким ID не найдена"))
		return nil
	}

	datePart, timePart := input, ""
	if d, tm, err := parser.ParseDateTime(input); err == nil {
		datePart = d.Format("2006-01-02")
		timePart = tm.Format("15:04")
	}

	date, err := parser.ParseDate(datePart, time.Time{})
	if err != nil {
		fmt.Println(color.Red(errMark + " Некорректная дата"))
		return nil
	}
	dateStr := date.Format("2006-01-02")

	if timePart == "" {
		de := a.svc.FindByDate(dateStr)
		if de == nil || len(de.Sessions) == 0 {
			fmt.Println(color.Yellow(warnMark + " Записей на " + color.Magenta(parser.FormatDate(date)) + " не найдено"))
			return nil
		}
		return a.svc.SessionsOn(dateStr)
	}

	sessions := a.svc.FindByDateTime(dateStr, timePart)
	if len(sessions) == 0 {
		fmt.Println(color.Yellow(warnMark + " Записей на " +
			color.Magenta(parser.FormatDate(date)) + " " + timePart + " не найдено"))
	}
	return sessions
}
