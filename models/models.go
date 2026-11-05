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

// DefaultConfig is the single source of truth for configuration defaults.
var DefaultConfig = Config{
	DefaultDuration: 60,
	SplitMode:       SplitNone,
	DataFileName:    "mycal",
	DateCheckMode:   DateCheckOff,
	UseSystemDate:   true,
	AskOnConflict:   false,
	ShowHints:       false,
	DefaultMode:     ModeCLI,
	WebPort:         8080,
	SortField:       "date",
	SortAsc:         false,
	DurationUnit:    "min",
	TUIDefaultPeriod: "today",
}

// Config mirrors config_mycal.json.
type Config struct {
	DefaultDuration  int      `json:"default_duration"`
	DefaultType      string   `json:"default_type"`
	DataPath         string   `json:"data_path"`
	SplitMode        string   `json:"split_mode"`
	DataFileName     string   `json:"data_file_name"`
	DateCheckMode    string   `json:"date_check_mode"`
	UseSystemDate    bool     `json:"use_system_date"`
	CustomDate       string   `json:"custom_date"`
	CustomStatuses   []string `json:"custom_statuses"`
	CustomNames      []string `json:"custom_names"`
	CustomTypes      []string `json:"custom_types,omitempty"`
	ExcludedTypes    []string `json:"excluded_types,omitempty"`
	ExcludedStatuses []string `json:"excluded_statuses,omitempty"`
	SilentAddNames   bool     `json:"silent_add_names"`
	AskOnConflict    bool     `json:"ask_on_conflict"`
	ShowHints        bool     `json:"show_hints"`
	DefaultMode      string   `json:"default_mode"`
	WebPort          int      `json:"web_port"`
	SortField        string   `json:"sort_field,omitempty"`
	SortAsc          bool     `json:"sort_asc,omitempty"`
	DurationUnit     string   `json:"duration_unit,omitempty"`
	TUIDefaultPeriod string   `json:"tui_default_period,omitempty"`
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
}

// Calendar is the on-disk document: a flat, self-contained list of sessions.
// (There is no date grouping on disk; grouping is a display concern.)
type Calendar struct {
	Sessions []Session `json:"sessions"`
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

// IsRepeat reports whether the session belongs to a repeating series.
func (s Session) IsRepeat() bool { return s.SeriesID != "" }
