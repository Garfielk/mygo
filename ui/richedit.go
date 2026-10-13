package ui

import (
	"strconv"
	"unicode/utf8"
	"unsafe"

	"github.com/egoist/mygo/internal/rope"
	"github.com/egoist/mygo/internal/text"
)

// richEditor is what a RichTextEditor adds to the editor of a text area:
// its document, whose text the editor's buffer holds (they share the
// rope), and how the runs and paragraphs of that text look. Every edit
// goes through the document, as one of its steps of undo, so that its
// attributes are undone with its text; the editor follows changes the app
// makes to the document at its next frame, laying out again only the
// paragraphs they touched.
type richEditor struct {
	doc *RichTextDocument
	// shown is the text the buffer holds, version and selVersion the
	// document's versions it is of.
	shown      AttributedText
	version    uint64
	selVersion uint64
	// mode is how the next edit joins the steps of undo: typing joins
	// typing's step; after an edit, the edits of the same action join it.
	mode richMode
	// dirty is set by an edit an input method made, which the next build
	// reports (Changed).
	dirty bool
	look  richLook
	// spans caches the spans of each paragraph's layout, by paragraph.
	spans map[uint64]*richSpans
	// numbers caches the numbers of the items of numbered lists, by
	// paragraph, for the text of numbersOf.
	numbers   map[int]int
	numbersOf AttributedText
	// markdown turns Markdown typed at the start of a paragraph and around
	// words into formats (RichTextEditor's MarkdownShortcuts).
	markdown bool
	// onLink opens a link clicked; onLinkCommand runs for ⌘K.
	onLink        func(url string)
	onLinkCommand func()
	// pressLink is the link a press began on.
	pressLink string
	rt        *engine
}

type richMode uint8

const (
	richNewStep richMode = iota
	richTyping
	richContinue
)

// richLook is how the text of a rich text editor looks: the editor's text
// style and the theme's colors, set as it builds.
type richLook struct {
	size              float32 // the text's
	mono              string
	text, muted, link Color
	code, quote       Color // code's background, a quotation's bar
	accent            Color
	indent            float32 // DIPs a level of indentation takes
	tagStyle          func(tag string, s *Span)
}

// richSpans is what a paragraph's runs made for its layout: the spans
// encoded and how they paint, for the runs, style and composition they
// were made of.
type richSpans struct {
	runs    *attrRun
	nruns   int
	style   ParagraphStyle
	compose string
	at      int
	look    richLook
	key     string
	paint   *spanPaint
	used    bool
}

// lockedSync gets the buffer in step with the document, whose lock the
// caller holds. It reports the change it took from the document, mapping
// offsets of the old text to the new one.
func (r *richEditor) lockedSync(ed *editor) (richChange, bool) {
	d := r.doc
	var change richChange
	moved := false
	if d.version != r.version {
		c, ok := d.since(r.version)
		if ok && r.version != 0 {
			first, old, after := ed.buf.replaceRoot(c.start, c.oldEnd, c.newEnd, d.text.text)
			if ed.area != nil {
				ed.area.edited(&ed.buf, first, old, after)
			}
		} else {
			ed.buf.setSnapshot(TextSnapshot{root: d.text.text})
			if ed.area != nil {
				ed.area.reveal = true
			}
		}
		r.shown, r.version = d.text, d.version
		change, moved = c, ok
		n := ed.buf.n
		if ok {
			ed.anchor = mapPosition(ed.anchor, c.start, c.oldEnd-c.start, c.newEnd-c.start, ed.anchor > ed.caret)
			ed.caret = mapPosition(ed.caret, c.start, c.oldEnd-c.start, c.newEnd-c.start, ed.caret > ed.anchor)
		}
		ed.anchor, ed.caret = min(ed.anchor, n), min(ed.caret, n)
		ed.visualSelection, ed.selected = false, nil
	}
	if d.selVersion != r.selVersion {
		r.selVersion = d.selVersion
		ed.anchor, ed.caret = d.anchor, d.caret
		ed.caretAffinity, ed.anchorAffinity = text.Downstream, text.Downstream
		ed.visualSelection, ed.selected = false, nil
		ed.hasDesired = false
		ed.compose = ""
		if ed.area != nil {
			ed.area.reveal = true
		}
	}
	return change, moved
}

