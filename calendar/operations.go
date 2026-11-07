package calendar

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"mycalendar/models"
	"mycalendar/parser"
	"mycalendar/storage"
)

// EntryInput holds the parameters for creating one or more sessions (supporting
// both multi-name splitting via ';' / newline and repeating series).
type EntryInput struct {
	Date        time.Time
	Time        time.Time
	RawName     string
	Type        string
	Duration    int
	Cash        float64
	Status      string
	Notes       string
	RepeatMode  string // "", "d", "w", "m"
	RepeatUntil time.Time
}

// CreateResult reports the outcome of CreateEntries.
type CreateResult struct {
	Added      int
	AddedCount int
	First      models.Session
	Sessions   []models.Session
	SeriesID   string
	Truncated  bool
	ConfigMod  bool
}

// CreateEntries is the unified session creation engine used by CLI, TUI, and Web.
// It handles multi-name splitting, repeat series expansion, custom list updates,
// batch insertion, and disk persistence.
func (s *Service) CreateEntries(ctx context.Context, cfg *models.Config, in EntryInput) (CreateResult, error) {
	var res CreateResult

	names := SplitEntryNames(in.RawName)
	if len(names) == 0 {
		trimmed := strings.TrimSpace(in.RawName)
		if trimmed == "" {
			return res, fmt.Errorf("наименование записи не может быть пустым")
		}
		names = []string{trimmed}
	}

	dur := in.Duration
	if dur <= 0 {
		if cfg != nil && cfg.DefaultDuration > 0 {
			dur = cfg.DefaultDuration
		} else {
			dur = 60
		}
	}

	typ := strings.TrimSpace(in.Type)
	if typ == "" && cfg != nil && cfg.DefaultType != "" {
		typ = cfg.DefaultType
	}
	status := NormalizeStatus(in.Status)
	notes := strings.TrimSpace(in.Notes)
	hhmm := in.Time.Format("15:04")

	s.BeginUndoTx("добавление записи")
	defer s.EndUndoTx()

	if cfg != nil {
		if typ != "" {
			cfg.AddCustomType(typ)
			res.ConfigMod = true
		}
		if status != "" {
			cfg.AddCustomStatus(status)
			res.ConfigMod = true
		}
		if cfg.SilentAddNames {
			for _, n := range names {
				if cfg.AddCustomName(n) {
					res.ConfigMod = true
				}
			}
		}
	}

	dates := []time.Time{in.Date}
	if in.RepeatMode != "" && !in.RepeatUntil.IsZero() {
		occurrences, truncated := RepeatDates(in.Date, in.RepeatUntil, in.RepeatMode)
		res.Truncated = truncated
		dates = append(dates, occurrences...)
	}

	var toAdd []models.Session
	for _, nameItem := range names {
		base := models.Session{
			Time:     hhmm,
			Name:     nameItem,
			Type:     typ,
			Duration: dur,
			Cash:     in.Cash,
			Status:   status,
			Notes:    notes,
		}

		first := base
		first.ID = s.GenerateID(dates[0], in.Time)
		seriesID := ""
		if len(dates) > 1 {
			seriesID = first.ID
			first.SeriesID = seriesID
		}
		if res.First.ID == "" {
			res.First = first
			res.SeriesID = seriesID
		}

		// Reserve ID in service or generate distinct IDs across batch
		toAdd = append(toAdd, first)
		for _, d := range dates[1:] {
			occ := base
			occ.ID = s.GenerateID(d, in.Time)
			occ.SeriesID = seriesID
			toAdd = append(toAdd, occ)
		}

		// Add per-name batch so GenerateID sees previous name's sessions on the same date/time
		added, err := s.AddBatch(toAdd)
		res.Added += added
		res.AddedCount = res.Added
		res.Sessions = append(res.Sessions, toAdd...)
		toAdd = nil
		if err != nil {
			return res, err
		}
	}

	if err := s.Save(ctx); err != nil {
		return res, err
	}
	return res, nil
}

// UpdateInput holds parameters for updating a session or its series across all UIs.
type UpdateInput struct {
	NewDate      *time.Time
	Patch        Patch
	UpdateSeries bool
}

