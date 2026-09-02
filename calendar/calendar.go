package calendar

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"mycalendar/models"
	"mycalendar/storage"
)

// DefaultTypes and DefaultStatuses seed the pick-lists shown when adding an
// entry. They are exported so a caller can replace them with its own list.
var (
	DefaultTypes    = []string{"Стрельба из лука", "Метание ножей"}
	DefaultStatuses = []string{"Активно", "Пропущено", "Отменено"}
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
func (s *Service) Mode() string { return s.mode }

// DataDir returns the directory calendar files are stored in.
func (s *Service) DataDir() string { return s.dir }

// UpdateMode changes the split mode. The caller must call Save afterwards.
func (s *Service) UpdateMode(mode string) { s.mode = mode }

// AddEntry inserts a session, keeping entries sorted by date and sessions
// within a date sorted by time, without re-sorting the whole calendar.
func (s *Service) AddEntry(session models.Session) error {
	if session.ID == "" {
		return fmt.Errorf("ID записи не сгенерирован")
	}
	dateKey := session.Date()

	i := sort.Search(len(s.data.Entries), func(i int) bool {
		return s.data.Entries[i].Date >= dateKey
	})
	if i < len(s.data.Entries) && s.data.Entries[i].Date == dateKey {
		de := &s.data.Entries[i]
		j := sort.Search(len(de.Sessions), func(j int) bool {
			return de.Sessions[j].Time > session.Time
		})
		de.Sessions = append(de.Sessions, models.Session{})
		copy(de.Sessions[j+1:], de.Sessions[j:])
		de.Sessions[j] = session
		return nil
	}

	s.data.Entries = append(s.data.Entries, models.DateEntry{})
	copy(s.data.Entries[i+1:], s.data.Entries[i:])
	s.data.Entries[i] = models.DateEntry{Date: dateKey, Sessions: []models.Session{session}}
	return nil
}

// FindConflicts returns hydrated sessions at the given date and time.
func (s *Service) FindConflicts(date, tm string) []models.Session {
	dateKey := strings.ReplaceAll(date, "-", "")
	searchTime := strings.ReplaceAll(tm, ":", "")
	originals := s.seriesOriginals()
	var result []models.Session
	for _, de := range s.data.Entries {
		if strings.ReplaceAll(de.Date, "-", "") != dateKey {
			continue
		}
		for _, sess := range de.Sessions {
			if strings.ReplaceAll(sess.Time, ":", "") == searchTime {
				result = append(result, hydrateWith(sess, originals))
			}
		}
	}
	return result
}

// GenerateID creates an ID in YYYYMMDDHHMMSS format, choosing a seconds value
// that is not already used by another session in the same minute.
func (s *Service) GenerateID(date, tm time.Time) string {
	return s.generateID(date, tm, time.Now().Second())
}

func (s *Service) generateID(date, tm time.Time, preferredSec int) string {
	base := date.Format("20060102") + tm.Format("1504")

	used := make(map[int]bool)
	for _, de := range s.data.Entries {
		for _, sess := range de.Sessions {
			if len(sess.ID) >= models.IDLen && strings.HasPrefix(sess.ID, base) {
				if ss, err := strconv.Atoi(sess.ID[12:14]); err == nil {
					used[ss] = true
				}
			}
		}
	}

	sec := preferredSec % 60
	if used[sec] {
		sec = -1
		for c := 0; c < 60; c++ {
			if !used[c] {
				sec = c
				break
			}
		}
		if sec == -1 {
			sec = preferredSec % 60 // minute is impossibly full; accept a dupe
		}
	}
	return fmt.Sprintf("%s%02d", base, sec)
}

// FindByID returns every raw (un-hydrated) session with the given ID.
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

// SessionsOn returns every hydrated session on the given YYYY-MM-DD date.
func (s *Service) SessionsOn(date string) []models.Session {
	de := s.FindByDate(date)
	if de == nil {
		return nil
	}
	originals := s.seriesOriginals()
	out := make([]models.Session, len(de.Sessions))
	for i, sess := range de.Sessions {
		out[i] = hydrateWith(sess, originals)
	}
	return out
}

// FindByPeriod returns hydrated DateEntries whose dates fall within [start, end].
func (s *Service) FindByPeriod(start, end time.Time) []models.DateEntry {
	startStr := start.Format("2006-01-02")
	endStr := end.Format("2006-01-02")

	var result []models.DateEntry
	for _, de := range s.data.Entries {
		if de.Date >= startStr && de.Date <= endStr {
			result = append(result, de)
		}
	}
	sortEntries(result)
	return s.hydrate(result)
}

// seriesOriginals maps a series ID (with the repeat suffix) to its template
// session, so hydration is a map lookup instead of a full scan per occurrence.
func (s *Service) seriesOriginals() map[string]models.Session {
	m := make(map[string]models.Session)
	for _, de := range s.data.Entries {
		for _, sess := range de.Sessions {
			if models.HasRepeatSuffix(sess.ID) {
				m[sess.ID] = sess
			}
		}
	}
	return m
}

func (s *Service) hydrate(entries []models.DateEntry) []models.DateEntry {
	originals := s.seriesOriginals()
	out := make([]models.DateEntry, len(entries))
	for i, de := range entries {
		hd := models.DateEntry{Date: de.Date, Sessions: make([]models.Session, len(de.Sessions))}
		for j, session := range de.Sessions {
			hd.Sessions[j] = hydrateWith(session, originals)
		}
		out[i] = hd
	}
	return out
}

// HydrateSession returns a hydrated copy of a single session.
func (s *Service) HydrateSession(session models.Session) models.Session {
	return hydrateWith(session, s.seriesOriginals())
}

// hydrateWith fills a child occurrence's blank fields from its series template
// and marks repeat sessions. Fields the occurrence overrides (non-zero) are
// kept, which is what makes per-occurrence exceptions work.
func hydrateWith(session models.Session, originals map[string]models.Session) models.Session {
	if ref := session.SeriesRef(); ref != "" {
		if orig, ok := originals[ref]; ok {
			if session.Type == "" {
				session.Type = orig.Type
			}
			if session.Duration == 0 {
				session.Duration = orig.Duration
			}
			if session.Notes == "" {
				session.Notes = orig.Notes
			}
			if session.Status == "" {
				session.Status = orig.Status
			}
			if session.Time == "" {
				session.Time = orig.Time
			}
			session.Name = orig.Name
			session.IsRepeat = true
			session.OriginalID = orig.ID
		}
	}
	if session.IsSeriesOriginal() {
		session.IsRepeat = true
		session.OriginalID = session.ID
	}
	return session
}

// EditEntry updates the first session identified by id.
func (s *Service) EditEntry(id string, updated models.Session) error {
	for i := range s.data.Entries {
		for j := range s.data.Entries[i].Sessions {
			if s.data.Entries[i].Sessions[j].ID != id {
				continue
			}
			session := &s.data.Entries[i].Sessions[j]
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
			if updated.Time != "" {
				de := &s.data.Entries[i]
				sort.SliceStable(de.Sessions, func(a, b int) bool {
					return de.Sessions[a].Time < de.Sessions[b].Time
				})
			}
			return nil
		}
	}
	return fmt.Errorf("запись с ID %s не найдена", id)
}

// EditSeriesTime updates the template time and every child occurrence that has
// not overridden its own time. Returns the number of sessions changed.
func (s *Service) EditSeriesTime(seriesID, newTime string) int {
	if newTime == "" {
		return 0
	}
	var origTime string
	for _, de := range s.data.Entries {
		for _, sess := range de.Sessions {
			if sess.ID == seriesID {
				origTime = sess.Time
			}
		}
	}
	changed := 0
	for i := range s.data.Entries {
		for j := range s.data.Entries[i].Sessions {
			sess := &s.data.Entries[i].Sessions[j]
			isOriginal := sess.ID == seriesID
			isChildFollowing := sess.Name == seriesID && (sess.Time == "" || sess.Time == origTime)
			if isOriginal || isChildFollowing {
				sess.Time = newTime
				changed++
			}
		}
		sort.SliceStable(s.data.Entries[i].Sessions, func(a, b int) bool {
			return s.data.Entries[i].Sessions[a].Time < s.data.Entries[i].Sessions[b].Time
		})
	}
	return changed
}

// DeleteEntry removes ALL sessions with the given ID. Returns the count.
func (s *Service) DeleteEntry(id string) int {
	return s.deleteMatching(func(sess models.Session) bool { return sess.ID == id })
}

// DeleteRepeats removes every child occurrence referencing originalID.
func (s *Service) DeleteRepeats(originalID string) int {
	return s.deleteMatching(func(sess models.Session) bool { return sess.Name == originalID })
}

func (s *Service) deleteMatching(match func(models.Session) bool) int {
	count := 0
	for i := len(s.data.Entries) - 1; i >= 0; i-- {
		de := &s.data.Entries[i]
		kept := de.Sessions[:0]
		for _, sess := range de.Sessions {
			if match(sess) {
				count++
			} else {
				kept = append(kept, sess)
			}
		}
		de.Sessions = kept
		if len(de.Sessions) == 0 {
			s.data.Entries = append(s.data.Entries[:i], s.data.Entries[i+1:]...)
		}
	}
	return count
}

// DeleteAll removes all entries.
func (s *Service) DeleteAll() { s.data.Entries = nil }

// DeleteByPeriod removes all DateEntries in [start, end] inclusive and returns
// the number of sessions deleted.
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

// OrphanRepeats returns child occurrences whose series template is missing.
func (s *Service) OrphanRepeats() []models.Session {
	originals := s.seriesOriginals()
	var orphans []models.Session
	for _, de := range s.data.Entries {
		for _, sess := range de.Sessions {
			if ref := sess.SeriesRef(); ref != "" {
				if _, ok := originals[ref]; !ok {
					orphans = append(orphans, sess)
				}
			}
		}
	}
	return orphans
}

// GetWeekEntries returns entries for the ISO week containing refDate.
func (s *Service) GetWeekEntries(refDate time.Time) []models.DateEntry {
	monday, sunday := WeekBounds(refDate)
	return s.FindByPeriod(monday, sunday)
}

// GetMonthEntries returns entries for the month containing refDate.
func (s *Service) GetMonthEntries(refDate time.Time) []models.DateEntry {
	first, last := MonthBounds(refDate)
	return s.FindByPeriod(first, last)
}

// GetTodayEntries returns entries for refDate's calendar day.
func (s *Service) GetTodayEntries(refDate time.Time) []models.DateEntry {
	start := DayStart(refDate)
	return s.FindByPeriod(start, start)
}

// GetAllEntries returns all entries, hydrated and sorted by date.
func (s *Service) GetAllEntries() []models.DateEntry {
	result := make([]models.DateEntry, len(s.data.Entries))
	copy(result, s.data.Entries)
	sortEntries(result)
	return s.hydrate(result)
}

// TotalHours calculates the total number of hours across all sessions.
func TotalHours(entries []models.DateEntry) float64 {
	total := 0
	for _, de := range entries {
		for _, sess := range de.Sessions {
			total += sess.Duration
		}
	}
	return float64(total) / 60.0
}

// AllTypes returns DefaultTypes followed by any other types present in the data.
func (s *Service) AllTypes() []string {
	return mergeDefaults(DefaultTypes, s.collect(func(sess models.Session) string { return sess.Type }))
}

// AllStatuses returns DefaultStatuses followed by any others present in the data.
func (s *Service) AllStatuses() []string {
	return mergeDefaults(DefaultStatuses, s.collect(func(sess models.Session) string { return sess.Status }))
}

func (s *Service) collect(field func(models.Session) string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, de := range s.data.Entries {
		for _, sess := range de.Sessions {
			if v := field(sess); v != "" && !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
		}
	}
	return out
}

func mergeDefaults(defaults, found []string) []string {
	inDefaults := make(map[string]bool, len(defaults))
	for _, d := range defaults {
		inDefaults[d] = true
	}
	extra := make([]string, 0, len(found))
	for _, v := range found {
		if !inDefaults[v] {
			extra = append(extra, v)
		}
	}
	sort.Strings(extra)
	return append(append([]string{}, defaults...), extra...)
}

// SearchByName returns sessions whose name contains sub (case-insensitive).
func (s *Service) SearchByName(sub string) []models.Session {
	return s.search(sub, true, false)
}

// SearchByType returns sessions whose type contains sub (case-insensitive).
func (s *Service) SearchByType(sub string) []models.Session {
	return s.search(sub, false, true)
}

// SearchByNameOrType returns sessions matching name or type (case-insensitive).
func (s *Service) SearchByNameOrType(query string) []models.Session {
	return s.search(query, true, true)
}

func (s *Service) search(query string, byName, byType bool) []models.Session {
	lower := strings.ToLower(query)
	originals := s.seriesOriginals()
	var result []models.Session
	for _, de := range s.data.Entries {
		for _, sess := range de.Sessions {
			h := hydrateWith(sess, originals)
			if (byName && strings.Contains(strings.ToLower(h.Name), lower)) ||
				(byType && strings.Contains(strings.ToLower(h.Type), lower)) {
				result = append(result, h)
			}
		}
	}
	return result
}

func sortEntries(entries []models.DateEntry) {
	sort.Slice(entries, func(i, j int) bool { return entries[i].Date < entries[j].Date })
}
