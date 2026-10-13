//go:build !js

package e2e

import (
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"
)

// TestRichTextEditor drives a rich text editor in a window through the
// system: the Format menu bolds what an input method then composes, the
// Edit menu pastes HTML another app copied with its formats, and undoes
// it.
func TestRichTextEditor(t *testing.T) {
	prev := mygo.App.Menu()
	defer mygo.App.SetMenu(prev)
	mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Label: "App", Submenu: []*mygo.MenuItem{{Role: mygo.RoleQuit}}},
		{Role: mygo.RoleEditMenu},
		{Role: mygo.RoleFormatMenu},
	}))
	doc := richtext.NewDocument(richtext.NewText("Hello "))
	var frames atomic.Int32
	view := func(c *ui.Context) {
		richtext.Editor(c, doc, richtext.EditorOptions{Label: "Note"}).Fill().AutoFocus()
		frames.Add(1)
	}
	w := newWindow(t, mygo.WindowOptions{Title: "Rich text", Width: 420, Height: 220, Content: ui.View(view)})
	eventually(t, "a frame", func() bool { return frames.Load() > 0 })
	if !click(w, 300, 160) {
		t.Skip("native click automation unavailable")
	}
	runs := func() string {
		var s string
		mygo.RunOnMain(func() {
			for _, r := range doc.Text().Runs(0, doc.Text().Len()) {
				a := r.Attributes
				text := r.Text
				if a.Bold() {
					text = "*" + text + "*"
				}
				if a.Italic {
					text = "_" + text + "_"
				}
				s += text + "|"
			}
		})
		return s
	}
	// Bold at the caret, then an input method's text takes it.
	if err := activateMenu(w, "Format", "Bold"); err != nil {
		t.Skipf("menu automation unavailable: %v", err)
	}
	eventually(t, "bold typing attributes", func() bool {
		var bold bool
		mygo.RunOnMain(func() { bold = doc.Formatting().Bold == richtext.FormatOn })
		return bold
	})
	if !compose(w, "世界", 2, false) {
		t.Skip("input method automation unavailable")
	}
	compose(w, "世界", 2, true)
	eventually(t, "the composition committed in bold", func() bool { return runs() == "Hello |*世界*|" })
	// HTML pasted keeps its formats; one undo takes it back.
	err := mygo.Clipboard.Write(transfer.New(transfer.NewItem(
		transfer.Bytes(transfer.HTML, []byte("<p><i>again</i></p>")),
		transfer.Bytes(transfer.Text, []byte("again")),
	)))
	if err != nil {
		t.Fatal(err)
	}
	if err := activateMenu(w, "Edit", "Paste"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the HTML pasted", func() bool { return runs() == "Hello |*世界*|_again_|" })
	if err := activateMenu(w, "Edit", "Undo"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the paste undone", func() bool { return runs() == "Hello |*世界*|" })
}