// UpdateEntry is the unified session update engine used by CLI, TUI, and Web.
// It handles single-session updates, whole-series updates, date relocation
// (rebuilding the date-based ID), custom type/status registration, and saving.
func (s *Service) UpdateEntry(ctx context.Context, cfg *models.Config, id string, in UpdateInput) (models.Session, int, error) {
	sess, ok := s.ByID(id)
	if !ok {
		return models.Session{}, 0, fmt.Errorf("запись с ID %s не найдена", id)
	}

	if in.Patch.Status != nil {
		norm := NormalizeStatus(*in.Patch.Status)
		in.Patch.Status = &norm
	}
	if cfg != nil {
		if in.Patch.Type != nil && strings.TrimSpace(*in.Patch.Type) != "" {
			cfg.AddCustomType(*in.Patch.Type)
		}
		if in.Patch.Status != nil && *in.Patch.Status != "" {
			cfg.AddCustomStatus(*in.Patch.Status)
		}
	}

	s.BeginUndoTx("редактирование записи")
	defer s.EndUndoTx()

	if in.UpdateSeries && sess.IsRepeat() {
		n := s.UpdateSeries(sess.SeriesID, in.Patch)
		if err := s.Save(ctx); err != nil {
			return sess, n, err
		}
		updated, _ := s.ByID(id)
		return updated, n, nil
	}

	// Check if date is being changed for a single session
	if in.NewDate != nil {
		newISO := in.NewDate.Format("2006-01-02")
		if newISO != sess.Date() {
			in.Patch.applyTo(&sess)
			tm, err := parser.ParseTime(sess.Time)
			if err != nil {
				tm, _ = parser.ParseTime("10:00")
			}
			_ = s.Delete(id)
			sess.ID = s.GenerateID(*in.NewDate, tm)
			if err := s.Add(sess); err != nil {
				return sess, 0, err
			}
			if err := s.Save(ctx); err != nil {
				return sess, 1, err
			}
			return sess, 1, nil
		}
	}

	if err := s.Update(id, in.Patch); err != nil {
		return sess, 0, err
	}
	if err := s.Save(ctx); err != nil {
		return sess, 1, err
	}
	updated, _ := s.ByID(id)
	return updated, 1, nil
}

// TypeStat holds aggregated duration and cash for a single session category.
type TypeStat struct {
	Type    string  `json:"type"`
	Count   int     `json:"count"`
	Minutes int     `json:"minutes"`
	Hours   float64 `json:"hours"`
	Cash    float64 `json:"cash"`
}

// StatsSummary holds aggregated metrics for a slice of sessions.
type StatsSummary struct {
	Count        int            `json:"count"`
	TotalEntries int            `json:"total_entries"`
	TotalMinutes int            `json:"total_minutes"`
	TotalHours   float64        `json:"total_hours"`
	AvgMinutes   float64        `json:"avg_minutes"`
	TotalCash    float64        `json:"total_cash"`
	ByType       []TypeStat     `json:"by_type"`
	StatusCounts map[string]int `json:"status_counts"`
	TypeCounts   map[string]int `json:"type_counts"`
	TypeMinutes  map[string]int `json:"type_minutes"`
}

// ComputeStats calculates unified statistics for any list of sessions.
func ComputeStats(sessions []models.Session) StatsSummary {
	st := StatsSummary{
		Count:        len(sessions),
		TotalEntries: len(sessions),
		StatusCounts: map[string]int{},
		TypeCounts:   map[string]int{},
		TypeMinutes:  map[string]int{},
	}
	if len(sessions) == 0 {
		return st
	}

	byTypeMin := map[string]int{}
	byTypeCash := map[string]float64{}
	byTypeCnt := map[string]int{}

	for _, sess := range sessions {
		st.TotalMinutes += sess.Duration
		st.TotalCash += sess.Cash
		byTypeMin[sess.Type] += sess.Duration
		byTypeCash[sess.Type] += sess.Cash
		byTypeCnt[sess.Type]++
		st.StatusCounts[sess.Status]++
		tName := sess.Type
		if tName == "" {
			tName = "(без типа)"
		}
		st.TypeCounts[tName]++
		st.TypeMinutes[tName] += sess.Duration
	}

	st.TotalHours = float64(st.TotalMinutes) / 60.0
	st.AvgMinutes = float64(st.TotalMinutes) / float64(len(sessions))

	keys := make([]string, 0, len(byTypeCnt))
	for k := range byTypeCnt {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		mins := byTypeMin[k]
		st.ByType = append(st.ByType, TypeStat{
			Type:    k,
			Count:   byTypeCnt[k],
			Minutes: mins,
			Hours:   float64(mins) / 60.0,
			Cash:    byTypeCash[k],
		})
	}
	return st
}

