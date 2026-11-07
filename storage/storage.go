// Package storage loads and saves the calendar as JSON, atomically, and keeps
// the on-disk file layout in sync with the configured split mode.
package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mycalendar/models"
)

// Load reads every data file for the given split mode and merges them.
//
// The returned warnings are human-readable (Russian) notes about recoverable
// problems — a corrupt file restored from its backup, a corrupt file with no
// backup that was set aside. A non-nil error means loading genuinely failed
// (no permission, context cancelled) and the program should not continue.
func Load(ctx context.Context, dir, baseName, mode string) (cal *models.Calendar, warnings []string, err error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
	}

	var files []string
	switch mode {
	case models.SplitNone:
		files = []string{filepath.Join(dir, baseName+".json")}
	case models.SplitYear:
		files = globFiles(dir, "????_"+baseName+".json")
		for _, bak := range globFiles(dir, "????_"+baseName+".json.bak") {
			target := strings.TrimSuffix(bak, ".bak")
			if !contains(files, target) {
				files = append(files, target)
			}
		}
		sort.Strings(files)
		files = append(files, orphanPath(dir, baseName))
	case models.SplitMonth:
		files = globFiles(dir, "????-??_"+baseName+".json")
		for _, bak := range globFiles(dir, "????-??_"+baseName+".json.bak") {
			target := strings.TrimSuffix(bak, ".bak")
			if !contains(files, target) {
				files = append(files, target)
			}
		}
		sort.Strings(files)
		files = append(files, orphanPath(dir, baseName))
	default:
		return nil, nil, fmt.Errorf("неизвестный режим хранения: %q", mode)
	}

	merged := &models.Calendar{}
	for _, path := range files {
		part, w, err := loadFile(path)
		if err != nil {
			return nil, warnings, err
		}
		warnings = append(warnings, w...)
		merged.Sessions = append(merged.Sessions, part.Sessions...)
	}
	SortSessions(merged.Sessions)
	return merged, warnings, nil
}

// loadFile reads one file. A missing file is not an error (empty calendar),
// unless a valid backup exists, in which case it is restored.
func loadFile(path string) (*models.Calendar, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			base := filepath.Base(path)
			if bak, bakErr := os.ReadFile(path + ".bak"); bakErr == nil {
				if cal, ok := decode(bak); ok {
					if writeErr := os.WriteFile(path, bak, 0o644); writeErr != nil {
						return cal, []string{fmt.Sprintf(
							"файл %s отсутствовал; восстановлен из резервной копии в памяти, но записать его обратно не удалось (%s)",
							base, reason(writeErr))}, nil
					}
					return cal, []string{fmt.Sprintf("файл %s отсутствовал и был восстановлен из резервной копии %s.bak", base, base)}, nil
				}
			}
			return &models.Calendar{}, nil, nil
		}
		return nil, nil, fmt.Errorf("не удалось прочитать %s: %s", path, reason(err))
	}

	if cal, ok := decode(data); ok {
		return cal, nil, nil
	}

	base := filepath.Base(path)
	isEmpty := len(strings.TrimSpace(string(data))) == 0

	// The main file is corrupt or empty. Try its backup.
	if bak, err := os.ReadFile(path + ".bak"); err == nil {
		if cal, ok := decode(bak); ok {
			var corruptNote string
			if !isEmpty {
				aside := corruptPath(path)
				if renameErr := os.Rename(path, aside); renameErr == nil {
					corruptNote = fmt.Sprintf(" (повреждённый файл сохранён как %s)", filepath.Base(aside))
				}
			}
			if writeErr := os.WriteFile(path, bak, 0o644); writeErr != nil {
				return cal, []string{fmt.Sprintf(
					"файл %s повреждён; восстановлен из резервной копии в памяти, но записать его обратно не удалось (%s)%s",
					base, reason(writeErr), corruptNote)}, nil
			}
			if isEmpty {
				return cal, []string{fmt.Sprintf("файл %s был пуст и восстановлен из резервной копии %s.bak", base, base)}, nil
			}
			return cal, []string{fmt.Sprintf("файл %s был повреждён и восстановлен из резервной копии %s.bak%s", base, base, corruptNote)}, nil
		}
	}

	// No usable backup.
	if isEmpty {
		return &models.Calendar{}, []string{fmt.Sprintf("файл %s пуст, календарь открыт пустым.", base)}, nil
	}

	aside := corruptPath(path)
	msg := fmt.Sprintf("файл %s повреждён (нарушена структура) и не может быть прочитан.", base)
	if renameErr := os.Rename(path, aside); renameErr == nil {
		msg += " Он сохранён как " + filepath.Base(aside) + ", календарь открыт пустым."
	} else {
		msg += " Переименовать его не удалось (" + reason(renameErr) + "); при следующем сохранении он будет перезаписан."
	}
	return &models.Calendar{}, []string{msg}, nil
}

