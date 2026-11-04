//go:build windows

package service

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type windowsManager struct{}

// NewManager returns the platform-specific service manager for Windows.
func NewManager() Manager {
	return &windowsManager{}
}

const taskName = "MyCalWeb"

func (m *windowsManager) Install(ctx context.Context, port int) error {
	exePath, err := GetExePath()
	if err != nil {
		return fmt.Errorf("не удалось определить путь к бинарнику: %w", err)
	}

	cmdStr := fmt.Sprintf("\"%s\" web %d", exePath, port)
	cmd := exec.CommandContext(ctx, "schtasks", "/Create", "/TN", taskName, "/TR", cmdStr, "/SC", "ONLOGON", "/F")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ошибка создания задачи в планировщике Windows: %s (%w)", strings.TrimSpace(string(out)), err)
	}

	_ = exec.CommandContext(ctx, "schtasks", "/Run", "/TN", taskName).Run()
	return nil
}

func (m *windowsManager) Uninstall(ctx context.Context) error {
	_ = exec.CommandContext(ctx, "schtasks", "/End", "/TN", taskName).Run()
	cmd := exec.CommandContext(ctx, "schtasks", "/Delete", "/TN", taskName, "/F")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ошибка удаления задачи из планировщика: %s (%w)", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (m *windowsManager) Start(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "schtasks", "/Run", "/TN", taskName)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ошибка запуска задачи: %s (%w)", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (m *windowsManager) Stop(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "schtasks", "/End", "/TN", taskName)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ошибка остановки задачи: %s (%w)", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (m *windowsManager) Restart(ctx context.Context) error {
	_ = m.Stop(ctx)
	return m.Start(ctx)
}

func (m *windowsManager) Status(ctx context.Context) (*StatusInfo, error) {
	cmd := exec.CommandContext(ctx, "schtasks", "/Query", "/TN", taskName, "/FO", "LIST")
	out, err := cmd.CombinedOutput()
	outStr := strings.TrimSpace(string(out))

	info := &StatusInfo{
		Platform: "Windows Task Scheduler (ONLOGON)",
		Details:  outStr,
	}

	if err == nil && strings.Contains(outStr, taskName) {
		info.Installed = true
		if strings.Contains(outStr, "Running") || strings.Contains(outStr, "Работает") {
			info.Running = true
		}
	}

	return info, nil
}
