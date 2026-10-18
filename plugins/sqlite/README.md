# SQLite

SQLite for MyGo, with Go connections and the `@mygo-plugins/sqlite`
TypeScript client. The plugin ships prebuilt SQLite libraries and loads
them through purego, so apps build without cgo or a C compiler.

Register `mygo.Use(sqlite.Plugin)` only when the web frontend uses
`@mygo-plugins/sqlite`. Go and native UI apps call `sqlite.Open` directly;
they need no plugin registration and close their own connections.

```go
import (
    "github.com/egoist/mygo"
    "github.com/egoist/mygo/plugins/sqlite"
)

func main() {
    mygo.Use(sqlite.Plugin)
    // Create windows, then call mygo.App.Run().
}
```

```ts
import { open } from "@mygo-plugins/sqlite";

const db = await open("app.db");
await db.execute("CREATE TABLE IF NOT EXISTS tasks (id INTEGER PRIMARY KEY, title TEXT, done INTEGER)");
const inserted = await db.execute("INSERT INTO tasks(title, done) VALUES (?, ?)", ["Learn MyGo", false]);
const { columns, rows } = await db.query("SELECT id, title, done FROM tasks WHERE done = ?", [false]);
await db.transaction([
  { sql: "UPDATE tasks SET done = ? WHERE id = ?", params: [true, inserted.lastInsertId] },
  { sql: "INSERT INTO tasks(title, done) VALUES (?, ?)", params: ["Ship the app", false] },
]);
await db.close();
```

Use `bun add @mygo-plugins/sqlite` once the npm package is published. In a
checkout, `bun install && bun run build` installs and builds the workspace
package. See [the full guide](../../docs/plugins/sqlite.md) for the Go API,
types, cancellation, options and packaging.

## Building the native libraries

Prebuilt libraries are published in
[sqlite-3.53.4-1](https://github.com/egoist/mygo/releases/tag/sqlite-3.53.4-1).
CI and unpackaged programs download the matching library and verify its
SHA-256. Testing and building apps require no Zig installation.

The library is built by MyGo's pipeline for
[native libraries](../../docs/native-libraries.md): `native/` is a Zig
package whose `build.zig.zon` pins SQLite **3.53.4**'s amalgamation by its
hash, and `build.zig` compiles it with the C shim. `mygo-plugin.json`
names it, `"build": {"zig": "./native"}`, with its published builds, each
with the hash of the sources it was built from: apps use them until
`native/` changes, and then `mygo dev` and `mygo build` build the library
themselves, with Zig **0.16.0** on the path.

To publish new builds, after changing SQLite or its shim:

```sh
go generate ./plugins/sqlite   # mygo native build: every platform, into mygo-plugin.json
go run ./cmd/mygo native build -upload ./plugins/sqlite   # and upload them
# Or build just this machine's, for go test:
go run ./cmd/mygo native build -target host ./plugins/sqlite
```

It builds macOS, Linux and Windows for amd64 and arm64 (Linux for glibc
2.28 or later, macOS for 12 or later), names the files after their SHA-256,
and writes their URLs under the release in `mygo-plugin.json`. Name a new
release there for a new SQLite version; `-src`'s replacement is
`"args": ["-Dsqlite=<directory>"]`, which builds another amalgamation.

`MYGO_SQLITE_LIBRARY` overrides the library path. Otherwise the plugin
looks in app resources, next to the executable, then the CLI's cache.
Unpackaged programs download a missing library and verify its SHA-256;
packaged apps never download native code at run time.

SQLite is public domain; the plugin and C shim use the repository's MIT
license. FTS5, RTree and JSON are enabled, with foreign keys on by default.
Loadable extensions, shared cache and deprecated APIs are omitted.
