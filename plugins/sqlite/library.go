package sqlite

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/egoist/mygo/ffi"
)

//go:generate go run ../../cmd/mygo native build .

//go:embed mygo-plugin.json
var natives []byte

var load struct {
	once sync.Once
	err  error
}

// Load loads the plugin's SQLite library once. Open calls it lazily, so
// binding the plugin and generating clients require no native library.
func Load() error {
	load.once.Do(func() {
		if !supported {
			load.err = fmt.Errorf("sqlite: %s/%s: %w", runtime.GOOS, runtime.GOARCH, errors.ErrUnsupported)
			return
		}
		path, err := LibraryPath()
		if err == nil {
			err = loadNative(path)
		}
		load.err = err
	})
	return load.err
}

// LibraryPath finds the library named by MYGO_SQLITE_LIBRARY, or else the
// one ffi.Library.Path finds: among the app's resources, next to the
// executable, or in the CLI's SHA-256-checked cache. Unpackaged programs
// download a missing library into that cache; packaged apps require the
// library that mygo dev/build put into them.
func LibraryPath() (string, error) {
	if path := os.Getenv("MYGO_SQLITE_LIBRARY"); path != "" {
		return filepath.Abs(path)
	}
	path, err := ffi.New("mygo-sqlite3", natives).Path()
	if err != nil {
		return "", fmt.Errorf("sqlite: %w", err)
	}
	return path, nil
}
