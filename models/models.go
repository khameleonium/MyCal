package models

import (
	"encoding/json"
)

// IDLen is the length of a canonical session ID (YYYYMMDDHHMMSS).
const IDLen = 14

// RepeatSuffix marks the original session of a repeating series. Child
// occurrences store this original ID (suffix included) in their Name field.
const RepeatSuffix = "_r"

// SplitMode constants.
const (
	SplitNone  = "none"
	SplitYear  = "year"
	SplitMonth = "month"
)

// DateCheckMode constants.
const (
	DateCheckOff   = ""
	DateCheckAsk   = "ask"
	DateCheckFix   = "fix"
	DateCheckReask = "reask"
)

// DefaultConfig is the single source of truth for configuration defaults.
// Both config.Load (missing file) and Config.UnmarshalJSON (missing fields)
// derive their defaults from this value.
var DefaultConfig = Config{
	DefaultDuration: 60,
	SplitMode:       SplitNone,
	DataFileName:    "mycal",
	DateCheckMode:   DateCheckOff,
	UseSystemDate:   true,
}

// Config represents the config_mycal.json file structure.
type Config struct {
	DefaultDuration int      `json:"default_duration"`
	DefaultType     string   `json:"default_type"`
	DataPath        string   `json:"data_path"`
	SplitMode       string   `json:"split_mode"`
	DataFileName    string   `json:"data_file_name"`
	DateCheckMode   string   `json:"date_check_mode"`
	UseSystemDate   bool     `json:"use_system_date"`
	CustomDate      string   `json:"custom_date"`
	CustomStatuses  []string `json:"custom_statuses"`
	CustomNames     []string `json:"custom_names"`
	SilentAddNames  bool     `json:"silent_add_names"`
}

// UnmarshalJSON fills the receiver from DefaultConfig, then overlays whatever
// keys are present in data. The type alias avoids infinite recursion and keeps
// a single field list (the previous implementation duplicated every field into
// a shadow struct that had to be kept in sync by hand).
func (c *Config) UnmarshalJSON(data []byte) error {
	type alias Config
	tmp := alias(DefaultConfig)
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}
	*c = Config(tmp)
	c.ApplyDefaults()
	return nil
}

// ApplyDefaults repairs values that are invalid rather than merely absent
// (e.g. a non-positive duration or an empty split mode written by an older
// version).
func (c *Config) ApplyDefaults() {
	if c.DefaultDuration <= 0 {
		c.DefaultDuration = DefaultConfig.DefaultDuration
	}
	if c.SplitMode == "" {
		c.SplitMode = DefaultConfig.SplitMode
	}
	if c.DataFileName == "" {
		c.DataFileName = DefaultConfig.DataFileName
	}
}

// Calendar represents the top-level structure of the data file(s).
type Calendar struct {
	Entries []DateEntry `json:"my_calendar"`
}

// DateEntry groups sessions under a single date.
type DateEntry struct {
	Date     string    `json:"date"`
	Sessions []Session `json:"session"`
}

// Session represents a single calendar entry.
type Session struct {
	ID       string `json:"id"`
	Time     string `json:"time"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Duration int    `json:"duration"`
	Notes    string `json:"notes"`
	Status   string `json:"status"`

	// IsRepeat and OriginalID are runtime-only fields populated by the
	// calendar service when a session belongs to a repeating series.
	IsRepeat   bool   `json:"-"`
	OriginalID string `json:"-"`
}

// Date extracts the date from the session ID in YYYY-MM-DD format.
func (s Session) Date() string {
	if len(s.ID) >= IDLen {
		return s.ID[0:4] + "-" + s.ID[4:6] + "-" + s.ID[6:8]
	}
	return ""
}

// IsSeriesOriginal reports whether this session is the template of a repeating
// series (its own ID carries the repeat suffix).
func (s Session) IsSeriesOriginal() bool {
	return HasRepeatSuffix(s.ID)
}

// SeriesRef returns the original series ID referenced by a child occurrence,
// or "" if this session is not a child occurrence.
func (s Session) SeriesRef() string {
	if HasRepeatSuffix(s.Name) {
		return s.Name
	}
	return ""
}

// HasRepeatSuffix reports whether v ends with RepeatSuffix.
func HasRepeatSuffix(v string) bool {
	return len(v) > len(RepeatSuffix) && v[len(v)-len(RepeatSuffix):] == RepeatSuffix
}
