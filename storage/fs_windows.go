//go:build windows

package storage

import (
	"errors"
	"syscall"
)

// syncDir is a no-op on Windows: directory handles cannot be flushed the way
// they can on POSIX systems.
func syncDir(string) {}

// ERROR_DISK_FULL / ERROR_HANDLE_DISK_FULL.
const (
	errorDiskFull       syscall.Errno = 112
	errorHandleDiskFull syscall.Errno = 39
)

func isNoSpace(err error) bool {
	return errors.Is(err, errorDiskFull) || errors.Is(err, errorHandleDiskFull) || errors.Is(err, syscall.ENOSPC)
}
