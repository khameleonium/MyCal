// Command mycalendar is a small offline CLI calendar / time tracker.
package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"mycalendar/app"
	"mycalendar/calendar"
	"mycalendar/config"
)

// version is set at build time via -ldflags "-X main.version=vX.Y.Z".
var version = "dev"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "-v", "--version":
			fmt.Printf("mycalendar %s\n", version)
			return
		}
	}

	// SIGINT cancels the context so an in-flight disk write can abort cleanly.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "[✗] "+err.Error())
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cfgPath := config.ResolveConfigPath()
	cfg, err := config.Load(ctx, cfgPath)
	if err != nil {
		return fmt.Errorf("не удалось загрузить настройки (%s): %w", cfgPath, err)
	}

	// Move a legacy config file to the canonical name.
	if cfgPath != config.ConfigFile {
		if err := config.Save(ctx, config.ConfigFile, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "[!] не удалось создать %s: %v\n", config.ConfigFile, err)
		} else {
			_ = os.Remove(cfgPath)
			cfgPath = config.ConfigFile
		}
	}

	dataDir, err := prepareDataDir(cfg.DataPath)
	if err != nil {
		return err
	}

	svc, err := calendar.NewService(ctx, dataDir, cfg.DataFileName, cfg.SplitMode)
	if err != nil {
		return fmt.Errorf("не удалось открыть календарь: %w", err)
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

// prepareDataDir resolves the data directory, creating it if needed, and checks
// that it is writable so problems are reported up front rather than on first save.
func prepareDataDir(dataPath string) (string, error) {
	dataPath = strings.TrimSpace(dataPath)
	if dataPath == "" {
		dataPath = "."
	}
	if dataPath != "." {
		if err := os.MkdirAll(dataPath, 0o755); err != nil {
			return "", fmt.Errorf("не удалось создать каталог данных %q: %s", dataPath, explain(err))
		}
	}
	probe := filepath.Join(dataPath, ".mycal-write-test")
	if err := os.WriteFile(probe, []byte("ok"), 0o644); err != nil {
		return "", fmt.Errorf("каталог данных %q недоступен для записи: %s", dataPath, explain(err))
	}
	_ = os.Remove(probe)
	return dataPath, nil
}

func explain(err error) string {
	if errors.Is(err, fs.ErrPermission) {
		return "нет прав доступа"
	}
	return err.Error()
}
