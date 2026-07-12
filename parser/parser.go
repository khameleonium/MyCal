package parser

import (
	"errors"
	"fmt"
	"regexp"
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

var sepRegexp = regexp.MustCompile(`[.\-]`)

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
