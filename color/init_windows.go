//go:build windows

package color

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func init() {
	var mode uint32
	if err := windows.GetConsoleMode(windows.Stdout, &mode); err == nil {
		if err := windows.SetConsoleMode(windows.Stdout, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING); err != nil {
			fmt.Fprintf(os.Stderr, "предупреждение: не удалось включить поддержку ANSI цветов: %v\n", err)
		}
	}
}
