package app

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"mycalendar/color"
	"mycalendar/models"
)

// pickTypesMulti prompts the user to select one or more types from allTypes using
// an interactive toggle list with checkboxes and an in-place substring filter.
func (a *App) pickTypesMulti(allTypes []string, title string) ([]string, bool) {
	if len(allTypes) == 0 {
		fmt.Println(color.Yellow(warnMark + " Нет доступных типов записей."))
		custom := a.dialog("Введите название типа для фильтрации (0 — отмена):", "")
		if isCancel(custom) || a.stdinClosed || custom == "" {
			return nil, false
		}
		return []string{custom}, true
	}

	selected := make(map[string]bool, len(allTypes))
	filterText := ""

	for {
		// Compute visible types based on filterText
		q := strings.ToLower(strings.TrimSpace(filterText))
		var visible []string
		for _, t := range allTypes {
			if q == "" || strings.Contains(strings.ToLower(t), q) {
				visible = append(visible, t)
			}
		}

		fmt.Println()
		fmt.Println(color.Yellow(hdrSep + " " + title + " " + hdrSep))

		if len(visible) == 0 {
			fmt.Println(color.Yellow("  (нет типов, содержащих: \"" + filterText + "\")"))
		} else {
			for i, t := range visible {
				box := color.Yellow("[ ]")
				if selected[t] {
					box = color.Green("[✓]")
				}
				fmt.Printf("  %s %2d. %s\n", box, i+1, t)
			}
		}
		fmt.Println("  " + strings.Repeat("─", 44))

		selCount := 0
		for _, t := range allTypes {
			if selected[t] {
				selCount++
			}
		}

		if filterText != "" {
			fmt.Println(color.Cyan(fmt.Sprintf("  Поиск: \"%s\" (найдено %d из %d, выбрано: %d)  ['*' — сбросить поиск]",
				filterText, len(visible), len(allTypes), selCount)))
		} else {
			fmt.Printf("  Выбрано типов: %d из %d\n", selCount, len(allTypes))
		}

		if a.cfg.ShowHints {
			fmt.Println(color.Cyan("  → '1', '1, 3' переключить галочки; 'стрельб' поиск типа; '+' все; '-' снять; Enter готово"))
		}
		a.printPrompt("Номер(а) — переключить [✓]  |  текст — поиск  |  '+' — все  |  Enter — готово (0 — отмена) > ")

		in, ok := a.readLine()
		in = strings.TrimSpace(in)
		if !ok || a.stdinClosed || isCancel(in) {
			return nil, false
		}

		// Done / confirm
		if in == "" || match(in, "ok", "done", "готово", "v", "д") {
			if selCount == 0 {
				fmt.Println(color.Yellow(warnMark + " Ни один тип не выбран. Отметьте номера, '+' для выбора всех, или 0 для отмены."))
				continue
			}
			var res []string
			for _, t := range allTypes {
				if selected[t] {
					res = append(res, t)
				}
			}
			return res, true
		}

		// Quick bulk actions
		if match(in, "+", "all", "все") {
			for _, t := range visible {
				selected[t] = true
			}
			continue
		}
		if match(in, "-", "none", "нет", "снять") {
			for _, t := range visible {
				selected[t] = false
			}
			continue
		}

		// Clear search filter
		if in == "*" || in == "/" || match(in, "clear", "сброс") {
			filterText = ""
			continue
		}

		// Explicit filter syntax: /text
		if strings.HasPrefix(in, "/") {
			filterText = strings.TrimSpace(strings.TrimPrefix(in, "/"))
			if filterText == "*" {
				filterText = ""
			}
			continue
		}

		// Check if input is index selection
		indices, isIndices, err := parseTypeIndices(in, len(visible))
		if isIndices {
			if err != nil {
				fmt.Println(color.Yellow(warnMark + " " + err.Error()))
				continue
			}
			for _, idx := range indices {
				t := visible[idx-1]
				selected[t] = !selected[t]
			}
			continue
		}

		// Otherwise, input contains text/letters: treat as search filter!
		filterText = in
	}
}

// parseTypeIndices parses toggle strings such as "1", "1, 2", "1 3", "1-3", "1 – 3".
// If the string contains letters, it reports isIndices=false so caller treats it as a search query.
func parseTypeIndices(input string, maxLen int) (indices []int, isIndices bool, err error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return nil, false, nil
	}

	// If there are letters, it's definitely a text search filter
	for _, r := range s {
		if unicode.IsLetter(r) {
			return nil, false, nil
		}
	}

	// Normalize dash characters to ASCII '-'
	s = strings.ReplaceAll(s, "–", "-")
	s = strings.ReplaceAll(s, "—", "-")
	for strings.Contains(s, " -") || strings.Contains(s, "- ") {
		s = strings.ReplaceAll(s, " -", "-")
		s = strings.ReplaceAll(s, "- ", "-")
	}

	// Split by commas and whitespace
	tokens := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	})
	if len(tokens) == 0 {
		return nil, false, nil
	}

	seen := map[int]bool{}
	for _, tok := range tokens {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		if strings.Contains(tok, "-") {
			parts := strings.Split(tok, "-")
			if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
				return nil, true, fmt.Errorf("неверный диапазон номеров: %q", tok)
			}
			from, err1 := strconv.Atoi(parts[0])
			to, err2 := strconv.Atoi(parts[1])
			if err1 != nil || err2 != nil {
				return nil, true, fmt.Errorf("неверный диапазон номеров: %q", tok)
			}
			if from > to {
				from, to = to, from
			}
			for i := from; i <= to; i++ {
				if i < 1 || (maxLen > 0 && i > maxLen) {
					return nil, true, fmt.Errorf("номер %d вне диапазона (1–%d)", i, maxLen)
				}
				if !seen[i] {
					seen[i] = true
					indices = append(indices, i)
				}
			}
		} else {
			val, err := strconv.Atoi(tok)
			if err != nil {
				return nil, true, fmt.Errorf("не число: %q", tok)
			}
			if val < 1 || (maxLen > 0 && val > maxLen) {
				return nil, true, fmt.Errorf("номер %d вне диапазона (1–%d)", val, maxLen)
			}
			if !seen[val] {
				seen[val] = true
				indices = append(indices, val)
			}
		}
	}

	return indices, true, nil
}

