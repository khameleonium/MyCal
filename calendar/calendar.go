package calendar

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"mycalendar/models"
	"mycalendar/storage"
)

// Service manages calendar entries and persistence.
type Service struct {
	data     *models.Calendar
	dir      string
	baseName string
	mode     string
}

// NewService loads the calendar from the given directory, base file name, and split mode.
func NewService(ctx context.Context, dir, baseName, mode string) (*Service, error) {
	data, err := storage.Load(ctx, dir, baseName, mode)
	if err != nil {
		return nil, err
	}
	return &Service{data: data, dir: dir, baseName: baseName, mode: mode}, nil
}

// Save persists the calendar to disk.
func (s *Service) Save(ctx context.Context) error {
	return storage.Save(ctx, s.dir, s.baseName, s.data, s.mode)
}

// Mode returns the current split mode.
func (s *Service) Mode() string {
	return s.mode
}

// UpdateMode changes the split mode. The caller is responsible for calling Save after this.
func (s *Service) UpdateMode(mode string) {
	s.mode = mode
}

// AddEntry adds a session to the calendar.
func (s *Service) AddEntry(session models.Session) error {
	if session.ID == "" {
		return fmt.Errorf("ID записи не сгенерирован")
	}

	dateKey := session.Date()
	for i := range s.data.Entries {
		if s.data.Entries[i].Date == dateKey {
			s.data.Entries[i].Sessions = append(s.data.Entries[i].Sessions, session)
			sort.Slice(s.data.Entries[i].Sessions, func(a, b int) bool {
				return s.data.Entries[i].Sessions[a].Time < s.data.Entries[i].Sessions[b].Time
			})
			return nil
		}
	}

	s.data.Entries = append(s.data.Entries, models.DateEntry{
		Date:     dateKey,
		Sessions: []models.Session{session},
	})
	sort.Slice(s.data.Entries, func(i, j int) bool {
		return s.data.Entries[i].Date < s.data.Entries[j].Date
	})
	return nil
}

// FindConflicts returns all sessions at the given date and time.
func (s *Service) FindConflicts(date, tm string) []models.Session {
	dateKey := strings.ReplaceAll(date, "-", "")
	var result []models.Session
	for _, de := range s.data.Entries {
		entryDate := strings.ReplaceAll(de.Date, "-", "")
		if entryDate == dateKey {
			for _, sess := range de.Sessions {
				sessionTime := strings.ReplaceAll(sess.Time, ":", "")
				searchTime := strings.ReplaceAll(tm, ":", "")
				if sessionTime == searchTime {
					result = append(result, sess)
				}
			}
		}
	}
	return result
}

// GenerateID creates a unique ID in YYYYMMDDHHMMSS format.
func (s *Service) GenerateID(date, tm time.Time) string {
	base := date.Format("20060102") + tm.Format("1504")
	now := time.Now()
	sec := now.Second()

	usedSecs := make(map[int]bool)
	for _, de := range s.data.Entries {
		for _, sess := range de.Sessions {
			if strings.HasPrefix(sess.ID, base) {
				if len(sess.ID) == models.IDLen {
					var ss int
					if _, err := fmt.Sscanf(sess.ID[12:14], "%d", &ss); err == nil {
						usedSecs[ss] = true
					}
				}
			}
		}
	}

	attempts := 0
	for usedSecs[sec] && attempts < 100 {
		sec = (sec + 1) % 100
		attempts++
	}

	return fmt.Sprintf("%s%02d", base, sec)
}

// FindByID locates all sessions by ID.
func (s *Service) FindByID(id string) []models.Session {
	var results []models.Session
	for di := range s.data.Entries {
		for si := range s.data.Entries[di].Sessions {
			if s.data.Entries[di].Sessions[si].ID == id {
				results = append(results, s.data.Entries[di].Sessions[si])
			}
		}
	}
	return results
}

// FindByDate returns the DateEntry for a given date string (YYYY-MM-DD).
func (s *Service) FindByDate(date string) *models.DateEntry {
	for i := range s.data.Entries {
		if s.data.Entries[i].Date == date {
			return &s.data.Entries[i]
		}
	}
	return nil
}

// FindByDateTime returns all sessions at a given date and time.
func (s *Service) FindByDateTime(date, tm string) []models.Session {
	return s.FindConflicts(date, tm)
}

// FindByPeriod returns DateEntries whose dates fall within [start, end].
func (s *Service) FindByPeriod(start, end time.Time) []models.DateEntry {
	var result []models.DateEntry
	startStr := start.Format("2006-01-02")
	endStr := end.Format("2006-01-02")

	for _, de := range s.data.Entries {
		if de.Date >= startStr && de.Date <= endStr {
			result = append(result, de)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Date < result[j].Date
	})
	return s.hydrate(result)
}