func decode(data []byte) (*models.Calendar, bool) {
	data = trimBOM(data)
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, false
	}
	var cal models.Calendar
	if err := json.Unmarshal(data, &cal); err != nil {
		return nil, false
	}
	return &cal, true
}

// Save writes the calendar for the given split mode and deletes data files that
// belong to the other modes, so switching modes never leaves stale copies.
func Save(ctx context.Context, dir, baseName string, cal *models.Calendar, mode string) error {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	SortSessions(cal.Sessions)

	switch mode {
	case models.SplitNone:
		if err := writeJSON(filepath.Join(dir, baseName+".json"), cal); err != nil {
			return err
		}
		removeFiles(globFiles(dir, "????_"+baseName+".json"))
		removeFiles(globFiles(dir, "????-??_"+baseName+".json"))
		removeFiles([]string{orphanPath(dir, baseName)})
		return nil
	case models.SplitYear:
		return saveSplit(dir, baseName, cal, 4)
	case models.SplitMonth:
		return saveSplit(dir, baseName, cal, 7)
	default:
		return fmt.Errorf("неизвестный режим хранения: %q", mode)
	}
}

func saveSplit(dir, baseName string, cal *models.Calendar, keyLen int) error {
	groups := make(map[string]*models.Calendar)
	// Sessions whose ID carries no usable date cannot be filed under a year or
	// month. They are kept in a dedicated bucket file (which Load reads back)
	// rather than dropped, so a split-mode save never loses data.
	var orphans models.Calendar
	for _, s := range cal.Sessions {
		d := s.Date()
		if len(d) < keyLen {
			orphans.Sessions = append(orphans.Sessions, s)
			continue
		}
		key := d[:keyLen]
		if groups[key] == nil {
			groups[key] = &models.Calendar{}
		}
		groups[key].Sessions = append(groups[key].Sessions, s)
	}

	// Ensure that an empty calendar in split mode still writes a placeholder
	// file for the current year or month so the calendar remains discoverable on disk.
	if len(groups) == 0 && len(orphans.Sessions) == 0 {
		nowKey := time.Now().Format("2006")
		if keyLen == 7 {
			nowKey = time.Now().Format("2006-01")
		}
		groups[nowKey] = &models.Calendar{Sessions: []models.Session{}}
	}

	for key, part := range groups {
		if err := writeJSON(filepath.Join(dir, key+"_"+baseName+".json"), part); err != nil {
			return err
		}
	}
	if len(orphans.Sessions) > 0 {
		if err := writeJSON(orphanPath(dir, baseName), &orphans); err != nil {
			return err
		}
	}

	// Delete the single-file copy, the other split mode's files, and any files
	// of this mode whose group no longer has sessions.
	var stale []string
	stale = append(stale, filepath.Join(dir, baseName+".json"))
	if len(orphans.Sessions) == 0 {
		stale = append(stale, orphanPath(dir, baseName))
	}
	if keyLen == 4 {
		stale = append(stale, globFiles(dir, "????-??_"+baseName+".json")...)
		for _, f := range globFiles(dir, "????_"+baseName+".json") {
			if groups[groupKey(f, baseName, 4)] == nil {
				stale = append(stale, f)
			}
		}
	} else {
		stale = append(stale, globFiles(dir, "????_"+baseName+".json")...)
		for _, f := range globFiles(dir, "????-??_"+baseName+".json") {
			if groups[groupKey(f, baseName, 7)] == nil {
				stale = append(stale, f)
			}
		}
	}
	removeFiles(stale)
	return nil
}

