// Frosted-folder redraws Alex Barashkov's "progressive blur + frosted
// glass experiments in blender"
// (https://x.com/alex_barashkov/status/2112171007465103548) in native UI:
// a folder whose front is a pane of frosted glass, hinged at its bottom.
// It opens, tipping its pane toward you, and the glass comes into focus:
// the pane's lower part, blurred progressively, sharpens, and the frost
// clears enough to show the blue back and the sheet in it. Then it
// closes, and the glass frosts over again.
//
//	go run ./examples/frosted-folder
//
// It plays the video's loop, timed from its frames; the pointer over the
// folder holds it open. The folder is one shader written in Go (package
// ui/shader), which draws the scene in perspective and blurs it in one
// pass, as every blur is of rectangles it knows.
package main

import (
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func main() {
	f := newFolder()
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title:  "Frosted Folder",
			Width:  windowSize,
			Height: windowSize,
			// The folder floats on the page, under the window controls.
			TitleBarStyle:   mygo.TitleBarHidden,
			BackgroundColor: "#f7f8fe",
			Content:         ui.View(f.view),
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
