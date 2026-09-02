package app

import (
	"fmt"
	"strconv"

	"mycalendar/color"
	"mycalendar/config"
	"mycalendar/models"
	"mycalendar/parser"
)

func (a *App) settingsMenu() {
	startMode := a.cfg.SplitMode
	for {
		fmt.Println()
		fmt.Println(color.Yellow(hdrSep + " Настройки " + hdrSep))
		fmt.Printf(" m | 1. Режим хранения          [%s]\n", splitModeLabels[a.cfg.SplitMode])
		fmt.Printf(" n | 2. Имя файла данных        [%s]\n", a.cfg.DataFileName)
		fmt.Printf(" l | 3. Продолж. по умолчанию   [%d мин]\n", a.cfg.DefaultDuration)
		fmt.Printf(" t | 4. Тип по умолчанию        [%s]\n", a.cfg.DefaultType)
		fmt.Printf(" p | 5. Путь к данным           [%s]\n", dataPathDisplay(a.cfg.DataPath))
		fmt.Printf(" c | 6. Проверка даты           [%s]\n", dateCheckLabels[a.cfg.DateCheckMode])
		fmt.Printf(" d | 7. Дата                    [%s]\n", a.dateDisplay())
		fmt.Printf(" u | 8. Частые имена            [%d шт]\n", len(a.cfg.CustomNames))
		fmt.Printf(" i | 9. Молча добавлять частые имена [%s]\n", onOff(a.cfg.SilentAddNames))
		fmt.Println(" q | 0. Сохранить и выйти")

		choice, ok := a.menuChoice()
		if !ok {
			choice = "0"
		}
		switch {
		case match(choice, "1", "m"):
			a.cfg.SplitMode = nextInCycle([]string{models.SplitNone, models.SplitYear, models.SplitMonth}, a.cfg.SplitMode)
		case match(choice, "2", "n"):
			if v := a.dialog(fmt.Sprintf("Имя файла данных [%s]:", a.cfg.DataFileName), "0 — отмена"); v != "" && !isCancel(v) {
				a.cfg.DataFileName = v
			}
		case match(choice, "3", "l"):
			if v := a.dialog(fmt.Sprintf("Продолжительность по умолчанию (мин) [%d]:", a.cfg.DefaultDuration), "0 — отмена"); v != "" && !isCancel(v) {
				if d, err := strconv.Atoi(v); err == nil && d > 0 {
					a.cfg.DefaultDuration = d
				} else {
					fmt.Println(color.Yellow(warnMark + " Некорректное значение"))
				}
			}
		case match(choice, "4", "t"):
			if v := a.dialog(fmt.Sprintf("Тип по умолчанию [%s]:", a.cfg.DefaultType), "0 — отмена"); v != "" && !isCancel(v) {
				a.cfg.DefaultType = v
			}
		case match(choice, "5", "p"):
			if v := a.dialog(fmt.Sprintf("Путь к данным [%s]:", dataPathDisplay(a.cfg.DataPath)), "0 — отмена"); v != "" && !isCancel(v) {
				a.cfg.DataPath = v
			}
		case match(choice, "6", "c"):
			a.cfg.DateCheckMode = nextInCycle(dateCheckOrder, a.cfg.DateCheckMode)
		case match(choice, "7", "d"):
			a.dateSettings()
		case match(choice, "8", "u"):
			a.customNamesSettings()
		case match(choice, "9", "i"):
			a.cfg.SilentAddNames = !a.cfg.SilentAddNames
		case match(choice, "0", "q"):
			a.persistSettings(startMode)
			return
		default:
			fmt.Println(color.Red(errMark + " Некорректный выбор (0–9)"))
		}
	}
}

// persistSettings writes the config and, if the split mode changed, re-persists
// the calendar in the new mode (storage.Save removes files from the old mode).
func (a *App) persistSettings(startMode string) {
	if a.cfg.SplitMode != startMode {
		fmt.Println(color.Yellow(warnMark + " Режим хранения изменён — данные перезаписываются в новом формате."))
		a.svc.SetMode(a.cfg.SplitMode)
		if a.save() != nil {
			return
		}
	}
	if err := config.Save(a.ctx, a.cfgPath, a.cfg); err != nil {
		fmt.Println(color.Red(errMark + " Не удалось сохранить настройки: " + err.Error()))
		return
	}
	fmt.Println(color.Green(okMark + " Настройки сохранены"))
}

