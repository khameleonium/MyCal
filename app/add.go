package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"mycalendar/calendar"
	"mycalendar/color"
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
	eff := a.effectiveConfig()

	var date time.Time
	if presetDate != nil {
		date = *presetDate
	} else if calendar.IsFieldVisible(eff.VisibleFields, calendar.FieldDate) {
		var ok bool
		date, ok = a.askDate("Дата записи:",
			"DD-MM-YYYY, DD.MM.YYYY, YYYY-MM-DD; Enter — сегодня, 0 — отмена")
		if !ok {
			return false
		}
	} else {
		date = a.today()
	}

	var tm time.Time
	if calendar.IsFieldVisible(eff.VisibleFields, calendar.FieldTime) {
		var ok bool
		tm, ok = a.askTime()
		if !ok {
			return false
		}
	}

	var name string
	if calendar.IsFieldVisible(eff.VisibleFields, calendar.FieldName) {
		name = a.askName("Наименование записи:", "")
		if name == "" {
			return false
		}
	} else {
		name = "(Запись " + parser.FormatDate(date) + ")"
	}

	iso := date.Format("2006-01-02")
	if calendar.IsFieldVisible(eff.VisibleFields, calendar.FieldTime) && !tm.IsZero() {
		hhmm := tm.Format("15:04")
		if !a.resolveConflicts(iso, hhmm) {
			return false
		}
	}

	// Optional questions: can be skipped individually with Enter,
	// or all at once by typing "8" at any prompt.
	skipRest := false

	typ := eff.DefaultType
	if typ == "" {
		if types := a.svc.Types(); len(types) > 0 {
			typ = types[0]
		}
	}
	if calendar.IsFieldVisible(eff.VisibleFields, calendar.FieldType) && !skipRest {
		t, ok, skip := a.askTypeAdd(typ)
		if !ok {
			return false
		}
		typ = t
		if skip {
			skipRest = true
		}
	} else if !calendar.IsFieldVisible(eff.VisibleFields, calendar.FieldType) {
		typ = ""
	}

	duration := eff.DefaultDuration
	if calendar.IsFieldVisible(eff.VisibleFields, calendar.FieldDuration) && !skipRest {
		d, ok, skip := a.askDurationAdd(duration)
		if !ok {
			return false
		}
		duration = d
		if skip {
			skipRest = true
		}
	} else if !calendar.IsFieldVisible(eff.VisibleFields, calendar.FieldDuration) {
		duration = 0
	}

	cash := 0.0
	if calendar.IsFieldVisible(eff.VisibleFields, calendar.FieldCash) && !skipRest {
		c, ok, skip := a.askCashAdd()
		if !ok {
			return false
		}
		cash = c
		if skip {
			skipRest = true
		}
	}

	notes := ""
	if calendar.IsFieldVisible(eff.VisibleFields, calendar.FieldNotes) && !skipRest {
		n, ok, skip := a.askNotesAdd()
		if !ok {
			return false
		}
		notes = n
		if skip {
			skipRest = true
		}
	}

	status := ""
	if calendar.IsFieldVisible(eff.VisibleFields, calendar.FieldStatus) && !skipRest {
		st, ok, skip := a.askStatusAdd()
		if !ok {
			return false
		}
		status = st
		if skip {
			skipRest = true
		}
	}

	var mode string
	var until time.Time
	if !skipRest {
		m, u, ok := a.askRepeat(date)
		if !ok {
			return false
		}
		mode = m
		until = u
	}

	res, err := a.svc.CreateEntries(a.ctx, a.cfg, calendar.EntryInput{
		Date:        date,
		Time:        tm,
		RawName:     name,
		Type:        typ,
		Duration:    duration,
		Cash:        cash,
		Status:      status,
		Notes:       notes,
		RepeatMode:  mode,
		RepeatUntil: until,
	})
	if res.Truncated {
		fmt.Println(color.Yellow(fmt.Sprintf(
			"%s Достигнут предел в %d повторов — серия обрезана.", warnMark, calendar.MaxOccurrences)))
	}
	if err != nil {
		if res.Added == 0 {
			fmt.Println(color.Red(errMark + " Не удалось добавить запись: " + err.Error()))
			return false
		}
		fmt.Println(color.Red(errMark + " Не удалось добавить повтор: " + err.Error()))
	}
	if res.ConfigMod {
		a.saveConfig()
	}
	if res.Added > 1 {
		if res.SeriesID != "" {
			fmt.Println(color.Green(okMark + fmt.Sprintf(" Добавлено записей: %d (серия %s)", res.Added, color.Yellow(res.SeriesID))))
		} else {
			fmt.Println(color.Green(okMark + fmt.Sprintf(" Добавлено записей: %d", res.Added)))
		}
	} else {
		fmt.Println(color.Green(okMark + " Запись добавлена  ID: " + color.Yellow(res.First.ID)))
	}
	return true
}

