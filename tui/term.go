package tui

import (
	"fmt"
	"os"
)

// Terminal manages the terminal screen state and raw mode.
type Terminal struct {
	restoreRaw func()
	stopResize func()
}

// Enter sets up the terminal for full-screen TUI: raw mode, alternate screen buffer, hidden cursor.
func Enter(onResize func()) (*Terminal, error) {
	restore, err := makeRawMode(os.Stdin.Fd())
	if err != nil {
		return nil, fmt.Errorf("не удалось перевести терминал в сырой режим: %w", err)
	}

	// Alternate screen buffer, clear, hide cursor
	fmt.Print("\033[?1049h\033[2J\033[H\033[?25l")

	t := &Terminal{
		restoreRaw: restore,
	}

	if onResize != nil {
		t.stopResize = setupResizeHandler(onResize)
	}

	return t, nil
}

// Exit restores the terminal to normal cooked mode, restores cursor, and exits alternate buffer.
func (t *Terminal) Exit() {
	if t.stopResize != nil {
		t.stopResize()
	}
	// Show cursor, exit alternate screen buffer
	fmt.Print("\033[?25h\033[?1049l")
	if t.restoreRaw != nil {
		t.restoreRaw()
	}
}

// Size returns the current width and height of the terminal in character columns and rows.
func Size() (int, int) {
	w, h := getTerminalSize(os.Stdout.Fd())
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}
	return w, h
}
