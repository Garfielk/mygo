package ui

import (
	"runtime"
	"strings"
	"unicode"
	"unicode/utf8"
)

// wants reports whether a rich text editor takes a key that a text area
// would leave to shortcuts: those of its formats, and Tab in a list or a
// block of code.
func (r *richEditor) wants(ed *editor, k keyEvent) bool {
	if ed.readOnly {
		return false
	}
	m := k.mods
	if k.key == KeyTab && m&^Shift == 0 {
		st := r.para(ed.buf.para(ed.caret)).style
		return st.List != NoList || st.CodeBlock
	}
	_, ok := r.shortcut(k.key, m)
	return ok
}

// richCommand is a command of a rich text editor's shortcuts, as named in
// SurfaceCommand by the Format menu's roles.
type richCommand string

// shortcut returns the command key and mods press in a rich text editor:
// ⌘B, ⌘I, ⌘U, ⇧⌘X, ⌘E, ⌘K, ⇧⌘7, ⇧⌘8, ⇧⌘L, ⌘], ⌘[, ⌘{, ⌘|, ⌘} and ⌥⌘0–6
// on macOS, as Pages, Notes and Google Docs have them, with Control in
// place of Command elsewhere, where Ctrl+L, Ctrl+E, Ctrl+R and Ctrl+J
// align, as in Word.
func (r *richEditor) shortcut(key Key, mods Modifiers) (richCommand, bool) {
	mac := runtime.GOOS == "darwin"
	switch mods {
	case Cmd:
		switch key {
		case KeyB:
			return "bold", true
		case KeyI:
			return "italic", true
		case KeyU:
			return "underline", true
		case KeyK:
			return "link", true
		case KeyBracketRight:
			return "indent", true
		case KeyBracketLeft:
			return "outdent", true
		case KeyE:
			if mac {
				return "code", true
			}
			return "alignCenter", true
		case KeyL:
			if !mac {
				return "alignLeft", true
			}
		case KeyR:
			if !mac {
				return "alignRight", true
			}
		}
	case Cmd | Shift:
		switch key {
		case KeyX:
			return "strikethrough", true
		case Key7:
			return "numbers", true
		case Key8:
			return "bullets", true
		case KeyL:
			return "tasks", true
		case KeyBracketLeft:
			if mac {
				return "alignLeft", true
			}
		case KeyBracketRight:
			if mac {
				return "alignRight", true
			}
		case KeyBackslash:
			if mac {
				return "alignCenter", true
			}
		case KeyE:
			if !mac {
				return "code", true
			}
		}
	case Cmd | Alt:
		if key >= Key0 && key <= Key6 {
			return richCommand("heading" + string(rune('0'+int(key-Key0)))), true
		}
	}
	return "", false
}

// key handles a key in a rich text editor, before the text area does, and
// reports whether it took it.
func (r *richEditor) key(ed *editor, k editEvent) bool {
	if ed.readOnly {
		return false
	}
	if cmd, ok := r.shortcut(k.key, k.mods); ok {
		return r.run(ed, cmd)
	}
	a, z := ed.selection()
	p := ed.buf.para(ed.caret)
	st := r.para(p).style
	ps, pe := ed.buf.start(p), ed.buf.end(p)
	switch k.key {
	case KeyTab:
		switch {
		case k.mods&^Shift != 0:
			return false
		case st.List != NoList:
			if k.mods&Shift != 0 {
				return r.run(ed, "outdent")
			}
			return r.run(ed, "indent")
		case st.CodeBlock && k.mods == 0:
			ed.record(false)
			ed.insert("\t")
			return true
		}
	case KeyEnter:
		if k.mods&^Shift != 0 || ed.onEnter != nil {
			return false
		}
		return r.enter(ed, a, z, p, ps, pe, st)
	case KeyBackspace:
		if k.mods != 0 || a != z || ed.caret != ps || !special(st) {
			return false
		}
		// At the start of a list's item, a heading, a quotation or a block
		// of code, Backspace takes the style away before joining.
		r.command(ed, func(d *RichTextDocument) {
			d.FormatParagraphs(func(s *ParagraphStyle) {
				switch {
				case s.List != NoList && s.Indent > 0:
					s.Indent--
				case s.List != NoList:
					s.List, s.Checked = NoList, false
				case s.Heading > 0:
					s.Heading = 0
				case s.Quote:
					s.Quote = false
				case s.CodeBlock:
					s.CodeBlock = false
				case s.Indent > 0:
					s.Indent--
				}
			})
		})
		return true
	}
	return false
}

