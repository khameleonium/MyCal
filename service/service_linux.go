//go:build linux

package service

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type linuxManager struct{}

// NewManager returns the platform-specific service manager.
func NewManager() Manager {
	return &linuxManager{}
}

func getUnitPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("не удалось определить домашнюю директорию: %w", err)
	}
	dir := filepath.Join(home, ".config", "systemd", "user")
	return filepath.Join(dir, "mycal.service"), nil
}

func (m *linuxManager) Install(ctx context.Context, port int) error {
	exePath, err := GetExePath()
	if err != nil {
		return fmt.Errorf("не удалось определить путь к бинарнику: %w", err)
	}
	unitPath, err := getUnitPath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(unitPath), 0755); err != nil {
		return fmt.Errorf("не удалось создать директорию %s: %w", filepath.Dir(unitPath), err)
	}

	workDir := filepath.Dir(exePath)
	unitContent := fmt.Sprintf(`[Unit]
Description=MyCal Web Calendar & Time Tracker
After=network.target

[Service]
Type=simple
ExecStart=%s web %d
Restart=always
RestartSec=3
WorkingDirectory=%s
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=default.target
`, exePath, port, workDir)

	if err := os.WriteFile(unitPath, []byte(unitContent), 0644); err != nil {
		return fmt.Errorf("не удалось записать юнит-файл %s: %w", unitPath, err)
	}

	_ = exec.CommandContext(ctx, "systemctl", "--user", "daemon-reload").Run()
	cmd := exec.CommandContext(ctx, "systemctl", "--user", "enable", "--now", "mycal.service")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ошибка активации службы (systemctl --user enable --now mycal): %s (%w)", strings.TrimSpace(string(out)), err)
	}

	return nil
}

func (m *linuxManager) Uninstall(ctx context.Context) error {
	unitPath, err := getUnitPath()
	if err != nil {
		return err
	}

	_ = exec.CommandContext(ctx, "systemctl", "--user", "stop", "mycal.service").Run()
	_ = exec.CommandContext(ctx, "systemctl", "--user", "disable", "mycal.service").Run()

	if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("не удалось удалить юнит-файл: %w", err)
	}

	_ = exec.CommandContext(ctx, "systemctl", "--user", "daemon-reload").Run()
	_ = exec.CommandContext(ctx, "systemctl", "--user", "reset-failed").Run()
	return nil
}

func (m *linuxManager) Start(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "systemctl", "--user", "start", "mycal.service")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ошибка запуска службы: %s (%w)", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (m *linuxManager) Stop(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "systemctl", "--user", "stop", "mycal.service")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ошибка остановки службы: %s (%w)", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (m *linuxManager) Restart(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "systemctl", "--user", "restart", "mycal.service")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ошибка перезапуска службы: %s (%w)", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (m *linuxManager) Status(ctx context.Context) (*StatusInfo, error) {
	unitPath, err := getUnitPath()
	if err != nil {
		return nil, err
	}

	info := &StatusInfo{
		Platform: "systemd (user)",
		UnitPath: unitPath,
	}

	if _, err := os.Stat(unitPath); err == nil {
		info.Installed = true
	}

	cmdActive := exec.CommandContext(ctx, "systemctl", "--user", "is-active", "mycal.service")
	outActive, _ := cmdActive.Output()
	if strings.TrimSpace(string(outActive)) == "active" {
		info.Running = true
	}

	cmdStatus := exec.CommandContext(ctx, "systemctl", "--user", "status", "mycal.service", "--no-pager")
	var out bytes.Buffer
	cmdStatus.Stdout = &out
	cmdStatus.Stderr = &out
	_ = cmdStatus.Run()
	info.Details = strings.TrimSpace(out.String())

	return info, nil
}