// ExportCSV writes sessions to a UTF-8 CSV file (with BOM) in the service data
// directory using the unified SessionFields schema, and returns the file path.
func (s *Service) ExportCSV(sessions []models.Session) (string, error) {
	if len(sessions) == 0 {
		return "", fmt.Errorf("нет записей для экспорта")
	}
	stamp := time.Now().Format("20060102_150405")
	path := filepath.Join(s.DataDir(), "export_"+stamp+".csv")
	for i := 2; fileExists(path); i++ {
		path = filepath.Join(s.DataDir(), fmt.Sprintf("export_%s_%d.csv", stamp, i))
	}

	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	// Write UTF-8 BOM for Excel compatibility
	if _, err := f.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return "", err
	}

	w := csv.NewWriter(f)
	headers := make([]string, 0, len(SessionFields))
	for _, fSpec := range SessionFields {
		headers = append(headers, fSpec.CSVHeader)
	}

	rows := [][]string{headers}
	for _, sess := range sessions {
		row := make([]string, 0, len(SessionFields))
		for _, fSpec := range SessionFields {
			if fSpec.Key == FieldDate {
				row = append(row, fSpec.Format(sess, "min"))
			} else {
				row = append(row, fSpec.GetRaw(sess))
			}
		}
		rows = append(rows, row)
	}

	if err := w.WriteAll(rows); err != nil {
		return "", err
	}
	if err := f.Sync(); err != nil {
		return "", err
	}
	return path, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// MoveEntriesToCalendar moves the sessions with the given IDs from the current
// calendar into the target calendar (identified by targetCalKey: "" for default,
// or a prefix like "work"). It saves the target calendar first and then removes
// the moved sessions from the current calendar (recording an undo snapshot).
func (s *Service) MoveEntriesToCalendar(ctx context.Context, cfg *models.Config, ids []string, targetCalKey string) (int, error) {
	if len(ids) == 0 {
		return 0, fmt.Errorf("не выбраны записи для переноса")
	}
	targetCalKey = models.SanitizeCalendarKey(targetCalKey)

	dir := s.DataDir()
	curBase := s.BaseName()
	var targetBase, targetMode string
	if cfg != nil {
		_ = s.ListCalendars(cfg)
		targetBase = cfg.EffectiveBaseName(targetCalKey)
		targetMode = cfg.GetCalendarSplitMode(targetCalKey)
	} else {
		base := curBase
		if idx := strings.Index(base, "--"); idx >= 0 {
			base = base[:idx]
		}
		if targetCalKey == "" {
			targetBase = base
		} else {
			targetBase = base + "--" + targetCalKey
		}
		targetMode = s.Mode()
	}

	if strings.EqualFold(curBase, targetBase) {
		return 0, fmt.Errorf("выбран тот же самый календарь")
	}

	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" {
			idSet[id] = true
		}
	}

	var toMove []models.Session
	for _, sess := range s.All() {
		if idSet[sess.ID] {
			toMove = append(toMove, sess)
		}
	}
	if len(toMove) == 0 {
		return 0, fmt.Errorf("записи для переноса не найдены")
	}

	targetCal, _, err := storage.Load(ctx, dir, targetBase, targetMode)
	if err != nil {
		return 0, fmt.Errorf("не удалось загрузить целевой календарь: %w", err)
	}

	usedIDs := make(map[string]bool, len(targetCal.Sessions)+len(toMove))
	for _, existing := range targetCal.Sessions {
		usedIDs[existing.ID] = true
	}

	for _, sess := range toMove {
		if usedIDs[sess.ID] {
			sess.ID = freeID(sess.ID, usedIDs)
		}
		usedIDs[sess.ID] = true
		targetCal.Sessions = append(targetCal.Sessions, sess)
	}
	storage.SortSessions(targetCal.Sessions)

	if err := storage.Save(ctx, dir, targetBase, targetCal, targetMode); err != nil {
		return 0, fmt.Errorf("не удалось сохранить целевой календарь: %w", err)
	}

	s.BeginUndoTx("перенос записей в другой календарь")
	defer s.EndUndoTx()
	moved := 0
	for _, sess := range toMove {
		if s.Delete(sess.ID) {
			moved++
		}
	}
	if err := s.Save(ctx); err != nil {
		return moved, fmt.Errorf("ошибка сохранения исходного календаря: %w", err)
	}
	return moved, nil
}

// ImportResult summarizes a CSV or JSON import operation.
type ImportResult struct {
	Added     int  `json:"added"`
	Skipped   int  `json:"skipped"`
	Total     int  `json:"total"`
	ConfigMod bool `json:"config_mod"`
}

