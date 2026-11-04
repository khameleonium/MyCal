//go:build !linux && !windows

package service

import (
	"context"
	"errors"
)

type otherManager struct{}

// NewManager returns a fallback service manager for unsupported platforms.
func NewManager() Manager {
	return &otherManager{}
}

func (m *otherManager) Install(ctx context.Context, port int) error {
	return errors.New("автоматическое управление службой пока не поддерживается на этой ОС")
}

func (m *otherManager) Uninstall(ctx context.Context) error {
	return errors.New("автоматическое управление службой пока не поддерживается на этой ОС")
}

func (m *otherManager) Start(ctx context.Context) error {
	return errors.New("автоматическое управление службой пока не поддерживается на этой ОС")
}

func (m *otherManager) Stop(ctx context.Context) error {
	return errors.New("автоматическое управление службой пока не поддерживается на этой ОС")
}

func (m *otherManager) Restart(ctx context.Context) error {
	return errors.New("автоматическое управление службой пока не поддерживается на этой ОС")
}

func (m *otherManager) Status(ctx context.Context) (*StatusInfo, error) {
	return &StatusInfo{
		Platform: "unsupported",
		Details:  "Управление службой не реализовано для данной платформы",
	}, nil
}