// sync gets the buffer and the selection in step with the document.
func (r *richEditor) sync(ed *editor) {
	r.doc.lock()
	defer r.doc.mu.Unlock()
	r.lockedSync(ed)
}

// lockedPublish gives the document the editor's selection, whose lock the
// caller holds: a selection moved forgets the typing attributes set at
// the caret, as in NSTextView.
func (r *richEditor) lockedPublish(ed *editor) {
	d := r.doc
	a, z := ed.selection()
	anchor, caret := a, z
	if ed.caret < ed.anchor {
		anchor, caret = z, a
	}
	if d.anchor != anchor || d.caret != caret {
		d.anchor, d.caret = anchor, caret
		d.typing = nil
		d.coalesce = false
	}
}

// publish gives the document the editor's selection.
func (r *richEditor) publish(ed *editor) {
	r.doc.lock()
	defer r.doc.mu.Unlock()
	r.lockedPublish(ed)
}

// record starts an action of the editor: typing joins the step typing
// made last, anything else starts a step.
func (r *richEditor) record(typing bool) {
	if typing {
		r.mode = richTyping
	} else {
		r.mode = richNewStep
	}
}

// replace replaces the runes from start to end with s as the editor
// does: in the typing attributes, as part of the action's step of undo.
// It returns where the caret goes.
func (r *richEditor) replace(ed *editor, start, end int, s string) int {
	d := r.doc
	d.lock()
	defer d.mu.Unlock()
	if c, ok := r.lockedSync(ed); ok && c.version != 0 {
		start = mapPosition(start, c.start, c.oldEnd-c.start, c.newEnd-c.start, false)
		end = mapPosition(end, c.start, c.oldEnd-c.start, c.newEnd-c.start, true)
	}
	r.lockedPublish(ed)
	attrs := typingAttributes(d.text, start, end)
	if d.typing != nil {
		attrs = *d.typing
	}
	r.lockedEdit(ed, func(e *RichTextEdit) { e.ReplaceString(start, end, s, attrs) })
	caret := start + utf8.RuneCountInString(s)
	d.anchor, d.caret = caret, caret
	if s == "" && start < end {
		// Typing again where text was deleted gets its attributes, until
		// the caret moves.
		keep := attrs
		d.typing = &keep
	}
	return caret
}

// lockedEdit runs fn as an edit of the document in the action's step, and
// takes what it changed into the buffer. The caller holds the lock.
func (r *richEditor) lockedEdit(ed *editor, fn func(e *RichTextEdit)) {
	d := r.doc
	before := d.version
	switch r.mode {
	case richContinue:
		if len(d.undo) > 0 {
			step := &d.undo[len(d.undo)-1]
			fn(&RichTextEdit{d: d, step: step})
			step.selAfter = [2]int{d.anchor, d.caret}
			clear(d.redo)
			d.redo = d.redo[:0]
			break
		}
		d.edit(false, fn)
	default:
		d.edit(r.mode == richTyping, fn)
	}
	r.mode = richContinue
	if d.version != before {
		r.lockedSync(ed)
	}
}

// edit runs fn as a step of undo of its own, as a command does.
func (r *richEditor) edit(ed *editor, fn func(e *RichTextEdit)) {
	d := r.doc
	d.lock()
	defer d.mu.Unlock()
	r.lockedSync(ed)
	r.lockedPublish(ed)
	r.mode = richNewStep
	r.lockedEdit(ed, fn)
	r.mode = richNewStep
}

// command runs a command of the document on the editor's selection.
func (r *richEditor) command(ed *editor, fn func(d *RichTextDocument)) {
	r.publish(ed)
	fn(r.doc)
	r.sync(ed)
	r.mode = richNewStep
	ed.coalesce = false
}

// takeBack undoes the document's last step, or redoes the last undone.
func (r *richEditor) takeBack(ed *editor, redo bool) {
	r.publish(ed)
	if redo {
		r.doc.Redo()
	} else {
		r.doc.Undo()
	}
	r.sync(ed)
	r.mode = richNewStep
	ed.coalesce = false
	if ed.area != nil {
		ed.area.reveal = true
	}
}

