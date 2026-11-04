package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"mycalendar/calendar"
	"mycalendar/models"
	"mycalendar/parser"
	"mycalendar/service"
)

// Modal represents a floating dialog overlay on top of the main screen.
type Modal interface {
	Draw(s *Screen)
	HandleKey(k Key, app *App) bool // returns true if modal was closed or consumed
}

// ----------------- STATUS PICKER MODAL -----------------

type ModalStatusPicker struct {
	statuses []string
	selected int
	targetID string // empty if batch
}

func NewModalStatusPicker(customStatuses []string, currentStatus, targetID string) *ModalStatusPicker {
	defaults := []string{"Активно", "Завершено", "Запланировано", "В процессе", "Отменено"}
	var list []string
	seen := map[string]bool{}
	for _, st := range customStatuses {
		if strings.TrimSpace(st) != "" && !seen[st] {
			list = append(list, st)
			seen[st] = true
		}
	}
	for _, st := range defaults {
		if !seen[st] {
			list = append(list, st)
			seen[st] = true
		}
	}

	sel := 0
	for i, st := range list {
		if strings.EqualFold(st, currentStatus) {
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
	w := 38
	h := len(m.statuses) + 6
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

	hint := "[↑↓/1-9] Выбор  [Enter]  [Esc Отмена]"
	s.DrawBottomWrappedHint(x+2, y+h-2, w-4, 2, hint, FgDim, BgModal)
}

func (m *ModalStatusPicker) HandleKey(k Key, app *App) bool {
	switch {
	case k.Type == KeyEsc:
		app.activeModal = nil
		return true

	case k.Type == KeyUp || k.Matches('k', 'л'):
		if m.selected > 0 {
			m.selected--
		} else {
			m.selected = len(m.statuses) - 1
		}
		return true

	case k.Type == KeyDown || k.Matches('j', 'о'):
		if m.selected < len(m.statuses)-1 {
			m.selected++
		} else {
			m.selected = 0
		}
		return true

	case k.Type == KeyEnter:
		m.applyStatus(app, m.statuses[m.selected])
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
	if m.targetID != "" {
		// Single session update
		patch := calendar.Patch{
			Status: &status,
		}
		_ = app.svc.Update(m.targetID, patch)
		_ = app.svc.Save(app.ctx)
		app.setStatusMsg("Статус записи изменён на: " + status)
	} else if len(app.selectedIDs) > 0 {
		// Batch update
		count := 0
		for id := range app.selectedIDs {
			patch := calendar.Patch{
				Status: &status,
			}
			if err := app.svc.Update(id, patch); err == nil {
				count++
			}
		}
		_ = app.svc.Save(app.ctx)
		app.setStatusMsg(fmt.Sprintf("Статус обновлён для %d записей", count))
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

	// Legacy aliases for test compatibility
	catPickerOpen   bool
	catPickerIdx    int
	catPickerScroll int
	catPickerList   []string
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
			s.DrawBottomWrappedHint(x+2, y+h-2, w-4, 2, "[↑↓] Выбор из списка  [Enter] Применить  [Esc/←] Закрыть", FgDim, BgModal)
		} else if canHaveDropdown {
			s.DrawBottomWrappedHint(x+2, y+h-2, w-4, 2, "[Enter] Сохранить  [→] Выбрать из списка  [Esc] Назад", FgDim, BgModal)
		} else {
			s.DrawBottomWrappedHint(x+2, y+h-2, w-4, 2, "[Enter] Сохранить  [Esc] Назад", FgDim, BgModal)
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
				m.catPickerOpen = false
				return true
			case k.Type == KeyUp || k.Matches('k', 'л'):
				if m.pickerIdx > 0 {
					m.pickerIdx--
					if m.pickerIdx < m.pickerScroll {
						m.pickerScroll = m.pickerIdx
					}
					m.catPickerIdx = m.pickerIdx
					m.catPickerScroll = m.pickerScroll
				}
				return true
			case k.Type == KeyDown || k.Matches('j', 'о'):
				if m.pickerIdx < len(m.pickerList)-1 {
					m.pickerIdx++
					visibleRows := 5
					if m.pickerIdx >= m.pickerScroll+visibleRows {
						m.pickerScroll = m.pickerIdx - visibleRows + 1
					}
					m.catPickerIdx = m.pickerIdx
					m.catPickerScroll = m.pickerScroll
				}
				return true
			case k.Type == KeyEnter || k.Type == KeySpace:
				if m.pickerIdx >= 0 && m.pickerIdx < len(m.pickerList) {
					m.value = m.pickerList[m.pickerIdx]
				}
				m.pickerOpen = false
				m.catPickerOpen = false
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

	defaults := []string{"Работа", "Личное", "Спорт", "Отдых", "Здоровье", "Обучение", "Хобби"}
	if app != nil && app.cfg.DefaultType != "" {
		defaults = append([]string{app.cfg.DefaultType}, defaults...)
	}
	for _, d := range defaults {
		if !seen[d] {
			seen[d] = true
			list = append(list, d)
		}
	}

	m.pickerList = list
	m.pickerTitle = " Категории [Enter] "
	m.pickerField = 2
	m.pickerOpen = true
	m.pickerScroll = 0
	m.pickerIdx = 0

	m.catPickerList = list
	m.catPickerOpen = true
	m.catPickerScroll = 0
	m.catPickerIdx = 0

	cur := strings.TrimSpace(m.value)
	for i, item := range m.pickerList {
		if strings.EqualFold(item, cur) {
			m.pickerIdx = i
			m.catPickerIdx = i
			if m.pickerIdx >= 5 {
				m.pickerScroll = m.pickerIdx - 4
				m.catPickerScroll = m.pickerScroll
			}
			break
		}
	}
}

func (m *ModalQuickEditField) openStatusPicker(app *App) {
	var list []string
	if app != nil {
		list = app.getAvailableStatuses()
	} else {
		list = []string{"Активно", "Завершено", "Запланировано", "В процессе", "Отменено"}
	}
	m.pickerList = list
	m.pickerTitle = " Статусы [Enter] "
	m.pickerField = 3
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

	switch m.selected {
	case 0: // Name
		patch.Name = &val
	case 1: // Cash
		if c, err := strconv.ParseFloat(val, 64); err == nil {
			patch.Cash = &c
		} else {
			zero := 0.0
			patch.Cash = &zero
		}
	case 2: // Type
		patch.Type = &val
	case 3: // Status
		patch.Status = &val
	case 4: // Time
		if tm, err := parser.ParseTime(val); err == nil {
			tStr := parser.FormatTime(tm)
			patch.Time = &tStr
		}
	case 5: // Duration
		if d, err := strconv.Atoi(val); err == nil && d > 0 {
			patch.Duration = &d
		}
	case 7: // Notes
		patch.Notes = &val
	}

	if m.targetID != "" {
		if m.selected == 6 { // Date move
			if d, err := parser.ParseDate(val, time.Now()); err == nil {
				sess, ok := app.svc.ByID(m.targetID)
				if ok {
					tm, _ := parser.ParseTime(sess.Time)
					_ = app.svc.Delete(sess.ID)
					sess.ID = app.svc.GenerateID(d, tm)
					_ = app.svc.Add(sess)
				}
			}
		} else {
			_ = app.svc.Update(m.targetID, patch)
		}
		_ = app.svc.Save(app.ctx)
		app.setStatusMsg("Поле успешно обновлено")
	} else if len(app.selectedIDs) > 0 {
		count := 0
		for id := range app.selectedIDs {
			if err := app.svc.Update(id, patch); err == nil {
				count++
			}
		}
		_ = app.svc.Save(app.ctx)
		app.selectedIDs = map[string]bool{}
		app.setStatusMsg(fmt.Sprintf("Поле обновлено для %d записей", count))
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

	// Legacy aliases for test compatibility
	catPickerOpen   bool
	catPickerIdx    int
	catPickerScroll int
	catPickerList   []string
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
			status:    sess.Status,
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
		s.DrawBottomWrappedHint(x+2, y+h-2, w-4, 2, "[↑↓] Выбор из списка  [Enter] Выбрать  [Esc/←] Закрыть", FgDim, BgModal)
	} else {
		switch m.fieldIdx {
		case 1:
			s.DrawBottomWrappedHint(x+2, y+h-2, w-4, 2, "[Tab] Поля  [→] Выбрать время  [Enter] Сохранить  [Esc] Отмена", FgDim, BgModal)
		case 2:
			s.DrawBottomWrappedHint(x+2, y+h-2, w-4, 2, "[Tab] Поля  [→] Выбрать из списка  [Enter] Сохранить  [Esc] Отмена", FgDim, BgModal)
		case 3:
			s.DrawBottomWrappedHint(x+2, y+h-2, w-4, 2, "[Tab] Поля  [→] Выбрать из списка  [Enter] Сохранить  [Esc] Отмена", FgDim, BgModal)
		case 6:
			s.DrawBottomWrappedHint(x+2, y+h-2, w-4, 2, "[Tab] Поля  [→] Выбрать из списка  [Space] След. статус  [Enter] Сохранить  [Esc] Отмена", FgDim, BgModal)
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
		if popX+popW > s.Width-1 {
			popW = s.Width - 1 - popX
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
			m.catPickerOpen = false
			return true

		case k.Type == KeyUp || k.Matches('k', 'л'):
			if m.pickerIdx > 0 {
				m.pickerIdx--
				if m.pickerIdx < m.pickerScroll {
					m.pickerScroll = m.pickerIdx
				}
				m.catPickerIdx = m.pickerIdx
				m.catPickerScroll = m.pickerScroll
			}
			return true

		case k.Type == KeyDown || k.Matches('j', 'о'):
			if m.pickerIdx < len(m.pickerList)-1 {
				m.pickerIdx++
				visibleRows := 6
				if m.pickerIdx >= m.pickerScroll+visibleRows {
					m.pickerScroll = m.pickerIdx - visibleRows + 1
				}
				m.catPickerIdx = m.pickerIdx
				m.catPickerScroll = m.pickerScroll
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
			m.catPickerOpen = false
			return true

		case k.Type == KeyTab:
			m.pickerOpen = false
			m.catPickerOpen = false
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
		m.save(app)
		app.activeModal = nil
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

	defaults := []string{"Работа", "Личное", "Спорт", "Отдых", "Здоровье", "Обучение", "Хобби"}
	if app != nil && app.cfg.DefaultType != "" {
		defaults = append([]string{app.cfg.DefaultType}, defaults...)
	}
	for _, d := range defaults {
		if !seen[d] {
			seen[d] = true
			list = append(list, d)
		}
	}

	m.pickerList = list
	m.pickerTitle = " Категории [Enter] "
	m.pickerField = 3
	m.pickerOpen = true
	m.pickerScroll = 0
	m.pickerIdx = 0

	m.catPickerList = list
	m.catPickerOpen = true
	m.catPickerScroll = 0
	m.catPickerIdx = 0

	cur := strings.TrimSpace(m.typeStr)
	for i, item := range m.pickerList {
		if strings.EqualFold(item, cur) {
			m.pickerIdx = i
			m.catPickerIdx = i
			if m.pickerIdx >= 6 {
				m.pickerScroll = m.pickerIdx - 5
				m.catPickerScroll = m.pickerScroll
			}
			break
		}
	}
}

func (m *ModalSessionForm) openStatusPicker(app *App) {
	var list []string
	if app != nil {
		list = app.getAvailableStatuses()
	} else {
		list = []string{"Активно", "Завершено", "Запланировано", "В процессе", "Отменено"}
	}
	m.pickerList = list
	m.pickerTitle = " Статусы [Enter] "
	m.pickerField = 6
	m.pickerOpen = true
	m.pickerScroll = 0
	m.pickerIdx = 0

	cur := strings.TrimSpace(m.status)
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
	order := []string{"Активно", "Завершено", "Запланировано", "В процессе", "Отменено"}
	if app != nil {
		order = app.getAvailableStatuses()
	}
	for i, st := range order {
		if strings.EqualFold(st, m.status) {
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

func (m *ModalSessionForm) save(app *App) {
	d, err := parser.ParseDate(strings.TrimSpace(m.dateStr), time.Now())
	if err != nil {
		d = time.Now()
	}
	tm, err := parser.ParseTime(strings.TrimSpace(m.timeStr))
	if err != nil {
		tm, _ = parser.ParseTime("10:00")
	}

	dur, _ := strconv.Atoi(strings.TrimSpace(m.durStr))
	if dur <= 0 {
		dur = 60
	}

	cashVal, _ := strconv.ParseFloat(strings.TrimSpace(m.cashStr), 64)

	nameVal := strings.TrimSpace(m.name)
	if nameVal == "" {
		nameVal = "Запись"
	}
	typeVal := strings.TrimSpace(m.typeStr)
	statusVal := strings.TrimSpace(m.status)
	if statusVal == "" {
		statusVal = "Активно"
	}
	notesVal := strings.TrimSpace(m.notes)

	var savedID string
	if m.isEdit {
		sess, ok := app.svc.ByID(m.targetID)
		if ok {
			origDate := sess.Date()
			newDate := parser.FormatDate(d)
			patch := calendar.Patch{
				Name:     &nameVal,
				Type:     &typeVal,
				Duration: &dur,
				Cash:     &cashVal,
				Status:   &statusVal,
				Notes:    &notesVal,
			}
			tStr := parser.FormatTime(tm)
			patch.Time = &tStr

			if origDate != newDate {
				_ = app.svc.Delete(sess.ID)
				savedID = app.svc.GenerateID(d, tm)
				sess.ID = savedID
				sess.Time = tStr
				sess.Name = nameVal
				sess.Type = typeVal
				sess.Duration = dur
				sess.Cash = cashVal
				sess.Status = statusVal
				sess.Notes = notesVal
				_ = app.svc.Add(sess)
				app.calYear = d.Year()
				app.calMonth = int(d.Month())
				app.calDay = d.Day()
			} else {
				savedID = sess.ID
				_ = app.svc.Update(sess.ID, patch)
			}
			_ = app.svc.Save(app.ctx)
			app.setStatusMsg("Запись успешно сохранена")
		}
	} else {
		savedID = app.svc.GenerateID(d, tm)
		sess := models.Session{
			ID:       savedID,
			Time:     parser.FormatTime(tm),
			Name:     nameVal,
			Type:     typeVal,
			Duration: dur,
			Cash:     cashVal,
			Status:   statusVal,
			Notes:    notesVal,
		}
		_ = app.svc.Add(sess)
		_ = app.svc.Save(app.ctx)
		app.setStatusMsg("Запись успешно создана: ID " + savedID)

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
			_ = app.svc.Save(app.ctx)
			app.setStatusMsg("Запись удалена")
		} else if len(app.selectedIDs) > 0 {
			delCount := 0
			for id := range app.selectedIDs {
				if app.svc.Delete(id) {
					delCount++
				}
			}
			_ = app.svc.Save(app.ctx)
			app.selectedIDs = map[string]bool{}
			app.setStatusMsg(fmt.Sprintf("Удалено записей: %d", delCount))
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
		year:     year,
		selMonth: selMonth,
		selDay:   selDay,
		sessions: sessions,
	}
	m.updateDaysWithSessions()
	m.syncRange()
	return m
}

func (m *ModalYearCalendar) updateDaysWithSessions() {
	m.daysWithSessions = make(map[int]bool)
	now := time.Now()
	for _, sess := range m.sessions {
		if d, err := parser.ParseDate(sess.Date(), now); err == nil {
			if d.Year() == m.year {
				key := int(d.Month())*100 + d.Day()
				m.daysWithSessions[key] = true
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
	now := time.Now()

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

type ModalSettings struct {
	defaultMode string // "cli", "tui", "web"
	portStr     string
	durationStr string
	selectedIdx int
	svcStatus   *service.StatusInfo
}

func NewModalSettings(cfg *models.Config) *ModalSettings {
	mgr := service.NewManager()
	st, _ := mgr.Status(context.Background())
	return &ModalSettings{
		defaultMode: cfg.DefaultMode,
		portStr:     strconv.Itoa(cfg.WebPort),
		durationStr: strconv.Itoa(cfg.DefaultDuration),
		selectedIdx: 0,
		svcStatus:   st,
	}
}

func (m *ModalSettings) Draw(s *Screen) {
	w := 54
	h := 18
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

	fields := []struct {
		lbl string
		val string
	}{
		{"1. Режим запуска по умолч.:", modeDisplay},
		{"2. Порт веб-сервера:        ", m.portStr},
		{"3. Длительность по умолч.:  ", m.durationStr + " мин"},
		{"4. Фоновая служба (сервис): ", m.svcStatusText()},
	}

	for i, f := range fields {
		rowY := y + 2 + (i * 3)
		isCur := i == m.selectedIdx
		fg := FgMuted
		if isCur {
			fg = FgYellow
		}
		lblW := 26
		if lblW > w-12 {
			lblW = w - 12
		}
		if lblW < 8 {
			lblW = 8
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
			if isCur && (i == 1 || i == 2) {
				disp += "█"
			}
			s.DrawText(valX+1, rowY, disp, valFg, valBg, valW-2, isCur, false, false)
		}
	}

	s.DrawBottomWrappedHint(x+2, y+h-2, w-4, 2, "[↑↓] Выбор  [Space/0-9] Изменить  [Enter] Сохранить  [Esc]", FgDim, BgModal)
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
	switch {
	case k.Type == KeyEsc:
		app.activeModal = nil
		return true

	case k.Type == KeyUp || k.Matches('k', 'л'):
		if m.selectedIdx > 0 {
			m.selectedIdx--
		} else {
			m.selectedIdx = 3
		}
		return true

	case k.Type == KeyDown || k.Matches('j', 'о'):
		if m.selectedIdx < 3 {
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
		}

	case k.Type == KeyRune && k.Rune >= '0' && k.Rune <= '9':
		if m.selectedIdx == 1 && len(m.portStr) < 5 {
			m.portStr += string(k.Rune)
			return true
		} else if m.selectedIdx == 2 && len(m.durationStr) < 4 {
			m.durationStr += string(k.Rune)
			return true
		}

	case k.Type == KeySpace || (k.Type == KeyEnter && (m.selectedIdx == 0 || m.selectedIdx == 3)):
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
		app.cfg.DefaultMode = m.defaultMode
		if p, err := strconv.Atoi(m.portStr); err == nil && p > 0 {
			app.cfg.WebPort = p
		}
		if d, err := strconv.Atoi(m.durationStr); err == nil && d > 0 {
			app.cfg.DefaultDuration = d
		}
		app.persistConfig()
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
		dFrom, err1 := parser.ParseDate(m.fromStr, time.Now())
		dTo, err2 := parser.ParseDate(m.toStr, time.Now())
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

type ModalHelp struct{}

func NewModalHelp() *ModalHelp {
	return &ModalHelp{}
}

func (m *ModalHelp) Draw(s *Screen) {
	w := 64
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

	s.DrawBox(x, y, w, h, "Справка по горячим клавишам MyCal TUI", FgGreen, BorderBright, BgModal, true)

	lines := []struct {
		key  string
		desc string
	}{
		{"↑ / ↓ / ← / →", "Навигация по записям и дням календаря"},
		{"Space (Пробел)", "Выбор записи [✓] / фиксация периода в календаре"},
		{"Enter", "Карточка записи / применить выбор дня"},
		{"s / ы", "Быстрая смена статуса (Активно/Завершено/...) [1-5]"},
		{"a / ф", "Создать новую запись"},
		{"e / у", "Редактировать выбранную запись"},
		{"f / а", "Быстрая правка любого поля (одиночно или пачкой)"},
		{"d / в", "Удалить выбранную запись (или все отмеченные)"},
		{"Tab / Shift+Tab", "Переключение блоков: Календарь / Периоды / Записи / Карточка"},
		{"< / >, б / ю", "Листание месяцев календаря назад/вперёд"},
		{"p / з", "Ввод периода дат (От и До)"},
		{"Home, t / е", "Возврат календаря на Сегодня"},
		{"1 .. 4", "Быстрый выбор периода: Все / День / Неделя / Месяц"},
		{"y / н", "Годовой календарь со всеми месяцами"},
		{"o / щ", "Настройки (режим по умолчанию, служба, порт)"},
		{"q / й, Ctrl+C", "Выход из полноэкранного режима TUI"},
	}

	maxRows := h - 4
	for i, item := range lines {
		if i >= maxRows {
			break
		}
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

	s.DrawBottomWrappedHint(x+2, y+h-2, w-4, 2, "[Esc / Enter / q] Закрыть справку", FgDim, BgModal)
}

func (m *ModalHelp) HandleKey(k Key, app *App) bool {
	if k.Type == KeyEsc || k.Type == KeyEnter || k.Matches('q', 'й') || k.Matches('h', 'р') || k.Matches('?', '?') {
		app.activeModal = nil
		return true
	}
	return false
}
