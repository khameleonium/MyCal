// Package calendar is the domain layer: an in-memory list of sessions with
// query, mutation and integrity operations, backed by the storage package.
package calendar

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"mycalendar/models"
	"mycalendar/parser"
	"mycalendar/storage"
)

// DefaultTypes and DefaultStatuses seed the pick-lists shown when adding an
// entry. Exported so a caller can override them.
var (
	DefaultTypes    = []string{"Стрельба из лука", "Метание ножей"}
	DefaultStatuses = []string{"Активно", "Завершено", "В процессе", "Запланировано", "Пропущено", "Отменено"}
)

const maxUndoStack = 15

type undoEntry struct {
	sessions []models.Session
	desc     string
}

// Service holds the calendar in memory and persists it on demand.
type Service struct {
	mu        sync.RWMutex
	settings  *models.CalendarSettings
	sessions  []models.Session
	dir       string
	baseName  string
	mode      string
	warnings  []string
	undoStack []undoEntry
	txDepth   int
}

func (s *Service) pushUndoUnlocked(desc string) {
	if s.txDepth > 0 {
		return
	}
	snap := make([]models.Session, len(s.sessions))
	copy(snap, s.sessions)
	s.undoStack = append(s.undoStack, undoEntry{
		sessions: snap,
		desc:     desc,
	})
	if len(s.undoStack) > maxUndoStack {
		s.undoStack = s.undoStack[len(s.undoStack)-maxUndoStack:]
	}
}

// BeginUndoTx starts a composite mutation, recording a single undo snapshot
// with the given description so nested calls to Add/Update/Delete do not push
// duplicate intermediate snapshots. Always pair with defer s.EndUndoTx().
func (s *Service) BeginUndoTx(desc string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.txDepth == 0 {
		s.pushUndoUnlocked(desc)
	}
	s.txDepth++
}

// EndUndoTx finishes a composite mutation started by BeginUndoTx.
func (s *Service) EndUndoTx() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.txDepth > 0 {
		s.txDepth--
	}
}

// CanUndo reports whether an undo snapshot is available and returns its description.
func (s *Service) CanUndo() (bool, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.undoStack) == 0 {
		return false, ""
	}
	return true, s.undoStack[len(s.undoStack)-1].desc
}

// Undo restores the calendar to the state before the last mutating operation,
// persists it to disk, and returns a description of the undone operation.
func (s *Service) Undo(ctx context.Context) (string, error) {
	s.mu.Lock()
	if len(s.undoStack) == 0 {
		s.mu.Unlock()
		return "", fmt.Errorf("нет действий для отмены")
	}
	last := s.undoStack[len(s.undoStack)-1]
	s.undoStack = s.undoStack[:len(s.undoStack)-1]
	restored := make([]models.Session, len(last.sessions))
	copy(restored, last.sessions)
	s.sessions = restored
	storage.SortSessions(s.sessions)
	s.mu.Unlock()

	if err := s.Save(ctx); err != nil {
		return last.desc, err
	}
	return last.desc, nil
}

// NewService loads the calendar. A non-nil error means the data could not be
// read at all; recoverable problems are reported via Warnings.
func NewService(ctx context.Context, dir, baseName, mode string) (*Service, error) {
	cal, warnings, err := storage.Load(ctx, dir, baseName, mode)
	if err != nil {
		return nil, err
	}
	return &Service{
		settings: cal.Settings,
		sessions: cal.Sessions,
		dir:      dir,
		baseName: baseName,
		mode:     mode,
		warnings: warnings,
	}, nil
}

// Reload reloads the calendar data from disk.
func (s *Service) Reload(ctx context.Context) error {
	cal, warnings, err := storage.Load(ctx, s.dir, s.baseName, s.mode)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings = cal.Settings
	s.sessions = cal.Sessions
	s.warnings = warnings
	return nil
}

