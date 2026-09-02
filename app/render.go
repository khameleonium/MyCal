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

// renderEntries prints a titled list of entries, the time/count summary, the
// per-type statistics, and then runs the interactive detail loop. Every view
// (week, month, period, all, today, CLI) funnels through here so the layout and
// the "\edit" / "/filter" affordances stay identical.
func (a *App) renderEntries(entries []models.DateEntry, label string) {
	fmt.Println()
	fmt.Println(color.Magenta(hdrSep + " " + label + " " + hdrSep))
	if countSessions(entries) == 0 {
		fmt.Println(color.Yellow(warnMark + " Записей за выбранный период не найдено"))
		return
	}

	sessions := a.printEntries(entries)
	fmt.Printf("\nОбщее время: %s | Всего записей: %s\n",
		color.Orange(fmt.Sprintf("%.1f ч", calendar.TotalHours(entries))),
		color.Green(strconv.Itoa(len(sessions))))
	a.printStats(sessions)
	a.detailLoop(sessions)
}

func (a *App) printEntries(entries []models.DateEntry) []models.Session {
	var all []models.Session
	for _, de := range entries {
		d, _ := time.Parse("2006-01-02", de.Date)
		fmt.Println(color.Magenta(d.Format("02-01-2006")))
		for _, s := range de.Sessions {
			timeStr := s.Time
			if s.IsRepeat {
				timeStr = "* " + timeStr
			}
			fmt.Printf(listFmt+"\n",
				len(all)+1, timeStr, color.Green(s.Name), s.Type,
				color.Orange(fmt.Sprintf("%d мин", s.Duration)),
				color.Yellow(s.ID))
			all = append(all, s)
		}
	}
	return all
}

func (a *App) printStats(sessions []models.Session) {
	if len(sessions) == 0 {
		return
	}
	totalMin := 0
	typeMin := make(map[string]int)
	for _, s := range sessions {
		totalMin += s.Duration
		typeMin[s.Type] += s.Duration
	}

	fmt.Println()
	fmt.Println(sep)
	fmt.Printf(statFmt+"\n", "Всего записей:", color.Orange(strconv.Itoa(len(sessions))))
	fmt.Printf(statFmt+"\n", "Средняя продолж.:",
		color.Orange(fmt.Sprintf("%.1f мин", float64(totalMin)/float64(len(sessions)))))

	fmt.Println()
	fmt.Println(color.Yellow("  По типам записей:"))
	typeKeys := make([]string, 0, len(typeMin))
	for t := range typeMin {
		typeKeys = append(typeKeys, t)
	}
	sort.Strings(typeKeys)
	for _, t := range typeKeys {
		label := t
		if label == "" {
			label = "(без типа)"
		}
		fmt.Printf(statFmt+"\n", "    "+label+":",
			color.Orange(fmt.Sprintf("%.1f ч (%d мин)", float64(typeMin[t])/60.0, typeMin[t])))
	}

	fmt.Println("  " + sep[:36])
	fmt.Printf(statFmt+"\n", "  Общее время:",
		color.Orange(fmt.Sprintf("%.1f ч (%d мин)", float64(totalMin)/60.0, totalMin)))
	fmt.Println(sep)
}

func (a *App) showSessionDetail(s models.Session) {
	if fresh := a.svc.FindByID(s.ID); len(fresh) > 0 {
		s = a.svc.HydrateSession(fresh[0])
	}
	fmt.Println()
	fmt.Println(sep)
	fmt.Printf("  ИД:               %s\n", color.Yellow(s.ID))
	fmt.Printf("  Дата:             %s\n", color.Magenta(parser.FormatDate(mustParseISO(s.Date()))))
	fmt.Printf("  Время:            %s\n", s.Time)
	fmt.Printf("  Имя:              %s\n", color.Green(s.Name))
	fmt.Printf("  Тип:              %s\n", s.Type)
	fmt.Printf("  Продолжительность: %s\n", color.Orange(fmt.Sprintf("%d мин", s.Duration)))
	fmt.Printf("  Комментарий:      %s\n", dash(s.Notes))
	fmt.Printf("  Статус:           %s\n", dash(s.Status))
	fmt.Println(sep)
}

