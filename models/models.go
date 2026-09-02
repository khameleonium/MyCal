// Package models holds the persistent data types shared across the calendar.
package models

import "encoding/json"

// IDLen is the length of a session ID (YYYYMMDDHHMMnn, minute precision plus a
// two-digit per-minute counter). IDs may be longer if a minute somehow holds
// more than 100 entries; code compares against IDLen with >=.
const IDLen = 14

// SplitMode selects how data files are laid out on disk.
const (
	SplitNone  = "none"  // one file: <base>.json
	SplitYear  = "year"  // YYYY_<base>.json
	SplitMonth = "month" // YYYY-MM_<base>.json
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
}

// Config mirrors config_mycal.json.
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
}

// Calendar is the on-disk document: a flat, self-contained list of sessions.
// (There is no date grouping on disk; grouping is a display concern.)
type Calendar struct {
	Sessions []Session `json:"sessions"`
}

// Session is one calendar entry. Every field is stored explicitly — occurrences
// of a repeating series are ordinary sessions that happen to share a SeriesID.
type Session struct {
	ID       string `json:"id"`
	Time     string `json:"time"` // "HH:MM"
	Name     string `json:"name"`
	Type     string `json:"type"`
	Duration int    `json:"duration"` // minutes
	Notes    string `json:"notes"`
	Status   string `json:"status"`
	SeriesID string `json:"series_id,omitempty"`
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