// DataFilesModTime returns the latest modification time of the calendar data files on disk.
func (s *Service) DataFilesModTime() time.Time {
	s.mu.RLock()
	dir, baseName, mode := s.dir, s.baseName, s.mode
	s.mu.RUnlock()

	var latest time.Time
	path := filepath.Join(dir, baseName+".json")
	if fi, err := os.Stat(path); err == nil {
		latest = fi.ModTime()
	}
	if mode != models.SplitNone {
		if matches, err := filepath.Glob(filepath.Join(dir, "*_"+baseName+".json")); err == nil {
			for _, m := range matches {
				if fi, err := os.Stat(m); err == nil && fi.ModTime().After(latest) {
					latest = fi.ModTime()
				}
			}
		}
		if fi, err := os.Stat(dir); err == nil && fi.ModTime().After(latest) {
			latest = fi.ModTime()
		}
	}
	return latest
}

// Warnings returns (and clears) any recoverable problems noticed while loading.
func (s *Service) Warnings() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	w := s.warnings
	s.warnings = nil
	return w
}

// Save persists the calendar to disk.
func (s *Service) Save(ctx context.Context) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return storage.Save(ctx, s.dir, s.baseName, &models.Calendar{
		Settings: s.settings,
		Sessions: s.sessions,
	}, s.mode)
}

// Settings returns the calendar-specific settings, or nil if none are set.
func (s *Service) Settings() *models.CalendarSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings
}

// SetSettings updates the calendar-specific settings in memory. Call Save() to persist.
func (s *Service) SetSettings(settings *models.CalendarSettings) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings = settings
}

// EffectiveConfig computes the merged configuration for this calendar against the provided global config.
func (s *Service) EffectiveConfig(global *models.Config) *models.Config {
	s.mu.RLock()
	st := s.settings
	s.mu.RUnlock()
	cal := &models.Calendar{Settings: st}
	return cal.EffectiveConfig(global)
}

func (s *Service) DataDir() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dir
}

func (s *Service) MainDataFilePath() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return filepath.Join(s.dir, s.baseName+".json")
}

func (s *Service) BaseName() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.baseName
}

func (s *Service) Mode() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mode
}

func (s *Service) SetMode(mode string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mode = mode
}

func (s *Service) SetBaseName(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.baseName = name
}

func (s *Service) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sessions)
}

// ListCalendars discovers all calendars on disk in s.dir and merges them with cfg.
func (s *Service) ListCalendars(cfg *models.Config) []models.CalendarInfo {
	s.mu.RLock()
	dir := s.dir
	curBase := s.baseName
	s.mu.RUnlock()

	base := "mycal"
	if cfg != nil && strings.TrimSpace(cfg.DataFileName) != "" {
		base = strings.TrimSpace(cfg.DataFileName)
	} else if curBase != "" {
		base = curBase
		if idx := strings.Index(base, "--"); idx >= 0 {
			base = base[:idx]
		}
	}
	return storage.DiscoverCalendars(dir, base, cfg)
}

// SwitchCalendar changes the active calendar to calKey ("" for default),
// updates cfg, and reloads the calendar's sessions from disk.
func (s *Service) SwitchCalendar(ctx context.Context, cfg *models.Config, calKey string) error {
	calKey = models.SanitizeCalendarKey(calKey)
	if cfg != nil {
		// Discover first so any on-disk split mode for calKey is recorded in cfg.
		_ = s.ListCalendars(cfg)
		cfg.ActiveCalendar = calKey
		mode := cfg.GetCalendarSplitMode(calKey)
		cfg.SetCalendarSplitMode(calKey, mode)
		s.mu.Lock()
		s.baseName = cfg.EffectiveBaseName(calKey)
		s.mode = mode
		s.undoStack = nil
		s.mu.Unlock()
	} else {
		s.mu.Lock()
		if calKey == "" {
			if idx := strings.Index(s.baseName, "--"); idx >= 0 {
				s.baseName = s.baseName[:idx]
			}
		} else {
			base := s.baseName
			if idx := strings.Index(base, "--"); idx >= 0 {
				base = base[:idx]
			}
			s.baseName = base + "--" + calKey
		}
		s.undoStack = nil
		s.mu.Unlock()
	}
	return s.Reload(ctx)
}

