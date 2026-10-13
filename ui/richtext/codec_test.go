package richtext

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

// sample is a text of every kind of paragraph and run.
func sample() Text {
	bold := Attributes{Weight: 700}
	return TextOf(
		Paragraph{Style: ParagraphStyle{Heading: 1}, Runs: []Run{{Text: "Trip"}}},
		Paragraph{Runs: []Run{{Text: "Land at "}, {Text: "10:40", Attributes: bold}, {Text: " near the "},
			{Text: "castle", Attributes: Attributes{Link: "https://x.org/a"}}, {Text: ", "}, {Text: "slowly", Attributes: Attributes{Italic: true}}, {Text: "."}}},
		Paragraph{Style: ParagraphStyle{List: Tasks, Checked: true}, Runs: []Run{{Text: "Passport"}}},
		Paragraph{Style: ParagraphStyle{List: Tasks}, Runs: []Run{{Text: "Adapter"}}},
		Paragraph{Style: ParagraphStyle{List: Numbers}, Runs: []Run{{Text: "One"}}},
		Paragraph{Style: ParagraphStyle{List: Numbers, Indent: 1}, Runs: []Run{{Text: "Inner"}}},
		Paragraph{Style: ParagraphStyle{List: Numbers}, Runs: []Run{{Text: "Two"}}},
		Paragraph{Style: ParagraphStyle{Quote: true}, Runs: []Run{{Text: "A quote"}}},
		Paragraph{Style: ParagraphStyle{CodeBlock: true}, Runs: []Run{{Text: "a := 1"}}},
		Paragraph{Style: ParagraphStyle{CodeBlock: true}, Runs: []Run{{Text: "b := 2"}}},
		Paragraph{Style: ParagraphStyle{Align: ui.Center}, Runs: []Run{{Text: "mid", Attributes: Attributes{Underline: true, Strikethrough: true}}, {Text: " and "}, {Text: "x()", Attributes: Attributes{Code: true}}}},
	)
}

func TestMarkdown(t *testing.T) {
	want := strings.Join([]string{
		"# Trip",
		"",
		"Land at **10:40** near the [castle](https://x.org/a), *slowly*.",
		"",
		"- [x] Passport",
		"- [ ] Adapter",
		"1. One",
		"    1. Inner",
		"2. Two",
		"",
		"> A quote",
		"",
		"```",
		"a := 1",
		"b := 2",
		"```",
		"",
		"~~<u>mid</u>~~ and `x()`",
	}, "\n")
	if got := sample().Markdown(); got != want {
		t.Fatalf("Markdown:\n%s\nwant:\n%s", got, want)
	}
	// What Markdown would read as a block is escaped in a body paragraph.
	if got := NewText("# not a heading").Markdown(); got != `\# not a heading` {
		t.Fatalf("%q", got)
	}
}

func TestHTMLRoundTrip(t *testing.T) {
	in := sample()
	html := in.HTML()
	out := ParseHTML(html)
	if out.String() != in.String() {
		t.Fatalf("text %q, want %q\nHTML %s", out.String(), in.String(), html)
	}
	if got, want := runsOf(out), runsOf(in); got != want {
		t.Fatalf("runs %q, want %q", got, want)
	}
	for start, p := range in.Paragraphs() {
		if got := out.ParagraphStyleAt(start); got != p.Style {
			t.Fatalf("paragraph at %d: %+v, want %+v", start, got, p.Style)
		}
	}
}

func TestParseForeignHTML(t *testing.T) {
	// As Google Docs copies: a bold wrapper made normal, spans styled.
	gdocs := `<meta charset="utf-8"><b style="font-weight:normal;" id="docs-internal-guid-1"><p dir="ltr"><span style="font-size:11pt;font-family:Arial;font-weight:700;">Bold</span><span style="font-size:11pt;"> and </span><span style="font-style:italic;">italic</span></p><ul><li><p><span>item</span></p></li></ul></b>`
	got := ParseHTML(gdocs)
	if s := got.String(); s != "Bold and italic\nitem" {
		t.Fatalf("text %q", s)
	}
	if r := runsOf(got.Slice(0, 15)); r != "*Bold*| and |_italic_" {
		t.Fatalf("runs %q", r)
	}
	if got.ParagraphStyleAt(17).List != Bullets {
		t.Fatalf("not a list: %+v", got.ParagraphStyleAt(17))
	}
	// Whitespace collapses; scripts and styles are left out.
	got = ParseHTML("<style>p{}</style><p>  a \n  b  </p><script>x()</script><p>c<br>d</p>")
	if s := got.String(); s != "a b\nc\nd" {
		t.Fatalf("text %q", s)
	}
}

func TestCopyPaste(t *testing.T) {
	doc := NewDocument(sample())
	tt, _ := richTester(t, doc)
	doc.SetSelection(5, 18) // "Land at 10:40"
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyC)
	d := tt.ClipboardData()
	for _, f := range []transfer.Format{Format, transfer.HTML, transfer.Text} {
		if _, err := d.Read(f); err != nil {
			t.Fatalf("no %s copied", f)
		}
	}
	if text, _ := d.Read(transfer.Text); string(text) != "Land at 10:40" {
		t.Fatalf("plain text %q", text)
	}
	doc.SetSelection(0, 0)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyV)
	if got := doc.Text().Slice(0, 13); runsOf(got) != "Land at |*10:40*" {
		t.Fatalf("pasted %q", runsOf(got))
	}
	// One step of undo takes the paste back.
	tt.Key(ui.Cmd, ui.KeyZ)
	if !doc.Text().Equal(sample()) {
		t.Fatalf("undo made %q", doc.Text().String())
	}
	// HTML from another app pastes its formats; Paste and Match Style
	// pastes its text in the typing attributes.
	tt.SetClipboardData(transfer.New(transfer.NewItem(transfer.Bytes(transfer.HTML, []byte("<b>hi</b>")), transfer.Bytes(transfer.Text, []byte("hi")))))
	doc.SetSelection(0, 0)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyV)
	if got := runsOf(doc.Text().Slice(0, 2)); got != "*hi*" {
		t.Fatalf("pasted HTML %q", got)
	}
	tt.Command("pasteAndMatchStyle")
	if got := runsOf(doc.Text().Slice(0, 4)); got != "*hihi*" {
		t.Fatalf("pasted matching the style %q", got)
	}
}

func TestJSON(t *testing.T) {
	in := sample()
	b, err := in.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var out Text
	if err := out.UnmarshalJSON(b); err != nil {
		t.Fatal(err)
	}
	if !out.Equal(in) {
		t.Fatalf("%q, want %q", runsOf(out), runsOf(in))
	}
}
