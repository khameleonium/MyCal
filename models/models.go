// Package models holds the persistent data types shared across the calendar.
package models

import (
	"encoding/json"
	"strings"
)

// SplitMode selects how data files are laid out on disk.
const (
	SplitNone  = "none"  // one file: <base>.json
	SplitYear  = "year"  // YYYY_<base>.json
	SplitMonth = "month" // YYYY-MM_<base>.json
)

// Launch modes for mycal.
const (
	ModeCLI = "cli"
	ModeTUI = "tui"
	ModeWeb = "web"
)

// DateCheckMode selects what happens when a typed date had to be normalised
// (e.g. "32-01-2026" -> "01-02-2026").
const (
	DateCheckOff   = ""
	DateCheckAsk   = "ask"
	DateCheckFix   = "fix"
	DateCheckReask = "reask"
)

// DefaultSettingModes returns the canonical mapping of setting keys to the
// interfaces (modes) where each setting is applicable and displayed.
func DefaultSettingModes() map[string][]string {
	return map[string][]string{
		"default_mode":       {ModeCLI, ModeTUI, ModeWeb},
		"web_port":           {ModeCLI, ModeTUI, ModeWeb},
		"web_lan":            {ModeCLI, ModeTUI, ModeWeb},
		"default_duration":   {ModeCLI, ModeTUI, ModeWeb},
		"duration_unit":      {ModeCLI, ModeTUI, ModeWeb},
		"default_type":       {ModeCLI, ModeTUI, ModeWeb},
		"data_path":          {ModeCLI, ModeTUI, ModeWeb},
		"data_file_name":     {ModeCLI, ModeTUI, ModeWeb},
		"split_mode":         {ModeCLI, ModeTUI, ModeWeb},
		"ask_on_conflict":    {ModeCLI, ModeTUI, ModeWeb},
		"use_system_date":    {ModeCLI, ModeTUI, ModeWeb},
		"custom_date":        {ModeCLI, ModeTUI, ModeWeb},
		"date_check_mode":    {ModeCLI, ModeTUI},
		"tui_default_period": {ModeTUI},
		"sort_field":         {ModeTUI},
		"sort_asc":           {ModeTUI},
		"show_hints":         {ModeCLI},
		"silent_add_names":   {ModeCLI},
		"language":           {ModeCLI, ModeTUI, ModeWeb},
		"locale_path":        {ModeCLI, ModeTUI, ModeWeb},
	}
}

// DefaultConfig is the single source of truth for configuration defaults.
var DefaultConfig = Config{
	DefaultDuration:  60,
	SplitMode:        SplitNone,
	DataFileName:     "mycal",
	DateCheckMode:    DateCheckOff,
	UseSystemDate:    true,
	AskOnConflict:    false,
	ShowHints:        false,
	DefaultMode:      ModeCLI,
	WebPort:          8080,
	WebLAN:           false,
	SortField:        "date",
	SortAsc:          false,
	DurationUnit:     "min",
	TUIDefaultPeriod: "today",
	ActiveCalendar:   "",
	CalendarSplits:   map[string]string{},
	SettingModes:     DefaultSettingModes(),
	Theme:            "tokyo",
	VisibleFields:    "Дата; Время; Наименование; Категория; Длительность; Статус; Кэш; Заметка",
	Language:         "ru",
	LocalePath:       "",
}

// CalendarInfo describes one calendar discovered on disk or configured in settings.
type CalendarInfo struct {
	Key       string `json:"key"`        // "" for default calendar, or suffix after "--" (e.g. "work")
	Label     string `json:"label"`      // human-readable label (e.g. "Основной (mycal)" or "work (mycal--work)")
	BaseName  string `json:"base_name"`  // effective base filename before .json (e.g. "mycal" or "mycal--work")
	SplitMode string `json:"split_mode"` // "none", "year", "month"
	Active    bool   `json:"active"`     // true if currently active
}