// writeJSON serialises cal and writes it atomically: back up the current file,
// write a temp file, fsync it, then rename over the target (os.Rename replaces
// the destination on every supported OS, so the target is never missing).
func writeJSON(path string, cal *models.Calendar) error {
	toWrite := cal
	if toWrite != nil && toWrite.Sessions == nil {
		cp := *toWrite
		cp.Sessions = []models.Session{}
		toWrite = &cp
	}
	data, err := json.MarshalIndent(toWrite, "", "  ")
	if err != nil {
		return fmt.Errorf("сериализация календаря: %w", err)
	}
	data = append(data, '\n')

	if cur, err := os.ReadFile(path); err == nil {
		if err := os.WriteFile(path+".bak", cur, 0o644); err != nil {
			warn("не удалось создать резервную копию %s: %s", filepath.Base(path)+".bak", reason(err))
		}
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("не удалось создать временный файл в %s: %s", filepath.Dir(path), reason(err))
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("не удалось записать %s: %s", filepath.Base(path), reason(err))
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("не удалось сбросить %s на диск: %s", filepath.Base(path), reason(err))
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("не удалось закрыть временный файл: %s", reason(err))
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("не удалось заменить %s: %s", filepath.Base(path), reason(err))
	}
	syncDir(filepath.Dir(path))
	return nil
}

// reason turns a filesystem error into a short Russian explanation.
func reason(err error) string {
	switch {
	case errors.Is(err, fs.ErrPermission):
		return "нет прав доступа"
	case errors.Is(err, fs.ErrNotExist):
		return "файл или каталог не существует"
	case IsNoSpace(err):
		return "недостаточно места на диске"
	case errors.Is(err, fs.ErrExist):
		return "файл уже существует"
	default:
		return err.Error()
	}
}

func trimBOM(b []byte) []byte {
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		return b[3:]
	}
	return b
}

func corruptPath(path string) string {
	ts := time.Now().Format("20060102-150405")
	cand := path + ".corrupt-" + ts
	if _, err := os.Stat(cand); errors.Is(err, fs.ErrNotExist) {
		return cand
	}
	for n := 1; ; n++ {
		candWithN := fmt.Sprintf("%s.%d", cand, n)
		if _, err := os.Stat(candWithN); errors.Is(err, fs.ErrNotExist) {
			return candWithN
		}
	}
}

// orphanPath is the bucket file holding sessions whose ID has no usable date.
// Its name deliberately does not match the ????_ / ????-??_ globs, so it is
// never mistaken for a year or month group.
func orphanPath(dir, baseName string) string {
	return filepath.Join(dir, "unknown_"+baseName+".json")
}

func groupKey(path, baseName string, keyLen int) string {
	prefix := strings.TrimSuffix(filepath.Base(path), "_"+baseName+".json")
	if len(prefix) >= keyLen {
		return prefix[:keyLen]
	}
	return prefix
}

func globFiles(dir, pattern string) []string {
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		return nil
	}
	sort.Strings(matches)
	return matches
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func removeFiles(paths []string) {
	for _, p := range paths {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			warn("не удалось удалить неиспользуемый файл %s: %s", filepath.Base(p), reason(err))
		}
		_ = os.Remove(p + ".bak")
	}
}

// RemoveDataFiles deletes every data file (and its .bak) for baseName in dir,
// across all split-mode layouts. Used when the data file name changes.
func RemoveDataFiles(dir, baseName string) {
	removeFiles([]string{filepath.Join(dir, baseName+".json"), orphanPath(dir, baseName)})
	removeFiles(globFiles(dir, "????_"+baseName+".json"))
	removeFiles(globFiles(dir, "????-??_"+baseName+".json"))
}

