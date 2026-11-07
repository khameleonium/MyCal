package app

import (
	"fmt"
	"strconv"
	"strings"

	"mycalendar/color"
	"mycalendar/config"
	"mycalendar/models"
	"mycalendar/parser"
	"mycalendar/service"
	"mycalendar/storage"
)

var defaultModeLabels = config.DefaultModeLabels

func (a *App) settingsMenu() {
	startMode := a.cfg.ActiveSplitMode()
	startName := a.cfg.DataFileName
	startEffName := a.cfg.ActiveEffectiveBaseName()
	startPath := a.cfg.DataPath
	for {
		fmt.Println()
		fmt.Println(color.Yellow(hdrSep + " Настройки " + hdrSep))
		if a.cfg.IsSettingForMode("split_mode", models.ModeCLI) {
			fmt.Printf(" m | 1. Режим хранения          [%s]\n", splitModeLabels[a.cfg.ActiveSplitMode()])
			a.hint("Способ сохранения текущего календаря (в одном файле, по годам или по месяцам)")
		}
		if a.cfg.IsSettingForMode("data_file_name", models.ModeCLI) {
			fmt.Printf(" n | 2. Имя файла данных        [%s]\n", a.cfg.DataFileName)
			a.hint("Базовое имя файла базы (по умолчанию \"mycal\")")
		}
		if a.cfg.IsSettingForMode("default_duration", models.ModeCLI) {
			fmt.Printf(" l | 3. Продолж. по умолчанию   [%d мин]\n", a.cfg.DefaultDuration)
			a.hint("Длительность события при нажатии Enter (напр. 30, 45, 60 мин)")
		}
		if a.cfg.IsSettingForMode("default_type", models.ModeCLI) {
			fmt.Printf(" t | 4. Тип по умолчанию        [%s]\n", a.cfg.DefaultType)
			a.hint("Категория события, подставляемая по умолчанию")
		}
		if a.cfg.IsSettingForMode("data_path", models.ModeCLI) {
			fmt.Printf(" p | 5. Путь к данным           [%s]\n", dataPathDisplay(a.cfg.DataPath))
			a.hint("Каталог, где сохраняются json-файлы записей")
		}
		if a.cfg.IsSettingForMode("date_check_mode", models.ModeCLI) {
			fmt.Printf(" c | 6. Проверка даты           [%s]\n", dateCheckLabels[a.cfg.DateCheckMode])
			a.hint(config.DateCheckHint(a.cfg.DateCheckMode))
		}
		if a.cfg.IsSettingForMode("use_system_date", models.ModeCLI) || a.cfg.IsSettingForMode("custom_date", models.ModeCLI) {
			fmt.Printf(" d | 7. Дата                    [%s]\n", a.dateDisplay())
			a.hint("Использовать системное время или ручной ввод фиксированной даты")
		}
		fmt.Printf(" u | 8. Свои списки             [имён: %d, статусов: %d]\n",
			len(a.cfg.CustomNames), len(a.cfg.CustomStatuses))
		a.hint("Настройка списков для быстрого выбора")
		if a.cfg.IsSettingForMode("silent_add_names", models.ModeCLI) {
			fmt.Printf(" i | 9. Молча добавлять частые имена [%s]\n", onOff(a.cfg.SilentAddNames))
			a.hint("Автоматическое сохранение часто используемых имён")
		}
		if a.cfg.IsSettingForMode("ask_on_conflict", models.ModeCLI) {
			fmt.Printf(" k | 10. Спрашивать при совпадении   [%s]\n", onOff(a.cfg.AskOnConflict))
			a.hint("Предупреждать ли, если на это же время уже есть запись")
		}
		if a.cfg.IsSettingForMode("show_hints", models.ModeCLI) {
			fmt.Printf(" h | 11. Подсказки                   [%s]\n", onOff(a.cfg.ShowHints))
			a.hint("Пояснения и примеры под каждым пунктом меню и действием")
		}
		if a.cfg.IsSettingForMode("default_mode", models.ModeCLI) {
			fmt.Printf(" w | 12. Интерфейс по умолчанию      [%s]\n", defaultModeLabels[a.cfg.DefaultMode])
			a.hint("Что открывать при запуске 'mycal' без параметров (CLI / TUI / Web)")
		}
		fmt.Println(" s | 13. Служба автозапуска (Сервис)")
		a.hint("Управление работой MyCal как фоновой системной службы")
		if a.cfg.IsSettingForMode("duration_unit", models.ModeCLI) {
			fmt.Printf(" f | 14. Формат длительности         [%s]\n", durationUnitDisplay(a.cfg.DurationUnit))
			a.hint("Отображение продолжительности (в минутах или часах)")
		}
		if a.cfg.IsSettingForMode("web_port", models.ModeCLI) {
			fmt.Printf(" o | 15. Порт веб-сервера            [%d]\n", a.cfg.WebPort)
			a.hint("TCP-порт для запуска веб-интерфейса (1–65535)")
		}
		if a.cfg.IsSettingForMode("tui_default_period", models.ModeCLI) {
			fmt.Printf(" e | 16. Период запуска в TUI        [%s]\n", tuiDefaultPeriodDisplay(a.cfg.TUIDefaultPeriod))
			a.hint("Основной период при запуске в TUI (Сегодня, Все записи, Неделя, Месяц)")
		}
		fmt.Printf(" b | 17. Выбрать календарь записей   [%s]\n", a.cfg.ActiveCalendarDisplayName())
		a.hint("Переключение активного календаря записей")
		fmt.Println(" g | 18. Создать календарь записей")
		a.hint("Создать новый календарь с префиксом (единый или погодовой файл)")
		if a.cfg.IsSettingForMode("web_lan", models.ModeCLI) {
			fmt.Printf(" a | 19. Доступ к Web из сети (LAN)  [%s]\n", onOff(a.cfg.WebLAN))
			a.hint("Разрешить открытие веб-интерфейса с других устройств в локальной сети (0.0.0.0)")
		}
		fmt.Println(" q | 0. Сохранить и выйти")
		a.hint("Применить изменения и вернуться в главное меню")

		choice, ok := a.menuChoice()
		if !ok {
			choice = "0"
		}
		switch {
		case match(choice, "1", "m"):
			config.CycleSetting(a.cfg, "split_mode", nil)
		case match(choice, "2", "n"):
			if v := a.dialog(fmt.Sprintf("Имя файла данных [%s]:", a.cfg.DataFileName),
				"без расширения и путей; 0 — отмена"); v != "" && !isCancel(v) {
				if clean := config.SanitizeBaseName(v); clean == "" {
					fmt.Println(color.Yellow(warnMark + " Недопустимое имя файла"))
				} else {
					a.cfg.DataFileName = clean
				}
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
				norm := storage.NormalizeDataPath(v)
				// Checked here, not at next launch: an unusable path saved to
				// the config would stop the program from starting at all.
				checkDir := norm
				if checkDir == "" {
					checkDir = config.EffectiveDataDir("", a.cfgPath)
				}
				if err := storage.EnsureWritableDir(checkDir); err != nil {
					fmt.Println(color.Red(errMark + " " + err.Error()))
					fmt.Println(color.Yellow(warnMark + " Путь не изменён"))
				} else {
					a.cfg.DataPath = norm
				}
			}
		case match(choice, "6", "c"):
			config.CycleSetting(a.cfg, "date_check_mode", nil)
		case match(choice, "7", "d"):
			a.dateSettings()
		case match(choice, "8", "u"):
			a.listsSettings()
		case match(choice, "9", "i"):
			config.CycleSetting(a.cfg, "silent_add_names", nil)
		case match(choice, "10", "k", "с"):
			config.CycleSetting(a.cfg, "ask_on_conflict", nil)
		case match(choice, "11", "h", "п"):
			config.CycleSetting(a.cfg, "show_hints", nil)
		case match(choice, "12", "w"):
			config.CycleSetting(a.cfg, "default_mode", nil)
		case match(choice, "13", "s"):
			a.serviceSettings()
		case match(choice, "14", "f"):
			config.CycleSetting(a.cfg, "duration_unit", nil)
		case match(choice, "15", "o", "о"):
			if v := a.dialog(fmt.Sprintf("Порт веб-сервера (1–65535) [%d]:", a.cfg.WebPort), "0 — отмена"); v != "" && !isCancel(v) {
				if p, err := strconv.Atoi(v); err == nil && p > 0 && p <= 65535 {
					a.cfg.WebPort = p
				} else {
					fmt.Println(color.Yellow(warnMark + " Некорректный порт (допустимо 1–65535)"))
				}
			}
		case match(choice, "16", "e", "у"):
			config.CycleSetting(a.cfg, "tui_default_period", nil)
		case match(choice, "17", "b"):
			a.selectCalendarDialog()
			startMode = a.cfg.ActiveSplitMode()
			startEffName = a.cfg.ActiveEffectiveBaseName()
		case match(choice, "18", "g"):
			a.createCalendarDialog()
			startMode = a.cfg.ActiveSplitMode()
			startEffName = a.cfg.ActiveEffectiveBaseName()
		case match(choice, "19", "a", "ф"):
			config.CycleSetting(a.cfg, "web_lan", nil)
		case match(choice, "0", "q"):
			a.persistSettings(startMode, startName, startEffName, startPath)
			return
		default:
			fmt.Println(color.Red(errMark + " Некорректный выбор (0–19)"))
		}
	}
}

