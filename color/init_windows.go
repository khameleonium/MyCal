//go:build windows

package color

import "syscall"

// ENABLE_VIRTUAL_TERMINAL_PROCESSING — lets the Windows console interpret ANSI
// escape sequences. Available on Windows 10 1511+ and Windows 11.
const enableVTProcessing = 0x0004

// syscall exposes GetConsoleMode but not SetConsoleMode, so the latter is
// called through kernel32 directly. This keeps the project dependency-free.
var procSetConsoleMode = syscall.NewLazyDLL("kernel32.dll").NewProc("SetConsoleMode")

// init enables ANSI escape processing on the Windows console. It stays silent
// on failure and disables colour so raw escape codes are never printed.
func init() {
	if !enabled {
		return
	}
	h := syscall.Handle(syscall.Stdout)
	var mode uint32
	if syscall.GetConsoleMode(h, &mode) != nil {
		return
	}
	if r, _, _ := procSetConsoleMode.Call(uintptr(h), uintptr(mode|enableVTProcessing)); r == 0 {
		enabled = false
	}
}
