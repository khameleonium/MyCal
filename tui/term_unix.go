//go:build linux || darwin

package tui

import (
	"os"
	"os/signal"
	"syscall"
	"unsafe"
)

type winsize struct {
	row    uint16
	col    uint16
	xpixel uint16
	ypixel uint16
}

func getTerminalSize(fd uintptr) (int, int) {
	var ws winsize
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCGWINSZ, uintptr(unsafe.Pointer(&ws))); e == 0 && ws.col > 0 && ws.row > 0 {
		return int(ws.col), int(ws.row)
	}
	return 80, 24
}

func makeRawMode(fd uintptr) (restore func(), err error) {
	var old syscall.Termios
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, ioctlReadTermios, uintptr(unsafe.Pointer(&old))); e != 0 {
		return nil, e
	}
	raw := old
	raw.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP |
		syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	raw.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	raw.Cflag &^= syscall.CSIZE | syscall.PARENB
	raw.Cflag |= syscall.CS8
	raw.Cc[syscall.VMIN] = 1
	raw.Cc[syscall.VTIME] = 0
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, ioctlWriteTermios, uintptr(unsafe.Pointer(&raw))); e != 0 {
		return nil, e
	}
	return func() {
		_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, fd, ioctlWriteTermios, uintptr(unsafe.Pointer(&old)))
	}, nil
}

func setupResizeHandler(onResize func()) (cleanup func()) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGWINCH)
	done := make(chan struct{})

	go func() {
		for {
			select {
			case <-sigChan:
				onResize()
			case <-done:
				return
			}
		}
	}()

	return func() {
		signal.Stop(sigChan)
		close(done)
	}
}
