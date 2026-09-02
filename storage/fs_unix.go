//go:build !windows

package storage

import (
	"errors"
	"os"
	"syscall"
)

// syncDir flushes a directory entry so a freshly renamed file survives a crash.
func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	defer d.Close()
	_ = d.Sync()
}

func isNoSpace(err error) bool {
	return errors.Is(err, syscall.ENOSPC)
}
