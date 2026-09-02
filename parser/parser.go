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

// ParseDate parses a date, leniently. Supported shapes (any separator run of
// . - / \ or spaces): DD-MM-YYYY, YYYY-MM-DD, DD-MM (year from defaultDate).
//
// Out-of-range parts roll over the way a person expects: "32.01.2026" ->
// 01.02.2026, "13.2026" is not a date but "13" as a month rolls to next year.
// Empty input returns defaultDate. Use ParseDateStrict / ValidateDate to tell
// whether the input was a real calendar date as typed.
func ParseDate(input string, defaultDate time.Time) (time.Time, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return defaultDate, nil
	}

	loc := time.Local
	refYear := time.Now().Year()
	if !defaultDate.IsZero() {
		loc = defaultDate.Location()
		refYear = defaultDate.Year()
	}

	tokens := strings.Split(normalizeSeparators(input), "-")
	nums := make([]int, 0, len(tokens))
	for _, tok := range tokens {
		n, err := strconv.Atoi(tok)
		if err != nil {
			return time.Time{}, fmt.Errorf("не удалось распознать дату: %s", input)
		}
		nums = append(nums, n)
	}

	var y, m, d int
	switch len(nums) {
	case 3:
		if len(tokens[0]) == 4 { // YYYY-MM-DD
			y, m, d = nums[0], nums[1], nums[2]
		} else { // DD-MM-YYYY
			d, m, y = nums[0], nums[1], nums[2]
		}
	case 2: // DD-MM, year from the reference date
		d, m, y = nums[0], nums[1], refYear
	default:
		return time.Time{}, fmt.Errorf("не удалось распознать дату: %s", input)
	}

	// Bounds loose enough to allow a deliberate roll-over, tight enough to
	// reject noise.
	if y < 1 || y > 9999 || m < 1 || m > 99 || d < 1 || d > 99 {
		return time.Time{}, fmt.Errorf("не удалось распознать дату: %s", input)
	}
	return time.Date(y, time.Month(m), d, 0, 0, 0, 0, loc), nil
}

// ParseDateStrict parses only a genuine calendar date exactly as written
// (DD-MM-YYYY, YYYY-MM-DD, or DD-MM with no year). It errors on anything that
// would have to roll over (e.g. day 32, month 13, 29 Feb in a common year).
func ParseDateStrict(input string) (time.Time, error) {
	normalized := normalizeSeparators(strings.TrimSpace(input))
	for _, layout := range []string{"02-01-2006", "2006-01-02", "02-01"} {
		if t, err := time.Parse(layout, normalized); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("не удалось распознать дату: %s", input)
}

// ParseTime parses a wall-clock time, leniently. Accepted forms:
//
//	"9"      -> 09:00        "24"     -> 00:00
//	"930"    -> 09:30        "1430"   -> 14:30
//	"9:5"    -> 09:05        "14:30"  -> 14:30
//	"14.30", "14-30", "14 30" (any separator run)
//
// Empty input returns ErrEmptyTime; anything out of range 00:00–23:59 is an error.
func ParseTime(input string) (time.Time, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return time.Time{}, ErrEmptyTime
	}
	s = timeSepRegexp.ReplaceAllString(s, ":")

	var hh, mm int
	var err error
	if i := strings.IndexByte(s, ':'); i >= 0 {
		hh, err = atoiStrict(s[:i])
		if err == nil {
			mm, err = atoiStrict(s[i+1:])
		}
	} else {
		switch len(s) {
		case 1, 2:
			hh, err = atoiStrict(s)
		case 3:
			hh, err = atoiStrict(s[:1])
			if err == nil {
				mm, err = atoiStrict(s[1:])
			}
		case 4:
			hh, err = atoiStrict(s[:2])
			if err == nil {
				mm, err = atoiStrict(s[2:])
			}
		default:
			err = errBadFormat
		}
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("не удалось распознать время: %s", input)
	}
	if hh == 24 && mm == 0 {
		hh = 0
	}
	if hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return time.Time{}, fmt.Errorf("время вне диапазона 00:00–23:59: %s", input)
	}
	return time.Date(0, 1, 1, hh, mm, 0, 0, time.UTC), nil
}

var (
	timeSepRegexp = regexp.MustCompile(`[.\-\s]+`)
	errBadFormat  = errors.New("bad format")
)

func atoiStrict(s string) (int, error) {
	if s == "" {
		return 0, errBadFormat
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, errBadFormat
		}
	}
	return strconv.Atoi(s)
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

// ValidateDate reports whether rawInput is a genuine calendar date as typed
// (i.e. ParseDate did not have to roll any part over). parsed is accepted for
// call-site symmetry but not required.
func ValidateDate(rawInput string, parsed time.Time) bool {
	strict, err := ParseDateStrict(rawInput)
	if err != nil {
		return false
	}
	// For the year-less form, only day and month are meaningful.
	if len(strings.Split(normalizeSeparators(rawInput), "-")) == 2 {
		return strict.Day() == parsed.Day() && strict.Month() == parsed.Month()
	}
	return strict.Equal(time.Date(parsed.Year(), parsed.Month(), parsed.Day(), 0, 0, 0, 0, strict.Location()))
}

// sepRegexp matches any run of date separators (dot, dash, slash, backslash,
// whitespace) so that "29 . 12 -- 2025" collapses cleanly to "29-12-2025".
var sepRegexp = regexp.MustCompile(`[.\-/\\\s]+`)

func normalizeSeparators(s string) string {
	return sepRegexp.ReplaceAllString(strings.TrimSpace(s), "-")
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

	// 2. Normalize separators to handle tokens separated by '.', '/', '\', '-'.
	// normalizeSeparators already collapses repeated separators.
	tokens := strings.Split(normalizeSeparators(input), "-")

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