// ImportFile imports sessions from a CSV or JSON file on disk, deduplicating
// against existing sessions by ID or by (Date, Time, Name).
func (s *Service) ImportFile(ctx context.Context, cfg *models.Config, path string) (ImportResult, error) {
	expanded := storage.ExpandPath(strings.TrimSpace(path))
	if expanded == "" {
		return ImportResult{}, fmt.Errorf("не указан путь к файлу")
	}
	data, err := os.ReadFile(expanded)
	if err != nil {
		return ImportResult{}, err
	}
	ext := strings.ToLower(filepath.Ext(expanded))
	trimmed := bytes.TrimSpace(bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF}))
	if ext == ".json" || (len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[')) {
		return s.ImportJSON(ctx, cfg, bytes.NewReader(data))
	}
	return s.ImportCSV(ctx, cfg, bytes.NewReader(data))
}

// ImportJSON imports sessions from a JSON reader supporting either
// {"sessions": [...]} or a raw [...] array of sessions.
func (s *Service) ImportJSON(ctx context.Context, cfg *models.Config, r io.Reader) (ImportResult, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return ImportResult{}, err
	}
	data = bytes.TrimSpace(bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF}))
	if len(data) == 0 {
		return ImportResult{}, fmt.Errorf("файл пуст")
	}

	var incoming []models.Session
	if data[0] == '[' {
		if err := json.Unmarshal(data, &incoming); err != nil {
			return ImportResult{}, fmt.Errorf("некорректный JSON массив: %w", err)
		}
	} else {
		var cal models.Calendar
		if err := json.Unmarshal(data, &cal); err != nil {
			return ImportResult{}, fmt.Errorf("некорректный JSON файл календаря: %w", err)
		}
		incoming = cal.Sessions
	}
	return s.importSessionList(ctx, cfg, incoming, "импорт из JSON")
}

// ImportCSV imports sessions from a CSV reader (supporting UTF-8 BOM and ',' or ';' delimiters).
func (s *Service) ImportCSV(ctx context.Context, cfg *models.Config, r io.Reader) (ImportResult, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return ImportResult{}, err
	}
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	text := strings.TrimSpace(string(data))
	if text == "" {
		return ImportResult{}, fmt.Errorf("CSV файл пуст")
	}

	// Auto-detect delimiter from the first line
	firstLine := text
	if idx := strings.IndexAny(text, "\r\n"); idx >= 0 {
		firstLine = text[:idx]
	}
	comma := ','
	if strings.Count(firstLine, ";") > strings.Count(firstLine, ",") {
		comma = ';'
	}

	cr := csv.NewReader(strings.NewReader(text))
	cr.Comma = comma
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true

	records, err := cr.ReadAll()
	if err != nil {
		return ImportResult{}, fmt.Errorf("ошибка чтения CSV: %w", err)
	}
	if len(records) == 0 {
		return ImportResult{}, fmt.Errorf("нет данных в CSV")
	}

	// Map columns by header if first row looks like a header
	colMap := map[string]int{
		FieldID:       0,
		FieldDate:     1,
		FieldTime:     2,
		FieldName:     3,
		FieldType:     4,
		FieldDuration: 5,
		FieldCash:     6,
		FieldStatus:   7,
		FieldNotes:    8,
		FieldSeries:   9,
	}
	startRow := 0
	hdr := records[0]
	isHeader := false
	detectedCols := map[string]int{}
	for idx, cell := range hdr {
		c := strings.ToLower(strings.TrimSpace(cell))
		switch c {
		case "id", "ид":
			detectedCols[FieldID] = idx
			isHeader = true
		case "дата", "date":
			detectedCols[FieldDate] = idx
			isHeader = true
		case "время", "time":
			detectedCols[FieldTime] = idx
			isHeader = true
		case "имя", "название", "наименование", "name":
			detectedCols[FieldName] = idx
			isHeader = true
		case "тип", "категория", "type", "category":
			detectedCols[FieldType] = idx
			isHeader = true
		case "продолжительность (мин)", "продолжительность", "длительность", "duration":
			detectedCols[FieldDuration] = idx
			isHeader = true
		case "кэш", "cash", "сумма":
			detectedCols[FieldCash] = idx
			isHeader = true
		case "статус", "status":
			detectedCols[FieldStatus] = idx
			isHeader = true
		case "комментарий", "примечания", "заметки", "notes":
			detectedCols[FieldNotes] = idx
			isHeader = true
		case "серия", "series_id", "series":
			detectedCols[FieldSeries] = idx
			isHeader = true
		}
	}
	if isHeader {
		colMap = detectedCols
		startRow = 1
	}

	getCol := func(row []string, key string) string {
		if idx, ok := colMap[key]; ok && idx >= 0 && idx < len(row) {
			return strings.TrimSpace(row[idx])
		}
		return ""
	}

	var incoming []models.Session
	refNow := time.Now()
	for _, row := range records[startRow:] {
		if len(row) == 0 {
			continue
		}
		rawID := getCol(row, FieldID)
		rawDate := getCol(row, FieldDate)
		rawTime := getCol(row, FieldTime)
		rawName := getCol(row, FieldName)
		if rawName == "" && rawID == "" && rawDate == "" {
			continue
		}
		if rawName == "" {
			rawName = "Запись"
		}

		tm, err := parser.ParseTime(rawTime)
		if err != nil {
			if len(rawID) >= 12 && isDigits(rawID[:12]) {
				tm, err = parser.ParseTime(rawID[8:12])
			}
			if err != nil {
				tm, _ = parser.ParseTime("10:00")
			}
		}

		var d time.Time
		if rawDate != "" {
			if parsed, errD := parser.ParseDate(rawDate, refNow); errD == nil {
				d = parsed
			}
		}
		if d.IsZero() && len(rawID) >= 8 && isDigits(rawID[:8]) {
			if parsed, errD := time.Parse("20060102", rawID[:8]); errD == nil {
				d = parsed
			}
		}
		if d.IsZero() {
			d = refNow
		}

		dur, _ := strconv.Atoi(getCol(row, FieldDuration))
		if dur <= 0 {
			dur = 60
		}
		cashVal, _ := ParseCash(getCol(row, FieldCash))
		statusVal := NormalizeStatus(getCol(row, FieldStatus))
		typeVal := getCol(row, FieldType)
		notesVal := getCol(row, FieldNotes)
		seriesVal := getCol(row, FieldSeries)

		id := rawID
		if len(id) < 10 || !isDigits(id[:8]) {
			id = d.Format("20060102") + tm.Format("1504") + "00"
		}

		incoming = append(incoming, models.Session{
			ID:       id,
			Time:     tm.Format("15:04"),
			Name:     rawName,
			Type:     typeVal,
			Duration: dur,
			Cash:     cashVal,
			Status:   statusVal,
			Notes:    notesVal,
			SeriesID: seriesVal,
		})
	}

	return s.importSessionList(ctx, cfg, incoming, "импорт из CSV")
}