// replaceRoot makes root the text, in which the runes from a to z of the
// text are now a to nz, as the indexed buffer's replace does: the
// paragraphs from the one holding a to the one holding z give way to those
// now from a to nz, which have no layout yet.
func (b *buffer) replaceRoot(a, z, nz int, root *rope.Node) (first int, old []paragraph, after int) {
	pa, pz := b.para(a), b.para(z)
	old = append([]paragraph(nil), b.paras[pa:pz+1]...)
	b.indexed, b.s = true, ""
	b.root = root
	sum := rope.Sum(root)
	b.n, b.units = sum.Runes, sum.Units
	npz := rope.Prefix(root, rope.ByteOfRune(root, max(0, min(nz, b.n)))).Lines
	added := make([]paragraph, npz-pa)
	tail := append([]paragraph(nil), b.paras[pz+1:]...)
	b.paras = append(append(b.paras[:pa+1], added...), tail...)
	b.paras[pa] = paragraph{}
	b.version++
	return pa, old, npz - pa + 1
}

// para returns the paragraph p of the text the editor shows.
func (r *richEditor) para(p int) *richPara { return seqAt(r.shown.seq(), p) }

// paraParams returns how paragraph p is laid out: its heading's size, a
// block of code's font, its alignment, its width less its indentation.
func (ed *editor) paraParams(p int, base text.Params) text.Params {
	r := ed.rich
	if r == nil {
		return base
	}
	st := r.para(p).style
	look := &r.look
	switch {
	case st.Heading > 0:
		base.Style.Size = look.size * headingScale(st.Heading)
		base.Style.Weight = 700
	case st.CodeBlock:
		base.Style.Family = look.mono
		base.Style.Size = look.size * 0.92
	}
	switch st.Align {
	case Center:
		base.Align = text.Center
	case End:
		base.Align = text.End
	default:
		base.Align = text.Start
	}
	if base.Width > 0 {
		left, right := ed.paraInsets(p)
		base.Width = max(base.Width-left-right, 1)
	}
	return base
}

// headingScale is how much larger than the text a heading of level is.
func headingScale(level int) float32 {
	switch level {
	case 1:
		return 1.8
	case 2:
		return 1.45
	case 3:
		return 1.2
	case 4:
		return 1.1
	case 5:
		return 1
	}
	return 0.9
}

// paraInsets returns the room left of paragraph p and right of it, for
// its indentation, its list's marker, a quotation's bar or a block of
// code's padding.
func (ed *editor) paraInsets(p int) (left, right float32) {
	r := ed.rich
	if r == nil {
		return 0, 0
	}
	st := r.para(p).style
	look := &r.look
	left = float32(st.Indent) * look.indent
	if st.List != NoList {
		left += look.indent
	}
	if st.Quote {
		left += look.indent * 0.75
	}
	if st.CodeBlock {
		left += look.size * 0.6
		right += look.size * 0.6
	}
	return left, right
}

// paraSpace returns the room above and below paragraph p: a heading's
// margins, a block of code's padding at its ends.
func (ed *editor) paraSpace(p int) (above, below float32) {
	r := ed.rich
	if r == nil {
		return 0, 0
	}
	st := r.para(p).style
	look := &r.look
	switch {
	case st.Heading > 0:
		if p > 0 {
			above = look.size * headingScale(st.Heading) * 0.6
		}
		below = look.size * 0.25
	case st.CodeBlock:
		if p == 0 || !r.para(p-1).style.CodeBlock {
			above = look.size * 0.4
		}
		if p+1 >= seqLen(r.shown.seq()) || !r.para(p+1).style.CodeBlock {
			below = look.size * 0.4
		}
	}
	return above, below
}

// span returns how a run with attributes a looks in a paragraph of style.
func (look *richLook) span(a TextAttributes, style ParagraphStyle) Span {
	s := Span{Font: a.Font, Size: a.Size, Weight: a.Weight, Italic: a.Italic, Color: a.Color,
		Underline: a.Underline, Strikethrough: a.Strikethrough, Background: a.Background}
	if a.Code {
		s.Font = look.mono
		if s.Size == 0 {
			s.Size = look.size * 0.92
		}
		if s.Background.A == 0 {
			s.Background = look.code
		}
	}
	if a.Link != "" {
		if s.Color.A == 0 {
			s.Color = look.link
		}
		s.Underline = true
	}
	if style.List == Tasks && style.Checked {
		s.Strikethrough = true
		if s.Color.A == 0 {
			s.Color = look.muted
		}
	}
	if style.Quote && s.Color.A == 0 {
		s.Color = look.muted
	}
	if a.Tag != "" && look.tagStyle != nil {
		look.tagStyle(a.Tag, &s)
	}
	return s
}

