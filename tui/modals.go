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
	s.DrawText(x+2, y+h-2, hint, FgDim, BgModal, w-4, false, false, false)
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
}

func NewModalQuickEditField(targetID string) *ModalQuickEditField {
	return &ModalQuickEditField{
		fields: []string{
			"Наименование",
			"Кэш",
			"Категория / Тип",
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
		s.DrawText(x+2, y+h-2, "[↑↓/1-7] Выбор  [Enter] Далее  [Esc Отмена]", FgDim, BgModal, w-4, false, false, false)
	} else {
		fieldName := m.fields[m.selected]
		s.DrawText(x+2, y+2, "Поле: "+fieldName, FgCyan, BgModal, w-4, true, false, false)
		s.DrawText(x+2, y+4, "Введите новое значение:", FgMuted, BgModal, w-4, false, false, false)

		// Input box
		s.Fill(x+2, y+6, w-4, 1, ' ', FgWhite, BgInput)
		s.DrawBox(x+1, y+5, w-2, 3, "", FgDim, BorderFocus, BgInput, false)
		inputDisplay := m.value + "█"
		s.DrawText(x+3, y+6, inputDisplay, FgWhite, BgInput, w-6, true, false, false)

		s.DrawText(x+2, y+h-2, "[Enter] Сохранить  [Esc] Назад", FgDim, BgModal, w-4, false, false, false)
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
		switch k.Type {
		case KeyEsc:
			m.stepInput = false
			return true
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
			case 3: // Время
				m.value = sess.Time
			case 4: // Длительность
				m.value = strconv.Itoa(sess.Duration)
			case 5: // Дата
				m.value = sess.Date()
			case 6: // Заметки
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
		if c, err := strconv.ParseFloat(val, 64); err == nil && c >= 0 {
			patch.Cash = &c
		} else {
			zero := 0.0
			patch.Cash = &zero
		}
	case 2: // Type
		patch.Type = &val
	case 3: // Time
		if tm, err := parser.ParseTime(val); err == nil {
			tStr := parser.FormatTime(tm)
			patch.Time = &tStr
		}
	case 4: // Duration
		if d, err := strconv.Atoi(val); err == nil && d > 0 {
			patch.Duration = &d
		}
	case 6: // Notes
		patch.Notes = &val
	}

	if m.targetID != "" {
		if m.selected == 5 { // Date move
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
	isEdit     bool
	targetID   string
	dateStr    string
	timeStr    string
	name       string
	typeStr    string
	durStr     string
	cashStr    string
	status     string
	notes      string
	repeatStr  string // "none", "d", "w", "m"
	repeatDate string // repeat until date
	fieldIdx   int    // 0..8
}

func NewModalSessionForm(sess *models.Session, initialDate string) *ModalSessionForm {
	if sess != nil {
		return &ModalSessionForm{
			isEdit:    true,
			targetID:  sess.ID,
			dateStr:   sess.Date(),
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
	if dStr == "" {
		dStr = parser.FormatDate(time.Now())
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
		valBoxW := w - 28
		s.Fill(x+25, rowY, valBoxW, 1, ' ', valFg, valBg)

		txt := f.val
		if isCur {
			txt += "█"
		}
		s.DrawText(x+26, rowY, txt, valFg, valBg, valBoxW-2, isCur, false, false)
	}

	s.DrawText(x+2, y+h-2, "[Tab/Shift+Tab] Поля  [Enter] Сохранить  [Esc] Отмена", FgDim, BgModal, w-4, false, false, false)
}

func (m *ModalSessionForm) HandleKey(k Key, app *App) bool {
	maxField := 7
	if !m.isEdit {
		maxField = 8
	}

	switch k.Type {
	case KeyEsc:
		app.activeModal = nil
		return true

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
		if (r >= '0' && r <= '9') || r == '.' || r == ',' {
			if r == ',' {
				r = '.'
			}
			m.cashStr += string(r)
		}
	case 6:
		// Cycle status on key
		if r == ' ' || r == 's' || r == 'ы' {
			m.cycleStatus()
		} else {
			m.status += string(r)
		}
	case 7:
		m.notes += string(r)
	case 8:
		// Toggle repeat
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

func (m *ModalSessionForm) cycleStatus() {
	order := []string{"Активно", "Завершено", "Запланировано", "В процессе", "Отменено"}
	for i, st := range order {
		if strings.EqualFold(st, m.status) {
			m.status = order[(i+1)%len(order)]
			return
		}
	}
	m.status = order[0]
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
	if cashVal < 0 {
		cashVal = 0.0
	}

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
				sess.ID = app.svc.GenerateID(d, tm)
				sess.Time = tStr
				sess.Name = nameVal
				sess.Type = typeVal
				sess.Duration = dur
				sess.Cash = cashVal
				sess.Status = statusVal
				sess.Notes = notesVal
				_ = app.svc.Add(sess)
			} else {
				_ = app.svc.Update(sess.ID, patch)
			}
			_ = app.svc.Save(app.ctx)
			app.setStatusMsg("Запись успешно сохранена")
		}
	} else {
		newID := app.svc.GenerateID(d, tm)
		sess := models.Session{
			ID:       newID,
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
		app.setStatusMsg("Запись успешно создана: ID " + newID)
	}
	app.reloadSessions()
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

	s.DrawText(x+3, y+5, "[Enter] Удалить      [Esc] Отмена", FgDim, BgModal, w-6, true, false, false)
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
	year      int
	inputMode bool
	inputYear string
}

func NewModalYearCalendar(year int) *ModalYearCalendar {
	if year <= 0 {
		year = time.Now().Year()
	}
	return &ModalYearCalendar{year: year}
}

func (m *ModalYearCalendar) Draw(s *Screen) {
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

	title := fmt.Sprintf("◄  Календарь на %d год  ►", m.year)
	s.DrawBox(x, y, w, h, title, FgCyan, BorderBright, BgModal, true)

	months := []string{
		"Январь", "Февраль", "Март", "Апрель",
		"Май", "Июнь", "Июль", "Август",
		"Сентябрь", "Октябрь", "Ноябрь", "Декабрь",
	}

	for i := 0; i < 12; i++ {
		col := i % 4
		row := i / 4
		bx := x + 2 + (col * 16)
		by := y + 2 + (row * 6)

		mNum := i + 1
		s.DrawText(bx, by, fmt.Sprintf("%02d. %s", mNum, months[i]), FgYellow, BgModal, 14, true, false, false)
		s.DrawText(bx, by+1, "Пн Вт Ср Чт Пт Сб Вс", FgDim, BgModal, 14, false, false, false)

		firstDay := time.Date(m.year, time.Month(mNum), 1, 0, 0, 0, 0, time.UTC)
		weekday := int(firstDay.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		weekday-- // 0..6 (Mon..Sun)

		daysInMonth := time.Date(m.year, time.Month(mNum+1), 0, 0, 0, 0, 0, time.UTC).Day()

		curX := bx + (weekday * 2)
		curY := by + 2

		for d := 1; d <= daysInMonth; d++ {
			s.DrawText(curX, curY, fmt.Sprintf("%2d", d), FgMuted, BgModal, 2, false, false, false)
			curX += 2
			weekday++
			if weekday == 7 {
				weekday = 0
				curX = bx
				curY++
			}
		}
	}

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
		return
	}

	s.DrawText(x+2, y+h-2, "[◄/►] Сменить год  [y] Ввести год  [Esc] Закрыть", FgDim, BgModal, w-4, false, false, false)
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
	case k.Type == KeyEsc || k.Type == KeyEnter || k.Matches('q', 'й'):
		app.activeModal = nil
		return true
	case k.Matches('y', 'н'):
		m.inputMode = true
		m.inputYear = ""
		return true
	case k.Type == KeyRune && k.Rune >= '0' && k.Rune <= '9':
		m.inputMode = true
		m.inputYear = string(k.Rune)
		return true
	case k.Type == KeyLeft || k.Matches('h', 'р') || k.Type == KeyDown || k.Matches('j', 'о'):
		m.year--
		return true
	case k.Type == KeyRight || k.Matches('l', 'д') || k.Type == KeyUp || k.Matches('k', 'л'):
		m.year++
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
		s.DrawText(x+2, rowY, f.lbl, fg, BgModal, 26, isCur, false, false)

		valBg := BgInput
		valFg := FgWhite
		if isCur {
			valBg = BgSelected
		}
		s.Fill(x+28, rowY, w-30, 1, ' ', valFg, valBg)
		disp := f.val
		if isCur && (i == 1 || i == 2) {
			disp += "█"
		}
		s.DrawText(x+29, rowY, disp, valFg, valBg, w-32, isCur, false, false)
	}

	s.DrawText(x+2, y+h-2, "[↑↓] Выбор  [Space/0-9] Изменить  [Enter] Сохранить  [Esc]", FgDim, BgModal, w-4, false, false, false)
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
	s.Fill(x+20, y+3, 16, 1, ' ', f0Fg, f0Bg)
	s.DrawText(x+21, y+3, m.fromStr, f0Fg, f0Bg, 14, m.focused == 0, false, false)

	// Field 1: To
	f1Bg := BgCard
	f1Fg := FgMain
	if m.focused == 1 {
		f1Bg = BgSelected
		f1Fg = FgWhite
	}
	s.DrawText(x+2, y+5, "Дата конца (До):", FgDim, BgModal, 18, false, false, false)
	s.Fill(x+20, y+5, 16, 1, ' ', f1Fg, f1Bg)
	s.DrawText(x+21, y+5, m.toStr, f1Fg, f1Bg, 14, m.focused == 1, false, false)

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
	s.Fill(x+4, btnY, len(applyLbl), 1, ' ', btnApplyFg, btnApplyBg)
	s.DrawText(x+4, btnY, applyLbl, btnApplyFg, btnApplyBg, len(applyLbl), m.focused == 2, false, false)

	btnCancelBg := BgPanelAlt
	btnCancelFg := FgMain
	if m.focused == 3 {
		btnCancelBg = BgHighlight
		btnCancelFg = FgWhite
	}
	cancelLbl := "  Отмена (Esc)  "
	s.Fill(x+26, btnY, len(cancelLbl), 1, ' ', btnCancelFg, btnCancelBg)
	s.DrawText(x+26, btnY, cancelLbl, btnCancelFg, btnCancelBg, len(cancelLbl), m.focused == 3, false, false)

	s.DrawText(x+2, y+h-2, "[Tab / ↑↓] Переход  [Enter] Применить  [Esc] Отмена", FgDim, BgModal, w-4, false, false, false)
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

	for i, item := range lines {
		rowY := y + 2 + i
		s.DrawText(x+2, rowY, PadRight(item.key, 18), FgCyan, BgModal, 18, true, false, false)
		s.DrawText(x+20, rowY, item.desc, FgMain, BgModal, w-22, false, false, false)
	}

	s.DrawText(x+2, y+h-2, "[Esc / Enter / q] Закрыть справку", FgDim, BgModal, w-4, false, false, false)
}

func (m *ModalHelp) HandleKey(k Key, app *App) bool {
	if k.Type == KeyEsc || k.Type == KeyEnter || k.Matches('q', 'й') || k.Matches('h', 'р') || k.Matches('?', '?') {
		app.activeModal = nil
		return true
	}
	return false
}
