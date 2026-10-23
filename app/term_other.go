//go:build !linux && !darwin && !windows

package app

import "errors"

// On other systems the terminal's own line discipline is used.

func isTerminal(fd uintptr) bool { return false }

func makeRaw(fd uintptr) (func(), error) { return nil, errors.New("raw mode is not supported") }

func terminalWidth(fd uintptr) int { return 0 }
