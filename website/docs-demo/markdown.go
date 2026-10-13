package main

import (
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

const demoMarkdown = "### A reply, streamed\n\n" +
	"The parser reads **CommonMark** and *GitHub's* extensions as text comes, so `**bol` is already **bold**:\n\n" +
	"- [x] tables, task lists and ~~strikethrough~~\n- [ ] [links](https://github.com/egoist/mygo) and footnotes\n\n" +
	"```go\nmarkdown.Stream(c, reply, &app.md)\n```\n\n" +
	"| Strategy | Per token |\n|---|--:|\n| Re-parse all | O(n) |\n| **MyGo** | O(open block) |\n"

func init() {
	var opts markdown.Options
	var started time.Time
	streaming := false
	demo("markdown", func(c *ui.Context) {
		text := demoMarkdown
		if streaming {
			// About 200 bytes a second, as a language model streams.
			n := min(len(demoMarkdown), int(c.Now().Sub(started).Seconds()*200))
			for n < len(demoMarkdown) && demoMarkdown[n]&0xC0 == 0x80 {
				n++
			}
			text = demoMarkdown[:n]
			if n == len(demoMarkdown) {
				streaming = false
			} else {
				c.AnimationFrame()
			}
		}
		ui.Column(c).AlignSelf(ui.Stretch).Gap(12).Children(func() {
			if ui.Button(c, "Stream it").Clicked() {
				streaming, started = true, c.Now()
			}
			if streaming {
				markdown.Stream(c, text, &opts)
			} else {
				markdown.View(c, text, &opts)
			}
		})
	})
}