func (s *Service) importSessionList(ctx context.Context, cfg *models.Config, incoming []models.Session, undoDesc string) (ImportResult, error) {
	var res ImportResult
	res.Total = len(incoming)
	if len(incoming) == 0 {
		return res, nil
	}

	existing := s.All()
	existingIDs := make(map[string]bool, len(existing))
	existingSigs := make(map[string]bool, len(existing))
	sigOf := func(x models.Session) string {
		return x.Date() + "|" + strings.TrimSpace(x.Time) + "|" + strings.ToLower(strings.TrimSpace(x.Name))
	}
	for _, ex := range existing {
		existingIDs[ex.ID] = true
		existingSigs[sigOf(ex)] = true
	}

	var toAdd []models.Session
	for _, sess := range incoming {
		sess.Name = strings.TrimSpace(sess.Name)
		if sess.Name == "" {
			res.Skipped++
			continue
		}
		sess.Status = NormalizeStatus(sess.Status)
		sess.Type = strings.TrimSpace(sess.Type)
		if sess.Duration <= 0 {
			sess.Duration = 60
		}
		if tm, err := parser.ParseTime(sess.Time); err == nil {
			sess.Time = tm.Format("15:04")
		} else {
			sess.Time = "10:00"
		}

		// Deduplicate by exact ID+(Date,Time,Name) or by signature
		sig := sigOf(sess)
		if existingSigs[sig] {
			res.Skipped++
			continue
		}
		if sess.ID == "" || len(sess.ID) < 10 || !isDigits(sess.ID[:8]) {
			sess.ID = time.Now().Format("20060102") + strings.ReplaceAll(sess.Time, ":", "") + "00"
		}
		if existingIDs[sess.ID] {
			sess.ID = freeID(sess.ID, existingIDs)
		}
		existingIDs[sess.ID] = true
		existingSigs[sig] = true

		if cfg != nil {
			if sess.Type != "" {
				cfg.AddCustomType(sess.Type)
				res.ConfigMod = true
			}
			if sess.Status != "" {
				cfg.AddCustomStatus(sess.Status)
				res.ConfigMod = true
			}
		}
		toAdd = append(toAdd, sess)
	}

	if len(toAdd) == 0 {
		return res, nil
	}

	s.BeginUndoTx(undoDesc)
	defer s.EndUndoTx()
	added, err := s.AddBatch(toAdd)
	res.Added = added
	if err != nil {
		return res, err
	}
	if err := s.Save(ctx); err != nil {
		return res, err
	}
	return res, nil
}
