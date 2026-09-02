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
)

func (a *App) exportCSV() {
	fmt.Println()
	fmt.Println(color.Yellow(hdrSep + " Экспорт в CSV " + hdrSep))
	fmt.Println(" w | 1. Записи за эту неделю")
	fmt.Println(" m | 2. Записи за этот месяц")
	fmt.Println(" p | 3. Указать период вручную")
	fmt.Println(" a | 4. Экспортировать все записи")
	fmt.Println(" q | 0. Назад")

	choice, ok := a.promptChoice("")
	if !ok {
		return
	}

	var entries []models.DateEntry
	switch {
	case match(choice, "1", "w"):
		entries = a.svc.GetWeekEntries(a.resolveDate())
	case match(choice, "2", "m"):
		entries = a.svc.GetMonthEntries(a.resolveDate())
	case match(choice, "3", "p"):
		start, end, ok := a.dialogPeriod("Введите период для экспорта:", "Например: 2025, 12.2025, 10-12; 0 — отмена")
		if !ok {
			return
		}
		entries = a.svc.FindByPeriod(start, end)
	case match(choice, "4", "a"):
		entries = a.svc.GetAllEntries()
	case match(choice, "0", "q"):
		return
	default:
		fmt.Println(color.Red(errMark + " Некорректный выбор"))
		return
	}

	a.exportEntries(entries)
}

// exportEntries writes a UTF-8 CSV (with BOM, so Excel on Windows renders
// Cyrillic correctly) next to the calendar data files.
func (a *App) exportEntries(entries []models.DateEntry) {
	if countSessions(entries) == 0 {
		fmt.Println(color.Yellow(warnMark + " Нет записей для экспорта"))
		return
	}

	name := fmt.Sprintf("export_%s.csv", time.Now().Format("20060102_150405"))
	path := filepath.Join(a.svc.DataDir(), name)

	file, err := os.Create(path)
	if err != nil {
		fmt.Println(color.Red(errMark + " Ошибка создания файла: " + err.Error()))
		return
	}
	defer file.Close()

	// UTF-8 BOM so Excel on Windows detects the encoding.
	if _, err := file.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		fmt.Println(color.Red(errMark + " Ошибка записи BOM: " + err.Error()))
		return
	}

	w := csv.NewWriter(file)
	rows := [][]string{{"ID", "Дата", "Время", "Имя", "Тип", "Продолжительность (мин)", "Комментарий", "Статус"}}
	for _, de := range entries {
		for _, s := range de.Sessions {
			rows = append(rows, []string{
				s.ID, s.Date(), s.Time, s.Name, s.Type,
				strconv.Itoa(s.Duration), s.Notes, s.Status,
			})
		}
	}
	if err := w.WriteAll(rows); err != nil {
		fmt.Println(color.Red(errMark + " Ошибка записи CSV: " + err.Error()))
		return
	}
	if err := file.Sync(); err != nil {
		fmt.Println(color.Red(errMark + " Ошибка записи на диск: " + err.Error()))
		return
	}
	fmt.Println(color.Green(okMark + " Экспортировано: " + color.Green(path)))
}
