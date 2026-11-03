package app

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"mycalendar/calendar"
	"mycalendar/color"
	"mycalendar/models"
	"mycalendar/parser"
)

// renderList prints a titled, date-grouped list of sessions plus the summary
// and per-type statistics, then runs the interactive detail loop. Every view
// (week/month/period/all/today/CLI) goes through here so the layout and the
// "\edit" / "/filter" affordances stay identical.
func (a *App) renderList(sessions []models.Session, label string) {
	if len(sessions) == 0 {
		fmt.Println()
		fmt.Println(color.Magenta(hdrSep + " " + label + " " + hdrSep))
		fmt.Println(color.Yellow(warnMark + " Записей нет"))
		return
	}
	ordered := append([]models.Session(nil), sessions...)
	sortForDisplay(ordered)

	a.displayList(ordered, label)
	a.detailLoop(ordered, label)
}

// displayList prints the header, date-grouped sessions, summary, and stats.
func (a *App) displayList(ordered []models.Session, label string) {
	fmt.Println()
	fmt.Println(color.Magenta(hdrSep + " " + label + " " + hdrSep))
	a.printSessions(ordered)
	a.printSummary(ordered)
	a.printStats(ordered)
}

// printSessions prints an already display-ordered list, grouped by date, with a
// running row number. The caller sorts once so the numbers printed here and the
// numbers detailLoop accepts cannot drift apart.
func (a *App) printSessions(ordered []models.Session) {
	lastDate := ""
	for i, s := range ordered {
		if s.Date() != lastDate {
			lastDate = s.Date()
			fmt.Println(color.Magenta(parser.FormatDate(mustISO(lastDate))))
		}
		t := s.Time
		if s.IsRepeat() {
			t = "* " + t
		}
		fmt.Printf(listFmt+"\n", i+1, t, color.Green(s.Name), s.Type,
			color.Orange(fmt.Sprintf("%d мин", s.Duration)), color.Yellow(s.ID))
	}
}

func (a *App) printSummary(sessions []models.Session) {
	mins := calendar.TotalMinutes(sessions)
	fmt.Printf("\nОбщее время: %s | Всего записей: %s\n",
		color.Orange(fmt.Sprintf("%.1f ч", float64(mins)/60.0)),
		color.Green(strconv.Itoa(len(sessions))))
}

func (a *App) printStats(sessions []models.Session) {
	if len(sessions) == 0 {
		return
	}
	totalMin := 0
	byType := map[string]int{}
	for _, s := range sessions {
		totalMin += s.Duration
		byType[s.Type] += s.Duration
	}

	fmt.Println()
	fmt.Println(sep)
	fmt.Printf(statFmt+"\n", "Всего записей:", color.Orange(strconv.Itoa(len(sessions))))
	fmt.Printf(statFmt+"\n", "Средняя продолж.:",
		color.Orange(fmt.Sprintf("%.1f мин", float64(totalMin)/float64(len(sessions)))))

	fmt.Println()
	fmt.Println(color.Yellow("  По типам записей:"))
	keys := make([]string, 0, len(byType))
	for t := range byType {
		keys = append(keys, t)
	}
	sort.Strings(keys)
	for _, t := range keys {
		name := t
		if name == "" {
			name = "(без типа)"
		}
		fmt.Printf(statFmt+"\n", "    "+name+":",
			color.Orange(fmt.Sprintf("%.1f ч (%d мин)", float64(byType[t])/60.0, byType[t])))
	}
	fmt.Println("  " + sep[:36])
	fmt.Printf(statFmt+"\n", "  Общее время:",
		color.Orange(fmt.Sprintf("%.1f ч (%d мин)", float64(totalMin)/60.0, totalMin)))
	fmt.Println(sep)
}