func (a *App) askTypeAdd(defaultType string) (string, bool, bool) {
	types := a.svc.Types(defaultType)
	fmt.Println()
	fmt.Println(color.Yellow("Тип записи:"))
	for i, t := range types {
		fmt.Printf("  %d. %s\n", i+1, t)
	}
	fmt.Printf("  %d. Ручной ввод\n", len(types)+1)
	fmt.Println("  Enter — по умолчанию: " + color.Green(defaultType))
	fmt.Println("  8 — пропустить остальные вопросы и добавить запись, 0 — отмена")
	if a.cfg.ShowHints {
		fmt.Println(color.Cyan("  → Категория события; Enter выберет значение по умолчанию, 8 сразу добавит запись"))
	}

	for {
		a.printPrompt("> ")
		input := a.line()
		if a.stdinClosed || isCancel(input) {
			return "", false, false
		}
		if input == "8" {
			return defaultType, true, true
		}
		if input == "" {
			return defaultType, true, false
		}
		idx, err := strconv.Atoi(input)
		if err != nil || idx < 1 || idx > len(types)+1 {
			fmt.Println(color.Yellow(warnMark + " Введите номер из списка или 8 для сохранения"))
			continue
		}
		if idx == len(types)+1 {
			custom := a.dialog("Название нового типа:", "0 — отмена")
			if isCancel(custom) {
				return "", false, false
			}
			if custom == "" {
				return defaultType, true, false
			}
			return custom, true, false
		}
		return types[idx-1], true, false
	}
}

func (a *App) askDurationAdd(def int) (int, bool, bool) {
	promptMsg := fmt.Sprintf("Продолжительность (мин) [%d]:", def)
	helpMsg := fmt.Sprintf("Enter — %d мин; 8 — пропустить остальные вопросы и добавить; 0 — отмена", def)
	if a.cfg.ShowHints {
		helpMsg += "\n" + color.Cyan("  → Длительность события в минутах (напр. 30, 45, 60)")
	}
	in := a.dialog(promptMsg, helpMsg)
	if isCancel(in) || a.stdinClosed {
		return 0, false, false
	}
	if in == "8" {
		return def, true, true
	}
	if in == "" {
		return def, true, false
	}
	d, err := strconv.Atoi(in)
	if err != nil || d <= 0 {
		fmt.Println(color.Yellow(warnMark + " Некорректно, используется " + strconv.Itoa(def)))
		return def, true, false
	}
	return d, true, false
}

func (a *App) askCashAdd() (float64, bool, bool) {
	promptMsg := "Кэш [0.0]:"
	helpMsg := "Enter — 0, число (напр. 1500, -300); 8 — пропустить остальные вопросы и добавить; 0 — отмена"
	if a.cfg.ShowHints {
		helpMsg += "\n" + color.Cyan("  → Сумма в рублях / валюте (напр. 1500, -500 или 0.0)")
	}
	val := a.dialog(promptMsg, helpMsg)
	switch {
	case isCancel(val) || a.stdinClosed:
		fmt.Println(color.Yellow(warnMark + " Добавление отменено"))
		return 0, false, false
	case val == "8":
		return 0, true, true
	case val == "" || val == "0" || val == "0.0" || val == "0,0":
		return 0, true, false
	default:
		c, err := calendar.ParseCash(val)
		if err != nil {
			fmt.Println(color.Red(errMark + " Некорректная сумма (введите число, напр. 1500 или -300)"))
			return a.askCashAdd()
		}
		return c, true, false
	}
}

