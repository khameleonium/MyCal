package app

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"mycalendar/color"
	"mycalendar/models"
	"mycalendar/parser"
)

// readLine reads one line from stdin. The second result is false once stdin is
// closed (EOF); every interactive loop must stop when it sees that, otherwise a
// closed pipe turns into a busy loop.
func (a *App) readLine() (string, bool) {
	if a.stdinClosed {
		return "", false
	}
	if !a.scanner.Scan() {
		a.stdinClosed = true
		return "", false
	}
	return a.scanner.Text(), true
}

// line reads a trimmed line, treating EOF as empty input.
func (a *App) line() string {
	s, _ := a.readLine()
	return strings.TrimSpace(s)
}

// prompt prints an optional message then "> " and returns the trimmed reply.
func (a *App) prompt(msg string) string {
	if msg != "" {
		fmt.Println(msg)
	}
	fmt.Print("> ")
	return a.line()
}

// promptChoice is prompt plus an EOF signal, for menu loops that must exit
// cleanly when stdin closes.
func (a *App) promptChoice(msg string) (string, bool) {
	if msg != "" {
		fmt.Println(msg)
	}
	fmt.Print("> ")
	s, ok := a.readLine()
	return strings.TrimSpace(s), ok && !a.stdinClosed
}

// confirm asks a yes/no question and returns true only for an explicit yes.
// EOF counts as "no".
func (a *App) confirm(question string) bool {
	if question != "" {
		fmt.Print(question + " ")
	}
	fmt.Print("(Да/Нет) > ")
	return isConfirmWord(a.line())
}

func (a *App) dialogPrompt(title, help string) string {
	if title != "" {
		fmt.Println(title)
	}
	if help != "" {
		fmt.Println(help)
	}
	fmt.Print("> ")
	return a.line()
}

func (a *App) dialogPeriod(title, help string) (time.Time, time.Time, bool) {
	for {
		input := a.dialogPrompt(title, help)
		if isCancelled(input) || a.stdinClosed {
			return time.Time{}, time.Time{}, false
		}
		start, end, err := parser.ParsePeriod(input)
		if err != nil {
			fmt.Println(color.Red(errMark + " " + err.Error()))
			continue
		}
		return start, end, true
	}
}

func (a *App) dialogDate(title, help string) (time.Time, bool) {
	for {
		input := a.dialogPrompt(title, help)
		if isCancelled(input) || a.stdinClosed {
			return time.Time{}, false
		}
		date, err := parser.ParseDate(input, a.resolveDate())
		if err != nil {
			fmt.Println(color.Red(errMark + " Не удалось распознать дату"))
			continue
		}
		if a.cfg.DateCheckMode == models.DateCheckOff {
			return date, true
		}
		if parser.ValidateDate(input, date) {
			return date, true
		}
		if corrected, ok := a.handleBadDate(input, date); ok {
			return corrected, true
		}
		// handleBadDate asked for a re-entry
	}
}

func (a *App) handleBadDate(raw string, corrected time.Time) (time.Time, bool) {
	switch a.cfg.DateCheckMode {
	case models.DateCheckFix:
		fmt.Println(color.Yellow(warnMark + " \"" + raw + "\" скорректирована на " + parser.FormatDate(corrected)))
		return corrected, true
	case models.DateCheckReask:
		fmt.Println(color.Yellow(warnMark + " Некорректная дата, введите заново"))
		return time.Time{}, false
	case models.DateCheckAsk:
		correctedStr := parser.FormatDate(corrected)
		fmt.Printf(color.Yellow("\n"+warnMark+" \"%s\" не является корректной датой.\n"), raw)
		fmt.Printf("    Будет преобразовано в %s\n\n", correctedStr)
		fmt.Println("  1. Принять (" + correctedStr + ")")
		fmt.Println("  2. Ввести другую дату")
		if a.prompt("") != "1" {
			return time.Time{}, false
		}
		if a.confirm(color.Yellow(askMark + " Запомнить выбор?")) {
			a.cfg.DateCheckMode = models.DateCheckFix
		}
		return corrected, true
	}
	return corrected, true
}

func (a *App) dialogDuration(defaultDur int) (int, bool) {
	input := a.dialogPrompt(
		fmt.Sprintf("Продолжительность (мин) [%d]:", defaultDur),
		"Enter — "+strconv.Itoa(defaultDur)+"; 0 — отмена")
	if isCancelled(input) || a.stdinClosed {
		return 0, false
	}
	if input == "" {
		return defaultDur, true
	}
	d, err := strconv.Atoi(input)
	if err != nil || d <= 0 {
		fmt.Println(color.Yellow(warnMark + " Некорректно, используется " + strconv.Itoa(defaultDur)))
		return defaultDur, true
	}
	return d, true
}

func (a *App) askTimeLooped() (time.Time, bool) {
	for {
		input := a.dialogPrompt("Время записи:", "HH:MM, HH MM — обязательно; 0 — отмена")
		if isCancelled(input) || a.stdinClosed {
			return time.Time{}, false
		}
		tm, err := parser.ParseTime(input)
		if errors.Is(err, parser.ErrEmptyTime) {
			fmt.Println(color.Yellow(warnMark + " Время не указано"))
			continue
		}
		if err != nil {
			fmt.Println(color.Red(errMark + " Некорректное время"))
			continue
		}
		return tm, true
	}
}

func (a *App) askID() string {
	for {
		input := a.dialogPrompt("ID записи:", "14 цифр; 0 — отмена")
		if isCancelled(input) || a.stdinClosed {
			return ""
		}
		if len(input) != models.IDLen {
			fmt.Println(color.Red(errMark + " Некорректный ID (должно быть 14 цифр)"))
			continue
		}
		if len(a.svc.FindByID(input)) > 0 {
			return input
		}
		fmt.Println(color.Yellow(warnMark + " Запись с таким ID не найдена"))
	}
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