func (s *Service) hydrate(entries []models.DateEntry) []models.DateEntry {
	hydrated := make([]models.DateEntry, len(entries))
	for i, de := range entries {
		hydratedDE := models.DateEntry{Date: de.Date, Sessions: make([]models.Session, len(de.Sessions))}
		for j, session := range de.Sessions {
			if strings.HasSuffix(session.Name, "_r") {
				origs := s.FindByID(session.Name)
				if len(origs) > 0 {
					orig := origs[0]
					session.Type = orig.Type
					session.Duration = orig.Duration
					session.Notes = orig.Notes
					session.Status = orig.Status
					session.Name = orig.Name
				}
			}
			hydratedDE.Sessions[j] = session
		}
		hydrated[i] = hydratedDE
	}
	return hydrated
}

// HydrateSession returns a hydrated copy of a single session.
func (s *Service) HydrateSession(session models.Session) models.Session {
	if strings.HasSuffix(session.Name, "_r") {
		origs := s.FindByID(session.Name)
		if len(origs) > 0 {
			orig := origs[0]
			session.Type = orig.Type
			session.Duration = orig.Duration
			session.Notes = orig.Notes
			session.Status = orig.Status
			session.Name = orig.Name
		}
	}
	return session
}

// EditEntry updates the session identified by its ID. If multiple match, updates the first one.
func (s *Service) EditEntry(id string, updated models.Session) error {
	var session *models.Session
	var di int
	found := false
outer:
	for i := range s.data.Entries {
		for j := range s.data.Entries[i].Sessions {
			if s.data.Entries[i].Sessions[j].ID == id {
				session = &s.data.Entries[i].Sessions[j]
				di = i
				found = true
				break outer
			}
		}
	}

	if !found {
		return fmt.Errorf("запись с ID %s не найдена", id)
	}

	if updated.Time != "" {
		session.Time = updated.Time
	}
	if updated.Name != "" {
		session.Name = updated.Name
	}
	if updated.Type != "" {
		session.Type = updated.Type
	}
	if updated.Duration != 0 {
		session.Duration = updated.Duration
	}
	if updated.Notes != "" {
		session.Notes = updated.Notes
	}
	if updated.Status != "" {
		session.Status = updated.Status
	}

	// Re-sort sessions in case Time was modified
	if updated.Time != "" {
		de := &s.data.Entries[di]
		sort.Slice(de.Sessions, func(a, b int) bool {
			return de.Sessions[a].Time < de.Sessions[b].Time
		})
	}

	return nil
}

// DeleteEntry removes ALL sessions by their ID. Returns number of deleted entries.
func (s *Service) DeleteEntry(id string) int {
	count := 0
	for i := len(s.data.Entries) - 1; i >= 0; i-- {
		de := &s.data.Entries[i]
		for j := len(de.Sessions) - 1; j >= 0; j-- {
			if de.Sessions[j].ID == id {
				de.Sessions = append(de.Sessions[:j], de.Sessions[j+1:]...)
				count++
			}
		}
		if len(de.Sessions) == 0 {
			s.data.Entries = append(s.data.Entries[:i], s.data.Entries[i+1:]...)
		}
	}
	return count
}

// DeleteRepeats removes all duplicate sessions that reference the given original ID.
func (s *Service) DeleteRepeats(originalID string) int {
	count := 0
	for i := len(s.data.Entries) - 1; i >= 0; i-- {
		de := &s.data.Entries[i]
		for j := len(de.Sessions) - 1; j >= 0; j-- {
			if de.Sessions[j].Name == originalID {
				de.Sessions = append(de.Sessions[:j], de.Sessions[j+1:]...)
				count++
			}
		}
		if len(de.Sessions) == 0 {
			s.data.Entries = append(s.data.Entries[:i], s.data.Entries[i+1:]...)
		}
	}
	return count
}

// DeleteAll removes all entries.
func (s *Service) DeleteAll() {
	s.data.Entries = []models.DateEntry{}
}

// DeleteByPeriod removes all DateEntries in [start, end] inclusive.
// Returns the number of sessions deleted.
func (s *Service) DeleteByPeriod(start, end time.Time) int {
	startStr := start.Format("2006-01-02")
	endStr := end.Format("2006-01-02")
	count := 0

	for i := len(s.data.Entries) - 1; i >= 0; i-- {
		if s.data.Entries[i].Date >= startStr && s.data.Entries[i].Date <= endStr {
			count += len(s.data.Entries[i].Sessions)
			s.data.Entries = append(s.data.Entries[:i], s.data.Entries[i+1:]...)
		}
	}
	return count
}

