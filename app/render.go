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
	unit := "min"
	if a.cfg != nil && a.cfg.DurationUnit == "hours" {
		unit = "hours"
	}
	for i, s := range ordered {
		if s.Date() != lastDate {
			lastDate = s.Date()
			fmt.Println(color.Magenta(parser.FormatDate(mustISO(lastDate))))
		}
		t := s.Time
		if s.IsRepeat() {
			t = "* " + t
		}
		durStr := calendar.FormatDuration(s.Duration, unit)
		extra := ""
		if s.Cash != 0 {
			extra += " Кэш: " + formatCashDetail(s.Cash)
		}
		if s.Status != "" {
			extra += " [" + s.Status + "]"
		}
		fmt.Printf(listFmt+"\n", i+1, t, color.Green(s.Name), s.Type,
			color.Orange(durStr), extra, color.Yellow(s.ID))
	}
}

func (a *App) printSummary(sessions []models.Session) {
	st := calendar.ComputeStats(sessions)
	cashPart := ""
	if st.TotalCash != 0 {
		cashPart = " | Общий кэш: " + formatCashDetail(st.TotalCash)
	}
	fmt.Printf("\nОбщее время: %s | Всего записей: %s%s\n",
		color.Orange(fmt.Sprintf("%.1f ч", st.TotalHours)),
		color.Green(strconv.Itoa(st.Count)),
		cashPart)
}

func (a *App) printStats(sessions []models.Session) {
	if len(sessions) == 0 {
		return
	}
	st := calendar.ComputeStats(sessions)

	fmt.Println()
	fmt.Println(sep)
	fmt.Printf(statFmt+"\n", "Всего записей:", color.Orange(strconv.Itoa(st.Count)))
	fmt.Printf(statFmt+"\n", "Средняя продолж.:",
		color.Orange(fmt.Sprintf("%.1f мин", st.AvgMinutes)))
	if st.TotalCash != 0 {
		fmt.Printf(statFmt+"\n", "Общий кэш:", formatCashDetail(st.TotalCash))
	}

	fmt.Println()
	fmt.Println(color.Yellow("  По типам записей:"))
	for _, ts := range st.ByType {
		name := ts.Type
		if name == "" {
			name = "(без типа)"
		}
		statStr := color.Orange(fmt.Sprintf("%.1f ч (%d мин)", ts.Hours, ts.Minutes))
		if ts.Cash != 0 {
			statStr += fmt.Sprintf(" | Кэш: %s", formatCashDetail(ts.Cash))
		}
		fmt.Printf(statFmt+"\n", "    "+name+":", statStr)
	}
	fmt.Println("  " + sep[:36])
	totalStatStr := color.Orange(fmt.Sprintf("%.1f ч (%d мин)", st.TotalHours, st.TotalMinutes))
	if st.TotalCash != 0 {
		totalStatStr += fmt.Sprintf(" | Кэш: %s", formatCashDetail(st.TotalCash))
	}
	fmt.Printf(statFmt+"\n", "  Общее время:", totalStatStr)
	fmt.Println(sep)
}

func (a *App) showSessionDetail(s models.Session) {
	fmt.Println()
	fmt.Println(sep)
	unit := "min"
	if a.cfg != nil && a.cfg.DurationUnit == "hours" {
		unit = "hours"
	}
	for _, fSpec := range calendar.SessionFields {
		if fSpec.Key == calendar.FieldSeries {
			if s.IsRepeat() {
				n := len(a.svc.BySeries(s.SeriesID))
				fmt.Printf("  %s%s\n", detailLabel(fSpec.Label+":"), color.Magenta(fmt.Sprintf("повтор, записей в серии: %d", n)))
			}
			continue
		}
		val := fSpec.Format(s, unit)
		switch fSpec.Key {
		case calendar.FieldID:
			val = color.Yellow(val)
		case calendar.FieldDate:
			val = color.Magenta(val)
		case calendar.FieldName:
			val = color.Green(val)
		case calendar.FieldDuration:
			val = color.Orange(val)
		case calendar.FieldCash:
			val = formatCashDetail(s.Cash)
		case calendar.FieldStatus, calendar.FieldNotes:
			val = dash(val)
		}
		fmt.Printf("  %s%s\n", detailLabel(fSpec.Label+":"), val)
	}
	fmt.Println(sep)
}

func formatCashDetail(cash float64) string {
	s := calendar.FormatCash(cash)
	if cash < 0 {
		return color.Red(s)
	}
	if cash > 0 {
		return color.Green(s)
	}
	return s
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
	unit := "min"
	if a.cfg != nil && a.cfg.DurationUnit == "hours" {
		unit = "hours"
	}
	for i, s := range sessions {
		t := s.Time
		if s.IsRepeat() {
			t = "* " + t
		}
		durStr := calendar.FormatDuration(s.Duration, unit)
		extra := ""
		if s.Cash != 0 {
			extra += " Кэш: " + formatCashDetail(s.Cash)
		}
		if s.Status != "" {
			extra += " [" + s.Status + "]"
		}
		fmt.Printf(listFmt+"\n", i+1, t, color.Green(s.Name), s.Type,
			color.Orange(durStr), extra, color.Yellow(s.ID))
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