func (a *App) showSessionDetail(s models.Session) {
	fmt.Println()
	fmt.Println(sep)
	fmt.Printf("  %s%s\n", detailLabel("ИД:"), color.Yellow(s.ID))
	fmt.Printf("  %s%s\n", detailLabel("Дата:"), color.Magenta(parser.FormatDate(mustISO(s.Date()))))
	fmt.Printf("  %s%s\n", detailLabel("Время:"), s.Time)
	fmt.Printf("  %s%s\n", detailLabel("Имя:"), color.Green(s.Name))
	fmt.Printf("  %s%s\n", detailLabel("Тип:"), s.Type)
	fmt.Printf("  %s%s\n", detailLabel("Продолжительность:"), color.Orange(fmt.Sprintf("%d мин", s.Duration)))
	fmt.Printf("  %s%s\n", detailLabel("Комментарий:"), dash(s.Notes))
	fmt.Printf("  %s%s\n", detailLabel("Статус:"), dash(s.Status))
	if s.IsRepeat() {
		n := len(a.svc.BySeries(s.SeriesID))
		fmt.Printf("  %s%s\n", detailLabel("Серия:"), color.Magenta(fmt.Sprintf("повтор, записей в серии: %d", n)))
	}
	fmt.Println(sep)
}

// detailLabel pads a field name so every value in the card starts at the same
// column. Padding counts runes, not bytes: "%-19s" would measure the Cyrillic
// labels in UTF-8 bytes and align nothing.
func detailLabel(name string) string {
	const width = 19
	if n := utf8.RuneCountInString(name); n < width {
		return name + strings.Repeat(" ", width-n)
	}
	return name + " "
}

// detailLoop lets the user drill into a listed session by number or ID, edit
// one with a "\" prefix, filter the current list by type with "t" (or "t text"),
// search with "/text", or expand everything with "all". Returns on Enter or closed stdin.
func (a *App) detailLoop(ordered []models.Session, currentLabel ...string) {
	label := "Записи"
	if len(currentLabel) > 0 && currentLabel[0] != "" {
		label = currentLabel[0]
	}

	for {
		fmt.Println()
		if a.cfg.ShowHints {
			fmt.Println(color.Cyan("  → '1' подробности, 't' фильтр по типам, '\\1' правка, '/лук' поиск, 'all' все карточки"))
		}
		a.printPrompt("Номер/ID — подробности  |  t — по типам  |  \\N — правка  |  /текст — поиск  |  Enter — назад > ")
		in, ok := a.readLine()
		in = strings.TrimSpace(in)
		if !ok || in == "" {
			return
		}

		// Direct type filter command: "t", "т", "тип", "type"
		if match(in, "t", "т", "тип", "type") {
			if a.filterByTypesInteractive(ordered, label) {
				a.displayList(ordered, label)
			}
			continue
		}
		if after, ok := trimPrefixWord(in, "t", "т", "тип", "type"); ok {
			if a.filterByTypesDirect(ordered, label, after) {
				a.displayList(ordered, label)
			}
			continue
		}

		if rest, isFilter := strings.CutPrefix(in, "/"); isFilter {
			rest = strings.TrimSpace(rest)
			if rest == "" {
				continue
			}
			if match(rest, "t", "т", "тип", "type") {
				if a.filterByTypesInteractive(ordered, label) {
					a.displayList(ordered, label)
				}
				continue
			}
			if after, ok := trimPrefixWord(rest, "t", "т", "тип", "type"); ok {
				if a.filterByTypesDirect(ordered, label, after) {
					a.displayList(ordered, label)
				}
				continue
			}
			if a.filterAndShow(ordered, label, rest) {
				a.displayList(ordered, label)
			}
			continue
		}
		if match(in, "all", "все", "a") {
			for _, s := range ordered {
				a.showSessionDetail(s)
			}
			continue
		}

		target, edit := strings.TrimPrefix(in, "\\"), strings.HasPrefix(in, "\\")
		id := target
		if idx, err := strconv.Atoi(target); err == nil && idx >= 1 && idx <= len(ordered) {
			id = ordered[idx-1].ID
		}
		// Always re-read through the service: the listed slice is a snapshot
		// taken before any edit or delete made from inside this loop.
		s, ok := a.svc.ByID(id)
		if !ok {
			fmt.Println(color.Yellow(warnMark + " Запись не найдена (возможно, уже удалена)"))
			continue
		}
		if edit {
			a.editSession(s.ID)
		} else {
			a.showSessionDetail(s)
		}
	}
}

