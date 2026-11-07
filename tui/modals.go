package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"mycalendar/calendar"
	"mycalendar/config"
	"mycalendar/models"
	"mycalendar/parser"
	"mycalendar/service"
	"mycalendar/storage"
)

// Modal represents a floating dialog overlay on top of the main screen.
type Modal interface {
	Draw(s *Screen)
	HandleKey(k Key, app *App) bool // returns true if modal was closed or consumed
}

// ----------------- STATUS PICKER MODAL -----------------

type ModalStatusPicker struct {
	statuses      []string
	selected      int
	targetID      string // empty if batch
	isCustomInput bool
	customInput   string
}

func NewModalStatusPicker(customStatuses []string, currentStatus, targetID string) *ModalStatusPicker {
	defaults := []string{"Активно", "Завершено", "В процессе", "Запланировано", "Пропущено", "Отменено"}
	var list []string
	seen := map[string]bool{}
	for _, st := range customStatuses {
		stTrim := strings.TrimSpace(st)
		if stTrim != "" && !strings.EqualFold(stTrim, calendar.EmptyStatusLabel) && !seen[stTrim] {
			list = append(list, stTrim)
			seen[stTrim] = true
		}
	}
	for _, st := range defaults {
		if !seen[st] {
			list = append(list, st)
			seen[st] = true
		}
	}
	list = append(list, calendar.EmptyStatusLabel)

	sel := 0
	normCur := calendar.FormatStatusDisplay(currentStatus)
	for i, st := range list {
		if strings.EqualFold(st, normCur) {
			sel = i
			break
		}
	}

	return &ModalStatusPicker{
		statuses: list,
		selected: sel,
		targetID: targetID,
	}
}

func (m *ModalStatusPicker) Draw(s *Screen) {
	if m.isCustomInput {
		w := 44
		h := 9
		if w > s.Width-2 {
			w = s.Width - 2
		}
		if h > s.Height-2 {
			h = s.Height - 2
		}
		x := (s.Width - w) / 2
		y := (s.Height - h) / 2
		if x < 1 {
			x = 1
		}
		if y < 1 {
			y = 1
		}

		s.DrawBox(x, y, w, h, "Новый статус", FgCyan, BorderBright, BgModal, true)
		s.DrawText(x+2, y+2, "Введите название статуса:", FgMuted, BgModal, w-4, false, false, false)

		s.Fill(x+2, y+4, w-4, 1, ' ', FgWhite, BgInput)
		s.DrawBox(x+1, y+3, w-2, 3, "", FgDim, BorderFocus, BgInput, false)
		s.DrawText(x+3, y+4, m.customInput+"█", FgWhite, BgInput, w-6, true, false, false)

		s.DrawBottomWrappedHint(x+2, y+h-2, w-4, 2, "[Enter] Сохранить и применить  [Esc] Отмена", FgDim, BgModal)
		return
	}

	totalItems := len(m.statuses) + 1
	w := 38
	h := totalItems + 6
	if w > s.Width-2 {
		w = s.Width - 2
	}
	if h > s.Height-2 {
		h = s.Height - 2
	}
	x := (s.Width - w) / 2
	y := (s.Height - h) / 2
	if x < 1 {
		x = 1
	}
	if y < 1 {
		y = 1
	}

	s.DrawBox(x, y, w, h, "Смена статуса", FgBlue, BorderBright, BgModal, true)

	s.DrawText(x+2, y+1, "Выберите новый статус:", FgMuted, BgModal, w-4, false, false, false)

	for i, st := range m.statuses {
		rowY := y + 3 + i
		isCur := i == m.selected
		bg := BgModal
		fg := FgMain
		prefix := "  "
		if isCur {
			bg = BgSelected
			fg = FgWhite
			prefix = "► "
		}

		s.Fill(x+2, rowY, w-4, 1, ' ', fg, bg)
		numStr := fmt.Sprintf("[%d] ", i+1)
		s.DrawText(x+2, rowY, prefix+numStr, FgCyan, bg, 8, isCur, false, false)

		badgeText, bFg, _ := StatusBadge(st)
		s.DrawText(x+8, rowY, badgeText, bFg, bg, w-10, true, false, false)
	}

	// Option to enter a custom status
	customIdx := len(m.statuses)
	customY := y + 3 + customIdx
	isCustomCur := m.selected == customIdx
	customBg := BgModal
	customFg := FgYellow
	customPrefix := "  "
	if isCustomCur {
		customBg = BgSelected
		customFg = FgWhite
		customPrefix = "► "
	}
	s.Fill(x+2, customY, w-4, 1, ' ', customFg, customBg)
	s.DrawText(x+2, customY, customPrefix+"+ Свой статус...", customFg, customBg, w-4, isCustomCur, false, false)

	hint := fmt.Sprintf("[↑↓/1-%d] Выбор  [+] Добавить  [d] Удалить  [Enter]  [Esc]", len(m.statuses))
	s.DrawBottomWrappedHint(x+2, y+h-2, w-4, 2, hint, FgDim, BgModal)
}

func (m *ModalStatusPicker) HandleKey(k Key, app *App) bool {
	if m.isCustomInput {
		switch {
		case k.Type == KeyEsc:
			m.isCustomInput = false
			return true

		case k.Type == KeyEnter:
			val := strings.TrimSpace(m.customInput)
			if val != "" {
				if app.cfg != nil {
					app.cfg.AddCustomStatus(val)
					app.persistConfig()
				}
				m.applyStatus(app, val)
				app.activeModal = nil
			} else {
				m.isCustomInput = false
			}
			return true

		case k.Type == KeyBackspace:
			if len(m.customInput) > 0 {
				runes := []rune(m.customInput)
				m.customInput = string(runes[:len(runes)-1])
			}
			return true

		case k.Type == KeySpace:
			m.customInput += " "
			return true

		case k.Type == KeyRune:
			m.customInput += string(k.Rune)
			return true
		}
		return true
	}

	switch {
	case k.Type == KeyEsc:
		app.activeModal = nil
		return true

	case k.Matches('d', 'в') || k.Type == KeyDelete:
		if m.selected < len(m.statuses) {
			st := m.statuses[m.selected]
			if st == calendar.EmptyStatusLabel {
				return true
			}
			if app.cfg != nil {
				app.cfg.RemoveStatus(st)
				app.persistConfig()
			}
			app.setStatusMsg("Статус «" + st + "» удалён из списков выбора")
			m.statuses = append(app.getAvailableStatuses(), calendar.EmptyStatusLabel)
			if m.selected >= len(m.statuses) {
				m.selected = len(m.statuses)
			}
			return true
		}

	case k.Matches('+', '='):
		m.isCustomInput = true
		m.customInput = ""
		return true

	case k.Type == KeyUp || k.Matches('k', 'л'):
		if m.selected > 0 {
			m.selected--
		} else {
			m.selected = len(m.statuses) // wrap to [+] Свой статус
		}
		return true

	case k.Type == KeyDown || k.Matches('j', 'о'):
		if m.selected < len(m.statuses) {
			m.selected++
		} else {
			m.selected = 0
		}
		return true

	case k.Type == KeyEnter:
		if m.selected == len(m.statuses) {
			m.isCustomInput = true
			m.customInput = ""
			return true
		}
		m.applyStatus(app, m.statuses[m.selected])
		app.activeModal = nil
		return true

	case k.Type == KeyRune && k.Rune == '0':
		m.applyStatus(app, calendar.EmptyStatusLabel)
		app.activeModal = nil
		return true

	case k.Type == KeyRune && k.Rune >= '1' && k.Rune <= '9':
		idx := int(k.Rune - '1')
		if idx < len(m.statuses) {
			m.applyStatus(app, m.statuses[idx])
			app.activeModal = nil
			return true
		}
	}
	return false
}

func (m *ModalStatusPicker) applyStatus(app *App, status string) {
	status = calendar.NormalizeStatus(status)
	if app.cfg != nil && status != "" {
		app.cfg.AddCustomStatus(status)
		app.persistConfig()
	}

	if m.targetID != "" {
		// Single session update
		patch := calendar.Patch{
			Status: &status,
		}
		_ = app.svc.Update(m.targetID, patch)
		if err := app.svc.Save(app.ctx); err != nil {
			app.setStatusMsg("⚠ Ошибка сохранения: " + err.Error())
		} else {
			app.lastDataMod = app.svc.DataFilesModTime()
			app.setStatusMsg("Статус записи изменён на: " + calendar.FormatStatusDisplay(status))
		}
	} else if len(app.selectedIDs) > 0 {
		// Batch update
		app.svc.BeginUndoTx("групповая смена статуса")
		count := 0
		for id := range app.selectedIDs {
			patch := calendar.Patch{
				Status: &status,
			}
			if err := app.svc.Update(id, patch); err == nil {
				count++
			}
		}
		app.svc.EndUndoTx()
		if err := app.svc.Save(app.ctx); err != nil {
			app.setStatusMsg("⚠ Ошибка сохранения: " + err.Error())
		} else {
			app.lastDataMod = app.svc.DataFilesModTime()
			app.setStatusMsg(fmt.Sprintf("Статус обновлён для %d записей (%s)", count, calendar.FormatStatusDisplay(status)))
		}
	}
	app.reloadSessions()
}

// ----------------- QUICK FIELD EDIT MODAL -----------------

type ModalQuickEditField struct {
	fields    []string
	selected  int
	stepInput bool // false: choosing field, true: typing value
	value     string
	targetID  string

	pickerOpen   bool
	pickerField  int // 0: Name, 2: Type, 3: Status
	pickerIdx    int
	pickerScroll int
	pickerList   []string
	pickerTitle  string
}

func NewModalQuickEditField(targetID string) *ModalQuickEditField {
	return &ModalQuickEditField{
		fields: []string{
			"Наименование",
			"Кэш",
			"Категория / Тип",
			"Статус",
			"Время (ЧЧ:ММ)",
			"Длительность (мин)",
			"Дата (ДД-ММ-ГГГГ)",
			"Комментарий / Заметки",
		},
		selected:  0,
		targetID:  targetID,
		stepInput: false,
	}
}

func (m *ModalQuickEditField) Draw(s *Screen) {
	w := 50
	h := len(m.fields) + 7
	if w > s.Width-2 {
		w = s.Width - 2
	}
	if h > s.Height-2 {
		h = s.Height - 2
	}
	x := (s.Width - w) / 2
	y := (s.Height - h) / 2
	if x < 1 {
		x = 1
	}
	if y < 1 {
		y = 1
	}

	title := "Редактировать поле"
	if m.targetID == "" {
		title = "Групповое изменение поля"
	}
	s.DrawBox(x, y, w, h, title, FgYellow, BorderBright, BgModal, true)

	if !m.stepInput {
		s.DrawText(x+2, y+1, "Выберите поле для изменения:", FgMuted, BgModal, w-4, false, false, false)
		for i, f := range m.fields {
			rowY := y + 3 + i
			isCur := i == m.selected
			bg := BgModal
			fg := FgMain
			prefix := "  "
			if isCur {
				bg = BgSelected
				fg = FgWhite
				prefix = "► "
			}
			s.Fill(x+2, rowY, w-4, 1, ' ', fg, bg)
			s.DrawText(x+2, rowY, fmt.Sprintf("%s[%d] %s", prefix, i+1, f), fg, bg, w-4, isCur, false, false)
		}
		s.DrawBottomWrappedHint(x+2, y+h-2, w-4, 2, fmt.Sprintf("[↑↓/1-%d] Выбор  [Enter] Далее  [Esc Отмена]", len(m.fields)), FgDim, BgModal)
	} else {
		fieldName := m.fields[m.selected]
		s.DrawText(x+2, y+2, "Поле: "+fieldName, FgCyan, BgModal, w-4, true, false, false)
		s.DrawText(x+2, y+4, "Введите новое значение:", FgMuted, BgModal, w-4, false, false, false)

		// Input box
		s.Fill(x+2, y+6, w-4, 1, ' ', FgWhite, BgInput)
		s.DrawBox(x+1, y+5, w-2, 3, "", FgDim, BorderFocus, BgInput, false)
		inputDisplay := m.value + "█"
		s.DrawText(x+3, y+6, inputDisplay, FgWhite, BgInput, w-6, true, false, false)
		canHaveDropdown := m.selected == 0 || m.selected == 2 || m.selected == 3 || m.selected == 4
		if canHaveDropdown && !m.pickerOpen && w > 20 {
			s.DrawText(x+w-12, y+6, "[→ список]", FgCyan, BgInput, 10, true, false, false)
		}

		if m.pickerOpen {
			s.DrawBottomWrappedHint(x+2, y+h-2, w-4, 2, "[↑↓] Выбор  [d] Удалить  [Enter] Применить  [Esc/←] Ввести вручную", FgDim, BgModal)
		} else if canHaveDropdown {
			s.DrawBottomWrappedHint(x+2, y+h-2, w-4, 2, "[Ввод текста] Новое значение  [→] Выбрать из списка  [Enter] Сохранить  [Esc] Назад", FgDim, BgModal)
		} else {
			s.DrawBottomWrappedHint(x+2, y+h-2, w-4, 2, "[Ввод текста] Новое значение  [Enter] Сохранить  [Esc] Назад", FgDim, BgModal)
		}

		if m.pickerOpen && len(m.pickerList) > 0 {
			popX := x + 2
			popY := y + 8
			popW := w - 4
			visibleRows := 5
			if len(m.pickerList) < visibleRows {
				visibleRows = len(m.pickerList)
			}
			popH := visibleRows + 2
			if popY+popH > y+h-1 || popY+popH > s.Height-1 {
				popY = (y + 5) - popH
				if popY < y+1 {
					popY = y + 1
				}
			}

			s.DrawBox(popX, popY, popW, popH, m.pickerTitle, FgCyan, BorderFocus, BgCard, true)
			for i := 0; i < visibleRows; i++ {
				itemIdx := m.pickerScroll + i
				if itemIdx >= len(m.pickerList) {
					break
				}
				item := m.pickerList[itemIdx]
				itemY := popY + 1 + i
				isSel := itemIdx == m.pickerIdx

				bg := BgCard
				fg := FgWhite
				prefix := "  "
				if isSel {
					bg = BgHighlight
					fg = FgWhite
					prefix = "► "
				}
				s.Fill(popX+1, itemY, popW-2, 1, ' ', fg, bg)
				if m.pickerField == 3 { // Status
					badgeText, bFg, _ := StatusBadge(item)
					s.DrawText(popX+1, itemY, prefix+badgeText, bFg, bg, popW-2, isSel, false, false)
				} else {
					s.DrawText(popX+1, itemY, prefix+item, fg, bg, popW-2, isSel, false, false)
				}
			}
		}
	}
}

func (m *ModalQuickEditField) HandleKey(k Key, app *App) bool {
	if !m.stepInput {
		switch {
		case k.Type == KeyEsc:
			app.activeModal = nil
			return true
		case k.Type == KeyUp || k.Matches('k', 'л'):
			if m.selected > 0 {
				m.selected--
			} else {
				m.selected = len(m.fields) - 1
			}
			return true
		case k.Type == KeyDown || k.Matches('j', 'о'):
			if m.selected < len(m.fields)-1 {
				m.selected++
			} else {
				m.selected = 0
			}
			return true
		case k.Type == KeyEnter:
			m.initValue(app)
			m.stepInput = true
			return true
		case k.Type == KeyRune && k.Rune >= '1' && k.Rune <= rune('0'+len(m.fields)):
			m.selected = int(k.Rune - '1')
			m.initValue(app)
			m.stepInput = true
			return true
		}
	} else {
		if m.pickerOpen {
			switch {
			case k.Type == KeyEsc || k.Type == KeyLeft:
				m.pickerOpen = false
				return true
			case k.Type == KeyUp || k.Matches('k', 'л'):
				if m.pickerIdx > 0 {
					m.pickerIdx--
					if m.pickerIdx < m.pickerScroll {
						m.pickerScroll = m.pickerIdx
					}
				}
				return true
			case k.Type == KeyDown || k.Matches('j', 'о'):
				if m.pickerIdx < len(m.pickerList)-1 {
					m.pickerIdx++
					visibleRows := 5
					if m.pickerIdx >= m.pickerScroll+visibleRows {
						m.pickerScroll = m.pickerIdx - visibleRows + 1
					}
				}
				return true
			case k.Matches('d', 'в') || k.Type == KeyDelete:
				if m.pickerIdx >= 0 && m.pickerIdx < len(m.pickerList) {
					item := m.pickerList[m.pickerIdx]
					if m.pickerField == 2 { // Category / Type
						if app.cfg != nil {
							app.cfg.RemoveType(item)
							app.persistConfig()
						}
						app.setStatusMsg("Тип «" + item + "» удалён из списков выбора")
						m.openCatPicker(app)
					} else if m.pickerField == 3 { // Status
						if item == calendar.EmptyStatusLabel {
							return true
						}
						if app.cfg != nil {
							app.cfg.RemoveStatus(item)
							app.persistConfig()
						}
						app.setStatusMsg("Статус «" + item + "» удалён из списков выбора")
						m.openStatusPicker(app)
					}
				}
				return true
			case k.Type == KeyEnter || k.Type == KeySpace:
				if m.pickerIdx >= 0 && m.pickerIdx < len(m.pickerList) {
					m.value = m.pickerList[m.pickerIdx]
				}
				m.pickerOpen = false
				return true
			}
			return true
		}

		switch k.Type {
		case KeyEsc:
			m.stepInput = false
			return true
		case KeyRight, KeyF2:
			if m.selected == 0 {
				m.openNamePicker(app)
				return true
			} else if m.selected == 2 {
				m.openCatPicker(app)
				return true
			} else if m.selected == 3 {
				m.openStatusPicker(app)
				return true
			} else if m.selected == 4 {
				m.openTimePicker()
				return true
			}
		case KeyEnter:
			m.applyValue(app)
			app.activeModal = nil
			return true
		case KeyBackspace:
			if len(m.value) > 0 {
				runes := []rune(m.value)
				m.value = string(runes[:len(runes)-1])
			}
			return true
		case KeySpace:
			m.value += " "
			return true
		case KeyRune:
			m.value += string(k.Rune)
			return true
		}
	}
	return false
}

