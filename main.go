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
	switch cmd {
	case "add", "a":
		app.AddEntryQuick()
	case "today", "t":
		app.TodayView()
	case "week", "w":
		app.WeekView()
	case "month", "m":
		app.MonthView()
	default:
		fmt.Fprintf(os.Stderr, "Неизвестная команда: %s\n", cmd)
		fmt.Fprintln(os.Stderr, "  add | a     — быстрое добавление записей")
		fmt.Fprintln(os.Stderr, "  today | t   — записи на сегодня")
		fmt.Fprintln(os.Stderr, "  week | w    — записи на этой неделе")
		fmt.Fprintln(os.Stderr, "  month | m   — записи за этот месяц")
		os.Exit(1)
	}
}