// Config mirrors config_mycal.json.
type Config struct {
	DefaultDuration  int                 `json:"default_duration"`
	DefaultType      string              `json:"default_type"`
	DataPath         string              `json:"data_path"`
	SplitMode        string              `json:"split_mode"`
	DataFileName     string              `json:"data_file_name"`
	DateCheckMode    string              `json:"date_check_mode"`
	UseSystemDate    bool                `json:"use_system_date"`
	CustomDate       string              `json:"custom_date"`
	CustomStatuses   []string            `json:"custom_statuses"`
	CustomNames      []string            `json:"custom_names"`
	CustomTypes      []string            `json:"custom_types,omitempty"`
	ExcludedTypes    []string            `json:"excluded_types,omitempty"`
	ExcludedStatuses []string            `json:"excluded_statuses,omitempty"`
	SilentAddNames   bool                `json:"silent_add_names"`
	AskOnConflict    bool                `json:"ask_on_conflict"`
	ShowHints        bool                `json:"show_hints"`
	DefaultMode      string              `json:"default_mode"`
	WebPort          int                 `json:"web_port"`
	WebLAN           bool                `json:"web_lan"`
	SortField        string              `json:"sort_field"`
	SortAsc          bool                `json:"sort_asc"`
	DurationUnit     string              `json:"duration_unit"`
	TUIDefaultPeriod string              `json:"tui_default_period"`
	ActiveCalendar   string              `json:"active_calendar"`
	CalendarSplits   map[string]string   `json:"calendar_splits"`
	SettingModes     map[string][]string `json:"setting_modes"`
	Theme            string              `json:"theme,omitempty"`
	VisibleFields    string              `json:"visible_fields,omitempty"`
	Language         string              `json:"language,omitempty"`
	LocalePath       string              `json:"locale_path,omitempty"`
}

// SanitizeBaseName cleans a filename base, removing path separators, ".json",
// and reserved "--" sequences.
func SanitizeBaseName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, ".json")
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|', 0:
		default:
			b.WriteRune(r)
		}
	}
	res := strings.Trim(b.String(), ". ")
	for strings.Contains(res, "--") {
		res = strings.ReplaceAll(res, "--", "-")
	}
	return strings.Trim(res, "- ")
}

// SanitizeCalendarKey cleans a calendar suffix/prefix identifier so it can be
// safely used in `<base>--<key>.json`.
func SanitizeCalendarKey(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, ".json")
	s = strings.TrimPrefix(s, "--")
	s = strings.TrimSuffix(s, "--")
	return SanitizeBaseName(s)
}

// EffectiveBaseName returns the on-disk base file name for the given calendar key.
// For the default calendar (""), it returns DataFileName (e.g. "mycal").
// For a named calendar ("work"), it returns DataFileName + "--" + calKey (e.g. "mycal--work").
func (c *Config) EffectiveBaseName(calKey string) string {
	base := "mycal"
	if c != nil && strings.TrimSpace(c.DataFileName) != "" {
		base = strings.TrimSpace(c.DataFileName)
	}
	calKey = strings.TrimSpace(calKey)
	if calKey == "" {
		return base
	}
	return base + "--" + calKey
}

// ActiveEffectiveBaseName returns the on-disk base file name for the active calendar.
func (c *Config) ActiveEffectiveBaseName() string {
	if c == nil {
		return "mycal"
	}
	return c.EffectiveBaseName(c.ActiveCalendar)
}

// GetCalendarSplitMode returns the configured split mode for the given calendar key.
func (c *Config) GetCalendarSplitMode(calKey string) string {
	if c == nil {
		return SplitNone
	}
	calKey = strings.TrimSpace(calKey)
	if c.CalendarSplits != nil {
		if m, ok := c.CalendarSplits[calKey]; ok {
			switch m {
			case SplitNone, SplitYear, SplitMonth:
				return m
			}
		}
	}
	if calKey == "" {
		switch c.SplitMode {
		case SplitNone, SplitYear, SplitMonth:
			return c.SplitMode
		}
	}
	return SplitNone
}

// ActiveSplitMode returns the split mode of the currently active calendar.
func (c *Config) ActiveSplitMode() string {
	if c == nil {
		return SplitNone
	}
	return c.GetCalendarSplitMode(c.ActiveCalendar)
}

