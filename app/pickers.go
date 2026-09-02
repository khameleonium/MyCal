package app

import (
	"fmt"
	"strconv"
	"strings"

	"mycalendar/color"
	"mycalendar/config"
)

// askName prompts for a mandatory entry name, offering the frequent-names
// picker via "/". Returns "" if cancelled or stdin closes.
func (a *App) askName(title, current string) string {
	hasFreq := len(a.cfg.CustomNames) > 0
	for {
		fmt.Println(title)
		switch {
		case hasFreq && current != "":
			fmt.Println("Enter — без изменений, 0 — отмена, '/' — выбрать из частых имён")
		case hasFreq:
			fmt.Println("Введите текст, 0 — отмена, или '/' — выбрать из частых имён")
		case current != "":
			fmt.Println("Enter — без изменений, 0 — отмена")
		default:
			fmt.Println("Обязательное поле; 0 — отмена")
		}
		fmt.Print("> ")
		input := a.line()

		if a.stdinClosed || isCancel(input) {
			return ""
		}
		if input == "" {
			if current != "" {
				return current // unchanged
			}
			fmt.Println(color.Yellow(warnMark + " Поле не может быть пустым"))
			continue
		}
		if input == "/" && hasFreq {
			if picked := a.pickFrequentName(); picked != "" {
				return picked
			}
			continue
		}
		a.trackNameFrequency(input)
		return input
	}
}

func (a *App) pickFrequentName() string {
	fmt.Println(color.Yellow("\n  Частые имена:"))
	for i, cn := range a.cfg.CustomNames {
		fmt.Printf("  %d. %s\n", i+1, cn)
	}
	idxStr := a.dialog("", "Выберите номер (0 — отмена)")
	if isCancel(idxStr) {
		return ""
	}
	idx, err := strconv.Atoi(idxStr)
	if err != nil || idx < 1 || idx > len(a.cfg.CustomNames) {
		fmt.Println(color.Red(errMark + " Неверный выбор"))
		return ""
	}
	return a.cfg.CustomNames[idx-1]
}

// trackNameFrequency promotes a name to CustomNames once it has been used
// three or more times (counting this use).
func (a *App) trackNameFrequency(name string) {
	for _, cn := range a.cfg.CustomNames {
		if strings.EqualFold(cn, name) {
			return
		}
	}
	count := 1
	for _, s := range a.svc.All() {
		if strings.EqualFold(s.Name, name) {
			count++
		}
	}
	if count < 3 {
		return
	}
	add := a.cfg.SilentAddNames ||
		a.confirm(fmt.Sprintf("Имя \"%s\" часто используется. Добавить в список для быстрого выбора?", name))
	if !add {
		return
	}
	a.cfg.CustomNames = append(a.cfg.CustomNames, name)
	if err := config.Save(a.ctx, a.cfgPath, a.cfg); err != nil {
		fmt.Println(color.Red(errMark + " Ошибка сохранения настроек: " + err.Error()))
		return
	}
	if !a.cfg.SilentAddNames {
		fmt.Println(color.Green(okMark + " Добавлено"))
	}
}

// askType shows the type pick-list (defaults + used types + manual entry).
// If current != "" an empty reply keeps it; otherwise an empty reply picks the
// configured default type.
func (a *App) askType(current string) (string, bool) {
	types := a.svc.Types(a.cfg.DefaultType, current)
	fmt.Println()
	fmt.Println(color.Yellow("Тип записи:"))
	for i, t := range types {
		fmt.Printf("  %d. %s\n", i+1, t)
	}
	fmt.Printf("  %d. Ручной ввод\n", len(types)+1)
	if current != "" {
		fmt.Println("  Enter — оставить: " + color.Green(current))
	}

	defaultIdx := 1
	for i, t := range types {
		if strings.EqualFold(t, a.cfg.DefaultType) {
			defaultIdx = i + 1
			break
		}
	}

	for {
		fmt.Print("> ")
		input := a.line()
		if a.stdinClosed || isCancel(input) {
			return "", false
		}
		if input == "" {
			if current != "" {
				return current, true
			}
			return types[defaultIdx-1], true
		}
		idx, err := strconv.Atoi(input)
		if err != nil || idx < 1 || idx > len(types)+1 {
			fmt.Println(color.Yellow(warnMark + " Введите номер из списка"))
			continue
		}
		if idx == len(types)+1 {
			custom := a.dialog("Название нового типа:", "0 — отмена")
			if isCancel(custom) {
				return "", false
			}
			if custom == "" {
				if current != "" {
					return current, true
				}
				return types[defaultIdx-1], true
			}
			return custom, true
		}
		return types[idx-1], true
	}
}

// askStatus shows the status pick-list. "0" always means "<empty>". If
// current != "" an empty reply keeps it; otherwise an empty reply means empty.
func (a *App) askStatus(current string) (string, bool) {
	statuses := a.svc.Statuses(a.cfg.CustomStatuses...)

	fmt.Println()
	fmt.Println(color.Yellow("Статус:"))
	fmt.Println("  0. <пусто>")
	for i, st := range statuses {
		fmt.Printf("  %d. %s\n", i+1, st)
	}
	fmt.Printf("  %d. Ручной ввод\n", len(statuses)+1)
	if current != "" {
		fmt.Println("  Enter — оставить: " + color.Green(current))
	}

	for {
		fmt.Print("> ")
		input := a.line()
		if input == "0" {
			return "", true // explicit clear
		}
		if input == "" {
			return current, true // keep (== "" when there is nothing to keep)
		}
		if a.stdinClosed || strings.EqualFold(strings.TrimSpace(input), "отмена") {
			return "", false
		}
		idx, err := strconv.Atoi(input)
		if err != nil || idx < 1 || idx > len(statuses)+1 {
			fmt.Println(color.Yellow(warnMark + " Введите номер из списка"))
			continue
		}
		if idx == len(statuses)+1 {
			custom := a.dialog("Название статуса:", "0 — отмена")
			if isCancel(custom) {
				return "", false
			}
			if custom == "" {
				return current, true
			}
			if a.confirm(color.Yellow(askMark + " Добавить в список?")) {
				a.cfg.CustomStatuses = append(a.cfg.CustomStatuses, custom)
			}
			return custom, true
		}
		return statuses[idx-1], true
	}
}
