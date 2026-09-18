package calendar

import (
	"testing"

	"mycalendar/models"
)

// Validate accepts IDs of 10 or 11 characters, so Repair must cope with them.
// Regenerating a duplicate used to slice ID[:12] unconditionally and panic.
func TestRepairDuplicateShortIDDoesNotPanic(t *testing.T) {
	s := &Service{sessions: []models.Session{
		{ID: "2026010112", Time: "12:00", Name: "первая"},
		{ID: "2026010112", Time: "12:00", Name: "вторая"},
	}}

	if n := s.Repair(); n == 0 {
		t.Fatal("expected Repair to report a change for the duplicate ID")
	}
	if len(s.sessions) != 2 {
		t.Fatalf("expected both sessions kept, got %d", len(s.sessions))
	}
	if s.sessions[0].ID == s.sessions[1].ID {
		t.Errorf("IDs still duplicated: %q", s.sessions[0].ID)
	}
	for _, x := range s.sessions {
		if x.Date() != "2026-01-01" {
			t.Errorf("session %q lost its date: %q", x.ID, x.Date())
		}
	}
	if issues := s.Validate(); len(issues) != 0 {
		t.Errorf("calendar still invalid after repair: %+v", issues)
	}
}

// Every occurrence of a duplicated ID past the first must be reported, so the
// pre-repair count matches what Repair actually rewrites.
func TestValidateCountsEveryDuplicate(t *testing.T) {
	s := &Service{sessions: []models.Session{
		{ID: "20260101120000", Time: "12:00"},
		{ID: "20260101120000", Time: "12:00"},
		{ID: "20260101120000", Time: "12:00"},
	}}
	dupes := 0
	for _, is := range s.Validate() {
		if is.Detail == "дублирующийся ID" {
			dupes++
		}
	}
	if dupes != 2 {
		t.Errorf("got %d duplicate issues for 3 identical IDs, want 2", dupes)
	}
}