// SetCalendarSplitMode sets the split mode for a specific calendar key.
// For the default calendar (""), it also updates SplitMode.
func (c *Config) SetCalendarSplitMode(calKey, mode string) {
	if c == nil {
		return
	}
	calKey = strings.TrimSpace(calKey)
	switch mode {
	case SplitNone, SplitYear, SplitMonth:
	default:
		mode = SplitNone
	}
	if c.CalendarSplits == nil {
		c.CalendarSplits = make(map[string]string)
	}
	c.CalendarSplits[calKey] = mode
	if calKey == "" {
		c.SplitMode = mode
	}
}

// SetActiveSplitMode sets the split mode for the currently active calendar.
func (c *Config) SetActiveSplitMode(mode string) {
	if c == nil {
		return
	}
	c.SetCalendarSplitMode(c.ActiveCalendar, mode)
}

// CalendarDisplayName returns a human-readable display title for the given calendar key.
func (c *Config) CalendarDisplayName(calKey string) string {
	calKey = strings.TrimSpace(calKey)
	base := c.EffectiveBaseName(calKey)
	if calKey == "" {
		return "Основной (" + base + ")"
	}
	return calKey + " (" + base + ")"
}

// ActiveCalendarDisplayName returns a human-readable display title for the active calendar.
func (c *Config) ActiveCalendarDisplayName() string {
	if c == nil {
		return "Основной (mycal)"
	}
	return c.CalendarDisplayName(c.ActiveCalendar)
}

// IsSettingForMode reports whether settingKey should be shown in the given interface mode.
func (c *Config) IsSettingForMode(settingKey, mode string) bool {
	if mode == "" {
		return true
	}
	var modes []string
	if c != nil && c.SettingModes != nil {
		modes = c.SettingModes[settingKey]
	}
	if len(modes) == 0 {
		modes = DefaultSettingModes()[settingKey]
	}
	if len(modes) == 0 {
		return true
	}
	for _, m := range modes {
		if strings.EqualFold(m, mode) || strings.EqualFold(m, "all") {
			return true
		}
	}
	return false
}

// AddCustomName adds a frequent name to CustomNames if not already present.
func (c *Config) AddCustomName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, cn := range c.CustomNames {
		if strings.EqualFold(cn, name) {
			return false
		}
	}
	c.CustomNames = append(c.CustomNames, name)
	return true
}

// AddCustomType adds a custom type and ensures it is not in ExcludedTypes.
func (c *Config) AddCustomType(t string) {
	t = strings.TrimSpace(t)
	if t == "" {
		return
	}
	// Remove from ExcludedTypes
	var newExcluded []string
	for _, ex := range c.ExcludedTypes {
		if !strings.EqualFold(ex, t) {
			newExcluded = append(newExcluded, ex)
		}
	}
	c.ExcludedTypes = newExcluded

	// Add to CustomTypes if not present
	for _, ct := range c.CustomTypes {
		if strings.EqualFold(ct, t) {
			return
		}
	}
	c.CustomTypes = append(c.CustomTypes, t)
}

// RemoveType removes a type from CustomTypes and marks it in ExcludedTypes.
func (c *Config) RemoveType(t string) {
	t = strings.TrimSpace(t)
	if t == "" {
		return
	}
	var newCustom []string
	for _, ct := range c.CustomTypes {
		if !strings.EqualFold(ct, t) {
			newCustom = append(newCustom, ct)
		}
	}
	c.CustomTypes = newCustom

	for _, ex := range c.ExcludedTypes {
		if strings.EqualFold(ex, t) {
			return
		}
	}
	c.ExcludedTypes = append(c.ExcludedTypes, t)
}

// AddCustomStatus adds a custom status and ensures it is not in ExcludedStatuses.
func (c *Config) AddCustomStatus(s string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return
	}
	var newExcluded []string
	for _, ex := range c.ExcludedStatuses {
		if !strings.EqualFold(ex, s) {
			newExcluded = append(newExcluded, ex)
		}
	}
	c.ExcludedStatuses = newExcluded

	for _, cs := range c.CustomStatuses {
		if strings.EqualFold(cs, s) {
			return
		}
	}
	c.CustomStatuses = append(c.CustomStatuses, s)
}

