package main

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	fetch "github.com/egoist/mygo/internal/download"
	"github.com/egoist/mygo/internal/nativelib"
)

const nativeUsage = `Usage: mygo native build [flags] [dir]

Builds the native libraries that the packages in dir (default ".") name
in their mygo-plugin.json from their sources (a Rust crate with cargo, a
Zig package with zig build) for every platform, and writes the published
builds into mygo-plugin.json: their names, SHA-256 and URLs under the
library's release. The files to publish go into .mygo/native/release of
the module, named after their SHA-256 so that a release never replaces a
file that published versions point at; -upload publishes them as the
assets of a GitHub release, before writing mygo-plugin.json.

Platforms that do not build here are reported, and left as they are in
mygo-plugin.json: Rust libraries for macOS build on a Mac only, so build
them there with -target darwin-arm64,darwin-amd64, and the others
elsewhere; each run adds its platforms to the manifest.

mygo dev and mygo build build the libraries of the app's own packages
themselves; apps that use a package from the module cache download its
published builds instead, so they need neither Rust nor Zig.

With -target host, it builds this machine's library only, for go run and
go test, which find it in .mygo/native/<goos>-<goarch> of the module, and
leaves mygo-plugin.json alone.
`

func runNative(args []string) error {
	if len(args) == 0 || args[0] != "build" {
		fmt.Fprint(os.Stderr, nativeUsage)
		if len(args) > 0 && (args[0] == "-h" || args[0] == "--help" || args[0] == "help") {
			return nil
		}
		return errors.New("mygo native: unknown subcommand; want build")
	}
	flags := newFlags("native build", "[dir]", "Builds the native libraries of the packages in dir for every platform, ready to publish.")
	target := flags.String("target", "all", `the platforms, GOOS-GOARCH separated by commas, "all" or "host"`)
	release := flags.String("release", "", "the URL the files are published under, as https://github.com/<owner>/<repo>/releases/download/<tag>\n(default: the library's release in mygo-plugin.json)")
	out := flags.String("out", "", "the directory of the files to publish (default: .mygo/native/release in the module)")
	upload := flags.Bool("upload", false, "upload the files to the GitHub release of -release with the GitHub CLI")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return fmt.Errorf("unexpected arguments %q: flags go before the directory (mygo native build [flags] [dir])", flags.Args()[1:])
	}
	dir, err := filepath.Abs(dirArg(flags.Args()))
	if err != nil {
		return err
	}
	targets, err := nativeTargets(*target)
	if err != nil {
		return err
	}
	host := *target == "host"
	if host && *upload {
		return errors.New("-upload publishes the builds of every platform: leave -target out")
	}
	pkgs, err := nativePackagesIn(dir)
	if err != nil {
		return err
	}
	var libs []*nativelib.Library
	for _, p := range pkgs {
		for i := range p.manifest.Libraries {
			if l := &p.manifest.Libraries[i]; l.Build != nil {
				if *upload && cmp.Or(*release, l.Release) == "" {
					return fmt.Errorf("-upload: %s has no release: name one with -release", l.Name)
				}
				libs = append(libs, l)
			}
		}
	}
	if len(libs) == 0 {
		return fmt.Errorf("no %s in %s names a library to build (\"build\": {\"cargo\": \"<dir>\"} or {\"zig\": \"<dir>\"})", pluginFile, dir)
	}
	if !host {
		warnNativeRelease(pkgs, targets)
	}

	// Build every platform that builds here, then publish them, and only
	// then name them in the manifests, so that these never point at files
	// that are not there.
	var assets, failed []string
	uploads := map[string][]string{} // the files of each release
	changed := map[string]bool{}     // the packages whose manifest to write
	for _, p := range pkgs {
		for i := range p.manifest.Libraries {
			l := &p.manifest.Libraries[i]
			if l.Build == nil {
				continue
			}
			r := cmp.Or(*release, l.Release)
			for _, t := range targets {
				goos, goarch, _ := strings.Cut(t, "-")
				path, err := buildNative(p.dir, *l, goos, goarch, "")
				if err == nil && !host {
					var a string
					var f nativelib.File
					if a, f, err = publishNative(p.dir, *out, *l, path, goos, goarch, r); err == nil {
						if l.Files == nil {
							l.Files = map[string]nativelib.File{}
						}
						l.Files[t] = f
						assets = append(assets, a)
						if r != "" {
							uploads[r] = append(uploads[r], a)
						}
					}
				}
				if err != nil {
					if host {
						return err
					}
					failed = append(failed, fmt.Sprintf("%s for %s: %v", l.Name, t, err))
				}
			}
			if !host {
				l.Release = r
				changed[p.dir] = true
			}
		}
	}
	if host {
		return nil
	}
	if len(assets) > 0 && *upload {
		for r, files := range uploads {
			if err := uploadNatives(r, files); err != nil {
				return fmt.Errorf("%w\n%s is unchanged", err, pluginFile)
			}
		}
	}
	for _, p := range pkgs {
		if !changed[p.dir] || len(assets) == 0 {
			continue
		}
		if err := writeManifest(filepath.Join(p.dir, pluginFile), p.manifest); err != nil {
			return err
		}
		logf("Wrote %s", cyan(filepath.Join(p.dir, pluginFile)))
	}
	for _, l := range libs {
		if l.Release == "" && len(assets) > 0 {
			logf("%s has no release: set it with -release so that apps can download its files", l.Name)
		}
	}
	if len(assets) > 0 && !*upload {
		logf("Publish %s under the release of each library, or run again with -upload", cyan(filepath.Dir(assets[0])))
	}
	if len(failed) > 0 {
		return fmt.Errorf("not built, and left as they were in %s:\n%s", pluginFile, strings.Join(failed, "\n\n"))
	}
	return nil
}

