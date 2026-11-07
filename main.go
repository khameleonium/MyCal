// Command mycal is a small offline CLI calendar / time tracker.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"mycalendar/app"
	"mycalendar/calendar"
	"mycalendar/color"
	"mycalendar/config"
	"mycalendar/models"
	"mycalendar/service"
	"mycalendar/storage"
	"mycalendar/tui"
	"mycalendar/web"
)

// version is set at build time via -ldflags "-X main.version=vX.Y.Z".
var version = "dev"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "-v", "--version":
			fmt.Printf("mycal %s\n", version)
			return
		case "help", "-h", "--help", "/?":
			app.PrintHelp()
			return
		}
	}

	// On Windows, if started by Task Scheduler (schtasks), the working directory
	// defaults to C:\Windows\System32. Switch to the executable's directory so
	// relative paths never point into System32.
	if runtime.GOOS == "windows" {
		if wd, err := os.Getwd(); err == nil {
			low := strings.ToLower(filepath.Clean(wd))
			if strings.HasSuffix(low, `\windows\system32`) || strings.HasSuffix(low, `\windows\syswow64`) {
				if exe, exeErr := service.GetExePath(); exeErr == nil {
					_ = os.Chdir(filepath.Dir(exe))
				}
			}
		}
	}

	// SIGINT cancels the context so an in-flight disk write or HTTP server can shut down cleanly.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	go func() {
		<-ctx.Done()
		// Allow a short grace period for defer/shutdown handlers to finish
		// before forcing exit if a blocking stdin read in cooked CLI mode hangs.
		time.Sleep(1500 * time.Millisecond)
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

	// Discover calendars in dataDir so any per-calendar split mode on disk is synced.
	_ = storage.DiscoverCalendars(dataDir, cfg.DataFileName, cfg)

	args, calFlag, hasCalFlag := extractCalendarFlag(os.Args[1:])
	origActiveCal := cfg.ActiveCalendar
	if hasCalFlag {
		switch strings.ToLower(strings.TrimSpace(calFlag)) {
		case "", "main", "default", "-", "основной", "mycal":
			cfg.ActiveCalendar = ""
		default:
			cfg.ActiveCalendar = models.SanitizeCalendarKey(calFlag)
		}
	}

	svc, err := calendar.NewService(ctx, dataDir, cfg.ActiveEffectiveBaseName(), cfg.ActiveSplitMode())
	if err != nil {
		return fmt.Errorf("не удалось открыть календарь: %w", err)
	}

	application := app.NewApp(ctx, svc, cfg, cfgPath)

	if len(args) > 0 {
		cmd := strings.ToLower(args[0])
		switch cmd {
		case "service":
			return handleServiceCmd(ctx, args[1:], cfg)
		case "tui", "-tui", "--tui":
			tuiApp := tui.NewApp(svc, cfg, cfgPath, version)
			return tuiApp.Run(ctx)
		case "cli", "-cli", "--cli":
			application.Start()
			return nil
		case "web", "-web", "--web":
			port := cfg.WebPort
			if port <= 0 {
				port = 8080
			}
			if len(args) > 1 {
				if p, err := strconv.Atoi(args[1]); err == nil && p > 0 {
					port = p
				}
			}
			webServer := web.NewServer(svc, cfg, cfgPath, version)
			return webServer.Start(ctx, port, true)
		default:
			ok := application.RunCLI(args[0], args[1:])
			if hasCalFlag && cfg.ActiveCalendar != origActiveCal {
				cfg.ActiveCalendar = origActiveCal
				_ = config.Save(ctx, cfgPath, cfg)
			}
			if !ok {
				os.Exit(1)
			}
			return nil
		}
	}

	// No arguments provided: launch configured default mode!
	switch cfg.DefaultMode {
	case models.ModeWeb:
		port := cfg.WebPort
		if port <= 0 {
			port = 8080
		}
		webServer := web.NewServer(svc, cfg, cfgPath, version)
		return webServer.Start(ctx, port, true)
	case models.ModeTUI:
		tuiApp := tui.NewApp(svc, cfg, cfgPath, version)
		return tuiApp.Run(ctx)
	default: // models.ModeCLI
		application.Start()
		return nil
	}
}

