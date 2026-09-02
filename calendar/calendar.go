// Package calendar is the domain layer: an in-memory list of sessions with
// query, mutation and integrity operations, backed by the storage package.
package calendar

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"mycalendar/models"
	"mycalendar/parser"
	"mycalendar/storage"
)

// DefaultTypes and DefaultStatuses seed the pick-lists shown when adding an
// entry. Exported so a caller can override them.
var (
	DefaultTypes    = []string{"Стрельба из лука", "Метание ножей"}
	DefaultStatuses = []string{"Активно", "Пропущено", "Отменено"}
)

// Service holds the calendar in memory and persists it on demand.
type Service struct {
	sessions []models.Session
	dir      string
	baseName string
	mode     string
	warnings []string
}

// NewService loads the calendar. A non-nil error means the data could not be
// read at all; recoverable problems are reported via Warnings.
func NewService(ctx context.Context, dir, baseName, mode string) (*Service, error) {
	cal, warnings, err := storage.Load(ctx, dir, baseName, mode)
	if err != nil {
		return nil, err
	}
	return &Service{
		sessions: cal.Sessions,
		dir:      dir,
		baseName: baseName,
		mode:     mode,
		warnings: warnings,
	}, nil
}

// Warnings returns (and clears) any recoverable problems noticed while loading.
func (s *Service) Warnings() []string {
	w := s.warnings
	s.warnings = nil
	return w
}

// Save persists the calendar to disk.
func (s *Service) Save(ctx context.Context) error {
	return storage.Save(ctx, s.dir, s.baseName, &models.Calendar{Sessions: s.sessions}, s.mode)
}

func (s *Service) Mode() string            { return s.mode }
func (s *Service) DataDir() string         { return s.dir }
func (s *Service) BaseName() string        { return s.baseName }
func (s *Service) SetMode(mode string)     { s.mode = mode }
func (s *Service) SetBaseName(name string) { s.baseName = name }
func (s *Service) Count() int              { return len(s.sessions) }

// RemoveDataFiles deletes on-disk files for a (now unused) base name in the
// data directory, in every split-mode layout.
func (s *Service) RemoveDataFiles(baseName string) {
	storage.RemoveDataFiles(s.dir, baseName)
}

// ---------------------------------------------------------------- queries ----

// All returns a sorted copy of every session.
func (s *Service) All() []models.Session {
	out := append([]models.Session(nil), s.sessions...)
	storage.SortSessions(out)
	return out
}

// InRange returns sessions whose date falls within [start, end] (inclusive).
func (s *Service) InRange(start, end time.Time) []models.Session {
	lo, hi := start.Format("2006-01-02"), end.Format("2006-01-02")
	var out []models.Session
	for _, x := range s.sessions {
		if d := x.Date(); d >= lo && d <= hi {
			out = append(out, x)
		}
	}
	storage.SortSessions(out)
	return out
}

func (s *Service) Day(ref time.Time) []models.Session {
	d := DayStart(ref)
	return s.InRange(d, d)
}

func (s *Service) Week(ref time.Time) []models.Session {
	mon, sun := WeekBounds(ref)
	return s.InRange(mon, sun)
}

func (s *Service) Month(ref time.Time) []models.Session {
	first, last := MonthBounds(ref)
	return s.InRange(first, last)
}

// OnDate returns sessions on an ISO (YYYY-MM-DD) date.
func (s *Service) OnDate(iso string) []models.Session {
	var out []models.Session
	for _, x := range s.sessions {
		if x.Date() == iso {
			out = append(out, x)
		}
	}
	storage.SortSessions(out)
	return out
}

// ByID returns the session with the given ID.
func (s *Service) ByID(id string) (models.Session, bool) {
	for _, x := range s.sessions {
		if x.ID == id {
			return x, true
		}
	}
	return models.Session{}, false
}

// BySeries returns every session in a repeating series, date-ordered.
func (s *Service) BySeries(seriesID string) []models.Session {
	if seriesID == "" {
		return nil
	}
	var out []models.Session
	for _, x := range s.sessions {
		if x.SeriesID == seriesID {
			out = append(out, x)
		}
	}
	storage.SortSessions(out)
	return out
}

// Conflicts returns sessions at the exact same ISO date and HH:MM time.
func (s *Service) Conflicts(iso, hhmm string) []models.Session {
	var out []models.Session
	for _, x := range s.sessions {
		if x.Date() == iso && x.Time == hhmm {
			out = append(out, x)
		}
	}
	return out
}

