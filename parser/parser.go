package parser

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ErrEmptyTime is returned when the user leaves the time input blank.
var ErrEmptyTime = errors.New("пустое время")

var dateFormats = []string{
	"02-01-2006",
	"02.01.2006",
	"2006-01-02",
}

// ParseDate parses a date string in various formats.
// Supported: DD-MM-YYYY, DD.MM.YYYY, YYYY-MM-DD, DD MM YYYY.
// Empty input returns the provided defaultDate.
func ParseDate(input string, defaultDate time.Time) (time.Time, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return defaultDate, nil
	}

	normalized := normalizeSeparators(input)

	for _, layout := range dateFormats {
		if t, err := time.Parse(layout, normalized); err == nil {
			return t, nil
		}
	}

	if t, err := time.Parse("02-01", normalized); err == nil {
		now := time.Now()
		return time.Date(now.Year(), t.Month(), t.Day(), 0, 0, 0, 0, now.Location()), nil
	}

	return time.Time{}, fmt.Errorf("не удалось распознать дату: %s", input)
}

// ParseTime parses a time string in various formats.
// Supported: HH:MM, HH MM, HH.MM, HH-MM.
// Empty input returns ErrEmptyTime.
func ParseTime(input string) (time.Time, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return time.Time{}, ErrEmptyTime
	}

	if match, _ := regexp.MatchString(`^\d{1,2}$`, input); match {
		if input == "24" {
			input = "00:00"
		} else {
			input = input + ":00"
		}
	}

	normalized := normalizeTime(input)
	t, err := time.Parse("15:04", normalized)
	if err != nil {
		return time.Time{}, fmt.Errorf("не удалось распознать время: %s", input)
	}

	return t, nil
}

func normalizeTime(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "  ", " ")
	s = strings.ReplaceAll(s, " ", ":")
	s = strings.ReplaceAll(s, ".", ":")
	s = strings.ReplaceAll(s, "-", ":")
	return s
}

// ParseDateTime parses a combined date+time string like "DD-MM-YYYY HH:MM".
func ParseDateTime(input string) (time.Time, time.Time, error) {
	input = strings.TrimSpace(input)
	parts := splitDateTime(input)

	if len(parts) < 2 {
		return time.Time{}, time.Time{}, fmt.Errorf("введите дату и время через пробел")
	}

	datePart := strings.Join(parts[:len(parts)-1], " ")
	timePart := parts[len(parts)-1]

	date, err := ParseDate(datePart, time.Time{})
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("некорректная дата: %w", err)
	}

	tm, err := ParseTime(timePart)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("некорректное время: %w", err)
	}

	return date, tm, nil
}

// FormatDate formats a time as DD-MM-YYYY.
func FormatDate(t time.Time) string {
	return t.Format("02-01-2006")
}

// FormatTime formats a time as HH:MM.
func FormatTime(t time.Time) string {
	return t.Format("15:04")
}

// IsDateNormalized checks whether the normalized input string was unchanged
// by Go's time normalization (e.g. "55-30-2025" → "25-07-2027" is NOT normalized).
func IsDateNormalized(normalizedInput string, t time.Time) bool {
	if t.Format("02-01-2006") == normalizedInput {
		return true
	}
	if t.Format("02-01") == normalizedInput {
		return true
	}
	return false
}

// ValidateDate checks whether the raw input was silently normalized by time.Parse.
// Returns true if the date is valid as-is.
func ValidateDate(rawInput string, parsed time.Time) bool {
	normalized := normalizeSeparators(rawInput)
	return IsDateNormalized(normalized, parsed)
}

var sepRegexp = regexp.MustCompile(`[.\-/\\]`)

func normalizeSeparators(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "  ", " ")
	s = strings.ReplaceAll(s, " ", "-")
	return sepRegexp.ReplaceAllString(s, "-")
}

func splitDateTime(input string) []string {
	input = strings.TrimSpace(input)
	// Try splitting by space
	parts := strings.Fields(input)
	if len(parts) >= 2 {
		return parts
	}
	return []string{input}
}