// RemoveStatus removes a status from CustomStatuses and marks it in ExcludedStatuses.
func (c *Config) RemoveStatus(s string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return
	}
	var newCustom []string
	for _, cs := range c.CustomStatuses {
		if !strings.EqualFold(cs, s) {
			newCustom = append(newCustom, cs)
		}
	}
	c.CustomStatuses = newCustom

	for _, ex := range c.ExcludedStatuses {
		if strings.EqualFold(ex, s) {
			return
		}
	}
	c.ExcludedStatuses = append(c.ExcludedStatuses, s)
}

// UnmarshalJSON overlays the JSON document onto a copy of DefaultConfig, so any
// key the user omitted keeps its default and the field list lives in one place.
func (c *Config) UnmarshalJSON(data []byte) error {
	type alias Config
	tmp := alias(DefaultConfig)
	tmp.SettingModes = nil
	tmp.CalendarSplits = nil
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}
	*c = Config(tmp)
	c.Normalize()
	return nil
}

// Normalize repairs values that are present but invalid.
func (c *Config) Normalize() {
	if c.DefaultDuration <= 0 {
		c.DefaultDuration = DefaultConfig.DefaultDuration
	}
	switch c.SplitMode {
	case SplitNone, SplitYear, SplitMonth:
	default:
		c.SplitMode = DefaultConfig.SplitMode
	}
	if c.DataFileName == "" {
		c.DataFileName = DefaultConfig.DataFileName
	}
	switch c.DateCheckMode {
	case DateCheckOff, DateCheckAsk, DateCheckFix, DateCheckReask:
	default:
		c.DateCheckMode = DateCheckOff
	}
	switch c.DefaultMode {
	case ModeCLI, ModeTUI, ModeWeb:
	default:
		c.DefaultMode = ModeCLI
	}
	if c.WebPort <= 0 {
		c.WebPort = 8080
	}
	if c.SortField == "" {
		c.SortField = DefaultConfig.SortField
	}
	switch c.DurationUnit {
	case "hours", "min":
	default:
		c.DurationUnit = "min"
	}
	switch c.TUIDefaultPeriod {
	case "today", "day", "all", "week", "month":
	default:
		c.TUIDefaultPeriod = "today"
	}
	c.ActiveCalendar = strings.TrimSpace(c.ActiveCalendar)
	if c.CalendarSplits == nil {
		c.CalendarSplits = make(map[string]string)
	}
	for k, v := range c.CalendarSplits {
		switch v {
		case SplitNone, SplitYear, SplitMonth:
		default:
			c.CalendarSplits[k] = SplitNone
		}
	}
	defModes := DefaultSettingModes()
	if c.SettingModes == nil {
		c.SettingModes = defModes
	} else {
		for k, v := range defModes {
			if len(c.SettingModes[k]) == 0 {
				c.SettingModes[k] = v
			}
		}
	}
}

// CalendarSettings defines per-calendar overrides for visual, behavioral,
// and default session attributes stored directly in the calendar data file.
// Any non-nil / non-empty field overrides the corresponding global Config setting.
type CalendarSettings struct {
	VisibleFields    *string  `json:"visible_fields,omitempty"`
	DefaultDuration  *int     `json:"default_duration,omitempty"`
	DurationUnit     *string  `json:"duration_unit,omitempty"`
	DefaultType      *string  `json:"default_type,omitempty"`
	CustomStatuses   []string `json:"custom_statuses,omitempty"`
	CustomTypes      []string `json:"custom_types,omitempty"`
	Theme            *string  `json:"theme,omitempty"`
	SortField        *string  `json:"sort_field,omitempty"`
	SortAsc          *bool    `json:"sort_asc,omitempty"`
	TUIDefaultPeriod *string  `json:"tui_default_period,omitempty"`
	DateCheckMode    *string  `json:"date_check_mode,omitempty"`
	AskOnConflict    *bool    `json:"ask_on_conflict,omitempty"`
	SilentAddNames   *bool    `json:"silent_add_names,omitempty"`
}

// Clone returns a deep copy of CalendarSettings.
func (s *CalendarSettings) Clone() *CalendarSettings {
	if s == nil {
		return nil
	}
	cp := *s
	if s.CustomStatuses != nil {
		cp.CustomStatuses = append([]string(nil), s.CustomStatuses...)
	}
	if s.CustomTypes != nil {
		cp.CustomTypes = append([]string(nil), s.CustomTypes...)
	}
	return &cp
}