// Search returns sessions whose name or type contains query (case-insensitive).
func (s *Service) Search(query string) []models.Session {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}
	var out []models.Session
	for _, x := range s.sessions {
		if strings.Contains(strings.ToLower(x.Name), q) || strings.Contains(strings.ToLower(x.Type), q) {
			out = append(out, x)
		}
	}
	storage.SortSessions(out)
	return out
}

// Types returns DefaultTypes plus extra ones (seed + used) found in the data.
func (s *Service) Types(seed ...string) []string {
	return merge(DefaultTypes, seed, s.distinct(func(x models.Session) string { return x.Type }))
}

// Statuses returns DefaultStatuses plus extra ones found in the data.
func (s *Service) Statuses(seed ...string) []string {
	return merge(DefaultStatuses, seed, s.distinct(func(x models.Session) string { return x.Status }))
}

func (s *Service) distinct(field func(models.Session) string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range s.sessions {
		if v := field(x); v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func merge(defaults, seed, found []string) []string {
	known := map[string]bool{}
	result := make([]string, 0, len(defaults)+len(seed)+len(found))
	for _, v := range defaults {
		if !known[v] {
			known[v] = true
			result = append(result, v)
		}
	}
	var extra []string
	for _, list := range [][]string{seed, found} {
		for _, v := range list {
			if v != "" && !known[v] {
				known[v] = true
				extra = append(extra, v)
			}
		}
	}
	sort.Strings(extra)
	return append(result, extra...)
}

// TotalMinutes sums session durations.
func TotalMinutes(sessions []models.Session) int {
	total := 0
	for _, x := range sessions {
		total += x.Duration
	}
	return total
}

// -------------------------------------------------------------- mutations ----

// GenerateID returns a unique 14-char ID: YYYYMMDDHHMM + a two-digit counter
// that is the smallest value free for that minute. It widens past two digits
// only if a single minute somehow holds 100+ entries.
func (s *Service) GenerateID(date, tm time.Time) string {
	prefix := date.Format("20060102") + tm.Format("1504")
	used := make(map[string]bool, len(s.sessions))
	for _, x := range s.sessions {
		used[x.ID] = true
	}
	for n := 0; n < 100; n++ {
		if id := fmt.Sprintf("%s%02d", prefix, n); !used[id] {
			return id
		}
	}
	for n := 100; ; n++ {
		if id := fmt.Sprintf("%s%d", prefix, n); !used[id] {
			return id
		}
	}
}

// Add inserts a session, keeping the list sorted.
func (s *Service) Add(session models.Session) error {
	if session.ID == "" {
		return fmt.Errorf("внутренняя ошибка: ID записи не сгенерирован")
	}
	if _, exists := s.ByID(session.ID); exists {
		return fmt.Errorf("запись с ID %s уже существует", session.ID)
	}
	s.sessions = append(s.sessions, session)
	storage.SortSessions(s.sessions)
	return nil
}

// Patch describes a change. A nil field is left untouched; a non-nil field is
// applied (including an empty string, which clears the field).
type Patch struct {
	Time     *string
	Name     *string
	Type     *string
	Duration *int
	Notes    *string
	Status   *string
}

// Empty reports whether the patch would change nothing.
func (p Patch) Empty() bool {
	return p.Time == nil && p.Name == nil && p.Type == nil &&
		p.Duration == nil && p.Notes == nil && p.Status == nil
}

func (p Patch) applyTo(x *models.Session) {
	if p.Time != nil {
		x.Time = *p.Time
	}
	if p.Name != nil {
		x.Name = *p.Name
	}
	if p.Type != nil {
		x.Type = *p.Type
	}
	if p.Duration != nil {
		x.Duration = *p.Duration
	}
	if p.Notes != nil {
		x.Notes = *p.Notes
	}
	if p.Status != nil {
		x.Status = *p.Status
	}
}

// Update applies patch to the session with the given ID.
func (s *Service) Update(id string, patch Patch) error {
	for i := range s.sessions {
		if s.sessions[i].ID == id {
			patch.applyTo(&s.sessions[i])
			storage.SortSessions(s.sessions)
			return nil
		}
	}
	return fmt.Errorf("запись с ID %s не найдена", id)
}

// UpdateSeries applies patch to every session in the series. Returns the count.
func (s *Service) UpdateSeries(seriesID string, patch Patch) int {
	if seriesID == "" {
		return 0
	}
	n := 0
	for i := range s.sessions {
		if s.sessions[i].SeriesID == seriesID {
			patch.applyTo(&s.sessions[i])
			n++
		}
	}
	if n > 0 {
		storage.SortSessions(s.sessions)
	}
	return n
}

// Delete removes the session with the given ID. Returns whether it existed.
func (s *Service) Delete(id string) bool {
	return s.deleteWhere(func(x models.Session) bool { return x.ID == id }) > 0
}

// DeleteSeries removes every session in a series. Returns the count.
func (s *Service) DeleteSeries(seriesID string) int {
	if seriesID == "" {
		return 0
	}
	return s.deleteWhere(func(x models.Session) bool { return x.SeriesID == seriesID })
}

// DeleteRange removes sessions whose date is within [start, end]. Returns count.
func (s *Service) DeleteRange(start, end time.Time) int {
	lo, hi := start.Format("2006-01-02"), end.Format("2006-01-02")
	return s.deleteWhere(func(x models.Session) bool {
		d := x.Date()
		return d >= lo && d <= hi
	})
}

// DeleteAll clears the calendar.
func (s *Service) DeleteAll() { s.sessions = nil }

func (s *Service) deleteWhere(match func(models.Session) bool) int {
	kept := s.sessions[:0]
	n := 0
	for _, x := range s.sessions {
		if match(x) {
			n++
			continue
		}
		kept = append(kept, x)
	}
	s.sessions = kept
	return n
}

// -------------------------------------------------------------- integrity ----

// Issue is a structural problem found by Validate.
type Issue struct {
	ID     string
	Detail string
}

// Validate scans for structural problems: missing/short IDs, IDs whose date
// part is unparseable, duplicate IDs, negative durations, unparseable times.
func (s *Service) Validate() []Issue {
	var issues []Issue
	seen := map[string]int{}
	for _, x := range s.sessions {
		switch {
		case x.ID == "":
			issues = append(issues, Issue{"", "запись без ID (" + x.Name + ")"})
		case len(x.ID) < 10 || !isDigits(x.ID[:8]):
			issues = append(issues, Issue{x.ID, "ID не содержит корректной даты"})
		default:
			if _, err := time.Parse("2006-01-02", x.Date()); err != nil {
				issues = append(issues, Issue{x.ID, "дата в ID недействительна: " + x.Date()})
			}
		}
		if x.ID != "" {
			seen[x.ID]++
			if seen[x.ID] == 2 {
				issues = append(issues, Issue{x.ID, "дублирующийся ID"})
			}
		}
		if x.Duration < 0 {
			issues = append(issues, Issue{x.ID, fmt.Sprintf("отрицательная продолжительность (%d)", x.Duration)})
		}
		if x.Time != "" {
			if _, err := parser.ParseTime(x.Time); err != nil {
				issues = append(issues, Issue{x.ID, "время не распознано: " + x.Time})
			}
		}
	}
	return issues
}

// Repair fixes what it safely can: drops sessions with an unusable ID,
// regenerates duplicate IDs, clamps negative durations, normalises times.
// Returns the number of sessions changed or dropped.
func (s *Service) Repair() int {
	changed := 0
	var kept []models.Session
	seen := map[string]bool{}

	for _, x := range s.sessions {
		if x.ID == "" || len(x.ID) < 10 || !isDigits(x.ID[:8]) {
			changed++ // drop: cannot place it in time
			continue
		}
		if _, err := time.Parse("2006-01-02", x.Date()); err != nil {
			changed++
			continue
		}
		if seen[x.ID] {
			base := x.ID[:12]
			for n := 0; ; n++ {
				cand := fmt.Sprintf("%s%02d", base, n)
				if n >= 100 {
					cand = fmt.Sprintf("%s%d", base, n)
				}
				if !seen[cand] {
					x.ID = cand
					break
				}
			}
			changed++
		}
		if x.Duration < 0 {
			x.Duration = 0
			changed++
		}
		if tm, err := parser.ParseTime(x.Time); err == nil {
			if norm := tm.Format("15:04"); norm != x.Time {
				x.Time = norm
				changed++
			}
		} else {
			// Unparseable — fall back to the HHMM encoded in the ID.
			repaired := "00:00"
			if len(x.ID) >= 12 {
				if t, e := parser.ParseTime(x.ID[8:12]); e == nil {
					repaired = t.Format("15:04")
				}
			}
			x.Time = repaired
			changed++
		}
		seen[x.ID] = true
		kept = append(kept, x)
	}

	s.sessions = kept
	storage.SortSessions(s.sessions)
	return changed
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}