// ParsePeriod parses a period string and returns start and end dates.
// Supported formats:
// - YYYY (e.g., 2025) -> 01-01-YYYY to 31-12-YYYY
// - MM-YYYY (e.g., 12.2025) -> 01-12-2025 to 31-12-2025
// - MM (e.g., 12) -> 01-12-CurrentYear to 31-12-CurrentYear
// - MM-MM (e.g., 10-12) -> 01-10-CurrentYear to 31-12-CurrentYear
// - DD-MM-DD-MM (e.g., 01.12-15.12) -> 01-12-CurrentYear to 15-12-CurrentYear
// - FullDate - FullDate -> parses using ParseDate
func ParsePeriod(input string) (time.Time, time.Time, error) {
	input = strings.TrimSpace(input)

	now := time.Now()
	currYear := now.Year()

	if input == "" {
		start := time.Date(currYear, 1, 1, 0, 0, 0, 0, time.Local)
		end := time.Date(currYear, 12, 31, 0, 0, 0, 0, time.Local)
		return start, end, nil
	}

	// 1. Check for standard two full dates with space or common range separators
	cleanInput := strings.ReplaceAll(input, " - ", " ")
	cleanInput = strings.ReplaceAll(cleanInput, " — ", " ")
	parts := strings.Fields(cleanInput)
	if len(parts) == 2 {
		d1, err1 := ParseDate(parts[0], time.Time{})
		d2, err2 := ParseDate(parts[1], time.Time{})
		if err1 == nil && err2 == nil {
			if d1.After(d2) {
				d1, d2 = d2, d1
			}
			return d1, d2, nil
		}
	}

	// 2. Normalize separators to handle tokens separated by '.', '/', '\', '-'
	normalized := normalizeSeparators(input)
	for strings.Contains(normalized, "--") {
		normalized = strings.ReplaceAll(normalized, "--", "-")
	}

	tokens := strings.Split(normalized, "-")

	// 1 token: YYYY or MM
	if len(tokens) == 1 {
		if len(tokens[0]) == 4 { // YYYY
			year, err := strconv.Atoi(tokens[0])
			if err == nil {
				start := time.Date(year, 1, 1, 0, 0, 0, 0, time.Local)
				end := time.Date(year, 12, 31, 0, 0, 0, 0, time.Local)
				return start, end, nil
			}
		} else if len(tokens[0]) == 1 || len(tokens[0]) == 2 { // MM
			month, err := strconv.Atoi(tokens[0])
			if err == nil && month >= 1 && month <= 12 {
				start := time.Date(currYear, time.Month(month), 1, 0, 0, 0, 0, time.Local)
				end := time.Date(currYear, time.Month(month), lastDayOfMonth(currYear, month), 0, 0, 0, 0, time.Local)
				return start, end, nil
			}
		}
	}

	// 2 tokens: MM-YYYY or MM-MM
	if len(tokens) == 2 {
		v1, err1 := strconv.Atoi(tokens[0])
		v2, err2 := strconv.Atoi(tokens[1])
		if err1 == nil && err2 == nil {
			if len(tokens[1]) == 4 { // MM-YYYY
				if v1 >= 1 && v1 <= 12 {
					start := time.Date(v2, time.Month(v1), 1, 0, 0, 0, 0, time.Local)
					end := time.Date(v2, time.Month(v1), lastDayOfMonth(v2, v1), 0, 0, 0, 0, time.Local)
					return start, end, nil
				}
			} else if len(tokens[1]) <= 2 { // MM-MM
				if v1 >= 1 && v1 <= 12 && v2 >= 1 && v2 <= 12 {
					start := time.Date(currYear, time.Month(v1), 1, 0, 0, 0, 0, time.Local)
					end := time.Date(currYear, time.Month(v2), lastDayOfMonth(currYear, v2), 0, 0, 0, 0, time.Local)
					if start.After(end) {
						start, end = end, start
					}
					return start, end, nil
				}
			}
		}
	}

	// 4 tokens: DD-MM-DD-MM (e.g. 01.12-15.12)
	if len(tokens) == 4 {
		d1, err1 := ParseDate(tokens[0]+"-"+tokens[1], time.Time{})
		d2, err2 := ParseDate(tokens[2]+"-"+tokens[3], time.Time{})
		if err1 == nil && err2 == nil {
			if d1.After(d2) {
				d1, d2 = d2, d1
			}
			return d1, d2, nil
		}
	}

	// 6 tokens: DD-MM-YYYY-DD-MM-YYYY
	if len(tokens) == 6 {
		d1, err1 := ParseDate(tokens[0]+"-"+tokens[1]+"-"+tokens[2], time.Time{})
		d2, err2 := ParseDate(tokens[3]+"-"+tokens[4]+"-"+tokens[5], time.Time{})
		if err1 == nil && err2 == nil {
			if d1.After(d2) {
				d1, d2 = d2, d1
			}
			return d1, d2, nil
		}
	}

	// Fallback: try parsing as a single date
	d, err := ParseDate(input, time.Time{})
	if err == nil {
		return d, d, nil
	}

	return time.Time{}, time.Time{}, fmt.Errorf("не удалось распознать период: %s", input)
}

func lastDayOfMonth(year, month int) int {
	return time.Date(year, time.Month(month+1), 0, 0, 0, 0, 0, time.Local).Day()
}

