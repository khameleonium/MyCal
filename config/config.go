// Package config reads and writes config_mycal.json.
package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"mycalendar/models"
)

const ConfigFile = "config_mycal.json"

// DefaultConfig returns a fresh copy of the defaults (defined in one place, in
// models.DefaultConfig).
func DefaultConfig() *models.Config {
	cfg := models.DefaultConfig
	return &cfg
}

// Load reads the configuration.
//
//   - Missing file            -> defaults, no error.
//   - Unreadable (permissions) -> error.
//   - Corrupt JSON            -> the file is moved aside to *.corrupt-<ts>, a
//     note is printed to stderr, and defaults are returned. The tool stays
//     usable and the next Save writes a clean file.
func Load(ctx context.Context, path string) (*models.Config, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return DefaultConfig(), nil
		}
		if errors.Is(err, fs.ErrPermission) {
			return nil, fmt.Errorf("нет прав на чтение %s", path)
		}
		return nil, err
	}

	var cfg models.Config
	if err := json.Unmarshal(trimBOM(data), &cfg); err != nil {
		aside := path + ".corrupt-" + time.Now().Format("20060102-150405")
		if renameErr := os.Rename(path, aside); renameErr == nil {
			fmt.Fprintf(os.Stderr,
				"[!] файл настроек %s повреждён (нарушена структура); он сохранён как %s, применены значения по умолчанию.\n",
				filepath.Base(path), filepath.Base(aside))
		} else {
			fmt.Fprintf(os.Stderr,
				"[!] файл настроек %s повреждён; применены значения по умолчанию (при сохранении он будет перезаписан).\n",
				filepath.Base(path))
		}
		return DefaultConfig(), nil
	}
	return &cfg, nil
}

// Save writes the configuration atomically.
func Save(ctx context.Context, path string, cfg *models.Config) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("сериализация настроек: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("не удалось создать временный файл: %s", fsReason(err))
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("не удалось записать настройки: %s", fsReason(err))
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("не удалось сбросить настройки на диск: %s", fsReason(err))
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("не удалось закрыть временный файл: %s", fsReason(err))
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("не удалось заменить %s: %s", filepath.Base(path), fsReason(err))
	}
	return nil
}

// ResolveConfigPath returns the active config path, preferring config_mycal.json
// and falling back to a legacy config.json if only that exists.
func ResolveConfigPath() string {
	if _, err := os.Stat(ConfigFile); err == nil {
		return ConfigFile
	}
	if _, err := os.Stat("config.json"); err == nil {
		return "config.json"
	}
	return ConfigFile
}

func trimBOM(b []byte) []byte {
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		return b[3:]
	}
	return b
}

func fsReason(err error) string {
	switch {
	case errors.Is(err, fs.ErrPermission):
		return "нет прав доступа"
	case errors.Is(err, fs.ErrExist):
		return "файл уже существует"
	default:
		return err.Error()
	}
}
