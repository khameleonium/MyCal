package app

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"mycalendar/color"
	"mycalendar/models"
	"mycalendar/parser"
)

// readLine reads one line from stdin. ok is false once stdin is closed (EOF);
// every interactive loop must stop when it sees that, or a closed pipe becomes
// a busy loop.
func (a *App) readLine() (string, bool) {
	if a.stdinClosed {
		return "", false
	}
	s, err := a.in.ReadString('\n')
	if err != nil && s == "" {
		a.stdinClosed = true
		return "", false
	}
	if err != nil && !errors.Is(err, io.EOF) {
		a.stdinClosed = true
	}
	return strings.TrimRight(s, "\r\n"), true
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

// menuChoice is prompt plus an EOF signal, for menu loops that must exit
// cleanly when stdin closes.
func (a *App) menuChoice() (string, bool) {
	fmt.Print("> ")
	s, ok := a.readLine()
	return strings.TrimSpace(s), ok && !a.stdinClosed
}

// confirm asks a yes/no question; only an explicit yes returns true. EOF = no.
func (a *App) confirm(question string) bool {
	if question != "" {
		fmt.Print(question + " ")
	}
	fmt.Print("(Да/Нет) > ")
	return isYes(a.line())
}

func (a *App) dialog(title, help string) string {
	if title != "" {
		fmt.Println(title)
	}
	if help != "" {
		fmt.Println(help)
	}
	fmt.Print("> ")
	return a.line()
}

func (a *App) askPeriod(title, help string) (start, end time.Time, ok bool) {
	for {
		in := a.dialog(title, help)
		if isCancel(in) || a.stdinClosed {
			return time.Time{}, time.Time{}, false
		}
		s, e, err := parser.ParsePeriod(in)
		if err != nil {
			fmt.Println(color.Red(errMark + " " + err.Error()))
			continue
		}
		return s, e, true
	}
}

func (a *App) askDate(title, help string) (time.Time, bool) {
	for {
		in := a.dialog(title, help)
		if isCancel(in) || a.stdinClosed {
			return time.Time{}, false
		}
		date, err := parser.ParseDate(in, a.today())
		if err != nil {
			fmt.Println(color.Red(errMark + " Не удалось распознать дату"))
			continue
		}
		if a.cfg.DateCheckMode == models.DateCheckOff || parser.ValidateDate(in, date) {
			return date, true
		}
		if corrected, done := a.handleBadDate(in, date); done {
			return corrected, true
		}
	}
}

func (a *App) handleBadDate(raw string, corrected time.Time) (time.Time, bool) {
	switch a.cfg.DateCheckMode {
	case models.DateCheckFix:
		fmt.Println(color.Yellow(warnMark + " \"" + raw + "\" исправлена на " + parser.FormatDate(corrected)))
		return corrected, true
	case models.DateCheckReask:
		fmt.Println(color.Yellow(warnMark + " Некорректная дата, введите заново"))
		return time.Time{}, false
	case models.DateCheckAsk:
		s := parser.FormatDate(corrected)
		fmt.Printf(color.Yellow("\n"+warnMark+" \"%s\" — некорректная дата.\n"), raw)
		fmt.Printf("    Будет преобразовано в %s\n\n", s)
		fmt.Println("  1. Принять (" + s + ")")
		fmt.Println("  2. Ввести другую дату")
		if a.prompt("") != "1" {
			return time.Time{}, false
		}
		if a.confirm(color.Yellow(askMark + " Запомнить выбор и впредь исправлять автоматически?")) {
			a.cfg.DateCheckMode = models.DateCheckFix
		}
		return corrected, true
	}
	return corrected, true
}

func (a *App) askDuration(def int) (int, bool) {
	in := a.dialog(
		fmt.Sprintf("Продолжительность (мин) [%d]:", def),
		"Enter — "+strconv.Itoa(def)+"; 0 — отмена")
	if isCancel(in) || a.stdinClosed {
		return 0, false
	}
	if in == "" {
		return def, true
	}
	d, err := strconv.Atoi(in)
	if err != nil || d <= 0 {
		fmt.Println(color.Yellow(warnMark + " Некорректно, используется " + strconv.Itoa(def)))
		return def, true
	}
	return d, true
}

func (a *App) askTime() (time.Time, bool) {
	for {
		in := a.dialog("Время записи:", "HH:MM, 930, 9 — обязательно; 0 — отмена")
		if isCancel(in) || a.stdinClosed {
			return time.Time{}, false
		}
		tm, err := parser.ParseTime(in)
		if errors.Is(err, parser.ErrEmptyTime) {
			fmt.Println(color.Yellow(warnMark + " Время не указано"))
			continue
		}
		if err != nil {
			fmt.Println(color.Red(errMark + " " + err.Error()))
			continue
		}
		return tm, true
	}
}