// GetWeekEntries returns entries for the ISO week containing refDate.
func (s *Service) GetWeekEntries(refDate time.Time) []models.DateEntry {
	weekday := refDate.Weekday()
	if weekday == 0 {
		weekday = 7
	}
	monday := refDate.AddDate(0, 0, -int(weekday-1))
	sunday := monday.AddDate(0, 0, 6)
	return s.FindByPeriod(monday, sunday)
}

// GetMonthEntries returns entries for the month containing refDate.
func (s *Service) GetMonthEntries(refDate time.Time) []models.DateEntry {
	firstDay := time.Date(refDate.Year(), refDate.Month(), 1, 0, 0, 0, 0, refDate.Location())
	lastDay := firstDay.AddDate(0, 1, -1)
	return s.FindByPeriod(firstDay, lastDay)
}

// GetAllEntries returns all entries in the calendar.
func (s *Service) GetAllEntries() []models.DateEntry {
	result := make([]models.DateEntry, len(s.data.Entries))
	copy(result, s.data.Entries)
	sort.Slice(result, func(i, j int) bool {
		return result[i].Date < result[j].Date
	})
	return s.hydrate(result)
}

// TotalHours calculates the total number of hours across all sessions.
func TotalHours(entries []models.DateEntry) float64 {
	var totalMinutes int
	for _, de := range entries {
		for _, sess := range de.Sessions {
			totalMinutes += sess.Duration
		}
	}
	return float64(totalMinutes) / 60.0
}

// AllTypes returns the default types plus all unique types found in the data.
func (s *Service) AllTypes() []string {
	defaults := []string{"Стрельба из лука", "Метание ножей"}
	seen := make(map[string]bool)
	for _, t := range defaults {
		seen[t] = true
	}
	for _, de := range s.data.Entries {
		for _, sess := range de.Sessions {
			if sess.Type != "" {
				seen[sess.Type] = true
			}
		}
	}
	types := make([]string, 0, len(seen))
	for t := range seen {
		types = append(types, t)
	}
	sort.Strings(types)

	result := make([]string, 0, len(types))
	for _, t := range defaults {
		result = append(result, t)
	}
	for _, t := range types {
		if !slices.Contains(defaults, t) {
			result = append(result, t)
		}
	}
	return result
}

// AllStatuses returns the default statuses plus all unique statuses found in the data.
func (s *Service) AllStatuses() []string {
	defaults := []string{"Активно", "Пропущено", "Отменено"}
	seen := make(map[string]bool)
	for _, st := range defaults {
		seen[st] = true
	}
	for _, de := range s.data.Entries {
		for _, sess := range de.Sessions {
			if sess.Status != "" {
				seen[sess.Status] = true
			}
		}
	}
	statuses := make([]string, 0, len(seen))
	for st := range seen {
		statuses = append(statuses, st)
	}
	sort.Strings(statuses)

	result := make([]string, 0, len(statuses))
	for _, st := range defaults {
		result = append(result, st)
	}
	for _, st := range statuses {
		if !slices.Contains(defaults, st) {
			result = append(result, st)
		}
	}
	return result
}

// GetTodayEntries returns entries for the given reference date.
func (s *Service) GetTodayEntries(refDate time.Time) []models.DateEntry {
	start := time.Date(refDate.Year(), refDate.Month(), refDate.Day(), 0, 0, 0, 0, refDate.Location())
	end := start
	return s.FindByPeriod(start, end)
}

// SearchByName returns sessions whose name contains the given substring (case-insensitive).
func (s *Service) SearchByName(name string) []models.Session {
	lower := strings.ToLower(name)
	var result []models.Session
	for _, de := range s.data.Entries {
		for _, sess := range de.Sessions {
			if strings.Contains(strings.ToLower(sess.Name), lower) {
				result = append(result, sess)
			}
		}
	}
	return result
}

// SearchByType returns sessions whose type contains the given substring (case-insensitive).
func (s *Service) SearchByType(typ string) []models.Session {
	lower := strings.ToLower(typ)
	var result []models.Session
	for _, de := range s.data.Entries {
		for _, sess := range de.Sessions {
			if strings.Contains(strings.ToLower(sess.Type), lower) {
				result = append(result, sess)
			}
		}
	}
	return result
}

// SearchByNameOrType returns sessions matching name or type substring (case-insensitive).
func (s *Service) SearchByNameOrType(query string) []models.Session {
	lower := strings.ToLower(query)
	var result []models.Session
	for _, de := range s.data.Entries {
		for _, sess := range de.Sessions {
			if strings.Contains(strings.ToLower(sess.Name), lower) || strings.Contains(strings.ToLower(sess.Type), lower) {
				result = append(result, sess)
			}
		}
	}
	return result
}
