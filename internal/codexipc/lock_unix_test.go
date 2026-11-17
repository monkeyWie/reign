//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package codexipc

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestWriterProbeDistinguishesStaleFileFromHeldLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "thread.lock")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if locked, err := WriterLocked(path); locked || err != nil {
		t.Fatal("stale file blocked", locked, err)
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if locked, err := WriterLocked(path); !locked || err != nil {
		t.Fatal("held writer not detected", locked, err)
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	if locked, err := WriterLocked(path); locked || err != nil {
		t.Fatal("released writer still blocked", locked, err)
	}
}