func (m *ModalQuickEditField) openTimePicker() {
	var list []string
	for h := 0; h < 24; h++ {
		list = append(list, fmt.Sprintf("%02d:00", h))
	}
	m.pickerList = list
	m.pickerTitle = " Время [Enter] "
	m.pickerField = 4
	m.pickerOpen = true
	m.pickerScroll = 0
	m.pickerIdx = 10

	cur := strings.TrimSpace(m.value)
	curHour := -1
	if parts := strings.Split(cur, ":"); len(parts) >= 1 {
		if h, err := strconv.Atoi(parts[0]); err == nil && h >= 0 && h < 24 {
			curHour = h
		}
	}
	for i, item := range m.pickerList {
		if strings.EqualFold(item, cur) || (curHour >= 0 && i == curHour) {
			m.pickerIdx = i
			if m.pickerIdx >= 5 {
				m.pickerScroll = m.pickerIdx - 4
			}
			break
		}
	}
}

func (m *ModalQuickEditField) openCatPicker(app *App) {
	var list []string
	seen := map[string]bool{}

	if app != nil {
		for _, t := range app.getAvailableTypes() {
			t = strings.TrimSpace(t)
			if t != "" && !seen[t] {
				seen[t] = true
				list = append(list, t)
			}
		}
	}

	m.pickerList = list
	m.pickerTitle = " Категории [Enter] "
	m.pickerField = 2
	m.pickerOpen = true
	m.pickerScroll = 0
	m.pickerIdx = 0

	cur := strings.TrimSpace(m.value)
	for i, item := range m.pickerList {
		if strings.EqualFold(item, cur) {
			m.pickerIdx = i
			if m.pickerIdx >= 5 {
				m.pickerScroll = m.pickerIdx - 4
			}
			break
		}
	}
}

func (m *ModalQuickEditField) openStatusPicker(app *App) {
	var list []string
	if app != nil {
		list = append([]string{}, app.getAvailableStatuses()...)
	} else {
		list = []string{"Активно", "Завершено", "В процессе", "Запланировано", "Пропущено", "Отменено"}
	}
	hasEmpty := false
	for _, st := range list {
		if strings.EqualFold(st, calendar.EmptyStatusLabel) {
			hasEmpty = true
			break
		}
	}
	if !hasEmpty {
		list = append(list, calendar.EmptyStatusLabel)
	}
	m.pickerList = list
	m.pickerTitle = " Статусы [Enter] "
	m.pickerField = 3
	m.pickerOpen = true
	m.pickerScroll = 0
	m.pickerIdx = 0

	cur := calendar.FormatStatusDisplay(m.value)
	for i, item := range m.pickerList {
		if strings.EqualFold(item, cur) {
			m.pickerIdx = i
			if m.pickerIdx >= 5 {
				m.pickerScroll = m.pickerIdx - 4
			}
			break
		}
	}
}

func (m *ModalQuickEditField) openNamePicker(app *App) {
	var list []string
	if app != nil {
		list = app.getAvailableNames()
	}
	if len(list) == 0 {
		return
	}
	m.pickerList = list
	m.pickerTitle = " Частые имена [Enter] "
	m.pickerField = 0
	m.pickerOpen = true
	m.pickerScroll = 0
	m.pickerIdx = 0

	cur := strings.TrimSpace(m.value)
	for i, item := range m.pickerList {
		if strings.EqualFold(item, cur) {
			m.pickerIdx = i
			if m.pickerIdx >= 5 {
				m.pickerScroll = m.pickerIdx - 4
			}
			break
		}
	}
}

func (m *ModalQuickEditField) initValue(app *App) {
	if m.targetID != "" {
		if sess, ok := app.svc.ByID(m.targetID); ok {
			switch m.selected {
			case 0: // Наименование
				m.value = sess.Name
			case 1: // Кэш
				m.value = strconv.FormatFloat(sess.Cash, 'f', -1, 64)
			case 2: // Тип
				m.value = sess.Type
			case 3: // Статус
				m.value = sess.Status
			case 4: // Время
				m.value = sess.Time
			case 5: // Длительность
				m.value = strconv.Itoa(sess.Duration)
			case 6: // Дата
				m.value = sess.Date()
			case 7: // Заметки
				m.value = sess.Notes
			}
			return
		}
	}
	m.value = ""
}

func (m *ModalQuickEditField) applyValue(app *App) {
	val := strings.TrimSpace(m.value)
	var patch calendar.Patch
	var newDatePtr *time.Time

	switch m.selected {
	case 0: // Name
		patch.Name = &val
	case 1: // Cash
		if c, err := calendar.ParseCash(val); err == nil {
			patch.Cash = &c
		} else {
			zero := 0.0
			patch.Cash = &zero
		}
	case 2: // Type
		patch.Type = &val
	case 3: // Status
		normSt := calendar.NormalizeStatus(val)
		patch.Status = &normSt
	case 4: // Time
		if tm, err := parser.ParseTime(val); err == nil {
			tStr := parser.FormatTime(tm)
			patch.Time = &tStr
		} else {
			app.setStatusMsg("⚠ Неверный формат времени (ожидается ЧЧ:ММ)")
			return
		}
	case 5: // Duration
		if d, err := strconv.Atoi(val); err == nil && d > 0 {
			patch.Duration = &d
		} else {
			app.setStatusMsg("⚠ Длительность должна быть больше 0")
			return
		}
	case 6: // Date
		if d, err := parser.ParseDate(val, app.today()); err == nil {
			newDatePtr = &d
		} else {
			app.setStatusMsg("⚠ Неверный формат даты")
			return
		}
	case 7: // Notes
		patch.Notes = &val
	}

	if m.targetID != "" {
		if _, _, err := app.svc.UpdateEntry(app.ctx, app.cfg, m.targetID, calendar.UpdateInput{
			Patch:   patch,
			NewDate: newDatePtr,
		}); err != nil {
			app.setStatusMsg("⚠ Ошибка сохранения: " + err.Error())
		} else {
			app.lastDataMod = app.svc.DataFilesModTime()
			app.persistConfig()
			app.setStatusMsg("Поле успешно обновлено")
		}
	} else if len(app.selectedIDs) > 0 {
		if app.cfg != nil {
			if patch.Type != nil && strings.TrimSpace(*patch.Type) != "" {
				app.cfg.AddCustomType(*patch.Type)
			}
			if patch.Status != nil && *patch.Status != "" {
				app.cfg.AddCustomStatus(*patch.Status)
			}
		}
		app.svc.BeginUndoTx("групповое изменение поля")
		count := 0
		for id := range app.selectedIDs {
			if newDatePtr != nil {
				sess, ok := app.svc.ByID(id)
				if !ok {
					continue
				}
				tm, err := parser.ParseTime(sess.Time)
				if err != nil {
					tm, _ = parser.ParseTime("10:00")
				}
				_ = app.svc.Delete(id)
				sess.ID = app.svc.GenerateID(*newDatePtr, tm)
				if err := app.svc.Add(sess); err == nil {
					count++
				}
			} else {
				if err := app.svc.Update(id, patch); err == nil {
					count++
				}
			}
		}
		app.svc.EndUndoTx()
		saveErr := app.svc.Save(app.ctx)
		app.lastDataMod = app.svc.DataFilesModTime()
		app.persistConfig()
		app.selectedIDs = map[string]bool{}
		if saveErr != nil {
			app.setStatusMsg("⚠ Ошибка сохранения: " + saveErr.Error())
		} else {
			app.setStatusMsg(fmt.Sprintf("Поле обновлено для %d записей", count))
		}
	}
	app.reloadSessions()
}

// ----------------- FULL SESSION FORM MODAL (Add / Edit) -----------------

type ModalSessionForm struct {
	isEdit    bool
	targetID  string
	dateStr   string
	timeStr   string
	name      string
	typeStr   string
	durStr    string
	cashStr   string
	status    string
	notes     string
	repeatStr string // "none", "d", "w", "m"
	fieldIdx  int    // 0..8
	appRef    *App

	pickerOpen   bool
	pickerField  int // 2: Name, 3: Type, 6: Status, 8: Repeat
	pickerIdx    int
	pickerScroll int
	pickerList   []string
	pickerTitle  string
}

func NewModalSessionForm(sess *models.Session, initialDate string) *ModalSessionForm {
	if sess != nil {
		dateStr := sess.Date()
		if parsed, err := parser.ParseDate(sess.Date(), time.Now()); err == nil {
			dateStr = fmt.Sprintf("%02d.%02d.%04d", parsed.Day(), parsed.Month(), parsed.Year())
		}
		return &ModalSessionForm{
			isEdit:    true,
			targetID:  sess.ID,
			dateStr:   dateStr,
			timeStr:   sess.Time,
			name:      sess.Name,
			typeStr:   sess.Type,
			durStr:    strconv.Itoa(sess.Duration),
			cashStr:   strconv.FormatFloat(sess.Cash, 'f', -1, 64),
			status:    calendar.FormatStatusDisplay(sess.Status),
			notes:     sess.Notes,
			repeatStr: "none",
			fieldIdx:  2, // focus name by default
		}
	}

	dStr := initialDate
	if dStr != "" {
		if parsed, err := parser.ParseDate(dStr, time.Now()); err == nil {
			dStr = fmt.Sprintf("%02d.%02d.%04d", parsed.Day(), parsed.Month(), parsed.Year())
		}
	} else {
		now := time.Now()
		dStr = fmt.Sprintf("%02d.%02d.%04d", now.Day(), now.Month(), now.Year())
	}

	return &ModalSessionForm{
		isEdit:    false,
		dateStr:   dStr,
		timeStr:   "10:00",
		name:      "",
		typeStr:   "Стрельба из лука",
		durStr:    "60",
		cashStr:   "0.0",
		status:    "Активно",
		notes:     "",
		repeatStr: "none",
		fieldIdx:  2,
	}
}

func NewModalCloneSessionForm(sess *models.Session, initialDate string) *ModalSessionForm {
	if sess == nil {
		return NewModalSessionForm(nil, initialDate)
	}
	dStr := initialDate
	if dStr == "" {
		dStr = sess.Date()
	}
	if parsed, err := parser.ParseDate(dStr, time.Now()); err == nil {
		dStr = fmt.Sprintf("%02d.%02d.%04d", parsed.Day(), parsed.Month(), parsed.Year())
	}
	return &ModalSessionForm{
		isEdit:    false,
		dateStr:   dStr,
		timeStr:   sess.Time,
		name:      sess.Name,
		typeStr:   sess.Type,
		durStr:    strconv.Itoa(sess.Duration),
		cashStr:   strconv.FormatFloat(sess.Cash, 'f', -1, 64),
		status:    calendar.FormatStatusDisplay(sess.Status),
		notes:     sess.Notes,
		repeatStr: "none",
		fieldIdx:  0, // focus date so user can adjust date or press Enter immediately
	}
}

func (m *ModalSessionForm) Draw(s *Screen) {
	w := 58
	h := 20
	if !m.isEdit {
		h = 22
	}
	if w > s.Width-2 {
		w = s.Width - 2
	}
	if h > s.Height-2 {
		h = s.Height - 2
	}
	x := (s.Width - w) / 2
	y := (s.Height - h) / 2
	if x < 1 {
		x = 1
	}
	if y < 1 {
		y = 1
	}

	title := "Новая запись"
	if m.isEdit {
		title = "Редактирование записи: " + m.targetID
	}
	s.DrawBox(x, y, w, h, title, FgBlue, BorderBright, BgModal, true)

	fields := []struct {
		label string
		val   string
	}{
		{"1. Дата (ДД.ММ.ГГГГ):", m.dateStr},
		{"2. Время (ЧЧ:ММ):      ", m.timeStr},
		{"3. Название записи:    ", m.name},
		{"4. Тип / Категория:    ", m.typeStr},
		{"5. Длительность (мин): ", m.durStr},
		{"6. Кэш:                ", m.cashStr},
		{"7. Статус:             ", m.status},
		{"8. Заметки / Коммент.: ", m.notes},
	}
	if !m.isEdit {
		repLabel := "Без повтора"
		if m.repeatStr == "d" {
			repLabel = "Каждый день"
		} else if m.repeatStr == "w" {
			repLabel = "Каждую неделю"
		} else if m.repeatStr == "m" {
			repLabel = "Каждый месяц"
		}
		fields = append(fields, struct {
			label string
			val   string
		}{"9. Повтор записи:      ", repLabel})
	}

	valBoxW := w - 28
	for i, f := range fields {
		rowY := y + 2 + (i * 2)
		isCur := i == m.fieldIdx
		lblFg := FgMuted
		if isCur {
			lblFg = FgCyan
		}
		s.DrawText(x+2, rowY, f.label, lblFg, BgModal, 24, isCur, false, false)

		valBg := BgInput
		valFg := FgWhite
		s.Fill(x+25, rowY, valBoxW, 1, ' ', valFg, valBg)

		txt := f.val
		if isCur {
			txt += "█"
		}
		s.DrawText(x+26, rowY, txt, valFg, valBg, valBoxW-2, isCur, false, false)

		hasDropdown := false
		switch i {
		case 1, 2, 3, 6:
			hasDropdown = true
		case 8:
			if !m.isEdit {
				hasDropdown = true
			}
		}

		if hasDropdown && isCur && !m.pickerOpen && valBoxW > 6 {
			s.DrawText(x+25+valBoxW-5, rowY, "[▼]", FgCyan, valBg, 3, true, false, false)
		}
	}

	if m.pickerOpen {
		if m.pickerField == 3 || m.pickerField == 6 {
			s.DrawBottomWrappedHint(x+2, y+h-2, w-4, 2, "[↑↓] Выбор  [d] Удалить  [Enter] Выбрать  [Esc/←] Ввести вручную", FgDim, BgModal)
		} else {
			s.DrawBottomWrappedHint(x+2, y+h-2, w-4, 2, "[↑↓] Выбор из списка  [Enter] Выбрать  [Esc/←] Ввести свой вручную", FgDim, BgModal)
		}
	} else {
		switch m.fieldIdx {
		case 1:
			s.DrawBottomWrappedHint(x+2, y+h-2, w-4, 2, "[Tab] Поля  [→] Выбрать время  [Enter] Сохранить  [Esc] Отмена", FgDim, BgModal)
		case 2:
			s.DrawBottomWrappedHint(x+2, y+h-2, w-4, 2, "[Tab] Поля  [→] Подсказки  [Enter] Сохранить  [Esc] Отмена", FgDim, BgModal)
		case 3:
			s.DrawBottomWrappedHint(x+2, y+h-2, w-4, 2, "[Ввод текста] Свой тип  [→] Список  [Tab] Далее  [Enter] Сохранить", FgDim, BgModal)
		case 6:
			s.DrawBottomWrappedHint(x+2, y+h-2, w-4, 2, "[Ввод текста] Свой статус  [Space] След.  [→] Список  [Tab] Далее  [Enter] Сохранить", FgDim, BgModal)
		case 8:
			s.DrawBottomWrappedHint(x+2, y+h-2, w-4, 2, "[Tab] Поля  [→/Space] Выбрать повтор  [Enter] Сохранить  [Esc] Отмена", FgDim, BgModal)
		default:
			s.DrawBottomWrappedHint(x+2, y+h-2, w-4, 2, "[Tab/Shift+Tab] Поля  [Enter] Сохранить  [Esc] Отмена", FgDim, BgModal)
		}
	}

	if m.pickerOpen && len(m.pickerList) > 0 {
		popX := x + 25
		popW := valBoxW
		if popW < 28 {
			popW = 28
		}
		if popX+popW > x+w-1 {
			popW = x + w - 1 - popX
		}
		if popW < 14 {
			popW = 14
			popX = x + w - 1 - popW
		}
		if popX < x+1 {
			popX = x + 1
		}
		visibleRows := 6
		if len(m.pickerList) < visibleRows {
			visibleRows = len(m.pickerList)
		}
		popH := visibleRows + 2

		fieldY := y + 2 + (m.pickerField * 2)
		popY := fieldY + 1
		if popY+popH > y+h-1 || popY+popH > s.Height-1 {
			popY = fieldY - popH
			if popY < y+1 {
				popY = y + 1
			}
		}

		s.DrawBox(popX, popY, popW, popH, m.pickerTitle, FgCyan, BorderFocus, BgCard, true)
		for i := 0; i < visibleRows; i++ {
			itemIdx := m.pickerScroll + i
			if itemIdx >= len(m.pickerList) {
				break
			}
			item := m.pickerList[itemIdx]
			itemY := popY + 1 + i
			isSel := itemIdx == m.pickerIdx

			bg := BgCard
			fg := FgWhite
			prefix := "  "
			if isSel {
				bg = BgHighlight
				fg = FgWhite
				prefix = "► "
			}
			s.Fill(popX+1, itemY, popW-2, 1, ' ', fg, bg)
			if m.pickerField == 6 { // Status
				badgeText, bFg, _ := StatusBadge(item)
				s.DrawText(popX+1, itemY, prefix+badgeText, bFg, bg, popW-2, isSel, false, false)
			} else {
				s.DrawText(popX+1, itemY, prefix+item, fg, bg, popW-2, isSel, false, false)
			}
		}
	}
}

