// Command mycalendar is a small offline CLI calendar / time tracker.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
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

	go func() {
		<-ctx.Done()
		fmt.Println()
		os.Exit(0)
	}()

	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "[✗] "+err.Error())
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cfgPath, legacyPath, err := config.ResolveConfigPath()
	if err != nil {
		return err
	}
	loadPath := cfgPath
	if legacyPath != "" {
		loadPath = legacyPath
	}
	cfg, err := config.Load(ctx, loadPath)
	if err != nil {
		return fmt.Errorf("не удалось загрузить настройки (%s): %w", loadPath, err)
	}

	// Move a config from the current directory to the global location. A legacy
	// config kept the data next to it, so pin the data path to that directory.
	if legacyPath != "" {
		if strings.TrimSpace(cfg.DataPath) == "" {
			if wd, wdErr := os.Getwd(); wdErr == nil {
				cfg.DataPath = wd
			}
		}
		if err := config.Save(ctx, cfgPath, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "[!] не удалось создать %s: %v\n", cfgPath, err)
		} else {
			_ = os.Remove(legacyPath)
		}
	}

	dataDir, err := prepareDataDir(cfg.DataPath, filepath.Dir(cfgPath))
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
func prepareDataDir(dataPath, defaultDir string) (string, error) {
	dataPath = strings.TrimSpace(dataPath)
	if dataPath == "" {
		dataPath = defaultDir
	}
	if err := storage.EnsureWritableDir(dataPath); err != nil {
		return "", err
	}
	return dataPath, nil
}
