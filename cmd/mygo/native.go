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
the module; -upload publishes them as the assets of a GitHub release.

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
	var assets []string
	uploads := map[string][]string{} // the files of each release
	built := 0
	for _, p := range pkgs {
		changed := false
		for i := range p.manifest.Libraries {
			l := &p.manifest.Libraries[i]
			if l.Build == nil {
				continue
			}
			built++
			for _, t := range targets {
				goos, goarch, _ := strings.Cut(t, "-")
				path, err := buildNative(p.dir, *l, goos, goarch, "")
				if err != nil {
					return err
				}
				if host {
					continue
				}
				a, f, err := publishNative(p.dir, *out, *l, path, goos, goarch, cmp.Or(*release, l.Release))
				if err != nil {
					return err
				}
				if l.Files == nil {
					l.Files = map[string]nativelib.File{}
				}
				l.Files[t] = f
				assets = append(assets, a)
				if r := cmp.Or(*release, l.Release); r != "" {
					uploads[r] = append(uploads[r], a)
				}
			}
			if !host {
				l.Release = cmp.Or(*release, l.Release)
				if l.Release == "" {
					logf("%s has no release: set it with -release so that apps can download the files", l.Name)
				}
				changed = true
			}
		}
		if changed {
			if err := writeManifest(filepath.Join(p.dir, pluginFile), p.manifest); err != nil {
				return err
			}
			logf("Wrote %s", cyan(filepath.Join(p.dir, pluginFile)))
		}
	}
	if built == 0 {
		return fmt.Errorf("no %s in %s names a library to build (\"build\": {\"cargo\": \"<dir>\"} or {\"zig\": \"<dir>\"})", pluginFile, dir)
	}
	if host || len(assets) == 0 {
		return nil
	}
	if !*upload {
		logf("Publish %s under the release of each library, or run again with -upload", cyan(filepath.Dir(assets[0])))
		return nil
	}
	if len(uploads) == 0 {
		return errors.New("-upload needs -release")
	}
	for r, files := range uploads {
		if err := uploadNatives(r, files); err != nil {
			return err
		}
	}
	return nil
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
// of the files to publish, as <name>-<goos>-<goarch><ext>, and into the
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
	asset := filepath.Join(out, l.Name+"-"+goos+"-"+goarch+nativelib.Ext(goos))
	if err := replaceFile(built, asset); err != nil {
		return "", nativelib.File{}, err
	}
	sum, err := fetch.FileSHA256(asset)
	if err != nil {
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
	slices.Sort(files)
	t := con.start("Uploading " + uploadNames(files))
	out, err := exec.Command(gh, append([]string{"release", "upload", tag, "--repo", repo, "--clobber"}, files...)...).CombinedOutput()
	if t.end(err, "Uploaded "+uploadNames(files)) != nil {
		return fmt.Errorf("gh release upload: %v\n%s", err, out)
	}
	return nil
}
