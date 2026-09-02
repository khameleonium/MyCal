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