// CreateCalendar creates a new calendar with the given suffix/prefix key and
// splitMode ("none", "year", "month"), switches to it, and persists its initial
// JSON file on disk immediately.
func (s *Service) CreateCalendar(ctx context.Context, cfg *models.Config, calKey, splitMode string) error {
	calKey = models.SanitizeCalendarKey(calKey)
	if calKey == "" {
		return fmt.Errorf("укажите непустое имя (префикс) календаря")
	}
	switch splitMode {
	case models.SplitNone, models.SplitYear, models.SplitMonth:
	default:
		splitMode = models.SplitNone
	}
	if cfg != nil {
		cfg.SetCalendarSplitMode(calKey, splitMode)
		cfg.ActiveCalendar = calKey
		s.mu.Lock()
		s.baseName = cfg.EffectiveBaseName(calKey)
		s.mode = splitMode
		s.undoStack = nil
		s.mu.Unlock()
	} else {
		s.mu.Lock()
		base := s.baseName
		if idx := strings.Index(base, "--"); idx >= 0 {
			base = base[:idx]
		}
		s.baseName = base + "--" + calKey
		s.mode = splitMode
		s.undoStack = nil
		s.mu.Unlock()
	}
	if err := s.Reload(ctx); err != nil {
		return err
	}
	return s.Save(ctx)
}

// RemoveDataFiles deletes on-disk files for a (now unused) base name in the
// data directory, in every split-mode layout.
func (s *Service) RemoveDataFiles(baseName string) {
	s.mu.RLock()
	dir := s.dir
	s.mu.RUnlock()
	storage.RemoveDataFiles(dir, baseName)
}

// ---------------------------------------------------------------- queries ----

// All returns a sorted copy of every session.
func (s *Service) All() []models.Session {
	s.mu.RLock()
	out := append([]models.Session(nil), s.sessions...)
	s.mu.RUnlock()
	return out
}

// CountByName reports how many sessions have the given name (case-insensitive).
func (s *Service) CountByName(name string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, x := range s.sessions {
		if strings.EqualFold(x.Name, name) {
			n++
		}
	}
	return n
}

// InRange returns sessions whose date falls within [start, end] (inclusive).
func (s *Service) InRange(start, end time.Time) []models.Session {
	lo, hi := start.Format("20060102"), end.Format("20060102")
	s.mu.RLock()
	var out []models.Session
	for _, x := range s.sessions {
		if d := x.DateKey(); d >= lo && d <= hi {
			out = append(out, x)
		}
	}
	s.mu.RUnlock()
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
	s.mu.RLock()
	var out []models.Session
	for _, x := range s.sessions {
		if x.Date() == iso {
			out = append(out, x)
		}
	}
	s.mu.RUnlock()
	storage.SortSessions(out)
	return out
}

// ByID returns the session with the given ID.
func (s *Service) ByID(id string) (models.Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
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
	s.mu.RLock()
	var out []models.Session
	for _, x := range s.sessions {
		if x.SeriesID == seriesID {
			out = append(out, x)
		}
	}
	s.mu.RUnlock()
	storage.SortSessions(out)
	return out
}

// SeriesCounts returns the total count of sessions in the series and the count
// of sessions from fromID onwards (inclusive, ordered by DateKey and ID).
func (s *Service) SeriesCounts(seriesID, fromID string) (total, following int) {
	if seriesID == "" {
		return 0, 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var targetDateKey string
	if fromID != "" {
		for _, x := range s.sessions {
			if x.ID == fromID && x.SeriesID == seriesID {
				targetDateKey = x.DateKey()
				break
			}
		}
	}
	for _, x := range s.sessions {
		if x.SeriesID == seriesID {
			total++
			if fromID == "" {
				following++
			} else if targetDateKey != "" {
				dk := x.DateKey()
				if dk > targetDateKey || (dk == targetDateKey && x.ID >= fromID) {
					following++
				}
			}
		}
	}
	return total, following
}

// Conflicts returns sessions at the exact same ISO date and HH:MM time.
func (s *Service) Conflicts(iso, hhmm string) []models.Session {
	s.mu.RLock()
	var out []models.Session
	for _, x := range s.sessions {
		if x.Date() == iso && x.Time == hhmm {
			out = append(out, x)
		}
	}
	s.mu.RUnlock()
	storage.SortSessions(out)
	return out
}

// Matches reports whether a session's name or type contains query, ignoring
// case. It is the single definition of "matches a search/filter query", shared
// by Search and by the interactive list filter.
func Matches(x models.Session, query string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return false
	}
	return strings.Contains(strings.ToLower(x.Name), q) ||
		strings.Contains(strings.ToLower(x.Type), q)
}