func (m *ModalSessionForm) HandleKey(k Key, app *App) bool {
	m.appRef = app
	maxField := 7
	if !m.isEdit {
		maxField = 8
	}

	if m.pickerOpen {
		switch {
		case k.Type == KeyEsc || k.Type == KeyLeft:
			m.pickerOpen = false
			return true

		case k.Type == KeyUp || k.Matches('k', 'л'):
			if m.pickerIdx > 0 {
				m.pickerIdx--
				if m.pickerIdx < m.pickerScroll {
					m.pickerScroll = m.pickerIdx
				}
			}
			return true

		case k.Type == KeyDown || k.Matches('j', 'о'):
			if m.pickerIdx < len(m.pickerList)-1 {
				m.pickerIdx++
				visibleRows := 6
				if m.pickerIdx >= m.pickerScroll+visibleRows {
					m.pickerScroll = m.pickerIdx - visibleRows + 1
				}
			}
			return true

		case k.Matches('d', 'в') || k.Type == KeyDelete:
			if m.pickerIdx >= 0 && m.pickerIdx < len(m.pickerList) {
				item := m.pickerList[m.pickerIdx]
				if m.pickerField == 3 { // Type
					if app.cfg != nil {
						app.cfg.RemoveType(item)
						app.persistConfig()
					}
					app.setStatusMsg("Тип «" + item + "» удалён из списков выбора")
					m.openCatPicker(app)
				} else if m.pickerField == 6 { // Status
					if item == calendar.EmptyStatusLabel {
						return true
					}
					if app.cfg != nil {
						app.cfg.RemoveStatus(item)
						app.persistConfig()
					}
					app.setStatusMsg("Статус «" + item + "» удалён из списков выбора")
					m.openStatusPicker(app)
				}
			}
			return true

		case k.Type == KeyEnter || k.Type == KeySpace:
			if m.pickerIdx >= 0 && m.pickerIdx < len(m.pickerList) {
				val := m.pickerList[m.pickerIdx]
				switch m.pickerField {
				case 1:
					m.timeStr = val
				case 2:
					m.name = val
				case 3:
					m.typeStr = val
				case 6:
					m.status = val
				case 8:
					repCodes := []string{"none", "d", "w", "m"}
					if m.pickerIdx < len(repCodes) {
						m.repeatStr = repCodes[m.pickerIdx]
					}
				}
			}
			m.pickerOpen = false
			return true

		case k.Type == KeyTab:
			m.pickerOpen = false
			if m.fieldIdx < maxField {
				m.fieldIdx++
			} else {
				m.fieldIdx = 0
			}
			return true
		}
		return true // consume input when dropdown is active
	}

	switch k.Type {
	case KeyEsc:
		app.activeModal = nil
		return true

	case KeyRight, KeyF2:
		switch m.fieldIdx {
		case 1:
			m.openTimePicker()
			return true
		case 2:
			m.openNamePicker(app)
			return true
		case 3:
			m.openCatPicker(app)
			return true
		case 6:
			m.openStatusPicker(app)
			return true
		case 8:
			m.openRepeatPicker()
			return true
		}

	case KeyTab, KeyDown:
		if m.fieldIdx < maxField {
			m.fieldIdx++
		} else {
			m.fieldIdx = 0
		}
		return true

	case KeyBackTab, KeyUp:
		if m.fieldIdx > 0 {
			m.fieldIdx--
		} else {
			m.fieldIdx = maxField
		}
		return true

	case KeyEnter:
		if m.save(app) {
			app.activeModal = nil
		}
		return true

	case KeyBackspace:
		m.backspace()
		return true

	case KeySpace:
		m.appendChar(' ')
		return true

	case KeyRune:
		m.appendChar(k.Rune)
		return true
	}
	return false
}

func (m *ModalSessionForm) appendChar(r rune) {
	switch m.fieldIdx {
	case 0:
		m.dateStr += string(r)
	case 1:
		m.timeStr += string(r)
	case 2:
		m.name += string(r)
	case 3:
		m.typeStr += string(r)
	case 4:
		if r >= '0' && r <= '9' {
			m.durStr += string(r)
		}
	case 5:
		if (r >= '0' && r <= '9') || r == '.' || r == ',' || r == '-' {
			if r == ',' {
				r = '.'
			}
			if r == '-' {
				if m.cashStr == "0.0" || m.cashStr == "0" {
					m.cashStr = "-"
				} else if strings.HasPrefix(m.cashStr, "-") {
					m.cashStr = strings.TrimPrefix(m.cashStr, "-")
				} else {
					m.cashStr = "-" + m.cashStr
				}
			} else {
				if (m.cashStr == "0.0" || m.cashStr == "0") && r >= '0' && r <= '9' {
					m.cashStr = string(r)
				} else {
					m.cashStr += string(r)
				}
			}
		}
	case 6:
		// Cycle status on key
		if r == ' ' || r == 's' || r == 'ы' {
			m.cycleStatus(m.appRef)
		} else {
			m.status += string(r)
		}
	case 7:
		m.notes += string(r)
	case 8:
		m.cycleRepeat()
	}
}

func (m *ModalSessionForm) cycleRepeat() {
	if m.repeatStr == "none" {
		m.repeatStr = "d"
	} else if m.repeatStr == "d" {
		m.repeatStr = "w"
	} else if m.repeatStr == "w" {
		m.repeatStr = "m"
	} else {
		m.repeatStr = "none"
	}
}

func (m *ModalSessionForm) openRepeatPicker() {
	m.pickerList = []string{"Без повтора", "Каждый день", "Каждую неделю", "Каждый месяц"}
	m.pickerTitle = " Повтор [Enter] "
	m.pickerField = 8
	m.pickerOpen = true
	m.pickerScroll = 0
	m.pickerIdx = 0

	repCodes := []string{"none", "d", "w", "m"}
	for i, code := range repCodes {
		if code == m.repeatStr {
			m.pickerIdx = i
			break
		}
	}
}

func (m *ModalSessionForm) openTimePicker() {
	var list []string
	for h := 0; h < 24; h++ {
		list = append(list, fmt.Sprintf("%02d:00", h))
	}
	m.pickerList = list
	m.pickerTitle = " Время [Enter] "
	m.pickerField = 1
	m.pickerOpen = true
	m.pickerScroll = 0
	m.pickerIdx = 10

	cur := strings.TrimSpace(m.timeStr)
	curHour := -1
	if parts := strings.Split(cur, ":"); len(parts) >= 1 {
		if h, err := strconv.Atoi(parts[0]); err == nil && h >= 0 && h < 24 {
			curHour = h
		}
	}
	for i, item := range m.pickerList {
		if strings.EqualFold(item, cur) || (curHour >= 0 && i == curHour) {
			m.pickerIdx = i
			if m.pickerIdx >= 6 {
				m.pickerScroll = m.pickerIdx - 5
			}
			break
		}
	}
}

func (m *ModalSessionForm) openCatPicker(app *App) {
	var list []string
	seen := map[string]bool{}

	if app != nil {
		for _, t := range app.getAvailableTypes() {
			t = strings.TrimSpace(t)
			if t != "" && !seen[t] {
				seen[t] = true
				list = append(list, t)
			}
		}
	}

	m.pickerList = list
	m.pickerTitle = " Категории [Enter] "
	m.pickerField = 3
	m.pickerOpen = true
	m.pickerScroll = 0
	m.pickerIdx = 0

	cur := strings.TrimSpace(m.typeStr)
	for i, item := range m.pickerList {
		if strings.EqualFold(item, cur) {
			m.pickerIdx = i
			if m.pickerIdx >= 6 {
				m.pickerScroll = m.pickerIdx - 5
			}
			break
		}
	}
}

func (m *ModalSessionForm) openStatusPicker(app *App) {
	var list []string
	if app != nil {
		list = append([]string{}, app.getAvailableStatuses()...)
	} else {
		list = []string{"Активно", "Завершено", "В процессе", "Запланировано", "Пропущено", "Отменено"}
	}
	hasEmpty := false
	for _, st := range list {
		if strings.EqualFold(st, calendar.EmptyStatusLabel) {
			hasEmpty = true
			break
		}
	}
	if !hasEmpty {
		list = append(list, calendar.EmptyStatusLabel)
	}
	m.pickerList = list
	m.pickerTitle = " Статусы [Enter] "
	m.pickerField = 6
	m.pickerOpen = true
	m.pickerScroll = 0
	m.pickerIdx = 0

	cur := calendar.FormatStatusDisplay(m.status)
	for i, item := range m.pickerList {
		if strings.EqualFold(item, cur) {
			m.pickerIdx = i
			if m.pickerIdx >= 6 {
				m.pickerScroll = m.pickerIdx - 5
			}
			break
		}
	}
}

func (m *ModalSessionForm) openNamePicker(app *App) {
	var list []string
	if app != nil {
		list = app.getAvailableNames()
	}
	if len(list) == 0 {
		return
	}
	m.pickerList = list
	m.pickerTitle = " Частые имена [Enter] "
	m.pickerField = 2
	m.pickerOpen = true
	m.pickerScroll = 0
	m.pickerIdx = 0

	cur := strings.TrimSpace(m.name)
	for i, item := range m.pickerList {
		if strings.EqualFold(item, cur) {
			m.pickerIdx = i
			if m.pickerIdx >= 6 {
				m.pickerScroll = m.pickerIdx - 5
			}
			break
		}
	}
}

func (m *ModalSessionForm) cycleStatus(app *App) {
	order := []string{"Активно", "Завершено", "В процессе", "Запланировано", "Пропущено", "Отменено"}
	if app != nil {
		order = append([]string{}, app.getAvailableStatuses()...)
	}
	order = append(order, calendar.EmptyStatusLabel)
	cur := calendar.FormatStatusDisplay(m.status)
	for i, st := range order {
		if strings.EqualFold(st, cur) {
			m.status = order[(i+1)%len(order)]
			return
		}
	}
	if len(order) > 0 {
		m.status = order[0]
	}
}

func (m *ModalSessionForm) backspace() {
	delLast := func(s *string) {
		if len(*s) > 0 {
			runes := []rune(*s)
			*s = string(runes[:len(runes)-1])
		}
	}
	switch m.fieldIdx {
	case 0:
		delLast(&m.dateStr)
	case 1:
		delLast(&m.timeStr)
	case 2:
		delLast(&m.name)
	case 3:
		delLast(&m.typeStr)
	case 4:
		delLast(&m.durStr)
	case 5:
		delLast(&m.cashStr)
	case 6:
		delLast(&m.status)
	case 7:
		delLast(&m.notes)
	}
}

func (m *ModalSessionForm) save(app *App) bool {
	refDate := app.today()
	rawDate := strings.TrimSpace(m.dateStr)
	d, valid, err := calendar.CheckDateInput(rawDate, refDate)
	if err != nil {
		d = refDate
		valid = true
	} else if !valid && app.cfg != nil && app.cfg.DateCheckMode != models.DateCheckOff {
		correctedDot := fmt.Sprintf("%02d.%02d.%04d", d.Day(), d.Month(), d.Year())
		switch app.cfg.DateCheckMode {
		case models.DateCheckReask:
			m.fieldIdx = 0
			app.setStatusMsg(fmt.Sprintf("Некорректная дата %q — введите дату заново", rawDate))
			return false
		case models.DateCheckAsk:
			m.dateStr = correctedDot
			m.fieldIdx = 0
			app.setStatusMsg(fmt.Sprintf("Дата %q исправлена на %s — нажмите Enter для подтверждения", rawDate, correctedDot))
			return false
		case models.DateCheckFix:
			m.dateStr = correctedDot
		}
	}

	tm, err := parser.ParseTime(strings.TrimSpace(m.timeStr))
	if err != nil {
		tm, _ = parser.ParseTime("10:00")
	}

	dur, _ := strconv.Atoi(strings.TrimSpace(m.durStr))
	if dur <= 0 {
		dur = 60
	}

	cashVal, _ := calendar.ParseCash(m.cashStr)

	nameVal := strings.TrimSpace(m.name)
	if nameVal == "" {
		nameVal = "Запись"
	}
	typeVal := strings.TrimSpace(m.typeStr)
	statusVal := calendar.NormalizeStatus(m.status)
	notesVal := strings.TrimSpace(m.notes)

	var savedID string
	if m.isEdit {
		tStr := parser.FormatTime(tm)
		patch := calendar.Patch{
			Time:     &tStr,
			Name:     &nameVal,
			Type:     &typeVal,
			Duration: &dur,
			Cash:     &cashVal,
			Status:   &statusVal,
			Notes:    &notesVal,
		}
		updated, _, err := app.svc.UpdateEntry(app.ctx, app.cfg, m.targetID, calendar.UpdateInput{
			Patch:   patch,
			NewDate: &d,
		})
		if err != nil {
			app.setStatusMsg("⚠ Ошибка сохранения: " + err.Error())
			return false
		}
		savedID = updated.ID
		app.calYear = d.Year()
		app.calMonth = int(d.Month())
		app.calDay = d.Day()
		app.persistConfig()
		app.setStatusMsg("Запись успешно сохранена")
	} else {
		var repMode string
		var repUntil time.Time
		if m.repeatStr != "" && m.repeatStr != "none" {
			repMode = m.repeatStr
			if repMode == "m" {
				repUntil = calendar.AddMonths(d, 12)
			} else {
				repUntil = calendar.AddMonths(d, 1)
			}
		}
		res, err := app.svc.CreateEntries(app.ctx, app.cfg, calendar.EntryInput{
			Date:        d,
			Time:        tm,
			RawName:     nameVal,
			Type:        typeVal,
			Duration:    dur,
			Cash:        cashVal,
			Status:      statusVal,
			Notes:       notesVal,
			RepeatMode:  repMode,
			RepeatUntil: repUntil,
		})
		if err != nil {
			app.setStatusMsg("⚠ Ошибка создания записи: " + err.Error())
			return false
		}
		if len(res.Sessions) > 0 {
			app.persistConfig()
			savedID = res.Sessions[len(res.Sessions)-1].ID
			if len(res.Sessions) > 1 {
				app.setStatusMsg(fmt.Sprintf("Успешно создано записей: %d", len(res.Sessions)))
			} else {
				app.setStatusMsg("Запись успешно создана: ID " + savedID)
			}
		}

		app.calYear = d.Year()
		app.calMonth = int(d.Month())
		app.calDay = d.Day()
		if app.periodMode == "day" || app.periodMode == "today" {
			app.startDate = d
			app.endDate = d.Add(24*time.Hour - time.Nanosecond)
			app.periodMode = "day"
		}
	}
	app.reloadSessions()
	if savedID != "" {
		for idx, s := range app.sessions {
			if s.ID == savedID {
				app.cursorIdx = idx
				break
			}
		}
	}
	return true
}

// ----------------- CONFIRM DELETE MODAL -----------------

type ModalConfirmDelete struct {
	targetID string
	count    int
}

func NewModalConfirmDelete(targetID string, count int) *ModalConfirmDelete {
	return &ModalConfirmDelete{targetID: targetID, count: count}
}

func (m *ModalConfirmDelete) Draw(s *Screen) {
	w := 46
	h := 8
	if w > s.Width-2 {
		w = s.Width - 2
	}
	if h > s.Height-2 {
		h = s.Height - 2
	}
	x := (s.Width - w) / 2
	y := (s.Height - h) / 2
	if x < 1 {
		x = 1
	}
	if y < 1 {
		y = 1
	}

	s.DrawBox(x, y, w, h, "Подтверждение удаления", FgRed, BorderBright, BgModal, true)

	msg := "Удалить выбранную запись?"
	if m.count > 1 {
		msg = fmt.Sprintf("Удалить выбранные записи (%d шт.)?", m.count)
	}
	s.DrawText(x+3, y+2, msg, FgWhite, BgModal, w-6, true, false, false)
	s.DrawText(x+3, y+3, "Это действие необратимо.", FgRed, BgModal, w-6, false, false, false)

	s.DrawBottomWrappedHint(x+3, y+h-2, w-6, 2, "[Enter] Удалить      [Esc] Отмена", FgDim, BgModal)
}

func (m *ModalConfirmDelete) HandleKey(k Key, app *App) bool {
	switch k.Type {
	case KeyEsc:
		app.activeModal = nil
		return true
	case KeyEnter:
		if m.targetID != "" {
			_ = app.svc.Delete(m.targetID)
			if err := app.svc.Save(app.ctx); err != nil {
				app.setStatusMsg("⚠ Ошибка сохранения: " + err.Error())
			} else {
				app.lastDataMod = app.svc.DataFilesModTime()
				app.setStatusMsg("Запись удалена")
			}
		} else if len(app.selectedIDs) > 0 {
			app.svc.BeginUndoTx("групповое удаление записей")
			delCount := 0
			for id := range app.selectedIDs {
				if app.svc.Delete(id) {
					delCount++
				}
			}
			app.svc.EndUndoTx()
			if err := app.svc.Save(app.ctx); err != nil {
				app.setStatusMsg("⚠ Ошибка сохранения: " + err.Error())
			} else {
				app.lastDataMod = app.svc.DataFilesModTime()
				app.selectedIDs = map[string]bool{}
				app.setStatusMsg(fmt.Sprintf("Удалено записей: %d", delCount))
			}
		}
		app.activeModal = nil
		app.reloadSessions()
		return true
	}
	return false
}

// ----------------- YEAR CALENDAR MODAL -----------------

type ModalYearCalendar struct {
	year             int
	selMonth         int
	selDay           int
	todayDate        time.Time
	rangeAnchor      *time.Time
	startDate        time.Time
	endDate          time.Time
	sessions         []models.Session
	daysWithSessions map[int]bool
	inputMode        bool
	inputYear        string
}

func NewModalYearCalendar(year, selMonth, selDay int, sessions []models.Session) *ModalYearCalendar {
	now := time.Now()
	if year <= 0 {
		year = now.Year()
	}
	if selMonth <= 0 || selMonth > 12 {
		selMonth = int(now.Month())
	}
	if selDay <= 0 || selDay > 31 {
		selDay = now.Day()
	}
	m := &ModalYearCalendar{
		year:      year,
		selMonth:  selMonth,
		selDay:    selDay,
		todayDate: now,
		sessions:  sessions,
	}
	m.updateDaysWithSessions()
	m.syncRange()
	return m
}

func (m *ModalYearCalendar) updateDaysWithSessions() {
	m.daysWithSessions = make(map[int]bool)
	yrPrefix := fmt.Sprintf("%04d-", m.year)
	for _, sess := range m.sessions {
		if dStr := sess.Date(); len(dStr) == 10 && strings.HasPrefix(dStr, yrPrefix) {
			mo, err1 := strconv.Atoi(dStr[5:7])
			da, err2 := strconv.Atoi(dStr[8:10])
			if err1 == nil && err2 == nil {
				m.daysWithSessions[mo*100+da] = true
			}
		}
	}
}

func (m *ModalYearCalendar) syncRange() {
	curDate := time.Date(m.year, time.Month(m.selMonth), m.selDay, 0, 0, 0, 0, time.Local)
	if m.rangeAnchor != nil {
		anchor := *m.rangeAnchor
		if curDate.Before(anchor) {
			m.startDate = curDate
			m.endDate = anchor.Add(24*time.Hour - time.Nanosecond)
		} else {
			m.startDate = anchor
			m.endDate = curDate.Add(24*time.Hour - time.Nanosecond)
		}
	} else {
		m.startDate = curDate
		m.endDate = curDate.Add(24*time.Hour - time.Nanosecond)
	}
}

func (m *ModalYearCalendar) isDayInRange(year, month, day int) bool {
	if m.startDate.IsZero() || m.endDate.IsZero() {
		return false
	}
	if m.rangeAnchor == nil && m.startDate.Equal(m.endDate) {
		return false
	}
	d := time.Date(year, time.Month(month), day, 12, 0, 0, 0, time.Local)
	start := time.Date(m.startDate.Year(), m.startDate.Month(), m.startDate.Day(), 0, 0, 0, 0, time.Local)
	end := time.Date(m.endDate.Year(), m.endDate.Month(), m.endDate.Day(), 23, 59, 59, 0, time.Local)
	return (d.Equal(start) || d.After(start)) && (d.Equal(end) || d.Before(end))
}

