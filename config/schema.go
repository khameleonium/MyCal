package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"mycalendar/models"
	"mycalendar/parser"
	"mycalendar/storage"
)

// OptionSpec describes one selectable choice for an enum/boolean setting.
type OptionSpec struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// SettingSpec is the single source of truth for a configuration parameter:
// its key, human-readable label, hint, data kind, supported interfaces (Modes),
// selectable options, display formatter, and validator/mutator.
type SettingSpec struct {
	Key        string
	CLIKey     string
	Label      string
	ShortLabel string
	Hint       string
	Kind       string // "enum", "int", "bool", "string", "path", "date"
	Modes      []string
	Options    func(cfg *models.Config, availableTypes []string) []OptionSpec
	GetValue   func(cfg *models.Config) string
	GetDisplay func(cfg *models.Config) string
	SetValue   func(cfg *models.Config, raw string) error
}

// SplitModeLabels maps split_mode values to Russian labels.
var SplitModeLabels = map[string]string{
	models.SplitNone:  "В одном файле",
	models.SplitYear:  "По годам",
	models.SplitMonth: "По месяцам",
}

// DateCheckLabels maps date_check_mode values to Russian labels.
var DateCheckLabels = map[string]string{
	models.DateCheckOff:   "Выключена",
	models.DateCheckAsk:   "Спрашивать",
	models.DateCheckFix:   "Исправлять авто",
	models.DateCheckReask: "Переспрашивать",
}

// DateCheckOrder defines the cycle order for date_check_mode.
var DateCheckOrder = []string{
	models.DateCheckOff, models.DateCheckAsk, models.DateCheckFix, models.DateCheckReask,
}

// DefaultModeLabels maps default_mode values to Russian labels.
var DefaultModeLabels = map[string]string{
	models.ModeCLI: "Классический (CLI)",
	models.ModeTUI: "Интерактивный (TUI)",
	models.ModeWeb: "Веб-интерфейс (Web)",
}

// DefaultModeOrder defines the cycle order for default_mode.
var DefaultModeOrder = []string{models.ModeCLI, models.ModeTUI, models.ModeWeb}

// TUIDefaultPeriodLabels maps tui_default_period values to Russian labels.
var TUIDefaultPeriodLabels = map[string]string{
	"today": "Сегодня (по умолч.)",
	"all":   "Все записи",
	"week":  "Неделя",
	"month": "Месяц",
}

// DurationUnitLabels maps duration_unit values to Russian labels.
var DurationUnitLabels = map[string]string{
	"min":   "Минуты (мин)",
	"hours": "Часы (ч)",
}

// EffectiveToday returns the active "today" date based on UseSystemDate and CustomDate.
// Used across CLI, TUI, and Web so date pinning works identically everywhere.
func EffectiveToday(cfg *models.Config) time.Time {
	if cfg == nil || cfg.UseSystemDate || strings.TrimSpace(cfg.CustomDate) == "" {
		return time.Now()
	}
	if d, err := time.Parse("2006-01-02", strings.TrimSpace(cfg.CustomDate)); err == nil {
		return d
	}
	if d, err := parser.ParseDate(strings.TrimSpace(cfg.CustomDate), time.Now()); err == nil {
		return d
	}
	return time.Now()
}

// FormatCustomDateDisplay returns the human-readable status of system/custom date.
func FormatCustomDateDisplay(cfg *models.Config) string {
	if cfg == nil || cfg.UseSystemDate || strings.TrimSpace(cfg.CustomDate) == "" {
		return "Системная (" + parser.FormatDate(time.Now()) + ")"
	}
	return "Вручную (" + parser.FormatDate(EffectiveToday(cfg)) + ")"
}

// DateCheckHint returns the explanatory hint for the active date_check_mode.
func DateCheckHint(mode string) string {
	switch mode {
	case models.DateCheckOff:
		return "Выкл: 32.01 молча станет 01.02 без вопросов и предупреждений"
	case models.DateCheckAsk:
		return "Спрашивать: при вводе 32.01 спросит — оставить 01.02 или переввести"
	case models.DateCheckFix:
		return "Исправлять авто: 32.01 станет 01.02 с заметным уведомлением в консоли"
	case models.DateCheckReask:
		return "Переспрашивать: 32.01 отклоняется, программа попросит ввести дату заново"
	default:
		return "Реакция на несуществующие даты (напр. 32.01 -> 01.02)"
	}
}

