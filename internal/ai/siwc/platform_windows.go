//go:build windows

package siwc

import (
	"os"
	"syscall"
	"unsafe"
)

// Windows has no O_NOFOLLOW. The local sign-in helper is the only Windows
// build; it writes into a directory it creates under the user's profile.
const openNoFollow = 0

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = kernel32.NewProc("LockFileEx")
	procUnlockFileEx = kernel32.NewProc("UnlockFileEx")
)

const (
	lockfileFailImmediately = 0x1
	lockfileExclusiveLock   = 0x2
	errorLockViolation      = syscall.Errno(33)
)

// tryLock takes an exclusive lock on the first byte without waiting; held
// reports that another process has it.
func tryLock(f *os.File) (held bool, err error) {
	var overlapped syscall.Overlapped
	r, _, e := procLockFileEx.Call(f.Fd(), lockfileExclusiveLock|lockfileFailImmediately, 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
	if r != 0 {
		return false, nil
	}
	if e == errorLockViolation {
		return true, nil
	}
	return false, e
}

func unlock(f *os.File) {
	var overlapped syscall.Overlapped
	_, _, _ = procUnlockFileEx.Call(f.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
}

// Windows protects files with ACLs inherited from the user's profile, not with
// mode bits; Go reports 0666 for any writable file there.
func ownerOnly(os.FileInfo) bool { return true }

// Directories cannot be fsynced on Windows; the rename itself is the commit.
func syncDir(string) error { return nil }