// detailLoop lets the user drill into a listed session by number or ID, edit
// one with a "\" prefix, filter the current list with "/text", or expand
// everything with "all". It returns when the user presses Enter or stdin closes.
func (a *App) detailLoop(sessions []models.Session) {
	for {
		fmt.Println()
		fmt.Print("Номер/ID — подробности  |  \\N или \\ID — правка  |  /текст — фильтр  |  Enter — назад > ")
		in, ok := a.readLine()
		in = strings.TrimSpace(in)
		if !ok || in == "" {
			return
		}

		if rest, isFilter := strings.CutPrefix(in, "/"); isFilter {
			a.filterSessions(sessions, strings.TrimSpace(rest))
			continue
		}
		if match(in, "all", "все", "a") {
			for _, s := range sessions {
				a.showSessionDetail(s)
			}
			continue
		}

		target, edit := strings.TrimPrefix(in, "\\"), strings.HasPrefix(in, "\\")

		if idx, err := strconv.Atoi(target); err == nil && idx >= 1 && idx <= len(sessions) {
			a.act(sessions[idx-1], edit)
			continue
		}
		if found := a.svc.FindByID(target); len(found) > 0 {
			if edit {
				if s := a.chooseOne(found); s != nil {
					a.doInteractiveEdit(s.ID)
				}
			} else {
				for _, s := range found {
					a.showSessionDetail(a.svc.HydrateSession(s))
				}
			}
			continue
		}
		fmt.Println(color.Yellow(warnMark + " Запись не найдена"))
	}
}

func (a *App) act(s models.Session, edit bool) {
	if edit {
		a.doInteractiveEdit(s.ID)
		return
	}
	a.showSessionDetail(s)
}

// filterSessions narrows the sessions currently on screen (not the whole
// calendar) and prints the matches with their statistics.
func (a *App) filterSessions(sessions []models.Session, query string) {
	if query == "" {
		return
	}
	lower := strings.ToLower(query)
	var matched []models.Session
	for _, s := range sessions {
		if strings.Contains(strings.ToLower(s.Name), lower) || strings.Contains(strings.ToLower(s.Type), lower) {
			matched = append(matched, s)
		}
	}
	if len(matched) == 0 {
		fmt.Println(color.Yellow(warnMark + " Ничего не найдено по запросу: " + query))
		return
	}
	fmt.Println()
	fmt.Println(color.Magenta(hdrSep + " Фильтр: " + query + " " + hdrSep))
	a.printEntries(groupByDate(matched))
	a.printStats(matched)
	a.detailLoop(matched)
}

func (a *App) chooseOne(sessions []models.Session) *models.Session {
	if len(sessions) == 1 {
		return &sessions[0]
	}
	fmt.Println("\nНайдено несколько записей:")
	for i, s := range sessions {
		timeStr := s.Time
		if s.IsRepeat {
			timeStr = "* " + timeStr
		}
		fmt.Printf(listFmt+"\n", i+1, timeStr, color.Green(s.Name), s.Type,
			color.Orange(fmt.Sprintf("%d мин", s.Duration)), color.Yellow(s.ID))
	}
	idx, err := strconv.Atoi(a.prompt(""))
	if err != nil || idx < 1 || idx > len(sessions) {
		fmt.Println(color.Red(errMark + " Некорректный выбор"))
		return nil
	}
	return &sessions[idx-1]
}

func countSessions(entries []models.DateEntry) int {
	n := 0
	for _, de := range entries {
		n += len(de.Sessions)
	}
	return n
}

func groupByDate(sessions []models.Session) []models.DateEntry {
	byDate := make(map[string][]models.Session)
	for _, s := range sessions {
		byDate[s.Date()] = append(byDate[s.Date()], s)
	}
	result := make([]models.DateEntry, 0, len(byDate))
	for date, list := range byDate {
		sort.Slice(list, func(i, j int) bool { return list[i].Time < list[j].Time })
		result = append(result, models.DateEntry{Date: date, Sessions: list})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Date < result[j].Date })
	return result
}

func mustParseISO(iso string) time.Time {
	t, _ := time.Parse("2006-01-02", iso)
	return t
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
