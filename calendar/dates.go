package calendar

import "time"

// DayStart returns midnight of t in t's location.
func DayStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// WeekBounds returns Monday 00:00 and Sunday 00:00 of the ISO week containing ref.
func WeekBounds(ref time.Time) (monday, sunday time.Time) {
	wd := int(ref.Weekday())
	if wd == 0 { // Sunday
		wd = 7
	}
	monday = DayStart(ref).AddDate(0, 0, -(wd - 1))
	sunday = monday.AddDate(0, 0, 6)
	return
}

// MonthBounds returns the first and last day (both at 00:00) of ref's month.
func MonthBounds(ref time.Time) (first, last time.Time) {
	first = time.Date(ref.Year(), ref.Month(), 1, 0, 0, 0, 0, ref.Location())
	last = first.AddDate(0, 1, -1)
	return
}

// MaxOccurrences caps one repeating series, so an accidental "every day until
// 2099" cannot fill the calendar.
const MaxOccurrences = 5000

// RepeatDates returns the occurrence dates strictly after start, up to and
// including until. Every date is computed from start rather than from the
// previous occurrence, so a series never drifts. truncated reports whether the
// cap cut the series short.
func RepeatDates(start, until time.Time, mode string) (dates []time.Time, truncated bool) {
	for i := 1; ; i++ {
		var cur time.Time
		switch mode {
		case "d":
			cur = start.AddDate(0, 0, i)
		case "w":
			cur = start.AddDate(0, 0, 7*i)
		case "m":
			cur = AddMonths(start, i)
		default:
			return nil, false
		}
		if cur.After(until) {
			break
		}
		dates = append(dates, cur)
		if len(dates) >= MaxOccurrences {
			truncated = true
			break
		}
	}
	return dates, truncated
}

// AddMonths adds n months to t, clamping the day to the end of the target
// month. Go's AddDate would roll 31 January into 3 March and shift every later
// occurrence with it; clamping keeps a monthly series on its own day-of-month
// and lands on the last day of shorter months.
func AddMonths(t time.Time, n int) time.Time {
	first := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location()).AddDate(0, n, 0)
	day := t.Day()
	if last := DaysInMonth(first.Year(), first.Month()); day > last {
		day = last
	}
	return time.Date(first.Year(), first.Month(), day, 0, 0, 0, 0, t.Location())
}

// DaysInMonth returns the number of days in the given month and year.
func DaysInMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}