func (m *ModalYearCalendar) Draw(s *Screen) {
	w := 92
	if w > s.Width-2 {
		w = s.Width - 2
	}
	if w < 30 {
		w = 30
	}
	if w > s.Width {
		w = s.Width
	}

	h := 30
	if h > s.Height-2 {
		h = s.Height - 2
	}
	if h < 12 {
		h = 12
	}
	if h > s.Height {
		h = s.Height
	}

	x := (s.Width - w) / 2
	y := (s.Height - h) / 2
	if x < 1 {
		x = 1
	}
	if y < 1 {
		y = 1
	}

	title := fmt.Sprintf("◄  Годовой календарь на %d год  ►", m.year)
	s.DrawBox(x, y, w, h, title, FgCyan, BorderBright, BgModal, true)

	months := []string{
		"Январь", "Февраль", "Март", "Апрель",
		"Май", "Июнь", "Июль", "Август",
		"Сентябрь", "Октябрь", "Ноябрь", "Декабрь",
	}

	wide := (w >= 90)
	now := m.todayDate
	if now.IsZero() {
		now = time.Now()
	}

	startRow := 0
	numRows := 3
	if h < 28 {
		numRows = 2
		if m.selMonth > 8 {
			startRow = 1
		}
	}

	maxDrawX := x + w - 2
	maxDrawY := y + h - 2

	tilesBottomY := y + 2
	for r := 0; r < numRows; r++ {
		rowIdx := startRow + r
		by := y + 2 + (r * 8)

		for col := 0; col < 4; col++ {
			monthIdx := rowIdx*4 + col
			if monthIdx >= 12 {
				break
			}
			mNum := monthIdx + 1

			var bx int
			tileW := 15
			if wide {
				bx = x + 3 + (col * 22)
				tileW = 21
			} else {
				bx = x + 2 + (col * 16)
			}

			if bx > maxDrawX {
				continue
			}

			tileMaxX := bx + tileW - 1
			if tileMaxX > maxDrawX {
				tileMaxX = maxDrawX
			}

			maxTitleW := tileMaxX - bx + 1
			if maxTitleW <= 0 || by > maxDrawY {
				continue
			}

			// Month Title
			mTitle := fmt.Sprintf("%02d. %s", mNum, months[monthIdx])
			mTitleFg := FgYellow
			if mNum == m.selMonth {
				mTitleFg = FgCyan
			}
			s.DrawText(bx, by, mTitle, mTitleFg, BgModal, min(16, maxTitleW), mNum == m.selMonth, false, false)

			// Weekdays Header
			if by+1 <= maxDrawY {
				hdrW := 14
				if wide {
					hdrW = 20
				}
				s.DrawText(bx, by+1, "Пн Вт Ср Чт Пт Сб Вс", FgDim, BgModal, min(hdrW, maxTitleW), false, false, false)
			}

			firstDay := time.Date(m.year, time.Month(mNum), 1, 0, 0, 0, 0, time.UTC)
			weekday := int(firstDay.Weekday())
			if weekday == 0 {
				weekday = 7
			}
			weekday-- // 0..6 (Mon..Sun)

			daysInMonth := time.Date(m.year, time.Month(mNum+1), 0, 0, 0, 0, 0, time.UTC).Day()

			var curX int
			if wide {
				curX = bx + (weekday * 3)
			} else {
				curX = bx + (weekday * 2)
			}
			curY := by + 2

			for d := 1; d <= daysInMonth; d++ {
				if curY > maxDrawY {
					break
				}
				fg := FgMuted
				bg := BgModal
				bold := false
				under := false

				hasSess := m.daysWithSessions[mNum*100+d]
				if hasSess {
					fg = FgGreen
					bold = true
				}

				inRange := m.isDayInRange(m.year, mNum, d)
				if inRange {
					bg = BgCard
					if !hasSess {
						fg = FgCyan
					}
				}

				isAnchor := (m.rangeAnchor != nil && m.rangeAnchor.Year() == m.year && int(m.rangeAnchor.Month()) == mNum && m.rangeAnchor.Day() == d)
				if isAnchor {
					bg = BgHighlight
					fg = FgYellow
					bold = true
				}

				isCursor := (mNum == m.selMonth && d == m.selDay)
				if isCursor {
					bg = BgSelected
					fg = FgWhite
					bold = true
				}

				if m.year == now.Year() && mNum == int(now.Month()) && d == now.Day() {
					under = true
					if !isCursor && !isAnchor && !hasSess && !inRange {
						fg = FgCyan
						bold = true
					}
				}

				cellW := 2
				if curX+cellW-1 <= tileMaxX {
					s.Fill(curX, curY, cellW, 1, ' ', fg, bg)
					s.DrawText(curX, curY, fmt.Sprintf("%2d", d), fg, bg, 2, bold, false, under)
				}

				if wide {
					curX += 3
				} else {
					curX += 2
				}
				weekday++
				if weekday == 7 {
					weekday = 0
					curX = bx
					curY++
				}
				if curY > tilesBottomY {
					tilesBottomY = curY
				}
			}
		}
	}

	// Centered hint line located below the tiles
	hintY := y + h - 2
	if hintY <= tilesBottomY {
		hintY = tilesBottomY + 1
	}

	var hintText string
	if m.rangeAnchor != nil {
		hintText = fmt.Sprintf("[Стрелки] Выбор диапазона  [Space] Зафиксировать  [Esc] Отмена  [От: %02d.%02d]", m.rangeAnchor.Day(), m.rangeAnchor.Month())
	} else {
		hintText = "[↑↓←→] Выбор дня  [Space] Период  [Enter] Применить  [t] Сегодня  [y] Год  [Esc] Закрыть"
	}
	s.DrawBottomWrappedCenteredHint(x+2, hintY, w-4, 2, hintText, FgDim, BgModal)

	// Year text input overlay (if active)
	if m.inputMode {
		boxW := 36
		boxH := 7
		bx := x + (w-boxW)/2
		by := y + (h-boxH)/2
		s.Fill(bx, by, boxW, boxH, ' ', FgWhite, BgInput)
		s.DrawBox(bx, by, boxW, boxH, "Ввод года", FgCyan, BorderFocus, BgInput, true)
		s.DrawText(bx+3, by+2, "Введите 4-значный год:", FgMuted, BgInput, boxW-6, false, false, false)
		disp := m.inputYear + "█"
		s.DrawText(bx+3, by+3, disp, FgWhite, BgInput, boxW-6, true, false, false)
		s.DrawText(bx+3, by+5, "[Enter] Перейти  [Esc] Отмена", FgDim, BgInput, boxW-6, false, false, false)
	}
}

func (m *ModalYearCalendar) HandleKey(k Key, app *App) bool {
	if m.inputMode {
		switch {
		case k.Type == KeyEsc:
			m.inputMode = false
			return true
		case k.Type == KeyEnter:
			if yr, err := strconv.Atoi(m.inputYear); err == nil && yr >= 1900 && yr <= 2100 {
				m.year = yr
				m.updateDaysWithSessions()
				m.syncRange()
			}
			m.inputMode = false
			return true
		case k.Type == KeyBackspace:
			if len(m.inputYear) > 0 {
				m.inputYear = m.inputYear[:len(m.inputYear)-1]
			}
			return true
		case k.Type == KeyRune && k.Rune >= '0' && k.Rune <= '9':
			if len(m.inputYear) < 4 {
				m.inputYear += string(k.Rune)
			}
			return true
		}
		return false
	}

	switch {
	case k.Type == KeyEsc:
		if m.rangeAnchor != nil {
			m.rangeAnchor = nil
			m.syncRange()
			return true
		}
		app.activeModal = nil
		return true

	case k.Type == KeyEnter:
		// Apply selection to app
		app.calYear = m.year
		app.calMonth = m.selMonth
		app.calDay = m.selDay
		if m.rangeAnchor != nil {
			m.rangeAnchor = nil
		}

		if !m.startDate.IsZero() && !m.endDate.IsZero() && (m.endDate.Sub(m.startDate) > 24*time.Hour || m.startDate.Day() != m.endDate.Day()) {
			app.startDate = m.startDate
			app.endDate = m.endDate
			app.periodMode = "custom"
			app.reloadSessions()
			app.setStatusMsg(fmt.Sprintf("Выбран период: %s - %s", app.startDate.Format("02.01.2006"), app.endDate.Format("02.01.2006")))
		} else {
			curDate := time.Date(m.year, time.Month(m.selMonth), m.selDay, 0, 0, 0, 0, time.Local)
			app.startDate = curDate
			app.endDate = curDate.Add(24*time.Hour - time.Nanosecond)
			app.periodMode = "day"
			app.reloadSessions()
			app.setStatusMsg(fmt.Sprintf("Выбран день: %02d.%02d.%d", m.selDay, m.selMonth, m.year))
		}

		app.focusPanel = PanelFeed
		if app.width < 115 {
			app.activeTab = TabFeed
		}
		app.activeModal = nil
		return true

	case k.Matches('q', 'й'):
		app.activeModal = nil
		return true

	case k.Matches('a', 'ф'):
		app.calYear = m.year
		app.calMonth = m.selMonth
		app.calDay = m.selDay
		curDate := time.Date(m.year, time.Month(m.selMonth), m.selDay, 0, 0, 0, 0, time.Local)
		app.startDate = curDate
		app.endDate = curDate.Add(24*time.Hour - time.Nanosecond)
		app.periodMode = "day"
		app.reloadSessions()
		app.focusPanel = PanelFeed
		if app.width < 115 {
			app.activeTab = TabFeed
		}
		dateStr := fmt.Sprintf("%02d.%02d.%04d", m.selDay, m.selMonth, m.year)
		app.activeModal = NewModalSessionForm(nil, dateStr)
		return true

	case k.Type == KeySpace:
		if m.rangeAnchor == nil {
			anchor := time.Date(m.year, time.Month(m.selMonth), m.selDay, 0, 0, 0, 0, time.Local)
			m.rangeAnchor = &anchor
			m.syncRange()
		} else {
			m.rangeAnchor = nil
		}
		return true

	case k.Type == KeyHome || k.Matches('t', 'е'):
		now := time.Now()
		if app != nil {
			now = app.today()
		}
		m.todayDate = now
		oldYear := m.year
		m.year = now.Year()
		m.selMonth = int(now.Month())
		m.selDay = now.Day()
		m.rangeAnchor = nil
		if m.year != oldYear {
			m.updateDaysWithSessions()
		}
		m.syncRange()
		return true

	case k.Matches('y', 'н'):
		m.inputMode = true
		m.inputYear = ""
		return true

	case k.Type == KeyRune && k.Rune >= '0' && k.Rune <= '9':
		m.inputMode = true
		m.inputYear = string(k.Rune)
		return true

	case k.Type == KeyLeft || k.Matches('h', 'р'):
		cur := time.Date(m.year, time.Month(m.selMonth), m.selDay, 12, 0, 0, 0, time.Local)
		prev := cur.AddDate(0, 0, -1)
		oldYear := m.year
		m.year = prev.Year()
		m.selMonth = int(prev.Month())
		m.selDay = prev.Day()
		if m.year != oldYear {
			m.updateDaysWithSessions()
		}
		m.syncRange()
		return true

	case k.Type == KeyRight || k.Matches('l', 'д'):
		cur := time.Date(m.year, time.Month(m.selMonth), m.selDay, 12, 0, 0, 0, time.Local)
		next := cur.AddDate(0, 0, 1)
		oldYear := m.year
		m.year = next.Year()
		m.selMonth = int(next.Month())
		m.selDay = next.Day()
		if m.year != oldYear {
			m.updateDaysWithSessions()
		}
		m.syncRange()
		return true

	case k.Type == KeyUp || k.Matches('k', 'л'):
		cur := time.Date(m.year, time.Month(m.selMonth), m.selDay, 12, 0, 0, 0, time.Local)
		prevWeek := cur.AddDate(0, 0, -7)
		oldYear := m.year
		m.year = prevWeek.Year()
		m.selMonth = int(prevWeek.Month())
		m.selDay = prevWeek.Day()
		if m.year != oldYear {
			m.updateDaysWithSessions()
		}
		m.syncRange()
		return true

	case k.Type == KeyDown || k.Matches('j', 'о'):
		cur := time.Date(m.year, time.Month(m.selMonth), m.selDay, 12, 0, 0, 0, time.Local)
		nextWeek := cur.AddDate(0, 0, 7)
		oldYear := m.year
		m.year = nextWeek.Year()
		m.selMonth = int(nextWeek.Month())
		m.selDay = nextWeek.Day()
		if m.year != oldYear {
			m.updateDaysWithSessions()
		}
		m.syncRange()
		return true

	case k.Type == KeyTab:
		m.selMonth++
		if m.selMonth > 12 {
			m.selMonth = 1
			m.year++
			m.updateDaysWithSessions()
		}
		daysInMonth := time.Date(m.year, time.Month(m.selMonth+1), 0, 0, 0, 0, 0, time.Local).Day()
		if m.selDay > daysInMonth {
			m.selDay = daysInMonth
		}
		m.syncRange()
		return true

	case k.Type == KeyBackTab:
		m.selMonth--
		if m.selMonth < 1 {
			m.selMonth = 12
			m.year--
			m.updateDaysWithSessions()
		}
		daysInMonth := time.Date(m.year, time.Month(m.selMonth+1), 0, 0, 0, 0, 0, time.Local).Day()
		if m.selDay > daysInMonth {
			m.selDay = daysInMonth
		}
		m.syncRange()
		return true

	case k.Matches('<', 'б') || k.Rune == '<' || k.Rune == ',' || k.Rune == 'б' || k.Rune == 'Б':
		m.year--
		m.updateDaysWithSessions()
		m.syncRange()
		return true

	case k.Matches('>', 'ю') || k.Rune == '>' || k.Rune == '.' || k.Rune == 'ю' || k.Rune == 'Ю':
		m.year++
		m.updateDaysWithSessions()
		m.syncRange()
		return true

	case k.Type == KeyPgUp:
		m.year--
		m.updateDaysWithSessions()
		m.syncRange()
		return true

	case k.Type == KeyPgDn:
		m.year++
		m.updateDaysWithSessions()
		m.syncRange()
		return true
	}
	return false
}

// ----------------- SETTINGS MODAL -----------------

type ModalSettingsOption struct {
	Code  string
	Label string
}

type ModalSettings struct {
	defaultMode      string // "cli", "tui", "web"
	portStr          string
	durationStr      string
	durationUnit     string // "min" or "hours"
	defaultType      string
	dateCheckMode    string // "", "ask", "fix", "reask"
	askOnConflict    bool
	splitMode        string // "none", "year", "month"
	useSystemDate    bool
	customDate       string // "YYYY-MM-DD" or "DD.MM.YYYY"
	dataPath         string
	tuiDefaultPeriod string // "today", "all", "week", "month"
	svcStatus        *service.StatusInfo
	selectedIdx      int
	scrollOffset     int

	// Dropdown picker state
	pickerOpen   bool
	pickerField  int
	pickerTitle  string
	pickerList   []ModalSettingsOption
	pickerIdx    int
	pickerScroll int
}

func NewModalSettings(cfg *models.Config) *ModalSettings {
	mgr := service.NewManager()
	st, _ := mgr.Status(context.Background())
	durUnit := "min"
	defaultMode := models.ModeCLI
	portStr := "8080"
	durStr := "60"
	defaultType := ""
	dateCheckMode := ""
	askOnConflict := false
	splitMode := models.SplitNone
	useSystemDate := true
	customDate := time.Now().Format("2006-01-02")
	dataPath := ""
	tuiDefaultPeriod := "today"

	if cfg != nil {
		if cfg.DurationUnit == "hours" {
			durUnit = "hours"
		}
		if cfg.DefaultMode != "" {
			defaultMode = cfg.DefaultMode
		}
		if cfg.WebPort > 0 {
			portStr = strconv.Itoa(cfg.WebPort)
		}
		if cfg.DefaultDuration > 0 {
			durStr = strconv.Itoa(cfg.DefaultDuration)
		}
		defaultType = cfg.DefaultType
		dateCheckMode = cfg.DateCheckMode
		askOnConflict = cfg.AskOnConflict
		if cfg.ActiveSplitMode() != "" {
			splitMode = cfg.ActiveSplitMode()
		}
		useSystemDate = cfg.UseSystemDate
		if cfg.CustomDate != "" {
			customDate = cfg.CustomDate
		}
		if cfg.DataPath != "" {
			dataPath = cfg.DataPath
		}
		if cfg.TUIDefaultPeriod != "" {
			tuiDefaultPeriod = cfg.TUIDefaultPeriod
		}
	}
	return &ModalSettings{
		defaultMode:      defaultMode,
		portStr:          portStr,
		durationStr:      durStr,
		durationUnit:     durUnit,
		defaultType:      defaultType,
		dateCheckMode:    dateCheckMode,
		askOnConflict:    askOnConflict,
		splitMode:        splitMode,
		useSystemDate:    useSystemDate,
		customDate:       customDate,
		dataPath:         dataPath,
		tuiDefaultPeriod: tuiDefaultPeriod,
		selectedIdx:      0,
		scrollOffset:     0,
		svcStatus:        st,
	}
}

func (m *ModalSettings) canHavePicker(fieldIdx int) bool {
	switch fieldIdx {
	case 0, 2, 3, 4, 5, 6, 7, 8, 11, 12:
		return true
	default:
		return false
	}
}

