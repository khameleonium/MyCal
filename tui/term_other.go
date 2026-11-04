//go:build !linux && !darwin && !windows

package tui

func getTerminalSize(fd uintptr) (int, int) {
	return 80, 24
}

func makeRawMode(fd uintptr) (func(), error) {
	return func() {}, nil
}

func setupResizeHandler(onResize func()) func() {
	return func() {}
}
