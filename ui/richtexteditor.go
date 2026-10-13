package ui

// RichTextEditor creates an editor of the attributed text of doc, as
// NSTextView edits attributed text: bold, italic, underline,
// strikethrough, code, fonts, sizes and colors, links, headings, lists,
// quotations, blocks of code, alignment and attachments, with the
// shortcuts of word processors (⌘B, ⌘I, ⌘U, ⌘K, ⇧⌘7, ⇧⌘8, ⌘], …), undo
// of formats with text, rich copy and paste, and input methods composing
// in the typing attributes. The app keeps doc from frame to frame:
//
//	doc := ui.NewRichTextDocument(ui.NewAttributedText("Hello"))
//	// In the view:
//	if ui.RichTextEditor(c, doc).Fill().Changed() {
//		app.save(doc.Text())
//	}
//
// A toolbar formats the selection through doc (doc.ToggleBold,
// doc.SetHeading, …) and shows its formats with doc.Formatting. Changed
// reports the edits the user made in the editor, not those the app made
// through doc.
func coreRichTextEditor(c *context, doc *RichTextDocument) *node {
	t := c.theme
	e := richTextEditorBase(c, doc)
	e.Padding(t.Space(2.5), t.Space(3)).Radius(t.Radius).Background(t.Surface).Border(1, t.Border)
	e.MinHeight(t.Space(30))
	e.styleFn = func(e *node) { inputBorder(t, e, e) }
	return e
}

// RichTextEditorBase creates a RichTextEditor without padding, background,
// border or corners.
func coreRichTextEditorBase(c *context, doc *RichTextDocument) *node {
	return richTextEditorBase(c, doc)
}

func richTextEditorBase(c *context, doc *RichTextDocument) *node {
	if doc == nil {
		panic("ui: RichTextEditor requires a non-nil RichTextDocument")
	}
	e := c.newElement(kindInput)
	e.flags |= flagEditable | flagFocusable | flagHover | flagScrollY
	e.widget = "RichTextEditor"
	st := e.st
	if st.editor == nil || st.editor.rich == nil || st.editor.rich.doc != doc {
		ed := newEditor()
		ed.rich = &richEditor{doc: doc}
		ed.multiline = true
		ed.area = &area{reveal: true}
		st.editor = ed
		ed.rich.sync(ed)
		anchor, caret := doc.Selection()
		ed.anchor, ed.caret = anchor, caret
	}
	ed := st.editor
	if ed.client == nil {
		ed.client = &widgetTextInput{ed: ed, rt: c.rt, id: e.id}
	}
	e.textClient = ed.client
	ed.readOnly, ed.onPaste, ed.onEnter = false, nil, nil
	r := ed.rich
	r.rt = c.rt
	r.markdown, r.onLink, r.onLinkCommand, r.look.tagStyle = true, nil, nil, nil
	e.onValueInput(richInput)
	return e
}

// richInput applies the input queued for a rich text editor as it builds,
// and reports the edits it made.
func richInput(n *node) {
	ed, st, rt := n.st.editor, n.st, n.c.rt
	r := ed.rich
	r.sync(ed)
	if r.dirty {
		st.markChanged()
		r.dirty = false
	}
	if n.disabled() {
		clear(ed.queue)
		ed.queue = ed.queue[:0]
		ed.compose = ""
		return
	}
	if rt.focused == n.id || len(ed.queue) > 0 {
		before := r.version
		ed.process(n.c, n)
		if r.version != before {
			st.markChanged()
		}
	}
	if rt.focused != n.id {
		ed.compose = ""
	}
	if len(r.spans) > 4*maxLaid {
		r.forgetSpans()
	}
}

// setLook sets how the editor's text looks, from its text style and the
// theme, as it lays out.
func (r *richEditor) setLook(e *node) {
	t := e.c.theme
	ts := e.resolvedText()
	size := ts.size
	if size <= 0 {
		size = 14
	}
	r.look = richLook{
		size:     size,
		mono:     "monospace",
		text:     ts.color,
		muted:    t.TextMuted,
		link:     t.Accent,
		code:     t.Text.Alpha(0.07),
		quote:    t.Border,
		accent:   t.Accent,
		indent:   size * 1.7,
		tagStyle: r.look.tagStyle,
	}
}

// MarkdownShortcuts turns Markdown typed in a RichTextEditor into formats,
// as it is by default: "# " at the start of a paragraph makes a heading,
// "- " or "* " a bulleted list, "1. " a numbered one, "[ ] " a task, "> "
// a quotation and "```" a block of code; **bold**, *italic*, `code` and
// ~~strikethrough~~ format what they hold as the closing mark is typed.
// Undo takes a shortcut back to what was typed.
func (e *node) MarkdownShortcuts(on bool) *node {
	if ed := e.st.editor; ed != nil && ed.rich != nil {
		ed.rich.markdown = on
	}
	return e
}

// OnLink has fn open the URL of a link clicked in a RichTextEditor,
// instead of the system opening it.
func (e *node) OnLink(fn func(url string)) *node {
	if ed := e.st.editor; ed != nil && ed.rich != nil {
		ed.rich.onLink = fn
	}
	return e
}

// OnLinkCommand has fn run for ⌘K (Ctrl+K) in a RichTextEditor, as an app
// asks for the URL of a link in a popover and sets it with
// RichTextDocument.SetLink. Without it, ⌘K links a selected URL to itself.
func (e *node) OnLinkCommand(fn func()) *node {
	if ed := e.st.editor; ed != nil && ed.rich != nil {
		ed.rich.onLinkCommand = fn
	}
	return e
}

// TagStyle has fn style the runs of a RichTextEditor that carry a Tag, as
// an app shows mentions or comments.
func (e *node) TagStyle(fn func(tag string, s *Span)) *node {
	if ed := e.st.editor; ed != nil && ed.rich != nil {
		ed.rich.look.tagStyle = fn
	}
	return e
}