// warnNativeRelease warns of what makes published libraries load on fewer
// systems than they could, or a module that would commit the builds.
func warnNativeRelease(pkgs []nativePackage, targets []string) {
	linux := slices.ContainsFunc(targets, func(t string) bool { return strings.HasPrefix(t, "linux-") })
	roots := map[string]bool{}
	for _, p := range pkgs {
		for _, l := range p.manifest.Libraries {
			if l.Build == nil {
				continue
			}
			if tool, _ := l.Build.Tool(); tool == "cargo" && linux && !zigbuild("linux", "amd64") {
				logf("Without cargo-zigbuild (and Zig), %s's Linux libraries need the glibc of this machine or later: cargo install cargo-zigbuild", l.Name)
				linux = false
			}
		}
		root, err := nativelib.ModuleRoot(p.dir)
		if err != nil || roots[root] {
			continue
		}
		roots[root] = true
		data, _ := os.ReadFile(filepath.Join(root, ".gitignore"))
		if !strings.Contains(string(data), ".mygo") {
			logf("Add .mygo/ to %s: the builds go into %s", filepath.Join(root, ".gitignore"), filepath.Join(root, ".mygo"))
		}
	}
}

// nativeTargets returns the platforms -target names.
func nativeTargets(s string) ([]string, error) {
	switch s {
	case "all", "":
		return nativelib.Targets, nil
	case "host":
		return []string{runtime.GOOS + "-" + runtime.GOARCH}, nil
	}
	list := splitList(s)
	for _, t := range list {
		if !slices.Contains(nativelib.Targets, t) {
			return nil, fmt.Errorf("-target: unknown platform %q, want one of %s", t, strings.Join(nativelib.Targets, ", "))
		}
	}
	return list, nil
}

// nativePackagesIn returns the package in dir when it names native
// libraries, or else the packages under dir that do.
func nativePackagesIn(dir string) ([]nativePackage, error) {
	if _, err := os.Stat(filepath.Join(dir, pluginFile)); err == nil {
		return readNativePackages([]string{dir})
	}
	var out, stderr bytes.Buffer
	cmd := goCommand(dir, nil, "list", "-e", "-f", "{{.Dir}}", "./...")
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("go list: %v\n%s", err, strings.TrimSpace(stderr.String()))
	}
	return readNativePackages(strings.Split(out.String(), "\n"))
}

