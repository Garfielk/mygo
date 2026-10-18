# Native libraries

An app can call code written in Rust, Zig, C or any language that builds a
dynamic library with a C ABI, without cgo: package `ffi` loads the library
and binds its functions to Go functions, and the CLI builds the library
from its sources, puts it into the app and publishes it for the users of
your Go package.

The [`rust-native` example](../examples/rust-native) is a window of native
UI that calls a Rust crate.

## A Rust crate

Put the crate next to the Go package that binds it, and make it a `cdylib`:

```
adder/
├── adder.go
├── mygo-plugin.json
└── rust/
    ├── Cargo.toml
    └── src/lib.rs
```

```toml
# adder/rust/Cargo.toml
[package]
name = "adder"
version = "0.1.0"
edition = "2021"

[lib]
crate-type = ["cdylib"]
```

```rust
// adder/rust/src/lib.rs
#[no_mangle]
pub extern "C" fn add(a: i32, b: i32) -> i32 {
    a + b
}
```

`mygo-plugin.json` tells the CLI how to build it:

```json
{
  "libraries": [
    { "name": "adder", "build": { "cargo": "./rust" } }
  ]
}
```

The Go package embeds it and binds the functions:

```go
package adder

import (
	_ "embed"

	"github.com/egoist/mygo/ffi"
)

//go:embed mygo-plugin.json
var manifest []byte

var lib = ffi.New("adder", manifest)

var fns struct {
	Add func(a, b int32) int32
}

func Add(a, b int32) (int32, error) {
	if err := lib.Bind(&fns); err != nil {
		return 0, err
	}
	return fns.Add(a, b), nil
}
```

That is all: `mygo dev` builds the crate with cargo, puts the library into
the app and rebuilds it when the crate changes, and `mygo build` builds it
for the platform of each app. cargo keeps its files in `.mygo/native` of
the module, not in the crate: keep `.mygo/` in your `.gitignore`, as the
projects `mygo init` makes do. `mygo doctor` tells which of cargo, Zig and
cargo-zigbuild are installed.

## A Zig package

The same, with `"build": {"zig": "./zig"}` naming the directory of a
`build.zig` that installs a dynamic library:

```zig
// mul/zig/build.zig
const std = @import("std");

pub fn build(b: *std.Build) void {
    const module = b.createModule(.{
        .root_source_file = b.path("src/root.zig"),
        .target = b.standardTargetOptions(.{}),
        .optimize = b.standardOptimizeOption(.{}),
        .strip = true,
    });
    b.installArtifact(b.addLibrary(.{ .name = "mul", .linkage = .dynamic, .root_module = module }));
}
```

