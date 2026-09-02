package app

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

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
	fmt.Println()
	fmt.Println(color.Magenta(hdrSep + " " + label + " " + hdrSep))
	if len(sessions) == 0 {
		fmt.Println(color.Yellow(warnMark + " Записей нет"))
		return
	}
	a.printSessions(sessions)
	a.printSummary(sessions)
	a.printStats(sessions)
	a.detailLoop(sessions)
}

// printSessions prints sessions grouped by date with a running row number and
// returns the sessions in the exact order they were numbered.
func (a *App) printSessions(sessions []models.Session) {
	ordered := append([]models.Session(nil), sessions...)
	sortForDisplay(ordered)

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
	fmt.Printf("  ИД:               %s\n", color.Yellow(s.ID))
	fmt.Printf("  Дата:             %s\n", color.Magenta(parser.FormatDate(mustISO(s.Date()))))
	fmt.Printf("  Время:            %s\n", s.Time)
	fmt.Printf("  Имя:              %s\n", color.Green(s.Name))
	fmt.Printf("  Тип:              %s\n", s.Type)
	fmt.Printf("  Продолжительность: %s\n", color.Orange(fmt.Sprintf("%d мин", s.Duration)))
	fmt.Printf("  Комментарий:      %s\n", dash(s.Notes))
	fmt.Printf("  Статус:           %s\n", dash(s.Status))
	if s.IsRepeat() {
		n := len(a.svc.BySeries(s.SeriesID))
		fmt.Printf("  Серия:            %s\n", color.Magenta(fmt.Sprintf("повтор, записей в серии: %d", n)))
	}
	fmt.Println(sep)
}

// detailLoop lets the user drill into a listed session by number or ID, edit
// one with a "\" prefix, filter the current list with "/text", or expand
// everything with "all". Returns on Enter or closed stdin.
func (a *App) detailLoop(sessions []models.Session) {
	ordered := append([]models.Session(nil), sessions...)
	sortForDisplay(ordered)

	for {
		fmt.Println()
		fmt.Print("Номер/ID — подробности  |  \\N или \\ID — правка  |  /текст — фильтр  |  Enter — назад > ")
		in, ok := a.readLine()
		in = strings.TrimSpace(in)
		if !ok || in == "" {
			return
		}

		if rest, isFilter := strings.CutPrefix(in, "/"); isFilter {
			a.filterAndShow(ordered, strings.TrimSpace(rest))
			continue
		}
		if match(in, "all", "все", "a") {
			for _, s := range ordered {
				a.showSessionDetail(s)
			}
			continue
		}

		target, edit := strings.TrimPrefix(in, "\\"), strings.HasPrefix(in, "\\")
		if idx, err := strconv.Atoi(target); err == nil && idx >= 1 && idx <= len(ordered) {
			if edit {
				a.editSession(ordered[idx-1].ID)
			} else {
				a.showSessionDetail(ordered[idx-1])
			}
			continue
		}
		if s, ok := a.svc.ByID(target); ok {
			if edit {
				a.editSession(s.ID)
			} else {
				a.showSessionDetail(s)
			}
			continue
		}
		fmt.Println(color.Yellow(warnMark + " Запись не найдена"))
	}
}

// filterAndShow narrows the sessions currently on screen (not the whole
// calendar) and shows the matches with statistics.
func (a *App) filterAndShow(sessions []models.Session, query string) {
	if query == "" {
		return
	}
	q := strings.ToLower(query)
	var matched []models.Session
	for _, s := range sessions {
		if strings.Contains(strings.ToLower(s.Name), q) || strings.Contains(strings.ToLower(s.Type), q) {
			matched = append(matched, s)
		}
	}
	if len(matched) == 0 {
		fmt.Println(color.Yellow(warnMark + " Ничего не найдено по запросу: " + query))
		return
	}
	fmt.Println()
	fmt.Println(color.Magenta(hdrSep + " Фильтр: " + query + " " + hdrSep))
	a.printSessions(matched)
	a.printStats(matched)
	a.detailLoop(matched)
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