// special reports whether a paragraph of style st is more than a body
// paragraph, which Backspace at its start makes it before joining.
func special(st ParagraphStyle) bool {
	return st.List != NoList || st.Heading > 0 || st.Quote || st.CodeBlock || st.Indent > 0
}

// enter breaks the paragraph p, from ps to pe, holding the selection from
// a to z: a list's item continues the list, an empty one leaves it, a
// heading ends at its end, and "```" starts a block of code.
func (r *richEditor) enter(ed *editor, a, z, p, ps, pe int, st ParagraphStyle) bool {
	if a == z && ps == pe {
		exit := func(s *ParagraphStyle) bool {
			switch {
			case s.List != NoList && s.Indent > 0:
				s.Indent--
			case s.List != NoList:
				s.List, s.Checked = NoList, false
			case s.Quote:
				s.Quote = false
			case s.CodeBlock && p > 0 && r.para(p-1).style.CodeBlock:
				s.CodeBlock = false
			default:
				return false
			}
			return true
		}
		probe := st
		if exit(&probe) {
			r.command(ed, func(d *RichTextDocument) { d.FormatParagraphs(func(s *ParagraphStyle) { exit(s) }) })
			return true
		}
	}
	if r.markdown && a == z && ed.caret == pe && !st.CodeBlock {
		if line := ed.buf.text(p); strings.HasPrefix(line, "```") && !strings.Contains(line[3:], "`") {
			r.edit(ed, func(e *RichTextEdit) {
				e.ReplaceString(ps, pe, "", TextAttributes{})
				e.FormatParagraphs(ps, ps, func(s *ParagraphStyle) { *s = ParagraphStyle{CodeBlock: true, Indent: s.Indent} })
				e.Select(ps, ps)
			})
			return true
		}
	}
	atEnd := ed.caret == pe && a == z
	r.doc.lock()
	r.lockedPublish(ed)
	attrs := r.doc.typingAttributes()
	r.doc.mu.Unlock()
	ed.record(false)
	ed.caret = r.replace(ed, a, z, "\n")
	ed.anchor = ed.caret
	ed.visualSelection, ed.selected = false, nil
	r.doc.lock()
	r.lockedEdit(ed, func(e *RichTextEdit) {
		e.FormatParagraphs(ed.caret, ed.caret, func(s *ParagraphStyle) {
			if atEnd && s.Heading > 0 {
				s.Heading = 0
			}
			s.Checked = false
		})
	})
	r.doc.anchor, r.doc.caret = ed.caret, ed.caret
	keep := attrs
	r.doc.typing = &keep
	r.doc.mu.Unlock()
	ed.hasDesired = false
	if ed.area != nil {
		ed.area.reveal = true
	}
	return true
}

