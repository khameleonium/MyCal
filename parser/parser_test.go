package parser

import (
	"fmt"
	"testing"
	"time"
)

func TestParseDate(t *testing.T) {
	ref := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"DD-MM-YYYY", "29-12-2025", "2025-12-29", false},
		{"DD.MM.YYYY", "29.12.2025", "2025-12-29", false},
		{"YYYY-MM-DD", "2025-12-29", "2025-12-29", false},
		{"DD MM YYYY (normalized)", "29 12 2025", "2025-12-29", false},
		{"DD-MM without year", "29-12", "", false},
		{"empty returns default", "", ref.Format("2006-01-02"), false},
		{"garbage", "not-a-date", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseDate(tt.input, ref)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if tt.want == "" {
				wantYear := time.Now().Year()
				if got.Month() != 12 || got.Day() != 29 || got.Year() != wantYear {
					t.Errorf("got %s, want 29-12 with current year (%d)", got.Format("2006-01-02"), wantYear)
				}
			} else if got.Format("2006-01-02") != tt.want {
				t.Errorf("got %s, want %s", got.Format("2006-01-02"), tt.want)
			}
		})
	}
}

func TestParseTime(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{"HH:MM", "10:51", "10:51", nil},
		{"HH MM", "10 51", "10:51", nil},
		{"single digit hour", "9:05", "09:05", nil},
		{"just hours", "15", "15:00", nil},
		{"just hour single digit", "9", "09:00", nil},
		{"24 hours is 00:00", "24", "00:00", nil},
		{"empty", "", "", ErrEmptyTime},
		{"garbage", "xyz", "", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseTime(tt.input)
			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("got error %v, want %v", err, tt.wantErr)
				}
				return
			}
			if tt.wantErr == nil && err != nil && tt.name == "garbage" {
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if got.Format("15:04") != tt.want {
				t.Errorf("got %s, want %s", got.Format("15:04"), tt.want)
			}
		})
	}
}

func TestParseDateTime(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantDate   string
		wantTime   string
		wantErr    bool
	}{
		{"DD-MM-YYYY HH:MM", "29-12-2025 10:51", "2025-12-29", "10:51", false},
		{"YYYY-MM-DD HH:MM", "2025-12-29 10:51", "2025-12-29", "10:51", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			date, tm, err := ParseDateTime(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if date.Format("2006-01-02") != tt.wantDate {
				t.Errorf("date: got %s, want %s", date.Format("2006-01-02"), tt.wantDate)
			}
			if tm.Format("15:04") != tt.wantTime {
				t.Errorf("time: got %s, want %s", tm.Format("15:04"), tt.wantTime)
			}
		})
	}
}

func TestFormatDate(t *testing.T) {
	date := time.Date(2025, 12, 29, 0, 0, 0, 0, time.UTC)
	if got := FormatDate(date); got != "29-12-2025" {
		t.Errorf("got %s, want 29-12-2025", got)
	}
}

func TestFormatTime(t *testing.T) {
	tm := time.Date(0, 1, 1, 10, 51, 0, 0, time.UTC)
	if got := FormatTime(tm); got != "10:51" {
		t.Errorf("got %s, want 10:51", got)
	}
}

func TestParsePeriod(t *testing.T) {
	currYear := time.Now().Year()
	tests := []struct {
		name      string
		input     string
		wantStart string
		wantEnd   string
		wantErr   bool
	}{
		{"YYYY", "2025", "2025-01-01", "2025-12-31", false},
		{"MM.YYYY", "12.2025", "2025-12-01", "2025-12-31", false},
		{"MM/YYYY", "12/2025", "2025-12-01", "2025-12-31", false},
		{"MM", "12", fmt.Sprintf("%d-12-01", currYear), fmt.Sprintf("%d-12-31", currYear), false},
		{"MM-MM", "10-12", fmt.Sprintf("%d-10-01", currYear), fmt.Sprintf("%d-12-31", currYear), false},
		{"DD.MM-DD.MM", "01.12-15.12", fmt.Sprintf("%d-12-01", currYear), fmt.Sprintf("%d-12-15", currYear), false},
		{"DD.MM.YYYY - DD.MM.YYYY", "01.12.2025 - 15.12.2025", "2025-12-01", "2025-12-15", false},
		{"Single Date Fallback", "15.12.2025", "2025-12-15", "2025-12-15", false},
		{"Empty", "", fmt.Sprintf("%d-01-01", currYear), fmt.Sprintf("%d-12-31", currYear), false},
		{"Garbage", "not-a-period", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, err := ParsePeriod(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if start.Format("2006-01-02") != tt.wantStart {
				t.Errorf("start: got %s, want %s", start.Format("2006-01-02"), tt.wantStart)
			}
			if end.Format("2006-01-02") != tt.wantEnd {
				t.Errorf("end: got %s, want %s", end.Format("2006-01-02"), tt.wantEnd)
			}
		})
	}
}