// SanitizeBaseName strips path separators, dots, and whitespace from a base filename.
func SanitizeBaseName(s string) string {
	return models.SanitizeBaseName(s)
}

// SanitizeCalendarKey cleans a calendar suffix/prefix identifier so it can be
// safely used in `<base>--<key>.json`.
func SanitizeCalendarKey(s string) string {
	return models.SanitizeCalendarKey(s)
}

// SettingsSchema is the unified registry of all configuration parameters.
var SettingsSchema = []SettingSpec{
	{
		Key:        "default_mode",
		CLIKey:     "w",
		Label:      "Интерфейс по умолчанию",
		ShortLabel: "Режим запуска",
		Hint:       "Что открывать при запуске 'mycal' без параметров (CLI / TUI / Web)",
		Kind:       "enum",
		Modes:      []string{models.ModeCLI, models.ModeTUI, models.ModeWeb},
		Options: func(_ *models.Config, _ []string) []OptionSpec {
			return []OptionSpec{
				{Value: models.ModeCLI, Label: DefaultModeLabels[models.ModeCLI]},
				{Value: models.ModeTUI, Label: DefaultModeLabels[models.ModeTUI]},
				{Value: models.ModeWeb, Label: DefaultModeLabels[models.ModeWeb]},
			}
		},
		GetValue: func(cfg *models.Config) string { return cfg.DefaultMode },
		GetDisplay: func(cfg *models.Config) string {
			if l, ok := DefaultModeLabels[cfg.DefaultMode]; ok {
				return l
			}
			return DefaultModeLabels[models.ModeCLI]
		},
		SetValue: func(cfg *models.Config, raw string) error {
			switch strings.ToLower(strings.TrimSpace(raw)) {
			case models.ModeCLI, models.ModeTUI, models.ModeWeb:
				cfg.DefaultMode = strings.ToLower(strings.TrimSpace(raw))
				return nil
			default:
				return fmt.Errorf("некорректный режим запуска: %s", raw)
			}
		},
	},
	{
		Key:        "web_port",
		CLIKey:     "o",
		Label:      "Порт веб-сервера",
		ShortLabel: "Порт Web-сервера",
		Hint:       "TCP-порт для запуска веб-интерфейса (1–65535, по умолчанию 8080)",
		Kind:       "int",
		Modes:      []string{models.ModeCLI, models.ModeTUI, models.ModeWeb},
		GetValue:   func(cfg *models.Config) string { return strconv.Itoa(cfg.WebPort) },
		GetDisplay: func(cfg *models.Config) string { return strconv.Itoa(cfg.WebPort) },
		SetValue: func(cfg *models.Config, raw string) error {
			p, err := strconv.Atoi(strings.TrimSpace(raw))
			if err != nil || p <= 0 || p >= 65536 {
				return fmt.Errorf("порт должен быть числом от 1 до 65535")
			}
			cfg.WebPort = p
			return nil
		},
	},
	{
		Key:        "web_lan",
		CLIKey:     "a",
		Label:      "Доступ к Web из локальной сети",
		ShortLabel: "Доступ к Web из сети",
		Hint:       "Разрешить открытие веб-интерфейса с других устройств в локальной сети (0.0.0.0)",
		Kind:       "bool",
		Modes:      []string{models.ModeCLI, models.ModeTUI, models.ModeWeb},
		Options: func(_ *models.Config, _ []string) []OptionSpec {
			return []OptionSpec{
				{Value: "false", Label: "Выкл (только этот ПК, 127.0.0.1)"},
				{Value: "true", Label: "Вкл (все устройства в сети, 0.0.0.0)"},
			}
		},
		GetValue: func(cfg *models.Config) string { return strconv.FormatBool(cfg.WebLAN) },
		GetDisplay: func(cfg *models.Config) string {
			if cfg.WebLAN {
				return "Вкл (0.0.0.0)"
			}
			return "Выкл (127.0.0.1)"
		},
		SetValue: func(cfg *models.Config, raw string) error {
			cfg.WebLAN = parseBool(raw)
			return nil
		},
	},
	{
		Key:        "default_duration",
		CLIKey:     "l",
		Label:      "Продолж. по умолчанию",
		ShortLabel: "Длительность (мин)",
		Hint:       "Длительность события при нажатии Enter (напр. 30, 45, 60 мин)",
		Kind:       "int",
		Modes:      []string{models.ModeCLI, models.ModeTUI, models.ModeWeb},
		GetValue:   func(cfg *models.Config) string { return strconv.Itoa(cfg.DefaultDuration) },
		GetDisplay: func(cfg *models.Config) string { return fmt.Sprintf("%d мин", cfg.DefaultDuration) },
		SetValue: func(cfg *models.Config, raw string) error {
			d, err := strconv.Atoi(strings.TrimSpace(raw))
			if err != nil || d <= 0 {
				return fmt.Errorf("длительность должна быть положительным числом")
			}
			cfg.DefaultDuration = d
			return nil
		},
	},
	{
		Key:        "duration_unit",
		CLIKey:     "f",
		Label:      "Формат длительности",
		ShortLabel: "Единицы длительности",
		Hint:       "Отображение продолжительности (в минутах или часах)",
		Kind:       "enum",
		Modes:      []string{models.ModeCLI, models.ModeTUI, models.ModeWeb},
		Options: func(_ *models.Config, _ []string) []OptionSpec {
			return []OptionSpec{
				{Value: "min", Label: DurationUnitLabels["min"]},
				{Value: "hours", Label: DurationUnitLabels["hours"]},
			}
		},
		GetValue: func(cfg *models.Config) string {
			if cfg.DurationUnit == "hours" {
				return "hours"
			}
			return "min"
		},
		GetDisplay: func(cfg *models.Config) string {
			if cfg.DurationUnit == "hours" {
				return DurationUnitLabels["hours"]
			}
			return DurationUnitLabels["min"]
		},
		SetValue: func(cfg *models.Config, raw string) error {
			if strings.TrimSpace(raw) == "hours" {
				cfg.DurationUnit = "hours"
			} else {
				cfg.DurationUnit = "min"
			}
			return nil
		},
	},
	{
		Key:        "default_type",
		CLIKey:     "t",
		Label:      "Тип по умолчанию",
		ShortLabel: "Категория по умолч.",
		Hint:       "Категория события, подставляемая по умолчанию",
		Kind:       "enum",
		Modes:      []string{models.ModeCLI, models.ModeTUI, models.ModeWeb},
		Options: func(cfg *models.Config, availableTypes []string) []OptionSpec {
			opts := []OptionSpec{{Value: "", Label: "(не выбран)"}}
			seen := map[string]bool{"": true}
			for _, t := range availableTypes {
				if !seen[t] {
					seen[t] = true
					opts = append(opts, OptionSpec{Value: t, Label: t})
				}
			}
			if cfg != nil && cfg.DefaultType != "" && !seen[cfg.DefaultType] {
				opts = append(opts, OptionSpec{Value: cfg.DefaultType, Label: cfg.DefaultType})
			}
			return opts
		},
		GetValue: func(cfg *models.Config) string { return cfg.DefaultType },
		GetDisplay: func(cfg *models.Config) string {
			if cfg.DefaultType == "" {
				return "(не выбран)"
			}
			return cfg.DefaultType
		},
		SetValue: func(cfg *models.Config, raw string) error {
			cfg.DefaultType = strings.TrimSpace(raw)
			if cfg.DefaultType != "" {
				cfg.AddCustomType(cfg.DefaultType)
			}
			return nil
		},
	},
	{
		Key:        "date_check_mode",
		CLIKey:     "c",
		Label:      "Проверка даты",
		ShortLabel: "Опечатки в дате",
		Hint:       "Реакция на несуществующие даты (напр. 32.01 -> 01.02)",
		Kind:       "enum",
		Modes:      []string{models.ModeCLI, models.ModeTUI},
		Options: func(_ *models.Config, _ []string) []OptionSpec {
			return []OptionSpec{
				{Value: models.DateCheckOff, Label: DateCheckLabels[models.DateCheckOff]},
				{Value: models.DateCheckAsk, Label: DateCheckLabels[models.DateCheckAsk]},
				{Value: models.DateCheckFix, Label: DateCheckLabels[models.DateCheckFix]},
				{Value: models.DateCheckReask, Label: DateCheckLabels[models.DateCheckReask]},
			}
		},
		GetValue:   func(cfg *models.Config) string { return cfg.DateCheckMode },
		GetDisplay: func(cfg *models.Config) string { return DateCheckLabels[cfg.DateCheckMode] },
		SetValue: func(cfg *models.Config, raw string) error {
			switch raw {
			case models.DateCheckOff, models.DateCheckAsk, models.DateCheckFix, models.DateCheckReask:
				cfg.DateCheckMode = raw
				return nil
			default:
				return fmt.Errorf("некорректный режим проверки дат: %s", raw)
			}
		},
	},
	{
		Key:        "ask_on_conflict",
		CLIKey:     "k",
		Label:      "Спрашивать при совпадении",
		ShortLabel: "Конфликты времени",
		Hint:       "Предупреждать ли, если на это же время уже есть запись",
		Kind:       "bool",
		Modes:      []string{models.ModeCLI, models.ModeTUI, models.ModeWeb},
		Options: func(_ *models.Config, _ []string) []OptionSpec {
			return []OptionSpec{
				{Value: "false", Label: "Выкл (добавлять сразу)"},
				{Value: "true", Label: "Вкл (спрашивать)"},
			}
		},
		GetValue: func(cfg *models.Config) string { return strconv.FormatBool(cfg.AskOnConflict) },
		GetDisplay: func(cfg *models.Config) string {
			if cfg.AskOnConflict {
				return "Вкл (спрашивать)"
			}
			return "Выкл (добавлять сразу)"
		},
		SetValue: func(cfg *models.Config, raw string) error {
			cfg.AskOnConflict = parseBool(raw)
			return nil
		},
	},
	{
		Key:        "silent_add_names",
		CLIKey:     "i",
		Label:      "Молча добавлять частые имена",
		ShortLabel: "Автосохранение имён",
		Hint:       "Автоматическое сохранение часто используемых имён без лишних вопросов",
		Kind:       "bool",
		Modes:      []string{models.ModeCLI},
		Options: func(_ *models.Config, _ []string) []OptionSpec {
			return []OptionSpec{
				{Value: "false", Label: "Выкл (спрашивать)"},
				{Value: "true", Label: "Вкл (добавлять молча)"},
			}
		},
		GetValue: func(cfg *models.Config) string { return strconv.FormatBool(cfg.SilentAddNames) },
		GetDisplay: func(cfg *models.Config) string {
			if cfg.SilentAddNames {
				return "Вкл (добавлять молча)"
			}
			return "Выкл (спрашивать)"
		},
		SetValue: func(cfg *models.Config, raw string) error {
			cfg.SilentAddNames = parseBool(raw)
			return nil
		},
	},
	{
		Key:        "split_mode",
		CLIKey:     "m",
		Label:      "Режим хранения",
		ShortLabel: "Хранение на диске",
		Hint:       "Способ сохранения текущего календаря (в одном файле, по годам или по месяцам)",
		Kind:       "enum",
		Modes:      []string{models.ModeCLI, models.ModeTUI, models.ModeWeb},
		Options: func(_ *models.Config, _ []string) []OptionSpec {
			return []OptionSpec{
				{Value: models.SplitNone, Label: SplitModeLabels[models.SplitNone]},
				{Value: models.SplitYear, Label: SplitModeLabels[models.SplitYear]},
				{Value: models.SplitMonth, Label: SplitModeLabels[models.SplitMonth]},
			}
		},
		GetValue: func(cfg *models.Config) string { return cfg.ActiveSplitMode() },
		GetDisplay: func(cfg *models.Config) string {
			if l, ok := SplitModeLabels[cfg.ActiveSplitMode()]; ok {
				return l
			}
			return SplitModeLabels[models.SplitNone]
		},
		SetValue: func(cfg *models.Config, raw string) error {
			switch raw {
			case models.SplitNone, models.SplitYear, models.SplitMonth:
				cfg.SetActiveSplitMode(raw)
				return nil
			default:
				return fmt.Errorf("некорректный режим хранения: %s", raw)
			}
		},
	},
	{
		Key:        "use_system_date",
		CLIKey:     "d",
		Label:      "Использовать системную дату",
		ShortLabel: "Системная дата",
		Hint:       "Использовать системное время ОС или фиксированную дату",
		Kind:       "bool",
		Modes:      []string{models.ModeCLI, models.ModeTUI, models.ModeWeb},
		Options: func(_ *models.Config, _ []string) []OptionSpec {
			return []OptionSpec{
				{Value: "true", Label: "Да (системные часы ОС)"},
				{Value: "false", Label: "Нет (фиксированная дата)"},
			}
		},
		GetValue:   func(cfg *models.Config) string { return strconv.FormatBool(cfg.UseSystemDate) },
		GetDisplay: FormatCustomDateDisplay,
		SetValue: func(cfg *models.Config, raw string) error {
			cfg.UseSystemDate = parseBool(raw)
			if !cfg.UseSystemDate && strings.TrimSpace(cfg.CustomDate) == "" {
				cfg.CustomDate = time.Now().Format("2006-01-02")
			}
			return nil
		},
	},
	{
		Key:        "custom_date",
		CLIKey:     "cd",
		Label:      "Фиксированная дата",
		ShortLabel: "Фиксированная дата",
		Hint:       "Дата при отключённых системных часах (ДД.ММ.ГГГГ или ГГГГ-ММ-ДД)",
		Kind:       "date",
		Modes:      []string{models.ModeCLI, models.ModeTUI, models.ModeWeb},
		GetValue:   func(cfg *models.Config) string { return cfg.CustomDate },
		GetDisplay: func(cfg *models.Config) string {
			if strings.TrimSpace(cfg.CustomDate) == "" {
				return parser.FormatDate(time.Now())
			}
			return parser.FormatDate(EffectiveToday(cfg))
		},
		SetValue: func(cfg *models.Config, raw string) error {
			trimmed := strings.TrimSpace(raw)
			if trimmed == "" {
				cfg.CustomDate = ""
				return nil
			}
			d, err := parser.ParseDate(trimmed, time.Now())
			if err != nil {
				return fmt.Errorf("некорректная дата: %w", err)
			}
			cfg.CustomDate = d.Format("2006-01-02")
			return nil
		},
	},
	{
		Key:        "data_path",
		CLIKey:     "p",
		Label:      "Путь к данным",
		ShortLabel: "Каталог записей",
		Hint:       "Каталог, где сохраняются json-файлы записей",
		Kind:       "path",
		Modes:      []string{models.ModeCLI, models.ModeTUI, models.ModeWeb},
		GetValue:   func(cfg *models.Config) string { return cfg.DataPath },
		GetDisplay: func(cfg *models.Config) string {
			return DataPathDisplay(cfg.DataPath)
		},
		SetValue: func(cfg *models.Config, raw string) error {
			norm := storage.NormalizeDataPath(raw)
			if norm == "" {
				cfg.DataPath = ""
				return nil
			}
			if err := storage.EnsureWritableDir(norm); err != nil {
				return err
			}
			cfg.DataPath = norm
			return nil
		},
	},
	{
		Key:        "data_file_name",
		CLIKey:     "n",
		Label:      "Имя файла данных",
		ShortLabel: "Имя файла базы",
		Hint:       "Базовое имя файла базы без расширения (по умолчанию \"mycal\")",
		Kind:       "string",
		Modes:      []string{models.ModeCLI, models.ModeTUI, models.ModeWeb},
		GetValue:   func(cfg *models.Config) string { return cfg.DataFileName },
		GetDisplay: func(cfg *models.Config) string { return cfg.DataFileName },
		SetValue: func(cfg *models.Config, raw string) error {
			clean := SanitizeBaseName(raw)
			if clean == "" {
				return fmt.Errorf("недопустимое имя файла")
			}
			cfg.DataFileName = clean
			return nil
		},
	},
	{
		Key:        "tui_default_period",
		CLIKey:     "e",
		Label:      "Период запуска в TUI",
		ShortLabel: "Период по умолч. в TUI",
		Hint:       "Основной период при запуске в TUI (Сегодня, Все записи, Неделя, Месяц)",
		Kind:       "enum",
		Modes:      []string{models.ModeTUI},
		Options: func(_ *models.Config, _ []string) []OptionSpec {
			return []OptionSpec{
				{Value: "today", Label: TUIDefaultPeriodLabels["today"]},
				{Value: "all", Label: TUIDefaultPeriodLabels["all"]},
				{Value: "week", Label: TUIDefaultPeriodLabels["week"]},
				{Value: "month", Label: TUIDefaultPeriodLabels["month"]},
			}
		},
		GetValue: func(cfg *models.Config) string {
			if cfg.TUIDefaultPeriod == "" {
				return "today"
			}
			return cfg.TUIDefaultPeriod
		},
		GetDisplay: func(cfg *models.Config) string {
			if l, ok := TUIDefaultPeriodLabels[cfg.TUIDefaultPeriod]; ok {
				return l
			}
			return TUIDefaultPeriodLabels["today"]
		},
		SetValue: func(cfg *models.Config, raw string) error {
			switch strings.TrimSpace(raw) {
			case "today", "day", "":
				cfg.TUIDefaultPeriod = "today"
			case "all", "week", "month":
				cfg.TUIDefaultPeriod = strings.TrimSpace(raw)
			default:
				return fmt.Errorf("некорректный период TUI: %s", raw)
			}
			return nil
		},
	},
	{
		Key:        "show_hints",
		CLIKey:     "h",
		Label:      "Подсказки",
		ShortLabel: "Подсказки в меню",
		Hint:       "Пояснения и примеры под каждым пунктом меню и действием",
		Kind:       "bool",
		Modes:      []string{models.ModeCLI},
		Options: func(_ *models.Config, _ []string) []OptionSpec {
			return []OptionSpec{
				{Value: "false", Label: "Выкл"},
				{Value: "true", Label: "Вкл"},
			}
		},
		GetValue: func(cfg *models.Config) string { return strconv.FormatBool(cfg.ShowHints) },
		GetDisplay: func(cfg *models.Config) string {
			if cfg.ShowHints {
				return "Вкл"
			}
			return "Выкл"
		},
		SetValue: func(cfg *models.Config, raw string) error {
			cfg.ShowHints = parseBool(raw)
			return nil
		},
	},
	{
		Key:        "theme",
		CLIKey:     "th",
		Label:      "Тема оформления (стиль)",
		ShortLabel: "Тема",
		Hint:       "Визуальный стиль интерфейса (Tokyo, Nord, Dracula, Monokai, Solarized, Cyberpunk, Matrix, Paper)",
		Kind:       "enum",
		Modes:      []string{models.ModeCLI, models.ModeTUI, models.ModeWeb},
		Options: func(_ *models.Config, _ []string) []OptionSpec {
			return []OptionSpec{
				{Value: "tokyo", Label: "Tokyo Night (по умолч.)"},
				{Value: "nord", Label: "Nord Frost"},
				{Value: "dracula", Label: "Dracula"},
				{Value: "monokai", Label: "Monokai Pro"},
				{Value: "solarized", Label: "Solarized Dark"},
				{Value: "cyberpunk", Label: "Cyberpunk Neon"},
				{Value: "matrix", Label: "Matrix Emerald"},
				{Value: "catppuccin", Label: "Catppuccin Mocha"},
				{Value: "paper", Label: "Paper Light"},
			}
		},
		GetValue: func(cfg *models.Config) string {
			if cfg.Theme == "" {
				return "tokyo"
			}
			return cfg.Theme
		},
		GetDisplay: func(cfg *models.Config) string {
			switch cfg.Theme {
			case "nord":
				return "Nord Frost"
			case "dracula":
				return "Dracula"
			case "monokai":
				return "Monokai Pro"
			case "solarized":
				return "Solarized Dark"
			case "cyberpunk":
				return "Cyberpunk Neon"
			case "matrix":
				return "Matrix Emerald"
			case "catppuccin":
				return "Catppuccin Mocha"
			case "paper":
				return "Paper Light"
			default:
				return "Tokyo Night"
			}
		},
		SetValue: func(cfg *models.Config, raw string) error {
			val := strings.ToLower(strings.TrimSpace(raw))
			switch val {
			case "tokyo", "nord", "dracula", "monokai", "solarized", "cyberpunk", "matrix", "catppuccin", "paper":
				cfg.Theme = val
				return nil
			default:
				return fmt.Errorf("неизвестная тема %q, допустимые: tokyo, nord, dracula, monokai, solarized, cyberpunk, matrix, catppuccin, paper", raw)
			}
		},
	},
	{
		Key:        "visible_fields",
		CLIKey:     "vf",
		Label:      "Отображаемые поля",
		ShortLabel: "Поля записей",
		Hint:       "Список отображаемых и запрашиваемых полей через ';' (Дата; Время; Наименование; Категория; Длительность; Статус; Кэш; Заметка)",
		Kind:       "string",
		Modes:      []string{models.ModeCLI, models.ModeTUI, models.ModeWeb},
		GetValue: func(cfg *models.Config) string {
			if strings.TrimSpace(cfg.VisibleFields) == "" {
				return "Дата; Время; Наименование; Категория; Длительность; Статус; Кэш; Заметка"
			}
			return cfg.VisibleFields
		},
		GetDisplay: func(cfg *models.Config) string {
			if strings.TrimSpace(cfg.VisibleFields) == "" {
				return "Все поля (по умолч.)"
			}
			return cfg.VisibleFields
		},
		SetValue: func(cfg *models.Config, raw string) error {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				cfg.VisibleFields = "Дата; Время; Наименование; Категория; Длительность; Статус; Кэш; Заметка"
				return nil
			}
			cfg.VisibleFields = raw
			return nil
		},
	},
}

