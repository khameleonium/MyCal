package app

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"mycalendar/color"
	"mycalendar/models"
	"mycalendar/parser"
)

func (a *App) exportMenu() {
	fmt.Println()
	fmt.Println(color.Yellow(hdrSep + " Экспорт в CSV " + hdrSep))
	fmt.Println(" w | 1. За эту неделю")
	a.hint("Выгрузить события текущей недели в файл .csv")
	fmt.Println(" m | 2. За этот месяц")
	a.hint("Выгрузить события текущего месяца в файл .csv")
	fmt.Println(" p | 3. Указать период")
	a.hint("Выгрузить диапазон дат (напр. \"10-12\", \"01.10-15.10\")")
	fmt.Println(" a | 4. Все записи")
	a.hint("Экспортировать абсолютно все записи календаря")
	fmt.Println(" t | 5. По типам")
	a.hint("Выгрузить записи выбранных типов")
	fmt.Println(" q | 0. Назад")
	a.hint("Возврат в главное меню")

	choice, ok := a.menuChoice()
	if !ok {
		return
	}

	var sessions []models.Session
	switch {
	case match(choice, "1", "w"):
		sessions = a.svc.Week(a.today())
	case match(choice, "2", "m"):
		sessions = a.svc.Month(a.today())
	case match(choice, "3", "p"):
		start, end, ok := a.askPeriod("Период для экспорта:", "Например: 2025, 12.2025, 10-12; 0 — отмена")
		if !ok {
			return
		}
		sessions = a.svc.InRange(start, end)
	case match(choice, "4", "a"):
		sessions = a.svc.All()
	case match(choice, "5", "t", "т"):
		a.exportByTypes()
		return
	case match(choice, "0", "q"):
		return
	default:
		fmt.Println(color.Red(errMark + " Некорректный выбор"))
		return
	}
	a.exportCSV(sessions)
}

func (a *App) exportByTypes() {
	allTypes := a.calendarTypes(true)
	if len(allTypes) == 0 {
		fmt.Println(color.Yellow(warnMark + " В календаре пока нет типов записей."))
		return
	}
	selected, ok := a.pickTypesMulti(allTypes, "Экспорт по типам")
	if !ok || len(selected) == 0 {
		return
	}

	fmt.Println()
	fmt.Println(color.Yellow(hdrSep + " Период для экспорта " + hdrSep))
	fmt.Println(" w | 1. За эту неделю")
	fmt.Println(" m | 2. За этот месяц")
	fmt.Println(" p | 3. Указать период")
	fmt.Println(" a | 4. Все записи")
	fmt.Println(" q | 0. Отмена")
	fmt.Println(" Enter / 4 — Все записи")

	choice, ok := a.menuChoice()
	if !ok || match(choice, "0", "q") {
		return
	}

	var sessions []models.Session
	switch {
	case match(choice, "1", "w"):
		sessions = a.svc.Week(a.today())
	case match(choice, "2", "m"):
		sessions = a.svc.Month(a.today())
	case match(choice, "3", "p"):
		start, end, ok := a.askPeriod("Период для экспорта:", "Например: 2025, 12.2025, 10-12; 0 — отмена")
		if !ok {
			return
		}
		sessions = a.svc.InRange(start, end)
	default:
		sessions = a.svc.All()
	}

	filtered := filterSessionsByTypes(sessions, selected)
	a.exportCSV(filtered)
}

// exportCSV writes a UTF-8 CSV (with BOM so Excel on Windows reads Cyrillic)
// next to the calendar data files.
func (a *App) exportCSV(sessions []models.Session) {
	if len(sessions) == 0 {
		fmt.Println(color.Yellow(warnMark + " Нет записей для экспорта"))
		return
	}
	stamp := time.Now().Format("20060102_150405")
	path := filepath.Join(a.svc.DataDir(), "export_"+stamp+".csv")
	for i := 2; fileExists(path); i++ {
		path = filepath.Join(a.svc.DataDir(), fmt.Sprintf("export_%s_%d.csv", stamp, i))
	}

	f, err := os.Create(path)
	if err != nil {
		fmt.Println(color.Red(errMark + " Не удалось создать файл: " + explainFS(err)))
		return
	}
	defer f.Close()

	if _, err := f.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		fmt.Println(color.Red(errMark + " Ошибка записи: " + err.Error()))
		return
	}

	w := csv.NewWriter(f)
	rows := [][]string{{"ID", "Дата", "Время", "Имя", "Тип", "Продолжительность (мин)", "Комментарий", "Статус", "Серия"}}
	for _, s := range sessions {
		rows = append(rows, []string{
			s.ID, parser.FormatDate(mustISO(s.Date())), s.Time, s.Name, s.Type,
			strconv.Itoa(s.Duration), s.Notes, s.Status, s.SeriesID,
		})
	}
	if err := w.WriteAll(rows); err != nil {
		fmt.Println(color.Red(errMark + " Ошибка записи CSV: " + err.Error()))
		return
	}
	if err := f.Sync(); err != nil {
		fmt.Println(color.Red(errMark + " Ошибка записи на диск: " + err.Error()))
		return
	}
	fmt.Println(color.Green(okMark + " Экспортировано: " + color.Green(path)))
}
