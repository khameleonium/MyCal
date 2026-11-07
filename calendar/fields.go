package calendar

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"mycalendar/models"
	"mycalendar/parser"
)

// Field keys shared across CLI, TUI, Web, and CSV export.
const (
	FieldID       = "id"
	FieldDate     = "date"
	FieldTime     = "time"
	FieldName     = "name"
	FieldType     = "type"
	FieldDuration = "duration"
	FieldCash     = "cash"
	FieldStatus   = "status"
	FieldNotes    = "notes"
	FieldSeries   = "series_id"
)

// FieldSpec describes a session field in one central registry.
type FieldSpec struct {
	Key       string
	Label     string
	CSVHeader string
	Required  bool
	GetRaw    func(s models.Session) string
	Format    func(s models.Session, durationUnit string) string
}

// SessionFields is the single source of truth for session attributes, their
// order, labels, display formatting, and CSV export columns.
var SessionFields = []FieldSpec{
	{
		Key:       FieldID,
		Label:     "ИД",
		CSVHeader: "ID",
		Required:  true,
		GetRaw:    func(s models.Session) string { return s.ID },
		Format:    func(s models.Session, _ string) string { return s.ID },
	},
	{
		Key:       FieldDate,
		Label:     "Дата",
		CSVHeader: "Дата",
		Required:  true,
		GetRaw:    func(s models.Session) string { return s.Date() },
		Format: func(s models.Session, _ string) string {
			if t, err := time.Parse("2006-01-02", s.Date()); err == nil {
				return parser.FormatDate(t)
			}
			return s.Date()
		},
	},
	{
		Key:       FieldTime,
		Label:     "Время",
		CSVHeader: "Время",
		Required:  true,
		GetRaw:    func(s models.Session) string { return s.Time },
		Format:    func(s models.Session, _ string) string { return s.Time },
	},
	{
		Key:       FieldName,
		Label:     "Имя",
		CSVHeader: "Имя",
		Required:  true,
		GetRaw:    func(s models.Session) string { return s.Name },
		Format:    func(s models.Session, _ string) string { return s.Name },
	},
	{
		Key:       FieldType,
		Label:     "Тип",
		CSVHeader: "Тип",
		Required:  false,
		GetRaw:    func(s models.Session) string { return s.Type },
		Format:    func(s models.Session, _ string) string { return s.Type },
	},
	{
		Key:       FieldDuration,
		Label:     "Продолжительность",
		CSVHeader: "Продолжительность (мин)",
		Required:  false,
		GetRaw:    func(s models.Session) string { return strconv.Itoa(s.Duration) },
		Format: func(s models.Session, unit string) string {
			return FormatDuration(s.Duration, unit)
		},
	},
	{
		Key:       FieldCash,
		Label:     "Кэш",
		CSVHeader: "Кэш",
		Required:  false,
		GetRaw:    func(s models.Session) string { return FormatCash(s.Cash) },
		Format:    func(s models.Session, _ string) string { return FormatCash(s.Cash) },
	},
	{
		Key:       FieldStatus,
		Label:     "Статус",
		CSVHeader: "Статус",
		Required:  false,
		GetRaw:    func(s models.Session) string { return s.Status },
		Format:    func(s models.Session, _ string) string { return s.Status },
	},
	{
		Key:       FieldNotes,
		Label:     "Комментарий",
		CSVHeader: "Комментарий",
		Required:  false,
		GetRaw:    func(s models.Session) string { return s.Notes },
		Format:    func(s models.Session, _ string) string { return s.Notes },
	},
	{
		Key:       FieldSeries,
		Label:     "Серия",
		CSVHeader: "Серия",
		Required:  false,
		GetRaw:    func(s models.Session) string { return s.SeriesID },
		Format:    func(s models.Session, _ string) string { return s.SeriesID },
	},
}

// ParseCash parses a cash value from any representation (string with dot or comma,
// float64, int, or "-" to clear to 0).
func ParseCash(v interface{}) (float64, error) {
	if v == nil {
		return 0, nil
	}
	switch val := v.(type) {
	case float64:
		return val, nil
	case int:
		return float64(val), nil
	case int64:
		return float64(val), nil
	case string:
		s := strings.TrimSpace(val)
		if s == "" || s == "-" || s == "0" || s == "0.0" || s == "0,0" {
			return 0, nil
		}
		s = strings.ReplaceAll(s, ",", ".")
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, fmt.Errorf("некорректная сумма: %s", val)
		}
		return f, nil
	default:
		return 0, fmt.Errorf("неподдерживаемый формат суммы")
	}
}

// FormatCash formats a float64 cash value as a clean numeric string.
func FormatCash(val float64) string {
	return strconv.FormatFloat(val, 'f', -1, 64)
}

// SplitEntryNames splits a raw name input by ';' or newline into trimmed non-empty names.
func SplitEntryNames(raw string) []string {
	var names []string
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == '\n' || r == ';'
	}) {
		p := strings.TrimSpace(part)
		if p != "" {
			names = append(names, p)
		}
	}
	return names
}

// CheckDateInput parses a date string and checks whether the date was exact (`valid == true`)
// or rolled over due to calendar overflow (e.g. "32.01.2026" -> "01.02.2026", `valid == false`).
func CheckDateInput(in string, ref time.Time) (date time.Time, valid bool, err error) {
	d, err := parser.ParseDate(in, ref)
	if err != nil {
		return time.Time{}, false, err
	}
	valid = parser.ValidateDate(in, d)
	return d, valid, nil
}
