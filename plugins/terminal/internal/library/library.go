// Package library finds libghostty-vt for the terminal plugin, as
// mygo-plugin.json names it.
package library

import (
	"fmt"
	"os"

	"github.com/egoist/mygo/ffi"
)

// Find returns the libghostty-vt that manifest names for this platform:
// the file $MYGO_GHOSTTY_VT names, or else the one ffi.Library.Path finds
// among the app's resources, where `mygo build` and `mygo dev` put it,
// next to the executable, or in the user's cache, where programs that are
// not packaged apps, as under `go run` and `go test`, download it when it
// is missing, checking its SHA-256.
//
// Packaged apps never download it: build them with the CLI.
func Find(manifest []byte) (string, error) {
	if p := os.Getenv("MYGO_GHOSTTY_VT"); p != "" {
		return p, nil
	}
	path, err := ffi.New("libghostty-vt", manifest).Path()
	if err != nil {
		return "", fmt.Errorf("terminal: %w", err)
	}
	return path, nil
}
