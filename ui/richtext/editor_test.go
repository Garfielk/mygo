package richtext

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// richTester builds a RichTextEditor of doc filling a window, focused at
// the end of its text.
func richTester(t *testing.T, doc *Document) (*ui.Tester, *bool) {
	t.Helper()
	changed := new(bool)
	tt := ui.NewTester(func(c *ui.Context) {
		if Editor(c, doc).Fill().Changed() {
			*changed = true
		}
	}, 500, 400)
	tt.Press(250, 380)
	tt.Release(250, 380)
	tt.Key(ui.Cmd, ui.KeyDown)
	return tt, changed
}

// runsOf describes the runs of a text: their text, marked with what they
// are, as "plain|*bold*|_italic_|[link]".
func runsOf(at Text) string {
	var parts []string
	var last Attributes
	for i, r := range at.Runs(0, at.Len()) {
		if i > 0 && r.Attributes == last && len(parts) > 0 {
			// A run of the next paragraph, as the one before.
			parts[len(parts)-1] = joinRun(parts[len(parts)-1], r.Text)
			continue
		}
		last = r.Attributes
		s := r.Text
		a := r.Attributes
		if a.Bold() {
			s = "*" + s + "*"
		}
		if a.Italic {
			s = "_" + s + "_"
		}
		if a.Underline {
			s = "~" + s + "~"
		}
		if a.Code {
			s = "`" + s + "`"
		}
		if a.Link != "" {
			s = "[" + s + "]"
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, "|")
}

// joinRun puts text inside the marks of a run runsOf described.
func joinRun(run, text string) string {
	i := strings.LastIndexFunc(run, func(r rune) bool { return !strings.ContainsRune("*_~`]", r) })
	return run[:i+1] + text + run[i+1:]
}

// typeRunes types s a rune at a time, as a keyboard does.
func typeRunes(tt *ui.Tester, s string) {
	for _, r := range s {
		tt.Type(string(r))
	}
}

func TestRichTextTyping(t *testing.T) {
	doc := NewDocument(Text{})
	tt, changed := richTester(t, doc)
	tt.Type("plain ")
	tt.Key(ui.Cmd, ui.KeyB)
	tt.Type("bold")
	tt.Key(ui.Cmd, ui.KeyB)
	tt.Type(" more")
	if got := runsOf(doc.Text()); got != "plain |*bold*| more" {
		t.Fatalf("runs %q", got)
	}
	if !*changed {
		t.Fatal("typing reports no change")
	}
	// Typing at the end of bold continues it; at its start, it does not.
	doc.SetSelection(10, 10)
	tt.Frame()
	tt.Type("er")
	doc.SetSelection(6, 6)
	tt.Frame()
	tt.Type("x")
	if got := runsOf(doc.Text()); got != "plain x|*bolder*| more" {
		t.Fatalf("runs %q", got)
	}
}

func TestRichTextLinkEdges(t *testing.T) {
	doc := NewDocument(TextOf(Paragraph{Runs: []Run{
		{Text: "see "}, {Text: "docs", Attributes: Attributes{Link: "https://x"}}, {Text: " now"},
	}}))
	tt, _ := richTester(t, doc)
	doc.SetSelection(8, 8)
	tt.Frame()
	tt.Type("!")
	doc.SetSelection(6, 6)
	tt.Frame()
	tt.Type("o")
	if got := runsOf(doc.Text()); got != "see |[doocs]|! now" {
		t.Fatalf("runs %q", got)
	}
}

func TestRichTextCaretFormatIsNoStep(t *testing.T) {
	doc := NewDocument(NewText("hello"))
	tt, _ := richTester(t, doc)
	tt.Key(ui.Cmd, ui.KeyB)
	if doc.CanUndo() {
		t.Fatal("⌘B at a caret made a step of undo")
	}
	if f := doc.Formatting(); f.Bold != FormatOn {
		t.Fatalf("typing attributes not bold: %+v", f)
	}
	// Moving the caret forgets them.
	tt.Key(0, ui.KeyLeft)
	if doc.Formatting().Bold != FormatOff {
		t.Fatal("moving the caret kept the typing attributes")
	}
}

func TestRichTextUndoRestoresAttributes(t *testing.T) {
	doc := NewDocument(TextOf(Paragraph{Runs: []Run{
		{Text: "a "}, {Text: "bold", Attributes: Attributes{Weight: 700}}, {Text: " word"},
	}}))
	tt, _ := richTester(t, doc)
	before := doc.Text()
	doc.SetSelection(2, 6)
	tt.Frame()
	tt.Key(0, ui.KeyBackspace)
	if got := doc.Text().String(); got != "a  word" {
		t.Fatalf("deleted to %q", got)
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if !doc.Text().Equal(before) {
		t.Fatalf("undo made %q", runsOf(doc.Text()))
	}
	if a, c := doc.Selection(); a != 2 || c != 6 {
		t.Fatalf("undo selected %d–%d", a, c)
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyZ)
	if got := doc.Text().String(); got != "a  word" {
		t.Fatalf("redo made %q", got)
	}
}

func TestRichTextFormatUndo(t *testing.T) {
	doc := NewDocument(NewText("one two"))
	tt, _ := richTester(t, doc)
	doc.SetSelection(4, 7)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyI)
	tt.Key(ui.Cmd, ui.KeyU)
	if got := runsOf(doc.Text()); got != "one |~_two_~" {
		t.Fatalf("runs %q", got)
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if got := runsOf(doc.Text()); got != "one |_two_" {
		t.Fatalf("after one undo %q", got)
	}
	if a, c := doc.Selection(); a != 4 || c != 7 {
		t.Fatalf("undo selected %d–%d", a, c)
	}
}

func TestRichTextLists(t *testing.T) {
	doc := NewDocument(Text{})
	tt, _ := richTester(t, doc)
	typeRunes(tt, "- milk")
	tt.Key(0, ui.KeyEnter)
	typeRunes(tt, "eggs")
	tt.Key(0, ui.KeyEnter)
	at := doc.Text()
	if got := at.String(); got != "milk\neggs\n" {
		t.Fatalf("text %q", got)
	}
	for i, want := range []ListKind{Bullets, Bullets, Bullets} {
		if got := at.ParagraphStyleAt(at.paraStart(i)).List; got != want {
			t.Fatalf("paragraph %d: list %d, want %d", i, got, want)
		}
	}
	// Enter on an empty item leaves the list.
	tt.Key(0, ui.KeyEnter)
	at = doc.Text()
	if at.String() != "milk\neggs\n" || at.ParagraphStyleAt(at.Len()).List != NoList {
		t.Fatalf("%q %+v", at.String(), at.ParagraphStyleAt(at.Len()))
	}
	// Tab indents an item, Backspace at its start outdents it, then takes
	// it out of the list.
	doc.SetSelection(5, 5)
	tt.Frame()
	tt.Key(0, ui.KeyTab)
	if got := doc.Text().ParagraphStyleAt(5).Indent; got != 1 {
		t.Fatalf("indent %d", got)
	}
	tt.Key(0, ui.KeyBackspace)
	tt.Key(0, ui.KeyBackspace)
	if st := doc.Text().ParagraphStyleAt(5); st.List != NoList || st.Indent != 0 {
		t.Fatalf("style %+v", st)
	}
}

func TestRichTextHeadings(t *testing.T) {
	doc := NewDocument(Text{})
	tt, _ := richTester(t, doc)
	typeRunes(tt, "## Title")
	tt.Key(0, ui.KeyEnter)
	typeRunes(tt, "body")
	at := doc.Text()
	if at.String() != "Title\nbody" || at.ParagraphStyleAt(0).Heading != 2 || at.ParagraphStyleAt(7).Heading != 0 {
		t.Fatalf("%q %+v %+v", at.String(), at.ParagraphStyleAt(0), at.ParagraphStyleAt(7))
	}
	// Undo takes the shortcut back to what was typed.
	doc2 := NewDocument(Text{})
	tt2, _ := richTester(t, doc2)
	typeRunes(tt2, "# ")
	tt2.Key(ui.Cmd, ui.KeyZ)
	if got := doc2.Text(); got.String() != "# " || got.ParagraphStyleAt(0).Heading != 0 {
		t.Fatalf("undo of the shortcut: %q", got.String())
	}
}

func TestRichTextInlineShortcuts(t *testing.T) {
	doc := NewDocument(Text{})
	tt, _ := richTester(t, doc)
	typeRunes(tt, "a **big** and `x` then")
	if got := runsOf(doc.Text()); got != "a |*big*| and |`x`| then" {
		t.Fatalf("runs %q", got)
	}
}

func TestRichTextCompose(t *testing.T) {
	doc := NewDocument(NewText("ab"))
	tt, _ := richTester(t, doc)
	tt.Key(ui.Cmd, ui.KeyB)
	tt.Compose("ni", 2)
	tt.Compose("你", 1)
	tt.Compose("", 0)
	tt.Type("你")
	if got := runsOf(doc.Text()); got != "ab|*你*" {
		t.Fatalf("runs %q", got)
	}
}

func TestRichTextAppCommands(t *testing.T) {
	doc := NewDocument(NewText("first\nsecond"))
	tt, _ := richTester(t, doc)
	doc.SetSelection(0, 3)
	doc.ToggleBold()
	doc.SetHeading(1)
	tt.Frame()
	at := doc.Text()
	if runsOf(at) != "*fir*|st\nsecond" || at.ParagraphStyleAt(0).Heading != 1 {
		t.Fatalf("%q %+v", runsOf(at), at.ParagraphStyleAt(0))
	}
	// The editor follows: typing replaces the selection the app set.
	tt.Type("X")
	if got := runsOf(doc.Text()); got != "*X*|st\nsecond" {
		t.Fatalf("runs %q", got)
	}
}