// paraSpans returns the spans of the layout of the paragraph from start to
// end: what its runs look like, and an input method's composition at the
// caret, in the typing attributes.
func (r *richEditor) paraSpans(ed *editor, start, end int) (string, *spanPaint) {
	p := ed.buf.para(start)
	para := r.para(p)
	compose, at := "", 0
	if ed.compose != "" && start <= ed.caret && ed.caret <= end {
		compose, at = ed.compose, ed.caret-start
	}
	var first *attrRun
	if len(para.runs) > 0 {
		first = unsafe.SliceData(para.runs)
	}
	if r.spans == nil {
		r.spans = map[uint64]*richSpans{}
	}
	c := r.spans[para.id]
	if c != nil && c.runs == first && c.nruns == len(para.runs) && c.style == para.style && c.compose == compose && c.at == at && c.look.same(&r.look) {
		c.used = true
		return c.key, c.paint
	}
	if c == nil {
		c = &richSpans{}
		r.spans[para.id] = c
	}
	*c = richSpans{runs: first, nruns: len(para.runs), style: para.style, compose: compose, at: at, look: r.look, used: true}
	var spans []Span
	var styles []text.Span
	pos := 0
	add := func(n int, a TextAttributes) {
		if n <= 0 {
			return
		}
		pos += n
		s := r.look.span(a, para.style)
		spans = append(spans, s)
		styles = append(styles, spanStyle(s, pos))
	}
	typing := TextAttributes{}
	if compose != "" {
		r.doc.lock()
		typing = r.doc.typingAttributes()
		r.doc.mu.Unlock()
	}
	done := 0
	for _, run := range para.runs {
		n := min(run.n, para.n-done)
		if compose != "" && done <= at && at < done+n {
			add(at-done, run.a)
			add(utf8.RuneCountInString(compose), typing)
			add(done+n-at, run.a)
			compose = ""
		} else {
			add(n, run.a)
		}
		done += n
		if done >= para.n {
			break
		}
	}
	if compose != "" {
		add(utf8.RuneCountInString(compose), typing)
	}
	c.key = text.EncodeSpans(styles)
	if len(spans) > 0 {
		c.paint = &spanPaint{spans: spans, styles: styles}
	}
	return c.key, c.paint
}

// same reports whether two looks make the same spans.
func (look *richLook) same(o *richLook) bool {
	return look.size == o.size && look.mono == o.mono && look.text == o.text && look.muted == o.muted &&
		look.link == o.link && look.code == o.code
}

// forgetSpans forgets the spans of paragraphs not laid out or painted since
// the last time it ran.
func (r *richEditor) forgetSpans() {
	for id, c := range r.spans {
		if !c.used {
			delete(r.spans, id)
		} else {
			c.used = false
		}
	}
}

// listNumber returns the number of the item of a numbered list that
// paragraph p is: one more than the item of the same level before it,
// counting from the first of the items next to each other, deeper ones
// between them skipped.
func (r *richEditor) listNumber(p int) int {
	if !r.numbersOf.Same(r.shown) {
		clear(r.numbers)
		r.numbersOf = r.shown
	}
	if r.numbers == nil {
		r.numbers = map[int]int{}
	}
	if n, ok := r.numbers[p]; ok {
		return n
	}
	st := r.para(p).style
	n := 1
	for q := p - 1; q >= 0; q-- {
		qs := r.para(q).style
		if qs.List == NoList || qs.Indent < st.Indent {
			break
		}
		if qs.Indent > st.Indent {
			continue
		}
		if qs.List != Numbers {
			break
		}
		n = r.listNumber(q) + 1
		break
	}
	r.numbers[p] = n
	return n
}

// marker returns what marks item p of a list: a bullet for its level, or
// its number.
func (r *richEditor) marker(p int, st ParagraphStyle) string {
	switch st.List {
	case Bullets:
		return []string{"•", "◦", "▪"}[st.Indent%3]
	case Numbers:
		n := r.listNumber(p)
		switch st.Indent % 3 {
		case 1:
			return alphaNumber(n) + "."
		case 2:
			return romanNumber(n) + "."
		}
		return strconv.Itoa(n) + "."
	}
	return ""
}

