// Package nativelib is what the CLI and package ffi share about the native
// libraries that Go packages bind: mygo-plugin.json, the file names of
// libraries on each platform, and where libraries are built and cached.
//
// A package names its libraries in mygo-plugin.json, next to its sources:
//
//	{"libraries": [{
//		"name": "adder",
//		"build": {"cargo": "./rust"},
//		"release": "https://github.com/me/app/releases/download/adder-v1",
//		"files": {"darwin-arm64": {"name": "libadder.dylib", "url": "…", "sha256": "…"}, …}
//	}]}
//
// build is how the CLI builds the library from its sources (a Rust crate
// with cargo, a Zig package with zig build); files are the published builds,
// one per platform (GOOS-GOARCH), which `mygo native build` writes and
// apps download when they do not build the library themselves.
package nativelib

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ManifestFile is the file that names a package's native libraries.
const ManifestFile = "mygo-plugin.json"

// Manifest is the content of mygo-plugin.json.
type Manifest struct {
	Libraries []Library `json:"libraries"`
}

// Library is a native library a package binds.
type Library struct {
	// Name names the library, as ffi.New does: its files are
	// lib<name>.dylib, lib<name>.so and <name>.dll by default (a leading
	// "lib" is not doubled, and dropped on Windows).
	Name string `json:"name"`
	// Source tells where the sources of the published files come from.
	Source string `json:"source,omitempty"`
	// Build builds the library from sources next to the package.
	Build *Build `json:"build,omitempty"`
	// Release is the URL of the directory the files are published in,
	// as https://github.com/<owner>/<repo>/releases/download/<tag>.
	Release string `json:"release,omitempty"`
	// Files are the published builds, by GOOS-GOARCH.
	Files map[string]File `json:"files,omitempty"`
}

// Build is how a library is built from its sources: exactly one of Cargo
// and Zig, a directory relative to the package's, written "./rust" to read
// as one.
type Build struct {
	// Cargo is the directory of the Rust crate (its Cargo.toml), whose
	// crate-type includes "cdylib".
	Cargo string `json:"cargo,omitempty"`
	// Zig is the directory of the Zig package (its build.zig), which
	// installs a dynamic library.
	Zig string `json:"zig,omitempty"`
	// Args are more arguments of `cargo build` or `zig build`.
	Args []string `json:"args,omitempty"`
}

// Tool returns the build's tool, "cargo" or "zig", and its directory.
func (b *Build) Tool() (tool, dir string) {
	if b.Cargo != "" {
		return "cargo", b.Cargo
	}
	return "zig", b.Zig
}

// File is a published build of a library.
type File struct {
	Name   string `json:"name"`
	URL    string `json:"url,omitempty"`
	SHA256 string `json:"sha256"`
}

// Targets are the platforms MyGo builds libraries for, as GOOS-GOARCH.
var Targets = []string{"darwin-arm64", "darwin-amd64", "linux-amd64", "linux-arm64", "windows-amd64", "windows-arm64"}

// Parse reads and checks a manifest.
func Parse(data []byte) (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return m, err
	}
	seen := map[string]bool{}
	for _, l := range m.Libraries {
		if !validName(l.Name) {
			return m, fmt.Errorf("invalid library name %q", l.Name)
		}
		if seen[l.Name] {
			return m, fmt.Errorf("library %s is named twice", l.Name)
		}
		seen[l.Name] = true
		if b := l.Build; b != nil {
			if (b.Cargo == "") == (b.Zig == "") {
				return m, fmt.Errorf("library %s: build needs one of cargo and zig", l.Name)
			}
			if _, dir := b.Tool(); filepath.IsAbs(dir) || !filepath.IsLocal(filepath.FromSlash(dir)) {
				return m, fmt.Errorf("library %s: the build's directory %q is not inside the package", l.Name, dir)
			}
		}
		for target, f := range l.Files {
			if err := f.Check(); err != nil {
				return m, fmt.Errorf("library %s, %s: %w", l.Name, target, err)
			}
		}
	}
	return m, nil
}

// Lookup returns the library named name.
func (m Manifest) Lookup(name string) (Library, bool) {
	for _, l := range m.Libraries {
		if l.Name == name {
			return l, true
		}
	}
	return Library{}, false
}

// Check checks that the file's name and SHA-256 can be parts of a path: a
// package may not name another file than the library it downloads.
func (f File) Check() error {
	sum, err := hex.DecodeString(f.SHA256)
	if err != nil || len(sum) != sha256.Size || f.SHA256 != strings.ToLower(f.SHA256) {
		return fmt.Errorf("invalid SHA-256 %q", f.SHA256)
	}
	if !validName(f.Name) {
		return fmt.Errorf("invalid file name %q", f.Name)
	}
	return nil
}

func validName(name string) bool {
	return name != "" && name != "." && name != ".." && filepath.Base(name) == name && !strings.ContainsAny(name, `/\:`)
}

// FileName returns the file of the library named name on goos when no
// published file names another: lib<name>.dylib, lib<name>.so and
// <name>.dll, as cargo and zig build them.
func FileName(name, goos string) string {
	base := strings.TrimPrefix(name, "lib")
	switch goos {
	case "darwin":
		return "lib" + base + ".dylib"
	case "windows":
		return base + ".dll"
	default:
		return "lib" + base + ".so"
	}
}

// FileName returns the library's file on goos/goarch.
func (l Library) FileName(goos, goarch string) string {
	if f, ok := l.Files[goos+"-"+goarch]; ok {
		return f.Name
	}
	return FileName(l.Name, goos)
}

// Ext returns the extension of dynamic libraries on goos.
func Ext(goos string) string {
	switch goos {
	case "darwin":
		return ".dylib"
	case "windows":
		return ".dll"
	default:
		return ".so"
	}
}

// LocalDir returns where the CLI puts the libraries it builds from sources
// in the module at root for goos/goarch, which programs that are not
// packaged apps look into too: <root>/.mygo/native/<goos>-<goarch>.
func LocalDir(root, goos, goarch string) string {
	return filepath.Join(root, ".mygo", "native", goos+"-"+goarch)
}

// ModuleRoot returns the directory of the go.mod that dir is in.
func ModuleRoot(dir string) (string, error) {
	for d := dir; ; {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d, nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", errors.New(dir + " is not in a Go module")
		}
		d = parent
	}
}

// CachePath returns where a published file is kept in the user's cache:
// <cache>/mygo/natives/<sha256>/<name>.
func CachePath(f File) (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cache, "mygo", "natives", f.SHA256, f.Name), nil
}