// persistSettings writes the config and migrates data on disk when the split
// mode or the data file name changed. A data-path change only takes effect on
// the next launch (files are not moved between directories).
func (a *App) persistSettings(startMode, startName, startEffName, startPath string) {
	modeChanged := a.cfg.ActiveSplitMode() != startMode
	nameChanged := a.cfg.DataFileName != startName

	if modeChanged || nameChanged {
		switch {
		case modeChanged && nameChanged:
			fmt.Println(color.Yellow(warnMark + " Режим хранения и имя файла изменены — данные перенесены."))
		case modeChanged:
			fmt.Println(color.Yellow(warnMark + " Режим хранения изменён — данные перезаписаны в новом формате."))
		case nameChanged:
			fmt.Println(color.Yellow(warnMark + " Имя файла данных изменено — данные перенесены."))
		}
		a.svc.SetMode(a.cfg.ActiveSplitMode())
		a.svc.SetBaseName(a.cfg.ActiveEffectiveBaseName())
		if a.save() != nil {
			return
		}
		if nameChanged {
			a.svc.RemoveDataFiles(startEffName)
			storage.RenameFamilyDataFiles(a.svc.DataDir(), startName, a.cfg.DataFileName)
		}
	}

	if a.cfg.DataPath != startPath {
		fmt.Println(color.Yellow(warnMark + " Путь к данным изменён — вступит в силу после перезапуска (файлы не переносятся автоматически)."))
	}

	if err := config.Save(a.ctx, a.cfgPath, a.cfg); err != nil {
		fmt.Println(color.Red(errMark + " Не удалось сохранить настройки: " + err.Error()))
		return
	}
	fmt.Println(color.Green(okMark + " Настройки сохранены"))
}