// run runs a command of the Format menu's roles or of a shortcut.
func (r *richEditor) run(ed *editor, cmd richCommand) bool {
	var fn func(d *RichTextDocument)
	switch cmd {
	case "bold":
		fn = (*RichTextDocument).ToggleBold
	case "italic":
		fn = (*RichTextDocument).ToggleItalic
	case "underline":
		fn = (*RichTextDocument).ToggleUnderline
	case "strikethrough":
		fn = (*RichTextDocument).ToggleStrikethrough
	case "code":
		fn = (*RichTextDocument).ToggleCode
	case "bullets":
		fn = func(d *RichTextDocument) { d.ToggleList(Bullets) }
	case "numbers":
		fn = func(d *RichTextDocument) { d.ToggleList(Numbers) }
	case "tasks":
		fn = func(d *RichTextDocument) { d.ToggleList(Tasks) }
	case "indent":
		fn = func(d *RichTextDocument) { d.Indent(1) }
	case "outdent":
		fn = func(d *RichTextDocument) { d.Indent(-1) }
	case "alignLeft":
		fn = func(d *RichTextDocument) { d.SetAlign(Start) }
	case "alignCenter":
		fn = func(d *RichTextDocument) { d.SetAlign(Center) }
	case "alignRight":
		fn = func(d *RichTextDocument) { d.SetAlign(End) }
	case "quote":
		fn = (*RichTextDocument).ToggleQuote
	case "codeBlock":
		fn = (*RichTextDocument).ToggleCodeBlock
	case "link":
		if r.onLinkCommand != nil {
			r.publish(ed)
			r.onLinkCommand()
			if r.rt != nil {
				r.rt.consumed = true
			}
			return true
		}
		a, z := ed.selection()
		url := strings.TrimSpace(ed.buf.slice(a, z))
		if a == z || !looksLikeURL(url) {
			return true
		}
		fn = func(d *RichTextDocument) { d.SetLink(url) }
	default:
		if level, ok := strings.CutPrefix(string(cmd), "heading"); ok && len(level) == 1 {
			fn = func(d *RichTextDocument) { d.SetHeading(int(level[0] - '0')) }
		}
	}
	if fn == nil {
		return false
	}
	r.command(ed, fn)
	return true
}

