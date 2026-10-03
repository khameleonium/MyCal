// Command mycalendar is a small offline CLI calendar / time tracker.
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
	"mycalendar/storage"
)

// version is set at build time via -ldflags "-X main.version=vX.Y.Z".
var version = "dev"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "-v", "--version":
			fmt.Printf("mycalendar %s\n", version)
			return
		case "help", "-h", "--help", "/?":
			app.PrintHelp()
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
	if err := storage.EnsureWritableDir(dataPath); err != nil {
		return "", err
	}
	return dataPath, nil
}