// RenameFamilyDataFiles migrates all calendar files in dir that belong to
// oldBase (both the default calendar and any named `<oldBase>--<key>` calendars,
// across single, yearly, monthly, and orphan layouts, plus `.bak` files) to newBase.
// If a target file already exists (for example, the active calendar was just saved
// under newBase), the stale oldBase file is removed.
func RenameFamilyDataFiles(dir, oldBase, newBase string) {
	oldBase = strings.TrimSpace(oldBase)
	newBase = strings.TrimSpace(newBase)
	if oldBase == "" || newBase == "" || oldBase == newBase {
		return
	}
	if dir == "" {
		dir = "."
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		isBak := strings.HasSuffix(name, ".json.bak")
		isJSON := strings.HasSuffix(name, ".json")
		if !isJSON && !isBak {
			continue
		}
		if name == "config_mycal.json" || name == "config.json" ||
			strings.Contains(name, ".tmp") || strings.Contains(name, ".corrupt") {
			continue
		}
		ext := ".json"
		if isBak {
			ext = ".json.bak"
		}
		stem := strings.TrimSuffix(name, ext)
		var suffix string
		left := stem
		if idx := strings.Index(stem, "--"); idx >= 0 {
			left = stem[:idx]
			suffix = stem[idx:] // "--<key>"
		}
		var newLeft string
		switch {
		case left == oldBase:
			newLeft = newBase
		case left == "unknown_"+oldBase:
			newLeft = "unknown_" + newBase
		case len(left) == len(oldBase)+5 && left[4] == '_' && left[5:] == oldBase && isAllDigits(left[:4]):
			newLeft = left[:5] + newBase
		case len(left) == len(oldBase)+8 && left[4] == '-' && left[7] == '_' && left[8:] == oldBase &&
			isAllDigits(left[:4]) && isAllDigits(left[5:7]):
			newLeft = left[:8] + newBase
		default:
			continue
		}
		oldPath := filepath.Join(dir, name)
		newPath := filepath.Join(dir, newLeft+suffix+ext)
		if _, statErr := os.Stat(newPath); statErr == nil {
			// Target already exists (e.g. active calendar just saved); remove old file.
			_ = os.Remove(oldPath)
		} else {
			if err := os.Rename(oldPath, newPath); err != nil {
				warn("не удалось переименовать %s в %s: %s", name, filepath.Base(newPath), reason(err))
			}
		}
	}
}

// DiscoverCalendars scans dir for calendar JSON files matching baseName and
// `<baseName>--<key>.json` (including yearly `YYYY_<baseName>--<key>.json` and
// monthly `YYYY-MM_<baseName>--<key>.json`), merges them with calendars known
// in cfg, and returns a sorted list with the default calendar ("") first.
func DiscoverCalendars(dir, baseName string, cfg *models.Config) []models.CalendarInfo {
	baseName = strings.TrimSpace(baseName)
	if baseName == "" && cfg != nil {
		baseName = strings.TrimSpace(cfg.DataFileName)
	}
	if baseName == "" {
		baseName = "mycal"
	}

	activeKey := ""
	if cfg != nil {
		activeKey = strings.TrimSpace(cfg.ActiveCalendar)
	}

	inferredModes := make(map[string]string)
	entries, err := os.ReadDir(dir)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if !strings.HasSuffix(name, ".json") {
				continue
			}
			if name == "config_mycal.json" || name == "config.json" ||
				strings.Contains(name, ".tmp") || strings.Contains(name, ".corrupt") {
				continue
			}
			stem := strings.TrimSuffix(name, ".json")
			idx := strings.Index(stem, "--")
			if idx < 0 {
				// Default calendar file?
				if mode, ok := matchBaseStemMode(stem, baseName); ok {
					if inferredModes[""] == "" || mode != models.SplitNone {
						inferredModes[""] = mode
					}
				}
				continue
			}
			left := stem[:idx]
			calKey := strings.TrimSpace(stem[idx+2:])
			if calKey == "" {
				continue
			}
			if mode, ok := matchBaseStemMode(left, baseName); ok {
				if inferredModes[calKey] == "" || mode != models.SplitNone {
					inferredModes[calKey] = mode
				}
			}
		}
	}

	keysMap := map[string]bool{"": true}
	for k := range inferredModes {
		keysMap[k] = true
	}
	if cfg != nil {
		if activeKey != "" {
			keysMap[activeKey] = true
		}
		for k := range cfg.CalendarSplits {
			keysMap[strings.TrimSpace(k)] = true
		}
	}

	var namedKeys []string
	for k := range keysMap {
		if k != "" {
			namedKeys = append(namedKeys, k)
		}
	}
	sort.Strings(namedKeys)

	allKeys := append([]string{""}, namedKeys...)
	out := make([]models.CalendarInfo, 0, len(allKeys))
	for _, k := range allKeys {
		mode := resolveCalendarSplitMode(k, inferredModes[k], cfg)
		effBase := baseName
		label := fmt.Sprintf("Основной (%s)", baseName)
		if k != "" {
			effBase = baseName + "--" + k
			label = fmt.Sprintf("%s (%s)", k, effBase)
		}
		out = append(out, models.CalendarInfo{
			Key:       k,
			Label:     label,
			BaseName:  effBase,
			SplitMode: mode,
			Active:    k == activeKey,
		})
	}
	return out
}