// looksLikeURL reports whether s is a URL, as ⌘K links one to itself.
func looksLikeURL(s string) bool {
	if strings.ContainsAny(s, " \n\t") {
		return false
	}
	for _, prefix := range []string{"http://", "https://", "mailto:", "www."} {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}

// typed runs the Markdown shortcuts after s was typed at the caret.
func (r *richEditor) typed(ed *editor, s string) {
	if !r.markdown || ed.readOnly || utf8.RuneCountInString(s) != 1 {
		return
	}
	p := ed.buf.para(ed.caret)
	ps := ed.buf.start(p)
	st := r.para(p).style
	if st.CodeBlock {
		return
	}
	before := []rune(ed.buf.slice(ps, ed.caret))
	if s == " " {
		r.blockShortcut(ed, ps, string(before[:len(before)-1]), st)
		return
	}
	switch s {
	case "*", "_", "`", "~":
		r.inlineShortcut(ed, ps, before)
	}
}

// blockShortcut turns what was typed before a space at the start of a
// paragraph into its style: "# " a heading, "- " a list, "> " a quotation.
func (r *richEditor) blockShortcut(ed *editor, ps int, prefix string, st ParagraphStyle) {
	var set func(s *ParagraphStyle)
	switch {
	case prefix != "" && strings.Trim(prefix, "#") == "" && len(prefix) <= 6:
		level := len(prefix)
		set = func(s *ParagraphStyle) { s.Heading, s.List, s.Checked = level, NoList, false }
	case prefix == "-" || prefix == "*" || prefix == "+":
		set = func(s *ParagraphStyle) { s.List, s.Heading = Bullets, 0 }
	case prefix == "[]" || prefix == "[ ]" || prefix == "- [ ]" || prefix == "-[ ]":
		set = func(s *ParagraphStyle) { s.List, s.Heading, s.Checked = Tasks, 0, false }
	case prefix == "[x]" || prefix == "- [x]" || prefix == "[X]":
		set = func(s *ParagraphStyle) { s.List, s.Heading, s.Checked = Tasks, 0, true }
	case prefix == ">":
		set = func(s *ParagraphStyle) { s.Quote = true }
	case len(prefix) >= 2 && (prefix[len(prefix)-1] == '.' || prefix[len(prefix)-1] == ')') && strings.Trim(prefix[:len(prefix)-1], "0123456789") == "":
		set = func(s *ParagraphStyle) { s.List, s.Heading = Numbers, 0 }
	}
	if set == nil {
		return
	}
	n := utf8.RuneCountInString(prefix) + 1
	r.edit(ed, func(e *RichTextEdit) {
		e.ReplaceString(ps, ps+n, "", TextAttributes{})
		e.FormatParagraphs(ps, ps, set)
		e.Select(ps, ps)
	})
}

// inlineShortcut formats what a pair of marks typed around words holds:
// **bold**, *italic*, _italic_, `code` and ~~strikethrough~~.
func (r *richEditor) inlineShortcut(ed *editor, ps int, before []rune) {
	n := len(before)
	last := before[n-1]
	mark := string(last)
	if (last == '*' || last == '~' || last == '_') && n >= 2 && before[n-2] == last {
		mark += mark
	}
	if last == '~' && len(mark) == 1 {
		return
	}
	m := len([]rune(mark))
	// The opening mark: the nearest before the closing one with words
	// between, which touch both marks.
	closeAt := n - m
	if closeAt <= 0 || unicode.IsSpace(before[closeAt-1]) {
		return
	}
	open := -1
	for i := closeAt - 1; i >= 0; i-- {
		if string(before[i:min(i+m, n)]) == mark && i+m < closeAt {
			// Not the inside of a longer mark, and words right after it.
			if (i > 0 && before[i-1] == last) || before[i+m] == last || unicode.IsSpace(before[i+m]) {
				continue
			}
			open = i
			break
		}
	}
	if open < 0 {
		return
	}
	set := func(a *TextAttributes) {
		switch mark {
		case "**", "__":
			a.Weight = 700
		case "*", "_":
			a.Italic = true
		case "`":
			a.Code = true
		case "~~":
			a.Strikethrough = true
		}
	}
	start := ps + open
	end := ps + closeAt
	r.edit(ed, func(e *RichTextEdit) {
		e.ReplaceString(end, end+m, "", TextAttributes{})
		e.ReplaceString(start, start+m, "", TextAttributes{})
		e.Format(start, end-m, set)
		e.Select(end-m, end-m)
	})
	// What is typed next is outside the format, as after a closing mark.
	r.doc.lock()
	plain := TextAttributes{}
	if start > ps {
		plain = r.doc.text.AttributesAt(start - 1).extending()
	}
	r.doc.typing = &plain
	r.doc.mu.Unlock()
}

// pressed handles a press of the primary button in a rich text editor at
// (x, y) in its content, before the text area does: on the check box of a
// task it checks it; on a link it notes the link, which opens when the
// press ends without a drag. It reports whether it took the press.
func (r *richEditor) pressed(ed *editor, x float32, y float64, clicks int) bool {
	r.pressLink = ""
	if p, ok := r.taskBox(ed, x, y); ok && clicks == 1 && !ed.readOnly {
		start := ed.buf.start(p)
		r.command(ed, func(d *RichTextDocument) { d.ToggleChecked(start) })
		return true
	}
	if clicks == 1 && ed.pressMods&Shift == 0 {
		pos := ed.area.positionAt(ed, x, y)
		i := pos.Index
		if i < ed.buf.n {
			if link := r.shown.AttributesAt(i).Link; link != "" {
				r.pressLink = link
			}
		}
	}
	return false
}

// released opens the link the press began on, unless it selected text.
func (r *richEditor) released(ed *editor) {
	link := r.pressLink
	r.pressLink = ""
	if link == "" || ed.caret != ed.anchor {
		return
	}
	switch {
	case r.onLink != nil:
		r.onLink(link)
	case r.rt != nil:
		r.rt.host.openURL(link, nil)
	}
}
