//go:build windows

package color

import (
	"golang.org/x/sys/windows"
)

// init enables ANSI escape processing on the Windows console. It stays silent
// on failure: if it does not work, colours are simply disabled elsewhere.
func init() {
	if !enabled {
		return
	}
	var mode uint32
	if windows.GetConsoleMode(windows.Stdout, &mode) != nil {
		return
	}
	if windows.SetConsoleMode(windows.Stdout, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING) != nil {
		// Could not enable virtual terminal processing — disable colours so we
		// do not print raw escape codes.
		enabled = false
	}
}
