package service

import (
	"context"
	"os"
	"path/filepath"
)

// StatusInfo represents the service status.
type StatusInfo struct {
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
	Platform  string `json:"platform"`
	UnitPath  string `json:"unit_path,omitempty"`
	Details   string `json:"details"`
}

// Manager defines the interface for managing MyCal as a background service.
type Manager interface {
	Install(ctx context.Context, port int) error
	Uninstall(ctx context.Context) error
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Restart(ctx context.Context) error
	Status(ctx context.Context) (*StatusInfo, error)
}

// GetExePath returns the absolute resolved path to the running executable.
func GetExePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}
