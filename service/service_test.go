package service

import (
	"context"
	"testing"
)

func TestServiceManagerStatus(t *testing.T) {
	mgr := NewManager()
	if mgr == nil {
		t.Fatal("NewManager returned nil")
	}

	info, err := mgr.Status(context.Background())
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}

	if info == nil {
		t.Fatal("Status returned nil StatusInfo")
	}

	if info.Platform == "" {
		t.Errorf("expected non-empty platform, got %q", info.Platform)
	}
}

func TestGetExePath(t *testing.T) {
	exePath, err := GetExePath()
	if err != nil {
		t.Fatalf("GetExePath error: %v", err)
	}
	if exePath == "" {
		t.Error("expected non-empty exePath")
	}
}