The CLI runs `zig build -Dtarget=<target> -Doptimize=ReleaseFast` in that
directory, and cargo with `--release`. Zig 0.16 keeps the packages a
`build.zig.zon` fetches in `zig-pkg/` there: add it to `.gitignore`. A
Zig package whose `build.zig.zon` pins another (a library's sources, as
Ghostty's for the [terminal plugin](plugins/terminal.md)) builds it the
same way. A `build.zig` that compiles C sources (`addCSourceFiles`) binds
a C library the same way, as the [SQLite plugin](plugins/sqlite.md) does.

`"args"` adds arguments to `cargo build` or `zig build`, and replaces the
release profile or the optimize mode when it names one (`--profile`,
`-Doptimize=ReleaseSmall`), and `"macOS"` names the oldest macOS a library
runs on when it needs a later one than the app's minimum or 12:

```json
{ "name": "adder", "build": { "cargo": "./rust", "args": ["--features", "simd"], "macOS": "13.0" } }
```

The CLI removes debug information from the Linux libraries it builds, as
`strip --strip-debug` would, because Zig can leave it in even when
`build.zig` asks to strip: libghostty-vt would be 10 MB instead of 2.4 MB.
Exported functions stay, so `ffi` still finds them.

## Binding functions

`Library.Bind` takes a pointer to a struct of functions, which it binds the
first time; later calls with the same pointer, from any goroutine, return
the first one's result without binding again, so calling it before each
use, as above, is safe and cheap. A function bound to nothing, as an
unknown tag option or a missing function, makes it return an error. A field calls the library's function named by its tag, or else its
name in snake case (`CountVowels` calls `count_vowels`):

```go
var fns struct {
	Add         func(a, b int32) int32
	CountVowels func(s string) uint32
	Version     func() string `ffi:"adder_version"`
	Extra       func()        `ffi:"adder_extra,optional"` // nil when the library lacks it
}
```

`BindFunc` binds one function, and `Symbol` returns the address of any
symbol, for `purego.SyscallN` or a pointer to data.

Functions are called through [purego](https://github.com/ebitengine/purego),
on the calling goroutine, and pass what C does:

| Go | C | Rust |
|---|---|---|
| `int8` … `int64`, `uint8` … `uint64`, `int`, `uintptr` | integers of the same size | `i8` … `i64`, `u8` … `u64`, `isize`, `usize` |
| `float32`, `float64` | `float`, `double` | `f32`, `f64` |
| `bool` | `bool` | `bool` |
| `string` argument | `const char *`, valid during the call | `*const c_char` |
| `string` result | `const char *`, copied | `*const c_char` |
| `unsafe.Pointer`, `*T` | pointers | `*mut T`, `*const T` |
| `[]byte` | a pointer to the first byte, valid during the call | `*const u8` (pass the length too) |
| structs of the types above | structs, whose padding the Go struct must spell out | `#[repr(C)]` structs |

The library must not keep Go memory past the call, as with cgo. A string
the library returns is copied into Go but not freed, so return static
strings, or return a pointer (`unsafe.Pointer`) and export a function that
frees what the library allocated:

```rust
#[no_mangle]
pub extern "C" fn greeting(name: *const c_char) -> *mut c_char {
    let name = unsafe { CStr::from_ptr(name) }.to_string_lossy();
    CString::new(format!("Hello, {name}")).unwrap().into_raw()
}

#[no_mangle]
pub extern "C" fn free_string(s: *mut c_char) {
    if !s.is_null() {
        drop(unsafe { CString::from_raw(s) });
    }
}
```

```go
var fns struct {
	Greeting   func(name string) unsafe.Pointer
	FreeString func(s unsafe.Pointer)
}

func Greeting(name string) string {
	p := fns.Greeting(name)
	defer fns.FreeString(p)
	return ffi.GoString(p) // copies it
}
```
 Libraries are loaded once and
stay loaded. `ffi` works on macOS, Linux and Windows on amd64 and arm64;
elsewhere, as in browsers, `Load` returns an error that wraps
`errors.ErrUnsupported`.

## Where libraries are found

`Library.Load` loads the first of:

1. the file `$MYGO_NATIVE_<NAME>` names (`MYGO_NATIVE_ADDER`);
2. the one among the app's resources, where `mygo dev` and `mygo build` put
   it, signed with the app on macOS;
3. the one next to the executable;

and, in programs that are not packaged apps:

4. the one the CLI built, in `.mygo/native/<goos>-<goarch>` of the module
   of the working directory;
5. the published one, downloaded once into the user's cache and checked
   against its SHA-256.

Packaged apps never download libraries: the production builds of `mygo
build` leave the downloader out, and HTTP with it.

## Testing

`go run` and `go test` find the library once the CLI built it for the
machine. A `go:generate` line in the binding package builds it:

```go
//go:generate go tool mygo native build -target host .
```

```sh
go generate ./... && go test ./...
```

Without a build of its own, a program downloads the published build,
which may be older than the sources. In CI, build it before the tests;
GitHub's runners have cargo:

```yaml
- run: go tool mygo native build -target host ./adder
- run: go test ./...
```

## Publishing a package

Apps whose packages are their own, or in a module they replace with a
directory, build libraries from their sources. Apps that use your package
from the module cache download its published builds instead, so they need
neither Rust nor Zig. `mygo native build` makes them:

```sh
mygo native build -release https://github.com/me/adder/releases/download/adder-v1 ./adder
```

It builds the library for macOS, Linux and Windows on amd64 and arm64,
writes the files into `.mygo/native/release` of the module, and writes
each one's name, URL and SHA-256 into `mygo-plugin.json`, with the release,
which later runs reuse. `-upload` uploads the files to that GitHub release
with the [GitHub CLI](https://cli.github.com), creating it if needed,
before writing `mygo-plugin.json`. Then commit `mygo-plugin.json` and tag
your module.

The files' names carry their SHA-256 (`adder-linux-amd64-1a2b3c4d5e6f.so`),
so publishing new builds into the same release never replaces a file that
an earlier version of your module points at. Platforms that fail to build
are listed at the end, and left as they were in `mygo-plugin.json`.

Each file records the hash of the sources it was built from
(`sourceHash`: the files of the build's directory, its `args` and
`macOS`). Platforms whose files were built from the sources as they are
are skipped (`-force` builds them again), and `mygo dev` and `mygo build`
use those files rather than build the library, even in your own module,
until the sources change. So people who work on your app or package but
not on its library need neither Rust nor Zig.

| Flag | |
|---|---|
| `-target` | the platforms, `GOOS-GOARCH` separated by commas, `all` (the default) or `host` |
| `-release` | the URL of the directory the files are published in (default: the library's `release`) |
| `-out` | the directory of the files to publish (default: `.mygo/native/release` of the module) |
| `-upload` | uploads the files to the GitHub release of `-release` |
| `-force` | builds the platforms whose published files were built from the sources as they are too |

Building for other platforms needs their toolchains:

- **Zig** cross-compiles to every platform, macOS included.
- **Rust** needs each target (`rustup target add aarch64-unknown-linux-gnu`
  …) and a linker for it: with
  [cargo-zigbuild](https://github.com/rust-cross/cargo-zigbuild)
  installed, the CLI links Linux and Windows libraries with Zig. macOS
  libraries need Apple's SDK: build them on a Mac.

Builds of some platforms add to those already in `mygo-plugin.json`, so the
libraries of each platform can be built where it is easiest, as on a Mac
and then on Linux:

```sh
mygo native build -target darwin-arm64,darwin-amd64 -upload ./adder   # on a Mac
mygo native build -target linux-amd64,linux-arm64,windows-amd64,windows-arm64 -upload ./adder
```

Linux libraries are linked against glibc 2.28 (by Zig, and by
cargo-zigbuild when it is installed), so that they load on Debian 10,
Ubuntu 20.04 and later. macOS libraries that `mygo dev` and `mygo build`
build support the app's `macOS.minimumSystemVersion`, and published ones
macOS 12 and later.

## mygo-plugin.json

The manifest sits next to the Go package that binds the libraries, which
embeds it for `ffi.New`. The CLI finds it in every package an app imports.
A library being published looks like this:

```json
{
  "libraries": [
    {
      "name": "adder",
      "build": { "cargo": "./rust", "args": ["--features", "simd"], "macOS": "13.0" },
      "release": "https://github.com/me/adder/releases/download/adder-v1",
      "files": {
        "linux-amd64": {
          "name": "libadder.so",
          "url": "https://github.com/me/adder/releases/download/adder-v1/adder-linux-amd64-1a2b3c4d5e6f.so",
          "sha256": "1a2b3c4d5e6f…",
          "sourceHash": "9f8e7d6c5b4a…"
        }
      }
    }
  ]
}
```

You write `name` and `build`. `mygo native build` writes `release` (from
`-release`) and `files`.

### name

Identifies the library everywhere:

- `ffi.New("adder", manifest)` finds the library's entry by it.
- The library's files are named after it: `libadder.dylib`, `libadder.so`
  and `adder.dll`. A leading `lib` is not doubled, and is dropped on
  Windows, so `libghostty-vt` is `ghostty-vt.dll`. A file in `files` may
  name itself otherwise.
- `MYGO_NATIVE_ADDER` (`ffi.EnvVar`) names a file to load instead.
- Published files are named after it: `adder-linux-amd64-<sha256>.so`.
- An app ships one library of each name, even when several of its
  packages name it.

It may hold letters, digits, `-`, `_` and `.`.

### build

How to build the library from sources in the package's directory; leave
it out for a library you publish by other means.

| Field | |
|---|---|
| `cargo` | the directory of a Rust crate (its `Cargo.toml`), as `"./rust"`, whose `crate-type` includes `"cdylib"` |
| `zig` | the directory of a Zig package (its `build.zig`), as `"./zig"`, which installs a dynamic library |
| `args` | more arguments of `cargo build` or `zig build`; one naming a profile (`--profile`) or an optimize mode (`-Doptimize=…`) replaces the release one |
| `macOS` | the oldest macOS the library runs on, when it needs a later one than the app's minimum or 12.0 |

Name exactly one of `cargo` and `zig`. The directory must be inside the
package. When the package is the app's own, or in a module the app
replaces with a directory, `mygo dev` and `mygo build` build the library
for each target, unless a published file was built from the sources as
they are (see `files`).

### release

The URL of the directory the published files go into, as
`https://github.com/<owner>/<repo>/releases/download/<tag>` for a GitHub
release. `mygo native build -release <url>` sets it and later runs reuse
it. Each file's `url` is `release` and the file's name, and `-upload`
uploads the files to the release this URL names. Apps never read it:
each file has its full `url`.

### files

The published builds, by `GOOS-GOARCH` (`darwin-arm64`, `linux-amd64`,
`windows-arm64` …), which apps get when they do not build the library:

| Field | |
|---|---|
| `name` | the file in apps, as `libadder.so` |
| `url` | where to download it |
| `sha256` | its SHA-256: a download that does not match is refused, and the file stays in the user's cache under it |
| `sourceHash` | the hash of the sources it was built from: the build directory's files (but hidden ones and `target`, `zig-out`, `zig-pkg`, `node_modules`, with LF line endings), `args` and `macOS` |

While `sourceHash` matches the sources, `mygo dev` and `mygo build` use
the published file, and `mygo native build` skips that platform. A package
may publish libraries without `build`, built by any means, by writing
`files` itself, without `sourceHash`. The official plugins build theirs
with Zig: [SQLite](plugins/sqlite.md) from its amalgamation, and the
[terminal](plugins/terminal.md) from Ghostty's sources.
