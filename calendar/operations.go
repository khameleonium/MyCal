package calendar

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mycalendar/models"
	"mycalendar/parser"
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
	status := strings.TrimSpace(in.Status)
	notes := strings.TrimSpace(in.Notes)
	hhmm := in.Time.Format("15:04")

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

	if cfg != nil {
		if in.Patch.Type != nil && strings.TrimSpace(*in.Patch.Type) != "" {
			cfg.AddCustomType(*in.Patch.Type)
		}
		if in.Patch.Status != nil && strings.TrimSpace(*in.Patch.Status) != "" {
			cfg.AddCustomStatus(*in.Patch.Status)
		}
	}

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
