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
    { "name": "adder", "build": { "cargo": "rust" } }
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
the module, not in the crate.

## A Zig package

The same, with `"build": {"zig": "<dir>"}` naming the directory of a
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
directory. A `build.zig` that compiles C sources (`addCSourceFiles`) binds
a C library the same way, as the [SQLite plugin](plugins/sqlite.md) does.

`"args"` adds arguments to `cargo build` or `zig build`:

```json
{ "name": "adder", "build": { "cargo": "rust", "args": ["--features", "simd"] } }
```

## Binding functions

`Library.Bind` takes a pointer to a struct of functions, which it binds the
first time and leaves alone afterwards, so calling it before each use costs
little. A field calls the library's function named by its tag, or else its
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
the library returns is copied into Go but not freed, so return static strings, or export a function that
frees what the library allocated and call it. Libraries are loaded once and
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

So `go run` and `go test` find the library once the CLI built it for the
machine:

```sh
mygo native build -target host   # or run mygo dev once
go test ./adder
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
with the [GitHub CLI](https://cli.github.com), creating it if needed. Then
commit `mygo-plugin.json` and tag your module.

| Flag | |
|---|---|
| `-target` | the platforms, `GOOS-GOARCH` separated by commas, `all` (the default) or `host` |
| `-release` | the URL of the directory the files are published in (default: the library's `release`) |
| `-out` | the directory of the files to publish (default: `.mygo/native/release` of the module) |
| `-upload` | uploads the files to the GitHub release of `-release` |

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
Ubuntu 20.04 and later, and macOS libraries support the app's
`macOS.minimumSystemVersion`, or macOS 12 and later.

## mygo-plugin.json

| Field | |
|---|---|
| `libraries[].name` | the library: `ffi.New`'s name, and its files' (`lib<name>.dylib`, `lib<name>.so`, `<name>.dll`, a leading `lib` dropped on Windows) |
| `libraries[].build.cargo` | the directory of the Rust crate, relative to the package |
| `libraries[].build.zig` | the directory of the Zig package, relative to the package |
| `libraries[].build.args` | more arguments of `cargo build` or `zig build` |
| `libraries[].release` | where the published files are |
| `libraries[].files` | the published files by `GOOS-GOARCH`: `name`, `url` and `sha256`, written by `mygo native build` |
| `libraries[].source` | where the sources of the published files come from, for people |

A package may publish libraries without `build`, built by any means, by
writing their `files` itself, as the [terminal plugin](plugins/terminal.md)
does for Ghostty's.
