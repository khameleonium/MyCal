package models

import (
	"encoding/json"
)

const IDLen = 14

// SplitMode constants.
const (
	SplitNone  = "none"
	SplitYear  = "year"
	SplitMonth = "month"
)

// DateCheckMode constants.
const (
	DateCheckOff  = ""
	DateCheckAsk  = "ask"
	DateCheckFix  = "fix"
	DateCheckReask = "reask"
)

// Config represents the config_mycal.json file structure.
type Config struct {
	DefaultDuration int    `json:"default_duration"`
	DefaultType     string `json:"default_type"`
	DataPath        string `json:"data_path"`
	SplitMode       string `json:"split_mode"`
	DataFileName    string `json:"data_file_name"`
	DateCheckMode   string `json:"date_check_mode"`
	UseSystemDate   bool   `json:"use_system_date"`
	CustomDate      string   `json:"custom_date"`
	CustomStatuses  []string `json:"custom_statuses"`
	CustomNames     []string `json:"custom_names"`
	SilentAddNames  bool     `json:"silent_add_names"`
}

// UnmarshalJSON implements custom unmarshalling with defaults.
func (c *Config) UnmarshalJSON(data []byte) error {
	type rawConfig struct {
		DefaultDuration int    `json:"default_duration"`
		DefaultType     string `json:"default_type"`
		DataPath        string `json:"data_path"`
		SplitMode       string `json:"split_mode"`
		DataFileName    string `json:"data_file_name"`
		DateCheckMode   string `json:"date_check_mode"`
		UseSystemDate   *bool    `json:"use_system_date"`
		CustomDate      string   `json:"custom_date"`
		CustomStatuses  []string `json:"custom_statuses"`
		CustomNames     []string `json:"custom_names"`
		SilentAddNames  *bool    `json:"silent_add_names"`
	}
	var raw rawConfig
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	c.DefaultDuration = raw.DefaultDuration
	if c.DefaultDuration <= 0 {
		c.DefaultDuration = 60
	}
	c.DefaultType = raw.DefaultType
	c.DataPath = raw.DataPath

	c.SplitMode = raw.SplitMode
	if c.SplitMode == "" {
		c.SplitMode = SplitNone
	}

	c.DataFileName = raw.DataFileName
	if c.DataFileName == "" {
		c.DataFileName = "mycal"
	}

	c.DateCheckMode = raw.DateCheckMode

	if raw.UseSystemDate != nil {
		c.UseSystemDate = *raw.UseSystemDate
	} else {
		c.UseSystemDate = true
	}
	c.CustomDate = raw.CustomDate
	c.CustomStatuses = raw.CustomStatuses
	c.CustomNames = raw.CustomNames
	if raw.SilentAddNames != nil {
		c.SilentAddNames = *raw.SilentAddNames
	} else {
		c.SilentAddNames = false
	}

	return nil
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
}

// Date extracts the date from the session ID in YYYY-MM-DD format.
func (s Session) Date() string {
	if len(s.ID) >= IDLen {
		return s.ID[0:4] + "-" + s.ID[4:6] + "-" + s.ID[6:8]
	}
	return ""
}
