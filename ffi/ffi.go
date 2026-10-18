// Package ffi loads native libraries, such as a Rust crate or a Zig package
// built as a dynamic library, and binds their functions to Go functions,
// without cgo.
//
// A package names the libraries it binds in mygo-plugin.json, next to its
// sources, with how to build them:
//
//	{"libraries": [{"name": "adder", "build": {"cargo": "rust"}}]}
//
// `mygo dev` and `mygo build` build them for the app's platform (cargo
// build or zig build) and put them among the app's resources;
// `mygo native build` builds them for every platform, ready to publish, so
// that the apps of others download them instead of building them. The
// package embeds the manifest and binds the library's functions:
//
//	//go:embed mygo-plugin.json
//	var manifest []byte
//
//	var adder = ffi.New("adder", manifest)
//
//	var fns struct {
//		Add      func(a, b int32) int32
//		Greeting func(name string) string `ffi:"adder_greeting"`
//	}
//
//	func Add(a, b int32) (int32, error) {
//		if err := adder.Bind(&fns); err != nil {
//			return 0, err
//		}
//		return fns.Add(a, b), nil
//	}
//
// Functions are called through purego (github.com/ebitengine/purego),
// which converts their arguments and results as the C ABI of the platform
// passes them: integers, floats, pointers, bools, strings (to and from
// NUL-terminated C strings) and, on some platforms, structs of those.
// Libraries are loaded once and never unloaded. ffi works on macOS, Linux
// and Windows on amd64 and arm64; elsewhere Load reports
// errors.ErrUnsupported.
package ffi

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/download"
	"github.com/egoist/mygo/internal/nativelib"
)

// Library is a native library, loaded on first use. Its methods are safe
// for concurrent use.
type Library struct {
	name     string
	manifest []byte

	once   sync.Once
	handle uintptr
	path   string
	err    error
}

// New returns the library named name in manifest, the content of the
// package's mygo-plugin.json, or nil when the package names none, which
// leaves the library to the app's resources, the executable's directory
// and $MYGO_NATIVE_<NAME>.
//
// name is the library's name without its prefix and extension: "adder" is
// libadder.dylib on macOS, libadder.so on Linux and adder.dll on Windows,
// unless the manifest's files name others.
func New(name string, manifest []byte) *Library {
	return &Library{name: name, manifest: manifest}
}

// Name returns the library's name.
func (l *Library) Name() string { return l.name }

// EnvVar returns the environment variable that overrides where a library is
// found: MYGO_NATIVE_ and its name in upper case, with characters other
// than letters and digits as underscores ("adder" is MYGO_NATIVE_ADDER).
func EnvVar(name string) string {
	return "MYGO_NATIVE_" + strings.Map(func(r rune) rune {
		if r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			return unicode.ToUpper(r)
		}
		return '_'
	}, name)
}

// Path returns the file Load loads, the first of:
//
//   - the file $MYGO_NATIVE_<NAME> names (see EnvVar);
//   - the one among the app's resources (mygo.PathResources), where `mygo
//     build` and `mygo dev` put it, signed with the app on macOS;
//   - the one next to the executable;
//
// and, in programs that are not packaged apps, as under `go run` and `go
// test`:
//
//   - the one the CLI built from sources (`mygo native build -target host`,
//     or `mygo dev`), in .mygo/native/<goos>-<goarch> of the working
//     directory's module;
//   - the published one in the user's cache (<cache>/mygo/natives/<sha256>/),
//     downloaded there when it is missing, checking its SHA-256.
//
// Packaged apps never download libraries: build them with the CLI.
func (l *Library) Path() (string, error) {
	if p := os.Getenv(EnvVar(l.name)); p != "" {
		return filepath.Abs(p)
	}
	lib := nativelib.Library{Name: l.name}
	if l.manifest != nil {
		m, err := nativelib.Parse(l.manifest)
		if err != nil {
			return "", fmt.Errorf("ffi: %s: %w", nativelib.ManifestFile, err)
		}
		var ok bool
		if lib, ok = m.Lookup(l.name); !ok {
			return "", fmt.Errorf("ffi: %s names no library %s", nativelib.ManifestFile, l.name)
		}
	}
	name := lib.FileName(runtime.GOOS, runtime.GOARCH)
	var dirs []string
	if dir, err := mygo.App.Path(mygo.PathResources); err == nil {
		dirs = append(dirs, dir)
	}
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
	}
	packaged := mygo.App.IsPackaged()
	if !packaged {
		dirs = append(dirs, localDirs()...)
	}
	for _, dir := range dirs {
		p := filepath.Join(dir, name)
		if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() {
			return p, nil
		}
	}
	if packaged {
		return "", fmt.Errorf("ffi: %s is missing from the app: build it with mygo build", name)
	}
	target := runtime.GOOS + "-" + runtime.GOARCH
	f, ok := lib.Files[target]
	if !ok {
		if lib.Build != nil {
			return "", fmt.Errorf("ffi: %s is not built for %s: run mygo native build -target host, or mygo dev", l.name, target)
		}
		return "", fmt.Errorf("ffi: %s has no build for %s", l.name, target)
	}
	cached, err := nativelib.CachePath(f)
	if err != nil {
		return "", err
	}
	if download.Check(cached, f.SHA256) {
		return cached, nil
	}
	if f.URL == "" {
		return "", fmt.Errorf("ffi: %s for %s is not published: run mygo native build", l.name, target)
	}
	if err := fetch(f, cached); err != nil {
		return "", fmt.Errorf("ffi: downloading %s: %w", f.Name, err)
	}
	return cached, nil
}