// alphaNumber returns n as letters: a, b, … z, aa.
func alphaNumber(n int) string {
	var b []byte
	for ; n > 0; n = (n - 1) / 26 {
		b = append([]byte{byte('a' + (n-1)%26)}, b...)
	}
	return string(b)
}

// romanNumber returns n in lowercase Roman numerals.
func romanNumber(n int) string {
	if n <= 0 || n >= 4000 {
		return strconv.Itoa(n)
	}
	vals := []int{1000, 900, 500, 400, 100, 90, 50, 40, 10, 9, 5, 4, 1}
	syms := []string{"m", "cm", "d", "cd", "c", "xc", "l", "xl", "x", "ix", "v", "iv", "i"}
	var b []byte
	for i, v := range vals {
		for n >= v {
			b = append(b, syms[i]...)
			n -= v
		}
	}
	return string(b)
}

// paintParagraph paints what paragraph p shows around its text, its
// layout l at (x, y) in the window: the bar of a quotation, the background
// of a block of code, the marker of a list's item. width is the content's.
func (r *richEditor) paintParagraph(ed *editor, pt *Painter, p int, l *text.Layout, x, y, top, height, width float32) {
	st := r.para(p).style
	look := &r.look
	left, _ := ed.paraInsets(p)
	if st.CodeBlock {
		// One block for the paragraphs of code next to each other, rounded
		// at its ends.
		radius := look.size * 0.3
		box := Rect{x - left + float32(st.Indent)*look.indent, top, width - float32(st.Indent)*look.indent, height}
		first := p == 0 || !r.para(p-1).style.CodeBlock
		last := p+1 >= seqLen(r.shown.seq()) || !r.para(p+1).style.CodeBlock
		fill := box
		if !first {
			fill.Y, fill.H = fill.Y-2*radius, fill.H+2*radius
		}
		if !last {
			fill.H += 2 * radius
		}
		saved := pt.clip
		pt.pushClip(box, [4]float32{})
		pt.Fill(fill, look.code, radius)
		pt.popClip()
		pt.clip = saved
	}
	if st.Quote {
		bar := x - left + float32(st.Indent)*look.indent
		pt.Fill(Rect{bar, top, 3, height}, look.quote, 1.5)
	}
	if st.List == NoList || len(l.Lines) == 0 {
		return
	}
	line := &l.Lines[0]
	size := look.size
	if st.Heading > 0 {
		size *= headingScale(st.Heading)
	}
	if st.List == Tasks {
		box := size * 0.95
		bx := x - look.indent + (look.indent-box)/2 - look.indent*0.1
		by := y + line.Baseline - size*0.82
		if st.Checked {
			pt.Fill(Rect{bx, by, box, box}, look.accent, box*0.25)
			var path Path
			path.MoveTo(bx+box*0.25, by+box*0.52).LineTo(bx+box*0.43, by+box*0.7).LineTo(bx+box*0.76, by+box*0.32)
			pt.StrokePath(&path, max(1.5, box*0.12), RGB(255, 255, 255))
		} else {
			pt.Stroke(Rect{bx, by, box, box}, look.muted, box*0.25, 1.25)
		}
		return
	}
	mark := r.marker(p, st)
	params := text.Params{Text: mark, Style: text.Style{Family: "", Size: size}}
	ml := textSystem().Layout(params)
	mx := x - ml.Width - look.indent*0.25
	if st.List == Bullets {
		mx = x - look.indent*0.5 - ml.Width/2
	}
	pt.textLayout(ml, mx, y+line.Baseline-ml.Lines[0].Baseline, look.text, textStyle{size: size}, nil)
}

// taskBox reports whether (x, y), in the content, is on the check box of a
// task, and which paragraph's.
func (r *richEditor) taskBox(ed *editor, x float32, y float64) (int, bool) {
	a := ed.area
	if a == nil {
		return 0, false
	}
	p := a.hs.at(y)
	if p < 0 || p >= seqLen(r.shown.seq()) {
		return 0, false
	}
	st := r.para(p).style
	if st.List != Tasks {
		return 0, false
	}
	left, _ := ed.paraInsets(p)
	return p, x >= left-r.look.indent && x < left+2
}
