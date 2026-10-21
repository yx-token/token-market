//go:build unix

package update

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func (r Runtime) writable() bool { return unix.Access(r.Root, unix.W_OK) == nil }

func (r Runtime) lock() (*os.File, error) {
	f, err := os.OpenFile(filepath.Join(r.Root, ".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, ErrBusy
		}
		return nil, err
	}
	return f, nil
}

func unlock(f *os.File) { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN); _ = f.Close() }