func (a *App) dateSettings() {
	for {
		fmt.Println()
		fmt.Println(color.Yellow("  Дата"))
		fmt.Printf("  Текущая: %s\n", color.Magenta(parser.FormatDate(a.today())))
		fmt.Println(" s | 1. Системное время")
		fmt.Println(" m | 2. Ввести дату вручную")
		fmt.Println(" q | 0. Назад")

		choice, ok := a.menuChoice()
		if !ok {
			return
		}
		switch {
		case match(choice, "1", "s"):
			a.cfg.UseSystemDate = true
			a.forgetToday()
			fmt.Println(color.Green(okMark + " Используется системное время"))
			return
		case match(choice, "2", "m"):
			if date, ok := a.askDate("Текущая дата:", "DD-MM-YYYY, DD.MM.YYYY, YYYY-MM-DD; 0 — отмена"); ok {
				a.cfg.CustomDate = date.Format("2006-01-02")
				a.cfg.UseSystemDate = false
				a.forgetToday()
				fmt.Println(color.Green(okMark + " Установлено: " + color.Magenta(parser.FormatDate(date))))
			}
			return
		case match(choice, "0", "q"):
			return
		default:
			fmt.Println(color.Red(errMark + " Некорректный выбор (0–2)"))
		}
	}
}

func (a *App) customNamesSettings() {
	for {
		fmt.Println()
		fmt.Println(color.Yellow("  Частые имена (для автодополнения)"))
		if len(a.cfg.CustomNames) == 0 {
			fmt.Println("  (Список пуст)")
		}
		for i, name := range a.cfg.CustomNames {
			fmt.Printf("  %d. %s\n", i+1, name)
		}
		fmt.Println(sep)
		fmt.Println(" a | 1. Добавить имя")
		fmt.Println(" r | 2. Удалить имя")
		fmt.Println(" q | 0. Назад")

		choice, ok := a.menuChoice()
		if !ok {
			return
		}
		switch {
		case match(choice, "1", "a"):
			if name := a.dialog("Новое имя:", "0 — отмена"); name != "" && !isCancel(name) {
				a.cfg.CustomNames = append(a.cfg.CustomNames, name)
				fmt.Println(color.Green(okMark + " Добавлено"))
			}
		case match(choice, "2", "r"):
			if len(a.cfg.CustomNames) == 0 {
				fmt.Println(color.Yellow(warnMark + " Список пуст"))
				continue
			}
			idxStr := a.dialog("Номер для удаления:", "0 — отмена")
			if isCancel(idxStr) {
				continue
			}
			idx, err := strconv.Atoi(idxStr)
			if err != nil || idx < 1 || idx > len(a.cfg.CustomNames) {
				fmt.Println(color.Red(errMark + " Неверный номер"))
				continue
			}
			a.cfg.CustomNames = append(a.cfg.CustomNames[:idx-1], a.cfg.CustomNames[idx:]...)
			fmt.Println(color.Green(okMark + " Удалено"))
		case match(choice, "0", "q"):
			return
		default:
			fmt.Println(color.Red(errMark + " Некорректный выбор"))
		}
	}
}

func (a *App) dateDisplay() string {
	if a.cfg.UseSystemDate || a.cfg.CustomDate == "" {
		return "Системная"
	}
	return a.cfg.CustomDate
}

func dataPathDisplay(p string) string {
	if p == "" || p == "." {
		return "./"
	}
	return p
}

func onOff(v bool) string {
	if v {
		return "Вкл"
	}
	return "Выкл"
}

func nextInCycle(order []string, current string) string {
	for i, v := range order {
		if v == current {
			return order[(i+1)%len(order)]
		}
	}
	if len(order) > 0 {
		return order[0]
	}
	return current
}