// filterSessionsByTypes returns only the sessions whose Type matches any of the
// requested types (case-insensitive). If "(без типа)" is requested, sessions with
// empty Type are also included.
func filterSessionsByTypes(sessions []models.Session, types []string) []models.Session {
	if len(types) == 0 {
		return sessions
	}

	matchEmpty := false
	typeSet := make(map[string]bool, len(types))
	for _, t := range types {
		t = strings.TrimSpace(t)
		if t == "(без типа)" || t == "" {
			matchEmpty = true
		} else {
			typeSet[strings.ToLower(t)] = true
		}
	}

	var out []models.Session
	for _, s := range sessions {
		st := strings.TrimSpace(s.Type)
		if st == "" {
			if matchEmpty {
				out = append(out, s)
			}
			continue
		}
		if typeSet[strings.ToLower(st)] {
			out = append(out, s)
		}
	}
	return out
}

// calendarTypes returns all unique types available in the calendar and default types,
// sorted alphabetically. If includeEmpty is true and there are sessions with an empty type,
// "(без типа)" is appended to the list.
func (a *App) calendarTypes(includeEmpty bool) []string {
	seen := map[string]bool{}
	var out []string
	hasEmpty := false

	for _, s := range a.svc.All() {
		t := strings.TrimSpace(s.Type)
		if t == "" {
			hasEmpty = true
			continue
		}
		if !seen[strings.ToLower(t)] {
			seen[strings.ToLower(t)] = true
			out = append(out, t)
		}
	}

	defaults := a.svc.Types(a.cfg.DefaultType)
	for _, dt := range defaults {
		dt = strings.TrimSpace(dt)
		if dt != "" && !seen[strings.ToLower(dt)] {
			seen[strings.ToLower(dt)] = true
			out = append(out, dt)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i]) < strings.ToLower(out[j])
	})

	if includeEmpty && hasEmpty {
		out = append(out, "(без типа)")
	}
	return out
}

// existingTypes returns only the types that are actually used in the given sessions.
func (a *App) existingTypes(sessions []models.Session) []string {
	seen := map[string]bool{}
	var out []string
	hasEmpty := false

	for _, s := range sessions {
		t := strings.TrimSpace(s.Type)
		if t == "" {
			hasEmpty = true
			continue
		}
		if !seen[strings.ToLower(t)] {
			seen[strings.ToLower(t)] = true
			out = append(out, t)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i]) < strings.ToLower(out[j])
	})

	if hasEmpty {
		out = append(out, "(без типа)")
	}
	return out
}

// formatTypesLabel appends a compact summary of selected types to the base label.
func formatTypesLabel(baseLabel string, types []string) string {
	if len(types) == 0 {
		return baseLabel
	}
	var tag string
	if len(types) == 1 {
		tag = "Тип: " + types[0]
	} else if len(types) <= 3 {
		tag = "Типы: " + strings.Join(types, ", ")
	} else {
		tag = fmt.Sprintf("Выбрано типов: %d", len(types))
	}

	if baseLabel == "" {
		return tag
	}
	return baseLabel + " [" + tag + "]"
}

// extractTypeArg extracts --type, -t, --тип, -т from CLI args.
func extractTypeArg(args []string) (typeVal string, remaining []string) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--type" || arg == "-t" || arg == "--тип" || arg == "-т" {
			if i+1 < len(args) {
				typeVal = args[i+1]
				i++
				continue
			}
		} else if strings.HasPrefix(arg, "--type=") || strings.HasPrefix(arg, "-t=") ||
			strings.HasPrefix(arg, "--тип=") || strings.HasPrefix(arg, "-т=") {
			parts := strings.SplitN(arg, "=", 2)
			typeVal = parts[1]
			continue
		}
		remaining = append(remaining, arg)
	}
	return strings.TrimSpace(typeVal), remaining
}

// trimPrefixWord checks if s starts with any of prefixes followed by a delimiter (space, colon, '=').
func trimPrefixWord(s string, prefixes ...string) (string, bool) {
	lower := strings.ToLower(strings.TrimSpace(s))
	for _, p := range prefixes {
		pLower := strings.ToLower(p)
		if strings.HasPrefix(lower, pLower) {
			rest := strings.TrimSpace(s)[len(p):]
			if strings.HasPrefix(rest, " ") || strings.HasPrefix(rest, ":") || strings.HasPrefix(rest, "=") {
				return strings.TrimSpace(rest[1:]), true
			}
		}
	}
	return "", false
}