// filterAndShow narrows the sessions currently on screen (not the whole
// calendar) and shows the matches with statistics.
// Returns true if a filtered sub-view was shown and exited, requiring re-display of the parent list.
func (a *App) filterAndShow(sessions []models.Session, currentLabel, query string) bool {
	if query == "" {
		return false
	}
	var matched []models.Session
	for _, s := range sessions {
		if calendar.Matches(s, query) {
			matched = append(matched, s)
		}
	}
	if len(matched) == 0 {
		fmt.Println(color.Yellow(warnMark + " Ничего не найдено по запросу: " + query))
		return false
	}
	a.renderList(matched, currentLabel+" [Поиск: "+query+"]")
	return true
}

// filterByTypesInteractive prompts for types present in the current view and displays matching sessions.
// Returns true if a sub-view or picker was shown, requiring re-display of the parent list.
func (a *App) filterByTypesInteractive(sessions []models.Session, currentLabel string) bool {
	types := a.existingTypes(sessions)
	if len(types) == 0 {
		fmt.Println(color.Yellow(warnMark + " В текущем списке нет записей с типами."))
		return false
	}
	selected, ok := a.pickTypesMulti(types, "Фильтр текущего списка по типам")
	if !ok || len(selected) == 0 {
		return true
	}
	matched := filterSessionsByTypes(sessions, selected)
	if len(matched) == 0 {
		fmt.Println(color.Yellow(warnMark + " Записей выбранных типов не найдено."))
		return true
	}
	label := formatTypesLabel(currentLabel, selected)
	a.renderList(matched, label)
	return true
}

// filterByTypesDirect filters the current view by types matching query substring.
// Returns true if a filtered sub-view was shown and exited, requiring re-display of the parent list.
func (a *App) filterByTypesDirect(sessions []models.Session, currentLabel, query string) bool {
	query = strings.TrimSpace(query)
	if query == "" {
		return false
	}
	matched := filterSessionsByTypes(sessions, []string{query})
	if len(matched) == 0 {
		fmt.Println(color.Yellow(warnMark + " Нет записей с типом, содержащим: " + query))
		return false
	}
	label := formatTypesLabel(currentLabel, []string{query})
	a.renderList(matched, label)
	return true
}

// pickSession asks the user to choose when a lookup returned several sessions.
func (a *App) pickSession(sessions []models.Session) (models.Session, bool) {
	if len(sessions) == 1 {
		return sessions[0], true
	}
	fmt.Println("\nНайдено несколько записей:")
	for i, s := range sessions {
		t := s.Time
		if s.IsRepeat() {
			t = "* " + t
		}
		fmt.Printf(listFmt+"\n", i+1, t, color.Green(s.Name), s.Type,
			color.Orange(fmt.Sprintf("%d мин", s.Duration)), color.Yellow(s.ID))
	}
	idx, err := strconv.Atoi(a.prompt(""))
	if err != nil || idx < 1 || idx > len(sessions) {
		fmt.Println(color.Red(errMark + " Некорректный выбор"))
		return models.Session{}, false
	}
	return sessions[idx-1], true
}

func sortForDisplay(sessions []models.Session) {
	sort.SliceStable(sessions, func(i, j int) bool {
		a, b := sessions[i], sessions[j]
		if a.Date() != b.Date() {
			return a.Date() < b.Date()
		}
		if a.Time != b.Time {
			return a.Time < b.Time
		}
		return a.ID < b.ID
	})
}

func mustISO(iso string) time.Time {
	t, _ := time.Parse("2006-01-02", iso)
	return t
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