// FindSetting looks up a SettingSpec by its JSON key.
func FindSetting(key string) (SettingSpec, bool) {
	for _, s := range SettingsSchema {
		if s.Key == key {
			return s, true
		}
	}
	return SettingSpec{}, false
}

// SettingsForMode returns all SettingSpecs that are enabled for the given mode
// according to cfg.IsSettingForMode(spec.Key, mode).
func SettingsForMode(cfg *models.Config, mode string) []SettingSpec {
	var out []SettingSpec
	for _, spec := range SettingsSchema {
		if cfg != nil {
			if cfg.IsSettingForMode(spec.Key, mode) {
				out = append(out, spec)
			}
		} else {
			for _, m := range spec.Modes {
				if strings.EqualFold(m, mode) {
					out = append(out, spec)
					break
				}
			}
		}
	}
	return out
}

// CycleSetting advances an enum/bool setting to its next option and returns the new value.
func CycleSetting(cfg *models.Config, key string, availableTypes []string) (string, error) {
	spec, ok := FindSetting(key)
	if !ok || spec.Options == nil {
		return "", fmt.Errorf("настройка %s не поддерживает перебор", key)
	}
	opts := spec.Options(cfg, availableTypes)
	if len(opts) == 0 {
		return "", nil
	}
	cur := spec.GetValue(cfg)
	nextVal := opts[0].Value
	for i, o := range opts {
		if o.Value == cur {
			nextVal = opts[(i+1)%len(opts)].Value
			break
		}
	}
	if err := spec.SetValue(cfg, nextVal); err != nil {
		return "", err
	}
	return nextVal, nil
}

func parseBool(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true", "1", "yes", "да", "вкл", "on":
		return true
	default:
		return false
	}
}