func handleServiceCmd(ctx context.Context, args []string, cfg *models.Config) error {
	mgr := service.NewManager()
	subcmd := "status"
	if len(args) > 0 {
		subcmd = strings.ToLower(args[0])
	}
	port := cfg.WebPort
	if port <= 0 {
		port = 8080
	}
	if len(args) > 1 {
		if p, err := strconv.Atoi(args[1]); err == nil && p > 0 {
			port = p
		}
	}

	switch subcmd {
	case "install", "enable":
		fmt.Printf("Установка службы MyCal (порт %d)...\n", port)
		if err := mgr.Install(ctx, port); err != nil {
			return err
		}
		fmt.Println(color.Green("[✓] Служба MyCal успешно установлена и запущена!"))
		fmt.Printf("    Веб-интерфейс доступен по адресу: http://127.0.0.1:%d\n", port)
		fmt.Println("    Для проверки статуса службы: mycal service status")
		fmt.Println("    Для остановки службы:        mycal service stop")
		return nil

	case "uninstall", "disable":
		fmt.Println("Удаление службы MyCal...")
		if err := mgr.Uninstall(ctx); err != nil {
			return err
		}
		fmt.Println(color.Green("[✓] Служба MyCal успешно остановлена и удалена."))
		return nil

	case "start":
		if err := mgr.Start(ctx); err != nil {
			return err
		}
		fmt.Println(color.Green("[✓] Служба MyCal запущена."))
		return nil

	case "stop":
		if err := mgr.Stop(ctx); err != nil {
			return err
		}
		fmt.Println(color.Green("[✓] Служба MyCal остановлена."))
		return nil

	case "restart":
		if err := mgr.Restart(ctx); err != nil {
			return err
		}
		fmt.Println(color.Green("[✓] Служба MyCal перезапущена."))
		return nil

	case "status":
		info, err := mgr.Status(ctx)
		if err != nil {
			return err
		}
		fmt.Println("Платформа службы:", info.Platform)
		if info.UnitPath != "" {
			fmt.Println("Файл юнита:     ", info.UnitPath)
		}
		if info.Installed {
			fmt.Println("Установлена:     ", color.Green("Да"))
		} else {
			fmt.Println("Установлена:     ", color.Yellow("Нет (используйте 'mycal service install')"))
		}
		if info.Running {
			fmt.Println("Состояние:       ", color.Green("Работает (active)"))
		} else {
			fmt.Println("Состояние:       ", color.Red("Остановлена / Неактивна"))
		}
		if info.Details != "" {
			fmt.Println("\nПодробный статус:")
			fmt.Println(info.Details)
		}
		return nil

	default:
		fmt.Println("Использование: mycal service <команда> [порт]")
		fmt.Println("Команды:")
		fmt.Println("  install [порт]  Установить и запустить службу (по умолчанию порт 8080)")
		fmt.Println("  uninstall       Остановить и удалить службу")
		fmt.Println("  start           Запустить службу")
		fmt.Println("  stop            Остановить службу")
		fmt.Println("  restart         Перезапустить службу")
		fmt.Println("  status          Проверить статус службы")
		return nil
	}
}

// prepareDataDir resolves the data directory, creating it if needed, and checks
// that it is writable so problems are reported up front rather than on first save.
func prepareDataDir(dataPath, defaultDir string) (string, error) {
	dataPath = storage.ExpandPath(dataPath)
	if dataPath == "" {
		dataPath = defaultDir
	}
	if err := storage.EnsureWritableDir(dataPath); err != nil {
		return "", err
	}
	return dataPath, nil
}

// extractCalendarFlag parses and strips -c <prefix>, --calendar <prefix>, -cal <prefix>
// (and their =<prefix> variants) from command-line arguments.
func extractCalendarFlag(args []string) ([]string, string, bool) {
	var clean []string
	var cal string
	var found bool
	for i := 0; i < len(args); i++ {
		arg := args[i]
		lower := strings.ToLower(arg)
		if lower == "-c" || lower == "--calendar" || lower == "-cal" || lower == "--cal" {
			if i+1 < len(args) {
				cal = args[i+1]
				found = true
				i++
				continue
			}
		}
		matched := false
		for _, prefix := range []string{"-c=", "--calendar=", "-cal=", "--cal="} {
			if strings.HasPrefix(lower, prefix) {
				cal = arg[len(prefix):]
				found = true
				matched = true
				break
			}
		}
		if !matched {
			clean = append(clean, arg)
		}
	}
	return clean, strings.TrimSpace(cal), found
}
