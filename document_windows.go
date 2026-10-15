//go:build windows

package mygo

import (
	"os"
	"syscall"
	"unsafe"
)

func fileLinks(os.FileInfo) int { return 1 }

// openShared opens the file at path to read it, letting other apps
// delete, rename or replace it meanwhile, as editors save: os.Open shares
// reading and writing only, and an app saving over a document while the
// app read it would be refused ("Access is denied").
func openShared(path string) (*os.File, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}

// fileWritable reports whether the file at path lacks the read-only
// attribute, as Notepad checks it.
func fileWritable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().Perm()&0o200 != 0
}

var procLockFileEx = syscall.NewLazyDLL("kernel32.dll").NewProc("LockFileEx")

// lockFile takes an exclusive lock on f, which fails while another
// process holds it, until f is closed.
func lockFile(f *os.File) error {
	const lockfileFailImmediately, lockfileExclusiveLock = 1, 2
	var ol syscall.Overlapped
	r, _, err := procLockFileEx.Call(f.Fd(), lockfileFailImmediately|lockfileExclusiveLock, 0, 1, 0, uintptr(unsafe.Pointer(&ol)))
	if r == 0 {
		return err
	}
	return nil
}