// selectCalendarDialog displays all calendars in the data directory and switches
// to the chosen calendar.
func (a *App) selectCalendarDialog() {
	cals := a.svc.ListCalendars(a.cfg)
	fmt.Println()
	fmt.Println(color.Yellow(hdrSep + " Выбрать календарь записей " + hdrSep))
	for i, c := range cals {
		modeLbl := splitModeLabels[c.SplitMode]
		if modeLbl == "" {
			modeLbl = splitModeLabels[models.SplitNone]
		}
		activeTag := ""
		if c.Active {
			activeTag = color.Green(" ← активный")
		}
		fmt.Printf("  %d. %s [%s]%s\n", i+1, c.Label, modeLbl, activeTag)
	}
	fmt.Println("  n. Создать новый календарь записей")
	fmt.Println("  0. Отмена")

	v := a.dialog("Номер календаря (или префикс):", "1..N, n — создать новый, 0 — отмена")
	if v == "" || isCancel(v) {
		return
	}
	if strings.EqualFold(v, "n") || strings.EqualFold(v, "+") || strings.EqualFold(v, "н") {
		a.createCalendarDialog()
		return
	}

	var targetKey string
	var matched bool
	if idx, err := strconv.Atoi(v); err == nil && idx >= 1 && idx <= len(cals) {
		targetKey = cals[idx-1].Key
		matched = true
	} else {
		for _, c := range cals {
			if strings.EqualFold(c.Key, v) || strings.EqualFold(c.BaseName, v) {
				targetKey = c.Key
				matched = true
				break
			}
		}
	}
	if !matched {
		fmt.Println(color.Red(errMark + " Календарь не найден"))
		return
	}
	if err := a.svc.SwitchCalendar(a.ctx, a.cfg, targetKey); err != nil {
		fmt.Println(color.Red(errMark + " Не удалось открыть календарь: " + err.Error()))
		return
	}
	a.saveConfig()
	fmt.Println(color.Green(fmt.Sprintf("%s Выбран календарь: %s [%s] (записей: %d)",
		okMark, a.cfg.ActiveCalendarDisplayName(), splitModeLabels[a.cfg.ActiveSplitMode()], a.svc.Count())))
}