// localDirs returns where the CLI puts the libraries it builds in the module
// of the working directory, and of the directories above it up to that
// module's root.
func localDirs() []string {
	dir, err := os.Getwd()
	if err != nil {
		return nil
	}
	var dirs []string
	for {
		dirs = append(dirs, nativelib.LocalDir(dir, runtime.GOOS, runtime.GOARCH))
		parent := filepath.Dir(dir)
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil || parent == dir {
			return dirs
		}
		dir = parent
	}
}

// Load loads the library, once: later calls return the first one's error.
func (l *Library) Load() error {
	l.once.Do(func() {
		if !supported {
			l.err = fmt.Errorf("ffi: %s/%s: %w", runtime.GOOS, runtime.GOARCH, errors.ErrUnsupported)
			return
		}
		if l.path, l.err = l.Path(); l.err != nil {
			return
		}
		if l.handle, l.err = open(l.path); l.err != nil {
			l.err = fmt.Errorf("ffi: loading %s: %w", l.path, l.err)
		}
	})
	return l.err
}

// Symbol returns the address of the symbol named name, loading the library.
func (l *Library) Symbol(name string) (uintptr, error) {
	if err := l.Load(); err != nil {
		return 0, err
	}
	addr, err := symbol(l.handle, name)
	if err == nil && addr == 0 {
		err = errors.New("not found")
	}
	if err != nil {
		return 0, fmt.Errorf("ffi: %s has no symbol %s: %w", filepath.Base(l.path), name, err)
	}
	return addr, nil
}

// BindFunc sets the function fptr points to to call the library's function
// named name, loading the library.
func (l *Library) BindFunc(fptr any, name string) error {
	v := reflect.ValueOf(fptr)
	if v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Func {
		return fmt.Errorf("ffi: BindFunc needs a pointer to a function, not %T", fptr)
	}
	addr, err := l.Symbol(name)
	if err != nil {
		return err
	}
	return register(fptr, addr, name)
}

// Bind sets the function fields of the struct fns points to to call the
// library's functions, loading the library. A field's tag names its
// function, which is otherwise its name in snake case (AddNumbers calls
// add_numbers):
//
//	var fns struct {
//		Add     func(a, b int32) int32
//		Version func() string `ffi:"adder_version"`
//		Extra   func()        `ffi:"adder_extra,optional"` // nil when missing
//		Cache   map[string]int                               // not a function: left alone
//		Hidden  func()        `ffi:"-"`                       // left alone
//	}
//
// Binding again, as on every call of a function that needs fns, does
// nothing once all of them are bound.
func (l *Library) Bind(fns any) error {
	v := reflect.ValueOf(fns)
	if v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("ffi: Bind needs a pointer to a struct, not %T", fns)
	}
	if err := l.Load(); err != nil {
		return err
	}
	s := v.Elem()
	for i := range s.NumField() {
		field, fv := s.Type().Field(i), s.Field(i)
		tag := field.Tag.Get("ffi")
		if fv.Kind() != reflect.Func || tag == "-" || !fv.IsNil() {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if name == "" {
			name = SnakeCase(field.Name)
		}
		if !field.IsExported() {
			return fmt.Errorf("ffi: Bind: field %s of %s is not exported", field.Name, s.Type())
		}
		addr, err := l.Symbol(name)
		if err != nil {
			if opts == "optional" {
				continue
			}
			return err
		}
		if err := register(fv.Addr().Interface(), addr, name); err != nil {
			return err
		}
	}
	return nil
}

// SnakeCase returns the name of the function Bind binds to a field without
// a tag: AddNumbers is add_numbers, and ParseURL parse_url.
func SnakeCase(name string) string {
	var b strings.Builder
	rs := []rune(name)
	for i, r := range rs {
		if unicode.IsUpper(r) {
			// A word starts at an upper-case letter after a lower-case one
			// or a digit, or before a lower-case one in a run of capitals.
			if i > 0 && (!unicode.IsUpper(rs[i-1]) || i+1 < len(rs) && unicode.IsLower(rs[i+1])) && rs[i-1] != '_' {
				b.WriteByte('_')
			}
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}

// fetch downloads a published library into the user's cache.
var fetch = func(f nativelib.File, path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return download.Cached(ctx, download.File{URL: f.URL, SHA256: f.SHA256}, path)
}