// IsEmpty reports whether no setting override is configured.
func (s *CalendarSettings) IsEmpty() bool {
	if s == nil {
		return true
	}
	return s.VisibleFields == nil &&
		s.DefaultDuration == nil &&
		s.DurationUnit == nil &&
		s.DefaultType == nil &&
		len(s.CustomStatuses) == 0 &&
		len(s.CustomTypes) == 0 &&
		s.Theme == nil &&
		s.SortField == nil &&
		s.SortAsc == nil &&
		s.TUIDefaultPeriod == nil &&
		s.DateCheckMode == nil &&
		s.AskOnConflict == nil &&
		s.SilentAddNames == nil
}

// Calendar is the on-disk document: an optional block of calendar-specific
// settings and a flat, self-contained list of sessions.
type Calendar struct {
	Settings *CalendarSettings `json:"settings,omitempty"`
	Sessions []Session         `json:"sessions"`
}

// EffectiveConfig returns a merged Config where non-nil CalendarSettings
// override the base global Config values.
func (c *Calendar) EffectiveConfig(global *Config) *Config {
	if global == nil {
		global = &DefaultConfig
	}
	res := *global
	if c == nil || c.Settings == nil {
		return &res
	}
	s := c.Settings
	if s.VisibleFields != nil && strings.TrimSpace(*s.VisibleFields) != "" {
		res.VisibleFields = strings.TrimSpace(*s.VisibleFields)
	}
	if s.DefaultDuration != nil && *s.DefaultDuration >= 0 {
		res.DefaultDuration = *s.DefaultDuration
	}
	if s.DurationUnit != nil && *s.DurationUnit != "" {
		res.DurationUnit = *s.DurationUnit
	}
	if s.DefaultType != nil {
		res.DefaultType = *s.DefaultType
	}
	if len(s.CustomStatuses) > 0 {
		res.CustomStatuses = append([]string(nil), s.CustomStatuses...)
	}
	if len(s.CustomTypes) > 0 {
		res.CustomTypes = append([]string(nil), s.CustomTypes...)
	}
	if s.Theme != nil && *s.Theme != "" {
		res.Theme = *s.Theme
	}
	if s.SortField != nil && *s.SortField != "" {
		res.SortField = *s.SortField
	}
	if s.SortAsc != nil {
		res.SortAsc = *s.SortAsc
	}
	if s.TUIDefaultPeriod != nil && *s.TUIDefaultPeriod != "" {
		res.TUIDefaultPeriod = *s.TUIDefaultPeriod
	}
	if s.DateCheckMode != nil && *s.DateCheckMode != "" {
		res.DateCheckMode = *s.DateCheckMode
	}
	if s.AskOnConflict != nil {
		res.AskOnConflict = *s.AskOnConflict
	}
	if s.SilentAddNames != nil {
		res.SilentAddNames = *s.SilentAddNames
	}
	return &res
}

// Session is one calendar entry. Every field is stored explicitly — occurrences
// of a repeating series are ordinary sessions that happen to share a SeriesID.
type Session struct {
	ID       string  `json:"id"`
	Time     string  `json:"time"` // "HH:MM"
	Name     string  `json:"name"`
	Type     string  `json:"type"`
	Duration int     `json:"duration"` // minutes
	Notes    string  `json:"notes"`
	Status   string  `json:"status"`
	SeriesID string  `json:"series_id,omitempty"`
	Cash     float64 `json:"cash"`
}

// Date returns the YYYY-MM-DD encoded in the ID, or "" if the ID is too short.
func (s Session) Date() string {
	if len(s.ID) >= 10 {
		return s.ID[0:4] + "-" + s.ID[4:6] + "-" + s.ID[6:8]
	}
	return ""
}

// DateKey returns the zero-allocation YYYYMMDD prefix of the ID, or "" if too short.
// Its lexicographical order is identical to Date().
func (s Session) DateKey() string {
	if len(s.ID) >= 10 {
		return s.ID[0:8]
	}
	return ""
}

// IsRepeat reports whether the session belongs to a repeating series.
func (s Session) IsRepeat() bool { return s.SeriesID != "" }
