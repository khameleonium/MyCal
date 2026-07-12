package config

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"mycalendar/models"
)

const ConfigFile = "config_mycal.json"

func DefaultConfig() *models.Config {
	return &models.Config{
		DefaultDuration: 60,
		SplitMode:       models.SplitNone,
		DataFileName:    "mycal",
		UseSystemDate:   true,
	}
}

// Load reads the configuration file and returns a Config.
// If the file does not exist, returns a Config with defaults.
func Load(ctx context.Context, path string) (*models.Config, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return nil, fmt.Errorf("чтение файла конфигурации: %w", err)
	}

	var cfg models.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("разбор файла конфигурации: %w", err)
	}

	return &cfg, nil
}

// Save writes the configuration to the given path atomically.
func Save(ctx context.Context, path string, cfg *models.Config) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("сериализация конфигурации: %w", err)
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("запись конфигурации: %w", err)
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "предупреждение: не удалось удалить старый файл %s: %v\n", path, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		if rmErr := os.Remove(tmpPath); rmErr != nil && !os.IsNotExist(rmErr) {
			fmt.Fprintf(os.Stderr, "предупреждение: не удалось удалить временный файл %s: %v\n", tmpPath, rmErr)
		}
		return fmt.Errorf("сохранение конфигурации: %w", err)
	}

	return nil
}

// ResolveConfigPath returns the active config path, preferring config_mycal.json.
func ResolveConfigPath() string {
	const legacyFile = "config.json"
	if _, err := os.Stat(ConfigFile); err == nil {
		return ConfigFile
	}
	if _, err := os.Stat(legacyFile); err == nil {
		return legacyFile
	}
	return ConfigFile
}
