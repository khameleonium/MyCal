package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"mycalendar/models"
)

const legacyDataFile = "my_calendar.json"

// Load reads all data files according to the split mode and merges them.
func Load(ctx context.Context, dir, baseName, mode string) (*models.Calendar, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	switch mode {
	case models.SplitNone:
		mainPath := filepath.Join(dir, baseName+".json")
		cal, err := loadSingle(ctx, mainPath)
		if err != nil {
			return cal, err
		}
		if len(cal.Entries) == 0 {
			legacyPath := filepath.Join(dir, legacyDataFile)
			if legacyPath != mainPath {
				if legacyCal, legacyErr := loadSingle(ctx, legacyPath); legacyErr == nil && len(legacyCal.Entries) > 0 {
					return legacyCal, nil
				}
			}
		}
		return cal, nil
	case models.SplitYear:
		return loadMerged(ctx, globFiles(dir, "????_"+baseName+".json")), nil
	case models.SplitMonth:
		return loadMerged(ctx, globFiles(dir, "????-??_"+baseName+".json")), nil
	default:
		return &models.Calendar{}, nil
	}
}

func loadMerged(ctx context.Context, files []string) *models.Calendar {
	merged := &models.Calendar{}
	for _, f := range files {
		cal, err := loadSingle(ctx, f)
		if err != nil {
			continue
		}
		merged.Entries = append(merged.Entries, cal.Entries...)
	}
	sortEntries(merged.Entries)
	return merged
}

func loadSingle(ctx context.Context, path string) (*models.Calendar, error) {
	if err := ctx.Err(); err != nil {
		return &models.Calendar{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &models.Calendar{}, nil
		}
		// Unreadable main file — try the backup without writing it back.
		return loadBackup(path, false), nil
	}

	var cal models.Calendar
	if err := json.Unmarshal(data, &cal); err != nil {
		// Corrupt main file — restore from backup if one parses.
		return loadBackup(path, true), nil
	}
	return &cal, nil
}

func loadBackup(path string, writeBack bool) *models.Calendar {
	bakData, err := os.ReadFile(path + ".bak")
	if err != nil {
		return &models.Calendar{}
	}
	var cal models.Calendar
	if err := json.Unmarshal(bakData, &cal); err != nil {
		return &models.Calendar{}
	}
	if writeBack {
		if err := os.WriteFile(path, bakData, 0o644); err != nil {
			warnf("не удалось восстановить файл из бэкапа %s: %v", path, err)
		}
	}
	return &cal
}

// Save writes the calendar to disk according to the split mode and removes any
// data files that belong to the other split modes, so switching modes leaves no
// stale copies behind.
func Save(ctx context.Context, dir, baseName string, cal *models.Calendar, mode string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch mode {
	case models.SplitNone:
		if err := writeCalendar(filepath.Join(dir, baseName+".json"), cal); err != nil {
			return err
		}
		removeFiles(globFiles(dir, "????_"+baseName+".json"))
		removeFiles(globFiles(dir, "????-??_"+baseName+".json"))
		return nil
	case models.SplitYear:
		return saveSplit(dir, baseName, cal, "2006")
	case models.SplitMonth:
		return saveSplit(dir, baseName, cal, "2006-01")
	default:
		return fmt.Errorf("неизвестный режим хранения: %s", mode)
	}
}

func saveSplit(dir, baseName string, cal *models.Calendar, groupFmt string) error {
	groups := make(map[string]*models.Calendar)
	for _, de := range cal.Entries {
		key := dateKey(de.Date, groupFmt)
		if groups[key] == nil {
			groups[key] = &models.Calendar{}
		}
		groups[key].Entries = append(groups[key].Entries, de)
	}

	for key, c := range groups {
		if err := writeCalendar(filepath.Join(dir, key+"_"+baseName+".json"), c); err != nil {
			return err
		}
	}

	// Remove files from the sibling modes and stale groups of this mode.
	var stale []string
	if groupFmt == "2006" {
		stale = append(stale, globFiles(dir, "????-??_"+baseName+".json")...)
	} else {
		stale = append(stale, globFiles(dir, "????_"+baseName+".json")...)
	}
	stale = append(stale, filepath.Join(dir, baseName+".json"))
	for _, ef := range globFiles(dir, patternForFmt(groupFmt)+"_"+baseName+".json") {
		if _, ok := groups[fileNameKey(ef, baseName, groupFmt)]; !ok {
			stale = append(stale, ef)
		}
	}
	removeFiles(stale)
	return nil
}

// writeCalendar serialises cal and writes it to path atomically: the previous
// contents are copied to path+".bak", the new contents are written to a temp
// file, fsync'd, then renamed over path (os.Rename replaces the destination on
// every supported platform, so there is no window where path is missing).
func writeCalendar(path string, cal *models.Calendar) error {
	data, err := json.MarshalIndent(cal, "", "  ")
	if err != nil {
		return fmt.Errorf("сериализация календаря: %w", err)
	}
	return atomicWriteFile(path, data)
}

func atomicWriteFile(path string, data []byte) error {
	if current, readErr := os.ReadFile(path); readErr == nil {
		if err := os.WriteFile(path+".bak", current, 0o644); err != nil {
			warnf("не удалось создать бэкап %s: %v", path+".bak", err)
		}
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("создание временного файла: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once the rename succeeds

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("запись временного файла: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("сброс временного файла на диск: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("закрытие временного файла: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("замена файла: %w", err)
	}
	syncDir(filepath.Dir(path))
	return nil
}

// syncDir flushes a directory entry so a freshly renamed file survives a crash.
// It is a best-effort no-op on Windows, where directories cannot be opened for
// sync.
func syncDir(dir string) {
	if runtime.GOOS == "windows" {
		return
	}
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	defer d.Close()
	_ = d.Sync()
}

func removeFiles(paths []string) {
	for _, p := range paths {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			warnf("не удалось удалить неиспользуемый файл %s: %v", p, err)
		}
		if err := os.Remove(p + ".bak"); err != nil && !os.IsNotExist(err) {
			warnf("не удалось удалить бэкап %s: %v", p+".bak", err)
		}
	}
}

func patternForFmt(groupFmt string) string {
	if groupFmt == "2006-01" {
		return "????-??"
	}
	return "????"
}

func dateKey(isoDate, groupFmt string) string {
	switch groupFmt {
	case "2006":
		if len(isoDate) >= 4 {
			return isoDate[:4]
		}
	case "2006-01":
		if len(isoDate) >= 7 {
			return isoDate[:7]
		}
	}
	return isoDate
}

func fileNameKey(filePath, baseName, groupFmt string) string {
	prefix := strings.TrimSuffix(filepath.Base(filePath), "_"+baseName+".json")
	switch groupFmt {
	case "2006":
		if len(prefix) >= 4 {
			return prefix[:4]
		}
	case "2006-01":
		if len(prefix) >= 7 {
			return prefix[:7]
		}
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

func sortEntries(entries []models.DateEntry) {
	sort.Slice(entries, func(i, j int) bool { return entries[i].Date < entries[j].Date })
}

func warnf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "предупреждение: "+format+"\n", args...)
}
