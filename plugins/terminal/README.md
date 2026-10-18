# terminal

A terminal for [MyGo](https://github.com/egoist/mygo) apps of native UI: a
view that runs the user's shell, or any program, in a pseudo-terminal, with
[Ghostty](https://ghostty.org)'s terminal emulator, libghostty-vt, loaded
at run time with no cgo.

```go
import "github.com/egoist/mygo/plugins/terminal"

term, err := terminal.New(terminal.Options{})
// ...
mygo.NewWindow(mygo.WindowOptions{Title: "Terminal", Content: ui.View(func(c *ui.Context) {
	terminal.View(c, term).Fill().AutoFocus()
})})
```

See [the documentation](../../docs/plugins/terminal.md), and
`examples/terminal` for an app.

libghostty-vt is built by MyGo's pipeline for
[native libraries](../../docs/native-libraries.md): `native/` is a Zig
package that pins the commit of Ghostty in `build.zig.zon` and installs its
libghostty-vt, and `mygo-plugin.json` names it with its published builds.
Apps get the published builds until `native/` changes; then `mygo dev` and
`mygo build` build it, with the Zig version Ghostty asks for (0.16.0).

`go generate` here runs `mygo native build`, which builds it for every
platform that is not published from these sources yet and writes
`mygo-plugin.json` (publish the files with `-upload`, or as the assets of
the release it names), and writes `ghostty_themes.go`, the themes that
Ghostty ships, which `go run ./internal/themes` writes alone, without Zig.
To bind another commit of Ghostty, change it in `native/build.zig.zon`
(`zig fetch --save=ghostty "git+https://github.com/ghostty-org/ghostty#<commit>"`),
and name a new release in `mygo-plugin.json`.
