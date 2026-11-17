//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package codexipc

import (
	"errors"
	"os"
	"syscall"
)

// WriterLocked probes the OS lock, not the existence of a stale lock file.
// Errors fail closed; callers must not steal a possibly held thread.
func WriterLocked(path string) (bool, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
