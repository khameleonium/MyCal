package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"mycalendar/calendar"
	"mycalendar/config"
	"mycalendar/menu"
	"mycalendar/models"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfgPath := config.ResolveConfigPath()
	cfg, err := config.Load(ctx, cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Ошибка загрузки конфигурации: %v\n", err)
		os.Exit(1)
	}

	if cfgPath != config.ConfigFile {
		cfgPath = config.ConfigFile
		if err := config.Save(ctx, cfgPath, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "Ошибка создания %s: %v\n", config.ConfigFile, err)
		}
	}

	dataDir := resolveDataDir(cfg.DataPath)
	svc, err := calendar.NewService(ctx, dataDir, cfg.DataFileName, cfg.SplitMode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Ошибка загрузки календаря: %v\n", err)
		os.Exit(1)
	}

	if len(os.Args) > 1 {
		handleCommand(svc, cfg)
		return
	}

	app := menu.NewApp(svc, cfg, cfgPath)
	app.CheckIntegrity()
	app.Run()
}

func resolveDataDir(dataPath string) string {
	dataPath = strings.TrimSpace(dataPath)
	if dataPath == "" || dataPath == "." {
		return "."
	}
	if err := os.MkdirAll(dataPath, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Ошибка создания каталога данных: %v\n", err)
		os.Exit(1)
	}
	return dataPath
}

func handleCommand(svc *calendar.Service, cfg *models.Config) {
	app := menu.NewApp(svc, cfg, config.ResolveConfigPath())

	cmd := strings.ToLower(os.Args[1])
	args := os.Args[2:]

	switch cmd {
	case "add", "a":
		app.AddByArgs(args)
	case "view", "v":
		app.ViewByArgs(args)
	case "delete", "d":
		app.DeleteByArgs(args)
	case "export", "e":
		app.ExportByArgs(args)
	case "today", "t":
		app.TodayView()
	case "week", "w":
		app.WeekView()
	case "month", "m":
		app.MonthView()
	case "help", "-h", "--help":
		menu.PrintHelp()
	default:
		fmt.Fprintf(os.Stderr, "Неизвестная команда: %s\n", cmd)
		menu.PrintHelp()
		os.Exit(1)
	}
}
