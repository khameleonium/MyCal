package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"mycalendar/app"
	"mycalendar/calendar"
	"mycalendar/config"
)

func main() {
	// SIGINT/SIGTERM cancels the context so an in-flight disk write can abort
	// cleanly instead of leaving a half-written file.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cfgPath := config.ResolveConfigPath()
	cfg, err := config.Load(ctx, cfgPath)
	if err != nil {
		return fmt.Errorf("загрузка конфигурации: %w", err)
	}

	// Migrate a legacy config file name to the canonical one.
	if cfgPath != config.ConfigFile {
		if err := config.Save(ctx, config.ConfigFile, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "предупреждение: не удалось создать %s: %v\n", config.ConfigFile, err)
		} else {
			_ = os.Remove(cfgPath)
			cfgPath = config.ConfigFile
		}
	}

	dataDir, err := resolveDataDir(cfg.DataPath)
	if err != nil {
		return err
	}

	svc, err := calendar.NewService(ctx, dataDir, cfg.DataFileName, cfg.SplitMode)
	if err != nil {
		return fmt.Errorf("загрузка календаря: %w", err)
	}

	application := app.NewApp(ctx, svc, cfg, cfgPath)

	if len(os.Args) > 1 {
		if !application.RunCLI(os.Args[1], os.Args[2:]) {
			os.Exit(1)
		}
		return nil
	}

	application.Start()
	return nil
}

func resolveDataDir(dataPath string) (string, error) {
	dataPath = strings.TrimSpace(dataPath)
	if dataPath == "" || dataPath == "." {
		return ".", nil
	}
	if err := os.MkdirAll(dataPath, 0o755); err != nil {
		return "", fmt.Errorf("создание каталога данных: %w", err)
	}
	return dataPath, nil
}
