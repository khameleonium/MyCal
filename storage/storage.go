package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"mycalendar/models"
)

// Load reads all data files according to the split mode and merges them.
func Load(ctx context.Context, dir, baseName, mode string) (*models.Calendar, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var files []string

	switch mode {
	case models.SplitNone:
		mainPath := filepath.Join(dir, baseName+".json")
		cal, err := loadSingle(ctx, mainPath)
		if err != nil || len(cal.Entries) == 0 {
			legacyPath := filepath.Join(dir, "my_calendar.json")
			if legacyPath != mainPath {
				if legacyCal, legacyErr := loadSingle(ctx, legacyPath); legacyErr == nil && len(legacyCal.Entries) > 0 {
					return legacyCal, nil
				}
			}
		}
		return cal, err
	case models.SplitYear:
		files = globFiles(dir, "????_"+baseName+".json")
	case models.SplitMonth:
		files = globFiles(dir, "????-??_"+baseName+".json")
	default:
		return &models.Calendar{}, nil
	}

	if len(files) == 0 {
		return &models.Calendar{}, nil
	}

	merged := &models.Calendar{}
	for _, f := range files {
		cal, err := loadSingle(ctx, f)
		if err != nil {
			continue
		}
		merged.Entries = append(merged.Entries, cal.Entries...)
	}

	sort.Slice(merged.Entries, func(i, j int) bool {
		return merged.Entries[i].Date < merged.Entries[j].Date
	})
	return merged, nil
}

func loadSingle(ctx context.Context, path string) (*models.Calendar, error) {
	if err := ctx.Err(); err != nil {
		return &models.Calendar{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return loadBackup(ctx, path, false)
	}

	var cal models.Calendar
	if err := json.Unmarshal(data, &cal); err != nil {
		restored, restoreErr := loadBackup(ctx, path, true)
		if restoreErr != nil {
			return &models.Calendar{}, nil
		}
		return restored, nil
	}

	return &cal, nil
}

func loadBackup(ctx context.Context, path string, writeBack bool) (*models.Calendar, error) {
	if err := ctx.Err(); err != nil {
		return &models.Calendar{}, err
	}
	bakPath := path + ".bak"
	bakData, err := os.ReadFile(bakPath)
	if err != nil {
		return &models.Calendar{}, nil
	}
	var cal models.Calendar
	if err := json.Unmarshal(bakData, &cal); err != nil {
		return &models.Calendar{}, nil
	}
	if writeBack {
		if err := os.WriteFile(path, bakData, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "предупреждение: не удалось восстановить файл из бэкапа %s: %v\n", path, err)
		}
	}
	return &cal, nil
}

// Save writes the calendar to disk according to the split mode.
func Save(ctx context.Context, dir, baseName string, cal *models.Calendar, mode string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch mode {
	case models.SplitNone:
		return saveSingle(ctx, filepath.Join(dir, baseName+".json"), cal)
	case models.SplitYear:
		return saveSplit(ctx, dir, baseName, cal, "2006")
	case models.SplitMonth:
		return saveSplit(ctx, dir, baseName, cal, "2006-01")
	default:
		return fmt.Errorf("неизвестный режим хранения: %s", mode)
	}
}

func saveSingle(ctx context.Context, path string, cal *models.Calendar) error {
	return atomicWrite(ctx, path, cal)
}

func saveSplit(ctx context.Context, dir, baseName string, cal *models.Calendar, groupFmt string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	groups := make(map[string]*models.Calendar)
	for _, de := range cal.Entries {
		key := dateKey(de.Date, groupFmt)
		if groups[key] == nil {
			groups[key] = &models.Calendar{}
		}
		groups[key].Entries = append(groups[key].Entries, de)
	}

	// Remove old split files that are no longer needed.
	existingPattern := strings.ReplaceAll(groupFmt, "2006", "????")
	existingPattern = strings.ReplaceAll(existingPattern, "01", "??")
	existingFiles := globFiles(dir, existingPattern+"_"+baseName+".json")
	for _, ef := range existingFiles {
		key := fileNameKey(ef, baseName, groupFmt)
		if _, ok := groups[key]; !ok {
			if err := os.Remove(ef); err != nil && !os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "предупреждение: не удалось удалить неиспользуемый файл %s: %v\n", ef, err)
			}
			if err := os.Remove(ef + ".bak"); err != nil && !os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "предупреждение: не удалось удалить бэкап неиспользуемого файла %s: %v\n", ef+".bak", err)
			}
		}
	}

	for key, c := range groups {
		path := filepath.Join(dir, key+"_"+baseName+".json")
		if err := atomicWrite(ctx, path, c); err != nil {
			return err
		}
	}
	return nil
}

func atomicWrite(ctx context.Context, path string, cal *models.Calendar) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cal, "", "  ")
	if err != nil {
		return fmt.Errorf("сериализация календаря: %w", err)
	}

	if current, readErr := os.ReadFile(path); readErr == nil {
		if err := os.WriteFile(path+".bak", current, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "предупреждение: не удалось создать бэкап %s: %v\n", path+".bak", err)
		}
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("запись временного файла: %w", err)
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "предупреждение: не удалось удалить старый файл %s: %v\n", path, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		if rmErr := os.Remove(tmpPath); rmErr != nil && !os.IsNotExist(rmErr) {
			fmt.Fprintf(os.Stderr, "предупреждение: не удалось удалить временный файл %s: %v\n", tmpPath, rmErr)
		}
		return fmt.Errorf("замена файла: %w", err)
	}

	return nil
}

func dateKey(isoDate, groupFmt string) string {
	// isoDate is "YYYY-MM-DD"
	switch groupFmt {
	case "2006":
		return isoDate[:4]
	case "2006-01":
		return isoDate[:7]
	}
	return isoDate
}

func fileNameKey(filePath, baseName, groupFmt string) string {
	base := filepath.Base(filePath)
	prefix := strings.TrimSuffix(base, "_"+baseName+".json")
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