// publishNative copies a library built for goos/goarch into the directory
// of the files to publish, as <name>-<goos>-<goarch>-<sha256[:12]><ext>,
// and into the
// user's cache, where apps on this machine find it before it is published,
// and returns the copy and its entry in the manifest.
func publishNative(dir, out string, l nativelib.Library, built, goos, goarch, release string) (string, nativelib.File, error) {
	if out == "" {
		root, err := nativelib.ModuleRoot(dir)
		if err != nil {
			return "", nativelib.File{}, err
		}
		out = filepath.Join(root, ".mygo", "native", "release")
	}
	// The SHA-256 is part of the name: a release never gets another file
	// under a name that published versions of the package point at.
	sum, err := fetch.FileSHA256(built)
	if err != nil {
		return "", nativelib.File{}, err
	}
	prefix := filepath.Join(out, l.Name+"-"+goos+"-"+goarch+"-")
	asset := prefix + sum[:12] + nativelib.Ext(goos)
	// Earlier builds would be published again with this one.
	old, _ := filepath.Glob(prefix + "*" + nativelib.Ext(goos))
	for _, f := range old {
		if f != asset {
			os.Remove(f)
		}
	}
	if err := replaceFile(built, asset); err != nil {
		return "", nativelib.File{}, err
	}
	f := nativelib.File{Name: l.FileName(goos, goarch), SHA256: sum}
	if release != "" {
		f.URL = strings.TrimSuffix(release, "/") + "/" + filepath.Base(asset)
	}
	cached, err := nativelib.CachePath(f)
	if err == nil && !fetch.Check(cached, sum) {
		err = replaceFile(asset, cached)
	}
	return asset, f, err
}

// writeManifest writes a package's mygo-plugin.json.
func writeManifest(path string, m nativelib.Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// githubRelease returns the repository and the tag of a GitHub release's
// download URL: https://github.com/<owner>/<repo>/releases/download/<tag>.
func githubRelease(release string) (repo, tag string, err error) {
	u, err := url.Parse(release)
	var parts []string
	if err == nil {
		parts = strings.Split(strings.Trim(u.Path, "/"), "/")
	}
	if err != nil || u.Host != "github.com" || len(parts) != 5 || parts[2] != "releases" || parts[3] != "download" {
		return "", "", fmt.Errorf("-upload needs the release of a GitHub repository, https://github.com/<owner>/<repo>/releases/download/<tag>, not %q", release)
	}
	return parts[0] + "/" + parts[1], parts[4], nil
}

// uploadNatives uploads files to a GitHub release, created if needed (not
// as the repository's latest release, which is its apps'), with the gh CLI.
func uploadNatives(release string, files []string) error {
	repo, tag, err := githubRelease(release)
	if err != nil {
		return err
	}
	gh, err := exec.LookPath("gh")
	if err != nil {
		return errors.New("-upload needs the GitHub CLI (https://cli.github.com), signed in with gh auth login or GH_TOKEN")
	}
	if exec.Command(gh, "release", "view", tag, "--repo", repo).Run() != nil {
		t := con.start("Creating the release " + tag + " of " + repo)
		out, err := exec.Command(gh, "release", "create", tag, "--repo", repo, "--title", tag, "--notes", "Native libraries built by mygo native build.", "--latest=false").CombinedOutput()
		if t.end(err, "Created the release "+tag+" of "+repo) != nil {
			return fmt.Errorf("gh release create: %v\n%s", err, out)
		}
	}
	// The files' names carry their SHA-256: one replaced is the same file.
	slices.Sort(files)
	t := con.start("Uploading " + uploadNames(files))
	out, err := exec.Command(gh, append([]string{"release", "upload", tag, "--repo", repo, "--clobber"}, files...)...).CombinedOutput()
	if t.end(err, "Uploaded "+uploadNames(files)) != nil {
		return fmt.Errorf("gh release upload: %v\n%s", err, out)
	}
	return nil
}
