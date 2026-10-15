//go:build !windows && !plan9

package mygo

import (
	"os"
	"reflect"
	"sync/atomic"
	"syscall"
)

// fileStatType is the type of the os package's FileInfo, once seen.
var fileStatType atomic.Pointer[reflect.Type]

// fileWatchIdentity returns the identity os.SameFile compares, the device
// and inode of the os package's fileStat (os/types_unix.go). Another
// FileInfo, which os.SameFile takes for no file, has none.
func fileWatchIdentity(info os.FileInfo) (fileWatchID, bool) {
	t := reflect.TypeOf(info)
	if t == nil {
		return fileWatchID{}, false
	}
	if known := fileStatType.Load(); known == nil || *known != t {
		if t.Kind() != reflect.Pointer || t.Elem().Name() != "fileStat" || t.Elem().PkgPath() != "os" {
			return fileWatchID{}, false
		}
		fileStatType.Store(&t)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fileWatchID{}, false
	}
	return fileWatchID{uint64(st.Dev), uint64(st.Ino)}, true
}
