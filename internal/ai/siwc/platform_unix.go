//go:build !windows

package siwc

import (
	"errors"
	"os"
	"syscall"
)

// Never follow a symlink when opening credential or lock files.
const openNoFollow = syscall.O_NOFOLLOW

// tryLock takes the exclusive advisory lock without waiting; held reports
// that another process has it.
func tryLock(f *os.File) (held bool, err error) {
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return true, nil
	}
	return false, err
}

func unlock(f *os.File) { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }

// ownerOnly reports that no group or other permission bits are set.
func ownerOnly(info os.FileInfo) bool { return info.Mode().Perm()&0077 == 0 }

// syncDir makes the rename of the credential file durable.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
