// Themes writes ghostty_themes.go, the themes that the Ghostty pinned by
// native/build.zig.zon ships, from the archive Ghostty's build.zig.zon
// names:
//
//	go generate ./plugins/terminal
//
// With -src, it reads Ghostty's build.zig.zon from a checkout rather than
// from GitHub.
package main

import (
	"flag"
	"log"
	"os"
	"regexp"
)

// commit is the commit of Ghostty that native/build.zig.zon pins.
var commit string

func main() {
	src := flag.String("src", "", "a checkout of Ghostty, at the commit")
	flag.Parse()
	zon, err := os.ReadFile("native/build.zig.zon")
	if err != nil {
		log.Fatal(err)
	}
	m := regexp.MustCompile(`"git\+https://github\.com/ghostty-org/ghostty#([0-9a-f]{40})"`).FindSubmatch(zon)
	if m == nil {
		log.Fatal("native/build.zig.zon pins no commit of Ghostty")
	}
	commit = string(m[1])
	writeThemes(*src)
}
