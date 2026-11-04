//go:build windows

package tui

import (
	"syscall"
	"unsafe"
)

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

func getTerminalSize(fd uintptr) (int, int) {
	var info consoleScreenBufferInfo
	r, _, _ := procGetConsoleScreenBufferInfo.Call(fd, uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		return 80, 24
	}
	w := int(info.window[2]-info.window[0]) + 1
	h := int(info.window[3]-info.window[1]) + 1
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}
	return w, h
}

func makeRawMode(fd uintptr) (restore func(), err error) {
	h := syscall.Handle(fd)
	var old uint32
	if err := syscall.GetConsoleMode(h, &old); err != nil {
		return nil, err
	}
	raw := old&^(enableEchoInput|enableLineInput|enableProcessedInput) | enableVirtualTerminalInput
	if r, _, e := procSetConsoleMode.Call(uintptr(h), uintptr(raw)); r == 0 {
		return nil, e
	}
	return func() {
		procSetConsoleMode.Call(uintptr(h), uintptr(old))
	}, nil
}

func setupResizeHandler(onResize func()) (cleanup func()) {
	// On Windows, resizing is polled or handled via buffer check during render loop.
	return func() {}
}
