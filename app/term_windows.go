//go:build windows

package app

import (
	"syscall"
	"unsafe"
)

// The syscall package exposes GetConsoleMode but not SetConsoleMode or the
// screen-buffer query, so they are called through kernel32 directly. This keeps
// the project dependency-free.
var (
	kernel32                       = syscall.NewLazyDLL("kernel32.dll")
	procSetConsoleMode             = kernel32.NewProc("SetConsoleMode")
	procGetConsoleScreenBufferInfo = kernel32.NewProc("GetConsoleScreenBufferInfo")
)

const (
	enableProcessedInput       = 0x0001
	enableLineInput            = 0x0002
	enableEchoInput            = 0x0004
	enableVirtualTerminalInput = 0x0200
)

type consoleCoord struct{ x, y int16 }

type consoleScreenBufferInfo struct {
	size              consoleCoord
	cursorPosition    consoleCoord
	attributes        uint16
	window            [4]int16 // left, top, right, bottom
	maximumWindowSize consoleCoord
}

// isTerminal reports whether fd is a console handle.
func isTerminal(fd uintptr) bool {
	var mode uint32
	return syscall.GetConsoleMode(syscall.Handle(fd), &mode) == nil
}

// makeRaw switches the console input to character-at-a-time mode without echo.
// Keys arrive as VT sequences (arrows are ESC [ D and so on). The returned
// function restores the previous mode.
func makeRaw(fd uintptr) (restore func(), err error) {
	h := syscall.Handle(fd)
	var old uint32
	if err := syscall.GetConsoleMode(h, &old); err != nil {
		return nil, err
	}
	raw := old&^(enableEchoInput|enableLineInput|enableProcessedInput) | enableVirtualTerminalInput
	if r, _, e := procSetConsoleMode.Call(uintptr(h), uintptr(raw)); r == 0 {
		return nil, e
	}
	return func() { procSetConsoleMode.Call(uintptr(h), uintptr(old)) }, nil
}

// terminalWidth returns the console width in columns, or 0 if unknown.
func terminalWidth(fd uintptr) int {
	var info consoleScreenBufferInfo
	r, _, _ := procGetConsoleScreenBufferInfo.Call(fd, uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		return 0
	}
	return int(info.window[2]-info.window[0]) + 1
}