// Search returns sessions whose name or type contains query (case-insensitive).
func (s *Service) Search(query string) []models.Session {
	s.mu.RLock()
	var out []models.Session
	for _, x := range s.sessions {
		if Matches(x, query) {
			out = append(out, x)
		}
	}
	s.mu.RUnlock()
	storage.SortSessions(out)
	return out
}

// Types returns DefaultTypes plus extra ones (seed + used) found in the data.
func (s *Service) Types(seed ...string) []string {
	return merge(DefaultTypes, seed, s.distinct(func(x models.Session) string { return x.Type }))
}

// AvailableTypes returns all unique types (defaults + custom + sessions) excluding excludedTypes.
func (s *Service) AvailableTypes(customTypes, excludedTypes []string) []string {
	all := s.Types(customTypes...)
	excluded := make(map[string]bool)
	for _, ex := range excludedTypes {
		excluded[strings.ToLower(strings.TrimSpace(ex))] = true
	}
	var res []string
	for _, t := range all {
		trimmed := strings.TrimSpace(t)
		if trimmed != "" && !excluded[strings.ToLower(trimmed)] {
			res = append(res, trimmed)
		}
	}
	return res
}

// Statuses returns DefaultStatuses plus extra ones found in the data.
func (s *Service) Statuses(seed ...string) []string {
	return merge(DefaultStatuses, seed, s.distinct(func(x models.Session) string { return x.Status }))
}

// AvailableStatuses returns all unique statuses (defaults + custom + sessions) excluding excludedStatuses.
func (s *Service) AvailableStatuses(customStatuses, excludedStatuses []string) []string {
	all := s.Statuses(customStatuses...)
	excluded := make(map[string]bool)
	for _, ex := range excludedStatuses {
		excluded[strings.ToLower(strings.TrimSpace(ex))] = true
	}
	var res []string
	for _, st := range all {
		trimmed := strings.TrimSpace(st)
		if trimmed != "" && !excluded[strings.ToLower(trimmed)] {
			res = append(res, trimmed)
		}
	}
	return res
}