func (m *ModalSettings) openPicker(fieldIdx int, app *App) {
	var list []ModalSettingsOption
	title := ""
	curCode := ""

	switch fieldIdx {
	case 0:
		title = " Режим запуска [Enter] "
		list = []ModalSettingsOption{
			{Code: models.ModeCLI, Label: "Классическое меню (CLI)"},
			{Code: models.ModeTUI, Label: "Полноэкранный TUI (-tui)"},
			{Code: models.ModeWeb, Label: "Веб-интерфейс (Web)"},
		}
		curCode = m.defaultMode

	case 2:
		title = " Длительность [Enter] "
		list = []ModalSettingsOption{
			{Code: "15", Label: "15 мин"},
			{Code: "30", Label: "30 мин"},
			{Code: "45", Label: "45 мин"},
			{Code: "60", Label: "60 мин (1 час)"},
			{Code: "90", Label: "90 мин (1.5 часа)"},
			{Code: "120", Label: "120 мин (2 часа)"},
			{Code: "180", Label: "180 мин (3 часа)"},
		}
		curCode = m.durationStr

	case 3:
		title = " Формат длительности [Enter] "
		list = []ModalSettingsOption{
			{Code: "min", Label: "Минуты (мин) — по умолч."},
			{Code: "hours", Label: "Часы (1 ч / 1.5 ч)"},
		}
		curCode = m.durationUnit

	case 4:
		title = " Категория по умолчанию [Enter] "
		list = append(list, ModalSettingsOption{Code: "", Label: "(по умолчанию / первая)"})
		if app != nil {
			for _, t := range app.getAvailableTypes() {
				t = strings.TrimSpace(t)
				if t != "" {
					list = append(list, ModalSettingsOption{Code: t, Label: t})
				}
			}
		}
		curCode = m.defaultType

	case 5:
		title = " Проверка даты [Enter] "
		list = []ModalSettingsOption{
			{Code: "", Label: "Выкл (автосдвиг на след. месяц)"},
			{Code: "ask", Label: "Спрашивать (подтверждение автосдвига)"},
			{Code: "fix", Label: "Исправлять авто (сдвиг с предупреждением)"},
			{Code: "reask", Label: "Переспрашивать (повторный ввод даты)"},
		}
		curCode = m.dateCheckMode

	case 6:
		title = " Конфликт времени [Enter] "
		list = []ModalSettingsOption{
			{Code: "false", Label: "Выкл (добавлять сразу без лишних вопросов)"},
			{Code: "true", Label: "Вкл (предупреждать о пересечении времени)"},
		}
		curCode = strconv.FormatBool(m.askOnConflict)

	case 7:
		title = " Режим хранения на диске [Enter] "
		list = []ModalSettingsOption{
			{Code: models.SplitNone, Label: "Один файл (mycal.json)"},
			{Code: models.SplitYear, Label: "По годам (mycal_YYYY.json)"},
			{Code: models.SplitMonth, Label: "По месяцам (mycal_YYYY-MM.json)"},
		}
		curCode = m.splitMode

	case 8:
		title = " Системная дата [Enter] "
		list = []ModalSettingsOption{
			{Code: "true", Label: "Вкл (текущее системное время)"},
			{Code: "false", Label: "Фиксированная (ручной ввод даты)"},
		}
		curCode = strconv.FormatBool(m.useSystemDate)

	case 11:
		title = " Период запуска в TUI [Enter] "
		list = []ModalSettingsOption{
			{Code: "today", Label: "Сегодня (по умолч.)"},
			{Code: "all", Label: "Все записи"},
			{Code: "week", Label: "Неделя"},
			{Code: "month", Label: "Месяц"},
		}
		curCode = m.tuiDefaultPeriod

	case 12:
		title = " Фоновая служба [Enter] "
		list = []ModalSettingsOption{
			{Code: "start", Label: "Запустить службу"},
			{Code: "stop", Label: "Остановить службу"},
			{Code: "restart", Label: "Перезапустить службу"},
			{Code: "install", Label: "Установить службу (systemd)"},
		}
	}

	if len(list) == 0 {
		return
	}

	selIdx := 0
	for i, opt := range list {
		if strings.EqualFold(opt.Code, curCode) {
			selIdx = i
			break
		}
	}

	m.pickerOpen = true
	m.pickerField = fieldIdx
	m.pickerTitle = title
	m.pickerList = list
	m.pickerIdx = selIdx
	m.pickerScroll = 0
	if m.pickerIdx >= 6 {
		m.pickerScroll = m.pickerIdx - 5
	}
}

func (m *ModalSettings) applyPickerOption(opt ModalSettingsOption, app *App) {
	switch m.pickerField {
	case 0:
		m.defaultMode = opt.Code
	case 2:
		m.durationStr = opt.Code
	case 3:
		m.durationUnit = opt.Code
	case 4:
		m.defaultType = opt.Code
	case 5:
		m.dateCheckMode = opt.Code
	case 6:
		m.askOnConflict = (opt.Code == "true")
	case 7:
		m.splitMode = opt.Code
	case 8:
		m.useSystemDate = (opt.Code == "true")
	case 11:
		m.tuiDefaultPeriod = opt.Code
	case 12:
		if app != nil {
			mgr := service.NewManager()
			switch opt.Code {
			case "start":
				_ = mgr.Start(app.ctx)
			case "stop":
				_ = mgr.Stop(app.ctx)
			case "restart":
				_ = mgr.Restart(app.ctx)
			case "install":
				p, _ := strconv.Atoi(m.portStr)
				if p <= 0 {
					p = 8080
				}
				_ = mgr.Install(app.ctx, p)
			}
			m.svcStatus, _ = mgr.Status(app.ctx)
		}
	}
}

