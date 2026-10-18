package main

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/egoist/mygo/internal/nativelib"
)

// cargoTriple returns the Rust target of goos/goarch. Windows libraries
// are built with MSVC's toolchain on Windows and with MinGW's elsewhere,
// which cargo-zigbuild provides.
func cargoTriple(goos, goarch string) (string, error) {
	arch := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[goarch]
	if arch == "" {
		return "", fmt.Errorf("cannot build native libraries for %s/%s", goos, goarch)
	}
	switch goos {
	case "darwin":
		return arch + "-apple-darwin", nil
	case "linux":
		return arch + "-unknown-linux-gnu", nil
	case "windows":
		if runtime.GOOS == "windows" {
			return arch + "-pc-windows-msvc", nil
		}
		if goarch == "arm64" {
			return "aarch64-pc-windows-gnullvm", nil
		}
		return "x86_64-pc-windows-gnu", nil
	}
	return "", fmt.Errorf("cannot build native libraries for %s/%s", goos, goarch)
}

// zigTriple returns the Zig target of goos/goarch: Linux libraries link
// against the glibc of Debian 10 and Ubuntu 20.04, and run on later ones.
func zigTriple(goos, goarch string) (string, error) {
	arch := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[goarch]
	switch {
	case arch == "":
	case goos == "darwin":
		return arch + "-macos", nil
	case goos == "linux":
		return arch + "-linux-gnu.2.28", nil
	case goos == "windows":
		return arch + "-windows-gnu", nil
	}
	return "", fmt.Errorf("cannot build native libraries for %s/%s", goos, goarch)
}

// buildNative builds lib, which the manifest of the package in dir names,
// from its sources for goos/goarch, and returns the built file, in the
// module's .mygo/native/<goos>-<goarch> (nativelib.LocalDir), where
// programs that are not packaged apps find it too. macOS libraries support
// macOS minMacOS and later ("" is 12.0). cargo and zig build only what
// changed, so building again is quick.
func buildNative(dir string, lib nativelib.Library, goos, goarch, minMacOS string) (string, error) {
	root, err := nativelib.ModuleRoot(dir)
	if err != nil {
		return "", err
	}
	tool, src := lib.Build.Tool()
	src = filepath.Join(dir, filepath.FromSlash(src))
	t := con.start(fmt.Sprintf("Building %s for %s/%s with %s", lib.Name, goos, goarch, tool))
	var built string
	if tool == "cargo" {
		built, err = buildCargo(root, src, lib, goos, goarch, minMacOS)
	} else {
		built, err = buildZig(root, src, lib, goos, goarch)
	}
	var out string
	if err == nil {
		out = filepath.Join(nativelib.LocalDir(root, goos, goarch), lib.FileName(goos, goarch))
		err = replaceFile(built, out)
	}
	if t.end(err, fmt.Sprintf("Built %s for %s/%s", lib.Name, goos, goarch)) != nil {
		return "", fmt.Errorf("building %s (%s): %w", lib.Name, filepath.Join(dir, pluginFile), err)
	}
	return out, nil
}