func (s *Service) distinct(field func(models.Session) string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
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

// -------------------------------------------------------------- mutations ----

// GenerateID returns a unique 14-char ID: YYYYMMDDHHMM + a two-digit counter
// that is the smallest value free for that minute. It widens past two digits
// only if a single minute somehow holds 100+ entries.
func (s *Service) GenerateID(date, tm time.Time) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	prefix := date.Format("20060102") + tm.Format("1504")
	var used map[string]bool
	for _, x := range s.sessions {
		if strings.HasPrefix(x.ID, prefix) {
			if used == nil {
				used = make(map[string]bool, 4)
			}
			used[x.ID] = true
		}
	}
	if used == nil {
		return prefix + "00"
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
	_, err := s.AddBatch([]models.Session{session})
	return err
}

// AddBatch inserts several sessions (e.g. the occurrences of a repeating
// series), sorting the list once at the end instead of after every session.
// It stops at the first invalid/duplicate session and returns how many were
// added before that.
func (s *Service) AddBatch(sessions []models.Session) (added int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(sessions) > 0 {
		s.pushUndoUnlocked("добавление записи")
	}
	var used map[string]bool
	if len(sessions) > 1 && len(s.sessions) > 0 {
		used = make(map[string]bool, len(s.sessions)+len(sessions))
		for _, cur := range s.sessions {
			used[cur.ID] = true
		}
	}
	for _, session := range sessions {
		if session.ID == "" {
			err = fmt.Errorf("внутренняя ошибка: ID записи не сгенерирован")
			break
		}
		exists := false
		if used != nil {
			exists = used[session.ID]
		} else {
			for _, cur := range s.sessions {
				if cur.ID == session.ID {
					exists = true
					break
				}
			}
		}
		if exists {
			err = fmt.Errorf("запись с ID %s уже существует", session.ID)
			break
		}
		if used != nil {
			used[session.ID] = true
		}
		s.sessions = append(s.sessions, session)
		added++
	}
	if added > 0 {
		storage.SortSessions(s.sessions)
	}
	return added, err
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
	Cash     *float64
}

// Empty reports whether the patch would change nothing.
func (p Patch) Empty() bool {
	return p.Time == nil && p.Name == nil && p.Type == nil &&
		p.Duration == nil && p.Notes == nil && p.Status == nil && p.Cash == nil
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
	if p.Cash != nil {
		x.Cash = *p.Cash
	}
}

// Update applies patch to the session with the given ID.
func (s *Service) Update(id string, patch Patch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.sessions {
		if s.sessions[i].ID == id {
			s.pushUndoUnlocked("изменение записи")
			patch.applyTo(&s.sessions[i])
			if patch.Time != nil {
				storage.SortSessions(s.sessions)
			}
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
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for i := range s.sessions {
		if s.sessions[i].SeriesID == seriesID {
			if n == 0 {
				s.pushUndoUnlocked("изменение серии записей")
			}
			patch.applyTo(&s.sessions[i])
			n++
		}
	}
	if n > 0 && patch.Time != nil {
		storage.SortSessions(s.sessions)
	}
	return n
}

// UpdateSeriesFrom applies patch to fromID and all subsequent sessions in the series.
// If fromID is not the start of the series, the following sessions are branched
// into their own series with SeriesID = fromID. Returns the count of updated sessions.
func (s *Service) UpdateSeriesFrom(seriesID, fromID string, patch Patch) int {
	if seriesID == "" || fromID == "" {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var targetDateKey string
	found := false
	for _, x := range s.sessions {
		if x.ID == fromID && x.SeriesID == seriesID {
			targetDateKey = x.DateKey()
			found = true
			break
		}
	}
	if !found {
		return 0
	}
	n := 0
	for i := range s.sessions {
		if s.sessions[i].SeriesID == seriesID {
			dk := s.sessions[i].DateKey()
			if dk > targetDateKey || (dk == targetDateKey && s.sessions[i].ID >= fromID) {
				if n == 0 {
					s.pushUndoUnlocked("изменение серии записей")
				}
				patch.applyTo(&s.sessions[i])
				if fromID != seriesID {
					s.sessions[i].SeriesID = fromID
				}
				n++
			}
		}
	}
	if n > 0 && patch.Time != nil {
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

// DeleteSeriesFrom removes fromID and all subsequent sessions in the same series. Returns the count.
func (s *Service) DeleteSeriesFrom(seriesID, fromID string) int {
	if seriesID == "" || fromID == "" {
		return 0
	}
	s.mu.RLock()
	var targetDateKey string
	found := false
	for _, x := range s.sessions {
		if x.ID == fromID && x.SeriesID == seriesID {
			targetDateKey = x.DateKey()
			found = true
			break
		}
	}
	s.mu.RUnlock()
	if !found {
		return 0
	}
	return s.deleteWhere(func(x models.Session) bool {
		if x.SeriesID != seriesID {
			return false
		}
		dk := x.DateKey()
		return dk > targetDateKey || (dk == targetDateKey && x.ID >= fromID)
	})
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
func (s *Service) DeleteAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.sessions) > 0 {
		s.pushUndoUnlocked("очистка всех записей")
	}
	s.sessions = nil
}

func (s *Service) deleteWhere(match func(models.Session) bool) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, x := range s.sessions {
		if match(x) {
			n++
		}
	}
	if n == 0 {
		return 0
	}
	s.pushUndoUnlocked("удаление записи")
	kept := s.sessions[:0]
	for _, x := range s.sessions {
		if match(x) {
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
	s.mu.RLock()
	defer s.mu.RUnlock()
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
			if seen[x.ID] >= 2 {
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
	s.mu.Lock()
	defer s.mu.Unlock()
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
			x.ID = freeID(x.ID, seen)
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

// freeID returns an unused ID built from id's date/time prefix. The prefix is
// only trimmed when it is longer than YYYYMMDDHHMM, so a short-but-usable ID
// (10 or 11 chars, which Validate lets through) is extended rather than sliced
// out of range.
func freeID(id string, seen map[string]bool) string {
	base := id
	if len(base) > 12 {
		base = base[:12]
	}
	for n := 0; ; n++ {
		cand := fmt.Sprintf("%s%02d", base, n)
		if n >= 100 {
			cand = fmt.Sprintf("%s%d", base, n)
		}
		if !seen[cand] {
			return cand
		}
	}
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// SortSessions sorts sessions in place by the given field and order direction.
// When sorting by "date", time within the same date is ALWAYS sorted ascending.
func SortSessions(sessions []models.Session, field string, asc bool) {
	sort.Slice(sessions, func(i, j int) bool {
		return CompareSessions(sessions[i], sessions[j], field, asc)
	})
}

// CompareSessions returns true if s1 should appear before s2.
func CompareSessions(s1, s2 models.Session, field string, asc bool) bool {
	d1 := s1.DateKey()
	d2 := s2.DateKey()

	switch field {
	case "date":
		// Requirement: time within same date is always ascending!
		if d1 == d2 {
			return s1.Time < s2.Time
		}
		if asc {
			return d1 < d2
		}
		return d1 > d2

	case "time":
		if s1.Time != s2.Time {
			if asc {
				return s1.Time < s2.Time
			}
			return s1.Time > s2.Time
		}
		if d1 != d2 {
			return d1 > d2
		}
		return strings.ToLower(s1.Name) < strings.ToLower(s2.Name)

	case "name":
		n1 := strings.ToLower(strings.TrimSpace(s1.Name))
		n2 := strings.ToLower(strings.TrimSpace(s2.Name))
		if n1 != n2 {
			if asc {
				return n1 < n2
			}
			return n1 > n2
		}
		if d1 != d2 {
			return d1 > d2
		}
		return s1.Time < s2.Time

	case "type":
		t1 := strings.ToLower(strings.TrimSpace(s1.Type))
		t2 := strings.ToLower(strings.TrimSpace(s2.Type))
		if t1 != t2 {
			if asc {
				return t1 < t2
			}
			return t1 > t2
		}
		if d1 != d2 {
			return d1 > d2
		}
		return s1.Time < s2.Time

	case "duration":
		if s1.Duration != s2.Duration {
			if asc {
				return s1.Duration < s2.Duration
			}
			return s1.Duration > s2.Duration
		}
		if d1 != d2 {
			return d1 > d2
		}
		return s1.Time < s2.Time

	case "cash":
		if s1.Cash != s2.Cash {
			if asc {
				return s1.Cash < s2.Cash
			}
			return s1.Cash > s2.Cash
		}
		if d1 != d2 {
			return d1 > d2
		}
		return s1.Time < s2.Time

	case "status":
		st1 := strings.ToLower(strings.TrimSpace(s1.Status))
		st2 := strings.ToLower(strings.TrimSpace(s2.Status))
		if st1 != st2 {
			if asc {
				return st1 < st2
			}
			return st1 > st2
		}
		if d1 != d2 {
			return d1 > d2
		}
		return s1.Time < s2.Time

	default: // fallback to date
		if d1 == d2 {
			return s1.Time < s2.Time
		}
		if asc {
			return d1 < d2
		}
		return d1 > d2
	}
}

// FormatDuration formats duration in minutes according to the configured unit ("min" or "hours").
func FormatDuration(duration int, unit string) string {
	if unit == "hours" {
		if duration%60 == 0 {
			return fmt.Sprintf("%d ч", duration/60)
		}
		hours := float64(duration) / 60.0
		return fmt.Sprintf("%.1f ч", hours)
	}
	return fmt.Sprintf("%d мин", duration)
}