func (m *ModalSettings) Draw(s *Screen) {
	w := 68
	h := 22
	if w > s.Width-2 {
		w = s.Width - 2
	}
	if h > s.Height-2 {
		h = s.Height - 2
	}
	x := (s.Width - w) / 2
	y := (s.Height - h) / 2
	if x < 1 {
		x = 1
	}
	if y < 1 {
		y = 1
	}

	s.DrawBox(x, y, w, h, "Настройки MyCal", FgCyan, BorderBright, BgModal, true)

	modeDisplay := "Классическое меню (CLI)"
	if m.defaultMode == models.ModeTUI {
		modeDisplay = "Полноэкранный TUI (-tui)"
	} else if m.defaultMode == models.ModeWeb {
		modeDisplay = "Веб-интерфейс (Web)"
	}

	unitDisplay := "Минуты (мин) — по умолч."
	if m.durationUnit == "hours" {
		unitDisplay = "Часы (1 ч / 1.5 ч)"
	}

	typeDisplay := m.defaultType
	if typeDisplay == "" {
		typeDisplay = "(по умолчанию / первая)"
	}

	dateCheckDisplay := "Выкл (автосдвиг)"
	switch m.dateCheckMode {
	case "ask":
		dateCheckDisplay = "Спрашивать (подтверждение)"
	case "fix":
		dateCheckDisplay = "Исправлять (автосдвиг)"
	case "reask":
		dateCheckDisplay = "Переспрашивать (повтор ввода)"
	}

	conflictDisplay := "Выкл (добавлять сразу)"
	if m.askOnConflict {
		conflictDisplay = "Вкл (предупреждать)"
	}

	splitDisplay := "Один файл (mycal.json)"
	switch m.splitMode {
	case models.SplitYear:
		splitDisplay = "По годам (mycal_YYYY.json)"
	case models.SplitMonth:
		splitDisplay = "По месяцам (mycal_YYYY-MM.json)"
	}

	dateDisplay := "Вкл (системное время)"
	if !m.useSystemDate {
		dateDisplay = "Фиксированная (ручной ввод)"
	}

	customDateDisplay := m.customDate
	if customDateDisplay == "" {
		customDateDisplay = time.Now().Format("2006-01-02")
	}

	pathDisplay := m.dataPath
	if pathDisplay == "" {
		pathDisplay = config.DataPathDisplay("")
	} else if pathDisplay == "." || pathDisplay == "./" || pathDisplay == `.\` {
		if m.selectedIdx != 10 {
			pathDisplay = config.DataPathDisplay(pathDisplay)
		}
	}

	tuiPeriodDisplay := "Сегодня (по умолч.)"
	switch m.tuiDefaultPeriod {
	case "all":
		tuiPeriodDisplay = "Все записи"
	case "week":
		tuiPeriodDisplay = "Неделя"
	case "month":
		tuiPeriodDisplay = "Месяц"
	case "today", "day":
		tuiPeriodDisplay = "Сегодня"
	}

	fields := []struct {
		lbl string
		val string
	}{
		{"1. Режим запуска:           ", modeDisplay},
		{"2. Порт веб-сервера:        ", m.portStr},
		{"3. Длительность по умолч.:  ", m.durationStr + " мин"},
		{"4. Формат длительности:     ", unitDisplay},
		{"5. Категория по умолчанию:  ", typeDisplay},
		{"6. Проверка даты (опечатки):", dateCheckDisplay},
		{"7. Конфликт времени:        ", conflictDisplay},
		{"8. Режим хранения записей:  ", splitDisplay},
		{"9. Системная дата:          ", dateDisplay},
		{"10. Фиксированная дата:     ", customDateDisplay},
		{"11. Путь к данным:          ", pathDisplay},
		{"12. Период по умолч. в TUI: ", tuiPeriodDisplay},
		{"13. Фоновая служба:         ", m.svcStatusText()},
	}

	availRows := h - 4
	if availRows < 1 {
		availRows = 1
	}

	// Adjust scroll offset
	if m.selectedIdx < m.scrollOffset {
		m.scrollOffset = m.selectedIdx
	} else if m.selectedIdx >= m.scrollOffset+availRows {
		m.scrollOffset = m.selectedIdx - availRows + 1
	}
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}

	lblW := 28
	if lblW > w-16 {
		lblW = w - 16
	}
	if lblW < 8 {
		lblW = 8
	}

	for i := 0; i < availRows && (i+m.scrollOffset) < len(fields); i++ {
		fieldIdx := i + m.scrollOffset
		f := fields[fieldIdx]
		rowY := y + 2 + i
		isCur := fieldIdx == m.selectedIdx
		fg := FgMuted
		if isCur {
			fg = FgYellow
		}

		s.DrawText(x+2, rowY, f.lbl, fg, BgModal, lblW, isCur, false, false)

		valBg := BgInput
		valFg := FgWhite
		if isCur {
			valBg = BgSelected
		}
		valX := x + 2 + lblW
		valW := (x + w - 2) - valX
		if valW > 0 {
			s.Fill(valX, rowY, valW, 1, ' ', valFg, valBg)
			disp := f.val
			if isCur && (fieldIdx == 1 || fieldIdx == 2 || fieldIdx == 9 || fieldIdx == 10) {
				disp += "█"
			}
			s.DrawText(valX+1, rowY, disp, valFg, valBg, valW-2, isCur, false, false)

			if isCur && m.canHavePicker(fieldIdx) && !m.pickerOpen && valW > 12 {
				s.DrawText(valX+valW-10, rowY, "[→ список]", FgCyan, valBg, 10, false, false, false)
			}
		}
	}

	// Scroll indicators
	if m.scrollOffset > 0 {
		s.DrawText(x+w-3, y+1, "▲", FgYellow, BgModal, 1, false, false, false)
	}
	if m.scrollOffset+availRows < len(fields) {
		s.DrawText(x+w-3, y+h-2, "▼", FgYellow, BgModal, 1, false, false, false)
	}

	if m.pickerOpen {
		s.DrawBottomWrappedHint(x+2, y+h-2, w-4, 2, "[↑↓] Выбор  [Enter/Space] Применить  [Esc/←] Закрыть список", FgDim, BgModal)
	} else {
		s.DrawBottomWrappedHint(x+2, y+h-2, w-4, 2, "[↑↓] Выбор  [Space/→] Изменить  [Enter] Сохранить  [Esc]", FgDim, BgModal)
	}

	// Draw dropdown popup if open
	if m.pickerOpen && len(m.pickerList) > 0 {
		popW := 42
		if popW > w-4 {
			popW = w - 4
		}
		if popW < 20 {
			popW = 20
		}
		valX := x + 2 + lblW
		popX := valX - 4
		if popX+popW > x+w-2 {
			popX = x + w - 2 - popW
		}
		if popX < x+2 {
			popX = x + 2
		}

		visibleRows := 6
		if len(m.pickerList) < visibleRows {
			visibleRows = len(m.pickerList)
		}
		popH := visibleRows + 2

		fieldScreenRow := y + 2 + (m.pickerField - m.scrollOffset)
		popY := fieldScreenRow + 1
		if popY+popH > y+h-1 || popY+popH > s.Height-1 {
			popY = fieldScreenRow - popH
			if popY < y+1 {
				popY = y + 1
			}
		}

		s.DrawBox(popX, popY, popW, popH, m.pickerTitle, FgCyan, BorderFocus, BgCard, true)
		for i := 0; i < visibleRows; i++ {
			itemIdx := m.pickerScroll + i
			if itemIdx >= len(m.pickerList) {
				break
			}
			opt := m.pickerList[itemIdx]
			itemY := popY + 1 + i
			isSel := itemIdx == m.pickerIdx

			bg := BgCard
			fg := FgWhite
			prefix := "  "
			if isSel {
				bg = BgHighlight
				fg = FgWhite
				prefix = "► "
			}
			s.Fill(popX+1, itemY, popW-2, 1, ' ', fg, bg)
			s.DrawText(popX+1, itemY, prefix+opt.Label, fg, bg, popW-2, isSel, false, false)
		}

		if m.pickerScroll > 0 {
			s.DrawText(popX+popW-2, popY+1, "▲", FgYellow, BgCard, 1, false, false, false)
		}
		if m.pickerScroll+visibleRows < len(m.pickerList) {
			s.DrawText(popX+popW-2, popY+popH-2, "▼", FgYellow, BgCard, 1, false, false, false)
		}
	}
}

func (m *ModalSettings) svcStatusText() string {
	if m.svcStatus != nil && m.svcStatus.Running {
		return "● Активна (работает)"
	} else if m.svcStatus != nil && m.svcStatus.Installed {
		return "○ Остановлена"
	}
	return "✕ Не установлена"
}

func (m *ModalSettings) HandleKey(k Key, app *App) bool {
	if m.pickerOpen {
		switch {
		case k.Type == KeyEsc || k.Type == KeyLeft:
			m.pickerOpen = false
			return true

		case k.Type == KeyUp || k.Matches('k', 'л'):
			if m.pickerIdx > 0 {
				m.pickerIdx--
				if m.pickerIdx < m.pickerScroll {
					m.pickerScroll = m.pickerIdx
				}
			}
			return true

		case k.Type == KeyDown || k.Matches('j', 'о'):
			if m.pickerIdx < len(m.pickerList)-1 {
				m.pickerIdx++
				visibleRows := 6
				if m.pickerIdx >= m.pickerScroll+visibleRows {
					m.pickerScroll = m.pickerIdx - visibleRows + 1
				}
			}
			return true

		case k.Type == KeyEnter || k.Type == KeySpace:
			if m.pickerIdx >= 0 && m.pickerIdx < len(m.pickerList) {
				opt := m.pickerList[m.pickerIdx]
				m.applyPickerOption(opt, app)
			}
			m.pickerOpen = false
			return true
		}
		return false
	}

	totalFields := 13
	switch {
	case k.Type == KeyEsc:
		app.activeModal = nil
		return true

	case k.Type == KeyRight || k.Matches('l', 'д'):
		if m.canHavePicker(m.selectedIdx) {
			m.openPicker(m.selectedIdx, app)
			return true
		}

	case k.Type == KeyUp || k.Matches('k', 'л'):
		if m.selectedIdx > 0 {
			m.selectedIdx--
		} else {
			m.selectedIdx = totalFields - 1
		}
		return true

	case k.Type == KeyDown || k.Matches('j', 'о'):
		if m.selectedIdx < totalFields-1 {
			m.selectedIdx++
		} else {
			m.selectedIdx = 0
		}
		return true

	case k.Type == KeyBackspace:
		if m.selectedIdx == 1 && len(m.portStr) > 0 {
			m.portStr = m.portStr[:len(m.portStr)-1]
			return true
		} else if m.selectedIdx == 2 && len(m.durationStr) > 0 {
			m.durationStr = m.durationStr[:len(m.durationStr)-1]
			return true
		} else if m.selectedIdx == 9 && len(m.customDate) > 0 {
			m.customDate = m.customDate[:len(m.customDate)-1]
			return true
		} else if m.selectedIdx == 10 && len(m.dataPath) > 0 {
			runes := []rune(m.dataPath)
			m.dataPath = string(runes[:len(runes)-1])
			return true
		}

	case k.Type == KeyRune:
		if m.selectedIdx == 1 && k.Rune >= '0' && k.Rune <= '9' && len(m.portStr) < 5 {
			m.portStr += string(k.Rune)
			return true
		} else if m.selectedIdx == 2 && k.Rune >= '0' && k.Rune <= '9' && len(m.durationStr) < 4 {
			m.durationStr += string(k.Rune)
			return true
		} else if m.selectedIdx == 9 && ((k.Rune >= '0' && k.Rune <= '9') || k.Rune == '-' || k.Rune == '.') && len(m.customDate) < 10 {
			m.customDate += string(k.Rune)
			m.useSystemDate = false
			return true
		} else if m.selectedIdx == 10 && k.Rune >= 32 && len([]rune(m.dataPath)) < 120 {
			m.dataPath += string(k.Rune)
			return true
		}

	case k.Type == KeySpace:
		switch m.selectedIdx {
		case 0:
			// Toggle default mode
			if m.defaultMode == models.ModeCLI {
				m.defaultMode = models.ModeTUI
			} else if m.defaultMode == models.ModeTUI {
				m.defaultMode = models.ModeWeb
			} else {
				m.defaultMode = models.ModeCLI
			}
			return true
		case 3:
			// Toggle duration unit
			if m.durationUnit == "hours" {
				m.durationUnit = "min"
			} else {
				m.durationUnit = "hours"
			}
			return true
		case 4:
			// Cycle default type
			var types []string
			if app != nil {
				types = app.getAvailableTypes()
			}
			if len(types) > 0 {
				curIdx := -1
				for idx, t := range types {
					if strings.EqualFold(t, m.defaultType) {
						curIdx = idx
						break
					}
				}
				nextIdx := (curIdx + 1) % len(types)
				m.defaultType = types[nextIdx]
			}
			return true
		case 5:
			// Cycle date check mode
			switch m.dateCheckMode {
			case "":
				m.dateCheckMode = "ask"
			case "ask":
				m.dateCheckMode = "fix"
			case "fix":
				m.dateCheckMode = "reask"
			case "reask":
				m.dateCheckMode = ""
			default:
				m.dateCheckMode = ""
			}
			return true
		case 6:
			// Toggle ask on conflict
			m.askOnConflict = !m.askOnConflict
			return true
		case 7:
			// Cycle split mode
			switch m.splitMode {
			case models.SplitNone, "":
				m.splitMode = models.SplitYear
			case models.SplitYear:
				m.splitMode = models.SplitMonth
			case models.SplitMonth:
				m.splitMode = models.SplitNone
			default:
				m.splitMode = models.SplitNone
			}
			return true
		case 8:
			// Toggle use system date
			m.useSystemDate = !m.useSystemDate
			return true
		case 11:
			// Cycle tui default period
			switch m.tuiDefaultPeriod {
			case "today", "day", "":
				m.tuiDefaultPeriod = "all"
			case "all":
				m.tuiDefaultPeriod = "week"
			case "week":
				m.tuiDefaultPeriod = "month"
			case "month":
				m.tuiDefaultPeriod = "today"
			default:
				m.tuiDefaultPeriod = "today"
			}
			return true
		case 12:
			// Toggle service
			mgr := service.NewManager()
			if m.svcStatus != nil && m.svcStatus.Running {
				_ = mgr.Stop(app.ctx)
			} else if m.svcStatus != nil && m.svcStatus.Installed {
				_ = mgr.Start(app.ctx)
			} else {
				p, _ := strconv.Atoi(m.portStr)
				if p <= 0 {
					p = 8080
				}
				_ = mgr.Install(app.ctx, p)
			}
			m.svcStatus, _ = mgr.Status(app.ctx)
			return true
		}

	case k.Type == KeyEnter:
		if m.selectedIdx == 12 {
			// Trigger service action on Enter
			mgr := service.NewManager()
			if m.svcStatus != nil && m.svcStatus.Running {
				_ = mgr.Stop(app.ctx)
			} else if m.svcStatus != nil && m.svcStatus.Installed {
				_ = mgr.Start(app.ctx)
			} else {
				p, _ := strconv.Atoi(m.portStr)
				if p <= 0 {
					p = 8080
				}
				_ = mgr.Install(app.ctx, p)
			}
			m.svcStatus, _ = mgr.Status(app.ctx)
			return true
		}

		app.cfg.DefaultMode = m.defaultMode
		app.cfg.TUIDefaultPeriod = m.tuiDefaultPeriod
		if p, err := strconv.Atoi(m.portStr); err == nil && p > 0 {
			app.cfg.WebPort = p
		}
		if d, err := strconv.Atoi(m.durationStr); err == nil && d > 0 {
			app.cfg.DefaultDuration = d
		}
		app.cfg.DurationUnit = m.durationUnit
		app.cfg.DefaultType = m.defaultType
		app.cfg.DateCheckMode = m.dateCheckMode
		app.cfg.AskOnConflict = m.askOnConflict
		oldSplit := app.cfg.ActiveSplitMode()
		app.cfg.SetActiveSplitMode(m.splitMode)
		app.cfg.UseSystemDate = m.useSystemDate
		if strings.TrimSpace(m.customDate) != "" {
			if parsedDate, err := parser.ParseDate(strings.TrimSpace(m.customDate), time.Now()); err == nil {
				app.cfg.CustomDate = parsedDate.Format("2006-01-02")
			}
		}
		if !app.cfg.UseSystemDate && app.cfg.CustomDate != "" {
			eff := config.EffectiveToday(app.cfg)
			app.calYear = eff.Year()
			app.calMonth = int(eff.Month())
			app.calDay = eff.Day()
		}
		if app.svc != nil && oldSplit != m.splitMode {
			app.svc.SetMode(m.splitMode)
			_ = app.svc.Save(app.ctx)
		}
		normPath := storage.NormalizeDataPath(m.dataPath)
		if normPath != app.cfg.DataPath {
			targetDir := config.EffectiveDataDir(normPath, app.cfgPath)
			if err := storage.EnsureWritableDir(targetDir); err != nil {
				app.setStatusMsg("⚠ " + err.Error())
				return true
			}
			app.cfg.DataPath = normPath
			if newSvc, err := calendar.NewService(app.ctx, targetDir, app.cfg.ActiveEffectiveBaseName(), app.cfg.ActiveSplitMode()); err == nil {
				app.svc = newSvc
			}
		}
		app.persistConfig()
		app.reloadSessions()
		app.setStatusMsg("Настройки сохранены")
		app.activeModal = nil
		return true
	}
	return false
}

// ----------------- DATE RANGE MODAL -----------------

type ModalDateRange struct {
	fromStr string
	toStr   string
	focused int // 0: From, 1: To, 2: Apply, 3: Cancel
	errMsg  string
}

func NewModalDateRange(initFrom, initTo string) *ModalDateRange {
	if strings.TrimSpace(initFrom) == "" {
		initFrom = time.Now().Format("02.01.2006")
	}
	if strings.TrimSpace(initTo) == "" {
		initTo = time.Now().Format("02.01.2006")
	}
	return &ModalDateRange{
		fromStr: initFrom,
		toStr:   initTo,
		focused: 0,
	}
}

func (m *ModalDateRange) Draw(s *Screen) {
	w := 48
	h := 13
	if w > s.Width-2 {
		w = s.Width - 2
	}
	if h > s.Height-2 {
		h = s.Height - 2
	}
	x := (s.Width - w) / 2
	y := (s.Height - h) / 2
	if x < 1 {
		x = 1
	}
	if y < 1 {
		y = 1
	}

	s.DrawBox(x, y, w, h, "Выбор периода дат", FgCyan, BorderBright, BgModal, true)

	s.DrawText(x+2, y+1, "Укажите диапазон дат для фильтрации записей:", FgMuted, BgModal, w-4, false, false, false)

	// Field 0: From
	f0Bg := BgCard
	f0Fg := FgMain
	if m.focused == 0 {
		f0Bg = BgSelected
		f0Fg = FgWhite
	}
	s.DrawText(x+2, y+3, "Дата начала (От):", FgDim, BgModal, 18, false, false, false)
	val0W := (x + w - 2) - (x + 20)
	if val0W > 16 {
		val0W = 16
	}
	if val0W > 0 {
		s.Fill(x+20, y+3, val0W, 1, ' ', f0Fg, f0Bg)
		s.DrawText(x+21, y+3, m.fromStr, f0Fg, f0Bg, val0W-1, m.focused == 0, false, false)
	}

	// Field 1: To
	f1Bg := BgCard
	f1Fg := FgMain
	if m.focused == 1 {
		f1Bg = BgSelected
		f1Fg = FgWhite
	}
	s.DrawText(x+2, y+5, "Дата конца (До):", FgDim, BgModal, 18, false, false, false)
	val1W := (x + w - 2) - (x + 20)
	if val1W > 16 {
		val1W = 16
	}
	if val1W > 0 {
		s.Fill(x+20, y+5, val1W, 1, ' ', f1Fg, f1Bg)
		s.DrawText(x+21, y+5, m.toStr, f1Fg, f1Bg, val1W-1, m.focused == 1, false, false)
	}

	// Error message
	if m.errMsg != "" {
		s.DrawText(x+2, y+7, m.errMsg, FgRed, BgModal, w-4, true, false, false)
	}

	// Buttons
	btnY := y + 8
	btnApplyBg := BgPanelAlt
	btnApplyFg := FgMain
	if m.focused == 2 {
		btnApplyBg = BgHighlight
		btnApplyFg = FgWhite
	}
	applyLbl := "  Применить (Enter)  "
	applyW := len(applyLbl)
	if x+4+applyW <= x+w-2 {
		s.Fill(x+4, btnY, applyW, 1, ' ', btnApplyFg, btnApplyBg)
		s.DrawText(x+4, btnY, applyLbl, btnApplyFg, btnApplyBg, applyW, m.focused == 2, false, false)
	}

	btnCancelBg := BgPanelAlt
	btnCancelFg := FgMain
	if m.focused == 3 {
		btnCancelBg = BgHighlight
		btnCancelFg = FgWhite
	}
	cancelLbl := "  Отмена (Esc)  "
	cancelW := len(cancelLbl)
	cancelX := x + 26
	if cancelX+cancelW > x+w-2 {
		cancelX = x + w - 2 - cancelW
	}
	if cancelX > x+4+applyW && cancelX+cancelW <= x+w-2 {
		s.Fill(cancelX, btnY, cancelW, 1, ' ', btnCancelFg, btnCancelBg)
		s.DrawText(cancelX, btnY, cancelLbl, btnCancelFg, btnCancelBg, cancelW, m.focused == 3, false, false)
	}

	s.DrawBottomWrappedHint(x+2, y+h-2, w-4, 2, "[Tab / ↑↓] Переход  [Enter] Применить  [Esc] Отмена", FgDim, BgModal)
}

func (m *ModalDateRange) HandleKey(k Key, app *App) bool {
	switch {
	case k.Type == KeyEsc:
		app.activeModal = nil
		return true

	case k.Type == KeyTab || (k.Type == KeyDown && m.focused < 3):
		m.focused = (m.focused + 1) % 4
		return true

	case k.Type == KeyBackTab || (k.Type == KeyUp && m.focused > 0):
		m.focused = (m.focused + 3) % 4
		return true

	case k.Type == KeyLeft && m.focused == 3:
		m.focused = 2
		return true

	case k.Type == KeyRight && m.focused == 2:
		m.focused = 3
		return true

	case k.Type == KeyEnter:
		if m.focused == 3 {
			app.activeModal = nil
			return true
		}
		if m.focused == 0 {
			m.focused = 1
			return true
		}
		// Apply
		refNow := time.Now()
		if app != nil {
			refNow = app.today()
		}
		dFrom, err1 := parser.ParseDate(m.fromStr, refNow)
		dTo, err2 := parser.ParseDate(m.toStr, refNow)
		if err1 != nil || err2 != nil {
			m.errMsg = "Неверная дата! Пример: 01.10.2026"
			return true
		}
		if dFrom.After(dTo) {
			dFrom, dTo = dTo, dFrom
		}
		app.startDate = time.Date(dFrom.Year(), dFrom.Month(), dFrom.Day(), 0, 0, 0, 0, time.Local)
		app.endDate = time.Date(dTo.Year(), dTo.Month(), dTo.Day(), 23, 59, 59, 999999999, time.Local)
		app.periodMode = "custom"
		app.calYear = dFrom.Year()
		app.calMonth = int(dFrom.Month())
		app.calDay = dFrom.Day()
		app.rangeAnchor = nil
		app.reloadSessions()
		app.setStatusMsg(fmt.Sprintf("Выбран период: %s - %s", app.startDate.Format("02.01.2006"), app.endDate.Format("02.01.2006")))
		app.activeModal = nil
		return true

	case k.Type == KeyBackspace:
		m.errMsg = ""
		if m.focused == 0 && len(m.fromStr) > 0 {
			m.fromStr = m.fromStr[:len(m.fromStr)-1]
		} else if m.focused == 1 && len(m.toStr) > 0 {
			m.toStr = m.toStr[:len(m.toStr)-1]
		}
		return true

	default:
		if k.Rune != 0 && ((k.Rune >= '0' && k.Rune <= '9') || k.Rune == '.' || k.Rune == '-' || k.Rune == '/' || k.Rune == ' ') {
			m.errMsg = ""
			if m.focused == 0 && len(m.fromStr) < 12 {
				m.fromStr += string(k.Rune)
			} else if m.focused == 1 && len(m.toStr) < 12 {
				m.toStr += string(k.Rune)
			}
			return true
		}
	}
	return false
}

// ----------------- HELP MODAL -----------------

type ModalHelp struct {
	scrollOffset int
}

func NewModalHelp() *ModalHelp {
	return &ModalHelp{
		scrollOffset: 0,
	}
}

func (m *ModalHelp) Draw(s *Screen) {
	w := 66
	h := 23
	if w > s.Width-2 {
		w = s.Width - 2
	}
	if h > s.Height-2 {
		h = s.Height - 2
	}
	x := (s.Width - w) / 2
	y := (s.Height - h) / 2
	if x < 1 {
		x = 1
	}
	if y < 1 {
		y = 1
	}

	s.DrawBox(x, y, w, h, "Справка по горячим клавишам MyCal TUI", FgGreen, BorderBright, BgModal, true)

	lines := []struct {
		key  string
		desc string
	}{
		{"↑ / ↓ / ← / →", "Навигация по записям и дням календаря"},
		{"Space (Пробел)", "Выбор записи [✓] / фиксация периода в календаре"},
		{"Enter", "Карточка записи / применить выбор дня"},
		{"v / м", "Быстрое переключение статуса по кругу (вкл. ~пусто~)"},
		{"s / ы", "Окно выбора статуса (Активно/Завершено/~пусто~/...)"},
		{"c / с", "Дублировать выбранную запись (клон в 1 клавишу)"},
		{"m / ь", "Перенести запись (или отмеченные) в другой календарь"},
		{"u / г", "Отменить последнее действие (Undo)"},
		{"x / ч, X / Ч", "Экспорт в CSV [x] / Импорт из CSV или JSON [Shift+X]"},
		{"b / и", "Календари записей (переключение / создание)"},
		{"l / д", "Сортировка записей (Дата, Время, Имя, Тип...)"},
		{"a / ф", "Создать новую запись"},
		{"e / у", "Редактировать выбранную запись"},
		{"f / а", "Быстрая правка любого поля (одиночно или пачкой)"},
		{"d / в", "Удалить выбранную запись (или все отмеченные)"},
		{"Tab / Shift+Tab", "Переключение блоков: Календарь / Периоды / Записи / Карточка"},
		{"F1 / F2 / F3 / F4", "Календарь / Категории / Записи / Статистика (в узком: вкладки)"},
		{"[ / ], х / ъ", "Листание разделов влево/вправо (в вертикальном/узком окне)"},
		{"< / >, б / ю", "Листание месяцев календаря назад/вперёд"},
		{"p / з", "Ввод произвольного периода дат (От и До)"},
		{"t / е", "Блок категорий (переход / перебор фильтра)"},
		{"Home", "Возврат календаря на Сегодня"},
		{"1 .. 4", "Быстрый выбор периода: Все / День / Неделя / Месяц"},
		{"y / н", "Годовой календарь со всеми месяцами"},
		{"o / щ", "Настройки MyCal (режим, служба, порт, хранение...)"},
		{"?", "Справка по горячим клавишам (это окно)"},
		{"q / й, Ctrl+C", "Выход из полноэкранного режима TUI"},
	}

	maxRows := h - 4
	if maxRows < 1 {
		maxRows = 1
	}
	if m.scrollOffset > len(lines)-maxRows {
		m.scrollOffset = len(lines) - maxRows
	}
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}

	for i := 0; i < maxRows && (i+m.scrollOffset) < len(lines); i++ {
		item := lines[i+m.scrollOffset]
		rowY := y + 2 + i
		keyW := 18
		if w < 50 {
			keyW = 14
		}
		s.DrawText(x+2, rowY, PadRight(item.key, keyW), FgCyan, BgModal, keyW, true, false, false)
		descW := (x + w - 2) - (x + 2 + keyW)
		if descW > 0 {
			s.DrawText(x+2+keyW, rowY, item.desc, FgMain, BgModal, descW, false, false, false)
		}
	}

	if m.scrollOffset > 0 {
		s.DrawText(x+w-3, y+1, "▲", FgYellow, BgModal, 1, false, false, false)
	}
	if m.scrollOffset+maxRows < len(lines) {
		s.DrawText(x+w-3, y+h-2, "▼", FgYellow, BgModal, 1, false, false, false)
	}

	s.DrawBottomWrappedHint(x+2, y+h-2, w-5, 2, "[↑↓] Прокрутка  [Esc / Enter / q / ?] Закрыть", FgDim, BgModal)
}

func (m *ModalHelp) HandleKey(k Key, app *App) bool {
	switch {
	case k.Type == KeyEsc || k.Type == KeyEnter || k.Matches('q', 'й') || k.Matches('h', 'р') || k.Matches('?', '?'):
		app.activeModal = nil
		return true
	case k.Type == KeyUp || k.Matches('k', 'л'):
		if m.scrollOffset > 0 {
			m.scrollOffset--
		}
		return true
	case k.Type == KeyDown || k.Matches('j', 'о'):
		m.scrollOffset++
		return true
	}
	return false
}

// ----------------- SORT MODAL -----------------

type ModalSortItem struct {
	Key   string
	Label string
}

type ModalSort struct {
	items    []ModalSortItem
	selected int
	appRef   *App
}

func NewModalSort(app *App) *ModalSort {
	items := []ModalSortItem{
		{"date", "Дата"},
		{"time", "Время"},
		{"name", "Название"},
		{"type", "Категория (Тип)"},
		{"duration", "Длительность"},
		{"cash", "Кэш (Сумма)"},
		{"status", "Статус"},
	}
	sel := 0
	curKey := "date"
	if app != nil && app.sortField != "" {
		curKey = app.sortField
	}
	for i, it := range items {
		if it.Key == curKey {
			sel = i
			break
		}
	}
	return &ModalSort{
		items:    items,
		selected: sel,
		appRef:   app,
	}
}

func (m *ModalSort) Draw(s *Screen) {
	w := 54
	h := len(m.items) + 6
	if w > s.Width-2 {
		w = s.Width - 2
	}
	if h > s.Height-2 {
		h = s.Height - 2
	}
	x := (s.Width - w) / 2
	y := (s.Height - h) / 2
	if x < 1 {
		x = 1
	}
	if y < 1 {
		y = 1
	}

	s.DrawBox(x, y, w, h, "Сортировка записей", FgCyan, BorderBright, BgModal, true)
	s.DrawText(x+2, y+1, "Выберите поле для сортировки записей:", FgMuted, BgModal, w-4, false, false, false)

	for i, it := range m.items {
		rowY := y + 3 + i
		if rowY >= y+h-2 {
			break
		}
		isCur := i == m.selected
		bg := BgModal
		fg := FgMain
		prefix := "  "
		if isCur {
			bg = BgSelected
			fg = FgWhite
			prefix = "► "
		}

		s.Fill(x+2, rowY, w-4, 1, ' ', fg, bg)
		numStr := fmt.Sprintf("[%d] ", i+1)
		s.DrawText(x+2, rowY, prefix+numStr, FgCyan, bg, 8, isCur, false, false)

		lbl := it.Label
		s.DrawText(x+8, rowY, lbl, fg, bg, 18, isCur, false, false)

		// Direction arrow & badge
		if m.isFieldActive(it.Key) {
			dirText := "▼ (сверху вниз)"
			arrowFg := FgYellow
			if m.isAscending() {
				dirText = "▲ (снизу вверх)"
				arrowFg = FgGreen
			}
			if it.Key == "date" {
				if m.isAscending() {
					dirText = "▲ сначала старые (время ↑)"
				} else {
					dirText = "▼ сначала новые (время ↑)"
				}
			}
			dirX := x + 26
			dirW := (x + w - 3) - dirX
			if dirW > 0 {
				s.DrawText(dirX, rowY, dirText, arrowFg, bg, dirW, isCur, false, false)
			}
		}
	}

	s.DrawBottomWrappedHint(x+2, y+h-2, w-4, 2, "[↑↓/1-7] Выбор  [Enter/Space] Сменить порядок  [Esc] Отмена", FgDim, BgModal)
}

func (m *ModalSort) isFieldActive(key string) bool {
	if m.appRef != nil {
		return m.appRef.sortField == key
	}
	return false
}

func (m *ModalSort) isAscending() bool {
	if m.appRef != nil {
		return m.appRef.sortAsc
	}
	return false
}

func (m *ModalSort) HandleKey(k Key, app *App) bool {
	m.appRef = app
	switch {
	case k.Type == KeyEsc:
		app.activeModal = nil
		return true

	case k.Type == KeyUp || k.Matches('k', 'л'):
		if m.selected > 0 {
			m.selected--
		} else {
			m.selected = len(m.items) - 1
		}
		return true

	case k.Type == KeyDown || k.Matches('j', 'о'):
		if m.selected < len(m.items)-1 {
			m.selected++
		} else {
			m.selected = 0
		}
		return true

	case k.Type == KeyRune && k.Rune >= '1' && k.Rune <= '7':
		idx := int(k.Rune - '1')
		if idx >= 0 && idx < len(m.items) {
			m.applySort(app, idx)
		}
		return true

	case k.Type == KeyEnter || k.Type == KeySpace:
		m.applySort(app, m.selected)
		return true
	}
	return false
}

func (m *ModalSort) applySort(app *App, idx int) {
	if idx < 0 || idx >= len(m.items) {
		return
	}
	chosen := m.items[idx]
	if app.sortField == chosen.Key {
		// Toggle direction if same field is chosen again!
		app.sortAsc = !app.sortAsc
	} else {
		app.sortField = chosen.Key
		if chosen.Key == "date" || chosen.Key == "duration" || chosen.Key == "cash" {
			app.sortAsc = false
		} else {
			app.sortAsc = true
		}
	}

	if app.cfg != nil {
		app.cfg.SortField = app.sortField
		app.cfg.SortAsc = app.sortAsc
		app.persistConfig()
	}

	dirText := "▼ (сверху вниз)"
	if app.sortAsc {
		dirText = "▲ (снизу вверх)"
	}
	if app.sortField == "date" {
		if app.sortAsc {
			dirText = "▲ (сначала старые, время ↑)"
		} else {
			dirText = "▼ сначала новые (время ↑)"
		}
	}

	app.reloadSessions()
	app.setStatusMsg(fmt.Sprintf("Сортировка: %s %s", chosen.Label, dirText))
	app.activeModal = nil
}

// ----------------- CALENDAR MANAGER MODAL -----------------

type ModalCalendarManager struct {
	calendars  []models.CalendarInfo
	baseName   string
	selected   int
	scroll     int
	creating   bool
	newKey     string
	newSplit   string
	focusField int // 0: newKey, 1: newSplit, 2: Create btn, 3: Cancel btn
	errMsg     string
}

func NewModalCalendarManager(app *App) *ModalCalendarManager {
	var cals []models.CalendarInfo
	base := "mycal"
	if app != nil {
		if app.cfg != nil && strings.TrimSpace(app.cfg.DataFileName) != "" {
			if clean := config.SanitizeBaseName(app.cfg.DataFileName); clean != "" {
				base = clean
			}
		}
		if app.svc != nil {
			cals = app.svc.ListCalendars(app.cfg)
		}
	}
	sel := 0
	for i, c := range cals {
		if c.Active {
			sel = i
			break
		}
	}
	return &ModalCalendarManager{
		calendars: cals,
		baseName:  base,
		selected:  sel,
		newSplit:  models.SplitNone,
	}
}

func (m *ModalCalendarManager) refreshList(app *App) {
	if app == nil || app.svc == nil {
		return
	}
	m.calendars = app.svc.ListCalendars(app.cfg)
	for i, c := range m.calendars {
		if c.Active {
			m.selected = i
			break
		}
	}
}

func (m *ModalCalendarManager) Draw(s *Screen) {
	if m.creating {
		m.drawCreateView(s)
		return
	}

	totalRows := len(m.calendars) + 1 // +1 for "+ Создать календарь записей..."
	visibleRows := totalRows
	if visibleRows > 10 {
		visibleRows = 10
	}
	w := 62
	h := visibleRows + 7
	if w > s.Width-2 {
		w = s.Width - 2
	}
	if h > s.Height-2 {
		h = s.Height - 2
	}
	x := (s.Width - w) / 2
	y := (s.Height - h) / 2
	if x < 1 {
		x = 1
	}
	if y < 1 {
		y = 1
	}

	s.DrawBox(x, y, w, h, "Календари записей [b]", FgCyan, BorderBright, BgModal, true)
	s.DrawText(x+2, y+1, "Выбор переключает календарь в реальном времени:", FgMuted, BgModal, w-4, false, false, false)

	maxListRows := h - 6
	if maxListRows < 1 {
		maxListRows = 1
	}
	if m.selected < m.scroll {
		m.scroll = m.selected
	} else if m.selected >= m.scroll+maxListRows {
		m.scroll = m.selected - maxListRows + 1
	}
	if m.scroll < 0 {
		m.scroll = 0
	}

	for row := 0; row < maxListRows; row++ {
		idx := m.scroll + row
		if idx >= totalRows {
			break
		}
		rowY := y + 3 + row
		isCur := idx == m.selected

		if idx < len(m.calendars) {
			cal := m.calendars[idx]
			bg := BgModal
			fg := FgMain
			prefix := "  "
			if isCur {
				bg = BgSelected
				fg = FgWhite
				prefix = "► "
			} else if cal.Active {
				fg = FgCyan
				prefix = "• "
			}
			s.Fill(x+2, rowY, w-4, 1, ' ', fg, bg)
			numStr := fmt.Sprintf("[%d] ", idx+1)
			s.DrawText(x+2, rowY, prefix+numStr, FgCyan, bg, 8, isCur, false, false)

			modeLabel := config.SplitModeLabels[cal.SplitMode]
			if modeLabel == "" {
				modeLabel = config.SplitModeLabels[models.SplitNone]
			}
			rightBadge := "[" + modeLabel + "]"
			rightW := StringVisualWidth(rightBadge)
			nameAvail := (w - 4) - 8 - rightW - 2
			if nameAvail < 8 {
				nameAvail = 8
			}
			s.DrawText(x+9, rowY, TruncateString(cal.Label, nameAvail, "…"), fg, bg, nameAvail, isCur || cal.Active, false, false)
			s.DrawText(x+w-2-rightW, rowY, rightBadge, FgYellow, bg, rightW, isCur, false, false)
		} else {
			// "+ Создать календарь записей..."
			bg := BgModal
			fg := FgGreen
			prefix := "  "
			if isCur {
				bg = BgSelected
				fg = FgWhite
				prefix = "► "
			}
			s.Fill(x+2, rowY, w-4, 1, ' ', fg, bg)
			s.DrawText(x+2, rowY, prefix+"+ Создать календарь записей...", fg, bg, w-4, isCur, false, false)
		}
	}

	hint := "[↑↓/1-9] Выбор (сразу)  [+ / n] Создать  [m] Режим (файл/год)  [Enter/Esc] Готово"
	s.DrawBottomWrappedHint(x+2, y+h-2, w-4, 2, hint, FgDim, BgModal)
}

func (m *ModalCalendarManager) drawCreateView(s *Screen) {
	w := 58
	h := 14
	if w > s.Width-2 {
		w = s.Width - 2
	}
	if h > s.Height-2 {
		h = s.Height - 2
	}
	x := (s.Width - w) / 2
	y := (s.Height - h) / 2
	if x < 1 {
		x = 1
	}
	if y < 1 {
		y = 1
	}

	s.DrawBox(x, y, w, h, "Создать календарь записей", FgGreen, BorderBright, BgModal, true)

	// Field 0: Prefix
	lbl0Fg := FgMuted
	if m.focusField == 0 {
		lbl0Fg = FgYellow
	}
	s.DrawText(x+2, y+2, "Префикс имени (напр. work, sport):", lbl0Fg, BgModal, w-4, m.focusField == 0, false, false)
	inpBg := BgInput
	if m.focusField == 0 {
		inpBg = BgSelected
	}
	s.Fill(x+2, y+3, w-4, 1, ' ', FgWhite, inpBg)
	dispKey := m.newKey
	if m.focusField == 0 {
		dispKey += "█"
	}
	s.DrawText(x+3, y+3, dispKey, FgWhite, inpBg, w-6, true, false, false)

	// Preview filename
	base := m.baseName
	if base == "" {
		base = "mycal"
	}
	previewKey := config.SanitizeCalendarKey(m.newKey)
	if previewKey == "" {
		previewKey = "префикс"
	}
	previewFile := fmt.Sprintf("%s--%s.json", base, previewKey)
	if m.newSplit == models.SplitYear {
		previewFile = fmt.Sprintf("%s_%s--%s.json", time.Now().Format("2006"), base, previewKey)
	} else if m.newSplit == models.SplitMonth {
		previewFile = fmt.Sprintf("%s_%s--%s.json", time.Now().Format("2006-01"), base, previewKey)
	}
	s.DrawText(x+2, y+4, "Файл: "+previewFile, FgDim, BgModal, w-4, false, false, false)

	// Field 1: Split mode
	lbl1Fg := FgMuted
	if m.focusField == 1 {
		lbl1Fg = FgYellow
	}
	s.DrawText(x+2, y+6, "Режим хранения:", lbl1Fg, BgModal, 18, m.focusField == 1, false, false)
	splitBg := BgInput
	if m.focusField == 1 {
		splitBg = BgSelected
	}
	splitLbl := "◄ " + config.SplitModeLabels[m.newSplit] + " ►"
	s.Fill(x+20, y+6, w-22, 1, ' ', FgCyan, splitBg)
	s.DrawText(x+21, y+6, splitLbl, FgCyan, splitBg, w-24, true, false, false)

	if m.errMsg != "" {
		s.DrawText(x+2, y+8, m.errMsg, FgRed, BgModal, w-4, true, false, false)
	}

	// Buttons
	btnY := y + h - 4
	createBg := BgInput
	createFg := FgGreen
	if m.focusField == 2 {
		createBg = BgSelected
		createFg = FgWhite
	}
	cancelBg := BgInput
	cancelFg := FgMuted
	if m.focusField == 3 {
		cancelBg = BgSelected
		cancelFg = FgWhite
	}
	s.DrawText(x+4, btnY, " [ Создать ] ", createFg, createBg, 14, true, false, false)
	s.DrawText(x+20, btnY, " [ Отмена ] ", cancelFg, cancelBg, 13, false, false, false)

	s.DrawBottomWrappedHint(x+2, y+h-2, w-4, 2, "[Tab/↑↓] Поле  [←→/Space] Режим  [Enter] Создать  [Esc] Назад", FgDim, BgModal)
}

func (m *ModalCalendarManager) switchRealtime(app *App, idx int) {
	if app == nil || app.svc == nil || idx < 0 || idx >= len(m.calendars) {
		return
	}
	target := m.calendars[idx]
	if err := app.svc.SwitchCalendar(app.ctx, app.cfg, target.Key); err != nil {
		app.setStatusMsg("Ошибка переключения календаря: " + err.Error())
		return
	}
	app.lastDataMod = app.svc.DataFilesModTime()
	app.persistConfig()
	app.reloadSessions()
	m.calendars = app.svc.ListCalendars(app.cfg)
	app.setStatusMsg(fmt.Sprintf("Календарь: %s [%s] (%d зап.)",
		target.Label, config.SplitModeLabels[app.cfg.ActiveSplitMode()], len(app.allSessions)))
}

func (m *ModalCalendarManager) HandleKey(k Key, app *App) bool {
	if m.creating {
		switch {
		case k.Type == KeyEsc:
			m.creating = false
			m.errMsg = ""
			return true

		case k.Type == KeyTab || k.Type == KeyDown:
			m.focusField = (m.focusField + 1) % 4
			return true

		case k.Type == KeyBackTab || k.Type == KeyUp:
			m.focusField = (m.focusField + 3) % 4
			return true

		case (k.Type == KeyLeft || k.Type == KeyRight || k.Type == KeySpace) && m.focusField == 1:
			switch m.newSplit {
			case models.SplitNone:
				m.newSplit = models.SplitYear
			case models.SplitYear:
				m.newSplit = models.SplitMonth
			default:
				m.newSplit = models.SplitNone
			}
			return true

		case k.Type == KeyBackspace && m.focusField == 0:
			if len(m.newKey) > 0 {
				runes := []rune(m.newKey)
				m.newKey = string(runes[:len(runes)-1])
			}
			return true

		case k.Type == KeyRune && m.focusField == 0:
			m.newKey += string(k.Rune)
			m.errMsg = ""
			return true

		case k.Type == KeyEnter:
			if m.focusField == 3 {
				m.creating = false
				m.errMsg = ""
				return true
			}
			cleanKey := config.SanitizeCalendarKey(m.newKey)
			if cleanKey == "" {
				m.errMsg = "Введите непустой префикс календаря!"
				m.focusField = 0
				return true
			}
			if app != nil && app.svc != nil {
				if err := app.svc.CreateCalendar(app.ctx, app.cfg, cleanKey, m.newSplit); err != nil {
					m.errMsg = err.Error()
					return true
				}
				app.lastDataMod = app.svc.DataFilesModTime()
				app.persistConfig()
				app.reloadSessions()
				app.setStatusMsg(fmt.Sprintf("Создан и выбран календарь: %s [%s]",
					app.cfg.ActiveCalendarDisplayName(), config.SplitModeLabels[app.cfg.ActiveSplitMode()]))
			}
			m.creating = false
			m.refreshList(app)
			app.activeModal = nil
			return true
		}
		return true
	}

	totalRows := len(m.calendars) + 1
	switch {
	case k.Type == KeyEsc || k.Matches('b', 'и') || k.Matches('q', 'й'):
		app.activeModal = nil
		return true

	case k.Matches('+', '=') || k.Matches('n', 'т'):
		m.creating = true
		m.newKey = ""
		m.newSplit = models.SplitNone
		m.focusField = 0
		m.errMsg = ""
		return true

	case k.Matches('m', 'ь') && m.selected < len(m.calendars):
		// Cycle split mode for the highlighted calendar
		cal := m.calendars[m.selected]
		nextMode := models.SplitYear
		switch cal.SplitMode {
		case models.SplitNone:
			nextMode = models.SplitYear
		case models.SplitYear:
			nextMode = models.SplitMonth
		default:
			nextMode = models.SplitNone
		}
		if app != nil && app.cfg != nil && app.svc != nil {
			_ = app.svc.SwitchCalendar(app.ctx, app.cfg, cal.Key)
			app.cfg.SetCalendarSplitMode(cal.Key, nextMode)
			app.svc.SetMode(nextMode)
			if err := app.svc.Save(app.ctx); err != nil {
				app.setStatusMsg("⚠ Ошибка сохранения: " + err.Error())
				return true
			}
			app.lastDataMod = app.svc.DataFilesModTime()
			app.persistConfig()
			app.reloadSessions()
			m.calendars = app.svc.ListCalendars(app.cfg)
			app.setStatusMsg(fmt.Sprintf("Календарь %s: режим хранения → %s",
				cal.Label, config.SplitModeLabels[nextMode]))
		}
		return true

	case k.Type == KeyUp || k.Matches('k', 'л'):
		if m.selected > 0 {
			m.selected--
		} else {
			m.selected = totalRows - 1
		}
		if m.selected < len(m.calendars) {
			m.switchRealtime(app, m.selected)
		}
		return true

	case k.Type == KeyDown || k.Matches('j', 'о'):
		if m.selected < totalRows-1 {
			m.selected++
		} else {
			m.selected = 0
		}
		if m.selected < len(m.calendars) {
			m.switchRealtime(app, m.selected)
		}
		return true

	case k.Type == KeyRune && k.Rune >= '1' && k.Rune <= '9':
		idx := int(k.Rune - '1')
		if idx < len(m.calendars) {
			m.selected = idx
			m.switchRealtime(app, idx)
			app.activeModal = nil
		}
		return true

	case k.Type == KeyEnter || k.Type == KeySpace:
		if m.selected == len(m.calendars) {
			m.creating = true
			m.newKey = ""
			m.newSplit = models.SplitNone
			m.focusField = 0
			m.errMsg = ""
			return true
		}
		m.switchRealtime(app, m.selected)
		if k.Type == KeyEnter {
			app.activeModal = nil
		}
		return true
	}
	return false
}

// ----------------- STATS MODAL (F4 in Wide Mode) -----------------

type ModalStats struct {
	appRef *App
}

func NewModalStats(app *App) *ModalStats {
	return &ModalStats{appRef: app}
}

func (m *ModalStats) Draw(s *Screen) {
	w := 62
	h := 24
	if w > s.Width-2 {
		w = s.Width - 2
	}
	if h > s.Height-2 {
		h = s.Height - 2
	}
	x := (s.Width - w) / 2
	y := (s.Height - h) / 2
	if x < 1 {
		x = 1
	}
	if y < 1 {
		y = 1
	}

	s.DrawBox(x, y, w, h, "Статистика за период", FgYellow, BorderFocus, BgModal, true)

	var sessions []models.Session
	periodMode := "all"
	if m.appRef != nil {
		sessions = m.appRef.sessions
		periodMode = m.appRef.periodMode
	}

	stSummary := calendar.ComputeStats(sessions)
	totalEntries := stSummary.TotalEntries
	totalMin := stSummary.TotalMinutes
	totalCash := stSummary.TotalCash
	statusCounts := stSummary.StatusCounts
	typeCounts := stSummary.TypeCounts
	totalHours := stSummary.TotalHours
	avgMin := int(stSummary.AvgMinutes)

	pLabel := "Все записи"
	switch periodMode {
	case "today", "day":
		pLabel = "Сегодня / День"
	case "week":
		pLabel = "Эта неделя"
	case "month":
		pLabel = "Этот месяц"
	case "custom":
		pLabel = "Выбранный период"
	}

	curY := y + 2
	s.DrawText(x+2, curY, fmt.Sprintf("Период:            %s", pLabel), FgWhite, BgModal, w-4, true, false, false)
	curY++
	s.DrawText(x+2, curY, fmt.Sprintf("Всего записей:     %d шт.", totalEntries), FgWhite, BgModal, w-4, true, false, false)
	curY++
	s.DrawText(x+2, curY, fmt.Sprintf("Общее время:       %.1f ч (%d мин)", totalHours, totalMin), FgCyan, BgModal, w-4, true, false, false)
	curY++
	s.DrawText(x+2, curY, fmt.Sprintf("Среднее на запись: %d мин (%02d:%02d)", avgMin, avgMin/60, avgMin%60), FgDim, BgModal, w-4, false, false, false)
	curY++
	cashFg := FgGreen
	if totalCash < 0 {
		cashFg = FgRed
	}
	s.DrawText(x+2, curY, fmt.Sprintf("Суммарный кэш:     %s", strconv.FormatFloat(totalCash, 'f', -1, 64)), cashFg, BgModal, w-4, true, false, false)
	curY += 2

	s.DrawHLine(x+1, x+w-2, curY, BoxH, BorderDim, BgModal)
	s.DrawText(x+2, curY, "┤ Распределение по статусам ├", FgDim, BgModal, w-4, false, false, false)
	curY++

	statusOrder := []string{"Активно", "Завершено", "Запланировано", "В процессе", "Отменено"}
	for _, st := range statusOrder {
		c := statusCounts[st]
		if c > 0 || totalEntries == 0 {
			bText, bFg, _ := StatusBadge(st)
			s.DrawText(x+2, curY, bText, bFg, BgModal, 16, true, false, false)
			s.DrawText(x+18, curY, fmt.Sprintf("%d записей", c), FgWhite, BgModal, w-20, false, false, false)
			curY++
		}
	}
	for st, c := range statusCounts {
		isDefault := false
		for _, def := range statusOrder {
			if def == st {
				isDefault = true
				break
			}
		}
		if !isDefault && c > 0 {
			bText, bFg, _ := StatusBadge(st)
			s.DrawText(x+2, curY, bText, bFg, BgModal, 16, true, false, false)
			s.DrawText(x+18, curY, fmt.Sprintf("%d записей", c), FgWhite, BgModal, w-20, false, false, false)
			curY++
		}
	}

	curY++
	if len(typeCounts) > 0 && curY < y+h-3 {
		s.DrawHLine(x+1, x+w-2, curY, BoxH, BorderDim, BgModal)
		s.DrawText(x+2, curY, "┤ Распределение по категориям ├", FgDim, BgModal, w-4, false, false, false)
		curY++
		for tName, c := range typeCounts {
			if curY >= y+h-2 {
				break
			}
			s.DrawText(x+2, curY, fmt.Sprintf("• %s:", tName), FgMuted, BgModal, 20, false, false, false)
			s.DrawText(x+24, curY, fmt.Sprintf("%d записей", c), FgCyan, BgModal, w-26, true, false, false)
			curY++
		}
	}

	s.DrawBottomWrappedHint(x+2, y+h-2, w-4, 2, "[Esc / Enter / F4] Закрыть окно статистики", FgDim, BgModal)
}

func (m *ModalStats) HandleKey(k Key, app *App) bool {
	switch {
	case k.Type == KeyEsc || k.Type == KeyEnter || k.Type == KeySpace || k.Type == KeyF4 || k.Matches('q', 'й'):
		app.activeModal = nil
		return true
	case k.Type == KeyF1:
		app.activeModal = nil
		app.focusPanel = PanelCalendar
		app.lastLeftPanel = PanelCalendar
		return true
	case k.Type == KeyF2:
		app.activeModal = nil
		app.focusPanel = PanelSidebar
		app.lastLeftPanel = PanelSidebar
		return true
	case k.Type == KeyF3:
		app.activeModal = nil
		app.focusPanel = PanelFeed
		return true
	}
	return false
}

// ----------------- MOVE TO CALENDAR MODAL [m] -----------------

type ModalMoveToCalendar struct {
	calendars     []models.CalendarInfo
	selected      int
	scroll        int
	targetIDs     []string
	isCustomInput bool
	customKey     string
	errMsg        string
}

func NewModalMoveToCalendar(app *App, targetIDs []string) *ModalMoveToCalendar {
	var other []models.CalendarInfo
	if app != nil && app.svc != nil {
		for _, c := range app.svc.ListCalendars(app.cfg) {
			if !c.Active {
				other = append(other, c)
			}
		}
	}
	return &ModalMoveToCalendar{
		calendars:     other,
		selected:      0,
		targetIDs:     targetIDs,
		isCustomInput: len(other) == 0,
	}
}

func (m *ModalMoveToCalendar) Draw(s *Screen) {
	if m.isCustomInput {
		w := 50
		h := 10
		if w > s.Width-2 {
			w = s.Width - 2
		}
		if h > s.Height-2 {
			h = s.Height - 2
		}
		x := (s.Width - w) / 2
		y := (s.Height - h) / 2
		if x < 1 {
			x = 1
		}
		if y < 1 {
			y = 1
		}
		s.DrawBox(x, y, w, h, "Перенос в календарь по префиксу", FgCyan, BorderBright, BgModal, true)
		s.DrawText(x+2, y+2, "Префикс целевого календаря (напр. work, sport):", FgMuted, BgModal, w-4, false, false, false)
		s.Fill(x+2, y+4, w-4, 1, ' ', FgWhite, BgInput)
		s.DrawBox(x+1, y+3, w-2, 3, "", FgDim, BorderFocus, BgInput, false)
		s.DrawText(x+3, y+4, m.customKey+"█", FgWhite, BgInput, w-6, true, false, false)
		if m.errMsg != "" {
			s.DrawText(x+2, y+6, m.errMsg, FgRed, BgModal, w-4, true, false, false)
		}
		s.DrawBottomWrappedHint(x+2, y+h-2, w-4, 2, "[Enter] Перенести  [Esc] Назад", FgDim, BgModal)
		return
	}

	totalRows := len(m.calendars) + 1
	w := 54
	h := totalRows + 7
	if h < 10 {
		h = 10
	}
	if h > 18 {
		h = 18
	}
	if w > s.Width-2 {
		w = s.Width - 2
	}
	if h > s.Height-2 {
		h = s.Height - 2
	}
	x := (s.Width - w) / 2
	y := (s.Height - h) / 2
	if x < 1 {
		x = 1
	}
	if y < 1 {
		y = 1
	}

	title := fmt.Sprintf("Перенос в другой календарь (%d зап.)", len(m.targetIDs))
	s.DrawBox(x, y, w, h, title, FgCyan, BorderBright, BgModal, true)
	s.DrawText(x+2, y+1, "Выберите целевой календарь:", FgMuted, BgModal, w-4, false, false, false)

	maxListRows := h - 6
	if maxListRows < 1 {
		maxListRows = 1
	}
	if m.selected < m.scroll {
		m.scroll = m.selected
	} else if m.selected >= m.scroll+maxListRows {
		m.scroll = m.selected - maxListRows + 1
	}

	for row := 0; row < maxListRows; row++ {
		idx := m.scroll + row
		if idx >= totalRows {
			break
		}
		rowY := y + 3 + row
		isCur := idx == m.selected
		bg := BgModal
		fg := FgMain
		prefix := "  "
		if isCur {
			bg = BgSelected
			fg = FgWhite
			prefix = "► "
		}
		s.Fill(x+2, rowY, w-4, 1, ' ', fg, bg)
		if idx < len(m.calendars) {
			cal := m.calendars[idx]
			numStr := fmt.Sprintf("[%d] ", idx+1)
			s.DrawText(x+2, rowY, prefix+numStr, FgCyan, bg, 8, isCur, false, false)
			s.DrawText(x+9, rowY, cal.Label, fg, bg, w-12, isCur, false, false)
		} else {
			if !isCur {
				fg = FgGreen
			}
			s.DrawText(x+2, rowY, prefix+"+ Ввести префикс календаря вручную...", fg, bg, w-4, isCur, false, false)
		}
	}

	s.DrawBottomWrappedHint(x+2, y+h-2, w-4, 2, "[↑↓/1-9] Выбор  [+] Свой префикс  [Enter] Перенести  [Esc] Отмена", FgDim, BgModal)
}

func (m *ModalMoveToCalendar) HandleKey(k Key, app *App) bool {
	if m.isCustomInput {
		switch {
		case k.Type == KeyEsc:
			if len(m.calendars) == 0 {
				app.activeModal = nil
				return true
			}
			m.isCustomInput = false
			m.errMsg = ""
			return true
		case k.Type == KeyBackspace:
			if len(m.customKey) > 0 {
				runes := []rune(m.customKey)
				m.customKey = string(runes[:len(runes)-1])
			}
			return true
		case k.Type == KeyRune:
			m.customKey += string(k.Rune)
			m.errMsg = ""
			return true
		case k.Type == KeyEnter:
			key := strings.TrimSpace(m.customKey)
			if strings.EqualFold(key, "default") || strings.EqualFold(key, "mycal") {
				key = ""
			}
			m.executeMove(app, key)
			return true
		}
		return true
	}

	totalRows := len(m.calendars) + 1
	switch {
	case k.Type == KeyEsc:
		app.activeModal = nil
		return true
	case k.Matches('+', '='):
		m.isCustomInput = true
		m.customKey = ""
		m.errMsg = ""
		return true
	case k.Type == KeyUp || k.Matches('k', 'л'):
		if m.selected > 0 {
			m.selected--
		} else {
			m.selected = totalRows - 1
		}
		return true
	case k.Type == KeyDown || k.Matches('j', 'о'):
		if m.selected < totalRows-1 {
			m.selected++
		} else {
			m.selected = 0
		}
		return true
	case k.Type == KeyRune && k.Rune >= '1' && k.Rune <= '9':
		idx := int(k.Rune - '1')
		if idx < len(m.calendars) {
			m.executeMove(app, m.calendars[idx].Key)
		}
		return true
	case k.Type == KeyEnter:
		if m.selected == len(m.calendars) {
			m.isCustomInput = true
			m.customKey = ""
			m.errMsg = ""
			return true
		}
		if m.selected >= 0 && m.selected < len(m.calendars) {
			m.executeMove(app, m.calendars[m.selected].Key)
		}
		return true
	}
	return false
}

func (m *ModalMoveToCalendar) executeMove(app *App, targetKey string) {
	if app == nil || app.svc == nil {
		return
	}
	moved, err := app.svc.MoveEntriesToCalendar(app.ctx, app.cfg, m.targetIDs, targetKey)
	if err != nil {
		if m.isCustomInput {
			m.errMsg = err.Error()
		} else {
			app.setStatusMsg("⚠ " + err.Error())
			app.activeModal = nil
		}
		return
	}
	app.selectedIDs = map[string]bool{}
	app.lastDataMod = app.svc.DataFilesModTime()
	app.persistConfig()
	app.reloadSessions()
	targetDisp := app.cfg.EffectiveBaseName(targetKey)
	app.setStatusMsg(fmt.Sprintf("Перенесено записей: %d → %s", moved, targetDisp))
	app.activeModal = nil
}

// ----------------- IMPORT CSV / JSON MODAL [Shift+X] -----------------

type ModalImport struct {
	files         []string
	selected      int
	scroll        int
	isCustomInput bool
	customPath    string
	errMsg        string
}

func NewModalImport(app *App) *ModalImport {
	var files []string
	seen := map[string]bool{}
	if app != nil && app.svc != nil {
		activeBase := filepath.Base(app.svc.MainDataFilePath())
		dirs := []string{app.svc.DataDir()}
		if wd, err := os.Getwd(); err == nil && wd != app.svc.DataDir() {
			dirs = append(dirs, wd)
		}
		for _, dir := range dirs {
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				name := e.Name()
				lower := strings.ToLower(name)
				if lower == "config_mycal.json" || name == activeBase {
					continue
				}
				if strings.HasSuffix(lower, ".csv") || strings.HasSuffix(lower, ".json") {
					full := filepath.Join(dir, name)
					if !seen[full] {
						seen[full] = true
						files = append(files, full)
					}
				}
			}
		}
		sort.Strings(files)
	}
	return &ModalImport{
		files:         files,
		selected:      0,
		isCustomInput: len(files) == 0,
	}
}

func (m *ModalImport) Draw(s *Screen) {
	if m.isCustomInput {
		w := 58
		h := 10
		if w > s.Width-2 {
			w = s.Width - 2
		}
		if h > s.Height-2 {
			h = s.Height - 2
		}
		x := (s.Width - w) / 2
		y := (s.Height - h) / 2
		if x < 1 {
			x = 1
		}
		if y < 1 {
			y = 1
		}
		s.DrawBox(x, y, w, h, "Импорт из файла (.csv / .json)", FgCyan, BorderBright, BgModal, true)
		s.DrawText(x+2, y+2, "Введите путь к файлу CSV или JSON:", FgMuted, BgModal, w-4, false, false, false)
		s.Fill(x+2, y+4, w-4, 1, ' ', FgWhite, BgInput)
		s.DrawBox(x+1, y+3, w-2, 3, "", FgDim, BorderFocus, BgInput, false)
		s.DrawText(x+3, y+4, m.customPath+"█", FgWhite, BgInput, w-6, true, false, false)
		if m.errMsg != "" {
			s.DrawText(x+2, y+6, m.errMsg, FgRed, BgModal, w-4, true, false, false)
		}
		s.DrawBottomWrappedHint(x+2, y+h-2, w-4, 2, "[Enter] Импортировать  [Esc] Назад", FgDim, BgModal)
		return
	}

	totalRows := len(m.files) + 1
	w := 62
	h := totalRows + 7
	if h < 10 {
		h = 10
	}
	if h > 18 {
		h = 18
	}
	if w > s.Width-2 {
		w = s.Width - 2
	}
	if h > s.Height-2 {
		h = s.Height - 2
	}
	x := (s.Width - w) / 2
	y := (s.Height - h) / 2
	if x < 1 {
		x = 1
	}
	if y < 1 {
		y = 1
	}

	s.DrawBox(x, y, w, h, "Импорт из CSV / JSON [Shift+X]", FgCyan, BorderBright, BgModal, true)
	s.DrawText(x+2, y+1, "Выберите файл для импорта (дубликаты пропускаются):", FgMuted, BgModal, w-4, false, false, false)

	maxListRows := h - 6
	if maxListRows < 1 {
		maxListRows = 1
	}
	if m.selected < m.scroll {
		m.scroll = m.selected
	} else if m.selected >= m.scroll+maxListRows {
		m.scroll = m.selected - maxListRows + 1
	}

	for row := 0; row < maxListRows; row++ {
		idx := m.scroll + row
		if idx >= totalRows {
			break
		}
		rowY := y + 3 + row
		isCur := idx == m.selected
		bg := BgModal
		fg := FgMain
		prefix := "  "
		if isCur {
			bg = BgSelected
			fg = FgWhite
			prefix = "► "
		}
		s.Fill(x+2, rowY, w-4, 1, ' ', fg, bg)
		if idx < len(m.files) {
			f := m.files[idx]
			numStr := fmt.Sprintf("[%d] ", idx+1)
			s.DrawText(x+2, rowY, prefix+numStr, FgCyan, bg, 8, isCur, false, false)
			s.DrawText(x+9, rowY, TruncateString(filepath.Base(f), w-12, "…"), fg, bg, w-12, isCur, false, false)
		} else {
			if !isCur {
				fg = FgGreen
			}
			s.DrawText(x+2, rowY, prefix+"+ Указать путь к файлу вручную...", fg, bg, w-4, isCur, false, false)
		}
	}

	s.DrawBottomWrappedHint(x+2, y+h-2, w-4, 2, "[↑↓/1-9] Выбор  [+] Указать путь  [Enter] Импорт  [Esc] Отмена", FgDim, BgModal)
}

func (m *ModalImport) HandleKey(k Key, app *App) bool {
	if m.isCustomInput {
		switch {
		case k.Type == KeyEsc:
			if len(m.files) == 0 {
				app.activeModal = nil
				return true
			}
			m.isCustomInput = false
			m.errMsg = ""
			return true
		case k.Type == KeyBackspace:
			if len(m.customPath) > 0 {
				runes := []rune(m.customPath)
				m.customPath = string(runes[:len(runes)-1])
			}
			return true
		case k.Type == KeySpace:
			m.customPath += " "
			return true
		case k.Type == KeyRune:
			m.customPath += string(k.Rune)
			m.errMsg = ""
			return true
		case k.Type == KeyEnter:
			m.executeImport(app, m.customPath)
			return true
		}
		return true
	}

	totalRows := len(m.files) + 1
	switch {
	case k.Type == KeyEsc:
		app.activeModal = nil
		return true
	case k.Matches('+', '='):
		m.isCustomInput = true
		m.customPath = ""
		m.errMsg = ""
		return true
	case k.Type == KeyUp || k.Matches('k', 'л'):
		if m.selected > 0 {
			m.selected--
		} else {
			m.selected = totalRows - 1
		}
		return true
	case k.Type == KeyDown || k.Matches('j', 'о'):
		if m.selected < totalRows-1 {
			m.selected++
		} else {
			m.selected = 0
		}
		return true
	case k.Type == KeyRune && k.Rune >= '1' && k.Rune <= '9':
		idx := int(k.Rune - '1')
		if idx < len(m.files) {
			m.executeImport(app, m.files[idx])
		}
		return true
	case k.Type == KeyEnter:
		if m.selected == len(m.files) {
			m.isCustomInput = true
			m.customPath = ""
			m.errMsg = ""
			return true
		}
		if m.selected >= 0 && m.selected < len(m.files) {
			m.executeImport(app, m.files[m.selected])
		}
		return true
	}
	return false
}

func (m *ModalImport) executeImport(app *App, path string) {
	if app == nil || app.svc == nil {
		return
	}
	res, err := app.svc.ImportFile(app.ctx, app.cfg, path)
	if err != nil {
		if m.isCustomInput {
			m.errMsg = err.Error()
		} else {
			app.setStatusMsg("⚠ Ошибка импорта: " + err.Error())
			app.activeModal = nil
		}
		return
	}
	app.lastDataMod = app.svc.DataFilesModTime()
	if res.ConfigMod {
		app.persistConfig()
	}
	app.reloadSessions()
	app.setStatusMsg(fmt.Sprintf("Импорт: добавлено %d, пропущено дубликатов %d (всего %d)", res.Added, res.Skipped, res.Total))
	app.activeModal = nil
}
