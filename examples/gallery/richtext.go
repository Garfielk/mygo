package main

import (
	"github.com/egoist/mygo/ui"
)

// sampleNote is the text the Rich Text page starts with.
func sampleNote() ui.AttributedText {
	bold := ui.TextAttributes{Weight: 700}
	return ui.AttributedTextOf(
		ui.Paragraph{Style: ui.ParagraphStyle{Heading: 1}, Runs: []ui.TextRun{{Text: "Trip to Lisbon"}}},
		ui.Paragraph{Runs: []ui.TextRun{
			{Text: "Flights land at "}, {Text: "10:40", Attributes: bold}, {Text: " on Friday. The hotel is in "},
			{Text: "Alfama", Attributes: ui.TextAttributes{Italic: true}}, {Text: ", near the "},
			{Text: "castle", Attributes: ui.TextAttributes{Link: "https://en.wikipedia.org/wiki/S%C3%A3o_Jorge_Castle"}}, {Text: "."},
		}},
		ui.Paragraph{Style: ui.ParagraphStyle{Heading: 2}, Runs: []ui.TextRun{{Text: "Pack"}}},
		ui.Paragraph{Style: ui.ParagraphStyle{List: ui.Tasks, Checked: true}, Runs: []ui.TextRun{{Text: "Passport"}}},
		ui.Paragraph{Style: ui.ParagraphStyle{List: ui.Tasks}, Runs: []ui.TextRun{{Text: "Walking shoes"}}},
		ui.Paragraph{Style: ui.ParagraphStyle{List: ui.Tasks}, Runs: []ui.TextRun{{Text: "Adapter for "}, {Text: "type F", Attributes: ui.TextAttributes{Code: true}}, {Text: " sockets"}}},
		ui.Paragraph{Style: ui.ParagraphStyle{Heading: 2}, Runs: []ui.TextRun{{Text: "Eat"}}},
		ui.Paragraph{Style: ui.ParagraphStyle{List: ui.Numbers}, Runs: []ui.TextRun{{Text: "Pastéis de nata", Attributes: bold}, {Text: " at Manteigaria"}}},
		ui.Paragraph{Style: ui.ParagraphStyle{List: ui.Numbers, Indent: 1}, Runs: []ui.TextRun{{Text: "Warm, with cinnamon"}}},
		ui.Paragraph{Style: ui.ParagraphStyle{List: ui.Numbers}, Runs: []ui.TextRun{{Text: "Grilled sardines"}}},
		ui.Paragraph{Style: ui.ParagraphStyle{Quote: true}, Runs: []ui.TextRun{{Text: "Lisbon is a city to walk, slowly, uphill."}}},
		ui.Paragraph{Runs: []ui.TextRun{{Text: "Type "}, {Text: "# ", Attributes: ui.TextAttributes{Code: true}}, {Text: ", "}, {Text: "- ", Attributes: ui.TextAttributes{Code: true}}, {Text: " or "}, {Text: "**bold**", Attributes: ui.TextAttributes{Code: true}}, {Text: " to format as you write."}}},
	)
}

// richText shows a rich text editor with a toolbar formatting its
// selection, which shows the selection's formats.
func (g *gallery) richText(c *ui.Context) {
	t := c.Theme()
	if g.note == nil {
		g.note = ui.NewRichTextDocument(sampleNote())
	}
	doc := g.note
	f := doc.Formatting()
	button := func(label string, on bool, run func()) {
		b := ui.Button(c, label).KeepFocus().Padding(4, 9).MinWidth(30)
		if on {
			b.Background(t.Accent.Alpha(0.18))
		}
		if b.Clicked() {
			run()
		}
	}
	ui.Row(c).Gap(4).Wrap().AlignItems(ui.Center).Children(func() {
		button("B", f.Bold == ui.FormatOn, doc.ToggleBold)
		button("I", f.Italic == ui.FormatOn, doc.ToggleItalic)
		button("U", f.Underline == ui.FormatOn, doc.ToggleUnderline)
		button("S", f.Strikethrough == ui.FormatOn, doc.ToggleStrikethrough)
		button("</>", f.Code == ui.FormatOn, doc.ToggleCode)
		ui.Box(c).Width(1).Height(20).Background(t.Border)
		for level, name := range []string{"Body", "H1", "H2", "H3"} {
			button(name, !f.ParagraphsMixed && f.Paragraph.Heading == level, func() { doc.SetHeading(level) })
		}
		ui.Box(c).Width(1).Height(20).Background(t.Border)
		button("• List", f.Paragraph.List == ui.Bullets, func() { doc.ToggleList(ui.Bullets) })
		button("1. List", f.Paragraph.List == ui.Numbers, func() { doc.ToggleList(ui.Numbers) })
		button("☑ Tasks", f.Paragraph.List == ui.Tasks, func() { doc.ToggleList(ui.Tasks) })
		button("❝", f.Paragraph.Quote, doc.ToggleQuote)
		button("{ }", f.Paragraph.CodeBlock, doc.ToggleCodeBlock)
		ui.Box(c).Width(1).Height(20).Background(t.Border)
		button("Left", f.Paragraph.Align == ui.Start, func() { doc.SetAlign(ui.Start) })
		button("Center", f.Paragraph.Align == ui.Center, func() { doc.SetAlign(ui.Center) })
		button("Right", f.Paragraph.Align == ui.End, func() { doc.SetAlign(ui.End) })
		ui.Box(c).Width(1).Height(20).Background(t.Border)
		if ui.Button(c, "Undo").KeepFocus().Disabled(!doc.CanUndo()).Clicked() {
			doc.Undo()
		}
		if ui.Button(c, "Redo").KeepFocus().Disabled(!doc.CanRedo()).Clicked() {
			doc.Redo()
		}
	})
	ui.RichTextEditor(c, doc).Height(460).Label("Note")
	ui.Text(c, "⌘B, ⌘I, ⌘U, ⇧⌘X, ⌘E and ⌘K format; ⇧⌘7, ⇧⌘8 and ⇧⌘L make lists, Tab and ⇧Tab indent them; ⌥⌘1–3 make headings.").FontSize(12).TextColor(t.TextMuted)
}
