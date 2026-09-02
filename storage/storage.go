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
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	var files []string
	switch mode {
	case models.SplitNone:
		files = []string{filepath.Join(dir, baseName+".json")}
	case models.SplitYear:
		files = globFiles(dir, "????_"+baseName+".json")
	case models.SplitMonth:
		files = globFiles(dir, "????-??_"+baseName+".json")
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

// loadFile reads one file. A missing file is not an error (empty calendar).
func loadFile(path string) (*models.Calendar, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &models.Calendar{}, nil, nil
		}
		return nil, nil, fmt.Errorf("не удалось прочитать %s: %s", path, reason(err))
	}

	if cal, ok := decode(data); ok {
		return cal, nil, nil
	}

	// The main file is corrupt. Try its backup.
	base := filepath.Base(path)
	if bak, err := os.ReadFile(path + ".bak"); err == nil {
		if cal, ok := decode(bak); ok {
			if writeErr := os.WriteFile(path, bak, 0o644); writeErr != nil {
				return cal, []string{fmt.Sprintf(
					"файл %s повреждён; восстановлен из резервной копии в памяти, но записать его обратно не удалось (%s)",
					base, reason(writeErr))}, nil
			}
			return cal, []string{fmt.Sprintf("файл %s был повреждён и восстановлен из резервной копии %s.bak", base, base)}, nil
		}
	}

	// No usable backup — set the corrupt file aside so nothing is lost and the
	// next save starts clean.
	aside := path + ".corrupt-" + time.Now().Format("20060102-150405")
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
		return &models.Calendar{}, true
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
	if err := ctx.Err(); err != nil {
		return err
	}
	SortSessions(cal.Sessions)

	switch mode {
	case models.SplitNone:
		if err := writeJSON(filepath.Join(dir, baseName+".json"), cal); err != nil {
			return err
		}
		removeFiles(globFiles(dir, "????_"+baseName+".json"))
		removeFiles(globFiles(dir, "????-??_"+baseName+".json"))
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
	for _, s := range cal.Sessions {
		d := s.Date()
		if len(d) < keyLen {
			continue
		}
		key := d[:keyLen]
		if groups[key] == nil {
			groups[key] = &models.Calendar{}
		}
		groups[key].Sessions = append(groups[key].Sessions, s)
	}

	for key, part := range groups {
		if err := writeJSON(filepath.Join(dir, key+"_"+baseName+".json"), part); err != nil {
			return err
		}
	}

	// Delete the single-file copy, the other split mode's files, and any files
	// of this mode whose group no longer has sessions.
	var stale []string
	stale = append(stale, filepath.Join(dir, baseName+".json"))
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
	data, err := json.MarshalIndent(cal, "", "  ")
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
	case isNoSpace(err):
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
	removeFiles([]string{filepath.Join(dir, baseName+".json")})
	removeFiles(globFiles(dir, "????_"+baseName+".json"))
	removeFiles(globFiles(dir, "????-??_"+baseName+".json"))
}

// SortSessions orders sessions by date then time then ID (stable, total order).
func SortSessions(sessions []models.Session) {
	sort.Slice(sessions, func(i, j int) bool {
		a, b := sessions[i], sessions[j]
		if da, db := a.Date(), b.Date(); da != db {
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