func resolveCalendarSplitMode(calKey, diskMode string, cfg *models.Config) string {
	if cfg != nil && cfg.CalendarSplits != nil {
		if m, ok := cfg.CalendarSplits[calKey]; ok {
			switch m {
			case models.SplitNone, models.SplitYear, models.SplitMonth:
				return m
			}
		}
	}
	if diskMode != "" {
		if cfg != nil {
			cfg.SetCalendarSplitMode(calKey, diskMode)
		}
		return diskMode
	}
	if calKey == "" && cfg != nil {
		return cfg.GetCalendarSplitMode("")
	}
	return models.SplitNone
}

func matchBaseStemMode(left, baseName string) (string, bool) {
	if left == baseName {
		return models.SplitNone, true
	}
	if left == "unknown_"+baseName {
		return models.SplitYear, true
	}
	// YYYY_<baseName>
	if len(left) == len(baseName)+5 && left[4] == '_' && left[5:] == baseName && isAllDigits(left[:4]) {
		return models.SplitYear, true
	}
	// YYYY-MM_<baseName>
	if len(left) == len(baseName)+8 && left[4] == '-' && left[7] == '_' && left[8:] == baseName &&
		isAllDigits(left[:4]) && isAllDigits(left[5:7]) {
		return models.SplitMonth, true
	}
	return "", false
}

func isAllDigits(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// ExpandPath expands leading "~" / "~/" / "~\" and environment variables
// (both Unix $VAR and Windows %VAR%) and cleans the resulting path.
func ExpandPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if strings.Contains(p, "%") {
		p = expandWindowsEnv(p)
	}
	if strings.Contains(p, "$") {
		p = os.ExpandEnv(p)
	}
	if p == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Clean(home)
		}
	} else if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return filepath.Clean(p)
}

func expandWindowsEnv(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '%' {
			end := strings.IndexByte(s[i+1:], '%')
			if end > 0 {
				varName := s[i+1 : i+1+end]
				if val := os.Getenv(varName); val != "" {
					out.WriteString(val)
					i += end + 2
					continue
				}
			}
		}
		out.WriteByte(s[i])
		i++
	}
	return out.String()
}

// NormalizeDataPath normalizes a user-supplied data directory path for storing
// in Config.DataPath: empty stays "" (meaning the default global ~/.mycal
// directory), current-directory markers (".", "./", ".\") become ".", and other
// paths are expanded via ExpandPath.
func NormalizeDataPath(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if trimmed == "." || trimmed == "./" || trimmed == `.\` {
		return "."
	}
	return ExpandPath(trimmed)
}

// EnsureWritableDir creates dir if needed and verifies it can be written to,
// so an unusable data path is reported when it is chosen rather than on the
// next launch. The probe file is created and removed via the temp-file API, so
// nothing is left behind if the process dies mid-check.
func EnsureWritableDir(dir string) error {
	dir = ExpandPath(dir)
	if dir == "" {
		dir = "."
	}
	if dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("не удалось создать каталог %q: %s", dir, reason(err))
		}
	}
	probe, err := os.CreateTemp(dir, ".mycal-write-test-*")
	if err != nil {
		return fmt.Errorf("каталог %q недоступен для записи: %s", dir, reason(err))
	}
	name := probe.Name()
	probe.Close()
	os.Remove(name)
	return nil
}

// SortSessions orders sessions by date then time then ID (stable, total order).
func SortSessions(sessions []models.Session) {
	sort.Slice(sessions, func(i, j int) bool {
		a, b := sessions[i], sessions[j]
		if da, db := a.DateKey(), b.DateKey(); da != db {
			return da < db
		}
		if a.Time != b.Time {
			return a.Time < b.Time
		}
		return a.ID < b.ID
	})
}

func warn(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "предупреждение: "+format+"\n", args...)
}