// buildCargo builds the cdylib of the crate in src with cargo, or
// cargo-zigbuild when it is installed and the target is Linux or another
// platform, which cargo alone cannot link for.
func buildCargo(root, src string, lib nativelib.Library, goos, goarch, minMacOS string) (string, error) {
	triple, err := cargoTriple(goos, goarch)
	if err != nil {
		return "", err
	}
	if _, err := exec.LookPath("cargo"); err != nil {
		return "", errors.New("cargo is not installed: get it from https://rustup.rs")
	}
	// cargo-zigbuild links Linux libraries against an old glibc, as
	// zigTriple, so that they load on older systems than the one building.
	cmd, target := "build", triple
	if goos == "linux" || goos != runtime.GOOS || goarch != runtime.GOARCH {
		if _, err := exec.LookPath("cargo-zigbuild"); err == nil {
			cmd = "zigbuild"
			if goos == "linux" {
				target += ".2.28"
			}
		}
	}
	manifest := filepath.Join(src, "Cargo.toml")
	args := append([]string{cmd, "--lib", "--release", "--manifest-path", manifest, "--target", target, "--message-format=json-render-diagnostics"}, lib.Build.Args...)
	c := exec.Command("cargo", args...)
	c.Env = append(os.Environ(), "CARGO_TARGET_DIR="+filepath.Join(root, ".mygo", "native", "cargo"))
	if goos == "darwin" {
		c.Env = append(c.Env, "MACOSX_DEPLOYMENT_TARGET="+cmp.Or(minMacOS, "12.0"))
	}
	var stdout, stderr bytes.Buffer
	c.Stdout, c.Stderr = &stdout, &stderr
	if err := c.Run(); err != nil {
		msg := tail(stderr.String(), 40)
		if strings.Contains(msg, "target may not be installed") {
			msg += "\n\nInstall the target with: rustup target add " + triple
		} else if cmd == "build" && (goos != runtime.GOOS || goarch != runtime.GOARCH) {
			msg += "\n\ncargo-zigbuild (cargo install cargo-zigbuild) links for other platforms with Zig"
		}
		return "", fmt.Errorf("cargo %s: %v\n%s", cmd, err, msg)
	}
	// The messages name what cargo built; the crate's own cdylib is the
	// library.
	want, err := filepath.Abs(manifest)
	if err != nil {
		return "", err
	}
	ext := nativelib.Ext(goos)
	var found string
	sc := bufio.NewScanner(&stdout)
	sc.Buffer(nil, 16<<20)
	for sc.Scan() {
		var m struct {
			Reason       string `json:"reason"`
			ManifestPath string `json:"manifest_path"`
			Target       struct {
				Kind []string `json:"kind"`
			} `json:"target"`
			Filenames []string `json:"filenames"`
		}
		if json.Unmarshal(sc.Bytes(), &m) != nil || m.Reason != "compiler-artifact" || !slices.Contains(m.Target.Kind, "cdylib") || !sameFile(m.ManifestPath, want) {
			continue
		}
		for _, f := range m.Filenames {
			if strings.HasSuffix(f, ext) {
				found = f
			}
		}
	}
	if found == "" {
		return "", fmt.Errorf("%s builds no dynamic library: add crate-type = [\"cdylib\"] under [lib]", manifest)
	}
	return found, nil
}

// buildZig builds the package in src with zig build, which installs the
// library into a prefix of its own.
func buildZig(root, src string, lib nativelib.Library, goos, goarch string) (string, error) {
	triple, err := zigTriple(goos, goarch)
	if err != nil {
		return "", err
	}
	if _, err := exec.LookPath("zig"); err != nil {
		return "", errors.New("zig is not installed: get it from https://ziglang.org/download")
	}
	prefix := filepath.Join(root, ".mygo", "native", "zig", lib.Name, goos+"-"+goarch)
	if err := os.RemoveAll(prefix); err != nil {
		return "", err
	}
	args := append([]string{"build", "-Dtarget=" + triple, "-Doptimize=ReleaseFast", "--prefix", prefix,
		"--cache-dir", filepath.Join(root, ".mygo", "native", "zig-cache")}, lib.Build.Args...)
	c := exec.Command("zig", args...)
	c.Dir = src
	var out bytes.Buffer
	c.Stdout, c.Stderr = &out, &out
	if err := c.Run(); err != nil {
		return "", fmt.Errorf("zig build: %v\n%s", err, tail(out.String(), 40))
	}
	// Windows libraries go into bin, beside their import libraries in lib.
	ext, want := nativelib.Ext(goos), lib.FileName(goos, goarch)
	var found []string
	for _, sub := range []string{"lib", "bin"} {
		entries, _ := os.ReadDir(filepath.Join(prefix, sub))
		for _, e := range entries {
			if filepath.Ext(e.Name()) != ext {
				continue
			}
			path := filepath.Join(prefix, sub, e.Name())
			if e.Name() == want {
				return path, nil
			}
			found = append(found, path)
		}
	}
	if len(found) == 1 {
		return found[0], nil
	}
	if len(found) == 0 {
		return "", fmt.Errorf("zig build installed no %s library: add b.installArtifact(b.addLibrary(.{.linkage = .dynamic, ...})) to build.zig", ext)
	}
	return "", fmt.Errorf("zig build installed several libraries and none is %s: %s", want, strings.Join(found, ", "))
}

// replaceFile copies src, following links, to dst through a temporary file,
// as dst may be loaded by a running program.
func replaceFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	if err := copyFile(src, tmp, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func sameFile(a, b string) bool {
	ia, err := os.Stat(a)
	if err != nil {
		return false
	}
	ib, err := os.Stat(b)
	return err == nil && os.SameFile(ia, ib)
}

// tail returns the last n lines of s.
func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