// createCalendarDialog prompts for a calendar prefix and split mode, creates
// the new calendar file on disk, and switches to it.
func (a *App) createCalendarDialog() {
	fmt.Println()
	fmt.Println(color.Yellow(hdrSep + " Создать календарь записей " + hdrSep))
	base := a.cfg.DataFileName
	if strings.TrimSpace(base) == "" {
		base = "mycal"
	}
	rawKey := a.dialog(
		fmt.Sprintf("Префикс нового календаря (файл %s--<префикс>.json):", base),
		"напр. work, sport, личное; 0 — отмена",
	)
	if rawKey == "" || isCancel(rawKey) {
		return
	}
	calKey := config.SanitizeCalendarKey(rawKey)
	if calKey == "" {
		fmt.Println(color.Yellow(warnMark + " Недопустимое имя (префикс) календаря"))
		return
	}

	fmt.Println("  Режим хранения для календаря «" + calKey + "»:")
	fmt.Printf("    1. В одном файле (%s--%s.json) [по умолчанию]\n", base, calKey)
	fmt.Printf("    2. По годам      (ГГГГ_%s--%s.json)\n", base, calKey)
	fmt.Printf("    3. По месяцам    (ГГГГ-ММ_%s--%s.json)\n", base, calKey)
	modeIn := a.dialog("Выбор (1–3, Enter = 1):", "0 — отмена")
	if isCancel(modeIn) {
		return
	}
	splitMode := models.SplitNone
	switch strings.TrimSpace(modeIn) {
	case "", "1", "none":
		splitMode = models.SplitNone
	case "2", "year":
		splitMode = models.SplitYear
	case "3", "month":
		splitMode = models.SplitMonth
	default:
		fmt.Println(color.Yellow(warnMark + " Неизвестный выбор, используется режим «В одном файле»"))
	}

	if err := a.svc.CreateCalendar(a.ctx, a.cfg, calKey, splitMode); err != nil {
		fmt.Println(color.Red(errMark + " Не удалось создать календарь: " + err.Error()))
		return
	}
	a.saveConfig()
	fmt.Println(color.Green(fmt.Sprintf("%s Создан и выбран календарь: %s [%s]",
		okMark, a.cfg.ActiveCalendarDisplayName(), splitModeLabels[a.cfg.ActiveSplitMode()])))
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

// listsSettings manages both user-editable lists. Custom statuses used to be
// append-only from the status picker with no way to review or remove them.
func (a *App) listsSettings() {
	for {
		fmt.Println()
		fmt.Println(color.Yellow("  Свои списки"))
		fmt.Printf(" n | 1. Частые имена   [%d шт]\n", len(a.cfg.CustomNames))
		fmt.Printf(" s | 2. Свои статусы   [%d шт]\n", len(a.cfg.CustomStatuses))
		fmt.Println(" q | 0. Назад")

		choice, ok := a.menuChoice()
		if !ok {
			return
		}
		switch {
		case match(choice, "1", "n"):
			a.editStringList("Частые имена (для быстрого выбора)", "имя", &a.cfg.CustomNames)
		case match(choice, "2", "s"):
			a.editStringList("Свои статусы", "статус", &a.cfg.CustomStatuses)
		case match(choice, "0", "q"):
			return
		default:
			fmt.Println(color.Red(errMark + " Некорректный выбор (0–2)"))
		}
	}
}

// editStringList is the add/remove editor shared by the frequent-names and the
// custom-statuses lists.
func (a *App) editStringList(title, noun string, list *[]string) {
	for {
		fmt.Println()
		fmt.Println(color.Yellow("  " + title))
		if len(*list) == 0 {
			fmt.Println("  (Список пуст)")
		}
		for i, v := range *list {
			fmt.Printf("  %d. %s\n", i+1, v)
		}
		fmt.Println(sep)
		fmt.Println(" a | 1. Добавить (" + noun + ")")
		fmt.Println(" r | 2. Удалить")
		fmt.Println(" q | 0. Назад")

		choice, ok := a.menuChoice()
		if !ok {
			return
		}
		switch {
		case match(choice, "1", "a"):
			if v := a.dialog("Новый "+noun+":", "0 — отмена"); v != "" && !isCancel(v) {
				if containsFold(*list, v) {
					fmt.Println(color.Yellow(warnMark + " Такое значение уже есть в списке"))
				} else {
					*list = append(*list, v)
					fmt.Println(color.Green(okMark + " Добавлено"))
				}
			}
		case match(choice, "2", "r"):
			if len(*list) == 0 {
				fmt.Println(color.Yellow(warnMark + " Список пуст"))
				continue
			}
			idxStr := a.dialog("Номер для удаления:", "0 — отмена")
			if isCancel(idxStr) {
				continue
			}
			idx, err := strconv.Atoi(idxStr)
			if err != nil || idx < 1 || idx > len(*list) {
				fmt.Println(color.Red(errMark + " Неверный номер"))
				continue
			}
			*list = append((*list)[:idx-1], (*list)[idx:]...)
			fmt.Println(color.Green(okMark + " Удалено"))
		case match(choice, "0", "q"):
			return
		default:
			fmt.Println(color.Red(errMark + " Некорректный выбор"))
		}
	}
}

func (a *App) serviceSettings() {
	mgr := service.NewManager()
	info, err := mgr.Status(a.ctx)
	if err != nil {
		fmt.Println(color.Red(errMark + " Ошибка проверки службы: " + err.Error()))
		return
	}
	fmt.Println()
	fmt.Println(color.Yellow(hdrSep + " Фоновая служба MyCal " + hdrSep))
	fmt.Printf("Платформа:   %s\n", info.Platform)
	if info.UnitPath != "" {
		fmt.Printf("Юнит-файл:   %s\n", info.UnitPath)
	}
	if info.Installed {
		fmt.Printf("Установлена: %s\n", color.Green("Да"))
	} else {
		fmt.Printf("Установлена: %s\n", color.Yellow("Нет"))
	}
	if info.Running {
		fmt.Printf("Статус:      %s\n", color.Green("Работает"))
	} else {
		fmt.Printf("Статус:      %s\n", color.Red("Остановлена / Неактивна"))
	}
	fmt.Println()
	fmt.Println("1. Установить и запустить службу (порт " + strconv.Itoa(a.cfg.WebPort) + ")")
	fmt.Println("2. Запустить службу")
	fmt.Println("3. Остановить службу")
	fmt.Println("4. Перезапустить службу")
	fmt.Println("5. Удалить службу")
	fmt.Println("0. Назад")
	c, ok := a.menuChoice()
	if !ok || c == "0" {
		return
	}
	switch c {
	case "1":
		if err := mgr.Install(a.ctx, a.cfg.WebPort); err != nil {
			fmt.Println(color.Red(errMark + " " + err.Error()))
		} else {
			fmt.Println(color.Green(okMark + " Служба успешно установлена и запущена!"))
		}
	case "2":
		if err := mgr.Start(a.ctx); err != nil {
			fmt.Println(color.Red(errMark + " " + err.Error()))
		} else {
			fmt.Println(color.Green(okMark + " Служба запущена."))
		}
	case "3":
		if err := mgr.Stop(a.ctx); err != nil {
			fmt.Println(color.Red(errMark + " " + err.Error()))
		} else {
			fmt.Println(color.Green(okMark + " Служба остановлена."))
		}
	case "4":
		if err := mgr.Restart(a.ctx); err != nil {
			fmt.Println(color.Red(errMark + " " + err.Error()))
		} else {
			fmt.Println(color.Green(okMark + " Служба перезапущена."))
		}
	case "5":
		if err := mgr.Uninstall(a.ctx); err != nil {
			fmt.Println(color.Red(errMark + " " + err.Error()))
		} else {
			fmt.Println(color.Green(okMark + " Служба удалена."))
		}
	}
}

func containsFold(list []string, v string) bool {
	for _, x := range list {
		if strings.EqualFold(x, v) {
			return true
		}
	}
	return false
}

func (a *App) dateDisplay() string {
	return config.FormatCustomDateDisplay(a.cfg)
}

func dataPathDisplay(p string) string {
	return config.DataPathDisplay(p)
}

func onOff(v bool) string {
	if v {
		return "Вкл"
	}
	return "Выкл"
}

func durationUnitDisplay(u string) string {
	if u == "hours" {
		return "Часы (1 ч / 1.5 ч)"
	}
	return "Минуты (мин)"
}

func tuiDefaultPeriodDisplay(p string) string {
	switch p {
	case "all":
		return "Все записи"
	case "week":
		return "Неделя"
	case "month":
		return "Месяц"
	default:
		return "Сегодня"
	}
}