func (a *App) askNotesAdd() (string, bool, bool) {
	helpMsg := "Enter — пропустить; 8 — пропустить остальные вопросы и добавить; 0 — отмена"
	if a.cfg.ShowHints {
		helpMsg += "\n" + color.Cyan("  → Дополнительный комментарий к записи (произвольный текст)")
	}
	notes := a.dialog("Комментарий:", helpMsg)
	if isCancel(notes) || a.stdinClosed {
		fmt.Println(color.Yellow(warnMark + " Добавление отменено"))
		return "", false, false
	}
	if notes == "8" {
		return "", true, true
	}
	return notes, true, false
}

func (a *App) askStatusAdd() (string, bool, bool) {
	statuses := a.svc.Statuses(a.cfg.CustomStatuses...)

	fmt.Println()
	fmt.Println(color.Yellow("Статус:"))
	fmt.Println("  0. ~пусто~")
	for i, st := range statuses {
		fmt.Printf("  %d. %s\n", i+1, st)
	}
	fmt.Printf("  %d. Ручной ввод\n", len(statuses)+1)
	fmt.Println("  Enter — пропустить (~пусто~)")
	fmt.Println("  8 — пропустить остальные вопросы и добавить, отмена — отмена")
	if a.cfg.ShowHints {
		fmt.Println(color.Cyan("  → Текущий статус (Активно, Пропущено, Отменено и др.)"))
	}

	for {
		a.printPrompt("> ")
		input := a.line()
		if a.stdinClosed || strings.EqualFold(strings.TrimSpace(input), "отмена") {
			return "", false, false
		}
		if input == "8" {
			return "", true, true
		}
		if input == "0" || input == "" {
			return "", true, false
		}
		idx, err := strconv.Atoi(input)
		if err != nil || idx < 1 || idx > len(statuses)+1 {
			fmt.Println(color.Yellow(warnMark + " Введите номер из списка или 8 для сохранения"))
			continue
		}
		if idx == len(statuses)+1 {
			custom := a.dialog("Название статуса:", "0 — отмена")
			if isCancel(custom) {
				return "", false, false
			}
			if custom == "" {
				return "", true, false
			}
			if a.confirm(color.Yellow(askMark + " Добавить в список?")) {
				a.cfg.CustomStatuses = append(a.cfg.CustomStatuses, custom)
				a.saveConfig()
			}
			return custom, true, false
		}
		return statuses[idx-1], true, false
	}
}

// resolveConflicts shows existing sessions at the same date/time and lets the
// user edit/delete one or proceed. Returns false if the user backed out.
func (a *App) resolveConflicts(iso, hhmm string) bool {
	eff := a.effectiveConfig()
	if !eff.AskOnConflict || hhmm == "" {
		return true
	}
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
		durStr := calendar.FormatDuration(c.Duration, eff.DurationUnit)
		extra := ""
		if c.Cash != 0 {
			extra += " Кэш: " + formatCashDetail(c.Cash)
		}
		if c.Status != "" {
			extra += " [" + c.Status + "]"
		}
		fmt.Printf(listFmt+"\n", i+1, t, color.Green(c.Name), c.Type,
			color.Orange(durStr), extra, color.Yellow(c.ID))
	}
	fmt.Println()
	fmt.Println("  1. Редактировать существующую")
	fmt.Println("  2. Удалить существующую")
	fmt.Println("  3. Всё равно добавить (Enter)")
	fmt.Println("  0. Вернуться в меню")
	if a.cfg.ShowHints {
		fmt.Println(color.Cyan("  → Enter или 3 добавит новую запись параллельно с существующими"))
	}

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
	case "3", "":
		return true
	default:
		return false
	}
}

// askRepeat asks whether to repeat and until when. An empty mode means "no
// repeat"; ok=false means the user backed out of adding altogether.
func (a *App) askRepeat(start time.Time) (string, time.Time, bool) {
	var mode string
	promptMsg := "\nПовторять запись? (d — каждый день, w — каждую неделю, m — каждый месяц, Enter или 8 — нет)"
	if a.cfg.ShowHints {
		promptMsg += "\n" + color.Cyan("  → Создание серии повторяющихся событий; Enter или 8 — одиночная запись")
	}
	raw := a.prompt(promptMsg)
	switch {
	case raw == "" || raw == "8":
		return "", time.Time{}, true
	case match(raw, "d", "д"):
		mode = "d"
	case match(raw, "w", "н"):
		mode = "w"
	case match(raw, "m", "м"):
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
